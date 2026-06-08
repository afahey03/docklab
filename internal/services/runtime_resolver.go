package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/afahey03/docklab/internal/config"
	"github.com/afahey03/docklab/internal/models"
)

type RuntimeResolver struct {
	local      ContainerRuntime
	sshFactory *SSHClientFactory
}

func NewRuntimeResolver(local ContainerRuntime, cfg config.Config) *RuntimeResolver {
	return &RuntimeResolver{
		local: local,
		sshFactory: NewSSHClientFactory(SSHConfig{
			User:           cfg.SSHUser,
			Port:           cfg.SSHPort,
			PrivateKeyPath: cfg.SSHPrivateKeyPath,
			ConnectTimeout: timeDurationSeconds(cfg.SSHConnectTimeout),
			BootstrapMax:   timeDurationSeconds(cfg.RemoteBootstrapMax),
		}),
	}
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
