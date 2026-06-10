package repositories

import (
	"context"
	"errors"

	"github.com/afahey03/docklab/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SettingsRepository interface {
	Get(ctx context.Context, userEmail string) (*models.UserSettings, error)
	Upsert(ctx context.Context, userEmail string, monthlyBudgetUSD float64, budgetAlertsEnabled bool) (*models.UserSettings, error)
	ListWithBudget(ctx context.Context) ([]models.UserSettings, error)
}

type PostgresSettingsRepository struct {
	db *pgxpool.Pool
}

func NewPostgresSettingsRepository(db *pgxpool.Pool) *PostgresSettingsRepository {
	return &PostgresSettingsRepository{db: db}
}

func (r *PostgresSettingsRepository) Get(ctx context.Context, userEmail string) (*models.UserSettings, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		SELECT user_email, monthly_budget_usd, budget_alerts_enabled, updated_at
		FROM user_settings
		WHERE user_email = $1`

	var settings models.UserSettings
	err := r.db.QueryRow(ctx, query, userEmail).Scan(
		&settings.UserEmail,
		&settings.MonthlyBudgetUSD,
		&settings.BudgetAlertsEnabled,
		&settings.UpdatedAt,
	)
	if err != nil {
		// Default settings for users who never saved any.
		if errors.Is(err, pgx.ErrNoRows) {
			return &models.UserSettings{UserEmail: userEmail, MonthlyBudgetUSD: 0, BudgetAlertsEnabled: true}, nil
		}
		return nil, err
	}
	return &settings, nil
}

func (r *PostgresSettingsRepository) Upsert(ctx context.Context, userEmail string, monthlyBudgetUSD float64, budgetAlertsEnabled bool) (*models.UserSettings, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		INSERT INTO user_settings (user_email, monthly_budget_usd, budget_alerts_enabled, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_email) DO UPDATE
		SET monthly_budget_usd = EXCLUDED.monthly_budget_usd,
		    budget_alerts_enabled = EXCLUDED.budget_alerts_enabled,
		    updated_at = NOW()
		RETURNING user_email, monthly_budget_usd, budget_alerts_enabled, updated_at`

	var settings models.UserSettings
	err := r.db.QueryRow(ctx, query, userEmail, monthlyBudgetUSD, budgetAlertsEnabled).Scan(
		&settings.UserEmail,
		&settings.MonthlyBudgetUSD,
		&settings.BudgetAlertsEnabled,
		&settings.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (r *PostgresSettingsRepository) ListWithBudget(ctx context.Context) ([]models.UserSettings, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		SELECT user_email, monthly_budget_usd, budget_alerts_enabled, updated_at
		FROM user_settings
		WHERE monthly_budget_usd > 0 AND budget_alerts_enabled = TRUE`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := make([]models.UserSettings, 0)
	for rows.Next() {
		var settings models.UserSettings
		if err := rows.Scan(&settings.UserEmail, &settings.MonthlyBudgetUSD, &settings.BudgetAlertsEnabled, &settings.UpdatedAt); err != nil {
			return nil, err
		}
		all = append(all, settings)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return all, nil
}
