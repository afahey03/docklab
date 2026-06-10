package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/afahey03/docklab/internal/repositories"
)

const (
	// staleOperationThreshold is how long an operation may stay in queued/running before
	// the reconciler marks it failed. This should be comfortably longer than the longest
	// expected Terraform apply/destroy (15 min timeout) plus some buffer.
	staleOperationThreshold = 30 * time.Minute

	// staleProvisioningThreshold is how long an environment may stay in a transitional
	// cloud_status before the reconciler marks it provision_failed.
	staleProvisioningThreshold = 30 * time.Minute

	// reconciliationInterval is how often the reconciliation loop runs.
	reconciliationInterval = 5 * time.Minute
)

// ReconciliationService runs background checks that detect and repair cloud drift:
//   - Operations stuck in queued/running for too long are marked failed.
//   - Environments stuck in provisioning/deprovisioning with no active operation are
//     marked provision_failed.
type ReconciliationService struct {
	environmentRepo repositories.EnvironmentRepository
	operationRepo   repositories.OperationRepository
	ec2             EC2InstanceClient
	log             *slog.Logger
	alerts          *AlertService
	usage           *UsageService
}

func (s *ReconciliationService) SetObservability(alerts *AlertService, usage *UsageService) {
	s.alerts = alerts
	s.usage = usage
}

func NewReconciliationService(
	environmentRepo repositories.EnvironmentRepository,
	operationRepo repositories.OperationRepository,
	ec2 EC2InstanceClient,
	log *slog.Logger,
) *ReconciliationService {
	return &ReconciliationService{
		environmentRepo: environmentRepo,
		operationRepo:   operationRepo,
		ec2:             ec2,
		log:             log,
	}
}

// Start launches the reconciliation loop as a background goroutine. It exits when ctx
// is cancelled (typically at server shutdown).
func (s *ReconciliationService) Start(ctx context.Context) {
	go func() {
		// Run once immediately on startup so we don't wait a full interval after a restart.
		s.runOnce(ctx)

		ticker := time.NewTicker(reconciliationInterval)
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

func (s *ReconciliationService) runOnce(ctx context.Context) {
	// 1. Mark stale operations as failed.
	stalOps, err := s.operationRepo.MarkStaleAsFailed(ctx, staleOperationThreshold)
	if err != nil {
		s.log.Error("reconciliation: failed to mark stale operations", "error", err)
	} else if stalOps > 0 {
		s.log.Warn("reconciliation: marked stale operations as failed", "count", stalOps)
		s.alerts.Send("reconciliation_stale_operations", "warning", "stale operations marked failed", map[string]any{"count": stalOps})
	}

	// 2. Mark environments stuck in a transitional cloud_status as provision_failed.
	staleEnvs, err := s.environmentRepo.ReconcileStaleProvisioning(ctx, staleProvisioningThreshold)
	if err != nil {
		s.log.Error("reconciliation: failed to reconcile stale provisioning", "error", err)
	} else if staleEnvs > 0 {
		s.log.Warn("reconciliation: marked stale provisioning environments as failed", "count", staleEnvs)
		s.alerts.Send("reconciliation_stale_provisioning", "warning", "stale provisioning environments repaired", map[string]any{"count": staleEnvs})
	}

	s.reconcileMissingCloudInstances(ctx)
}

func (s *ReconciliationService) reconcileMissingCloudInstances(ctx context.Context) {
	if s.ec2 == nil {
		return
	}

	envs, err := s.environmentRepo.ListWithCloudInstanceID(ctx)
	if err != nil {
		s.log.Error("reconciliation: failed to list cloud environments", "error", err)
		return
	}

	missing := make([]string, 0)
	missingEnvIDs := make([]string, 0)
	for _, env := range envs {
		state, describeErr := s.ec2.DescribeInstance(ctx, env.CloudRegion, env.InstanceID)
		if describeErr != nil {
			missing = append(missing, env.InstanceID)
			missingEnvIDs = append(missingEnvIDs, env.ID)
			continue
		}
		if state == nil || isEC2InstanceTerminated(state.State) {
			missing = append(missing, env.InstanceID)
			missingEnvIDs = append(missingEnvIDs, env.ID)
		}
	}

	if len(missing) == 0 {
		return
	}

	updated, err := s.environmentRepo.ReconcileMissingCloudInstances(ctx, missing)
	if err != nil {
		s.log.Error("reconciliation: failed to clear missing cloud instances", "error", err)
		return
	}
	if updated > 0 {
		s.log.Warn("reconciliation: cleared environments with missing ec2 instances", "count", updated)
		s.alerts.Send("reconciliation_missing_instances", "warning", "environments with missing EC2 instances cleared", map[string]any{"count": updated})
		for _, envID := range missingEnvIDs {
			s.usage.CloseSession(ctx, envID)
		}
	}
}
