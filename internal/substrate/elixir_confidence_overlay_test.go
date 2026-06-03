// Elixir http-framework substrate confidence_overlay proving tests
// (epic #3872, parity-grind-elixir).
//
// VERIFY-FIRST result for the confidence_overlay parity asymmetry: the cell was
// MISSING on the 4 trailing siblings (finch / guardian / req / tesla) while 8
// other Elixir http_backend siblings (absinthe / ash / bandit / cowboy / nerves
// / oban / phoenix / plug) carry it FULL citing the universal overlay infra
// (internal/types/confidence.go + internal/graph/graph.go + internal/mcp/
// tools.go). confidence.go is explicit that the overlay is "universal: the same
// rules apply across every supported language" and "Every entity ... carries a
// confidence value" — it is NOT framework-gated. The MISSING cells were a stale
// backfill omission (issue note "backfill:dictionary-completeness"), not an
// honest gap.
//
// The per-language data feed for the overlay is the framework-blind effect
// sniffer registered via RegisterEffectSniffer("elixir", sniffEffectsElixir).
// The effect_propagation pass calls EffectSnifferFor("elixir") and stamps the
// per-match Confidence onto entity properties regardless of framework. These
// tests drive the REAL sniffer on each trailing framework's idiomatic source
// and assert the EXACT effect + a positive Confidence value it carries — the
// concrete proof that the overlay is live for these framework entities.
package substrate

import "testing"

// elixirConfMatch returns the single effect match for fn carrying effect eff on
// the given source, or fails. It is the value-asserting core shared by the four
// per-framework confidence_overlay tests below.
func elixirConfMatch(t *testing.T, src, fn string, eff Effect) EffectMatch {
	t.Helper()
	for _, m := range sniffEffectsElixir(src) {
		if m.Function == fn && m.Effect == eff {
			return m
		}
	}
	t.Fatalf("expected %s effect on %q in elixir source; got %+v", eff, fn, sniffEffectsElixir(src))
	return EffectMatch{}
}

// TestElixirConfidenceOverlay_RegistrationCheck proves EffectSnifferFor("elixir")
// returns the registered sniffer — the prerequisite for the effect_propagation
// pass to stamp confidence on any Elixir entity at all.
func TestElixirConfidenceOverlay_RegistrationCheck(t *testing.T) {
	if EffectSnifferFor("elixir") == nil {
		t.Fatal("EffectSnifferFor(\"elixir\") == nil; " +
			"RegisterEffectSniffer(\"elixir\", sniffEffectsElixir) must run in init()")
	}
}

// TestElixirConfidenceOverlay_Guardian proves the Guardian auth-pipeline idiom
// feeds the overlay: MyApp.Repo.get -> db_read on authenticate, conf 0.9.
func TestElixirConfidenceOverlay_Guardian(t *testing.T) {
	m := elixirConfMatch(t, guardianSrc, "authenticate", EffectDBRead)
	if m.Confidence != 0.9 {
		t.Errorf("guardian authenticate db_read confidence = %v, want 0.9", m.Confidence)
	}
}

// TestElixirConfidenceOverlay_Finch proves the Finch HTTP-client idiom feeds the
// overlay: Finch.build/request -> http_out on fetch_user, conf 1.0.
func TestElixirConfidenceOverlay_Finch(t *testing.T) {
	m := elixirConfMatch(t, finchSrc, "fetch_user", EffectHTTPOut)
	if m.Confidence != 1.0 {
		t.Errorf("finch fetch_user http_out confidence = %v, want 1.0", m.Confidence)
	}
}

// TestElixirConfidenceOverlay_Req proves the Req HTTP-client idiom feeds the
// overlay: Req.post -> http_out on post_event, conf 1.0.
func TestElixirConfidenceOverlay_Req(t *testing.T) {
	m := elixirConfMatch(t, reqSrc, "post_event", EffectHTTPOut)
	if m.Confidence != 1.0 {
		t.Errorf("req post_event http_out confidence = %v, want 1.0", m.Confidence)
	}
}

// TestElixirConfidenceOverlay_Tesla proves the Tesla HTTP-client idiom feeds the
// overlay: Tesla.post -> http_out on create_order, conf 1.0.
func TestElixirConfidenceOverlay_Tesla(t *testing.T) {
	m := elixirConfMatch(t, teslaSrc, "create_order", EffectHTTPOut)
	if m.Confidence != 1.0 {
		t.Errorf("tesla create_order http_out confidence = %v, want 1.0", m.Confidence)
	}
}

// TestElixirConfidenceOverlay_AllMatchesPositive proves no match on any of the
// four trailing-framework idioms carries a non-positive confidence — a zero
// would mean the sniffer feeds the overlay no usable data.
func TestElixirConfidenceOverlay_AllMatchesPositive(t *testing.T) {
	for name, src := range map[string]string{
		"guardian": guardianSrc, "finch": finchSrc, "req": reqSrc, "tesla": teslaSrc,
	} {
		ms := sniffEffectsElixir(src)
		if len(ms) == 0 {
			t.Errorf("%s: sniffEffectsElixir produced no matches; overlay would get no data", name)
		}
		for _, m := range ms {
			if m.Confidence <= 0 {
				t.Errorf("%s: match %+v has non-positive confidence %v", name, m, m.Confidence)
			}
		}
	}
}
