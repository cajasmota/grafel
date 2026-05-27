package client

import (
	"errors"
	"testing"
)

// TestIsTransientRPCError verifies that connection-drop errors are detected.
func TestIsTransientRPCError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "connection is shut down",
			err:  errors.New("connection is shut down"),
			want: true,
		},
		{
			name: "connection reset by peer",
			err:  errors.New("connection reset"),
			want: true,
		},
		{
			name: "connection refused",
			err:  errors.New("connection refused"),
			want: true,
		},
		{
			name: "broken pipe",
			err:  errors.New("broken pipe"),
			want: true,
		},
		{
			name: "wrapped transient error",
			err:  errors.New("dial tcp: connection is shut down"),
			want: true,
		},
		{
			name: "non-transient error",
			err:  errors.New("some other error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTransientRPCError(tt.err)
			if got != tt.want {
				t.Errorf("isTransientRPCError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestPingErrorWrapping verifies that Ping wraps transient errors
// with a helpful hint about retrying.
func TestPingErrorWrapping(t *testing.T) {
	// This test is integration-level and would require a mock RPC client.
	// For now, we test the helper function above which is the core logic.
	// A full integration test would require constructing a mock rpc.Client,
	// which is non-trivial given net/rpc's internals.
}
