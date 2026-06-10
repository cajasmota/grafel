package stubdetector

import "testing"

func eff(resolved bool, kinds ...string) Effects {
	return Effects{Kinds: kinds, Resolved: resolved}
}

// The marquee case: v3 pure WHILE oracle computes → likely_stub. The contrast
// alone (no supporting signals) must clear the threshold.
func TestScore_Contrast_LikelyStub(t *testing.T) {
	r := Score(Input{
		Endpoint:      "GET /api/orders/{id}",
		V3Effects:     eff(true),                        // resolved + pure
		OracleEffects: eff(true, "db_read", "db_write"), // resolved + effects
	})
	if r.Verdict != VerdictLikelyStub {
		t.Fatalf("verdict = %q, want likely_stub (conf=%.2f)", r.Verdict, r.Confidence)
	}
	if r.Confidence < stubThreshold {
		t.Errorf("confidence %.2f below threshold %.2f", r.Confidence, stubThreshold)
	}
	if r.Signals.NoEffectsV3 != Yes || r.Signals.OracleHasEffects != Yes {
		t.Errorf("signals not set: %+v", r.Signals)
	}
}

// Supporting signals raise confidence above the bare-contrast level.
func TestScore_Contrast_SupportingSignalsRaiseConfidence(t *testing.T) {
	base := Score(Input{
		Endpoint:      "x",
		V3Effects:     eff(true),
		OracleEffects: eff(true, "db_write"),
	})
	full := Score(Input{
		Endpoint:       "x",
		V3Effects:      eff(true),
		OracleEffects:  eff(true, "db_write"),
		ReturnsLiteral: Yes,
		NoInputUse:     Yes,
	})
	if !(full.Confidence > base.Confidence) {
		t.Errorf("supporting signals did not raise confidence: base=%.2f full=%.2f", base.Confidence, full.Confidence)
	}
	if full.Verdict != VerdictLikelyStub || base.Verdict != VerdictLikelyStub {
		t.Errorf("both should be likely_stub: base=%q full=%q", base.Verdict, full.Verdict)
	}
}

// Both sides compute → implemented, never a stub.
func TestScore_BothHaveEffects_Implemented(t *testing.T) {
	r := Score(Input{
		Endpoint:      "POST /api/orders",
		V3Effects:     eff(true, "db_write"),
		OracleEffects: eff(true, "db_write", "http_out"),
	})
	if r.Verdict != VerdictImplemented {
		t.Fatalf("verdict = %q, want implemented", r.Verdict)
	}
	if r.Confidence < 0.9 {
		t.Errorf("db_write should be high-confidence implemented, got %.2f", r.Confidence)
	}
}

// v3 has effects even though oracle is pure → still implemented (v3 does work).
func TestScore_V3HasEffectsOraclePure_Implemented(t *testing.T) {
	r := Score(Input{
		Endpoint:      "x",
		V3Effects:     eff(true, "db_read"),
		OracleEffects: eff(true),
	})
	if r.Verdict != VerdictImplemented {
		t.Fatalf("verdict = %q, want implemented", r.Verdict)
	}
}

// Pure on BOTH sides → thin, NOT a stub (legitimately trivial). This is the
// false-positive guard the threshold exists for.
func TestScore_BothPure_Thin(t *testing.T) {
	r := Score(Input{
		Endpoint:      "GET /api/health",
		V3Effects:     eff(true),
		OracleEffects: eff(true),
		// Even with supporting signals, both-pure must NOT flag as stub.
		ReturnsLiteral: Yes,
		NoInputUse:     Yes,
	})
	if r.Verdict != VerdictThin {
		t.Fatalf("verdict = %q, want thin (both pure must never be likely_stub)", r.Verdict)
	}
}

// Oracle unresolved (no effect data) → cannot assert contrast → thin/low-conf,
// NOT a false stub.
func TestScore_OracleUnresolved_NoFalseStub(t *testing.T) {
	r := Score(Input{
		Endpoint:      "x",
		V3Effects:     eff(true),  // v3 pure
		OracleEffects: eff(false), // unresolved — unknown
	})
	if r.Verdict == VerdictLikelyStub {
		t.Fatalf("unresolved oracle must not yield likely_stub; got conf=%.2f", r.Confidence)
	}
	if r.Verdict != VerdictThin {
		t.Errorf("verdict = %q, want thin", r.Verdict)
	}
	if r.Signals.OracleHasEffects != Unknown {
		t.Errorf("oracle_has_effects should be Unknown, got %v", r.Signals.OracleHasEffects)
	}
}

// v3 unresolved → cannot claim v3 is pure → no stub.
func TestScore_V3Unresolved_NoFalseStub(t *testing.T) {
	r := Score(Input{
		Endpoint:      "x",
		V3Effects:     eff(false),
		OracleEffects: eff(true, "db_write"),
	})
	if r.Verdict == VerdictLikelyStub {
		t.Fatalf("unresolved v3 must not yield likely_stub")
	}
	if r.Signals.NoEffectsV3 != Unknown {
		t.Errorf("no_effects_v3 should be Unknown, got %v", r.Signals.NoEffectsV3)
	}
}

// A single supporting signal WITHOUT the contrast must never flag a stub.
func TestScore_SupportingSignalAloneNoStub(t *testing.T) {
	// v3 has effects, oracle has effects, but returns_literal=yes (noise). The
	// contrast is absent → implemented, not stub.
	r := Score(Input{
		Endpoint:       "x",
		V3Effects:      eff(true, "db_read"),
		OracleEffects:  eff(true, "db_read"),
		ReturnsLiteral: Yes,
		NoInputUse:     Yes,
	})
	if r.Verdict == VerdictLikelyStub {
		t.Fatalf("supporting signals alone must not flag a stub when v3 has effects")
	}
}

// Confidence is always within [0,1] and verdicts are deterministic.
func TestScore_ConfidenceBounded(t *testing.T) {
	inputs := []Input{
		{V3Effects: eff(true), OracleEffects: eff(true, "db_write", "http_out", "fs_write"), ReturnsLiteral: Yes, NoInputUse: Yes},
		{V3Effects: eff(true, "db_write"), OracleEffects: eff(true)},
		{V3Effects: eff(false), OracleEffects: eff(false)},
	}
	for i, in := range inputs {
		r := Score(in)
		if r.Confidence < 0 || r.Confidence > 1 {
			t.Errorf("case %d: confidence %.2f out of [0,1]", i, r.Confidence)
		}
	}
}
