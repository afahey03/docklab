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
			runtime_target TEXT NOT NULL DEFAULT 'local',
			cloud_status TEXT NOT NULL DEFAULT 'not_provisioned',
			cloud_region TEXT NOT NULL DEFAULT '',
			cloud_instance_type TEXT NOT NULL DEFAULT '',
			cloud_key_name TEXT NOT NULL DEFAULT '',
			instance_id TEXT NOT NULL DEFAULT '',
			public_ip TEXT NOT NULL DEFAULT '',
			terraform_dir TEXT NOT NULL DEFAULT '',
			cloud_error TEXT NOT NULL DEFAULT '',
			cloud_provisioned_at TIMESTAMPTZ NULL,
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
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS cloud_instance_type TEXT NOT NULL DEFAULT '';
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS instance_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS public_ip TEXT NOT NULL DEFAULT '';
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS terraform_dir TEXT NOT NULL DEFAULT '';
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS cloud_error TEXT NOT NULL DEFAULT '';
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS cloud_provisioned_at TIMESTAMPTZ NULL;
	`

	if _, err := pool.Exec(ctx, environmentsColumns); err != nil {
		return fmt.Errorf("failed to ensure environments columns: %w", err)
	}

	const operationsTable = `
		CREATE TABLE IF NOT EXISTS operations (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_email TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
			environment_id TEXT NOT NULL,
			type TEXT NOT NULL,
			status TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_operations_user_email ON operations(user_email);
		CREATE INDEX IF NOT EXISTS idx_operations_environment_status ON operations(environment_id, status);
	`

	if _, err := pool.Exec(ctx, operationsTable); err != nil {
		return fmt.Errorf("failed to create operations table: %w", err)
	}

	const operationsColumns = `
		ALTER TABLE operations ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '';
	`

	if _, err := pool.Exec(ctx, operationsColumns); err != nil {
		return fmt.Errorf("failed to ensure operations columns: %w", err)
	}

	const activityColumns = `
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
	`

	if _, err := pool.Exec(ctx, activityColumns); err != nil {
		return fmt.Errorf("failed to ensure last_activity_at column: %w", err)
	}

	const remoteRuntimeColumns = `
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS runtime_target TEXT NOT NULL DEFAULT 'local';
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS cloud_key_name TEXT NOT NULL DEFAULT '';
	`

	if _, err := pool.Exec(ctx, remoteRuntimeColumns); err != nil {
		return fmt.Errorf("failed to ensure remote runtime columns: %w", err)
	}

	const creationModeColumn = `
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS creation_mode TEXT NOT NULL DEFAULT 'local';
	`

	if _, err := pool.Exec(ctx, creationModeColumn); err != nil {
		return fmt.Errorf("failed to ensure creation_mode column: %w", err)
	}

	const sprint9Columns = `
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS repo_url TEXT NOT NULL DEFAULT '';
		ALTER TABLE environments ADD COLUMN IF NOT EXISTS template_id TEXT NOT NULL DEFAULT '';
	`

	if _, err := pool.Exec(ctx, sprint9Columns); err != nil {
		return fmt.Errorf("failed to ensure repo/template columns: %w", err)
	}

	const usersAuthColumns = `
		ALTER TABLE users ADD COLUMN IF NOT EXISTS auth_provider TEXT NOT NULL DEFAULT 'local';
	`

	if _, err := pool.Exec(ctx, usersAuthColumns); err != nil {
		return fmt.Errorf("failed to ensure users auth columns: %w", err)
	}

	const refreshTokensTable = `
		CREATE TABLE IF NOT EXISTS refresh_tokens (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_email TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TIMESTAMPTZ NOT NULL,
			revoked_at TIMESTAMPTZ NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_email ON refresh_tokens(user_email);
	`

	if _, err := pool.Exec(ctx, refreshTokensTable); err != nil {
		return fmt.Errorf("failed to create refresh_tokens table: %w", err)
	}

	const environmentUsageTable = `
		CREATE TABLE IF NOT EXISTS environment_usage (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			environment_id TEXT NOT NULL,
			user_email TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
			environment_name TEXT NOT NULL DEFAULT '',
			instance_type TEXT NOT NULL DEFAULT '',
			region TEXT NOT NULL DEFAULT '',
			hourly_rate_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
			started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			ended_at TIMESTAMPTZ NULL,
			runtime_minutes DOUBLE PRECISION NOT NULL DEFAULT 0,
			estimated_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0
		);

		CREATE INDEX IF NOT EXISTS idx_environment_usage_user_email ON environment_usage(user_email);
		CREATE INDEX IF NOT EXISTS idx_environment_usage_environment_open ON environment_usage(environment_id) WHERE ended_at IS NULL;
	`

	if _, err := pool.Exec(ctx, environmentUsageTable); err != nil {
		return fmt.Errorf("failed to create environment_usage table: %w", err)
	}

	const userSettingsTable = `
		CREATE TABLE IF NOT EXISTS user_settings (
			user_email TEXT PRIMARY KEY REFERENCES users(email) ON DELETE CASCADE,
			monthly_budget_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
			budget_alerts_enabled BOOLEAN NOT NULL DEFAULT TRUE,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`

	if _, err := pool.Exec(ctx, userSettingsTable); err != nil {
		return fmt.Errorf("failed to create user_settings table: %w", err)
	}

	const environmentSharesTable = `
		CREATE TABLE IF NOT EXISTS environment_shares (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			environment_id UUID NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
			owner_email TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
			shared_with_email TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (environment_id, shared_with_email)
		);

		CREATE INDEX IF NOT EXISTS idx_environment_shares_shared_with ON environment_shares(shared_with_email);
	`

	if _, err := pool.Exec(ctx, environmentSharesTable); err != nil {
		return fmt.Errorf("failed to create environment_shares table: %w", err)
	}

	const environmentSnapshotsTable = `
		CREATE TABLE IF NOT EXISTS environment_snapshots (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			environment_id UUID NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
			user_email TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
			image_tag TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			runtime_target TEXT NOT NULL DEFAULT 'local',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_environment_snapshots_environment ON environment_snapshots(environment_id);
	`

	if _, err := pool.Exec(ctx, environmentSnapshotsTable); err != nil {
		return fmt.Errorf("failed to create environment_snapshots table: %w", err)
	}

	return nil
}
