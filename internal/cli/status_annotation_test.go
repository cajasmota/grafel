package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// "My communities are missing" must be diagnosable from `grafel status` while
// the background annotation pass is still running — otherwise an absent overlay
// is indistinguishable from a fault.
func TestPrintAnnotationStatus_RunningPassIsVisible(t *testing.T) {
	var buf bytes.Buffer
	printAnnotationStatus(&buf, proto.StatusReply{
		GroupAlgoRunning:  []string{"acme"},
		GroupAlgoInFlight: 1,
	})
	out := buf.String()
	if !strings.Contains(out, "annotation:") || !strings.Contains(out, "running=acme") {
		t.Fatalf("a running annotation pass must be visible in `grafel status`, got %q", out)
	}
	if !strings.Contains(out, "queryable") {
		t.Fatalf("the line must say the graph itself is still queryable, got %q", out)
	}
}

// Split mode (the default) has no scheduler in the serve process, so only the
// COUNT is available. It must still be reported.
func TestPrintAnnotationStatus_SplitModeCountOnly(t *testing.T) {
	var buf bytes.Buffer
	printAnnotationStatus(&buf, proto.StatusReply{GroupAlgoInFlight: 1})
	if out := buf.String(); !strings.Contains(out, "running=1") {
		t.Fatalf("split mode must report the in-flight count, got %q", out)
	}
}

// A recompute armed (or queued behind a running pass) is pending, not missing.
func TestPrintAnnotationStatus_PendingIsVisible(t *testing.T) {
	var buf bytes.Buffer
	printAnnotationStatus(&buf, proto.StatusReply{PendingAlgo: []string{"acme"}})
	if out := buf.String(); !strings.Contains(out, "pending=acme") {
		t.Fatalf("a pending annotation pass must be visible, got %q", out)
	}
}

// Silent when there is nothing to say — the overwhelmingly common case.
func TestPrintAnnotationStatus_SilentWhenIdle(t *testing.T) {
	var buf bytes.Buffer
	printAnnotationStatus(&buf, proto.StatusReply{})
	if out := buf.String(); out != "" {
		t.Fatalf("idle daemon must print no annotation line, got %q", out)
	}
}
