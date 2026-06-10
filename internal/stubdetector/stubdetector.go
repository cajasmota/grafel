// Package stubdetector implements the pure scoring core for the cross-graph
// stub_detector MCP tool (#4425, epic #4419 capability F).
//
// # The problem it solves
//
// A greenfield rewrite (group `v3`) is audited READ-ONLY against a behavioral
// oracle (group `oracle`). Some v3 endpoints LOOK implemented — right path,
// right DTO, green shape-tests — but return hardcoded / empty values where the
// oracle COMPUTES. Shape-and-presence checks cannot see this: the response
// shape is correct, only the provenance is wrong (a constant where a DB read
// should be). The only observable that distinguishes the two is the SIDE-EFFECT
// PROFILE: a real implementation reads/writes/calls-out; a stub does not.
//
// # The signal model
//
// The strongest, framework-agnostic signal is the cross-graph EFFECTS CONTRAST:
//
//	oracle endpoint has db/http/fs effects  AND  v3 endpoint has none
//
// computed from the SAME effective-effects source the `effects` MCP tool and
// the dashboard side-effects aggregation (#4489) use — the transitive union of
// effect kinds over the handler's downstream CALLS closure. The caller computes
// each side's effective effects and hands them here; this package does not walk
// graphs (kept pure + unit-testable).
//
// Three supporting signals (best-effort, may be Unknown when the caller cannot
// determine them — they only ADD confidence, never gate the verdict alone):
//
//   - returns_literal — the v3 handler's return value is a constant / object
//     literal with no data-flow from request inputs or IO.
//   - no_input_use    — the request payload is unreferenced on the return path.
//
// (no_effects_v3 and oracle_has_effects are derived from the effects inputs.)
//
// # Conservatism
//
// The verdict is deliberately threshold-gated so a legitimately-thin endpoint
// (a real pass-through that genuinely has no effects on BOTH sides) is reported
// "thin", not "likely_stub". Only the cross-graph CONTRAST — v3 empty WHILE the
// oracle counterpart computes — drives "likely_stub". When neither side has
// effects we cannot tell a stub from an honestly-trivial endpoint, so we never
// flag it.
package stubdetector

import "sort"

// Tristate captures a best-effort boolean signal that the caller may be unable
// to determine. Unknown signals contribute nothing to the score (neither raise
// nor lower confidence) so an undetectable signal never produces a false flag.
type Tristate int

const (
	// Unknown — the caller could not determine this signal (e.g. no source
	// window, unsupported language). Contributes zero weight.
	Unknown Tristate = iota
	// Yes — the signal is present.
	Yes
	// No — the signal is confirmed absent.
	No
)

func (t Tristate) String() string {
	switch t {
	case Yes:
		return "yes"
	case No:
		return "no"
	default:
		return "unknown"
	}
}

// Verdict is the per-endpoint classification.
type Verdict string

const (
	// VerdictLikelyStub — v3 looks implemented but the effects contrast (and
	// supporting signals) indicate it returns canned data where the oracle
	// computes.
	VerdictLikelyStub Verdict = "likely_stub"
	// VerdictThin — the endpoint genuinely has few/no effects on BOTH sides; a
	// real pass-through, not a stub. Not actionable as a drift.
	VerdictThin Verdict = "thin"
	// VerdictImplemented — the v3 endpoint has real effects comparable to the
	// oracle; no stub signal.
	VerdictImplemented Verdict = "implemented"
)

// Effects is the effective-effect view of one endpoint: the union of effect
// kinds reachable transitively from its handler (db_read/db_write/http_out/fs/…
// — the `effects` MCP tool vocabulary). The caller computes this from the SAME
// links-effects sidecar + downstream CALLS walk the dashboard aggregation uses.
type Effects struct {
	// Kinds is the set of effect kinds, e.g. {"db_write","http_out"}. Empty /
	// nil means pure (no detected sink) — the load-bearing stub signal when the
	// oracle counterpart is NON-empty.
	Kinds []string
	// Resolved reports whether effect data was actually available for this
	// endpoint (a sidecar entry was found for at least one handler in the
	// closure). When false the empty Kinds is "no data", NOT "pure" — we must
	// not read an unindexed endpoint as a stub. This mirrors the `effects`
	// tool's honesty contract: absence of detection ≠ absence of effect.
	Resolved bool
}

// hasEffects reports a confirmed non-empty effect set (resolved AND non-empty).
func (e Effects) hasEffects() bool { return e.Resolved && len(e.Kinds) > 0 }

// isPure reports a confirmed-empty effect set (resolved AND empty). An
// UNRESOLVED endpoint is neither hasEffects nor isPure — it is "no data".
func (e Effects) isPure() bool { return e.Resolved && len(e.Kinds) == 0 }

// Signals is the boolean signal vector surfaced per endpoint. The two derived
// effect signals are always determinable from the inputs; returns_literal and
// no_input_use are best-effort (Unknown when the caller could not inspect the
// return path).
type Signals struct {
	// ReturnsLiteral — v3 return value is a constant / object-literal with no
	// data-flow from inputs or IO. Best-effort.
	ReturnsLiteral Tristate `json:"returns_literal"`
	// NoEffectsV3 — the v3 endpoint's effective effects are confirmed empty.
	NoEffectsV3 Tristate `json:"no_effects_v3"`
	// OracleHasEffects — the linked oracle counterpart has db/http/fs effects.
	OracleHasEffects Tristate `json:"oracle_has_effects"`
	// NoInputUse — the request payload is unreferenced on the return path.
	// Best-effort.
	NoInputUse Tristate `json:"no_input_use"`
}

