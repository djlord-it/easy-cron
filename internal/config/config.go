package config

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

// Config holds all configuration for the easycron application.
// All values are loaded from environment variables.
type Config struct {
	// DatabaseURL is the PostgreSQL connection string.
	// Required. Environment variable: DATABASE_URL
	DatabaseURL string `json:"database_url"`

	// RedisAddr is the Redis server address for analytics.
	// Optional. If empty, analytics is disabled.
	// Environment variable: REDIS_ADDR
	RedisAddr string `json:"redis_addr,omitempty"`

	// HTTPAddr is the address for the HTTP server to listen on.
	// Default: ":8080"
	// Environment variable: HTTP_ADDR
	HTTPAddr string `json:"http_addr"`

	// TickInterval is the interval between scheduler ticks.
	// Default: 30s
	// Environment variable: TICK_INTERVAL
	TickInterval time.Duration `json:"-"`

	// TickIntervalStr is the string representation for JSON output.
	TickIntervalStr string `json:"tick_interval"`

	// DBOpTimeout is the timeout for individual database operations.
	// Default: 5s
	// Environment variable: DB_OP_TIMEOUT
	DBOpTimeout time.Duration `json:"-"`

	// DBOpTimeoutStr is the string representation for JSON output.
	DBOpTimeoutStr string `json:"db_op_timeout"`

	// DBMaxOpenConns is the maximum number of open connections to the database.
	// Default: 25
	// Environment variable: DB_MAX_OPEN_CONNS
	DBMaxOpenConns int `json:"db_max_open_conns"`

	// DBMaxIdleConns is the maximum number of idle connections in the pool.
	// Default: 5
	// Environment variable: DB_MAX_IDLE_CONNS
	DBMaxIdleConns int `json:"db_max_idle_conns"`

	// DBConnMaxLifetime is the maximum lifetime of a connection.
	// Default: 30m
	// Environment variable: DB_CONN_MAX_LIFETIME
	DBConnMaxLifetime time.Duration `json:"-"`

	// DBConnMaxLifetimeStr is the string representation for JSON output.
	DBConnMaxLifetimeStr string `json:"db_conn_max_lifetime"`

	// DBConnMaxIdleTime is the maximum idle time of a connection.
	// Default: 5m
	// Environment variable: DB_CONN_MAX_IDLE_TIME
	DBConnMaxIdleTime time.Duration `json:"-"`

	// DBConnMaxIdleTimeStr is the string representation for JSON output.
	DBConnMaxIdleTimeStr string `json:"db_conn_max_idle_time"`

	// HTTPShutdownTimeout is the timeout for graceful HTTP server shutdown.
	// Default: 10s
	// Environment variable: HTTP_SHUTDOWN_TIMEOUT
	HTTPShutdownTimeout time.Duration `json:"-"`

	// HTTPShutdownTimeoutStr is the string representation for JSON output.
	HTTPShutdownTimeoutStr string `json:"http_shutdown_timeout"`

	// DispatcherDrainTimeout is the timeout for dispatcher to drain events on shutdown.
	// Default: 30s
	// Environment variable: DISPATCHER_DRAIN_TIMEOUT
	DispatcherDrainTimeout time.Duration `json:"-"`

	// DispatcherDrainTimeoutStr is the string representation for JSON output.
	DispatcherDrainTimeoutStr string `json:"dispatcher_drain_timeout"`

	// MetricsEnabled enables Prometheus metrics.
	// Default: false
	// Environment variable: METRICS_ENABLED
	MetricsEnabled bool `json:"metrics_enabled"`

	// MetricsPath is the HTTP path for the metrics endpoint.
	// Default: "/metrics"
	// Environment variable: METRICS_PATH
	MetricsPath string `json:"metrics_path"`

	// ReconcileEnabled enables the orphan execution reconciler.
	// Default: false
	// Environment variable: RECONCILE_ENABLED
	ReconcileEnabled bool `json:"reconcile_enabled"`

	// ReconcileInterval is how often the reconciler scans for orphans.
	// Default: 5m
	// Environment variable: RECONCILE_INTERVAL
	ReconcileInterval time.Duration `json:"-"`

	// ReconcileIntervalStr is the string representation for JSON output.
	ReconcileIntervalStr string `json:"reconcile_interval"`

	// ReconcileThreshold is the age after which an emitted execution is orphaned.
	// Must exceed the dispatcher's maximum retry window (currently 12m30s).
	// Default: 15m
	// Environment variable: RECONCILE_THRESHOLD
	ReconcileThreshold time.Duration `json:"-"`

	// ReconcileThresholdStr is the string representation for JSON output.
	ReconcileThresholdStr string `json:"reconcile_threshold"`

	// ReconcileBatchSize is the max orphans processed per cycle.
	// Default: 100
	// Environment variable: RECONCILE_BATCH_SIZE
	ReconcileBatchSize int `json:"reconcile_batch_size"`

	// EventBusBufferSize is the capacity of the in-memory event channel
	// between the scheduler and dispatcher.
	// Default: 100
	// Environment variable: EVENTBUS_BUFFER_SIZE
	EventBusBufferSize int `json:"eventbus_buffer_size"`
}

