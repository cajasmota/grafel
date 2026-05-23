package enrichment

// pricing_test.go — Unit tests for the cost-estimator pricing helpers (#1287).

import (
	"math"
	"testing"
)

func TestTokensPerEntity_knownKinds(t *testing.T) {
	cases := []struct {
		kind string
		want int
	}{
		{"http_endpoint", 900},
		{"HTTPEndpoint", 900},
		{"Route", 900},
		{"process_flow", 700},
		{"Process", 700},
		{"message_topic", 500},
		{"Service", 600},
		{"Controller", 600},
		{"Schema", 550},
		{"Model", 550},
		{"DataAccess", 550},
		{"unknown_kind", 500}, // default
	}
	for _, tc := range cases {
		got := TokensPerEntity(tc.kind)
		if got != tc.want {
			t.Errorf("TokensPerEntity(%q) = %d, want %d", tc.kind, got, tc.want)
		}
	}
}

func TestEstimateEntityTokens_includesOverhead(t *testing.T) {
	// EstimateEntityTokens must always add at least promptOverheadTokens (200).
	for _, kind := range []string{"http_endpoint", "Service", "unknown"} {
		base := TokensPerEntity(kind)
		got := EstimateEntityTokens(kind)
		want := base + promptOverheadTokens
		if got != want {
			t.Errorf("EstimateEntityTokens(%q) = %d, want %d", kind, got, want)
		}
	}
}

func TestUSDForTokens_haiku(t *testing.T) {
	// 1M tokens at haiku rates:
	//   input  = 700k → 700k * $0.80/M  = $0.56
	//   output = 300k → 300k * $4.00/M  = $1.20
	//   total  = $1.76
	total := 1_000_000
	got := USDForTokens(total, "haiku")
	want := 1.76 // precomputed
	if math.Abs(got-want) > 0.01 {
		t.Errorf("USDForTokens(1M, haiku) = %.4f, want %.4f", got, want)
	}
}

func TestUSDForTokens_sonnet(t *testing.T) {
	// 1M tokens at sonnet rates:
	//   input  = 700k → 700k * $3.00/M  = $2.10
	//   output = 300k → 300k * $15.00/M = $4.50
	//   total  = $6.60
	total := 1_000_000
	got := USDForTokens(total, "sonnet")
	want := 6.60
	if math.Abs(got-want) > 0.01 {
		t.Errorf("USDForTokens(1M, sonnet) = %.4f, want %.4f", got, want)
	}
}

func TestUSDForTokens_zeroTokens(t *testing.T) {
	if got := USDForTokens(0, "haiku"); got != 0 {
		t.Errorf("USDForTokens(0, haiku) = %v, want 0", got)
	}
}

func TestEstimateWallMinutes(t *testing.T) {
	// 0 tokens → 0 minutes.
	if got := EstimateWallMinutes(0); got != 0 {
		t.Errorf("EstimateWallMinutes(0) = %v, want 0", got)
	}

	// parallelWorkers=30, tokensPerSecondPerWorker=500 → 15000 tok/s.
	// 900_000 tokens → 60 s → 1 minute.
	got := EstimateWallMinutes(900_000)
	want := 1.0
	if math.Abs(got-want) > 0.01 {
		t.Errorf("EstimateWallMinutes(900k) = %.3f, want %.3f", got, want)
	}
}
