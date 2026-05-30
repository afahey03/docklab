package services

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"sync"
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
	runtime         ContainerRuntime
	terraformRunner TerraformRunner
	operationsMu    sync.RWMutex
	operations      map[string]*models.Operation
}

func NewEnvironmentService(repo repositories.EnvironmentRepository, runtime ContainerRuntime) *EnvironmentService {
	return &EnvironmentService{
		repo:            repo,
		runtime:         runtime,
		terraformRunner: NewTerraformCLIRunner(),
		operations:      make(map[string]*models.Operation),
	}
}

func (s *EnvironmentService) QueueProvisionEnvironment(ctx context.Context, id, userEmail string, req ProvisionRequest) (*models.Operation, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}
	if env.CloudStatus == cloudProvisioning {
		return nil, ErrProvisionInProgress
	}
	if s.hasInProgressOperation(env.ID, userEmail) {
		return nil, ErrOperationInProgress
	}

	return s.queueOperation(userEmail, env.ID, opTypeProvision, func() error {
		_, provisionErr := s.ProvisionEnvironment(context.Background(), env.ID, userEmail, req)
		return provisionErr
	}), nil
}

func (s *EnvironmentService) QueueDestroyCloudEnvironment(ctx context.Context, id, userEmail string) (*models.Operation, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}
	if s.hasInProgressOperation(env.ID, userEmail) {
		return nil, ErrOperationInProgress
	}
	if shouldDestroyCloudResources(env) {
		_, _ = s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudDeprovisioning, env.CloudRegion, env.InstanceID, env.PublicIP, env.TerraformDir, "")
	}

	return s.queueOperation(userEmail, env.ID, opTypeDestroyCloud, func() error {
		_, destroyErr := s.DestroyCloudEnvironment(context.Background(), env.ID, userEmail)
		return destroyErr
	}), nil
}

func (s *EnvironmentService) QueueDeleteEnvironment(ctx context.Context, id, userEmail string) (*models.Operation, error) {
	env, err := s.repo.GetByIDForUser(ctx, id, userEmail)
	if err != nil {
		return nil, err
	}
	if s.hasInProgressOperation(env.ID, userEmail) {
		return nil, ErrOperationInProgress
	}
	if shouldDestroyCloudResources(env) {
		_, _ = s.repo.UpdateProvisioning(ctx, env.ID, userEmail, cloudDeprovisioning, env.CloudRegion, env.InstanceID, env.PublicIP, env.TerraformDir, "")
	}

	return s.queueOperation(userEmail, env.ID, opTypeDeleteEnvironment, func() error {
		return s.DeleteEnvironment(context.Background(), env.ID, userEmail)
	}), nil
}

func (s *EnvironmentService) GetOperation(_ context.Context, operationID, userEmail string) (*models.Operation, error) {
	s.operationsMu.RLock()
	defer s.operationsMu.RUnlock()

	op, ok := s.operations[operationID]
	if !ok || op.UserEmail != userEmail {
		return nil, ErrOperationNotFound
	}

	copy := *op
	return &copy, nil
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

func (s *EnvironmentService) queueOperation(userEmail, environmentID, operationType string, job func() error) *models.Operation {
	now := time.Now().UTC()
	op := &models.Operation{
		ID:            fmt.Sprintf("op-%d-%d", now.UnixNano(), rand.Int63()),
		UserEmail:     userEmail,
		EnvironmentID: environmentID,
		Type:          operationType,
		Status:        opStatusQueued,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	s.operationsMu.Lock()
	s.operations[op.ID] = op
	s.operationsMu.Unlock()

	go func(operationID string) {
		s.updateOperation(operationID, opStatusRunning, "")
		err := job()
		if err != nil {
			s.updateOperation(operationID, opStatusFailed, err.Error())
			return
		}
		s.updateOperation(operationID, opStatusSucceeded, "")
	}(op.ID)

	copy := *op
	return &copy
}

func (s *EnvironmentService) hasInProgressOperation(environmentID, userEmail string) bool {
	s.operationsMu.RLock()
	defer s.operationsMu.RUnlock()

	for _, op := range s.operations {
		if op.EnvironmentID != environmentID || op.UserEmail != userEmail {
			continue
		}
		if op.Status == opStatusQueued || op.Status == opStatusRunning {
			return true
		}
	}

	return false
}

func (s *EnvironmentService) updateOperation(operationID, status, errMsg string) {
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()

	op, ok := s.operations[operationID]
	if !ok {
		return
	}

	op.Status = status
	op.Error = errMsg
	op.UpdatedAt = time.Now().UTC()
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
