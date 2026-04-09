// cmd/updater/main.go
//
// Metapus Updater Agent — sidecar service for Docker-based system updates.
//
// This service orchestrates blue-green container deployments:
//   1. Pulls new Docker image from GHCR
//   2. Starts new container in the same Docker network
//   3. Waits for health check
//   4. Switches traffic via Docker network alias swap
//   5. Triggers DB migration on the new server
//   6. Cleans up old container
//
// Communication with the main Metapus server is via /internal/* HTTP endpoints
// (no auth — internal network trust).
//
// State is persisted to disk (WAL) for crash recovery.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/pkg/logger"
)

func main() {
	// Load configuration
	cfg, err := LoadConfig()
	if err != nil {
		panic("load config: " + err.Error())
	}

	// Initialize logger
	log, err := logger.New(logger.Config{
		Level:       cfg.LogLevel,
		Development: cfg.LogLevel == "debug",
	})
	if err != nil {
		panic("init logger: " + err.Error())
	}
	log.Info("starting metapus updater agent",
		"port", cfg.Port,
		"server_url", cfg.ServerURL,
		"tenant_id", cfg.TenantID,
		"registry", cfg.RegistryImage,
	)

	// Initialize Docker client
	docker, err := NewDockerClient()
	if err != nil {
		log.Error("failed to connect to Docker", "error", err)
		os.Exit(1)
	}
	defer func() { _ = docker.Close() }()

	// Initialize state store (loads WAL from disk)
	state, err := NewStateStore(cfg.StateFilePath)
	if err != nil {
		log.Error("failed to initialize state store", "error", err)
		os.Exit(1)
	}

	// Initialize registry checker for automatic update discovery
	registry := NewRegistryChecker(cfg.RegistryImage, cfg.RegistryToken)

	// Create orchestrator
	orch := NewOrchestrator(cfg, docker, state, registry, log)

	// Recover from interrupted updates (WAL replay)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	orch.RecoverIfNeeded(ctx)

	// Start background registry polling (discovers new tags)
	if cfg.CheckInterval > 0 {
		log.Info("starting registry checker",
			"interval", cfg.CheckInterval,
			"image", cfg.RegistryImage,
		)
		registry.RunBackground(ctx, cfg.CheckInterval, func() string {
			info, err := orch.CheckAvailable(ctx)
			if err != nil || info == nil {
				return "unknown"
			}
			return info.CurrentVersion
		})
	}

	// Set up HTTP server
	if cfg.LogLevel != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())

	// CORS for frontend access
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// Register API routes
	api := NewAPIHandler(orch, log)
	api.RegisterRoutes(router)

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Start HTTP server
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		log.Info("updater API listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down updater agent")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("server shutdown error", "error", err)
	}

	log.Info("updater agent stopped")
}
