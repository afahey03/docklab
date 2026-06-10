package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/afahey03/docklab/internal/config"
	"github.com/afahey03/docklab/internal/database"
	"github.com/afahey03/docklab/internal/handlers"
	"github.com/afahey03/docklab/internal/middleware"
	"github.com/afahey03/docklab/internal/repositories"
	"github.com/afahey03/docklab/internal/services"
	"github.com/afahey03/docklab/pkg/logger"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	// Optionally hydrate env vars from AWS Secrets Manager before reading config so
	// production deployments don't need plaintext secrets in env files.
	bootstrapLogger := logger.New(os.Getenv("APP_ENV"))
	if applied, err := config.LoadSecretsFromAWS(context.Background()); err != nil {
		bootstrapLogger.Error("failed to load secrets from AWS Secrets Manager", "error", err)
		log.Fatal(err)
	} else if applied > 0 {
		bootstrapLogger.Info("loaded secrets from AWS Secrets Manager", "applied", applied)
	}

	cfg := config.Load()
	logr := logger.New(cfg.AppEnv)

	// Write a base64-delivered SSH key (e.g. from Secrets Manager) to disk so the
	// SSH runtime can use it without a mounted .pem file.
	if err := cfg.MaterializeSSHPrivateKey(); err != nil {
		logr.Error("failed to materialize SSH private key", "error", err)
		log.Fatal(err)
	}

	if cfg.AppEnv == "production" && cfg.JWTSecret == "dev-secret-change-me" {
		logr.Error("JWT_SECRET must be set in production (via env or Secrets Manager)")
		log.Fatal("refusing to start with the default JWT secret in production")
	}

	dbPool, err := database.NewPostgresPool(cfg.DatabaseURL)
	if err != nil {
		logr.Error("failed to connect to database", "error", err)
		log.Fatal(err)
	}
	defer dbPool.Close()

	if err := database.EnsureSchema(dbPool); err != nil {
		logr.Error("failed to initialize database schema", "error", err)
		log.Fatal(err)
	}

	// Repositories
	userRepo := repositories.NewPostgresUserRepository(dbPool)
	environmentRepo := repositories.NewPostgresEnvironmentRepository(dbPool)
	operationRepo := repositories.NewPostgresOperationRepository(dbPool)
	refreshTokenRepo := repositories.NewPostgresRefreshTokenRepository(dbPool)
	usageRepo := repositories.NewPostgresUsageRepository(dbPool)
	settingsRepo := repositories.NewPostgresSettingsRepository(dbPool)
	shareRepo := repositories.NewPostgresShareRepository(dbPool)
	snapshotRepo := repositories.NewPostgresSnapshotRepository(dbPool)

	// Observability
	metrics := services.NewMetrics()
	alerts := services.NewAlertService(cfg.AlertWebhookURL, metrics, logr)

	// Auth
	authService := services.NewAuthService(userRepo, cfg.JWTSecret, cfg.JWTTTLMinutes)
	authService.EnableRefreshTokens(refreshTokenRepo, cfg.RefreshTokenTTLDays)
	githubOAuth := services.NewGitHubOAuthService(
		cfg.GitHubClientID,
		cfg.GitHubClientSecret,
		cfg.GitHubRedirectURL,
		cfg.JWTSecret,
		userRepo,
		authService,
	)

	// Runtimes and core services
	localRuntime := services.NewDockerCLIRuntime()
	runtimeResolver := services.NewRuntimeResolver(localRuntime, cfg)
	if runtimeResolver.LocalBackend() == services.RuntimeBackendKubernetes {
		logr.Info("kubernetes runtime backend enabled", "namespace", cfg.KubernetesNamespace)
	}

	pricingService := services.NewPricingService(cfg.PricingAPIEnabled, logr)
	usageService := services.NewUsageService(usageRepo, settingsRepo, pricingService, alerts, logr)

	environmentService := services.NewEnvironmentService(environmentRepo, operationRepo, runtimeResolver)
	environmentService.SetQuotas(cfg.MaxEnvironmentsPerUser, cfg.MaxConcurrentOpsPerUser)
	environmentService.SetUsageService(usageService)
	environmentService.SetObservability(metrics, alerts)

	shareService := services.NewShareService(environmentRepo, shareRepo, userRepo)
	snapshotService := services.NewSnapshotService(environmentRepo, snapshotRepo, runtimeResolver)
	ideService := services.NewIDEService(runtimeResolver, cfg.IDEEnabled, cfg.IDEImage, cfg.IDERemotePort)

	ec2Client := services.NewAWSEC2InstanceClient()
	cloudLifecycle := services.NewCloudLifecycleService(
		environmentRepo,
		operationRepo,
		runtimeResolver,
		services.NewTerraformCLIRunner(),
		ec2Client,
		cfg.IdleStopMinutes,
		cfg.IdleCloudStopMinutes,
		cfg.IdleCloudTerminateMinutes,
		cfg.CloudIdlePolicyEnabled,
		logr,
	)
	cloudLifecycle.SetUsageService(usageService)
	cloudLifecycle.SetObservability(metrics, alerts)
	environmentService.SetCloudLifecycle(cloudLifecycle)

	terminalService := services.NewTerminalService(environmentRepo, runtimeResolver)
	terminalService.EnableSharing(shareService)
	terminalService.SetMetrics(metrics)

	// Handlers
	authHandler := handlers.NewAuthHandler(authService, githubOAuth, cfg.FrontendBaseURL)
	environmentHandler := handlers.NewEnvironmentHandler(environmentService, shareService)
	lifecycleHandler := handlers.NewLifecycleHandler(cloudLifecycle)
	terminalHandler := handlers.NewTerminalHandler(authService, terminalService)
	healthHandler := handlers.NewHealthHandler(dbPool)
	usageHandler := handlers.NewUsageHandler(usageService, pricingService)
	snapshotHandler := handlers.NewSnapshotHandler(snapshotService, environmentHandler)
	shareHandler := handlers.NewShareHandler(shareService, environmentHandler)
	ideHandler := handlers.NewIDEHandler(ideService, environmentService, environmentHandler)

	// Background context cancelled on shutdown to cleanly stop background workers.
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	// Cloud drift / orphan reconciliation.
	reconciler := services.NewReconciliationService(environmentRepo, operationRepo, ec2Client, logr)
	reconciler.SetObservability(alerts, usageService)
	reconciler.Start(bgCtx)

	// Auto-sleep idle workspace containers and idle cloud resources.
	workspaceLifecycle := services.NewLifecycleService(environmentRepo, runtimeResolver, cfg.IdleStopMinutes, logr)
	workspaceLifecycle.SetMetrics(metrics)
	cloudLifecycle.Start(bgCtx, workspaceLifecycle)

	// Budget alerts for users with a monthly cost budget configured.
	usageService.StartBudgetWatcher(bgCtx, 15*time.Minute)

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())
	if cfg.MetricsEnabled {
		router.Use(middleware.Metrics(metrics))
	}
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://127.0.0.1:5173", cfg.FrontendBaseURL},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	router.GET("/health", healthHandler.GetHealth)
	if cfg.MetricsEnabled {
		router.GET("/metrics", gin.WrapH(metrics.Handler()))
	}

	api := router.Group("/api/v1")
	if cfg.RateLimitEnabled {
		api.Use(middleware.RateLimit(middleware.NewRateLimiter(cfg.APIRateLimitPerMinute), "api"))
	}
	api.GET("/environments/:id/terminal/ws", terminalHandler.WebSocket)

	auth := api.Group("/auth")
	if cfg.RateLimitEnabled {
		auth.Use(middleware.RateLimit(middleware.NewRateLimiter(cfg.AuthRateLimitPerMinute), "auth"))
	}
	auth.POST("/register", authHandler.Register)
	auth.POST("/login", authHandler.Login)
	auth.POST("/refresh", authHandler.Refresh)
	auth.POST("/logout", authHandler.Logout)
	auth.GET("/github/login", authHandler.GitHubLogin)
	auth.GET("/github/callback", authHandler.GitHubCallback)

	// Keep protected routes in a separate group so auth concerns stay in middleware.
	protected := api.Group("")
	protected.Use(middleware.JWTAuth(authService))
	protected.GET("/auth/me", authHandler.Me)
	protected.GET("/lifecycle-policy", lifecycleHandler.GetPolicy)
	protected.GET("/templates", environmentHandler.ListTemplates)
	protected.GET("/usage", usageHandler.GetUsage)
	protected.GET("/billing/summary", usageHandler.GetBillingSummary)
	protected.GET("/billing/budget", usageHandler.GetBudget)
	protected.PUT("/billing/budget", usageHandler.UpdateBudget)
	protected.GET("/pricing", usageHandler.GetPricing)
	protected.POST("/environments", environmentHandler.Create)
	protected.GET("/environments", environmentHandler.List)
	protected.GET("/environments/:id", environmentHandler.Get)
	protected.POST("/environments/:id/retry-bootstrap", environmentHandler.RetryRemoteBootstrap)
	protected.GET("/environments/:id/remote-health", environmentHandler.GetRemoteHealth)
	protected.GET("/operations/:id", environmentHandler.GetOperation)
	protected.POST("/environments/:id/start", environmentHandler.Start)
	protected.POST("/environments/:id/stop", environmentHandler.Stop)
	protected.DELETE("/environments/:id", environmentHandler.Delete)

	// Snapshots (workspace persistence)
	protected.POST("/environments/:id/snapshots", snapshotHandler.Create)
	protected.GET("/environments/:id/snapshots", snapshotHandler.List)
	protected.POST("/environments/:id/snapshots/:snapshotId/restore", snapshotHandler.Restore)
	protected.DELETE("/environments/:id/snapshots/:snapshotId", snapshotHandler.Delete)

	// Collaboration (environment sharing)
	protected.POST("/environments/:id/shares", shareHandler.Create)
	protected.GET("/environments/:id/shares", shareHandler.List)
	protected.DELETE("/environments/:id/shares/:email", shareHandler.Delete)

	// Browser IDE (code-server sidecar)
	protected.POST("/environments/:id/ide/start", ideHandler.Start)
	protected.POST("/environments/:id/ide/stop", ideHandler.Stop)
	protected.GET("/environments/:id/ide", ideHandler.Status)

	// Provisioning endpoints get a stricter rate limit on top of JWT auth.
	provisioning := api.Group("")
	provisioning.Use(middleware.JWTAuth(authService))
	if cfg.RateLimitEnabled {
		provisioning.Use(middleware.RateLimit(middleware.NewRateLimiter(cfg.ProvisionRateLimitPerMinute), "provision"))
	}
	provisioning.POST("/environments/:id/provision", environmentHandler.Provision)
	provisioning.POST("/environments/:id/destroy-cloud", environmentHandler.DestroyCloud)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logr.Info("starting backend server", "port", cfg.Port, "env", cfg.AppEnv)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logr.Error("server error", "error", err)
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	// Stop background workers before shutting down the HTTP server.
	bgCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logr.Info("shutting down server")
	if err := server.Shutdown(ctx); err != nil {
		logr.Error("shutdown failed", "error", err)
	}
}
