package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/afahey03/docklab/internal/models"
)

func RemoteContainerName(environmentID string) string {
	return fmt.Sprintf("docklab-%s", environmentID)
}

func hasCloudInstance(env *models.Environment) bool {
	if env == nil {
		return false
	}
	return env.InstanceID != ""
}

type SSHDockerRuntime struct {
	factory *SSHClientFactory
	host    string
}

func NewSSHDockerRuntime(factory *SSHClientFactory, host string) *SSHDockerRuntime {
	return &SSHDockerRuntime{
		factory: factory,
		host:    strings.TrimSpace(host),
	}
}

func (r *SSHDockerRuntime) runDocker(ctx context.Context, args ...string) (string, error) {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	command := "docker " + strings.Join(quoted, " ")
	return r.factory.Run(ctx, r.host, command)
}

func (r *SSHDockerRuntime) CreateWorkspace(ctx context.Context, name, image string, labels map[string]string) (string, error) {
	args := []string{"run", "-d", "--name", name}
	for key, value := range labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, value))
	}
	args = append(args, image, "sleep", "infinity")

	return r.runDocker(ctx, args...)
}

func (r *SSHDockerRuntime) InspectWorkspace(ctx context.Context, name string) (containerID string, running bool, err error) {
	idOutput, inspectErr := r.runDocker(ctx, "inspect", "-f", "{{.Id}}", name)
	if inspectErr != nil {
		return "", false, inspectErr
	}

	containerID = strings.TrimSpace(idOutput)
	if containerID == "" {
		return "", false, fmt.Errorf("container %s not found", name)
	}

	stateOutput, stateErr := r.runDocker(ctx, "inspect", "-f", "{{.State.Running}}", name)
	if stateErr != nil {
		return containerID, false, nil
	}

	return containerID, strings.TrimSpace(stateOutput) == "true", nil
}

func (r *SSHDockerRuntime) EnsureWorkspace(ctx context.Context, environmentID, image string, labels map[string]string) (string, error) {
	name := RemoteContainerName(environmentID)

	if containerID, running, err := r.InspectWorkspace(ctx, name); err == nil {
		if running {
			return containerID, nil
		}
		if startErr := r.StartWorkspace(ctx, name); startErr == nil {
			return containerID, nil
		}
		_ = r.DeleteWorkspace(ctx, name)
	}

	containerID, err := r.CreateWorkspace(ctx, name, image, labels)
	if err != nil {
		if existingID, _, inspectErr := r.InspectWorkspace(ctx, name); inspectErr == nil {
			if startErr := r.StartWorkspace(ctx, name); startErr == nil {
				return existingID, nil
			}
		}
		return "", err
	}

	return containerID, nil
}

func (r *SSHDockerRuntime) StartWorkspace(ctx context.Context, containerID string) error {
	_, err := r.runDocker(ctx, "start", containerID)
	return err
}

func (r *SSHDockerRuntime) StopWorkspace(ctx context.Context, containerID string) error {
	_, err := r.runDocker(ctx, "stop", containerID)
	return err
}

func (r *SSHDockerRuntime) DeleteWorkspace(ctx context.Context, containerID string) error {
	_, err := r.runDocker(ctx, "rm", "-f", containerID)
	return err
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
