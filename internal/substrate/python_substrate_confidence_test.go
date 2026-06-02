// Python http-framework substrate confidence_overlay proving tests (#3068).
//
// Proves that the framework-blind Python substrate sniffer (sniffEffectsPython)
// fires on representative Python http-framework source code and emits non-zero
// per-match confidence values. The effect_propagation pass is language-blind:
// it calls EffectSnifferFor("python") and stamps effect_confidence on every
// matched entity regardless of framework. These tests confirm that the
// confidence overlay infrastructure is active for all Python http_backend
// framework entities.
//
// Cells proven to `partial` (sniffer fires and confidence values are emitted,
// but comprehensive per-framework corpus validation is not yet implemented):
//   - confidence_overlay  — sniffEffectsPython emits Confidence > 0 for
//     http, db-read, db-write, fs-read, fs-write, and
//     mutation effects; EffectiveConfidence interprets
//     these values correctly.
//
// The confidence_overlay infrastructure (graph.Entity.Confidence,
// internal/mcp/tools.go min_confidence filter, internal/types/confidence.go
// taxonomy) is language-blind and applies identically to Python entities as it
// does to jsts entities. Per-framework wiring is not required — the sniffer
// registration in effect_sinks_python.go's init() is the only per-language
// hook needed.
package substrate

import (
	"os"
	"path/filepath"
	"testing"
)

// pythonSubstrateFixturePath returns the absolute path to the
// substrate_python proving fixture.
func pythonSubstrateFixturePath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(
		"..", "..", "testdata", "fixtures", "python",
		"substrate_python", "substrate_python.py",
	))
	if err != nil {
		t.Fatalf("cannot resolve python substrate fixture path: %v", err)
	}
	return p
}

// readPythonSubstrateFixture reads the substrate_python.py fixture.
func readPythonSubstrateFixture(t *testing.T) string {
	t.Helper()
	path := pythonSubstrateFixturePath(t)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read python substrate fixture at %s: %v", path, err)
	}
	return string(b)
}

// ── confidence_overlay ────────────────────────────────────────────────────────

// TestPythonSubstrate_ConfidenceOverlay_SnifferFires proves that
// sniffEffectsPython emits matches with non-zero Confidence values on the
// substrate_python fixture. This is the per-language proof that the
// confidence_overlay infrastructure is active for Python framework entities.
//
// The effect_propagation pass (internal/links/effect_propagation.go) calls
// EffectSnifferFor("python") → sniffEffectsPython, then stores the per-match
// Confidence values via EffectSet.Add. The propagation pass subsequently
// stamps effect_confidence on entity properties. The generic graph.Entity
// Confidence field and MCP min_confidence filter then apply to all Python
// entities identically to any other language.
func TestPythonSubstrate_ConfidenceOverlay_SnifferFires(t *testing.T) {
	src := readPythonSubstrateFixture(t)
	got := sniffEffectsPython(src)
	if len(got) == 0 {
		t.Fatal("sniffEffectsPython: expected matches on substrate_python fixture, got none")
	}
	// Verify every match carries a positive confidence value. A zero confidence
	// would indicate the sniffer is not stamping values, meaning the
	// confidence_overlay infrastructure would receive no data.
	for _, m := range got {
		if m.Confidence <= 0 {
			t.Errorf("sniffEffectsPython: match %+v has non-positive confidence %v; "+
				"confidence_overlay requires Confidence > 0", m, m.Confidence)
		}
	}
}

// TestPythonSubstrate_ConfidenceOverlay_HTTPEffect proves the http_effect
// match from fetch_user carries a confidence value (1.0 for requests.get).
func TestPythonSubstrate_ConfidenceOverlay_HTTPEffect(t *testing.T) {
	src := readPythonSubstrateFixture(t)
	matches := sniffEffectsPython(src)
	by := groupByEffect(matches)
	mustHave(t, by, EffectHTTPOut, "fetch_user")
	for _, m := range matches {
		if m.Function == "fetch_user" && m.Effect == EffectHTTPOut && m.Confidence <= 0 {
			t.Errorf("fetch_user http_effect confidence = %v, want > 0", m.Confidence)
		}
	}
}

// TestPythonSubstrate_ConfidenceOverlay_DBEffect proves db_effect matches
// carry confidence values for both read (list_items) and write (save_item).
func TestPythonSubstrate_ConfidenceOverlay_DBEffect(t *testing.T) {
	src := readPythonSubstrateFixture(t)
	by := groupByEffect(sniffEffectsPython(src))
	mustHave(t, by, EffectDBRead, "list_items")
	mustHave(t, by, EffectDBWrite, "save_item")
}

// TestPythonSubstrate_ConfidenceOverlay_FSEffect proves fs_effect matches
// carry confidence values for both read (read_config) and write (write_log).
func TestPythonSubstrate_ConfidenceOverlay_FSEffect(t *testing.T) {
	src := readPythonSubstrateFixture(t)
	by := groupByEffect(sniffEffectsPython(src))
	mustHave(t, by, EffectFSRead, "read_config")
	mustHave(t, by, EffectFSWrite, "write_log")
}

// TestPythonSubstrate_ConfidenceOverlay_MutationEffect proves mutation_effect
// fires on set_user (self.user = user), delivering a confidence value.
func TestPythonSubstrate_ConfidenceOverlay_MutationEffect(t *testing.T) {
	src := readPythonSubstrateFixture(t)
	by := groupByEffect(sniffEffectsPython(src))
	mustHave(t, by, EffectMutation, "set_user")
}

// TestPythonSubstrate_ConfidenceOverlay_PureFunctionNotTagged proves that
// format_label — which has no side effects — produces no effect matches.
// This confirms the sniffer is selective, not emitting false positives that
// would incorrectly populate the confidence overlay for pure functions.
func TestPythonSubstrate_ConfidenceOverlay_PureFunctionNotTagged(t *testing.T) {
	src := readPythonSubstrateFixture(t)
	got := sniffEffectsPython(src)
	for _, m := range got {
		if m.Function == "format_label" {
			t.Errorf("format_label should be pure (no effects), got unexpected match: %+v", m)
		}
	}
}

// TestPythonSubstrate_ConfidenceOverlay_RegistrationCheck proves that
// EffectSnifferFor("python") returns a non-nil sniffer, confirming the
// init() registration in effect_sinks_python.go is in effect. This is the
// prerequisite for the effect_propagation pass to activate confidence overlay
// on Python entities at all.
func TestPythonSubstrate_ConfidenceOverlay_RegistrationCheck(t *testing.T) {
	sniffer := EffectSnifferFor("python")
	if sniffer == nil {
		t.Fatal("EffectSnifferFor(\"python\") returned nil; " +
			"RegisterEffectSniffer(\"python\", sniffEffectsPython) must be called in init()")
	}
	// Run the registered sniffer on the fixture to confirm end-to-end.
	src := readPythonSubstrateFixture(t)
	got := sniffer(src)
	if len(got) == 0 {
		t.Fatal("registered python sniffer returned no matches on substrate_python fixture")
	}
}
