package repositories

import (
	"context"
	"errors"

	"github.com/afahey03/docklab/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrShareAlreadyExists = errors.New("environment is already shared with this user")

type ShareRepository interface {
	Create(ctx context.Context, environmentID, ownerEmail, sharedWithEmail string) (*models.EnvironmentShare, error)
	ListForEnvironment(ctx context.Context, environmentID, ownerEmail string) ([]models.EnvironmentShare, error)
	Delete(ctx context.Context, environmentID, ownerEmail, sharedWithEmail string) error
	IsSharedWith(ctx context.Context, environmentID, userEmail string) (bool, error)
}

type PostgresShareRepository struct {
	db *pgxpool.Pool
}

func NewPostgresShareRepository(db *pgxpool.Pool) *PostgresShareRepository {
	return &PostgresShareRepository{db: db}
}

func (r *PostgresShareRepository) Create(ctx context.Context, environmentID, ownerEmail, sharedWithEmail string) (*models.EnvironmentShare, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		INSERT INTO environment_shares (environment_id, owner_email, shared_with_email)
		VALUES ($1, $2, $3)
		ON CONFLICT (environment_id, shared_with_email) DO NOTHING
		RETURNING id, environment_id, owner_email, shared_with_email, created_at`

	var share models.EnvironmentShare
	err := r.db.QueryRow(ctx, query, environmentID, ownerEmail, sharedWithEmail).Scan(
		&share.ID,
		&share.EnvironmentID,
		&share.OwnerEmail,
		&share.SharedWithEmail,
		&share.CreatedAt,
	)
	if err != nil {
		// ON CONFLICT DO NOTHING returns no rows when the share already exists.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrShareAlreadyExists
		}
		return nil, err
	}
	return &share, nil
}

func (r *PostgresShareRepository) ListForEnvironment(ctx context.Context, environmentID, ownerEmail string) ([]models.EnvironmentShare, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		SELECT id, environment_id, owner_email, shared_with_email, created_at
		FROM environment_shares
		WHERE environment_id = $1 AND owner_email = $2
		ORDER BY created_at ASC`

	rows, err := r.db.Query(ctx, query, environmentID, ownerEmail)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	shares := make([]models.EnvironmentShare, 0)
	for rows.Next() {
		var share models.EnvironmentShare
		if err := rows.Scan(&share.ID, &share.EnvironmentID, &share.OwnerEmail, &share.SharedWithEmail, &share.CreatedAt); err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return shares, nil
}

func (r *PostgresShareRepository) Delete(ctx context.Context, environmentID, ownerEmail, sharedWithEmail string) error {
	if r.db == nil {
		return errors.New("database connection is nil")
	}

	const query = `
		DELETE FROM environment_shares
		WHERE environment_id = $1 AND owner_email = $2 AND shared_with_email = $3`

	_, err := r.db.Exec(ctx, query, environmentID, ownerEmail, sharedWithEmail)
	return err
}

func (r *PostgresShareRepository) IsSharedWith(ctx context.Context, environmentID, userEmail string) (bool, error) {
	if r.db == nil {
		return false, errors.New("database connection is nil")
	}

	var exists bool
	err := r.db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM environment_shares WHERE environment_id = $1 AND shared_with_email = $2)`,
		environmentID,
		userEmail,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
