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

	return nil
}
