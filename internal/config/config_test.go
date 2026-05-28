package config

import (
	"testing"
)

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("JWT_TTL_MINUTES", "")

	cfg := Load()

	if cfg.AppEnv != "development" {
		t.Fatalf("expected default env, got %s", cfg.AppEnv)
	}
	if cfg.Port != "8080" {
		t.Fatalf("expected default port, got %s", cfg.Port)
	}
	if cfg.JWTSecret == "" {
		t.Fatal("expected jwt secret default")
	}
}

func TestLoadUsesEnvironmentVariables(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("PORT", "9000")
	t.Setenv("DATABASE_URL", "postgres://custom")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("JWT_TTL_MINUTES", "120")

	cfg := Load()

	if cfg.AppEnv != "production" {
		t.Fatalf("expected production env, got %s", cfg.AppEnv)
	}
	if cfg.Port != "9000" {
		t.Fatalf("expected port 9000, got %s", cfg.Port)
	}
	if cfg.DatabaseURL != "postgres://custom" {
		t.Fatalf("expected custom database url, got %s", cfg.DatabaseURL)
	}
	if cfg.JWTSecret != "secret" {
		t.Fatalf("expected secret jwt key, got %s", cfg.JWTSecret)
	}
	if cfg.JWTTTLMinutes != 120 {
		t.Fatalf("expected ttl 120, got %d", cfg.JWTTTLMinutes)
	}
}
