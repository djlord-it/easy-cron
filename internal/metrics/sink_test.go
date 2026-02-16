package metrics

import (
	"errors"
	"testing"
)

func TestClassifyStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		err        error
		want       string
	}{
		// Success codes
		{"200 OK", 200, nil, StatusClass2xx},
		{"201 Created", 201, nil, StatusClass2xx},
		{"204 No Content", 204, nil, StatusClass2xx},
		{"299 boundary", 299, nil, StatusClass2xx},

		// Client errors
		{"400 Bad Request", 400, nil, StatusClass4xx},
		{"404 Not Found", 404, nil, StatusClass4xx},
		{"429 Rate Limit", 429, nil, StatusClass4xx},
		{"499 boundary", 499, nil, StatusClass4xx},

		// Server errors
		{"500 Internal Server Error", 500, nil, StatusClass5xx},
		{"502 Bad Gateway", 502, nil, StatusClass5xx},
		{"503 Service Unavailable", 503, nil, StatusClass5xx},

		// Edge cases
		{"302 redirect", 302, nil, StatusClassOtherError},
		{"100 continue", 100, nil, StatusClassOtherError},

		// Timeout errors
		{"context timeout", 0, errors.New("context deadline exceeded"), StatusClassTimeout},
		{"timeout in message", 0, errors.New("operation timeout"), StatusClassTimeout},
		{"Timeout uppercase", 0, errors.New("Timeout exceeded"), StatusClassTimeout},

		// Connection errors
		{"connection refused", 0, errors.New("connection refused"), StatusClassConnectionError},
		{"no such host", 0, errors.New("no such host"), StatusClassConnectionError},
		{"network unreachable", 0, errors.New("network is unreachable"), StatusClassConnectionError},
		{"dial error", 0, errors.New("dial tcp 127.0.0.1:80: connect: refused"), StatusClassConnectionError},

		// Other errors
		{"generic error", 0, errors.New("unknown error"), StatusClassOtherError},
		{"empty error", 0, errors.New(""), StatusClassOtherError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyStatus(tt.statusCode, tt.err)
			if got != tt.want {
				t.Errorf("ClassifyStatus(%d, %v) = %q, want %q", tt.statusCode, tt.err, got, tt.want)
			}
		})
	}
}
