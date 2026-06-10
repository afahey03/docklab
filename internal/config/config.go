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
	RefreshTokenTTLDays       int
	IdleStopMinutes           int
	IdleCloudStopMinutes      int
	IdleCloudTerminateMinutes int
	CloudIdlePolicyEnabled    bool
	SSHUser                   string
	SSHPort                   int
	SSHPrivateKeyPath         string
	SSHPrivateKeyB64          string
	SSHConnectTimeout         int
	RemoteBootstrapMax        int

	// Sprint 9 — production hardening
	RateLimitEnabled            bool
	AuthRateLimitPerMinute      int
	ProvisionRateLimitPerMinute int
	APIRateLimitPerMinute       int
	MaxEnvironmentsPerUser      int
	MaxConcurrentOpsPerUser     int
	MetricsEnabled              bool
	AlertWebhookURL             string
	FrontendBaseURL             string

	// GitHub OAuth
	GitHubClientID     string
	GitHubClientSecret string
	GitHubRedirectURL  string

	// Sprint 10 — cost tracking
	PricingAPIEnabled bool

	// Runtime backend: "docker" (default) or "kubernetes"
	RuntimeBackend      string
	KubernetesNamespace string
	KubernetesContext   string

	// Browser IDE (code-server sidecar)
	IDEEnabled    bool
	IDEImage      string
	IDERemotePort int
}

func Load() Config {
	return Config{
		AppEnv:                    getEnv("APP_ENV", "development"),
		Port:                      getEnv("PORT", "8080"),
		DatabaseURL:               getEnv("DATABASE_URL", "host=localhost port=5432 user=postgres dbname=docklab sslmode=disable"),
		JWTSecret:                 getEnv("JWT_SECRET", "dev-secret-change-me"),
		JWTTTLMinutes:             getEnvAsInt("JWT_TTL_MINUTES", 60),
		RefreshTokenTTLDays:       getEnvAsInt("REFRESH_TOKEN_TTL_DAYS", 30),
		IdleStopMinutes:           getEnvAsInt("IDLE_STOP_AFTER_MINUTES", 60),
		IdleCloudStopMinutes:      getEnvAsInt("IDLE_CLOUD_STOP_AFTER_MINUTES", 0),
		IdleCloudTerminateMinutes: getEnvAsInt("IDLE_CLOUD_TERMINATE_AFTER_MINUTES", 24*60),
		CloudIdlePolicyEnabled:    getEnvAsBool("DOKLAB_CLOUD_IDLE_POLICY_ENABLED", true),
		SSHUser:                   getEnv("DOKLAB_SSH_USER", "ec2-user"),
		SSHPort:                   getEnvAsInt("DOKLAB_SSH_PORT", 22),
		SSHPrivateKeyPath:         getEnv("DOKLAB_SSH_PRIVATE_KEY_PATH", ""),
		SSHPrivateKeyB64:          getEnv("DOKLAB_SSH_PRIVATE_KEY_B64", ""),
		SSHConnectTimeout:         getEnvAsInt("DOKLAB_SSH_CONNECT_TIMEOUT_SECONDS", 15),
		RemoteBootstrapMax:        getEnvAsInt("DOKLAB_REMOTE_BOOTSTRAP_TIMEOUT_SECONDS", 300),

		RateLimitEnabled:            getEnvAsBool("DOKLAB_RATE_LIMIT_ENABLED", true),
		AuthRateLimitPerMinute:      getEnvAsInt("DOKLAB_AUTH_RATE_LIMIT_PER_MINUTE", 20),
		ProvisionRateLimitPerMinute: getEnvAsInt("DOKLAB_PROVISION_RATE_LIMIT_PER_MINUTE", 10),
		APIRateLimitPerMinute:       getEnvAsInt("DOKLAB_API_RATE_LIMIT_PER_MINUTE", 240),
		MaxEnvironmentsPerUser:      getEnvAsInt("DOKLAB_MAX_ENVIRONMENTS_PER_USER", 10),
		MaxConcurrentOpsPerUser:     getEnvAsInt("DOKLAB_MAX_CONCURRENT_OPERATIONS_PER_USER", 3),
		MetricsEnabled:              getEnvAsBool("DOKLAB_METRICS_ENABLED", true),
		AlertWebhookURL:             getEnv("DOKLAB_ALERT_WEBHOOK_URL", ""),
		FrontendBaseURL:             getEnv("DOKLAB_FRONTEND_BASE_URL", "http://localhost:5173"),

		GitHubClientID:     getEnv("DOKLAB_GITHUB_CLIENT_ID", ""),
		GitHubClientSecret: getEnv("DOKLAB_GITHUB_CLIENT_SECRET", ""),
		GitHubRedirectURL:  getEnv("DOKLAB_GITHUB_REDIRECT_URL", ""),

		PricingAPIEnabled: getEnvAsBool("DOKLAB_PRICING_API_ENABLED", true),

		RuntimeBackend:      getEnv("DOKLAB_RUNTIME", "docker"),
		KubernetesNamespace: getEnv("DOKLAB_K8S_NAMESPACE", "docklab"),
		KubernetesContext:   getEnv("DOKLAB_K8S_CONTEXT", ""),

		IDEEnabled:    getEnvAsBool("DOKLAB_IDE_ENABLED", true),
		IDEImage:      getEnv("DOKLAB_IDE_IMAGE", "codercom/code-server:latest"),
		IDERemotePort: getEnvAsInt("DOKLAB_IDE_REMOTE_PORT", 8443),
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
