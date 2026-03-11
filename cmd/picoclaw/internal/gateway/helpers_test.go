package gateway

import (
	"testing"
)

func TestCronExecuteResultError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		result    string
		wantError bool
	}{
		{
			name:      "ok result",
			result:    "ok",
			wantError: false,
		},
		{
			name:      "error prefix",
			result:    "Error: publish cron inbound failed",
			wantError: true,
		},
		{
			name:      "error prefix with leading spaces",
			result:    "   error: delegation missing",
			wantError: true,
		},
		{
			name:      "non error text",
			result:    "scheduled command executed",
			wantError: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := cronExecuteResultError(tt.result)
			if tt.wantError && err == nil {
				t.Fatalf("cronExecuteResultError(%q) returned nil error, want non-nil", tt.result)
			}
			if !tt.wantError && err != nil {
				t.Fatalf("cronExecuteResultError(%q) returned error %v, want nil", tt.result, err)
			}
		})
	}
}

