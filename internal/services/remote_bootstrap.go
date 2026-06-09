package services

import (
	"context"
	"fmt"

	"github.com/afahey03/docklab/internal/models"
)

type RemoteHealthStatus struct {
	RuntimeTarget   string `json:"runtime_target"`
	PublicIP        string `json:"public_ip"`
	SSHReachable    bool   `json:"ssh_reachable"`
	DockerAvailable bool   `json:"docker_available"`
	WorkspaceReady  bool   `json:"workspace_ready"`
	Error           string `json:"error,omitempty"`
}

type BootstrapProgressFunc func(message string)

type RemoteBootstrapService struct {
	resolver *RuntimeResolver
}

func NewRemoteBootstrapService(resolver *RuntimeResolver) *RemoteBootstrapService {
	return &RemoteBootstrapService{resolver: resolver}
}

func (s *RemoteBootstrapService) BootstrapAfterProvision(ctx context.Context, env *models.Environment, onProgress BootstrapProgressFunc) (remoteContainerID string, err error) {
	if env == nil {
		return "", fmt.Errorf("environment is nil")
	}
	if env.PublicIP == "" {
		return "", fmt.Errorf("environment has no public IP")
	}

	factory := s.resolver.SSHFactory()
	if !factory.PrivateKeyConfigured() {
		return "", ErrSSHPrivateKeyMissing
	}

	reportProgress(onProgress, "waiting for SSH on EC2")
	if err := factory.WaitForSSH(ctx, env.PublicIP); err != nil {
		return "", fmt.Errorf("wait for ssh: %w", err)
	}

	reportProgress(onProgress, "waiting for Docker on EC2")
	if err := factory.WaitForDocker(ctx, env.PublicIP); err != nil {
		return "", fmt.Errorf("wait for docker: %w", err)
	}

	reportProgress(onProgress, fmt.Sprintf("starting remote workspace container (%s)", env.Image))
	remoteRuntime := NewSSHDockerRuntime(factory, env.PublicIP)
	containerID, err := remoteRuntime.EnsureWorkspace(ctx, env.ID, env.Image, map[string]string{
		"docklab.user_email":     env.UserEmail,
		"docklab.name":           env.Name,
		"docklab.environment_id": env.ID,
	})
	if err != nil {
		return "", fmt.Errorf("ensure remote workspace: %w", err)
	}

	return containerID, nil
}

func (s *RemoteBootstrapService) CheckHealth(ctx context.Context, env *models.Environment) RemoteHealthStatus {
	status := RemoteHealthStatus{
		RuntimeTarget: env.RuntimeTarget,
		PublicIP:      env.PublicIP,
	}

	if env.PublicIP == "" {
		status.Error = "environment has no public IP"
		return status
	}

	factory := s.resolver.SSHFactory()
	if !factory.PrivateKeyConfigured() {
		status.Error = ErrSSHPrivateKeyMissing.Error()
		return status
	}

	client, err := factory.Connect(ctx, env.PublicIP)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	_ = client.Close()
	status.SSHReachable = true

	if _, err := factory.Run(ctx, env.PublicIP, remoteShellCommand("docker info >/dev/null 2>&1")); err != nil {
		status.Error = err.Error()
		return status
	}

	status.DockerAvailable = true

	workspaceName := RemoteContainerName(env.ID)
	inspectCommand := fmt.Sprintf("docker inspect -f '{{.State.Running}}' %s", workspaceName)
	if _, err := factory.Run(ctx, env.PublicIP, remoteShellCommand(inspectCommand)); err == nil {
		status.WorkspaceReady = true
	}

	switch {
	case env.RuntimeTarget == runtimeTargetRemote && status.WorkspaceReady:
		// fully healthy remote workspace
	case env.RuntimeTarget == runtimeTargetRemote && !status.WorkspaceReady:
		status.Error = "remote runtime is configured but the workspace container is missing or stopped on EC2"
	case hasCloudInstance(env) && env.RuntimeTarget != runtimeTargetRemote:
		status.Error = "EC2 is reachable but remote workspace bootstrap is not complete"
	case hasCloudInstance(env) && !status.WorkspaceReady:
		status.Error = "EC2 is reachable but the remote workspace container is not running yet"
	}

	return status
}

func reportProgress(onProgress BootstrapProgressFunc, message string) {
	if onProgress != nil {
		onProgress(message)
	}
}

func (s *RemoteBootstrapService) RevertToLocal(ctx context.Context, env *models.Environment) (localContainerID string, err error) {
	if env == nil {
		return "", fmt.Errorf("environment is nil")
	}

	localRuntime := s.resolver.LocalRuntime()
	containerID, err := localRuntime.CreateWorkspace(ctx, env.Name, env.Image, map[string]string{
		"docklab.user_email": env.UserEmail,
		"docklab.name":       env.Name,
	})
	if err != nil {
		return "", err
	}

	if env.RuntimeTarget == runtimeTargetRemote && env.PublicIP != "" && env.ContainerID != "" {
		if remoteRuntime, remoteErr := s.resolver.ForEnvironment(env); remoteErr == nil {
			_ = remoteRuntime.DeleteWorkspace(ctx, workspaceContainerRef(env))
		}
	}

	return containerID, nil
}
