package repositories

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrRefreshTokenNotFound = errors.New("refresh token not found")

type RefreshTokenRepository interface {
	Create(ctx context.Context, userEmail, tokenHash string, expiresAt time.Time) error
	// GetActive returns the owning user email for a non-revoked, non-expired token hash.
	GetActive(ctx context.Context, tokenHash string) (string, error)
	Revoke(ctx context.Context, tokenHash string) error
	RevokeAllForUser(ctx context.Context, userEmail string) error
	DeleteExpired(ctx context.Context) (int64, error)
}

type PostgresRefreshTokenRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRefreshTokenRepository(db *pgxpool.Pool) *PostgresRefreshTokenRepository {
	return &PostgresRefreshTokenRepository{db: db}
}

func (r *PostgresRefreshTokenRepository) Create(ctx context.Context, userEmail, tokenHash string, expiresAt time.Time) error {
	if r.db == nil {
		return errors.New("database connection is nil")
	}

	const query = `
		INSERT INTO refresh_tokens (user_email, token_hash, expires_at)
		VALUES ($1, $2, $3)`

	_, err := r.db.Exec(ctx, query, userEmail, tokenHash, expiresAt)
	return err
}

func (r *PostgresRefreshTokenRepository) GetActive(ctx context.Context, tokenHash string) (string, error) {
	if r.db == nil {
		return "", errors.New("database connection is nil")
	}

	const query = `
		SELECT user_email
		FROM refresh_tokens
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > NOW()`

	var userEmail string
	err := r.db.QueryRow(ctx, query, tokenHash).Scan(&userEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRefreshTokenNotFound
	}
	if err != nil {
		return "", err
	}
	return userEmail, nil
}

func (r *PostgresRefreshTokenRepository) Revoke(ctx context.Context, tokenHash string) error {
	if r.db == nil {
		return errors.New("database connection is nil")
	}

	const query = `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE token_hash = $1
		  AND revoked_at IS NULL`

	_, err := r.db.Exec(ctx, query, tokenHash)
	return err
}

func (r *PostgresRefreshTokenRepository) RevokeAllForUser(ctx context.Context, userEmail string) error {
	if r.db == nil {
		return errors.New("database connection is nil")
	}

	const query = `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE user_email = $1
		  AND revoked_at IS NULL`

	_, err := r.db.Exec(ctx, query, userEmail)
	return err
}

func (r *PostgresRefreshTokenRepository) DeleteExpired(ctx context.Context) (int64, error) {
	if r.db == nil {
		return 0, errors.New("database connection is nil")
	}

	result, err := r.db.Exec(ctx, `DELETE FROM refresh_tokens WHERE expires_at < NOW() - INTERVAL '7 days'`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
