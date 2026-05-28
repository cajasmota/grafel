package links

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Test2809VerifyRealSidecar reads the live upvate sidecar (if present on
// the test machine) and asserts the envelope/schema split expected by
// issue #2809. The test is skipped when the sidecar is absent so CI stays
// green.
func Test2809VerifyClassifyDrift(t *testing.T) {
	// Unit test: classifyDrift correctness for known field sets.
	tests := []struct {
		producer []string
		consumer []string
		want     DriftClass
	}{
		// Pure envelope fields → envelope
		{[]string{"success", "message"}, []string{"error"}, DriftClassEnvelope},
		{[]string{"data", "count"}, []string{"results"}, DriftClassEnvelope},
		{[]string{"payload", "status"}, nil, DriftClassEnvelope},
		{[]string{"detail"}, []string{"errors"}, DriftClassEnvelope},
		// Domain fields → schema
		{[]string{"first_name", "last_name"}, nil, DriftClassSchema},
		{[]string{"building_id"}, []string{"address"}, DriftClassSchema},
		// Mixed: one non-envelope → schema
		{[]string{"success", "building_id"}, nil, DriftClassSchema},
		// Empty fields → schema (safe default)
		{nil, nil, DriftClassSchema},
	}
	for _, tc := range tests {
		got := classifyDrift(tc.producer, tc.consumer)
		if got != tc.want {
			t.Errorf("classifyDrift(%v, %v) = %q, want %q", tc.producer, tc.consumer, got, tc.want)
		}
	}
}

// Test2809LiveSidecar checks the real upvate sidecar if present.
func Test2809LiveSidecar(t *testing.T) {
	sidecarPath := filepath.Join(os.Getenv("HOME"), ".archigraph", "groups", "upvate-links-payload-drift.json")
	buf, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Skipf("upvate sidecar not found (%v) — skipping live check", err)
	}
	var doc payloadDriftDocument
	if err := json.Unmarshal(buf, &doc); err != nil {
		t.Fatalf("unmarshal sidecar: %v", err)
	}
	if doc.SchemaCount == 0 && doc.EnvelopeCount == 0 {
		t.Skip("sidecar predates #2809 (no schema_count/envelope_count) — skipping live check")
	}
	t.Logf("total=%d schema=%d envelope=%d", doc.Total, doc.SchemaCount, doc.EnvelopeCount)
	if doc.SchemaCount+doc.EnvelopeCount != doc.Total {
		t.Errorf("schema_count(%d)+envelope_count(%d) != total(%d)", doc.SchemaCount, doc.EnvelopeCount, doc.Total)
	}
}
