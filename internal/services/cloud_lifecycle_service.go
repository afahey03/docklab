package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/afahey03/docklab/internal/models"
	"github.com/afahey03/docklab/internal/repositories"
)

const (
	cloudIdleStopMessage           = "EC2 stopped automatically due to idle cloud policy"
	cloudIdleTerminateMessage      = "EC2 terminated automatically due to idle cloud policy"
	defaultEC2StateWaitTimeout     = 10 * time.Minute
	defaultCloudLifecycleCheckStep = 5 * time.Minute
)

type CloudLifecyclePolicy struct {
	Enabled                   bool `json:"enabled"`
	WorkspaceIdleStopMinutes  int  `json:"workspace_idle_stop_minutes"`
	CloudIdleStopMinutes      int  `json:"cloud_idle_stop_minutes"`
	CloudIdleTerminateMinutes int  `json:"cloud_idle_terminate_minutes"`
}

type CloudLifecycleService struct {
	environmentRepo repositories.EnvironmentRepository
	operationRepo   repositories.OperationRepository
	resolver        *RuntimeResolver
	terraformRunner TerraformRunner
	ec2             EC2InstanceClient
	policy          CloudLifecyclePolicy
	log             *slog.Logger
}

func NewCloudLifecycleService(
	environmentRepo repositories.EnvironmentRepository,
	operationRepo repositories.OperationRepository,
	resolver *RuntimeResolver,
	terraformRunner TerraformRunner,
	ec2 EC2InstanceClient,
	workspaceIdleStopMinutes int,
	cloudIdleStopMinutes int,
	cloudIdleTerminateMinutes int,
	enabled bool,
	log *slog.Logger,
) *CloudLifecycleService {
	if workspaceIdleStopMinutes <= 0 {
		workspaceIdleStopMinutes = 60
	}
	if cloudIdleStopMinutes <= 0 {
		cloudIdleStopMinutes = workspaceIdleStopMinutes * 2
	}
	if cloudIdleTerminateMinutes <= 0 {
		cloudIdleTerminateMinutes = 24 * 60
	}

	return &CloudLifecycleService{
		environmentRepo: environmentRepo,
		operationRepo:   operationRepo,
		resolver:        resolver,
		terraformRunner: terraformRunner,
		ec2:             ec2,
		policy: CloudLifecyclePolicy{
			Enabled:                   enabled,
			WorkspaceIdleStopMinutes:  workspaceIdleStopMinutes,
			CloudIdleStopMinutes:      cloudIdleStopMinutes,
			CloudIdleTerminateMinutes: cloudIdleTerminateMinutes,
		},
		log: log,
	}
}

func (s *CloudLifecycleService) Policy() CloudLifecyclePolicy {
	return s.policy
}

func (s *CloudLifecycleService) Start(ctx context.Context, workspaceLifecycle *LifecycleService) {
	workspaceLifecycle.Start(ctx)

	if !s.policy.Enabled {
		s.log.Info("cloud lifecycle worker disabled")
		return
	}

	checkInterval := s.checkInterval()
	s.log.Info("cloud lifecycle worker started",
		"cloud_idle_stop", time.Duration(s.policy.CloudIdleStopMinutes)*time.Minute,
		"cloud_idle_terminate", time.Duration(s.policy.CloudIdleTerminateMinutes)*time.Minute,
		"check_interval", checkInterval,
	)

	go func() {
		s.runOnce(ctx)

		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runOnce(ctx)
			}
		}
	}()
}

func (s *CloudLifecycleService) checkInterval() time.Duration {
	shortest := time.Duration(s.policy.WorkspaceIdleStopMinutes) * time.Minute
	if cloudStop := time.Duration(s.policy.CloudIdleStopMinutes) * time.Minute; cloudStop > 0 && cloudStop < shortest {
		shortest = cloudStop
	}
	if cloudTerminate := time.Duration(s.policy.CloudIdleTerminateMinutes) * time.Minute; cloudTerminate > 0 && cloudTerminate < shortest {
		shortest = cloudTerminate
	}

	interval := shortest / 2
	if interval < minimumLifecycleCheckInterval {
		interval = minimumLifecycleCheckInterval
	}
	if interval > defaultLifecycleCheckInterval {
		interval = defaultLifecycleCheckInterval
	}
	return interval
}

func (s *CloudLifecycleService) runOnce(ctx context.Context) {
	s.stopIdleCloudInstances(ctx)
	s.terminateIdleCloudInstances(ctx)
}

