package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParsePagination_Defaults(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)

	limit, offset, err := parsePagination(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if limit != DefaultLimit {
		t.Errorf("expected default limit %d, got %d", DefaultLimit, limit)
	}
	if offset != 0 {
		t.Errorf("expected default offset 0, got %d", offset)
	}
}

func TestParsePagination_CustomValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/jobs?limit=50&offset=100", nil)

	limit, offset, err := parsePagination(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if limit != 50 {
		t.Errorf("expected limit 50, got %d", limit)
	}
	if offset != 100 {
		t.Errorf("expected offset 100, got %d", offset)
	}
}

func TestParsePagination_LimitExceedsMax(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/jobs?limit=2000", nil)

	_, _, err := parsePagination(req)
	if err == nil {
		t.Fatal("expected error for limit exceeding max, got nil")
	}

	expected := "limit exceeds maximum of 1000"
	if err.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, err.Error())
	}
}

func TestParsePagination_LimitAtMax(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/jobs?limit=1000", nil)

	limit, _, err := parsePagination(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if limit != MaxLimit {
		t.Errorf("expected limit %d, got %d", MaxLimit, limit)
	}
}

func TestParsePagination_NegativeLimit(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/jobs?limit=-1", nil)

	_, _, err := parsePagination(req)
	if err == nil {
		t.Fatal("expected error for negative limit, got nil")
	}
}

func TestParsePagination_NegativeOffset(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/jobs?offset=-1", nil)

	_, _, err := parsePagination(req)
	if err == nil {
		t.Fatal("expected error for negative offset, got nil")
	}
}

func TestParsePagination_InvalidLimit(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/jobs?limit=abc", nil)

	_, _, err := parsePagination(req)
	if err == nil {
		t.Fatal("expected error for invalid limit, got nil")
	}
}

func TestParsePagination_InvalidOffset(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/jobs?offset=xyz", nil)

	_, _, err := parsePagination(req)
	if err == nil {
		t.Fatal("expected error for invalid offset, got nil")
	}
}

func TestParsePagination_ZeroLimit(t *testing.T) {
	// limit=0 should be treated as "use default"
	req := httptest.NewRequest(http.MethodGet, "/jobs?limit=0", nil)

	limit, _, err := parsePagination(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if limit != DefaultLimit {
		t.Errorf("expected default limit %d for limit=0, got %d", DefaultLimit, limit)
	}
}
