package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/afahey03/docklab/internal/models"
	"github.com/afahey03/docklab/internal/repositories"
)

// UsageService persists EC2 runtime sessions and produces billing summaries plus
// budget alerts. Sessions open when an instance becomes billable and close when it
// stops being billable.
type UsageService struct {
	usageRepo    repositories.UsageRepository
	settingsRepo repositories.SettingsRepository
	pricing      *PricingService
	alerts       *AlertService
	log          *slog.Logger

	// budget alert dedupe: userEmail -> month key last alerted
	alertedMonths map[string]string
}

type UsageSummary struct {
	Sessions          []models.EnvironmentUsage `json:"sessions"`
	TotalCostUSD      float64                   `json:"total_cost_usd"`
	MonthToDateUSD    float64                   `json:"month_to_date_usd"`
	OpenSessionCount  int                       `json:"open_session_count"`
	TotalSessionCount int                       `json:"total_session_count"`
}

type BillingSummary struct {
	Month            string                `json:"month"`
	MonthToDateUSD   float64               `json:"month_to_date_usd"`
	MonthlyBudgetUSD float64               `json:"monthly_budget_usd"`
	OverBudget       bool                  `json:"over_budget"`
	BudgetUsedPct    float64               `json:"budget_used_pct"`
	Settings         *models.UserSettings  `json:"settings"`
	ByEnvironment    []EnvironmentBillItem `json:"by_environment"`
}

type EnvironmentBillItem struct {
	EnvironmentID   string  `json:"environment_id"`
	EnvironmentName string  `json:"environment_name"`
	InstanceType    string  `json:"instance_type"`
	Region          string  `json:"region"`
	RuntimeMinutes  float64 `json:"runtime_minutes"`
	CostUSD         float64 `json:"cost_usd"`
	Open            bool    `json:"open"`
}

func NewUsageService(
	usageRepo repositories.UsageRepository,
	settingsRepo repositories.SettingsRepository,
	pricing *PricingService,
	alerts *AlertService,
	log *slog.Logger,
) *UsageService {
	return &UsageService{
		usageRepo:     usageRepo,
		settingsRepo:  settingsRepo,
		pricing:       pricing,
		alerts:        alerts,
		log:           log,
		alertedMonths: make(map[string]string),
	}
}

// OpenSession starts a billable usage session for an environment's EC2 instance.
func (s *UsageService) OpenSession(ctx context.Context, env *models.Environment) {
	if s == nil || env == nil || env.InstanceID == "" {
		return
	}

	rate := s.pricing.GetRate(ctx, env.CloudInstanceType, env.CloudRegion)
	if _, err := s.usageRepo.OpenSession(ctx, env.ID, env.UserEmail, env.Name, env.CloudInstanceType, env.CloudRegion, rate.HourlyRateUSD); err != nil {
		s.log.Error("usage: failed to open session", "environment_id", env.ID, "error", err)
		return
	}
	s.log.Info("usage: opened session", "environment_id", env.ID, "instance_type", env.CloudInstanceType, "hourly_rate_usd", rate.HourlyRateUSD, "rate_source", rate.Source)
}

// CloseSession finalizes any open usage sessions for an environment.
func (s *UsageService) CloseSession(ctx context.Context, environmentID string) {
	if s == nil || environmentID == "" {
		return
	}

	closed, err := s.usageRepo.CloseOpenSessions(ctx, environmentID)
	if err != nil {
		s.log.Error("usage: failed to close sessions", "environment_id", environmentID, "error", err)
		return
	}
	if closed > 0 {
		s.log.Info("usage: closed sessions", "environment_id", environmentID, "count", closed)
	}
}

// EnsureSessionOpen opens a session only if none is currently open (used by
// reconciliation-style hooks where the trigger may fire more than once).
func (s *UsageService) EnsureSessionOpen(ctx context.Context, env *models.Environment) {
	if s == nil || env == nil || env.InstanceID == "" {
		return
	}
	open, err := s.usageRepo.HasOpenSession(ctx, env.ID)
	if err != nil {
		s.log.Error("usage: failed to check open session", "environment_id", env.ID, "error", err)
		return
	}
	if open {
		return
	}
	s.OpenSession(ctx, env)
}

