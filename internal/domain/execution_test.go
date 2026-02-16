package domain

import "testing"

func TestExecutionStatus_Values(t *testing.T) {
	tests := []struct {
		status ExecutionStatus
		want   string
	}{
		{ExecutionStatusEmitted, "emitted"},
		{ExecutionStatusDelivered, "delivered"},
		{ExecutionStatusFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("ExecutionStatus = %q, want %q", tt.status, tt.want)
			}
		})
	}
}
