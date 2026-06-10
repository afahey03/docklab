package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/afahey03/docklab/internal/models"
	"github.com/afahey03/docklab/internal/repositories"
)

// SnapshotService creates and restores point-in-time workspace snapshots backed by
// docker commit. Snapshot images live on the Docker host that runs the workspace
// (the local daemon, or the EC2 host for remote workspaces).
type SnapshotService struct {
	environmentRepo repositories.EnvironmentRepository
	snapshotRepo    repositories.SnapshotRepository
	resolver        *RuntimeResolver
}

func NewSnapshotService(
	environmentRepo repositories.EnvironmentRepository,
	snapshotRepo repositories.SnapshotRepository,
	resolver *RuntimeResolver,
) *SnapshotService {
	return &SnapshotService{
		environmentRepo: environmentRepo,
		snapshotRepo:    snapshotRepo,
		resolver:        resolver,
	}
}

func snapshotImageTag(environmentID string, takenAt time.Time) string {
	shortID := environmentID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return fmt.Sprintf("docklab-snapshot-%s:%d", strings.ToLower(shortID), takenAt.UTC().Unix())
}

const workspaceSnapshotArchive = "/.docklab-snapshot-workspace.tgz"

// stageWorkspaceSnapshotScript packs the mounted /workspace volume into a path inside
// the container root filesystem so docker commit includes workspace files (commits
// exclude bind/named volume mounts).
const stageWorkspaceSnapshotScript = "tar czf " + workspaceSnapshotArchive + " -C /workspace ."

// restoreWorkspaceSnapshotScript unpacks the archived workspace back onto the volume
// after a container is recreated from a snapshot image.
const restoreWorkspaceSnapshotScript = "if [ -f " + workspaceSnapshotArchive + " ]; then find /workspace -mindepth 1 -delete && tar xzf " + workspaceSnapshotArchive + " -C /workspace; fi"

func stageWorkspaceSnapshot(ctx context.Context, runner dockerCommandRunner, containerRef string) error {
	if strings.TrimSpace(containerRef) == "" {
		return fmt.Errorf("workspace container ref is empty")
	}
	_, err := runner.runDocker(ctx, "exec", containerRef, "sh", "-c", stageWorkspaceSnapshotScript)
	if err != nil {
		return fmt.Errorf("stage workspace for snapshot: %w", err)
	}
	return nil
}

func restoreWorkspaceSnapshot(ctx context.Context, runner dockerCommandRunner, containerRef string) error {
	if strings.TrimSpace(containerRef) == "" {
		return fmt.Errorf("workspace container ref is empty")
	}
	_, err := runner.runDocker(ctx, "exec", containerRef, "sh", "-c", restoreWorkspaceSnapshotScript)
	if err != nil {
		return fmt.Errorf("restore workspace from snapshot: %w", err)
	}
	return nil
}

func (s *SnapshotService) CreateSnapshot(ctx context.Context, environmentID, userEmail, note string) (*models.EnvironmentSnapshot, error) {
	env, err := s.environmentRepo.GetByIDForUser(ctx, environmentID, userEmail)
	if err != nil {
		return nil, err
	}
	if isPlaceholderContainerID(env.ContainerID) {
		return nil, &ProvisionValidationError{Code: "workspace_unavailable", Message: "workspace is not ready yet"}
	}

	runtime, err := s.resolver.ForEnvironment(env)
	if err != nil {
		return nil, err
	}

	runner, ok := runtime.(dockerCommandRunner)
	if !ok {
		return nil, ErrSnapshotUnsupportedRuntime
	}

	containerRef := workspaceContainerRef(env)
	// docker commit excludes mounted volumes; pack /workspace into the image layer first.
	if err := stageWorkspaceSnapshot(ctx, runner, containerRef); err != nil {
		return nil, err
	}

	imageTag := snapshotImageTag(env.ID, time.Now())
	if err := runtime.CommitWorkspace(ctx, containerRef, imageTag); err != nil {
		return nil, fmt.Errorf("commit workspace snapshot: %w", err)
	}

	return s.snapshotRepo.Create(ctx, env.ID, userEmail, imageTag, strings.TrimSpace(note), env.RuntimeTarget)
}

