// Tests for the applyFeatureFlagEdges pass (#3628 area #17).
//
// Strategy: drive the pass directly via DetectorPassArgs (the pass is a pure
// function of Content+Lang+accumulated slices), then assert on the emitted
// SCOPE.FeatureFlag entities and GATED_BY edges. Assertions are
// VALUE-ASSERTING — they check the exact flag key, the enclosing-function
// FromID, and the synthetic `feature:<key>` ToID — not just len>0.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// gateEdge is the minimal projection of a GATED_BY edge for assertions.
type gateEdge struct {
	From string
	To   string
	Flag string
	SDK  string
}

// runFlagPass runs applyFeatureFlagEdges on a single in-memory file and
// returns the FeatureFlag entities + GATED_BY edges it appended.
func runFlagPass(lang, path, src string) ([]types.EntityRecord, []gateEdge) {
	res := applyFeatureFlagEdges(DetectorPassArgs{
		Lang:    lang,
		Path:    path,
		Content: []byte(src),
	})
	var flags []types.EntityRecord
	for _, e := range res.Entities {
		if e.Kind == featureFlagEntityKind {
			flags = append(flags, e)
		}
	}
	var edges []gateEdge
	for _, r := range res.Relationships {
		if r.Kind != featureFlagEdgeKind {
			continue
		}
		edges = append(edges, gateEdge{
			From: r.FromID,
			To:   r.ToID,
			Flag: r.Properties["flag"],
			SDK:  r.Properties["sdk"],
		})
	}
	return flags, edges
}

// findGate returns the GATED_BY edge whose flag key == key, or false.
func findGate(edges []gateEdge, key string) (gateEdge, bool) {
	for _, e := range edges {
		if e.Flag == key {
			return e, true
		}
	}
	return gateEdge{}, false
}

// findFlag returns the FeatureFlag entity whose Name == key, or false.
func findFlag(flags []types.EntityRecord, key string) (types.EntityRecord, bool) {
	for _, f := range flags {
		if f.Name == key {
			return f, true
		}
	}
	return types.EntityRecord{}, false
}

// LaunchDarkly: client.variation("new-checkout", user, false) in checkout()
// → checkout GATED_BY feature:new-checkout.
func TestFeatureFlag_LaunchDarkly_Python_checkout(t *testing.T) {
	src := `
def checkout(cart, user):
    if client.variation("new-checkout", user, False):
        return new_checkout_flow(cart)
    return legacy_checkout(cart)
`
	flags, edges := runFlagPass("python", "checkout.py", src)

	flag, ok := findFlag(flags, "new-checkout")
	if !ok {
		t.Fatalf("expected a feature:new-checkout entity, got flags=%v", flags)
	}
	if flag.ID != "feature:new-checkout" {
		t.Errorf("flag ID = %q, want feature:new-checkout", flag.ID)
	}
	if flag.Subtype != "launchdarkly" {
		t.Errorf("flag SDK subtype = %q, want launchdarkly", flag.Subtype)
	}

	g, ok := findGate(edges, "new-checkout")
	if !ok {
		t.Fatalf("expected GATED_BY edge for new-checkout, got %v", edges)
	}
	if g.From != "Function:checkout" {
		t.Errorf("GATED_BY FromID = %q, want Function:checkout", g.From)
	}
	if g.To != "feature:new-checkout" {
		t.Errorf("GATED_BY ToID = %q, want feature:new-checkout", g.To)
	}
	if g.SDK != "launchdarkly" {
		t.Errorf("GATED_BY sdk = %q, want launchdarkly", g.SDK)
	}
}

// LaunchDarkly snake_case bool_variation (Python server SDK).
func TestFeatureFlag_LaunchDarkly_bool_variation(t *testing.T) {
	src := `
def render(user):
    show = ldclient.get().bool_variation("dark-mode", user, False)
    return show
`
	flags, edges := runFlagPass("python", "render.py", src)
	if _, ok := findFlag(flags, "dark-mode"); !ok {
		t.Fatalf("expected feature:dark-mode entity, got %v", flags)
	}
	g, ok := findGate(edges, "dark-mode")
	if !ok {
		t.Fatalf("expected GATED_BY for dark-mode, got %v", edges)
	}
	if g.From != "Function:render" || g.To != "feature:dark-mode" {
		t.Errorf("edge = %+v, want From=Function:render To=feature:dark-mode", g)
	}
}

// Unleash: isEnabled("dark-mode") → feature:dark-mode, attributed to the
// enclosing JS function.
func TestFeatureFlag_Unleash_JS_isEnabled(t *testing.T) {
	src := `
function renderNav() {
  if (unleash.isEnabled("dark-mode")) {
    return darkNav();
  }
  return lightNav();
}
`
	flags, edges := runFlagPass("javascript", "nav.js", src)

	flag, ok := findFlag(flags, "dark-mode")
	if !ok {
		t.Fatalf("expected feature:dark-mode entity, got %v", flags)
	}
	if flag.Subtype != "unleash" {
		t.Errorf("flag SDK = %q, want unleash", flag.Subtype)
	}
	g, ok := findGate(edges, "dark-mode")
	if !ok {
		t.Fatalf("expected GATED_BY for dark-mode, got %v", edges)
	}
	if g.From != "Function:renderNav" {
		t.Errorf("FromID = %q, want Function:renderNav", g.From)
	}
	if g.To != "feature:dark-mode" {
		t.Errorf("ToID = %q, want feature:dark-mode", g.To)
	}
}

