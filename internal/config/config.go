package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppEnv                    string
	Port                      string
	DatabaseURL               string
	JWTSecret                 string
	JWTTTLMinutes             int
	IdleStopMinutes           int
	IdleCloudStopMinutes      int
	IdleCloudTerminateMinutes int
	CloudIdlePolicyEnabled    bool
	SSHUser                   string
	SSHPort                   int
	SSHPrivateKeyPath         string
	SSHConnectTimeout         int
	RemoteBootstrapMax        int
}

func Load() Config {
	return Config{
		AppEnv:                    getEnv("APP_ENV", "development"),
		Port:                      getEnv("PORT", "8080"),
		DatabaseURL:               getEnv("DATABASE_URL", "host=localhost port=5432 user=postgres dbname=docklab sslmode=disable"),
		JWTSecret:                 getEnv("JWT_SECRET", "dev-secret-change-me"),
		JWTTTLMinutes:             getEnvAsInt("JWT_TTL_MINUTES", 60),
		IdleStopMinutes:           getEnvAsInt("IDLE_STOP_AFTER_MINUTES", 60),
		IdleCloudStopMinutes:      getEnvAsInt("IDLE_CLOUD_STOP_AFTER_MINUTES", 0),
		IdleCloudTerminateMinutes: getEnvAsInt("IDLE_CLOUD_TERMINATE_AFTER_MINUTES", 24*60),
		CloudIdlePolicyEnabled:    getEnvAsBool("DOKLAB_CLOUD_IDLE_POLICY_ENABLED", true),
		SSHUser:                   getEnv("DOKLAB_SSH_USER", "ec2-user"),
		SSHPort:                   getEnvAsInt("DOKLAB_SSH_PORT", 22),
		SSHPrivateKeyPath:         getEnv("DOKLAB_SSH_PRIVATE_KEY_PATH", ""),
		SSHConnectTimeout:         getEnvAsInt("DOKLAB_SSH_CONNECT_TIMEOUT_SECONDS", 15),
		RemoteBootstrapMax:        getEnvAsInt("DOKLAB_REMOTE_BOOTSTRAP_TIMEOUT_SECONDS", 300),
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvAsBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
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
