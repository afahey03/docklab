package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/afahey03/docklab/internal/repositories"
)

const (
	defaultLifecycleCheckInterval = 5 * time.Minute
	minimumLifecycleCheckInterval = 15 * time.Second
)

// LifecycleService runs a background worker that automatically stops running environments
// that have been idle (no terminal activity) for longer than the configured threshold.
// Cloud resources (EC2 instances) are left untouched; only the workspace container is
// stopped. Users can restart the environment from the dashboard at any time.
type LifecycleService struct {
	environmentRepo repositories.EnvironmentRepository
	resolver        *RuntimeResolver
	idleThreshold   time.Duration
	log             *slog.Logger
}

func NewLifecycleService(
	environmentRepo repositories.EnvironmentRepository,
	resolver *RuntimeResolver,
	idleStopMinutes int,
	log *slog.Logger,
) *LifecycleService {
	if idleStopMinutes <= 0 {
		idleStopMinutes = 60
	}
	return &LifecycleService{
		environmentRepo: environmentRepo,
		resolver:        resolver,
		idleThreshold:   time.Duration(idleStopMinutes) * time.Minute,
		log:             log,
	}
}

// Start launches the lifecycle worker as a background goroutine. It exits when ctx is
// cancelled (typically at server shutdown).
func (s *LifecycleService) Start(ctx context.Context) {
	checkInterval := s.checkInterval()
	s.log.Info("lifecycle worker started", "idle_threshold", s.idleThreshold, "check_interval", checkInterval)

	go func() {
		s.stopIdleEnvironments(ctx)

		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.stopIdleEnvironments(ctx)
			}
		}
	}()
}

func (s *LifecycleService) checkInterval() time.Duration {
	interval := s.idleThreshold / 2
	if interval < minimumLifecycleCheckInterval {
		interval = minimumLifecycleCheckInterval
	}
	if interval > defaultLifecycleCheckInterval {
		interval = defaultLifecycleCheckInterval
	}
	return interval
}

func (s *LifecycleService) stopIdleEnvironments(ctx context.Context) {
	cutoff := time.Now().Add(-s.idleThreshold)

	envs, err := s.environmentRepo.ListRunningIdleSince(ctx, cutoff)
	if err != nil {
		s.log.Error("lifecycle: failed to list idle environments", "error", err)
		return
	}

	for _, env := range envs {
		s.log.Info("lifecycle: stopping idle environment",
			"environment_id", env.ID,
			"user_email", env.UserEmail,
			"runtime_target", env.RuntimeTarget,
			"last_activity_at", env.LastActivityAt,
		)

		runtime, err := s.resolver.ForEnvironment(&env)
		if err != nil {
			s.log.Error("lifecycle: failed to resolve runtime",
				"environment_id", env.ID,
				"error", err,
			)
			continue
		}

		stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = runtime.StopWorkspace(stopCtx, env.ContainerID)
		cancel()

		if err != nil {
			s.log.Error("lifecycle: failed to stop idle environment",
				"environment_id", env.ID,
				"error", err,
			)
			continue
		}

		if _, err := s.environmentRepo.UpdateStatus(ctx, env.ID, env.UserEmail, statusStopped); err != nil {
			s.log.Error("lifecycle: failed to update status after idle stop",
				"environment_id", env.ID,
				"error", err,
			)
		}
	}
}
