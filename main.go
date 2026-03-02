package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/penguinpowernz/timeflux/auth"
	"github.com/penguinpowernz/timeflux/config"
	"github.com/penguinpowernz/timeflux/metrics"
	"github.com/penguinpowernz/timeflux/query"
	"github.com/penguinpowernz/timeflux/schema"
	"github.com/penguinpowernz/timeflux/usercli"
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

	// Check if this is a user management command
	if len(flag.Args()) > 0 {
		if err := handleUserCommand(cfg, flag.Args()); err != nil {
			log.Fatalf("Command failed: %v", err)
		}
		return
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

	// Initialize authentication system
	var userManager *auth.UserManager
	if cfg.Auth.Enabled {
		log.Printf("Initializing authentication system...")
		userManager = auth.NewUserManager(pool)
		if err := userManager.InitializeSchema(ctx); err != nil {
			log.Fatalf("Failed to initialize auth schema: %v", err)
		}
		log.Printf("Authentication enabled")
	} else {
		log.Printf("Authentication disabled")
	}

	// Create HTTP handlers
	writeHandler := write.NewHandler(pool, schemaManager, cfg.Database.AutoCreateDatabases)
	queryHandler := query.NewHandler(pool)

	// Initialize WAL if enabled
	var walBuffer *write.WALBuffer
	if cfg.WAL.Enabled {
		log.Printf("Initializing WAL buffer...")
		walCfg := write.WALConfig{
			Path:             cfg.WAL.Path,
			NumWorkers:       cfg.WAL.NumWorkers,
			FsyncIntervalMs:  cfg.WAL.FsyncIntervalMs,
			SegmentSizeMB:    cfg.WAL.SegmentSizeMB,
			SegmentCacheSize: cfg.WAL.SegmentCacheSize,
			NoSync:           cfg.WAL.NoSync,
		}

		var err error
		walBuffer, err = write.NewWALBuffer(walCfg, pool, schemaManager)
		if err != nil {
			log.Fatalf("Failed to initialize WAL: %v", err)
		}

		// Recover from WAL on startup
		if err := walBuffer.RecoverFromWAL(); err != nil {
			log.Fatalf("Failed to recover from WAL: %v", err)
		}

		// Attach WAL to write handler
		writeHandler.SetWALBuffer(walBuffer)
		log.Printf("WAL enabled: %s", cfg.WAL.Path)
	} else {
		log.Printf("WAL disabled, using synchronous writes")
	}

	// Setup Gin router
	if cfg.Logging.Level != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Add middleware
	router.Use(gin.Recovery())
	router.Use(ginLogger())

	// Add authentication middleware if enabled
	if cfg.Auth.Enabled {
		router.Use(auth.Middleware(userManager, true))
	}

	// Setup routes with authorization
	router.POST("/write", auth.RequirePermission(userManager, cfg.Auth.Enabled, true), writeHandler.Handle)
	router.GET("/query", auth.RequirePermission(userManager, cfg.Auth.Enabled, false), queryHandler.Handle)
	router.POST("/query", auth.RequirePermission(userManager, cfg.Auth.Enabled, false), queryHandler.Handle)

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

	// Shutdown WAL buffer if enabled
	if walBuffer != nil {
		log.Printf("Shutting down WAL buffer...")
		if err := walBuffer.Shutdown(); err != nil {
			log.Printf("WAL shutdown error: %v", err)
		}
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

// handleUserCommand processes user management commands
func handleUserCommand(cfg *config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command specified")
	}

	userCmd := usercli.NewUserCommand(cfg)

	switch args[0] {
	case "user:add":
		if len(args) < 2 {
			return fmt.Errorf("usage: user:add <username> [password]")
		}
		username := args[1]
		password := ""
		if len(args) >= 3 {
			password = args[2]
		}
		return userCmd.AddUser(username, password)

	case "user:delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: user:delete <username>")
		}
		return userCmd.DeleteUser(args[1])

	case "user:reset-password":
		if len(args) < 2 {
			return fmt.Errorf("usage: user:reset-password <username> [new-password]")
		}
		username := args[1]
		password := ""
		if len(args) >= 3 {
			password = args[2]
		}
		return userCmd.ResetPassword(username, password)

	case "user:grant":
		if len(args) < 3 {
			return fmt.Errorf("usage: user:grant <username> <database[.measurement]:r|w|rw>")
		}
		username := args[1]
		database, measurement, canRead, canWrite, err := usercli.ParsePermission(args[2])
		if err != nil {
			return err
		}
		return userCmd.GrantPermission(username, database, measurement, canRead, canWrite)

	case "user:revoke":
		if len(args) < 3 {
			return fmt.Errorf("usage: user:revoke <username> <database[.measurement]>")
		}
		username := args[1]
		target := args[2]
		database := target
		measurement := ""
		if strings.Contains(target, ".") {
			parts := strings.SplitN(target, ".", 2)
			database = parts[0]
			measurement = parts[1]
			if measurement == "*" {
				measurement = ""
			}
		}
		return userCmd.RevokePermission(username, database, measurement)

	case "user:list":
		return userCmd.ListUsers()

	case "user:show":
		if len(args) < 2 {
			return fmt.Errorf("usage: user:show <username>")
		}
		return userCmd.ShowUser(args[1])

	default:
		return fmt.Errorf("unknown command: %s\nAvailable commands:\n  user:add <username> [password]\n  user:delete <username>\n  user:reset-password <username> [new-password]\n  user:grant <username> <database[.measurement]:r|w|rw>\n  user:grant <username> *[.measurement]:r|w|rw  (wildcard for all databases)\n  user:revoke <username> <database[.measurement]>\n  user:list\n  user:show <username>", args[0])
	}
}
