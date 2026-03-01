package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/penguinpowernz/timeflux/config"
	"github.com/penguinpowernz/timeflux/metrics"
	"github.com/penguinpowernz/timeflux/query"
	"github.com/penguinpowernz/timeflux/schema"
	"github.com/penguinpowernz/timeflux/write"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting Timeflux - InfluxDB v1 to TimescaleDB facade")
	log.Printf("Server will listen on port %d", cfg.Server.Port)

	// Create database connection pool
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.Database.ConnectionString())
	if err != nil {
		log.Fatalf("Failed to create database pool: %v", err)
	}
	defer pool.Close()

	// Test database connection
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Printf("Connected to TimescaleDB at %s:%d", cfg.Database.Host, cfg.Database.Port)

	// Create schema manager
	schemaManager := schema.NewSchemaManager(pool)

	// Load existing schemas
	log.Printf("Loading existing schemas...")
	if err := schemaManager.LoadExistingSchemas(ctx); err != nil {
		log.Printf("Warning: failed to load existing schemas: %v", err)
	} else {
		log.Printf("Existing schemas loaded successfully")
	}

	// Create HTTP handlers
	writeHandler := write.NewHandler(pool, schemaManager)
	queryHandler := query.NewHandler(pool)

	// Setup Gin router
	if cfg.Logging.Level != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Add middleware
	router.Use(gin.Recovery())
	router.Use(ginLogger())

	// Setup routes
	router.POST("/write", writeHandler.Handle)
	router.GET("/query", queryHandler.Handle)
	router.POST("/query", queryHandler.Handle)

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": "timeflux (CLI not supported)"})
	})

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})

	router.GET("/metrics", func(c *gin.Context) {
		c.JSON(http.StatusOK, metrics.Global().Snapshot())
	})

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Server listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Printf("Shutting down server...")

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	// Shutdown schema manager (wait for background index creation to finish)
	log.Printf("Waiting for background index creation to complete...")
	schemaManager.Shutdown()

	log.Printf("Server stopped")
}

// ginLogger is a custom Gin logger middleware
func ginLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Log after request
		duration := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method

		if query != "" {
			path = path + "?" + query
		}

		log.Printf("%s %s %d %v", method, path, status, duration)
	}
}
