package config

import (
	"encoding/json"
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

	// MetricsEnabled enables Prometheus metrics.
	// Default: false
	// Environment variable: METRICS_ENABLED
	MetricsEnabled bool `json:"metrics_enabled"`

	// MetricsPath is the HTTP path for the metrics endpoint.
	// Default: "/metrics"
	// Environment variable: METRICS_PATH
	MetricsPath string `json:"metrics_path"`

	// MetricsPort is the port for the metrics HTTP server.
	// Default: "9090"
	// Environment variable: METRICS_PORT
	MetricsPort string `json:"metrics_port"`

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
	// Default: 10m
	// Environment variable: RECONCILE_THRESHOLD
	ReconcileThreshold time.Duration `json:"-"`

	// ReconcileThresholdStr is the string representation for JSON output.
	ReconcileThresholdStr string `json:"reconcile_threshold"`

	// ReconcileBatchSize is the max orphans processed per cycle.
	// Default: 100
	// Environment variable: RECONCILE_BATCH_SIZE
	ReconcileBatchSize int `json:"reconcile_batch_size"`
}

// Load reads configuration from environment variables.
// It applies defaults for optional values.
func Load() Config {
	cfg := Config{
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		RedisAddr:             os.Getenv("REDIS_ADDR"),
		HTTPAddr:              os.Getenv("HTTP_ADDR"),
		TickIntervalStr:       os.Getenv("TICK_INTERVAL"),
		MetricsEnabled:        os.Getenv("METRICS_ENABLED") == "true",
		MetricsPath:           os.Getenv("METRICS_PATH"),
		MetricsPort:           os.Getenv("METRICS_PORT"),
		ReconcileEnabled:      os.Getenv("RECONCILE_ENABLED") == "true",
		ReconcileIntervalStr:  os.Getenv("RECONCILE_INTERVAL"),
		ReconcileThresholdStr: os.Getenv("RECONCILE_THRESHOLD"),
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
	if cfg.MetricsPath == "" {
		cfg.MetricsPath = "/metrics"
	}
	if cfg.MetricsPort == "" {
		cfg.MetricsPort = "9090"
	}
	if cfg.ReconcileIntervalStr == "" {
		cfg.ReconcileIntervalStr = "5m"
	}
	if cfg.ReconcileThresholdStr == "" {
		cfg.ReconcileThresholdStr = "10m"
	}

	// Parse durations (validation happens separately)
	if d, err := time.ParseDuration(cfg.TickIntervalStr); err == nil {
		cfg.TickInterval = d
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
		DatabaseURL        string `json:"database_url"`
		RedisAddr          string `json:"redis_addr,omitempty"`
		HTTPAddr           string `json:"http_addr"`
		TickInterval       string `json:"tick_interval"`
		MetricsEnabled     bool   `json:"metrics_enabled"`
		MetricsPath        string `json:"metrics_path"`
		MetricsPort        string `json:"metrics_port"`
		ReconcileEnabled   bool   `json:"reconcile_enabled"`
		ReconcileInterval  string `json:"reconcile_interval"`
		ReconcileThreshold string `json:"reconcile_threshold"`
		ReconcileBatchSize int    `json:"reconcile_batch_size"`
	}{
		DatabaseURL:        maskSecret(c.DatabaseURL),
		RedisAddr:          c.RedisAddr,
		HTTPAddr:           c.HTTPAddr,
		TickInterval:       c.TickIntervalStr,
		MetricsEnabled:     c.MetricsEnabled,
		MetricsPath:        c.MetricsPath,
		MetricsPort:        c.MetricsPort,
		ReconcileEnabled:   c.ReconcileEnabled,
		ReconcileInterval:  c.ReconcileIntervalStr,
		ReconcileThreshold: c.ReconcileThresholdStr,
		ReconcileBatchSize: c.ReconcileBatchSize,
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
