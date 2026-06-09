package services

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/afahey03/docklab/internal/models"
	"github.com/afahey03/docklab/internal/repositories"
)

const (
	DefaultEnvironmentImage = "alpine:3.20"
	statusRunning           = "running"
	statusStopped           = "stopped"
	cloudNotProvisioned     = "not_provisioned"
	cloudProvisioning       = "provisioning"
	cloudProvisioned        = "provisioned"
	cloudProvisionFailed    = "provision_failed"
	cloudDeprovisioning     = "deprovisioning"
	opTypeProvision         = "provision"
	opTypeDestroyCloud      = "destroy_cloud"
	opTypeDeleteEnvironment = "delete_environment"
	opTypeRetryBootstrap    = "retry_bootstrap"
	opStatusQueued          = "queued"
	opStatusRunning         = "running"
	opStatusSucceeded       = "succeeded"
	opStatusFailed          = "failed"
	createTargetLocal       = "local"
	createTargetCloud       = "cloud"
	creationModeLocal       = "local"
	creationModeCloud       = "cloud"
)

const staleEnvironmentOperationThreshold = 10 * time.Minute

var ErrDockerUnavailable = errors.New("docker CLI is not installed or unavailable")
var ErrProvisionInProgress = errors.New("provisioning is already in progress for this environment")
var ErrOperationNotFound = errors.New("operation not found")
var ErrOperationInProgress = errors.New("another long-running operation is already in progress for this environment")
var ErrCloudAlreadyProvisioned = errors.New("environment already has cloud resources; terminate EC2 before reprovisioning")

type CreateEnvironmentInput struct {
	Name      string
	Image     string
	Target    string
	Provision ProvisionRequest
}

type CreateEnvironmentResult struct {
	Environment *models.Environment
	Operation   *models.Operation
}

type ProvisionValidationError struct {
	Code    string
	Message string
}

func (e *ProvisionValidationError) Error() string {
	if e == nil {
		return "invalid provisioning request"
	}
	if e.Message != "" {
		return e.Message
	}
	return "invalid provisioning request"
}