// Load reads configuration from environment variables.
// It applies defaults for optional values.
func Load() Config {
	cfg := Config{
		DatabaseURL:               os.Getenv("DATABASE_URL"),
		RedisAddr:                 os.Getenv("REDIS_ADDR"),
		HTTPAddr:                  os.Getenv("HTTP_ADDR"),
		TickIntervalStr:           os.Getenv("TICK_INTERVAL"),
		DBOpTimeoutStr:            os.Getenv("DB_OP_TIMEOUT"),
		DBConnMaxLifetimeStr:      os.Getenv("DB_CONN_MAX_LIFETIME"),
		DBConnMaxIdleTimeStr:      os.Getenv("DB_CONN_MAX_IDLE_TIME"),
		HTTPShutdownTimeoutStr:    os.Getenv("HTTP_SHUTDOWN_TIMEOUT"),
		DispatcherDrainTimeoutStr: os.Getenv("DISPATCHER_DRAIN_TIMEOUT"),
		MetricsEnabled:            os.Getenv("METRICS_ENABLED") == "true",
		MetricsPath:               os.Getenv("METRICS_PATH"),
		ReconcileEnabled:          os.Getenv("RECONCILE_ENABLED") == "true",
		ReconcileIntervalStr:      os.Getenv("RECONCILE_INTERVAL"),
		ReconcileThresholdStr:     os.Getenv("RECONCILE_THRESHOLD"),
	}

	// Parse batch size (default 100)
	if batchStr := os.Getenv("RECONCILE_BATCH_SIZE"); batchStr != "" {
		if batch, err := parseInt(batchStr); err == nil && batch > 0 {
			cfg.ReconcileBatchSize = batch
		}
	}
	if cfg.ReconcileBatchSize == 0 {
		cfg.ReconcileBatchSize = 100
	}

	// Parse event bus buffer size (default 100)
	if bufStr := os.Getenv("EVENTBUS_BUFFER_SIZE"); bufStr != "" {
		if n, err := parseInt(bufStr); err == nil && n > 0 {
			cfg.EventBusBufferSize = n
		} else {
			log.Printf("config: invalid EVENTBUS_BUFFER_SIZE %q (must be a positive integer), using default 100", bufStr)
		}
	}
	if cfg.EventBusBufferSize == 0 {
		cfg.EventBusBufferSize = 100
	}

	// Parse DB pool sizes
	if maxOpenStr := os.Getenv("DB_MAX_OPEN_CONNS"); maxOpenStr != "" {
		if n, err := parseInt(maxOpenStr); err == nil && n > 0 {
			cfg.DBMaxOpenConns = n
		}
	}
	if cfg.DBMaxOpenConns == 0 {
		cfg.DBMaxOpenConns = 25
	}

	if maxIdleStr := os.Getenv("DB_MAX_IDLE_CONNS"); maxIdleStr != "" {
		if n, err := parseInt(maxIdleStr); err == nil && n > 0 {
			cfg.DBMaxIdleConns = n
		}
	}
	if cfg.DBMaxIdleConns == 0 {
		cfg.DBMaxIdleConns = 5
	}

	// Apply defaults
	// Support Railway's PORT variable as fallback for HTTP_ADDR
	if cfg.HTTPAddr == "" {
		if port := os.Getenv("PORT"); port != "" {
			cfg.HTTPAddr = ":" + port
		} else {
			cfg.HTTPAddr = ":8080"
		}
	}
	if cfg.TickIntervalStr == "" {
		cfg.TickIntervalStr = "30s"
	}
	if cfg.DBOpTimeoutStr == "" {
		cfg.DBOpTimeoutStr = "5s"
	}
	if cfg.DBConnMaxLifetimeStr == "" {
		cfg.DBConnMaxLifetimeStr = "30m"
	}
	if cfg.DBConnMaxIdleTimeStr == "" {
		cfg.DBConnMaxIdleTimeStr = "5m"
	}
	if cfg.HTTPShutdownTimeoutStr == "" {
		cfg.HTTPShutdownTimeoutStr = "10s"
	}
	if cfg.DispatcherDrainTimeoutStr == "" {
		cfg.DispatcherDrainTimeoutStr = "30s"
	}
	if cfg.MetricsPath == "" {
		cfg.MetricsPath = "/metrics"
	}
	if cfg.ReconcileIntervalStr == "" {
		cfg.ReconcileIntervalStr = "5m"
	}
	if cfg.ReconcileThresholdStr == "" {
		cfg.ReconcileThresholdStr = "15m"
	}

	// Parse durations (validation happens separately)
	if d, err := time.ParseDuration(cfg.TickIntervalStr); err == nil {
		cfg.TickInterval = d
	}
	if d, err := time.ParseDuration(cfg.DBOpTimeoutStr); err == nil {
		cfg.DBOpTimeout = d
	}
	if d, err := time.ParseDuration(cfg.DBConnMaxLifetimeStr); err == nil {
		cfg.DBConnMaxLifetime = d
	}
	if d, err := time.ParseDuration(cfg.DBConnMaxIdleTimeStr); err == nil {
		cfg.DBConnMaxIdleTime = d
	}
	if d, err := time.ParseDuration(cfg.HTTPShutdownTimeoutStr); err == nil {
		cfg.HTTPShutdownTimeout = d
	}
	if d, err := time.ParseDuration(cfg.DispatcherDrainTimeoutStr); err == nil {
		cfg.DispatcherDrainTimeout = d
	}
	if d, err := time.ParseDuration(cfg.ReconcileIntervalStr); err == nil {
		cfg.ReconcileInterval = d
	}
	if d, err := time.ParseDuration(cfg.ReconcileThresholdStr); err == nil {
		cfg.ReconcileThreshold = d
	}

	return cfg
}