func (s *SnapshotService) ListSnapshots(ctx context.Context, environmentID, userEmail string) ([]models.EnvironmentSnapshot, error) {
	if _, err := s.environmentRepo.GetByIDForUser(ctx, environmentID, userEmail); err != nil {
		return nil, err
	}
	return s.snapshotRepo.ListForEnvironment(ctx, environmentID, userEmail)
}

// RestoreSnapshot replaces the workspace container with one created from the snapshot
// image and repopulates the /workspace volume from the archived copy embedded in
// that image (docker commit does not capture mounted volumes on its own).
func (s *SnapshotService) RestoreSnapshot(ctx context.Context, environmentID, snapshotID, userEmail string) (*models.Environment, error) {
	env, err := s.environmentRepo.GetByIDForUser(ctx, environmentID, userEmail)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.snapshotRepo.GetByIDForUser(ctx, snapshotID, userEmail)
	if err != nil {
		return nil, err
	}
	if snapshot.EnvironmentID != env.ID {
		return nil, repositories.ErrSnapshotNotFound
	}
	if snapshot.RuntimeTarget != env.RuntimeTarget {
		return nil, &ProvisionValidationError{
			Code:    "snapshot_runtime_mismatch",
			Message: "snapshot was taken on a different runtime target; restore is only supported on the same runtime",
		}
	}

	runtime, err := s.resolver.ForEnvironment(env)
	if err != nil {
		return nil, err
	}

	// Replace the container: stop and remove the current one, then recreate from the
	// snapshot image under the same stable name.
	if !isPlaceholderContainerID(env.ContainerID) {
		if err := runtime.StopWorkspace(ctx, workspaceContainerRef(env)); err != nil && !isWorkspaceStopIgnorable(err) {
			return nil, err
		}
		if err := runtime.DeleteWorkspace(ctx, workspaceContainerRef(env)); err != nil && !isWorkspaceDeleteIgnorable(err) {
			return nil, err
		}
	}

	containerName := workspaceContainerName(env)
	newContainerID, err := runtime.CreateWorkspace(ctx, containerName, snapshot.ImageTag, map[string]string{
		"docklab.user_email":  env.UserEmail,
		"docklab.name":        env.Name,
		"docklab.snapshot_id": snapshot.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("recreate workspace from snapshot: %w", err)
	}

	if runner, ok := runtime.(dockerCommandRunner); ok {
		if err := restoreWorkspaceSnapshot(ctx, runner, containerName); err != nil {
			return nil, err
		}
	}

	return s.environmentRepo.UpdateRuntime(ctx, env.ID, userEmail, env.RuntimeTarget, newContainerID, statusRunning)
}

func (s *SnapshotService) DeleteSnapshot(ctx context.Context, environmentID, snapshotID, userEmail string) error {
	env, err := s.environmentRepo.GetByIDForUser(ctx, environmentID, userEmail)
	if err != nil {
		return err
	}
	snapshot, err := s.snapshotRepo.GetByIDForUser(ctx, snapshotID, userEmail)
	if err != nil {
		return err
	}
	if snapshot.EnvironmentID != env.ID {
		return repositories.ErrSnapshotNotFound
	}

	// Remove the underlying image best-effort; the host may already have pruned it.
	if runtime, runtimeErr := s.resolver.ForEnvironment(env); runtimeErr == nil {
		if runner, ok := runtime.(dockerCommandRunner); ok {
			_, _ = runner.runDocker(ctx, "rmi", "-f", snapshot.ImageTag)
		}
	}

	return s.snapshotRepo.Delete(ctx, snapshotID, userEmail)
}
