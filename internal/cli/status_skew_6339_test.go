package cli

// status_skew_6339_test.go — #6339. A daemon whose `serve` process is running
// a much older build than its engine child was invisible to `grafel status`:
// the check existed only in `grafel doctor`. The observed case was a serve
// build roughly two months old.
//
// The line is CONDITIONAL by design: users who are not skewed must see no new
// output at all. Both directions are asserted here, because a test that only
// checked the skewed case would pass an implementation that printed
// unconditionally — precisely the failure mode the design forbids.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
)

func TestPrintEngineVersionSkew_Skewed_PrintsLine(t *testing.T) {
	orig := engineVersionSkew
	t.Cleanup(func() { engineVersionSkew = orig })
	engineVersionSkew = func() *install.VersionSkew {
		return &install.VersionSkew{Serve: "v0.1.9", Engine: "v0.2.2"}
	}

	var buf bytes.Buffer
	PrintEngineVersionSkew(&buf)
	out := buf.String()

	if out == "" {
		t.Fatal("skewed daemon printed nothing")
	}
	for _, want := range []string{"v0.1.9", "v0.2.2", "grafel restart"} {
		if !strings.Contains(out, want) {
			t.Errorf("skew line missing %q; got: %q", want, out)
		}
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("skew line must end in a newline; got: %q", out)
	}
}

func TestPrintEngineVersionSkew_NotSkewed_PrintsNothing(t *testing.T) {
	orig := engineVersionSkew
	t.Cleanup(func() { engineVersionSkew = orig })
	engineVersionSkew = func() *install.VersionSkew { return nil }

	var buf bytes.Buffer
	PrintEngineVersionSkew(&buf)

	if got := buf.String(); got != "" {
		t.Fatalf("un-skewed daemon must produce NO output, got: %q", got)
	}
}