// Input carries everything the scorer needs for one linked endpoint pair.
type Input struct {
	// Endpoint is a human label for the v3 endpoint ("GET /api/orders/{id}").
	Endpoint string
	// V3Effects / OracleEffects are the effective-effect views of the two
	// linked endpoints.
	V3Effects     Effects
	OracleEffects Effects
	// ReturnsLiteral / NoInputUse are best-effort source-derived signals; pass
	// Unknown when undeterminable.
	ReturnsLiteral Tristate
	NoInputUse     Tristate
}

// Result is one scored endpoint.
type Result struct {
	Endpoint      string   `json:"endpoint"`
	Signals       Signals  `json:"signals"`
	Verdict       Verdict  `json:"verdict"`
	Confidence    float64  `json:"confidence"`
	V3Effects     []string `json:"v3_effects"`
	OracleEffects []string `json:"oracle_effects"`
	// Rationale is a short human explanation of the verdict.
	Rationale string `json:"rationale"`
}

// Scoring weights. The effects CONTRAST is the dominant term; the supporting
// signals only nudge confidence. Tuned so the contrast alone clears the
// likely-stub threshold while either supporting signal alone never does.
const (
	weightContrast       = 0.70 // oracle computes AND v3 pure — the core signal
	weightReturnsLiteral = 0.15 // best-effort corroboration
	weightNoInputUse     = 0.15 // best-effort corroboration

	// stubThreshold gates the likely_stub verdict. The contrast (0.70) alone
	// clears it; a single supporting signal (0.15) alone does not. Conservative
	// by design (#4425: "needs a threshold to avoid flagging legitimately-thin
	// endpoints").
	stubThreshold = 0.60
)

// Score classifies one linked endpoint pair into a verdict + confidence from
// the effects contrast and supporting signals. Pure and deterministic.
func Score(in Input) Result {
	sig := Signals{
		ReturnsLiteral:   in.ReturnsLiteral,
		NoInputUse:       in.NoInputUse,
		NoEffectsV3:      boolToTri(in.V3Effects.isPure(), in.V3Effects.Resolved),
		OracleHasEffects: boolToTri(in.OracleEffects.hasEffects(), in.OracleEffects.Resolved),
	}

	res := Result{
		Endpoint:      in.Endpoint,
		Signals:       sig,
		V3Effects:     sortedCopy(in.V3Effects.Kinds),
		OracleEffects: sortedCopy(in.OracleEffects.Kinds),
	}

	// The contrast can only be asserted when BOTH sides resolved: oracle has
	// effects AND v3 is confirmed-pure. An unresolved side ⇒ no contrast claim.
	contrast := in.OracleEffects.hasEffects() && in.V3Effects.isPure()

	switch {
	case contrast:
		// Core stub signal present. Build confidence from contrast + supports.
		conf := weightContrast
		if in.ReturnsLiteral == Yes {
			conf += weightReturnsLiteral
		}
		if in.NoInputUse == Yes {
			conf += weightNoInputUse
		}
		res.Confidence = clamp01(conf)
		if res.Confidence >= stubThreshold {
			res.Verdict = VerdictLikelyStub
			res.Rationale = "oracle counterpart has effects (" +
				joinKinds(in.OracleEffects.Kinds) + ") while v3 endpoint is pure — " +
				"looks implemented but computes nothing"
		} else {
			// Defensive: contrast weight alone clears the threshold, so this
			// branch is unreachable with current weights, but keep the conservative
			// fall-through rather than assume.
			res.Verdict = VerdictThin
			res.Rationale = "weak contrast below stub threshold"
		}
		return res

	case in.V3Effects.hasEffects():
		// v3 does real work. Implemented regardless of the oracle side.
		res.Verdict = VerdictImplemented
		res.Confidence = effectConfidence(in.V3Effects.Kinds)
		res.Rationale = "v3 endpoint has effects (" + joinKinds(in.V3Effects.Kinds) + ")"
		return res

	case in.V3Effects.isPure() && in.OracleEffects.isPure():
		// Pure on BOTH sides — a legitimately-thin endpoint. NOT a stub: there
		// is nothing for the oracle to compute either. Conservative.
		res.Verdict = VerdictThin
		res.Confidence = 0.5
		res.Rationale = "both v3 and oracle endpoints are pure — legitimately thin, not a stub"
		return res

	default:
		// At least one side has NO effect data (unresolved). We cannot assert a
		// contrast or rule one out. Report thin with low confidence rather than
		// risk a false stub flag. Honesty over coverage (#4425 conservatism).
		res.Verdict = VerdictThin
		res.Confidence = 0.2
		res.Rationale = "insufficient effect data to assert a stub contrast " +
			"(v3 resolved=" + boolStr(in.V3Effects.Resolved) +
			", oracle resolved=" + boolStr(in.OracleEffects.Resolved) + ")"
		return res
	}
}

// effectConfidence scales the implemented-confidence by how strong the observed
// effect set is — a db_write / http_out is a stronger "implemented" signal than
// a lone db_read. Bounded to [0.7, 0.95].
func effectConfidence(kinds []string) float64 {
	conf := 0.7
	for _, k := range kinds {
		switch k {
		case "db_write", "http_out", "mutation", "fs_write":
			conf = 0.95
		}
	}
	return conf
}

// boolToTri maps a (value, resolved) pair to a Tristate: Unknown when not
// resolved, else Yes/No.
func boolToTri(v, resolved bool) Tristate {
	if !resolved {
		return Unknown
	}
	if v {
		return Yes
	}
	return No
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

func sortedCopy(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func joinKinds(kinds []string) string {
	c := sortedCopy(kinds)
	out := ""
	for i, k := range c {
		if i > 0 {
			out += ", "
		}
		out += k
	}
	return out
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
