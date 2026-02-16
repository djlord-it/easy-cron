package config

import (
	"strings"
	"testing"
)

func TestValidate_ValidConfig(t *testing.T) {
	cfg := Config{
		DatabaseURL:     "postgres://localhost/easycron",
		TickIntervalStr: "30s",
	}

	if err := Validate(cfg); err != nil {
		t.Errorf("valid config should not return error, got: %v", err)
	}
}

func TestValidate_MissingDatabaseURL(t *testing.T) {
	cfg := Config{
		DatabaseURL:     "",
		TickIntervalStr: "30s",
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing DATABASE_URL")
	}

	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("error should mention DATABASE_URL: %q", err.Error())
	}
}

func TestValidate_InvalidTickInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		wantErr  string
	}{
		{"non-parseable", "invalid", "invalid duration"},
		{"negative", "-1s", "must be positive"},
		{"zero", "0s", "must be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				DatabaseURL:     "postgres://localhost/easycron",
				TickIntervalStr: tt.interval,
			}

			err := Validate(cfg)
			if err == nil {
				t.Fatalf("expected error for tick_interval=%q", tt.interval)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := Config{
		DatabaseURL:     "", // missing
		TickIntervalStr: "invalid",
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected errors")
	}

	errs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	if len(errs) != 2 {
		t.Errorf("expected 2 validation errors, got %d: %v", len(errs), errs)
	}
}

func TestValidationError_Format(t *testing.T) {
	err := ValidationError{Field: "DATABASE_URL", Message: "required"}
	got := err.Error()
	want := "DATABASE_URL: required"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestValidationErrors_Format(t *testing.T) {
	// Single error
	single := ValidationErrors{{Field: "F1", Message: "M1"}}
	if single.Error() != "F1: M1" {
		t.Errorf("single error = %q, want 'F1: M1'", single.Error())
	}

	// Multiple errors
	multi := ValidationErrors{
		{Field: "F1", Message: "M1"},
		{Field: "F2", Message: "M2"},
	}
	got := multi.Error()
	if !strings.Contains(got, "2 validation errors") {
		t.Errorf("multi error should contain '2 validation errors': %q", got)
	}
	if !strings.Contains(got, "F1: M1") || !strings.Contains(got, "F2: M2") {
		t.Errorf("multi error should contain both errors: %q", got)
	}

	// Empty
	empty := ValidationErrors{}
	if empty.Error() != "" {
		t.Errorf("empty errors should return empty string, got %q", empty.Error())
	}
}