// parseInt parses a string as an integer.
func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// MaskedJSON returns the configuration as JSON with secrets masked.
func (c Config) MaskedJSON() ([]byte, error) {
	masked := struct {
		DatabaseURL            string `json:"database_url"`
		RedisAddr              string `json:"redis_addr,omitempty"`
		HTTPAddr               string `json:"http_addr"`
		TickInterval           string `json:"tick_interval"`
		DBOpTimeout            string `json:"db_op_timeout"`
		DBMaxOpenConns         int    `json:"db_max_open_conns"`
		DBMaxIdleConns         int    `json:"db_max_idle_conns"`
		DBConnMaxLifetime      string `json:"db_conn_max_lifetime"`
		DBConnMaxIdleTime      string `json:"db_conn_max_idle_time"`
		HTTPShutdownTimeout    string `json:"http_shutdown_timeout"`
		DispatcherDrainTimeout string `json:"dispatcher_drain_timeout"`
		MetricsEnabled         bool   `json:"metrics_enabled"`
		MetricsPath            string `json:"metrics_path"`
		ReconcileEnabled       bool   `json:"reconcile_enabled"`
		ReconcileInterval      string `json:"reconcile_interval"`
		ReconcileThreshold     string `json:"reconcile_threshold"`
		ReconcileBatchSize     int    `json:"reconcile_batch_size"`
		EventBusBufferSize     int    `json:"eventbus_buffer_size"`
	}{
		DatabaseURL:            maskSecret(c.DatabaseURL),
		RedisAddr:              c.RedisAddr,
		HTTPAddr:               c.HTTPAddr,
		TickInterval:           c.TickIntervalStr,
		DBOpTimeout:            c.DBOpTimeoutStr,
		DBMaxOpenConns:         c.DBMaxOpenConns,
		DBMaxIdleConns:         c.DBMaxIdleConns,
		DBConnMaxLifetime:      c.DBConnMaxLifetimeStr,
		DBConnMaxIdleTime:      c.DBConnMaxIdleTimeStr,
		HTTPShutdownTimeout:    c.HTTPShutdownTimeoutStr,
		DispatcherDrainTimeout: c.DispatcherDrainTimeoutStr,
		MetricsEnabled:         c.MetricsEnabled,
		MetricsPath:            c.MetricsPath,
		ReconcileEnabled:       c.ReconcileEnabled,
		ReconcileInterval:      c.ReconcileIntervalStr,
		ReconcileThreshold:     c.ReconcileThresholdStr,
		ReconcileBatchSize:     c.ReconcileBatchSize,
		EventBusBufferSize:     c.EventBusBufferSize,
	}
	return json.MarshalIndent(masked, "", "  ")
}

// maskSecret masks a secret value, preserving only the scheme if present.
func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	// For connection strings like postgres://user:pass@host/db,
	// show postgres://*** to indicate format without exposing credentials.
	for _, scheme := range []string{"postgres://", "postgresql://"} {
		if len(s) >= len(scheme) && s[:len(scheme)] == scheme {
			return scheme + "***"
		}
	}
	return "***"
}
