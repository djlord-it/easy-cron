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
}

// Load reads configuration from environment variables.
// It applies defaults for optional values.
func Load() Config {
	cfg := Config{
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		RedisAddr:       os.Getenv("REDIS_ADDR"),
		HTTPAddr:        os.Getenv("HTTP_ADDR"),
		TickIntervalStr: os.Getenv("TICK_INTERVAL"),
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

	// Parse tick interval (validation happens separately)
	if d, err := time.ParseDuration(cfg.TickIntervalStr); err == nil {
		cfg.TickInterval = d
	}

	return cfg
}

// MaskedJSON returns the configuration as JSON with secrets masked.
func (c Config) MaskedJSON() ([]byte, error) {
	masked := struct {
		DatabaseURL  string `json:"database_url"`
		RedisAddr    string `json:"redis_addr,omitempty"`
		HTTPAddr     string `json:"http_addr"`
		TickInterval string `json:"tick_interval"`
	}{
		DatabaseURL:  maskSecret(c.DatabaseURL),
		RedisAddr:    c.RedisAddr,
		HTTPAddr:     c.HTTPAddr,
		TickInterval: c.TickIntervalStr,
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
