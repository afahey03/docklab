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
	cloudStopped            = "cloud_stopped"
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
var ErrEnvironmentQuotaExceeded = errors.New("environment quota reached; delete an environment before creating another")
var ErrOperationQuotaExceeded = errors.New("too many concurrent operations in progress; wait for one to finish")

type CreateEnvironmentInput struct {
	Name       string
	Image      string
	Target     string
	RepoURL    string
	TemplateID string
	Provision  ProvisionRequest
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
	// CommitWorkspace captures the workspace filesystem as an image (snapshots).
	CommitWorkspace(ctx context.Context, containerRef, imageTag string) error
	// DeleteWorkspaceVolume removes the named workspace volume created for a workspace.
	DeleteWorkspaceVolume(ctx context.Context, workspaceName string) error
}

// WorkspaceVolumeName is the named volume mounted at /workspace inside every Docker
// workspace container. Snapshots archive its contents into the committed image.
func WorkspaceVolumeName(workspaceName string) string {
	return "docklab-ws-" + workspaceName
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
	args := []string{"run", "-d", "--name", name, "-v", WorkspaceVolumeName(name) + ":/workspace", "-w", "/workspace"}
	for key, value := range labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, value))
	}
	args = append(args, image, "sleep", "infinity")

	return d.runDocker(ctx, args...)
}

func (d *DockerCLIRuntime) CommitWorkspace(ctx context.Context, containerRef, imageTag string) error {
	_, err := d.runDocker(ctx, "commit", containerRef, imageTag)
	return err
}

