package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func EnsureSchema(pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("database pool is nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto;`); err != nil {
		return fmt.Errorf("failed to enable pgcrypto extension: %w", err)
	}

	const usersTable = `
		CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`

	if _, err := pool.Exec(ctx, usersTable); err != nil {
		return fmt.Errorf("failed to create users table: %w", err)
	}

	const environmentsTable = `
		CREATE TABLE IF NOT EXISTS environments (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_email TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
			name TEXT NOT NULL,
			image TEXT NOT NULL,
			status TEXT NOT NULL,
			container_id TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_environments_user_email ON environments(user_email);
	`

	if _, err := pool.Exec(ctx, environmentsTable); err != nil {
		return fmt.Errorf("failed to create environments table: %w", err)
	}

	return nil
}