// OpenFeature: client.getBooleanValue("new-ui", false).
func TestFeatureFlag_OpenFeature_getBooleanValue(t *testing.T) {
	src := `
function load() {
  const enabled = client.getBooleanValue("new-ui", false);
  return enabled ? newUI() : oldUI();
}
`
	flags, edges := runFlagPass("typescript", "load.ts", src)
	flag, ok := findFlag(flags, "new-ui")
	if !ok {
		t.Fatalf("expected feature:new-ui entity, got %v", flags)
	}
	if flag.Subtype != "openfeature" {
		t.Errorf("flag SDK = %q, want openfeature", flag.Subtype)
	}
	g, ok := findGate(edges, "new-ui")
	if !ok || g.From != "Function:load" || g.To != "feature:new-ui" {
		t.Fatalf("edge = %+v ok=%v, want Function:load -> feature:new-ui", g, ok)
	}
}

// Flipper (Ruby): Flipper.enabled?(:beta) → feature:beta.
func TestFeatureFlag_Flipper_Ruby_symbol(t *testing.T) {
	src := `
def show_beta
  if Flipper.enabled?(:beta, current_user)
    render_beta
  end
end
`
	flags, edges := runFlagPass("ruby", "beta.rb", src)
	flag, ok := findFlag(flags, "beta")
	if !ok {
		t.Fatalf("expected feature:beta entity (symbol key), got %v", flags)
	}
	if flag.Subtype != "flipper" {
		t.Errorf("flag SDK = %q, want flipper", flag.Subtype)
	}
	g, ok := findGate(edges, "beta")
	if !ok {
		t.Fatalf("expected GATED_BY for beta, got %v", edges)
	}
	if g.From != "Function:show_beta" {
		t.Errorf("FromID = %q, want Function:show_beta", g.From)
	}
	if g.To != "feature:beta" {
		t.Errorf("ToID = %q, want feature:beta", g.To)
	}
}

// Flagsmith: has_feature("promo-banner").
func TestFeatureFlag_Flagsmith_has_feature(t *testing.T) {
	src := `
def banner(self):
    if flagsmith.has_feature("promo-banner"):
        return promo()
`
	flags, edges := runFlagPass("python", "banner.py", src)
	flag, ok := findFlag(flags, "promo-banner")
	if !ok {
		t.Fatalf("expected feature:promo-banner entity, got %v", flags)
	}
	if flag.Subtype != "flagsmith" {
		t.Errorf("flag SDK = %q, want flagsmith", flag.Subtype)
	}
	if _, ok := findGate(edges, "promo-banner"); !ok {
		t.Fatalf("expected GATED_BY for promo-banner, got %v", edges)
	}
}

// NEGATIVE: a dynamic flag key (bare identifier argument) must NOT fabricate
// a flag entity or edge.
func TestFeatureFlag_DynamicKey_NoFabrication(t *testing.T) {
	src := `
def gate(flag_key, user):
    return client.variation(flag_key, user, False)
`
	flags, edges := runFlagPass("python", "gate.py", src)
	if len(flags) != 0 {
		t.Errorf("dynamic key should yield no flag entities, got %v", flags)
	}
	if len(edges) != 0 {
		t.Errorf("dynamic key should yield no GATED_BY edges, got %v", edges)
	}
}

// NEGATIVE: a file with no flag SDK calls is a clean no-op.
func TestFeatureFlag_NoFlags_NoOp(t *testing.T) {
	src := `
def add(a, b):
    return a + b
`
	flags, edges := runFlagPass("python", "math.py", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("expected no flag output, got flags=%v edges=%v", flags, edges)
	}
}

// Multiple checks of the SAME flag in one function collapse to ONE GATED_BY
// edge and ONE flag entity (dedup).
func TestFeatureFlag_Dedup_SameFlagOneEdge(t *testing.T) {
	src := `
function a() {
  if (unleash.isEnabled("x-flag")) { return 1; }
  if (unleash.isEnabled("x-flag")) { return 2; }
}
`
	flags, edges := runFlagPass("javascript", "a.js", src)
	n := 0
	for _, f := range flags {
		if f.Name == "x-flag" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected exactly 1 x-flag entity, got %d", n)
	}
	m := 0
	for _, e := range edges {
		if e.Flag == "x-flag" {
			m++
		}
	}
	if m != 1 {
		t.Errorf("expected exactly 1 GATED_BY edge for x-flag, got %d", m)
	}
}

// Two different functions each gating on the SAME flag converge on ONE flag
// entity but emit TWO edges (distinct callers) — the blast-radius shape.
func TestFeatureFlag_BlastRadius_SharedFlagTwoCallers(t *testing.T) {
	src := `
function checkout() {
  return unleash.isEnabled("new-pay");
}
function profile() {
  return unleash.isEnabled("new-pay");
}
`
	flags, edges := runFlagPass("javascript", "pay.js", src)
	n := 0
	for _, f := range flags {
		if f.Name == "new-pay" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected 1 shared new-pay entity, got %d", n)
	}
	froms := map[string]bool{}
	for _, e := range edges {
		if e.Flag == "new-pay" {
			froms[e.From] = true
		}
	}
	if !froms["Function:checkout"] || !froms["Function:profile"] {
		t.Errorf("expected GATED_BY from both checkout and profile, got froms=%v", froms)
	}
}