func (d *DockerCLIRuntime) DeleteWorkspaceVolume(ctx context.Context, workspaceName string) error {
	_, err := d.runDocker(ctx, "volume", "rm", "-f", WorkspaceVolumeName(workspaceName))
	return err
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
	cloudLifecycle  *CloudLifecycleService
	usage           *UsageService
	metrics         *Metrics
	alerts          *AlertService

	maxEnvironmentsPerUser  int
	maxConcurrentOpsPerUser int
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

func (s *EnvironmentService) SetCloudLifecycle(cloudLifecycle *CloudLifecycleService) {
	s.cloudLifecycle = cloudLifecycle
}

// SetQuotas configures per-user limits; zero or negative values disable a limit.
func (s *EnvironmentService) SetQuotas(maxEnvironments, maxConcurrentOps int) {
	s.maxEnvironmentsPerUser = maxEnvironments
	s.maxConcurrentOpsPerUser = maxConcurrentOps
}

func (s *EnvironmentService) SetUsageService(usage *UsageService) {
	s.usage = usage
}

func (s *EnvironmentService) SetObservability(metrics *Metrics, alerts *AlertService) {
	s.metrics = metrics
	s.alerts = alerts
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
	case cloudProvisioned, cloudStopped, cloudDeprovisioning:
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

	if provisionedEnv.RepoURL != "" {
		go s.cloneRepoIntoWorkspace(context.Background(), provisionedEnv)
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

	if err := s.checkEnvironmentQuota(ctx, userEmail); err != nil {
		return nil, err
	}

	image := strings.TrimSpace(input.Image)
	templateID := strings.TrimSpace(input.TemplateID)
	if templateID != "" {
		template := TemplateByID(templateID)
		if template == nil {
			return nil, &ProvisionValidationError{Code: "invalid_template", Message: "unknown environment template"}
		}
		image = template.Image
	}
	if image == "" {
		image = DefaultEnvironmentImage
	}

	repoURL, err := normalizeRepoURL(input.RepoURL)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = generateEnvironmentName(userEmail)
	}

	s.metrics.RecordEnvironmentCreated(target)

	switch target {
	case createTargetCloud:
		if strings.TrimSpace(input.Provision.Region) == "" {
			return nil, &ProvisionValidationError{Code: "provision_required", Message: "provision settings are required when target is cloud"}
		}
		return s.createCloudEnvironment(ctx, userEmail, name, image, repoURL, templateID, input.Provision)
	default:
		env, err := s.createLocalEnvironment(ctx, userEmail, name, image, repoURL, templateID)
		if err != nil {
			return nil, err
		}
		return &CreateEnvironmentResult{Environment: env}, nil
	}
}

func (s *EnvironmentService) checkEnvironmentQuota(ctx context.Context, userEmail string) error {
	if s.maxEnvironmentsPerUser <= 0 {
		return nil
	}
	count, err := s.repo.CountByUserEmail(ctx, userEmail)
	if err != nil {
		return err
	}
	if count >= s.maxEnvironmentsPerUser {
		return ErrEnvironmentQuotaExceeded
	}
	return nil
}

func (s *EnvironmentService) createLocalEnvironment(ctx context.Context, userEmail, name, image, repoURL, templateID string) (*models.Environment, error) {
	containerID, err := s.resolver.LocalRuntime().CreateWorkspace(ctx, name, image, map[string]string{
		"docklab.user_email": userEmail,
		"docklab.name":       name,
	})
	if err != nil {
		return nil, err
	}

	env, err := s.repo.Create(ctx, userEmail, name, image, statusRunning, containerID, creationModeLocal, repoURL, templateID)
	if err != nil {
		return nil, err
	}

	if repoURL != "" {
		// Clone in the background so creation stays fast; failures are logged via the
		// container itself (re-runs are idempotent because the clone script checks first).
		go s.cloneRepoIntoWorkspace(context.Background(), env)
	}

	return env, nil
}

func (s *EnvironmentService) createCloudEnvironment(ctx context.Context, userEmail, name, image, repoURL, templateID string, provision ProvisionRequest) (*CreateEnvironmentResult, error) {
	sanitizedReq, err := validateProvisionRequest(provision)
	if err != nil {
		return nil, err
	}

	env, err := s.repo.Create(ctx, userEmail, name, image, statusStopped, newPlaceholderContainerID(), creationModeCloud, repoURL, templateID)
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
	if env.Status == statusRunning && env.CloudStatus != cloudStopped {
		return env, nil
	}

	if env.CloudStatus == cloudStopped && env.InstanceID != "" {
		if s.cloudLifecycle == nil {
			return nil, &ProvisionValidationError{Code: "cloud_lifecycle_unavailable", Message: "cloud lifecycle is not configured"}
		}
		env, err = s.cloudLifecycle.StartStoppedCloudInstance(ctx, env)
		if err != nil {
			return nil, err
		}
		// The instance is billable again; resume usage tracking.
		s.usage.EnsureSessionOpen(ctx, env)
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
	return runtime.StartWorkspace(ctx, workspaceContainerRef(env))
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
	err = runtime.StopWorkspace(ctx, workspaceContainerRef(env))
	if err != nil && isWorkspaceStopIgnorable(err) {
		return nil
	}
	return err
}

func (s *EnvironmentService) DeleteEnvironment(ctx context.Context, id, userEmail string) error {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return err
	}

	_ = s.deleteWorkspaceBestEffort(ctx, env)

	if err := s.destroyCloudResources(ctx, env, userEmail); err != nil {
		return err
	}

	if env.RuntimeTarget != runtimeTargetRemote {
		if err := s.deleteWorkspace(ctx, env); err != nil {
			return err
		}
		// Best-effort cleanup of the named /workspace volume.
		if runtime, runtimeErr := s.runtimeFor(env); runtimeErr == nil {
			_ = runtime.DeleteWorkspaceVolume(ctx, workspaceContainerName(env))
		}
	}

	s.usage.CloseSession(ctx, env.ID)

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
	return runtime.DeleteWorkspace(ctx, workspaceContainerRef(env))
}

func (s *EnvironmentService) deleteWorkspaceBestEffort(ctx context.Context, env *models.Environment) error {
	if env == nil || isPlaceholderContainerID(env.ContainerID) {
		return nil
	}
	if env.RuntimeTarget != runtimeTargetRemote && env.InstanceID == "" {
		return nil
	}

	runtime, err := s.runtimeFor(env)
	if err != nil {
		return nil
	}

	deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err = runtime.DeleteWorkspace(deleteCtx, workspaceContainerRef(env))
	if err != nil && isWorkspaceDeleteIgnorable(err) {
		return nil
	}
	return err
}

func (s *EnvironmentService) DestroyCloudEnvironment(ctx context.Context, id, userEmail string) (*models.Environment, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}

	_ = s.deleteWorkspaceBestEffort(ctx, env)

	if err := s.destroyCloudResources(ctx, env, userEmail); err != nil {
		return nil, err
	}

	s.usage.CloseSession(ctx, env.ID)

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

	return env.CloudStatus == cloudProvisioned || env.CloudStatus == cloudStopped || env.InstanceID != "" || env.TerraformDir != ""
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
		req.Region,
		req.InstanceType,
		req.KeyName,
		result.InstanceID,
		result.PublicIP,
		result.TerraformDir,
		"bootstrapping remote workspace",
		cloudProvisionedAt,
	)
	if err != nil {
		return nil, err
	}

	// EC2 starts billing as soon as the instance exists; open a usage session now.
	s.usage.OpenSession(ctx, provisionedEnv)

	return s.completeRemoteBootstrap(ctx, id, userEmail)
}

