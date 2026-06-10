package repositories

import (
	"context"
	"errors"
	"time"

	"github.com/afahey03/docklab/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UsageRepository interface {
	OpenSession(ctx context.Context, environmentID, userEmail, environmentName, instanceType, region string, hourlyRateUSD float64) (*models.EnvironmentUsage, error)
	// CloseOpenSessions finalizes all open sessions for an environment, computing runtime
	// and estimated cost from the stored hourly rate. Returns the number closed.
	CloseOpenSessions(ctx context.Context, environmentID string) (int64, error)
	HasOpenSession(ctx context.Context, environmentID string) (bool, error)
	ListByUser(ctx context.Context, userEmail string, limit int) ([]models.EnvironmentUsage, error)
	// SumCostForUserSince totals estimated cost for closed sessions plus the accrued cost
	// of open sessions, counting only runtime after the since timestamp.
	SumCostForUserSince(ctx context.Context, userEmail string, since time.Time) (float64, error)
}

type PostgresUsageRepository struct {
	db *pgxpool.Pool
}

func NewPostgresUsageRepository(db *pgxpool.Pool) *PostgresUsageRepository {
	return &PostgresUsageRepository{db: db}
}

const usageColumns = `id, environment_id, user_email, environment_name, instance_type, region, hourly_rate_usd, started_at, ended_at, runtime_minutes, estimated_cost_usd`

func scanUsage(row interface{ Scan(dest ...any) error }, usage *models.EnvironmentUsage) error {
	return row.Scan(
		&usage.ID,
		&usage.EnvironmentID,
		&usage.UserEmail,
		&usage.EnvironmentName,
		&usage.InstanceType,
		&usage.Region,
		&usage.HourlyRateUSD,
		&usage.StartedAt,
		&usage.EndedAt,
		&usage.RuntimeMinutes,
		&usage.EstimatedCostUSD,
	)
}

func (r *PostgresUsageRepository) OpenSession(ctx context.Context, environmentID, userEmail, environmentName, instanceType, region string, hourlyRateUSD float64) (*models.EnvironmentUsage, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	// Avoid duplicate open sessions for the same environment.
	if _, err := r.CloseOpenSessions(ctx, environmentID); err != nil {
		return nil, err
	}

	query := `
		INSERT INTO environment_usage (environment_id, user_email, environment_name, instance_type, region, hourly_rate_usd)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + usageColumns

	var usage models.EnvironmentUsage
	if err := scanUsage(r.db.QueryRow(ctx, query, environmentID, userEmail, environmentName, instanceType, region, hourlyRateUSD), &usage); err != nil {
		return nil, err
	}
	return &usage, nil
}

func (r *PostgresUsageRepository) CloseOpenSessions(ctx context.Context, environmentID string) (int64, error) {
	if r.db == nil {
		return 0, errors.New("database connection is nil")
	}

	const query = `
		UPDATE environment_usage
		SET
			ended_at = NOW(),
			runtime_minutes = EXTRACT(EPOCH FROM (NOW() - started_at)) / 60,
			estimated_cost_usd = (EXTRACT(EPOCH FROM (NOW() - started_at)) / 3600) * hourly_rate_usd
		WHERE environment_id = $1
		  AND ended_at IS NULL`

	result, err := r.db.Exec(ctx, query, environmentID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *PostgresUsageRepository) HasOpenSession(ctx context.Context, environmentID string) (bool, error) {
	if r.db == nil {
		return false, errors.New("database connection is nil")
	}

	var exists bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM environment_usage WHERE environment_id = $1 AND ended_at IS NULL)`, environmentID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *PostgresUsageRepository) ListByUser(ctx context.Context, userEmail string, limit int) ([]models.EnvironmentUsage, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT ` + usageColumns + `
		FROM environment_usage
		WHERE user_email = $1
		ORDER BY started_at DESC
		LIMIT $2`

	rows, err := r.db.Query(ctx, query, userEmail, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make([]models.EnvironmentUsage, 0)
	for rows.Next() {
		var usage models.EnvironmentUsage
		if err := scanUsage(rows, &usage); err != nil {
			return nil, err
		}
		sessions = append(sessions, usage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return sessions, nil
}

func (r *PostgresUsageRepository) SumCostForUserSince(ctx context.Context, userEmail string, since time.Time) (float64, error) {
	if r.db == nil {
		return 0, errors.New("database connection is nil")
	}

	// Prorate sessions that started before the window so only in-window runtime counts.
	const query = `
		SELECT COALESCE(SUM(
			(EXTRACT(EPOCH FROM (COALESCE(ended_at, NOW()) - GREATEST(started_at, $2))) / 3600) * hourly_rate_usd
		), 0)
		FROM environment_usage
		WHERE user_email = $1
		  AND COALESCE(ended_at, NOW()) > $2`

	var total float64
	if err := r.db.QueryRow(ctx, query, userEmail, since).Scan(&total); err != nil {
		return 0, err
	}
	if total < 0 {
		total = 0
	}
	return total, nil
}
