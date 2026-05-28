package config

import (
	"os"
	"strconv"
)

type Config struct {
	AppEnv        string
	Port          string
	DatabaseURL   string
	JWTSecret     string
	JWTTTLMinutes int
}

func Load() Config {
	return Config{
		AppEnv:        getEnv("APP_ENV", "development"),
		Port:          getEnv("PORT", "8080"),
		DatabaseURL:   getEnv("DATABASE_URL", "host=localhost port=5432 user=postgres dbname=docklab sslmode=disable"),
		JWTSecret:     getEnv("JWT_SECRET", "dev-secret-change-me"),
		JWTTTLMinutes: getEnvAsInt("JWT_TTL_MINUTES", 60),
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvAsInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
