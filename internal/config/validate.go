package config

import (
	"fmt"
	"time"
)

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	if len(e) == 1 {
		return e[0].Error()
	}
	msg := fmt.Sprintf("%d validation errors:", len(e))
	for _, err := range e {
		msg += "\n  - " + err.Error()
	}
	return msg
}

// Validate checks the configuration for errors.
// Returns nil if valid, or ValidationErrors if invalid.
func Validate(cfg Config) error {
	var errs ValidationErrors

	// DATABASE_URL is required
	if cfg.DatabaseURL == "" {
		errs = append(errs, ValidationError{
			Field:   "DATABASE_URL",
			Message: "required",
		})
	}

	// TICK_INTERVAL must be a valid duration
	if cfg.TickIntervalStr != "" {
		d, err := time.ParseDuration(cfg.TickIntervalStr)
		if err != nil {
			errs = append(errs, ValidationError{
				Field:   "TICK_INTERVAL",
				Message: fmt.Sprintf("invalid duration: %v", err),
			})
		} else if d <= 0 {
			errs = append(errs, ValidationError{
				Field:   "TICK_INTERVAL",
				Message: "must be positive",
			})
		}
	}

	// DISPATCH_MODE must be "channel" or "db"
	if cfg.DispatchMode != "" && cfg.DispatchMode != "channel" && cfg.DispatchMode != "db" {
		errs = append(errs, ValidationError{
			Field:   "DISPATCH_MODE",
			Message: fmt.Sprintf("must be 'channel' or 'db', got %q", cfg.DispatchMode),
		})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}
