package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Logging  LoggingConfig  `yaml:"logging"`
	WAL      WALConfig      `yaml:"wal"`
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port int `yaml:"port"`
}

// DatabaseConfig holds PostgreSQL/TimescaleDB connection configuration
type DatabaseConfig struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	Database        string `yaml:"database"`
	User            string `yaml:"user"`
	Password        string `yaml:"password"`
	PoolSize        int    `yaml:"pool_size"`
	MaxConnLifetime int    `yaml:"max_conn_lifetime"` // in seconds
	MaxConnIdleTime int    `yaml:"max_conn_idle_time"` // in seconds
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// WALConfig holds write-ahead log configuration
type WALConfig struct {
	Enabled          bool   `yaml:"enabled"`
	Path             string `yaml:"path"`
	NumWorkers       int    `yaml:"num_workers"`
	FsyncIntervalMs  int    `yaml:"fsync_interval_ms"`
	SegmentSizeMB    int    `yaml:"segment_size_mb"`
	SegmentCacheSize int    `yaml:"segment_cache_size"`
	NoSync           bool   `yaml:"no_sync"` // disable fsync (for development/testing only)
}

// Load reads and parses the configuration file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8086
	}
	if cfg.Database.Port == 0 {
		cfg.Database.Port = 5432
	}
	if cfg.Database.PoolSize == 0 {
		cfg.Database.PoolSize = 32
	}
	if cfg.Database.MaxConnLifetime == 0 {
		cfg.Database.MaxConnLifetime = 3600 // 1 hour
	}
	if cfg.Database.MaxConnIdleTime == 0 {
		cfg.Database.MaxConnIdleTime = 300 // 5 minutes
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}
	if cfg.WAL.Path == "" {
		cfg.WAL.Path = "/tmp/timeflux/wal"
	}
	if cfg.WAL.NumWorkers == 0 {
		cfg.WAL.NumWorkers = 8
	}
	if cfg.WAL.FsyncIntervalMs == 0 {
		cfg.WAL.FsyncIntervalMs = 100
	}
	if cfg.WAL.SegmentSizeMB == 0 {
		cfg.WAL.SegmentSizeMB = 64
	}
	if cfg.WAL.SegmentCacheSize == 0 {
		cfg.WAL.SegmentCacheSize = 2
	}

	return &cfg, nil
}

// ConnectionString returns the PostgreSQL connection string
func (c *DatabaseConfig) ConnectionString() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?pool_max_conns=%d&pool_max_conn_lifetime=%ds&pool_max_conn_idle_time=%ds",
		c.User,
		c.Password,
		c.Host,
		c.Port,
		c.Database,
		c.PoolSize,
		c.MaxConnLifetime,
		c.MaxConnIdleTime,
	)
}
