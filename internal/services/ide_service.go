package services

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/afahey03/docklab/internal/models"
)

var ErrIDEDisabled = errors.New("browser IDE is disabled on this server")
var ErrIDEUnsupportedRuntime = errors.New("browser IDE is not supported for the kubernetes runtime")

// dockerCommandRunner is satisfied by both DockerCLIRuntime and SSHDockerRuntime.
type dockerCommandRunner interface {
	runDocker(ctx context.Context, args ...string) (string, error)
}

type IDEStatus struct {
	Running  bool   `json:"running"`
	URL      string `json:"url,omitempty"`
	Password string `json:"password,omitempty"`
}

// IDEService runs a code-server sidecar container next to a workspace container.
// The sidecar shares the workspace's volumes (including /workspace), giving a full
// VS Code experience in the browser against the same files as the terminal.
type IDEService struct {
	resolver   *RuntimeResolver
	enabled    bool
	image      string
	remotePort int
}

func NewIDEService(resolver *RuntimeResolver, enabled bool, image string, remotePort int) *IDEService {
	if strings.TrimSpace(image) == "" {
		image = "codercom/code-server:latest"
	}
	if remotePort <= 0 {
		remotePort = 8443
	}
	return &IDEService{
		resolver:   resolver,
		enabled:    enabled,
		image:      image,
		remotePort: remotePort,
	}
}

func IDEContainerName(env *models.Environment) string {
	return "docklab-ide-" + workspaceContainerName(env)
}

// workspaceContainerName is the stable docker name of the workspace container.
func workspaceContainerName(env *models.Environment) string {
	if env == nil {
		return ""
	}
	if env.RuntimeTarget == runtimeTargetRemote {
		return RemoteContainerName(env.ID)
	}
	return env.Name
}

func (s *IDEService) runnerFor(env *models.Environment) (dockerCommandRunner, error) {
	if !s.enabled {
		return nil, ErrIDEDisabled
	}
	if s.resolver.LocalBackend() == RuntimeBackendKubernetes && env.RuntimeTarget != runtimeTargetRemote {
		return nil, ErrIDEUnsupportedRuntime
	}

	runtime, err := s.resolver.ForEnvironment(env)
	if err != nil {
		return nil, err
	}
	runner, ok := runtime.(dockerCommandRunner)
	if !ok {
		return nil, ErrIDEUnsupportedRuntime
	}
	return runner, nil
}

// Start launches (or replaces) the code-server sidecar and returns its URL and password.
func (s *IDEService) Start(ctx context.Context, env *models.Environment) (*IDEStatus, error) {
	runner, err := s.runnerFor(env)
	if err != nil {
		return nil, err
	}
	if env.Status != statusRunning {
		return nil, &ProvisionValidationError{Code: "environment_not_running", Message: "environment must be running to open the IDE"}
	}

	workspaceRef := workspaceContainerRef(env)
	ideName := IDEContainerName(env)

	// Replace any previous sidecar so restarts pick up fresh credentials.
	_, _ = runner.runDocker(ctx, "rm", "-f", ideName)

	// The code-server image runs as a non-root user; make the shared volume writable.
	_, _ = runner.runDocker(ctx, "exec", workspaceRef, "chmod", "777", "/workspace")

	passwordBytes := make([]byte, 12)
	if _, err := cryptorand.Read(passwordBytes); err != nil {
		return nil, err
	}
	password := hex.EncodeToString(passwordBytes)

	portMapping := "127.0.0.1:0:8080"
	if env.RuntimeTarget == runtimeTargetRemote {
		portMapping = fmt.Sprintf("%d:8080", s.remotePort)
	}

	args := []string{
		"run", "-d",
		"--name", ideName,
		"--volumes-from", workspaceRef,
		"-e", "PASSWORD=" + password,
		"-p", portMapping,
		"--label", "docklab.ide=true",
		"--label", "docklab.environment_id=" + env.ID,
		s.image,
		"--bind-addr", "0.0.0.0:8080",
		"/workspace",
	}
	if _, err := runner.runDocker(ctx, args...); err != nil {
		return nil, fmt.Errorf("start ide sidecar: %w", err)
	}

	url, err := s.resolveURL(ctx, runner, env, ideName)
	if err != nil {
		return nil, err
	}

	return &IDEStatus{Running: true, URL: url, Password: password}, nil
}

// Stop removes the IDE sidecar container.
func (s *IDEService) Stop(ctx context.Context, env *models.Environment) error {
	runner, err := s.runnerFor(env)
	if err != nil {
		return err
	}
	_, err = runner.runDocker(ctx, "rm", "-f", IDEContainerName(env))
	if err != nil && isWorkspaceDeleteIgnorable(err) {
		return nil
	}
	return err
}

// Status reports whether the IDE sidecar is running, its URL, and its password.
func (s *IDEService) Status(ctx context.Context, env *models.Environment) (*IDEStatus, error) {
	runner, err := s.runnerFor(env)
	if err != nil {
		return nil, err
	}

	ideName := IDEContainerName(env)
	stateOutput, err := runner.runDocker(ctx, "inspect", "-f", "{{.State.Running}}", ideName)
	if err != nil || strings.TrimSpace(stateOutput) != "true" {
		return &IDEStatus{Running: false}, nil
	}

	url, err := s.resolveURL(ctx, runner, env, ideName)
	if err != nil {
		return &IDEStatus{Running: true}, nil
	}

	status := &IDEStatus{Running: true, URL: url}

	// Recover the password from the container env so the dashboard can re-show it.
	envOutput, err := runner.runDocker(ctx, "inspect", "-f", "{{range .Config.Env}}{{println .}}{{end}}", ideName)
	if err == nil {
		for _, line := range strings.Split(envOutput, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "PASSWORD=") {
				status.Password = strings.TrimPrefix(line, "PASSWORD=")
				break
			}
		}
	}

	return status, nil
}

func (s *IDEService) resolveURL(ctx context.Context, runner dockerCommandRunner, env *models.Environment, ideName string) (string, error) {
	if env.RuntimeTarget == runtimeTargetRemote {
		if env.PublicIP == "" {
			return "", fmt.Errorf("environment has no public IP for remote IDE access")
		}
		return fmt.Sprintf("http://%s:%d", env.PublicIP, s.remotePort), nil
	}

	portOutput, err := runner.runDocker(ctx, "port", ideName, "8080/tcp")
	if err != nil {
		return "", fmt.Errorf("resolve ide port: %w", err)
	}

	// docker port may return multiple lines (IPv4/IPv6); use the first.
	firstLine := strings.TrimSpace(strings.Split(portOutput, "\n")[0])
	lastColon := strings.LastIndex(firstLine, ":")
	if lastColon < 0 {
		return "", fmt.Errorf("unexpected docker port output: %s", firstLine)
	}
	hostPort := firstLine[lastColon+1:]

	return "http://localhost:" + hostPort, nil
}