func (s *UsageService) GetUsageSummary(ctx context.Context, userEmail string) (*UsageSummary, error) {
	sessions, err := s.usageRepo.ListByUser(ctx, userEmail, 200)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	summary := &UsageSummary{Sessions: sessions, TotalSessionCount: len(sessions)}
	for i := range sessions {
		session := &sessions[i]
		cost := session.EstimatedCostUSD
		if session.EndedAt == nil {
			summary.OpenSessionCount++
			cost = now.Sub(session.StartedAt).Hours() * session.HourlyRateUSD
			// Reflect accrued runtime for open sessions in the response.
			session.RuntimeMinutes = now.Sub(session.StartedAt).Minutes()
			session.EstimatedCostUSD = cost
		}
		summary.TotalCostUSD += cost
	}

	monthToDate, err := s.usageRepo.SumCostForUserSince(ctx, userEmail, monthStart)
	if err != nil {
		return nil, err
	}
	summary.MonthToDateUSD = monthToDate
	summary.Sessions = sessions

	return summary, nil
}

func (s *UsageService) GetBillingSummary(ctx context.Context, userEmail string) (*BillingSummary, error) {
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	monthToDate, err := s.usageRepo.SumCostForUserSince(ctx, userEmail, monthStart)
	if err != nil {
		return nil, err
	}

	settings, err := s.settingsRepo.Get(ctx, userEmail)
	if err != nil {
		return nil, err
	}

	sessions, err := s.usageRepo.ListByUser(ctx, userEmail, 500)
	if err != nil {
		return nil, err
	}

	byEnv := make(map[string]*EnvironmentBillItem)
	order := make([]string, 0)
	for _, session := range sessions {
		end := now
		if session.EndedAt != nil {
			end = *session.EndedAt
		}
		if end.Before(monthStart) {
			continue
		}
		start := session.StartedAt
		if start.Before(monthStart) {
			start = monthStart
		}
		minutes := end.Sub(start).Minutes()
		cost := end.Sub(start).Hours() * session.HourlyRateUSD

		item, ok := byEnv[session.EnvironmentID]
		if !ok {
			item = &EnvironmentBillItem{
				EnvironmentID:   session.EnvironmentID,
				EnvironmentName: session.EnvironmentName,
				InstanceType:    session.InstanceType,
				Region:          session.Region,
			}
			byEnv[session.EnvironmentID] = item
			order = append(order, session.EnvironmentID)
		}
		item.RuntimeMinutes += minutes
		item.CostUSD += cost
		if session.EndedAt == nil {
			item.Open = true
		}
	}

	items := make([]EnvironmentBillItem, 0, len(order))
	for _, id := range order {
		items = append(items, *byEnv[id])
	}

	summary := &BillingSummary{
		Month:            now.Format("2006-01"),
		MonthToDateUSD:   monthToDate,
		MonthlyBudgetUSD: settings.MonthlyBudgetUSD,
		Settings:         settings,
		ByEnvironment:    items,
	}
	if settings.MonthlyBudgetUSD > 0 {
		summary.BudgetUsedPct = (monthToDate / settings.MonthlyBudgetUSD) * 100
		summary.OverBudget = monthToDate > settings.MonthlyBudgetUSD
	}

	return summary, nil
}

func (s *UsageService) GetSettings(ctx context.Context, userEmail string) (*models.UserSettings, error) {
	return s.settingsRepo.Get(ctx, userEmail)
}

func (s *UsageService) UpdateSettings(ctx context.Context, userEmail string, monthlyBudgetUSD float64, alertsEnabled bool) (*models.UserSettings, error) {
	if monthlyBudgetUSD < 0 {
		monthlyBudgetUSD = 0
	}
	return s.settingsRepo.Upsert(ctx, userEmail, monthlyBudgetUSD, alertsEnabled)
}

// StartBudgetWatcher periodically checks users with budgets and raises a webhook alert
// once per month when month-to-date spend crosses the budget.
func (s *UsageService) StartBudgetWatcher(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Minute
	}

	go func() {
		s.checkBudgets(ctx)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkBudgets(ctx)
			}
		}
	}()
}

func (s *UsageService) checkBudgets(ctx context.Context) {
	users, err := s.settingsRepo.ListWithBudget(ctx)
	if err != nil {
		s.log.Error("usage: failed to list budget users", "error", err)
		return
	}

	now := time.Now().UTC()
	monthKey := now.Format("2006-01")
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	for _, user := range users {
		if s.alertedMonths[user.UserEmail] == monthKey {
			continue
		}

		spend, err := s.usageRepo.SumCostForUserSince(ctx, user.UserEmail, monthStart)
		if err != nil {
			s.log.Error("usage: failed to sum spend for budget check", "user", user.UserEmail, "error", err)
			continue
		}

		if spend > user.MonthlyBudgetUSD {
			s.alertedMonths[user.UserEmail] = monthKey
			s.alerts.Send("budget_exceeded", "warning",
				"monthly cloud budget exceeded",
				map[string]any{
					"user_email":         user.UserEmail,
					"month":              monthKey,
					"month_to_date_usd":  spend,
					"monthly_budget_usd": user.MonthlyBudgetUSD,
				},
			)
		}
	}
}