var (
	awsRegionPattern    = regexp.MustCompile(`^[a-z]{2}(-gov)?-[a-z]+-\d$`)
	instanceTypePattern = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z0-9]+$`)
	amiPattern          = regexp.MustCompile(`^ami-[0-9a-fA-F]{8,17}$`)
	keyNamePattern      = regexp.MustCompile(`^[A-Za-z0-9._-]{1,255}$`)
)

type ContainerRuntime interface {
	CreateWorkspace(ctx context.Context, name, image string, labels map[string]string) (string, error)
	StartWorkspace(ctx context.Context, containerID string) error
	StopWorkspace(ctx context.Context, containerID string) error
	DeleteWorkspace(ctx context.Context, containerID string) error
}

type DockerCLIRuntime struct{}

func NewDockerCLIRuntime() *DockerCLIRuntime {
	return &DockerCLIRuntime{}
}

func (d *DockerCLIRuntime) runDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrDockerUnavailable
		}
		trimmed := strings.TrimSpace(stderr.String())
		if trimmed == "" {
			trimmed = strings.TrimSpace(stdout.String())
		}
		if trimmed == "" {
			return "", fmt.Errorf("docker command failed: %w", err)
		}
		return "", fmt.Errorf("docker command failed: %s", trimmed)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (d *DockerCLIRuntime) CreateWorkspace(ctx context.Context, name, image string, labels map[string]string) (string, error) {
	args := []string{"run", "-d", "--name", name}
	for key, value := range labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, value))
	}
	args = append(args, image, "sleep", "infinity")

	return d.runDocker(ctx, args...)
}

func (d *DockerCLIRuntime) StartWorkspace(ctx context.Context, containerID string) error {
	_, err := d.runDocker(ctx, "start", containerID)
	return err
}

func (d *DockerCLIRuntime) StopWorkspace(ctx context.Context, containerID string) error {
	_, err := d.runDocker(ctx, "stop", containerID)
	return err
}

func (d *DockerCLIRuntime) DeleteWorkspace(ctx context.Context, containerID string) error {
	_, err := d.runDocker(ctx, "rm", "-f", containerID)
	return err
}

type EnvironmentService struct {
	repo            repositories.EnvironmentRepository
	operationRepo   repositories.OperationRepository
	resolver        *RuntimeResolver
	bootstrap       *RemoteBootstrapService
	terraformRunner TerraformRunner
}

func NewEnvironmentService(repo repositories.EnvironmentRepository, operationRepo repositories.OperationRepository, resolver *RuntimeResolver) *EnvironmentService {
	return &EnvironmentService{
		repo:            repo,
		operationRepo:   operationRepo,
		resolver:        resolver,
		bootstrap:       NewRemoteBootstrapService(resolver),
		terraformRunner: NewTerraformCLIRunner(),
	}
}

func (s *EnvironmentService) GetRemoteHealth(ctx context.Context, id, userEmail string) (RemoteHealthStatus, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return RemoteHealthStatus{}, err
	}
	return s.bootstrap.CheckHealth(ctx, env), nil
}

func (s *EnvironmentService) runtimeFor(env *models.Environment) (ContainerRuntime, error) {
	return s.resolver.ForEnvironment(env)
}

func hasExistingCloudResources(env *models.Environment) bool {
	if env == nil {
		return false
	}
	if hasProvisionedCloudResources(env) {
		return true
	}
	return env.CloudStatus == cloudProvisioning
}

func hasProvisionedCloudResources(env *models.Environment) bool {
	if env == nil {
		return false
	}
	if env.InstanceID != "" || env.TerraformDir != "" {
		return true
	}
	switch env.CloudStatus {
	case cloudProvisioned, cloudDeprovisioning:
		return true
	default:
		return false
	}
}

func (s *EnvironmentService) QueueRetryRemoteBootstrap(ctx context.Context, id, userEmail string) (*models.Operation, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}
	if env.PublicIP == "" || env.InstanceID == "" {
		return nil, &ProvisionValidationError{Code: "remote_bootstrap_unavailable", Message: "environment has no provisioned EC2 instance to bootstrap"}
	}
	needsBootstrap := env.RuntimeTarget != runtimeTargetRemote
	needsReconcile := env.RuntimeTarget == runtimeTargetRemote && (env.CloudStatus != cloudProvisioned || env.CloudError != "")
	if !needsBootstrap && !needsReconcile {
		return nil, &ProvisionValidationError{Code: "remote_bootstrap_unavailable", Message: "remote workspace is already configured"}
	}

	_, _ = s.operationRepo.FailInProgressForEnvironment(ctx, env.ID, userEmail, "superseded by remote bootstrap retry")
	s.clearStaleOperations(ctx, env.ID, userEmail)

	hasInProgress, err := s.hasInProgressOperation(ctx, env.ID, userEmail)
	if err != nil {
		return nil, err
	}
	if hasInProgress {
		return nil, ErrOperationInProgress
	}

	return s.queueOperation(ctx, userEmail, env.ID, opTypeRetryBootstrap, func() error {
		_, bootstrapErr := s.completeRemoteBootstrap(context.Background(), env.ID, userEmail)
		return bootstrapErr
	})
}

func (s *EnvironmentService) completeRemoteBootstrap(ctx context.Context, id, userEmail string) (*models.Environment, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}
	if env.PublicIP == "" || env.InstanceID == "" {
		return nil, fmt.Errorf("environment has no provisioned EC2 instance")
	}

	bootstrapCtx, bootstrapCancel := context.WithTimeout(ctx, s.resolver.BootstrapTimeout())
	defer bootstrapCancel()

	onProgress := func(message string) {
		s.reportBootstrapProgress(bootstrapCtx, env, message)
	}

	remoteContainerID, bootstrapErr := s.bootstrap.BootstrapAfterProvision(bootstrapCtx, env, onProgress)
	if bootstrapErr != nil {
		if env.RuntimeTarget != runtimeTargetRemote {
			failedEnv, updateErr := s.repo.UpdateProvisioning(
				ctx,
				env.ID,
				userEmail,
				cloudProvisionFailed,
				env.CloudRegion,
				env.CloudInstanceType,
				env.CloudKeyName,
				env.InstanceID,
				env.PublicIP,
				env.TerraformDir,
				fmt.Sprintf("remote bootstrap failed: %v", bootstrapErr),
				env.CloudProvisionedAt,
			)
			if updateErr == nil {
				return failedEnv, bootstrapErr
			}
		}
		return nil, bootstrapErr
	}

	var updatedEnv *models.Environment
	if env.RuntimeTarget != runtimeTargetRemote {
		localContainerID := env.ContainerID
		updatedEnv, err = s.repo.UpdateRuntime(ctx, env.ID, userEmail, runtimeTargetRemote, remoteContainerID, statusRunning)
		if err != nil {
			return nil, err
		}
		if localContainerID != "" && !isPlaceholderContainerID(localContainerID) {
			_ = s.resolver.LocalRuntime().DeleteWorkspace(ctx, localContainerID)
		}
	} else if remoteContainerID != "" && remoteContainerID != env.ContainerID {
		updatedEnv, err = s.repo.UpdateRuntime(ctx, env.ID, userEmail, runtimeTargetRemote, remoteContainerID, statusRunning)
		if err != nil {
			return nil, err
		}
	} else {
		updatedEnv = env
	}

	cloudProvisionedAt := env.CloudProvisionedAt
	if cloudProvisionedAt == nil {
		now := time.Now().UTC()
		cloudProvisionedAt = &now
	}
	provisionedEnv, err := s.repo.UpdateProvisioning(
		ctx,
		env.ID,
		userEmail,
		cloudProvisioned,
		env.CloudRegion,
		env.CloudInstanceType,
		env.CloudKeyName,
		env.InstanceID,
		env.PublicIP,
		env.TerraformDir,
		"",
		cloudProvisionedAt,
	)
	if err != nil {
		return updatedEnv, err
	}

	return provisionedEnv, nil
}

func (s *EnvironmentService) QueueProvisionEnvironment(ctx context.Context, id, userEmail string, req ProvisionRequest) (*models.Operation, error) {
	sanitizedReq, err := validateProvisionRequest(req)
	if err != nil {
		return nil, err
	}

	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}
	if env.CreationMode == creationModeCloud {
		return nil, &ProvisionValidationError{Code: "provision_not_allowed", Message: "cloud environments are provisioned at create time; retry remote setup instead"}
	}
	if env.CloudStatus == cloudProvisioning {
		return nil, ErrProvisionInProgress
	}
	if hasExistingCloudResources(env) {
		return nil, ErrCloudAlreadyProvisioned
	}
	s.clearStaleOperations(ctx, env.ID, userEmail)
	hasInProgress, err := s.hasInProgressOperation(ctx, env.ID, userEmail)
	if err != nil {
		return nil, err
	}
	if hasInProgress {
		return nil, ErrOperationInProgress
	}

	return s.queueOperation(ctx, userEmail, env.ID, opTypeProvision, func() error {
		_, provisionErr := s.ProvisionEnvironment(context.Background(), env.ID, userEmail, sanitizedReq)
		return provisionErr
	})
}

func (s *EnvironmentService) QueueDestroyCloudEnvironment(ctx context.Context, id, userEmail string) (*models.Operation, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}
	if env.CreationMode == creationModeCloud {
		return nil, &ProvisionValidationError{
			Code:    "terminate_not_allowed",
			Message: "cloud workspaces cannot be terminated separately; delete the environment instead",
		}
	}
	s.clearBlockingOperations(ctx, env)
	hasInProgress, err := s.hasInProgressOperation(ctx, env.ID, userEmail)
	if err != nil {
		return nil, err
	}
	if hasInProgress {
		return nil, ErrOperationInProgress
	}
	if shouldDestroyCloudResources(env) {
		_, _ = s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudDeprovisioning, env.CloudRegion, env.CloudInstanceType, env.CloudKeyName, env.InstanceID, env.PublicIP, env.TerraformDir, "", env.CloudProvisionedAt)
	}

	return s.queueOperation(ctx, userEmail, env.ID, opTypeDestroyCloud, func() error {
		_, destroyErr := s.DestroyCloudEnvironment(context.Background(), env.ID, userEmail)
		return destroyErr
	})
}

func (s *EnvironmentService) QueueDeleteEnvironment(ctx context.Context, id, userEmail string) (*models.Operation, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}
	s.clearBlockingOperations(ctx, env)
	hasInProgress, err := s.hasInProgressOperation(ctx, env.ID, userEmail)
	if err != nil {
		return nil, err
	}
	if hasInProgress {
		return nil, ErrOperationInProgress
	}
	if shouldDestroyCloudResources(env) {
		_, _ = s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudDeprovisioning, env.CloudRegion, env.CloudInstanceType, env.CloudKeyName, env.InstanceID, env.PublicIP, env.TerraformDir, "", env.CloudProvisionedAt)
	}

	return s.queueOperation(ctx, userEmail, env.ID, opTypeDeleteEnvironment, func() error {
		return s.DeleteEnvironment(context.Background(), env.ID, userEmail)
	})
}

func (s *EnvironmentService) GetOperation(ctx context.Context, operationID, userEmail string) (*models.Operation, error) {
	op, err := s.operationRepo.GetByIDForUser(ctx, operationID, userEmail)
	if errors.Is(err, repositories.ErrOperationNotFound) {
		return nil, ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}

	return op, nil
}

func (s *EnvironmentService) CreateEnvironment(ctx context.Context, userEmail string, input CreateEnvironmentInput) (*CreateEnvironmentResult, error) {
	target, err := normalizeCreateTarget(input.Target)
	if err != nil {
		return nil, err
	}

	image := strings.TrimSpace(input.Image)
	if image == "" {
		image = DefaultEnvironmentImage
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = generateEnvironmentName(userEmail)
	}

	switch target {
	case createTargetCloud:
		if strings.TrimSpace(input.Provision.Region) == "" {
			return nil, &ProvisionValidationError{Code: "provision_required", Message: "provision settings are required when target is cloud"}
		}
		return s.createCloudEnvironment(ctx, userEmail, name, image, input.Provision)
	default:
		env, err := s.createLocalEnvironment(ctx, userEmail, name, image)
		if err != nil {
			return nil, err
		}
		return &CreateEnvironmentResult{Environment: env}, nil
	}
}

func (s *EnvironmentService) createLocalEnvironment(ctx context.Context, userEmail, name, image string) (*models.Environment, error) {
	containerID, err := s.resolver.LocalRuntime().CreateWorkspace(ctx, name, image, map[string]string{
		"docklab.user_email": userEmail,
		"docklab.name":       name,
	})
	if err != nil {
		return nil, err
	}

	return s.repo.Create(ctx, userEmail, name, image, statusRunning, containerID, creationModeLocal)
}

func (s *EnvironmentService) createCloudEnvironment(ctx context.Context, userEmail, name, image string, provision ProvisionRequest) (*CreateEnvironmentResult, error) {
	sanitizedReq, err := validateProvisionRequest(provision)
	if err != nil {
		return nil, err
	}

	env, err := s.repo.Create(ctx, userEmail, name, image, statusStopped, newPlaceholderContainerID(), creationModeCloud)
	if err != nil {
		return nil, err
	}

	updatedEnv, err := s.repo.UpdateProvisioning(
		ctx,
		env.ID,
		userEmail,
		cloudProvisioning,
		sanitizedReq.Region,
		sanitizedReq.InstanceType,
		sanitizedReq.KeyName,
		"",
		"",
		"",
		"provisioning EC2 instance",
		nil,
	)
	if err != nil {
		_ = s.repo.Delete(ctx, env.ID, userEmail)
		return nil, err
	}

	op, err := s.queueOperation(ctx, userEmail, env.ID, opTypeProvision, func() error {
		_, provisionErr := s.ProvisionEnvironment(context.Background(), env.ID, userEmail, sanitizedReq)
		return provisionErr
	})
	if err != nil {
		_ = s.repo.Delete(ctx, env.ID, userEmail)
		return nil, err
	}

	return &CreateEnvironmentResult{
		Environment: updatedEnv,
		Operation:   op,
	}, nil
}

func (s *EnvironmentService) ListEnvironments(ctx context.Context, userEmail string) ([]models.Environment, error) {
	return s.repo.ListByUserEmail(ctx, userEmail)
}

func (s *EnvironmentService) StartEnvironment(ctx context.Context, id, userEmail string) (*models.Environment, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}
	if env.Status == statusRunning {
		return env, nil
	}

	if err := s.startWorkspace(ctx, env); err != nil {
		return nil, err
	}

	return s.repo.UpdateStatus(ctx, id, userEmail, statusRunning)
}

func (s *EnvironmentService) startWorkspace(ctx context.Context, env *models.Environment) error {
	if isPlaceholderContainerID(env.ContainerID) {
		return &ProvisionValidationError{Code: "workspace_unavailable", Message: "workspace is not ready yet"}
	}
	runtime, err := s.runtimeFor(env)
	if err != nil {
		return err
	}
	return runtime.StartWorkspace(ctx, env.ContainerID)
}

func (s *EnvironmentService) StopEnvironment(ctx context.Context, id, userEmail string) (*models.Environment, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}
	if env.Status == statusStopped {
		return env, nil
	}

	if err := s.stopWorkspace(ctx, env); err != nil {
		return nil, err
	}

	return s.repo.UpdateStatus(ctx, id, userEmail, statusStopped)
}

func (s *EnvironmentService) stopWorkspace(ctx context.Context, env *models.Environment) error {
	if isPlaceholderContainerID(env.ContainerID) {
		return nil
	}
	runtime, err := s.runtimeFor(env)
	if err != nil {
		return err
	}
	return runtime.StopWorkspace(ctx, env.ContainerID)
}

func (s *EnvironmentService) DeleteEnvironment(ctx context.Context, id, userEmail string) error {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return err
	}

	if err := s.destroyCloudResources(ctx, env, userEmail); err != nil {
		return err
	}

	if err := s.deleteWorkspace(ctx, env); err != nil {
		return err
	}

	return s.repo.Delete(ctx, id, userEmail)
}

func (s *EnvironmentService) deleteWorkspace(ctx context.Context, env *models.Environment) error {
	if isPlaceholderContainerID(env.ContainerID) {
		return nil
	}
	runtime, err := s.runtimeFor(env)
	if err != nil {
		return err
	}
	return runtime.DeleteWorkspace(ctx, env.ContainerID)
}

func (s *EnvironmentService) DestroyCloudEnvironment(ctx context.Context, id, userEmail string) (*models.Environment, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}

	if err := s.destroyCloudResources(ctx, env, userEmail); err != nil {
		return nil, err
	}

	if env.RuntimeTarget == runtimeTargetRemote && env.ContainerID != "" && !isPlaceholderContainerID(env.ContainerID) {
		if remoteRuntime, remoteErr := s.resolver.ForEnvironment(env); remoteErr == nil {
			_ = remoteRuntime.DeleteWorkspace(ctx, env.ContainerID)
		}
	}

	if env.CreationMode == creationModeCloud {
		placeholderID := newPlaceholderContainerID()
		_, updateErr := s.repo.UpdateRuntime(ctx, env.ID, userEmail, runtimeTargetLocal, placeholderID, statusStopped)
		if updateErr != nil {
			return nil, updateErr
		}
		return s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudNotProvisioned, "", "", "", "", "", "", "", nil)
	}

	localContainerID, err := s.bootstrap.RevertToLocal(ctx, env)
	if err != nil {
		return nil, err
	}
	_, err = s.repo.UpdateRuntime(ctx, env.ID, userEmail, runtimeTargetLocal, localContainerID, statusRunning)
	if err != nil {
		return nil, err
	}

	return s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudNotProvisioned, "", "", "", "", "", "", "", nil)
}

func shouldDestroyCloudResources(env *models.Environment) bool {
	if env == nil {
		return false
	}

	return env.CloudStatus == cloudProvisioned || env.InstanceID != "" || env.TerraformDir != ""
}

func (s *EnvironmentService) destroyCloudResources(ctx context.Context, env *models.Environment, userEmail string) error {
	if !shouldDestroyCloudResources(env) {
		return nil
	}

	destroyCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	err := s.terraformRunner.DestroyEC2(destroyCtx, env.ID, env.TerraformDir)
	cancel()
	if err != nil {
		_, _ = s.repo.UpdateProvisioning(
			ctx,
			env.ID,
			userEmail,
			cloudProvisionFailed,
			env.CloudRegion,
			env.CloudInstanceType,
			env.CloudKeyName,
			env.InstanceID,
			env.PublicIP,
			env.TerraformDir,
			fmt.Sprintf("destroy failed: %v", err),
			env.CloudProvisionedAt,
		)
		return err
	}

	return nil
}

func (s *EnvironmentService) ProvisionEnvironment(ctx context.Context, id, userEmail string, req ProvisionRequest) (*models.Environment, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}
	if hasProvisionedCloudResources(env) {
		return nil, ErrCloudAlreadyProvisioned
	}

	if env.CloudStatus != cloudProvisioning {
		_, err = s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudProvisioning, req.Region, req.InstanceType, req.KeyName, "", "", env.TerraformDir, "provisioning EC2 instance", env.CloudProvisionedAt)
		if err != nil {
			return nil, err
		}
	}

	provisionCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	req.WorkspaceImage = env.Image
	result, err := s.terraformRunner.ProvisionEC2(provisionCtx, env.ID, req, env.TerraformDir)
	if err != nil {
		failedTerraformDir := env.TerraformDir
		var workspaceErr *TerraformWorkspaceError
		if errors.As(err, &workspaceErr) && workspaceErr.TerraformDir != "" {
			failedTerraformDir = workspaceErr.TerraformDir
		}

		failedEnv, updateErr := s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudProvisionFailed, req.Region, req.InstanceType, req.KeyName, "", "", failedTerraformDir, err.Error(), env.CloudProvisionedAt)
		if updateErr == nil {
			return failedEnv, err
		}
		return nil, err
	}

	_, err = s.repo.UpdateProvisioning(
		ctx,
		env.ID,
		userEmail,
		cloudProvisioning,
		req.Region,
		req.InstanceType,
		req.KeyName,
		result.InstanceID,
		result.PublicIP,
		result.TerraformDir,
		"bootstrapping remote workspace",
		env.CloudProvisionedAt,
	)
	if err != nil {
		return nil, err
	}

	return s.completeRemoteBootstrap(ctx, id, userEmail)
}

func (s *EnvironmentService) GetEnvironment(ctx context.Context, id, userEmail string) (*models.Environment, error) {
	return s.repo.GetByIDForUser(ctx, id, userEmail)
}

func (s *EnvironmentService) queueOperation(ctx context.Context, userEmail, environmentID, operationType string, job func() error) (*models.Operation, error) {
	op, err := s.operationRepo.Create(ctx, userEmail, environmentID, operationType, opStatusQueued, "")
	if err != nil {
		return nil, err
	}

	go func(operationID string) {
		_, _ = s.operationRepo.UpdateStatus(context.Background(), operationID, userEmail, opStatusRunning, "")
		err := job()
		if err != nil {
			_, _ = s.operationRepo.UpdateStatus(context.Background(), operationID, userEmail, opStatusFailed, err.Error())
			return
		}
		_, _ = s.operationRepo.UpdateStatus(context.Background(), operationID, userEmail, opStatusSucceeded, "")
	}(op.ID)

	return op, nil
}

func (s *EnvironmentService) hasInProgressOperation(ctx context.Context, environmentID, userEmail string) (bool, error) {
	return s.operationRepo.ExistsInProgressForEnvironment(ctx, environmentID, userEmail)
}

func (s *EnvironmentService) clearStaleOperations(ctx context.Context, environmentID, userEmail string) {
	_, _ = s.operationRepo.FailStaleInProgressForEnvironment(ctx, environmentID, userEmail, staleEnvironmentOperationThreshold)
}

func (s *EnvironmentService) clearBlockingOperations(ctx context.Context, env *models.Environment) {
	if env == nil {
		return
	}
	s.clearStaleOperations(ctx, env.ID, env.UserEmail)
	if env.InstanceID != "" && env.RuntimeTarget != runtimeTargetRemote {
		_, _ = s.operationRepo.FailInProgressForEnvironment(ctx, env.ID, env.UserEmail, "cleared incomplete remote bootstrap to allow environment management")
	}
}

func (s *EnvironmentService) reportBootstrapProgress(ctx context.Context, env *models.Environment, message string) {
	if env == nil {
		return
	}
	_, _ = s.repo.UpdateProvisioning(
		ctx,
		env.ID,
		env.UserEmail,
		cloudProvisioning,
		env.CloudRegion,
		env.CloudInstanceType,
		env.CloudKeyName,
		env.InstanceID,
		env.PublicIP,
		env.TerraformDir,
		message,
		env.CloudProvisionedAt,
	)
}

func generateEnvironmentName(userEmail string) string {
	base := strings.Split(strings.ToLower(userEmail), "@")[0]
	base = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, base)
	if base == "" {
		base = "workspace"
	}

	randSource := rand.New(rand.NewSource(time.Now().UnixNano()))
	suffix := randSource.Intn(9000) + 1000
	return fmt.Sprintf("docklab-%s-%d", base, suffix)
}

func validateProvisionRequest(req ProvisionRequest) (ProvisionRequest, error) {
	req.Region = strings.TrimSpace(req.Region)
	req.InstanceType = strings.TrimSpace(req.InstanceType)
	req.AMI = strings.TrimSpace(req.AMI)
	req.KeyName = normalizeEC2KeyName(strings.TrimSpace(req.KeyName))

	if req.Region == "" {
		return ProvisionRequest{}, &ProvisionValidationError{Code: "invalid_region", Message: "region is required"}
	}
	if !awsRegionPattern.MatchString(req.Region) {
		return ProvisionRequest{}, &ProvisionValidationError{Code: "invalid_region", Message: "region must match AWS region format (for example, us-east-1)"}
	}

	if req.InstanceType == "" {
		return ProvisionRequest{}, &ProvisionValidationError{Code: "invalid_instance_type", Message: "instance_type is required"}
	}
	if !instanceTypePattern.MatchString(req.InstanceType) {
		return ProvisionRequest{}, &ProvisionValidationError{Code: "invalid_instance_type", Message: "instance_type must match EC2 instance type format (for example, t3.micro)"}
	}

	if req.AMI == "" {
		return ProvisionRequest{}, &ProvisionValidationError{Code: "invalid_ami", Message: "ami is required"}
	}
	if !amiPattern.MatchString(req.AMI) {
		return ProvisionRequest{}, &ProvisionValidationError{Code: "invalid_ami", Message: "ami must match AMI ID format (for example, ami-0c2b8ca1dad447f8a)"}
	}

	if req.KeyName == "" {
		return ProvisionRequest{}, &ProvisionValidationError{Code: "invalid_key_name", Message: "key_name is required for remote workspace access over SSH"}
	}
	if !keyNamePattern.MatchString(req.KeyName) {
		return ProvisionRequest{}, &ProvisionValidationError{Code: "invalid_key_name", Message: "key_name may only include letters, numbers, dot, underscore, and hyphen"}
	}

	return req, nil
}

// normalizeEC2KeyName strips a trailing .pem extension when users confuse the local
// private key filename with the EC2 key pair name registered in AWS.
func normalizeEC2KeyName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) > 4 && strings.EqualFold(name[len(name)-4:], ".pem") {
		return name[:len(name)-4]
	}
	return name
}

func normalizeCreateTarget(target string) (string, error) {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return createTargetLocal, nil
	}
	switch target {
	case createTargetLocal, createTargetCloud:
		return target, nil
	default:
		return "", &ProvisionValidationError{Code: "invalid_target", Message: "target must be local or cloud"}
	}
}

func isPlaceholderContainerID(containerID string) bool {
	return strings.HasPrefix(containerID, "pending-")
}

func newPlaceholderContainerID() string {
	buf := make([]byte, 16)
	if _, err := cryptorand.Read(buf); err != nil {
		return fmt.Sprintf("pending-%d", time.Now().UnixNano())
	}
	return "pending-" + hex.EncodeToString(buf)
}
