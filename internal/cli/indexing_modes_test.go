package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestPrintIndexingModes verifies the #5231 surfacing in `grafel doctor`:
// incremental defaults ON, subprocess defaults OFF, and the env override
// flips incremental to off.
func TestPrintIndexingModes(t *testing.T) {
	t.Run("defaults: incremental on, subprocess off", func(t *testing.T) {
		t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "")
		var buf bytes.Buffer
		printIndexingModes(&buf)
		out := buf.String()
		if !strings.Contains(out, "incremental reindex: on") {
			t.Fatalf("expected incremental ON by default, got:\n%s", out)
		}
		if !strings.Contains(out, "subprocess indexer: off") {
			t.Fatalf("expected subprocess OFF by default, got:\n%s", out)
		}
	})

	t.Run("incremental forced off via env", func(t *testing.T) {
		t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "0")
		var buf bytes.Buffer
		printIndexingModes(&buf)
		if !strings.Contains(buf.String(), "incremental reindex: off") {
			t.Fatalf("expected incremental OFF with =0, got:\n%s", buf.String())
		}
	})
}

// TestOnOff is a tiny guard for the shared on/off renderer used by both
// status and doctor.
func TestOnOff(t *testing.T) {
	if onOff(true) != "on" || onOff(false) != "off" {
		t.Fatalf("onOff: got %q/%q, want on/off", onOff(true), onOff(false))
	}
}
