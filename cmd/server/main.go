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
	cfg := config.Load()
	logr := logger.New(cfg.AppEnv)

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

	userRepo := repositories.NewPostgresUserRepository(dbPool)
	environmentRepo := repositories.NewPostgresEnvironmentRepository(dbPool)
	operationRepo := repositories.NewPostgresOperationRepository(dbPool)
	authService := services.NewAuthService(userRepo, cfg.JWTSecret, cfg.JWTTTLMinutes)
	localRuntime := services.NewDockerCLIRuntime()
	runtimeResolver := services.NewRuntimeResolver(localRuntime, cfg)
	environmentService := services.NewEnvironmentService(environmentRepo, operationRepo, runtimeResolver)
	terminalService := services.NewTerminalService(environmentRepo, runtimeResolver)
	authHandler := handlers.NewAuthHandler(authService)
	environmentHandler := handlers.NewEnvironmentHandler(environmentService)
	terminalHandler := handlers.NewTerminalHandler(authService, terminalService)
	healthHandler := handlers.NewHealthHandler(dbPool)

	// Background context cancelled on shutdown to cleanly stop background workers.
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	// Sprint 4: cloud drift / orphan reconciliation.
	reconciler := services.NewReconciliationService(environmentRepo, operationRepo, logr)
	reconciler.Start(bgCtx)

	// Sprint 5: auto-sleep idle environments.
	lifecycle := services.NewLifecycleService(environmentRepo, runtimeResolver, cfg.IdleStopMinutes, logr)
	lifecycle.Start(bgCtx)

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://127.0.0.1:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	router.GET("/health", healthHandler.GetHealth)

	api := router.Group("/api/v1")
	api.GET("/environments/:id/terminal/ws", terminalHandler.WebSocket)

	auth := api.Group("/auth")
	auth.POST("/register", authHandler.Register)
	auth.POST("/login", authHandler.Login)

	// Keep protected routes in a separate group so auth concerns stay in middleware.
	protected := api.Group("")
	protected.Use(middleware.JWTAuth(authService))
	protected.GET("/auth/me", authHandler.Me)
	protected.POST("/environments", environmentHandler.Create)
	protected.GET("/environments", environmentHandler.List)
	protected.GET("/environments/:id", environmentHandler.Get)
	protected.POST("/environments/:id/retry-bootstrap", environmentHandler.RetryRemoteBootstrap)
	protected.GET("/environments/:id/remote-health", environmentHandler.GetRemoteHealth)
	protected.GET("/operations/:id", environmentHandler.GetOperation)
	protected.POST("/environments/:id/start", environmentHandler.Start)
	protected.POST("/environments/:id/stop", environmentHandler.Stop)
	protected.POST("/environments/:id/provision", environmentHandler.Provision)
	protected.POST("/environments/:id/destroy-cloud", environmentHandler.DestroyCloud)
	protected.DELETE("/environments/:id", environmentHandler.Delete)

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
