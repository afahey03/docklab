package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/afahey03/docklab/internal/config"
	"github.com/afahey03/docklab/internal/models"
)

const (
	RuntimeBackendDocker     = "docker"
	RuntimeBackendKubernetes = "kubernetes"
)

type RuntimeResolver struct {
	local        ContainerRuntime
	sshFactory   *SSHClientFactory
	localBackend string
	k8sRuntime   *KubernetesRuntime
}

func NewRuntimeResolver(local ContainerRuntime, cfg config.Config) *RuntimeResolver {
	resolver := &RuntimeResolver{
		local:        local,
		localBackend: RuntimeBackendDocker,
		sshFactory: NewSSHClientFactory(SSHConfig{
			User:           cfg.SSHUser,
			Port:           cfg.SSHPort,
			PrivateKeyPath: cfg.SSHPrivateKeyPath,
			ConnectTimeout: timeDurationSeconds(cfg.SSHConnectTimeout),
			BootstrapMax:   timeDurationSeconds(cfg.RemoteBootstrapMax),
		}),
	}

	if strings.EqualFold(strings.TrimSpace(cfg.RuntimeBackend), RuntimeBackendKubernetes) {
		resolver.localBackend = RuntimeBackendKubernetes
		resolver.k8sRuntime = NewKubernetesRuntime(cfg.KubernetesNamespace, cfg.KubernetesContext)
		resolver.local = resolver.k8sRuntime
	}

	return resolver
}

// LocalBackend reports which orchestrator backs non-remote workspaces ("docker" or
// "kubernetes").
func (r *RuntimeResolver) LocalBackend() string {
	return r.localBackend
}

// KubernetesRuntime returns the K8s runtime when the kubernetes backend is active.
func (r *RuntimeResolver) KubernetesRuntime() *KubernetesRuntime {
	return r.k8sRuntime
}

func timeDurationSeconds(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}

func (r *RuntimeResolver) SSHFactory() *SSHClientFactory {
	return r.sshFactory
}

func (r *RuntimeResolver) ForEnvironment(env *models.Environment) (ContainerRuntime, error) {
	if env == nil {
		return nil, fmt.Errorf("environment is nil")
	}
	if env.RuntimeTarget != runtimeTargetRemote {
		return r.local, nil
	}
	if strings.TrimSpace(env.PublicIP) == "" {
		return nil, ErrRemoteRuntimeUnavailable
	}
	if !r.sshFactory.PrivateKeyConfigured() {
		return nil, ErrSSHPrivateKeyMissing
	}

	return NewSSHDockerRuntime(r.sshFactory, env.PublicIP), nil
}

func (r *RuntimeResolver) LocalRuntime() ContainerRuntime {
	return r.local
}

func (r *RuntimeResolver) BootstrapTimeout() time.Duration {
	return r.sshFactory.cfg.BootstrapMax
}
