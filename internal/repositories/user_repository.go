package repositories

import (
	"context"
	"errors"

	"github.com/afahey03/docklab/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUserNotFound     = errors.New("user not found")
	ErrUserAlreadyExist = errors.New("user already exists")
)

type UserRepository interface {
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	Create(ctx context.Context, email, passwordHash string) (*models.User, error)
	// UpsertOAuth creates the user on first OAuth sign-in and is a no-op afterwards.
	UpsertOAuth(ctx context.Context, email, passwordHash, provider string) (*models.User, error)
}

type PostgresUserRepository struct {
	db *pgxpool.Pool
}

func NewPostgresUserRepository(db *pgxpool.Pool) *PostgresUserRepository {
	return &PostgresUserRepository{db: db}
}

func (r *PostgresUserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		SELECT id, email, password_hash, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	var user models.User
	err := r.db.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.Password,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (r *PostgresUserRepository) Create(ctx context.Context, email, passwordHash string) (*models.User, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		INSERT INTO users (email, password_hash)
		VALUES ($1, $2)
		RETURNING id, email, password_hash, created_at, updated_at
	`

	var user models.User
	err := r.db.QueryRow(ctx, query, email, passwordHash).Scan(
		&user.ID,
		&user.Email,
		&user.Password,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrUserAlreadyExist
		}
		return nil, err
	}

	return &user, nil
}

func (r *PostgresUserRepository) UpsertOAuth(ctx context.Context, email, passwordHash, provider string) (*models.User, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		INSERT INTO users (email, password_hash, auth_provider)
		VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE SET updated_at = NOW()
		RETURNING id, email, password_hash, created_at, updated_at
	`

	var user models.User
	err := r.db.QueryRow(ctx, query, email, passwordHash, provider).Scan(
		&user.ID,
		&user.Email,
		&user.Password,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &user, nil
}
