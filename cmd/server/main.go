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
	"github.com/afahey03/docklab/internal/services"
	"github.com/afahey03/docklab/pkg/logger"
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

	authService := services.NewAuthService(cfg.JWTSecret, cfg.JWTTTLMinutes)
	authHandler := handlers.NewAuthHandler(authService)
	healthHandler := handlers.NewHealthHandler(dbPool)

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())

	router.GET("/health", healthHandler.GetHealth)

	api := router.Group("/api/v1")
	auth := api.Group("/auth")
	auth.POST("/login", authHandler.Login)

	// Keep protected routes in a separate group so auth concerns stay in middleware.
	protected := api.Group("")
	protected.Use(middleware.JWTAuth(authService))
	protected.GET("/auth/me", authHandler.Me)

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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logr.Info("shutting down server")
	if err := server.Shutdown(ctx); err != nil {
		logr.Error("shutdown failed", "error", err)
	}
}