func (s *CloudLifecycleService) stopIdleCloudInstances(ctx context.Context) {
	cutoff := time.Now().Add(-time.Duration(s.policy.CloudIdleStopMinutes) * time.Minute)
	envs, err := s.environmentRepo.ListIdleProvisionedCloudSince(ctx, cutoff)
	if err != nil {
		s.log.Error("cloud lifecycle: failed to list idle provisioned cloud environments", "error", err)
		return
	}

	for _, env := range envs {
		if blocked, err := s.hasBlockingOperation(ctx, env.ID, env.UserEmail); err != nil {
			s.log.Error("cloud lifecycle: failed to check operations", "environment_id", env.ID, "error", err)
			continue
		} else if blocked {
			continue
		}

		if env.Status == statusRunning {
			if err := s.stopWorkspaceContainer(ctx, &env); err != nil {
				s.log.Warn("cloud lifecycle: workspace stop failed before ec2 stop; continuing",
					"environment_id", env.ID,
					"error", err,
				)
			}
		}

		s.log.Info("cloud lifecycle: stopping idle ec2 instance",
			"environment_id", env.ID,
			"instance_id", env.InstanceID,
			"last_activity_at", env.LastActivityAt,
		)

		stopCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := s.ec2.StopInstance(stopCtx, env.CloudRegion, env.InstanceID)
		cancel()
		if err != nil {
			s.log.Error("cloud lifecycle: failed to stop ec2 instance",
				"environment_id", env.ID,
				"error", err,
			)
			_, _ = s.environmentRepo.UpdateProvisioning(
				ctx,
				env.ID,
				env.UserEmail,
				env.CloudStatus,
				env.CloudRegion,
				env.CloudInstanceType,
				env.CloudKeyName,
				env.InstanceID,
				env.PublicIP,
				env.TerraformDir,
				"idle cloud stop failed: "+err.Error(),
				env.CloudProvisionedAt,
			)
			continue
		}

		_, _ = s.environmentRepo.UpdateStatus(ctx, env.ID, env.UserEmail, statusStopped)

		_, err = s.environmentRepo.UpdateProvisioning(
			ctx,
			env.ID,
			env.UserEmail,
			cloudStopped,
			env.CloudRegion,
			env.CloudInstanceType,
			env.CloudKeyName,
			env.InstanceID,
			env.PublicIP,
			env.TerraformDir,
			cloudIdleStopMessage,
			env.CloudProvisionedAt,
		)
		if err != nil {
			s.log.Error("cloud lifecycle: failed to update cloud_stopped status",
				"environment_id", env.ID,
				"error", err,
			)
		}
	}
}

func (s *CloudLifecycleService) terminateIdleCloudInstances(ctx context.Context) {
	if s.policy.CloudIdleTerminateMinutes <= 0 {
		return
	}

	cutoff := time.Now().Add(-time.Duration(s.policy.CloudIdleTerminateMinutes) * time.Minute)
	envs, err := s.environmentRepo.ListIdleStoppedCloudSince(ctx, cutoff)
	if err != nil {
		s.log.Error("cloud lifecycle: failed to list idle stopped cloud environments", "error", err)
		return
	}

	for _, env := range envs {
		if blocked, err := s.hasBlockingOperation(ctx, env.ID, env.UserEmail); err != nil {
			s.log.Error("cloud lifecycle: failed to check operations", "environment_id", env.ID, "error", err)
			continue
		} else if blocked {
			continue
		}

		s.log.Info("cloud lifecycle: terminating idle ec2 instance",
			"environment_id", env.ID,
			"instance_id", env.InstanceID,
			"last_activity_at", env.LastActivityAt,
		)

		if err := s.terminateCloudResources(ctx, &env); err != nil {
			s.log.Error("cloud lifecycle: failed to terminate ec2 instance",
				"environment_id", env.ID,
				"error", err,
			)
		}
	}
}

func (s *CloudLifecycleService) stopWorkspaceContainer(ctx context.Context, env *models.Environment) error {
	if isPlaceholderContainerID(env.ContainerID) {
		_, err := s.environmentRepo.UpdateStatus(ctx, env.ID, env.UserEmail, statusStopped)
		return err
	}

	runtime, err := s.resolver.ForEnvironment(env)
	if err != nil {
		return err
	}

	stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err = runtime.StopWorkspace(stopCtx, workspaceContainerRef(env))
	if err != nil && !isWorkspaceStopIgnorable(err) {
		return err
	}

	_, err = s.environmentRepo.UpdateStatus(ctx, env.ID, env.UserEmail, statusStopped)
	return err
}

