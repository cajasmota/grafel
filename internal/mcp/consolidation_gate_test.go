package mcp

// consolidation_gate_test.go — the #5556 final regression gate for the
// #5546 MCP tool consolidation (68 → 22 intent-named tools).
//
// The consolidation collapsed 55 legacy tools into hidden, dispatchable
// back-compat aliases and now advertises exactly 22 canonical tools in the
// tools/list handshake. That dropped the per-connect handshake cost from a
// ~7,592-token baseline to ~3,545 tokens (~53% reduction).
//
// The structural guards (advertised==22, no alias leak, aliases still
// dispatchable, partition invariant) live in hidden_aliases_5552_test.go,
// and the instructions-name guard lives in server_test.go. What was missing —
// and what THIS file adds — is a TIGHT TOKEN CEILING on the advertised
// handshake. The pre-existing budget test (budget_test.go) measures against
// mcp.TokenCeiling (8000), which is loose enough that silently un-hiding the
// 55 aliases (~7,592 tokens) would NOT trip it. AdvertisedTokenCeiling closes
// that hole: any un-consolidation or schema bloat that pushes the advertised
// surface back over ~4,500 tokens fails the build.

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// AdvertisedTokenCeiling is the regression ceiling for the *advertised*
// (22-tool) handshake in tokens, using the same conservative 4-chars-per-token
// estimator as cmd/mcp-audit. The post-#5546 floor is ~3,545; 4,500 leaves
// comfortable headroom for tight per-tool schema growth while sitting well
// under the old 7,592-token, 68-tool baseline. If un-consolidation (un-hiding
// aliases) or schema bloat pushes the advertised surface past this, the build
// fails — that is the point. Raise it ONLY with a recorded justification.
const AdvertisedTokenCeiling = 4500

// advertisedEnvelopeBytes mirrors the cmd/mcp-audit init-envelope estimate
// (server name/version + JSON-RPC framing + the mcpInstructions orientation
// map). Kept here so this gate measures the same per-connect cost the audit
// reports, without importing the cmd package.
const advertisedEnvelopeBytes = 2392

// TestConsolidationAdvertisedTokenCeiling asserts the advertised handshake
// stays under AdvertisedTokenCeiling. This is the #5556 anti-un-consolidation
// gate: it trips if the 55 hidden aliases are ever re-advertised, or if schema
// bloat erodes the ~53% reduction the consolidation delivered.
func TestConsolidationAdvertisedTokenCeiling(t *testing.T) {
	repoDir := t.TempDir()
	srv := makeTestServer(t, map[string]map[string]string{
		"mygroup": {"myrepo": repoDir},
	})

	entries, err := srv.ListToolsForCWD(repoDir)
	if err != nil {
		t.Fatalf("ListToolsForCWD: %v", err)
	}

	totalChars := advertisedEnvelopeBytes
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal advertised tool %s: %v", e.Name, err)
		}
		totalChars += len(b)
	}
	tokens := int(math.Ceil(float64(totalChars) / 4))

	t.Logf("advertised=%d tools  chars=%d  tokens=%d  ceiling=%d (baseline was 7592 @ 68 tools)",
		len(entries), totalChars, tokens, AdvertisedTokenCeiling)

	if tokens > AdvertisedTokenCeiling {
		t.Errorf("advertised handshake %d tokens exceeds ceiling %d — the #5546 consolidation may have regressed (aliases un-hidden or schema bloat). Re-run `go run ./cmd/mcp-audit` and investigate before raising the ceiling.",
			tokens, AdvertisedTokenCeiling)
	}
}

// TestConsolidationAdvertisesExactly22 re-asserts the canonical allow-list in
// the same file as the token gate so the two move together: the headline
// "22 advertised + token ceiling" invariant lives in one place for #5556.
// (Structural detail is also covered by hidden_aliases_5552_test.go.)
func TestConsolidationAdvertisesExactly22(t *testing.T) {
	repoDir := t.TempDir()
	srv := makeTestServer(t, map[string]map[string]string{
		"mygroup": {"myrepo": repoDir},
	})

	entries, err := srv.ListToolsForCWD(repoDir)
	if err != nil {
		t.Fatalf("ListToolsForCWD: %v", err)
	}

	if len(canonicalToolNames) != 22 {
		t.Fatalf("canonicalToolNames has %d entries, want 22 — the #5546 allow-list changed", len(canonicalToolNames))
	}
	if len(entries) != 22 {
		t.Errorf("advertised %d tools, want exactly 22: %v", len(entries), toolNames(entries))
	}

	for _, e := range entries {
		if !canonicalToolNames[e.Name] {
			t.Errorf("advertised non-canonical tool %q", e.Name)
		}
		if aliasToolNames[e.Name] {
			t.Errorf("hidden alias %q leaked into the advertised handshake", e.Name)
		}
	}
}

// TestConsolidationInstructionsHaveNoAliases asserts the mcpInstructions
// orientation map (shipped in the initialize envelope) names none of the 55
// absorbed legacy tools — so the in-context routing guidance can't drift back
// to teaching deprecated names. (server_test.go also covers this; kept here as
// a cheap co-located guard for the consolidation end-state.)
func TestConsolidationInstructionsHaveNoAliases(t *testing.T) {
	for alias := range aliasToolNames {
		if strings.Contains(mcpInstructions, alias) {
			t.Errorf("mcpInstructions names absorbed alias %q — route via its canonical tool instead", alias)
		}
	}
}
