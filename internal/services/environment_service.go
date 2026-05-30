package services

import (
	"context"
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
	opStatusQueued          = "queued"
	opStatusRunning         = "running"
	opStatusSucceeded       = "succeeded"
	opStatusFailed          = "failed"
)

var ErrDockerUnavailable = errors.New("docker CLI is not installed or unavailable")
var ErrProvisionInProgress = errors.New("provisioning is already in progress for this environment")
var ErrOperationNotFound = errors.New("operation not found")
var ErrOperationInProgress = errors.New("another long-running operation is already in progress for this environment")

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
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrDockerUnavailable
		}
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return "", fmt.Errorf("docker command failed: %w", err)
		}
		return "", fmt.Errorf("docker command failed: %s", trimmed)
	}
	return strings.TrimSpace(string(output)), nil
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
	runtime         ContainerRuntime
	terraformRunner TerraformRunner
}

func NewEnvironmentService(repo repositories.EnvironmentRepository, operationRepo repositories.OperationRepository, runtime ContainerRuntime) *EnvironmentService {
	return &EnvironmentService{
		repo:            repo,
		operationRepo:   operationRepo,
		runtime:         runtime,
		terraformRunner: NewTerraformCLIRunner(),
	}
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
	if env.CloudStatus == cloudProvisioning {
		return nil, ErrProvisionInProgress
	}
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
	hasInProgress, err := s.hasInProgressOperation(ctx, env.ID, userEmail)
	if err != nil {
		return nil, err
	}
	if hasInProgress {
		return nil, ErrOperationInProgress
	}
	if shouldDestroyCloudResources(env) {
		_, _ = s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudDeprovisioning, env.CloudRegion, env.InstanceID, env.PublicIP, env.TerraformDir, "")
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
	hasInProgress, err := s.hasInProgressOperation(ctx, env.ID, userEmail)
	if err != nil {
		return nil, err
	}
	if hasInProgress {
		return nil, ErrOperationInProgress
	}
	if shouldDestroyCloudResources(env) {
		_, _ = s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudDeprovisioning, env.CloudRegion, env.InstanceID, env.PublicIP, env.TerraformDir, "")
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

func (s *EnvironmentService) CreateEnvironment(ctx context.Context, userEmail, name, image string) (*models.Environment, error) {
	if image == "" {
		image = DefaultEnvironmentImage
	}
	if name == "" {
		name = generateEnvironmentName(userEmail)
	}

	containerID, err := s.runtime.CreateWorkspace(ctx, name, image, map[string]string{
		"docklab.user_email": userEmail,
		"docklab.name":       name,
	})
	if err != nil {
		return nil, err
	}

	return s.repo.Create(ctx, userEmail, name, image, statusRunning, containerID)
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

	if err := s.runtime.StartWorkspace(ctx, env.ContainerID); err != nil {
		return nil, err
	}

	return s.repo.UpdateStatus(ctx, id, userEmail, statusRunning)
}

func (s *EnvironmentService) StopEnvironment(ctx context.Context, id, userEmail string) (*models.Environment, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}
	if env.Status == statusStopped {
		return env, nil
	}

	if err := s.runtime.StopWorkspace(ctx, env.ContainerID); err != nil {
		return nil, err
	}

	return s.repo.UpdateStatus(ctx, id, userEmail, statusStopped)
}

func (s *EnvironmentService) DeleteEnvironment(ctx context.Context, id, userEmail string) error {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return err
	}

	if err := s.destroyCloudResources(ctx, env, userEmail); err != nil {
		return err
	}

	if err := s.runtime.DeleteWorkspace(ctx, env.ContainerID); err != nil {
		return err
	}

	return s.repo.Delete(ctx, id, userEmail)
}

func (s *EnvironmentService) DestroyCloudEnvironment(ctx context.Context, id, userEmail string) (*models.Environment, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}

	if err := s.destroyCloudResources(ctx, env, userEmail); err != nil {
		return nil, err
	}

	return s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudNotProvisioned, "", "", "", "", "")
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
	err := s.terraformRunner.DestroyEC2(destroyCtx, env.TerraformDir)
	cancel()
	if err != nil {
		_, _ = s.repo.UpdateProvisioning(
			ctx,
			env.ID,
			userEmail,
			cloudProvisionFailed,
			env.CloudRegion,
			env.InstanceID,
			env.PublicIP,
			env.TerraformDir,
			fmt.Sprintf("destroy failed: %v", err),
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
	if env.CloudStatus == cloudProvisioning {
		return nil, ErrProvisionInProgress
	}

	_, err = s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudProvisioning, req.Region, "", "", env.TerraformDir, "")
	if err != nil {
		return nil, err
	}

	provisionCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	result, err := s.terraformRunner.ProvisionEC2(provisionCtx, env.ID, req, env.TerraformDir)
	if err != nil {
		failedTerraformDir := env.TerraformDir
		var workspaceErr *TerraformWorkspaceError
		if errors.As(err, &workspaceErr) && workspaceErr.TerraformDir != "" {
			failedTerraformDir = workspaceErr.TerraformDir
		}

		failedEnv, updateErr := s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudProvisionFailed, req.Region, "", "", failedTerraformDir, err.Error())
		if updateErr == nil {
			return failedEnv, nil
		}
		return nil, err
	}

	return s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudProvisioned, req.Region, result.InstanceID, result.PublicIP, result.TerraformDir, "")
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
	req.KeyName = strings.TrimSpace(req.KeyName)

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

	if req.KeyName != "" && !keyNamePattern.MatchString(req.KeyName) {
		return ProvisionRequest{}, &ProvisionValidationError{Code: "invalid_key_name", Message: "key_name may only include letters, numbers, dot, underscore, and hyphen"}
	}

	return req, nil
}