func (s *CloudLifecycleService) terminateCloudResources(ctx context.Context, env *models.Environment) error {
	if env.RuntimeTarget == runtimeTargetRemote && env.ContainerID != "" && !isPlaceholderContainerID(env.ContainerID) {
		if remoteRuntime, remoteErr := s.resolver.ForEnvironment(env); remoteErr == nil {
			deleteCtx, deleteCancel := context.WithTimeout(ctx, 30*time.Second)
			_ = remoteRuntime.DeleteWorkspace(deleteCtx, workspaceContainerRef(env))
			deleteCancel()
		}
	}

	_, _ = s.environmentRepo.UpdateProvisioning(
		ctx,
		env.ID,
		env.UserEmail,
		cloudDeprovisioning,
		env.CloudRegion,
		env.CloudInstanceType,
		env.CloudKeyName,
		env.InstanceID,
		env.PublicIP,
		env.TerraformDir,
		cloudIdleTerminateMessage,
		env.CloudProvisionedAt,
	)

	destroyCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	err := s.terraformRunner.DestroyEC2(destroyCtx, env.ID, env.TerraformDir)
	cancel()
	if err != nil {
		_, _ = s.environmentRepo.UpdateProvisioning(
			ctx,
			env.ID,
			env.UserEmail,
			cloudProvisionFailed,
			env.CloudRegion,
			env.CloudInstanceType,
			env.CloudKeyName,
			env.InstanceID,
			env.PublicIP,
			env.TerraformDir,
			"idle cloud terminate failed: "+err.Error(),
			env.CloudProvisionedAt,
		)
		return err
	}

	if env.CreationMode == creationModeCloud {
		placeholderID := newPlaceholderContainerID()
		_, updateErr := s.environmentRepo.UpdateRuntime(ctx, env.ID, env.UserEmail, runtimeTargetLocal, placeholderID, statusStopped)
		if updateErr != nil {
			return updateErr
		}
		_, err = s.environmentRepo.UpdateProvisioning(ctx, env.ID, env.UserEmail, cloudNotProvisioned, "", "", "", "", "", "", cloudIdleTerminateMessage, nil)
		return err
	}

	localContainerID, err := s.bootstrapRevertToLocal(ctx, env)
	if err != nil {
		return err
	}
	_, err = s.environmentRepo.UpdateRuntime(ctx, env.ID, env.UserEmail, runtimeTargetLocal, localContainerID, statusRunning)
	if err != nil {
		return err
	}
	_, err = s.environmentRepo.UpdateProvisioning(ctx, env.ID, env.UserEmail, cloudNotProvisioned, "", "", "", "", "", "", cloudIdleTerminateMessage, nil)
	return err
}

func (s *CloudLifecycleService) bootstrapRevertToLocal(ctx context.Context, env *models.Environment) (string, error) {
	bootstrap := NewRemoteBootstrapService(s.resolver)
	return bootstrap.RevertToLocal(ctx, env)
}

func (s *CloudLifecycleService) hasBlockingOperation(ctx context.Context, environmentID, userEmail string) (bool, error) {
	return s.operationRepo.ExistsInProgressForEnvironment(ctx, environmentID, userEmail)
}

func (s *CloudLifecycleService) StartStoppedCloudInstance(ctx context.Context, env *models.Environment) (*models.Environment, error) {
	if env == nil {
		return nil, errors.New("environment is nil")
	}
	if env.CloudStatus != cloudStopped || env.InstanceID == "" {
		return nil, &ProvisionValidationError{Code: "cloud_not_stopped", Message: "environment does not have a stopped EC2 instance to start"}
	}

	startCtx, cancel := context.WithTimeout(ctx, defaultEC2StateWaitTimeout)
	defer cancel()

	if err := s.ec2.StartInstance(startCtx, env.CloudRegion, env.InstanceID); err != nil {
		return nil, err
	}

	state, err := s.ec2.WaitForInstanceState(startCtx, env.CloudRegion, env.InstanceID, "running", defaultEC2StateWaitTimeout)
	if err != nil {
		return nil, err
	}

	publicIP := state.PublicIP
	if publicIP == "" {
		publicIP = env.PublicIP
	}

	updatedEnv, err := s.environmentRepo.UpdateProvisioning(
		ctx,
		env.ID,
		env.UserEmail,
		cloudProvisioned,
		env.CloudRegion,
		env.CloudInstanceType,
		env.CloudKeyName,
		env.InstanceID,
		publicIP,
		env.TerraformDir,
		"",
		env.CloudProvisionedAt,
	)
	if err != nil {
		return nil, err
	}

	if updatedEnv.RuntimeTarget == runtimeTargetRemote {
		factory := s.resolver.SSHFactory()
		waitCtx, waitCancel := context.WithTimeout(ctx, s.resolver.BootstrapTimeout())
		defer waitCancel()
		if err := factory.WaitForSSH(waitCtx, publicIP); err != nil {
			return nil, fmt.Errorf("wait for ssh after ec2 start: %w", err)
		}
		if err := factory.WaitForDocker(waitCtx, publicIP); err != nil {
			return nil, fmt.Errorf("wait for docker after ec2 start: %w", err)
		}
	}

	return updatedEnv, nil
}
