package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestPrintIndexingModes verifies the doctor surfacing of the resource-safe
// defaults (v0.1.1): incremental defaults ON (#5231) and subprocess defaults
// ON (CPU-capped child indexer). The env override still flips incremental off.
// Note: the subprocess toggle is resolved once at package init() from the
// process env, so this asserts the default-on label (the test binary runs with
// GRAFEL_SUBPROCESS_INDEXER unset).
func TestPrintIndexingModes(t *testing.T) {
	t.Run("defaults: incremental on, subprocess on", func(t *testing.T) {
		t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "")
		var buf bytes.Buffer
		printIndexingModes(&buf)
		out := buf.String()
		if !strings.Contains(out, "incremental reindex: on") {
			t.Fatalf("expected incremental ON by default, got:\n%s", out)
		}
		if !strings.Contains(out, "subprocess indexer: on") {
			t.Fatalf("expected subprocess ON by default, got:\n%s", out)
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
