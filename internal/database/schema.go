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
			cloud_status TEXT NOT NULL DEFAULT 'not_provisioned',
			cloud_region TEXT NOT NULL DEFAULT '',
			instance_id TEXT NOT NULL DEFAULT '',
			public_ip TEXT NOT NULL DEFAULT '',
			terraform_dir TEXT NOT NULL DEFAULT '',
			cloud_error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_environments_user_email ON environments(user_email);
	`

	if _, err := pool.Exec(ctx, environmentsTable); err != nil {
		return fmt.Errorf("failed to create environments table: %w", err)
	}

	const environmentsColumns = `
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS cloud_status TEXT NOT NULL DEFAULT 'not_provisioned';
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS cloud_region TEXT NOT NULL DEFAULT '';
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS instance_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS public_ip TEXT NOT NULL DEFAULT '';
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS terraform_dir TEXT NOT NULL DEFAULT '';
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS cloud_error TEXT NOT NULL DEFAULT '';
	`

	if _, err := pool.Exec(ctx, environmentsColumns); err != nil {
		return fmt.Errorf("failed to ensure environments columns: %w", err)
	}

	return nil
}
