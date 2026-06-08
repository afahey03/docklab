package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/afahey03/docklab/internal/models"
	"golang.org/x/crypto/ssh"
)

var (
	ErrSSHPrivateKeyMissing     = errors.New("DOKLAB_SSH_PRIVATE_KEY_PATH is not configured")
	ErrSSHPrivateKeyInvalid     = errors.New("failed to parse SSH private key")
	ErrSSHConnectionFailed      = errors.New("failed to connect to remote host over SSH")
	ErrRemoteRuntimeUnavailable = errors.New("remote runtime is not available for this environment")
)

type SSHConfig struct {
	User           string
	Port           int
	PrivateKeyPath string
	ConnectTimeout time.Duration
	BootstrapMax   time.Duration
}

type SSHClientFactory struct {
	cfg SSHConfig
}

func NewSSHClientFactory(cfg SSHConfig) *SSHClientFactory {
	return &SSHClientFactory{cfg: cfg}
}

func (f *SSHClientFactory) PrivateKeyConfigured() bool {
	return f.cfg.PrivateKeyPath != ""
}

func (f *SSHClientFactory) loadSigner() (ssh.Signer, error) {
	if f.cfg.PrivateKeyPath == "" {
		return nil, ErrSSHPrivateKeyMissing
	}

	keyBytes, err := os.ReadFile(f.cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read ssh private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSSHPrivateKeyInvalid, err)
	}

	return signer, nil
}

func (f *SSHClientFactory) Connect(ctx context.Context, host string) (*ssh.Client, error) {
	signer, err := f.loadSigner()
	if err != nil {
		return nil, err
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", f.cfg.Port))
	dialer := net.Dialer{Timeout: f.cfg.ConnectTimeout}

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSSHConnectionFailed, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, &ssh.ClientConfig{
		User:            f.cfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         f.cfg.ConnectTimeout,
	})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %v", ErrSSHConnectionFailed, err)
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

func (f *SSHClientFactory) Run(ctx context.Context, host, command string) (string, error) {
	client, err := f.Connect(ctx, host)
	if err != nil {
		return "", err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("create ssh session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(command)
	if err != nil {
		trimmed := string(output)
		if trimmed == "" {
			return "", fmt.Errorf("remote command failed: %w", err)
		}
		return "", fmt.Errorf("remote command failed: %s", trimmed)
	}

	return string(output), nil
}

func (f *SSHClientFactory) WaitForSSH(ctx context.Context, host string) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(f.cfg.BootstrapMax)
	}

	backoff := 5 * time.Second
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for ssh on %s", host)
		}

		client, err := f.Connect(ctx, host)
		if err == nil {
			_ = client.Close()
			return nil
		}
		if isNonRetryableSSHError(err) {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
}

func (f *SSHClientFactory) WaitForDocker(ctx context.Context, host string) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(f.cfg.BootstrapMax)
	}

	backoff := 5 * time.Second
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for docker on %s", host)
		}

		_, err := f.Run(ctx, host, "docker info >/dev/null 2>&1")
		if err == nil {
			return nil
		}
		if isNonRetryableSSHError(err) {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
}

func UsesRemoteRuntime(env *models.Environment) bool {
	if env == nil {
		return false
	}
	return env.RuntimeTarget == runtimeTargetRemote
}

const runtimeTargetLocal = "local"
const runtimeTargetRemote = "remote"

func isNonRetryableSSHError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrSSHPrivateKeyMissing) || errors.Is(err, ErrSSHPrivateKeyInvalid) {
		return true
	}
	return strings.Contains(err.Error(), "read ssh private key:")
}