func (s *EnvironmentService) GetEnvironment(ctx context.Context, id, userEmail string) (*models.Environment, error) {
	return s.repo.GetByIDForUser(ctx, id, userEmail)
}

func (s *EnvironmentService) queueOperation(ctx context.Context, userEmail, environmentID, operationType string, job func() error) (*models.Operation, error) {
	if s.maxConcurrentOpsPerUser > 0 {
		inProgress, err := s.operationRepo.CountInProgressForUser(ctx, userEmail)
		if err != nil {
			return nil, err
		}
		if inProgress >= s.maxConcurrentOpsPerUser {
			return nil, ErrOperationQuotaExceeded
		}
	}

	op, err := s.operationRepo.Create(ctx, userEmail, environmentID, operationType, opStatusQueued, "")
	if err != nil {
		return nil, err
	}

	go func(operationID string) {
		_, _ = s.operationRepo.UpdateStatus(context.Background(), operationID, userEmail, opStatusRunning, "")
		err := job()
		if err != nil {
			_, _ = s.operationRepo.UpdateStatus(context.Background(), operationID, userEmail, opStatusFailed, err.Error())
			s.metrics.RecordOperation(operationType, opStatusFailed)
			s.alerts.Send("operation_failed", "error", "async operation failed", map[string]any{
				"operation_id":   operationID,
				"operation_type": operationType,
				"environment_id": environmentID,
				"user_email":     userEmail,
				"error":          err.Error(),
			})
			return
		}
		_, _ = s.operationRepo.UpdateStatus(context.Background(), operationID, userEmail, opStatusSucceeded, "")
		s.metrics.RecordOperation(operationType, opStatusSucceeded)
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
	cloudStatus := cloudProvisioned
	if env.InstanceID == "" {
		cloudStatus = cloudProvisioning
	}
	_, _ = s.repo.UpdateProvisioning(
		ctx,
		env.ID,
		env.UserEmail,
		cloudStatus,
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

// normalizeRepoURL validates the optional auto-clone repository URL.
func normalizeRepoURL(repoURL string) (string, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", nil
	}
	if !strings.HasPrefix(repoURL, "https://") {
		return "", &ProvisionValidationError{Code: "invalid_repo_url", Message: "repo_url must be an https:// git URL"}
	}
	if strings.ContainsAny(repoURL, " '\"`;|&<>") {
		return "", &ProvisionValidationError{Code: "invalid_repo_url", Message: "repo_url contains invalid characters"}
	}
	return repoURL, nil
}

// buildCloneScript produces an idempotent shell script that installs git when missing
// and clones the repository into /workspace.
func buildCloneScript(repoURL string) string {
	return fmt.Sprintf(
		`set -e
if ! command -v git >/dev/null 2>&1; then
  (apk add --no-cache git >/dev/null 2>&1) || (apt-get update >/dev/null 2>&1 && apt-get install -y git >/dev/null 2>&1) || (dnf install -y git >/dev/null 2>&1) || (yum install -y git >/dev/null 2>&1)
fi
mkdir -p /workspace
cd /workspace
name=$(basename %s .git)
if [ ! -d "$name" ]; then
  git clone %s "$name"
fi`,
		shellQuote(repoURL),
		shellQuote(repoURL),
	)
}

// cloneRepoIntoWorkspace runs the auto-clone script inside the workspace container.
func (s *EnvironmentService) cloneRepoIntoWorkspace(ctx context.Context, env *models.Environment) {
	if env == nil || env.RepoURL == "" || isPlaceholderContainerID(env.ContainerID) {
		return
	}

	cloneCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	script := buildCloneScript(env.RepoURL)

	if s.resolver.LocalBackend() == RuntimeBackendKubernetes && env.RuntimeTarget != runtimeTargetRemote {
		k8s := s.resolver.KubernetesRuntime()
		_, _ = k8s.runKubectl(cloneCtx, "exec", "deploy/"+env.ContainerID, "--", "sh", "-c", script)
		return
	}

	runtime, err := s.runtimeFor(env)
	if err != nil {
		return
	}
	if runner, ok := runtime.(dockerCommandRunner); ok {
		_, _ = runner.runDocker(cloneCtx, "exec", workspaceContainerRef(env), "sh", "-c", script)
	}
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
