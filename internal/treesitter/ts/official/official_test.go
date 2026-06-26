package official

import (
	"testing"
	"time"
)

// TestParseTimeout_EnvResolution pins the watchdog deadline resolution: an unset
// or malformed GRAFEL_PARSE_TIMEOUT falls back to the default (never silently
// disabling the safety net), a valid duration is honoured, and "0" disables the
// watchdog. Pure function — no grammar/CGO needed, fully deterministic.
//
// os.Getenv treats an empty value the same as absent, so the empty case stands
// in for "unset" (the default branch) without mutating process-global state
// beyond t.Setenv's scoped, auto-restored override.
func TestParseTimeout_EnvResolution(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want time.Duration
	}{
		{name: "empty/unset uses default", val: "", want: defaultParseTimeout},
		{name: "valid seconds", val: "5s", want: 5 * time.Second},
		{name: "valid millis", val: "250ms", want: 250 * time.Millisecond},
		{name: "zero disables", val: "0", want: 0},
		{name: "malformed falls back to default", val: "not-a-duration", want: defaultParseTimeout},
		{name: "negative falls back to default", val: "-3s", want: defaultParseTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(parseTimeoutEnv, tc.val)
			if got := parseTimeout(); got != tc.want {
				t.Fatalf("parseTimeout() = %v, want %v", got, tc.want)
			}
		})
	}
}
