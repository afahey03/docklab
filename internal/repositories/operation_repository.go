package repositories

import (
	"context"
	"errors"
	"time"

	"github.com/afahey03/docklab/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrOperationNotFound = errors.New("operation not found")

type OperationRepository interface {
	Create(ctx context.Context, userEmail, environmentID, operationType, status, errorMessage string) (*models.Operation, error)
	GetByIDForUser(ctx context.Context, id, userEmail string) (*models.Operation, error)
	UpdateStatus(ctx context.Context, id, userEmail, status, errorMessage string) (*models.Operation, error)
	ExistsInProgressForEnvironment(ctx context.Context, environmentID, userEmail string) (bool, error)
	CountInProgressForUser(ctx context.Context, userEmail string) (int, error)
	FailStaleInProgressForEnvironment(ctx context.Context, environmentID, userEmail string, olderThan time.Duration) (int64, error)
	FailInProgressForEnvironment(ctx context.Context, environmentID, userEmail, reason string) (int64, error)

	// Reconciliation: marks queued/running operations older than olderThan as failed.
	MarkStaleAsFailed(ctx context.Context, olderThan time.Duration) (int64, error)
}

type PostgresOperationRepository struct {
	db *pgxpool.Pool
}

func NewPostgresOperationRepository(db *pgxpool.Pool) *PostgresOperationRepository {
	return &PostgresOperationRepository{db: db}
}

func (r *PostgresOperationRepository) Create(ctx context.Context, userEmail, environmentID, operationType, status, errorMessage string) (*models.Operation, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		INSERT INTO operations (user_email, environment_id, type, status, error)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_email, environment_id, type, status, error, created_at, updated_at
	`

	var op models.Operation
	err := r.db.QueryRow(ctx, query, userEmail, environmentID, operationType, status, errorMessage).Scan(
		&op.ID,
		&op.UserEmail,
		&op.EnvironmentID,
		&op.Type,
		&op.Status,
		&op.Error,
		&op.CreatedAt,
		&op.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &op, nil
}

func (r *PostgresOperationRepository) GetByIDForUser(ctx context.Context, id, userEmail string) (*models.Operation, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		SELECT id, user_email, environment_id, type, status, error, created_at, updated_at
		FROM operations
		WHERE id = $1 AND user_email = $2
	`

	var op models.Operation
	err := r.db.QueryRow(ctx, query, id, userEmail).Scan(
		&op.ID,
		&op.UserEmail,
		&op.EnvironmentID,
		&op.Type,
		&op.Status,
		&op.Error,
		&op.CreatedAt,
		&op.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}

	return &op, nil
}

func (r *PostgresOperationRepository) UpdateStatus(ctx context.Context, id, userEmail, status, errorMessage string) (*models.Operation, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		UPDATE operations
		SET status = $3, error = $4, updated_at = NOW()
		WHERE id = $1 AND user_email = $2
		RETURNING id, user_email, environment_id, type, status, error, created_at, updated_at
	`

	var op models.Operation
	err := r.db.QueryRow(ctx, query, id, userEmail, status, errorMessage).Scan(
		&op.ID,
		&op.UserEmail,
		&op.EnvironmentID,
		&op.Type,
		&op.Status,
		&op.Error,
		&op.CreatedAt,
		&op.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}

	return &op, nil
}

func (r *PostgresOperationRepository) ExistsInProgressForEnvironment(ctx context.Context, environmentID, userEmail string) (bool, error) {
	if r.db == nil {
		return false, errors.New("database connection is nil")
	}

	const query = `
		SELECT EXISTS(
			SELECT 1
			FROM operations
			WHERE environment_id = $1
			  AND user_email = $2
			  AND status IN ('queued', 'running')
		)
	`

	var exists bool
	if err := r.db.QueryRow(ctx, query, environmentID, userEmail).Scan(&exists); err != nil {
		return false, err
	}

	return exists, nil
}

func (r *PostgresOperationRepository) CountInProgressForUser(ctx context.Context, userEmail string) (int, error) {
	if r.db == nil {
		return 0, errors.New("database connection is nil")
	}

	const query = `
		SELECT COUNT(*)
		FROM operations
		WHERE user_email = $1
		  AND status IN ('queued', 'running')`

	var count int
	if err := r.db.QueryRow(ctx, query, userEmail).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *PostgresOperationRepository) FailStaleInProgressForEnvironment(ctx context.Context, environmentID, userEmail string, olderThan time.Duration) (int64, error) {
	if r.db == nil {
		return 0, errors.New("database connection is nil")
	}

	cutoff := time.Now().Add(-olderThan)
	const query = `
		UPDATE operations
		SET status = 'failed',
		    error = 'operation timed out: exceeded maximum allowed duration for this environment',
		    updated_at = NOW()
		WHERE environment_id = $1
		  AND user_email = $2
		  AND status IN ('queued', 'running')
		  AND updated_at < $3`

	result, err := r.db.Exec(ctx, query, environmentID, userEmail, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *PostgresOperationRepository) FailInProgressForEnvironment(ctx context.Context, environmentID, userEmail, reason string) (int64, error) {
	if r.db == nil {
		return 0, errors.New("database connection is nil")
	}

	const query = `
		UPDATE operations
		SET status = 'failed',
		    error = $3,
		    updated_at = NOW()
		WHERE environment_id = $1
		  AND user_email = $2
		  AND status IN ('queued', 'running')`

	result, err := r.db.Exec(ctx, query, environmentID, userEmail, reason)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// MarkStaleAsFailed transitions any operations stuck in queued or running for longer than
// olderThan to failed status. Returns the number of operations updated.
func (r *PostgresOperationRepository) MarkStaleAsFailed(ctx context.Context, olderThan time.Duration) (int64, error) {
	if r.db == nil {
		return 0, errors.New("database connection is nil")
	}

	cutoff := time.Now().Add(-olderThan)
	const query = `
		UPDATE operations
		SET status     = 'failed',
		    error      = 'operation timed out: exceeded maximum allowed duration',
		    updated_at = NOW()
		WHERE status IN ('queued', 'running')
		  AND updated_at < $1`

	result, err := r.db.Exec(ctx, query, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
