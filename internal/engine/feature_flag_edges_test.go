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

// RUBY — Unleash `?`-suffixed predicate: a Rails controller action calling
// `UNLEASH.is_enabled?("beta")` → feature:beta, SDK subtype "unleash",
// attributed to the enclosing Ruby method. The Ruby SDK uses the `?`-suffixed
// predicate form `is_enabled?` which the JS/Python `is_enabled(` matcher misses
// without the optional `\??`. #4140.
func TestFeatureFlag_Ruby_Unleash_is_enabled_predicate(t *testing.T) {
	src := `
def index
  if UNLEASH.is_enabled?("beta")
    render_beta
  else
    render_stable
  end
end
`
	flags, edges := runFlagPass("ruby", "controller.rb", src)

	flag, ok := findFlag(flags, "beta")
	if !ok {
		t.Fatalf("expected feature:beta entity, got flags=%v", flags)
	}
	if flag.ID != "feature:beta" {
		t.Errorf("flag ID = %q, want feature:beta", flag.ID)
	}
	if flag.Subtype != "unleash" {
		t.Errorf("flag SDK subtype = %q, want unleash", flag.Subtype)
	}
	g, ok := findGate(edges, "beta")
	if !ok {
		t.Fatalf("expected GATED_BY for beta, got %v", edges)
	}
	if g.From != "Function:index" {
		t.Errorf("GATED_BY FromID = %q, want Function:index", g.From)
	}
	if g.To != "feature:beta" {
		t.Errorf("GATED_BY ToID = %q, want feature:beta", g.To)
	}
	if g.SDK != "unleash" {
		t.Errorf("GATED_BY sdk = %q, want unleash", g.SDK)
	}
}

// RUBY — Flipper subscript form `Flipper[:new_dash].enabled?` → feature:new_dash
// (symbol normalized, leading `:` stripped), SDK subtype "flipper", attributed
// to the enclosing Ruby method. #4140.
func TestFeatureFlag_Ruby_Flipper_subscript(t *testing.T) {
	src := `
def dashboard
  return new_dashboard if Flipper[:new_dash].enabled?
  legacy_dashboard
end
`
	flags, edges := runFlagPass("ruby", "dash.rb", src)
	flag, ok := findFlag(flags, "new_dash")
	if !ok {
		t.Fatalf("expected feature:new_dash entity (symbol normalized), got %v", flags)
	}
	if flag.ID != "feature:new_dash" {
		t.Errorf("flag ID = %q, want feature:new_dash", flag.ID)
	}
	if flag.Subtype != "flipper" {
		t.Errorf("flag SDK = %q, want flipper", flag.Subtype)
	}
	g, ok := findGate(edges, "new_dash")
	if !ok || g.From != "Function:dashboard" || g.To != "feature:new_dash" {
		t.Fatalf("edge = %+v ok=%v, want Function:dashboard -> feature:new_dash", g, ok)
	}
	if g.SDK != "flipper" {
		t.Errorf("GATED_BY sdk = %q, want flipper", g.SDK)
	}
}

// RUBY — Flipper feature-object form `Flipper.feature(:promo).enabled?` →
// feature:promo, SDK subtype "flipper". #4140.
func TestFeatureFlag_Ruby_Flipper_feature_object(t *testing.T) {
	src := `
def promo
  Flipper.feature(:promo).enabled? ? show_promo : nil
end
`
	flags, edges := runFlagPass("ruby", "promo.rb", src)
	if _, ok := findFlag(flags, "promo"); !ok {
		t.Fatalf("expected feature:promo entity, got %v", flags)
	}
	g, ok := findGate(edges, "promo")
	if !ok || g.From != "Function:promo" || g.To != "feature:promo" || g.SDK != "flipper" {
		t.Fatalf("edge = %+v ok=%v, want Function:promo -> feature:promo (flipper)", g, ok)
	}
}

// RUBY — Rollout gem `$rollout.active?(:new_checkout, current_user)` →
// feature:new_checkout (symbol normalized), SDK subtype "rollout", attributed to
// the enclosing Ruby method. The receiver is the `$rollout` global. #4140.
func TestFeatureFlag_Ruby_Rollout_active(t *testing.T) {
	src := `
def checkout
  if $rollout.active?(:new_checkout, current_user)
    new_checkout_flow
  else
    legacy_checkout
  end
end
`
	flags, edges := runFlagPass("ruby", "checkout.rb", src)
	flag, ok := findFlag(flags, "new_checkout")
	if !ok {
		t.Fatalf("expected feature:new_checkout entity, got %v", flags)
	}
	if flag.ID != "feature:new_checkout" {
		t.Errorf("flag ID = %q, want feature:new_checkout", flag.ID)
	}
	if flag.Subtype != "rollout" {
		t.Errorf("flag SDK = %q, want rollout", flag.Subtype)
	}
	g, ok := findGate(edges, "new_checkout")
	if !ok {
		t.Fatalf("expected GATED_BY for new_checkout, got %v", edges)
	}
	if g.From != "Function:checkout" || g.To != "feature:new_checkout" {
		t.Errorf("edge = %+v, want Function:checkout -> feature:new_checkout", g)
	}
	if g.SDK != "rollout" {
		t.Errorf("GATED_BY sdk = %q, want rollout", g.SDK)
	}
}

// RUBY NEGATIVE — a bare `record.enabled?` predicate on a non-flag receiver (an
// ActiveRecord model) must NOT be attributed: there is no Flipper / Unleash /
// Rollout SDK token, so the receiver-gated matchers correctly emit nothing.
func TestFeatureFlag_Ruby_NonFlagPredicate_NoFabrication(t *testing.T) {
	src := `
def visible?
  record.enabled? && record.published?
end
`
	flags, edges := runFlagPass("ruby", "visible.rb", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("bare non-flag .enabled? predicate should yield no output, got flags=%v edges=%v", flags, edges)
	}
}

// RUBY NEGATIVE — a generic `.active?(:x)` on a NON-rollout receiver must NOT be
// attributed: the Rollout matcher requires a `rollout` receiver, so a
// `widget.active?(:on)` call on an arbitrary object emits nothing.
func TestFeatureFlag_Ruby_NonRolloutActive_NoFabrication(t *testing.T) {
	src := `
def click
  return unless widget.active?(:on)
  handle_click
end
`
	flags, edges := runFlagPass("ruby", "click.rb", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("non-rollout .active? should yield no output, got flags=%v edges=%v", flags, edges)
	}
}

// RUBY NEGATIVE — a dynamic (non-literal) key on the Rollout matcher must NOT
// fabricate a flag entity or edge (honest-partial, mirrors the other langs).
func TestFeatureFlag_Ruby_Rollout_DynamicKey_NoFabrication(t *testing.T) {
	src := `
def gate(flag_name, user)
  $rollout.active?(flag_name, user)
end
`
	flags, edges := runFlagPass("ruby", "gate.rb", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("dynamic Rollout key should yield no output, got flags=%v edges=%v", flags, edges)
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

// Unleash (Python snake_case): unleash_client.is_enabled("beta-dashboard")
// inside a handler → feature:beta-dashboard, SDK subtype "unleash", attributed
// to the enclosing Python function. Mirrors the JS isEnabled test for the
// Python SDK idiom (the canonical `is_enabled` snake_case call). #4044/#3706.
func TestFeatureFlag_Unleash_Python_is_enabled(t *testing.T) {
	src := `
def feature_view(request):
    if unleash_client.is_enabled("beta-dashboard"):
        return beta_response(request)
    return stable_response(request)
`
	flags, edges := runFlagPass("python", "views.py", src)

	flag, ok := findFlag(flags, "beta-dashboard")
	if !ok {
		t.Fatalf("expected feature:beta-dashboard entity, got %v", flags)
	}
	if flag.ID != "feature:beta-dashboard" {
		t.Errorf("flag ID = %q, want feature:beta-dashboard", flag.ID)
	}
	if flag.Subtype != "unleash" {
		t.Errorf("flag SDK = %q, want unleash", flag.Subtype)
	}
	g, ok := findGate(edges, "beta-dashboard")
	if !ok {
		t.Fatalf("expected GATED_BY for beta-dashboard, got %v", edges)
	}
	if g.From != "Function:feature_view" {
		t.Errorf("FromID = %q, want Function:feature_view", g.From)
	}
	if g.To != "feature:beta-dashboard" {
		t.Errorf("ToID = %q, want feature:beta-dashboard", g.To)
	}
	if g.SDK != "unleash" {
		t.Errorf("GATED_BY sdk = %q, want unleash", g.SDK)
	}
}

// OpenFeature (Python snake_case): client.get_boolean_value("new-checkout",
// False) inside an async handler → feature:new-checkout, SDK subtype
// "openfeature", attributed to the enclosing Python function. Mirrors the TS
// getBooleanValue test for the Python snake_case SDK idiom. #4044/#3706.
func TestFeatureFlag_OpenFeature_Python_get_boolean_value(t *testing.T) {
	src := `
async def checkout(request):
    enabled = await client.get_boolean_value("new-checkout", False)
    return new_checkout(request) if enabled else legacy_checkout(request)
`
	flags, edges := runFlagPass("python", "checkout_of.py", src)

	flag, ok := findFlag(flags, "new-checkout")
	if !ok {
		t.Fatalf("expected feature:new-checkout entity, got %v", flags)
	}
	if flag.Subtype != "openfeature" {
		t.Errorf("flag SDK = %q, want openfeature", flag.Subtype)
	}
	g, ok := findGate(edges, "new-checkout")
	if !ok {
		t.Fatalf("expected GATED_BY for new-checkout, got %v", edges)
	}
	if g.From != "Function:checkout" || g.To != "feature:new-checkout" {
		t.Errorf("edge = %+v, want Function:checkout -> feature:new-checkout", g)
	}
	if g.SDK != "openfeature" {
		t.Errorf("GATED_BY sdk = %q, want openfeature", g.SDK)
	}
}

// HONEST-PARTIAL (Python): OpenFeature's keyword-argument call form
// `get_boolean_value(flag_key="dark", default_value=False)` does NOT expose the
// key as the FIRST positional literal, so the positional-literal matcher does
// not fire and NO flag is fabricated. This documents the Python kwarg gap that
// keeps the framework cells honest-partial rather than full.
func TestFeatureFlag_OpenFeature_Python_kwarg_NotMatched(t *testing.T) {
	src := `
def handler(request):
    return client.get_boolean_value(flag_key="dark", default_value=False)
`
	flags, edges := runFlagPass("python", "of_kwarg.py", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("kwarg-form OpenFeature key should not be attributed (honest-partial), got flags=%v edges=%v", flags, edges)
	}
}

// HONEST-PARTIAL (Python): plain environment-variable gating
// `os.environ.get("FEATURE_NEW_UI")` is NOT a recognised flag-SDK call, so it
// is deliberately not attributed — env reads are config consumption
// (DEPENDS_ON_CONFIG territory), not SDK-managed feature flags. Documents the
// env-gating gap behind the honest-partial framework cells.
func TestFeatureFlag_Python_EnvGating_NotMatched(t *testing.T) {
	src := `
def view(request):
    if os.environ.get("FEATURE_NEW_UI"):
        return new_ui(request)
    return old_ui(request)
`
	flags, edges := runFlagPass("python", "env_gate.py", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("env-var gating is not an SDK flag check, expected no output, got flags=%v edges=%v", flags, edges)
	}
}

// Split.io: client.getTreatment("exp-pricing") → feature:exp-pricing,
// attributed to the enclosing function, SDK subtype "split".
func TestFeatureFlag_Split_getTreatment(t *testing.T) {
	src := `
function pricing() {
  const t = client.getTreatment("exp-pricing");
  return t === "on" ? newPrice() : oldPrice();
}
`
	flags, edges := runFlagPass("javascript", "pricing.js", src)
	flag, ok := findFlag(flags, "exp-pricing")
	if !ok {
		t.Fatalf("expected feature:exp-pricing entity, got %v", flags)
	}
	if flag.ID != "feature:exp-pricing" {
		t.Errorf("flag ID = %q, want feature:exp-pricing", flag.ID)
	}
	if flag.Subtype != "split" {
		t.Errorf("flag SDK = %q, want split", flag.Subtype)
	}
	g, ok := findGate(edges, "exp-pricing")
	if !ok {
		t.Fatalf("expected GATED_BY for exp-pricing, got %v", edges)
	}
	if g.From != "Function:pricing" || g.To != "feature:exp-pricing" {
		t.Errorf("edge = %+v, want Function:pricing -> feature:exp-pricing", g)
	}
	if g.SDK != "split" {
		t.Errorf("GATED_BY sdk = %q, want split", g.SDK)
	}
}

// Split.io getTreatmentWithConfig is part of the same treatment family.
func TestFeatureFlag_Split_getTreatmentWithConfig(t *testing.T) {
	src := `
function home() {
  const r = splitClient.getTreatmentWithConfig('home-banner', attrs);
  return r.treatment;
}
`
	flags, edges := runFlagPass("typescript", "home.ts", src)
	if _, ok := findFlag(flags, "home-banner"); !ok {
		t.Fatalf("expected feature:home-banner entity, got %v", flags)
	}
	g, ok := findGate(edges, "home-banner")
	if !ok || g.From != "Function:home" || g.To != "feature:home-banner" {
		t.Fatalf("edge = %+v ok=%v, want Function:home -> feature:home-banner", g, ok)
	}
}

// Unleash React proxy hook: useFlag("beta-ui") → feature:beta-ui, SDK subtype
// "unleash-react".
func TestFeatureFlag_UnleashReact_useFlag(t *testing.T) {
	src := `
function Nav() {
  const beta = useFlag("beta-ui");
  return beta ? <NewNav/> : <OldNav/>;
}
`
	flags, edges := runFlagPass("typescript", "Nav.tsx", src)
	flag, ok := findFlag(flags, "beta-ui")
	if !ok {
		t.Fatalf("expected feature:beta-ui entity, got %v", flags)
	}
	if flag.Subtype != "unleash-react" {
		t.Errorf("flag SDK = %q, want unleash-react", flag.Subtype)
	}
	g, ok := findGate(edges, "beta-ui")
	if !ok || g.From != "Function:Nav" || g.To != "feature:beta-ui" {
		t.Fatalf("edge = %+v ok=%v, want Function:Nav -> feature:beta-ui", g, ok)
	}
}

// Generic custom wrapper: getFlag("legacy-import") → feature:legacy-import,
// SDK subtype "custom".
func TestFeatureFlag_Custom_getFlag(t *testing.T) {
	src := `
function importer() {
  if (flags.getFlag("legacy-import")) {
    return legacyImport();
  }
}
`
	flags, edges := runFlagPass("javascript", "importer.js", src)
	flag, ok := findFlag(flags, "legacy-import")
	if !ok {
		t.Fatalf("expected feature:legacy-import entity, got %v", flags)
	}
	if flag.Subtype != "custom" {
		t.Errorf("flag SDK = %q, want custom", flag.Subtype)
	}
	if g, ok := findGate(edges, "legacy-import"); !ok || g.From != "Function:importer" {
		t.Fatalf("edge = %+v ok=%v, want Function:importer", g, ok)
	}
}

// Generic custom wrapper, snake_case Python: feature_enabled("new-report").
func TestFeatureFlag_Custom_feature_enabled_python(t *testing.T) {
	src := `
def report(user):
    if feature_enabled("new-report"):
        return new_report(user)
    return old_report(user)
`
	flags, edges := runFlagPass("python", "report.py", src)
	if _, ok := findFlag(flags, "new-report"); !ok {
		t.Fatalf("expected feature:new-report entity, got %v", flags)
	}
	g, ok := findGate(edges, "new-report")
	if !ok || g.From != "Function:report" || g.To != "feature:new-report" {
		t.Fatalf("edge = %+v ok=%v, want Function:report -> feature:new-report", g, ok)
	}
}

// CONVERGENCE: Split.io and LaunchDarkly checking the SAME key string in two
// functions converge on ONE flag node (cross-provider key identity), with two
// distinct GATED_BY edges. The first provider to detect the key wins the
// Subtype, but the node id `feature:<key>` is shared.
func TestFeatureFlag_Convergence_CrossProvider_SameKey(t *testing.T) {
	src := `
function a() {
  return client.variation("shared-key", user, false);
}
function b() {
  return client.getTreatment("shared-key");
}
`
	flags, edges := runFlagPass("javascript", "shared.js", src)
	n := 0
	for _, f := range flags {
		if f.Name == "shared-key" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected exactly 1 shared-key node (cross-provider convergence), got %d", n)
	}
	froms := map[string]bool{}
	for _, e := range edges {
		if e.Flag == "shared-key" && e.To != "feature:shared-key" {
			t.Errorf("convergence: edge To = %q, want feature:shared-key", e.To)
		}
		if e.Flag == "shared-key" {
			froms[e.From] = true
		}
	}
	if !froms["Function:a"] || !froms["Function:b"] {
		t.Errorf("expected GATED_BY from both a and b, got %v", froms)
	}
}

// NEGATIVE: Split.io getTreatment with a dynamic (non-literal) key must NOT
// fabricate a flag entity or edge.
func TestFeatureFlag_Split_DynamicKey_NoFabrication(t *testing.T) {
	src := `
function gate(splitName) {
  return client.getTreatment(splitName);
}
`
	flags, edges := runFlagPass("javascript", "gate.js", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("dynamic Split key should yield no output, got flags=%v edges=%v", flags, edges)
	}
}

// NEGATIVE: a bare `flags['x']` subscript on an unrelated object must NOT be
// treated as a feature-flag check — it is too common in ordinary code and the
// pass deliberately does not match subscript access.
func TestFeatureFlag_Subscript_NotAFlag(t *testing.T) {
	src := `
function f(flags) {
  return flags['enabled'] && flags['x-value'];
}
`
	flags, edges := runFlagPass("javascript", "sub.js", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("bare subscript should yield no flag output, got flags=%v edges=%v", flags, edges)
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

// JAVA — LaunchDarkly camelCase boolVariation in a Spring handler. The Java
// SDK uses camelCase typed-variation methods (boolVariation / stringVariation
// / intVariation) with no underscore, distinct from the python snake_case
// bool_variation. Asserts the SPECIFIC key + SDK on the SPECIFIC handler.
func TestFeatureFlag_Java_LaunchDarkly_boolVariation(t *testing.T) {
	src := `
public class CheckoutController {
    @GetMapping("/checkout")
    public String checkout(User user) {
        if (ldClient.boolVariation("new-checkout", user, false)) {
            return newFlow();
        }
        return legacyFlow();
    }
}
`
	flags, edges := runFlagPass("java", "CheckoutController.java", src)

	flag, ok := findFlag(flags, "new-checkout")
	if !ok {
		t.Fatalf("expected feature:new-checkout entity, got flags=%v", flags)
	}
	if flag.ID != "feature:new-checkout" {
		t.Errorf("flag ID = %q, want feature:new-checkout", flag.ID)
	}
	if flag.Subtype != "launchdarkly" {
		t.Errorf("flag SDK subtype = %q, want launchdarkly", flag.Subtype)
	}
	g, ok := findGate(edges, "new-checkout")
	if !ok {
		t.Fatalf("expected GATED_BY for new-checkout, got %v", edges)
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

// JAVA — LaunchDarkly camelCase stringVariation (typed variant) also fires.
func TestFeatureFlag_Java_LaunchDarkly_stringVariation(t *testing.T) {
	src := `
public class ThemeService {
    public String theme(User user) {
        return ldClient.stringVariation("theme-flag", user, "dark");
    }
}
`
	flags, edges := runFlagPass("java", "ThemeService.java", src)
	flag, ok := findFlag(flags, "theme-flag")
	if !ok {
		t.Fatalf("expected feature:theme-flag entity, got %v", flags)
	}
	if flag.Subtype != "launchdarkly" {
		t.Errorf("flag SDK = %q, want launchdarkly", flag.Subtype)
	}
	g, ok := findGate(edges, "theme-flag")
	if !ok || g.From != "Function:theme" || g.To != "feature:theme-flag" {
		t.Fatalf("edge = %+v ok=%v, want Function:theme -> feature:theme-flag", g, ok)
	}
}

// JAVA — Unleash isEnabled (camelCase) attributed to the enclosing Java method.
func TestFeatureFlag_Java_Unleash_isEnabled(t *testing.T) {
	src := `
public class NavController {
    public String nav() {
        if (unleash.isEnabled("beta")) {
            return betaNav();
        }
        return lightNav();
    }
}
`
	flags, edges := runFlagPass("java", "NavController.java", src)
	flag, ok := findFlag(flags, "beta")
	if !ok {
		t.Fatalf("expected feature:beta entity, got %v", flags)
	}
	if flag.Subtype != "unleash" {
		t.Errorf("flag SDK = %q, want unleash", flag.Subtype)
	}
	g, ok := findGate(edges, "beta")
	if !ok || g.From != "Function:nav" || g.To != "feature:beta" {
		t.Fatalf("edge = %+v ok=%v, want Function:nav -> feature:beta", g, ok)
	}
}

// JAVA — OpenFeature getBooleanValue (camelCase) attributed to the Java method.
func TestFeatureFlag_Java_OpenFeature_getBooleanValue(t *testing.T) {
	src := `
public class UiService {
    public String render() {
        boolean on = client.getBooleanValue("dark-mode", false);
        return on ? darkUi() : lightUi();
    }
}
`
	flags, edges := runFlagPass("java", "UiService.java", src)
	flag, ok := findFlag(flags, "dark-mode")
	if !ok {
		t.Fatalf("expected feature:dark-mode entity, got %v", flags)
	}
	if flag.Subtype != "openfeature" {
		t.Errorf("flag SDK = %q, want openfeature", flag.Subtype)
	}
	g, ok := findGate(edges, "dark-mode")
	if !ok || g.From != "Function:render" || g.To != "feature:dark-mode" {
		t.Fatalf("edge = %+v ok=%v, want Function:render -> feature:dark-mode", g, ok)
	}
}

// JAVA — FF4j ff4j.check("flag") fires via the ff4j-receiver matcher.
func TestFeatureFlag_Java_FF4j_check(t *testing.T) {
	src := `
public class ImportService {
    public void run() {
        if (ff4j.check("legacy-import")) {
            legacyImport();
        }
    }
}
`
	flags, edges := runFlagPass("java", "ImportService.java", src)
	flag, ok := findFlag(flags, "legacy-import")
	if !ok {
		t.Fatalf("expected feature:legacy-import entity, got %v", flags)
	}
	if flag.ID != "feature:legacy-import" {
		t.Errorf("flag ID = %q, want feature:legacy-import", flag.ID)
	}
	if flag.Subtype != "ff4j" {
		t.Errorf("flag SDK = %q, want ff4j", flag.Subtype)
	}
	g, ok := findGate(edges, "legacy-import")
	if !ok {
		t.Fatalf("expected GATED_BY for legacy-import, got %v", edges)
	}
	if g.From != "Function:run" || g.To != "feature:legacy-import" {
		t.Errorf("edge = %+v, want Function:run -> feature:legacy-import", g)
	}
	if g.SDK != "ff4j" {
		t.Errorf("GATED_BY sdk = %q, want ff4j", g.SDK)
	}
}

// JAVA NEGATIVE — Togglz uses enum-based keys (Features.NEW_UI.isActive()) with
// NO string literal, so the pass correctly attributes nothing (honest-partial:
// only literal keys are emitted; an enum key is not a literal string argument).
func TestFeatureFlag_Java_Togglz_EnumKey_NoFabrication(t *testing.T) {
	src := `
public class FeatureGate {
    public boolean newUi() {
        return Features.NEW_UI.isActive();
    }
}
`
	flags, edges := runFlagPass("java", "FeatureGate.java", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("Togglz enum key should yield no output, got flags=%v edges=%v", flags, edges)
	}
}

// JAVA NEGATIVE — a non-flag boolean helper with a string arg that is NOT an FF
// SDK call must NOT fabricate a flag.
func TestFeatureFlag_Java_NonFlagBool_NoFabrication(t *testing.T) {
	src := `
public class Validator {
    public boolean ok(String name) {
        return validate("input") && check(name);
    }
}
`
	flags, edges := runFlagPass("java", "Validator.java", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("non-flag bool should yield no output, got flags=%v edges=%v", flags, edges)
	}
}

// JAVA NEGATIVE — LaunchDarkly boolVariation with a dynamic (non-literal) key
// must NOT fabricate a flag entity or edge (honest-partial, mirrors python).
func TestFeatureFlag_Java_LaunchDarkly_DynamicKey_NoFabrication(t *testing.T) {
	src := `
public class Gate {
    public boolean gate(String flagKey, User user) {
        return ldClient.boolVariation(flagKey, user, false);
    }
}
`
	flags, edges := runFlagPass("java", "Gate.java", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("dynamic Java key should yield no output, got flags=%v edges=%v", flags, edges)
	}
}

// C# — Microsoft.FeatureManagement: an ASP.NET Core controller action calling
// `await _featureManager.IsEnabledAsync("new-checkout")`. The Task-returning
// `Async` suffix breaks the Unleash `enabled\s*\(` matcher, so the dedicated
// FeatureManagement matcher (receiver-gated) attributes it to the enclosing
// async action with SDK subtype "featuremanagement". This is the canonical
// .NET SDK idiom the task requires.
func TestFeatureFlag_CSharp_FeatureManagement_IsEnabledAsync(t *testing.T) {
	src := `
public class CheckoutController : Controller
{
    private readonly IFeatureManager _featureManager;

    public async Task<IActionResult> Index()
    {
        if (await _featureManager.IsEnabledAsync("new-checkout"))
        {
            return NewCheckout();
        }
        return LegacyCheckout();
    }
}
`
	flags, edges := runFlagPass("csharp", "CheckoutController.cs", src)

	flag, ok := findFlag(flags, "new-checkout")
	if !ok {
		t.Fatalf("expected feature:new-checkout entity, got flags=%v", flags)
	}
	if flag.ID != "feature:new-checkout" {
		t.Errorf("flag ID = %q, want feature:new-checkout", flag.ID)
	}
	if flag.Subtype != "featuremanagement" {
		t.Errorf("flag SDK subtype = %q, want featuremanagement", flag.Subtype)
	}
	g, ok := findGate(edges, "new-checkout")
	if !ok {
		t.Fatalf("expected GATED_BY for new-checkout, got %v", edges)
	}
	if g.From != "Function:Index" {
		t.Errorf("GATED_BY FromID = %q, want Function:Index", g.From)
	}
	if g.To != "feature:new-checkout" {
		t.Errorf("GATED_BY ToID = %q, want feature:new-checkout", g.To)
	}
	if g.SDK != "featuremanagement" {
		t.Errorf("GATED_BY sdk = %q, want featuremanagement", g.SDK)
	}
}

// C# — Microsoft.FeatureManagement synchronous IsEnabled (no Async suffix) on a
// FeatureManager receiver also attributes to FeatureManagement, NOT Unleash:
// the receiver-gated FeatureManagement matcher runs before the generic Unleash
// matcher (first-match-wins), so `_featureManager.IsEnabled(...)` is correctly
// credited to the .NET SDK.
func TestFeatureFlag_CSharp_FeatureManagement_IsEnabled_Sync(t *testing.T) {
	src := `
public class NavService
{
    private readonly IFeatureManager _featureManager;

    public string Nav()
    {
        return _featureManager.IsEnabled("dark-mode") ? DarkNav() : LightNav();
    }
}
`
	flags, edges := runFlagPass("csharp", "NavService.cs", src)
	flag, ok := findFlag(flags, "dark-mode")
	if !ok {
		t.Fatalf("expected feature:dark-mode entity, got %v", flags)
	}
	if flag.Subtype != "featuremanagement" {
		t.Errorf("flag SDK = %q, want featuremanagement (not unleash)", flag.Subtype)
	}
	g, ok := findGate(edges, "dark-mode")
	if !ok || g.From != "Function:Nav" || g.To != "feature:dark-mode" {
		t.Fatalf("edge = %+v ok=%v, want Function:Nav -> feature:dark-mode", g, ok)
	}
	if g.SDK != "featuremanagement" {
		t.Errorf("GATED_BY sdk = %q, want featuremanagement", g.SDK)
	}
}

// C# — Microsoft.FeatureManagement `[FeatureGate("admin-panel")]` attribute
// declaratively gating an ASP.NET Core action. The attribute sits between the
// Login() and Dashboard() method headers; the nearest preceding header is
// Login, so it attributes to Function:Login. The value invariant asserted is
// the SPECIFIC flag key + SDK on the SPECIFIC declarative attribute and a real
// enclosing-function FromID.
func TestFeatureFlag_CSharp_FeatureManagement_FeatureGateAttribute(t *testing.T) {
	src := `
public class AdminController : Controller
{
    public IActionResult Login()
    {
        return View();
    }

    [FeatureGate("admin-panel")]
    public IActionResult Dashboard()
    {
        return View();
    }
}
`
	flags, edges := runFlagPass("csharp", "AdminController.cs", src)
	flag, ok := findFlag(flags, "admin-panel")
	if !ok {
		t.Fatalf("expected feature:admin-panel entity, got %v", flags)
	}
	if flag.ID != "feature:admin-panel" {
		t.Errorf("flag ID = %q, want feature:admin-panel", flag.ID)
	}
	if flag.Subtype != "featuremanagement" {
		t.Errorf("flag SDK = %q, want featuremanagement", flag.Subtype)
	}
	g, ok := findGate(edges, "admin-panel")
	if !ok {
		t.Fatalf("expected GATED_BY for admin-panel, got %v", edges)
	}
	if g.From != "Function:Login" {
		t.Errorf("GATED_BY FromID = %q, want Function:Login (nearest preceding method header)", g.From)
	}
	if g.To != "feature:admin-panel" {
		t.Errorf("GATED_BY ToID = %q, want feature:admin-panel", g.To)
	}
	if g.SDK != "featuremanagement" {
		t.Errorf("GATED_BY sdk = %q, want featuremanagement", g.SDK)
	}
}

// C# — LaunchDarkly PascalCase BoolVariation (.NET server SDK) attributes to
// the enclosing action with SDK subtype "launchdarkly". Confirms the existing
// case-insensitive LD matcher already catches the .NET PascalCase form (no new
// matcher needed for LD).
func TestFeatureFlag_CSharp_LaunchDarkly_BoolVariation_Pascal(t *testing.T) {
	src := `
public class BetaController : Controller
{
    public IActionResult Index(LdUser user)
    {
        if (_ldClient.BoolVariation("beta", user, false))
        {
            return BetaView();
        }
        return StableView();
    }
}
`
	flags, edges := runFlagPass("csharp", "BetaController.cs", src)
	flag, ok := findFlag(flags, "beta")
	if !ok {
		t.Fatalf("expected feature:beta entity, got %v", flags)
	}
	if flag.Subtype != "launchdarkly" {
		t.Errorf("flag SDK = %q, want launchdarkly", flag.Subtype)
	}
	g, ok := findGate(edges, "beta")
	if !ok || g.From != "Function:Index" || g.To != "feature:beta" {
		t.Fatalf("edge = %+v ok=%v, want Function:Index -> feature:beta", g, ok)
	}
	if g.SDK != "launchdarkly" {
		t.Errorf("GATED_BY sdk = %q, want launchdarkly", g.SDK)
	}
}

// C# — OpenFeature .NET PascalCase GetBooleanValue attributes to the enclosing
// method (case-insensitive OpenFeature matcher already catches it).
func TestFeatureFlag_CSharp_OpenFeature_GetBooleanValue_Pascal(t *testing.T) {
	src := `
public class UiService
{
    public bool Render()
    {
        return _client.GetBooleanValue("new-ui", false);
    }
}
`
	flags, edges := runFlagPass("csharp", "UiService.cs", src)
	flag, ok := findFlag(flags, "new-ui")
	if !ok {
		t.Fatalf("expected feature:new-ui entity, got %v", flags)
	}
	if flag.Subtype != "openfeature" {
		t.Errorf("flag SDK = %q, want openfeature", flag.Subtype)
	}
	g, ok := findGate(edges, "new-ui")
	if !ok || g.From != "Function:Render" || g.To != "feature:new-ui" {
		t.Fatalf("edge = %+v ok=%v, want Function:Render -> feature:new-ui", g, ok)
	}
}

// C# NEGATIVE — FeatureManagement with a dynamic (non-literal) key must NOT
// fabricate a flag entity or edge (honest-partial, mirrors java/python).
func TestFeatureFlag_CSharp_FeatureManagement_DynamicKey_NoFabrication(t *testing.T) {
	src := `
public class Gate
{
    public async Task<bool> Check(string flagName)
    {
        return await _featureManager.IsEnabledAsync(flagName);
    }
}
`
	flags, edges := runFlagPass("csharp", "Gate.cs", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("dynamic .NET key should yield no output, got flags=%v edges=%v", flags, edges)
	}
}

// C# NEGATIVE — a `.IsEnabled` property access on a NON-FeatureManager receiver
// (a UI control) is NOT attributed: the FeatureManagement matcher requires a
// `featureManager` receiver, and there is no `enabled(` call form for the
// Unleash matcher to catch either, so nothing is emitted.
func TestFeatureFlag_CSharp_NonFeatureManagerIsEnabled_NoFabrication(t *testing.T) {
	src := `
public class Widget
{
    public bool CanClick(Button button)
    {
        return button.IsEnabled;
    }
}
`
	flags, edges := runFlagPass("csharp", "Widget.cs", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("non-FeatureManager IsEnabled property should yield no output, got flags=%v edges=%v", flags, edges)
	}
}

// GrowthBook (JS/TS): gb.isOn("dark-mode") in renderNav() →
// renderNav GATED_BY feature:dark-mode, SDK subtype "growthbook".
func TestFeatureFlag_GrowthBook_isOn(t *testing.T) {
	src := `
function renderNav() {
  if (gb.isOn("dark-mode")) {
    return darkNav();
  }
  return lightNav();
}
`
	flags, edges := runFlagPass("typescript", "nav.ts", src)
	flag, ok := findFlag(flags, "dark-mode")
	if !ok {
		t.Fatalf("expected feature:dark-mode entity, got %v", flags)
	}
	if flag.ID != "feature:dark-mode" {
		t.Errorf("flag ID = %q, want feature:dark-mode", flag.ID)
	}
	if flag.Subtype != "growthbook" {
		t.Errorf("flag SDK = %q, want growthbook", flag.Subtype)
	}
	g, ok := findGate(edges, "dark-mode")
	if !ok {
		t.Fatalf("expected GATED_BY for dark-mode, got %v", edges)
	}
	if g.From != "Function:renderNav" || g.To != "feature:dark-mode" {
		t.Errorf("edge = %+v, want From=Function:renderNav To=feature:dark-mode", g)
	}
	if g.SDK != "growthbook" {
		t.Errorf("edge sdk = %q, want growthbook", g.SDK)
	}
}

// GrowthBook isOff + getFeatureValue method variants both attribute and
// converge on the growthbook subtype.
func TestFeatureFlag_GrowthBook_isOff_and_getFeatureValue(t *testing.T) {
	src := `
function banner() {
  if (growthbook.isOff("promo")) {
    return null;
  }
  const price = gb.getFeatureValue("price-banner", 0);
  return render(price);
}
`
	flags, edges := runFlagPass("typescript", "banner.ts", src)
	if f, ok := findFlag(flags, "promo"); !ok || f.Subtype != "growthbook" {
		t.Fatalf("expected feature:promo growthbook entity, got %v", flags)
	}
	if f, ok := findFlag(flags, "price-banner"); !ok || f.Subtype != "growthbook" {
		t.Fatalf("expected feature:price-banner growthbook entity, got %v", flags)
	}
	g1, ok := findGate(edges, "promo")
	if !ok || g1.From != "Function:banner" || g1.To != "feature:promo" {
		t.Errorf("promo edge = %+v ok=%v, want Function:banner -> feature:promo", g1, ok)
	}
	g2, ok := findGate(edges, "price-banner")
	if !ok || g2.From != "Function:banner" || g2.To != "feature:price-banner" {
		t.Errorf("price-banner edge = %+v ok=%v, want Function:banner -> feature:price-banner", g2, ok)
	}
}

// GrowthBook dynamic key (gb.isOn(flagName)) — honest-partial: no entity,
// no edge. A bare .isOn(...) on a non-gb receiver is likewise not a flag.
func TestFeatureFlag_GrowthBook_DynamicAndBareReceiver_NoFabrication(t *testing.T) {
	src := `
function f(flagName, button) {
  const a = gb.isOn(flagName);     // dynamic key — skipped
  const b = button.isOn("blink");  // not a GrowthBook receiver — skipped
}
`
	flags, edges := runFlagPass("typescript", "f.ts", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("dynamic key + non-gb receiver should yield no output, got flags=%v edges=%v", flags, edges)
	}
}

// ConfigCat (JS/TS): configCatClient.getValueAsync("isMyFeatureEnabled",
// false) → feature:isMyFeatureEnabled, SDK subtype "configcat".
func TestFeatureFlag_ConfigCat_getValueAsync(t *testing.T) {
	src := `
async function gate() {
  const on = await configCatClient.getValueAsync("isMyFeatureEnabled", false);
  if (on) {
    return newPath();
  }
}
`
	flags, edges := runFlagPass("typescript", "gate.ts", src)
	flag, ok := findFlag(flags, "isMyFeatureEnabled")
	if !ok {
		t.Fatalf("expected feature:isMyFeatureEnabled entity, got %v", flags)
	}
	if flag.ID != "feature:isMyFeatureEnabled" {
		t.Errorf("flag ID = %q, want feature:isMyFeatureEnabled", flag.ID)
	}
	if flag.Subtype != "configcat" {
		t.Errorf("flag SDK = %q, want configcat", flag.Subtype)
	}
	g, ok := findGate(edges, "isMyFeatureEnabled")
	if !ok {
		t.Fatalf("expected GATED_BY for isMyFeatureEnabled, got %v", edges)
	}
	if g.From != "Function:gate" || g.To != "feature:isMyFeatureEnabled" {
		t.Errorf("edge = %+v, want From=Function:gate To=feature:isMyFeatureEnabled", g)
	}
	if g.SDK != "configcat" {
		t.Errorf("edge sdk = %q, want configcat", g.SDK)
	}
}

// ConfigCat synchronous getValue with a configCat-flavoured receiver also
// attributes; a generic .getValue("x") on an unrelated receiver does not.
func TestFeatureFlag_ConfigCat_getValue_ReceiverGated(t *testing.T) {
	src := `
function pick(user) {
  const v = configCat.getValue("checkout-redesign", false, user); // matched
  const w = formData.getValue("email");                           // not a flag
  return v;
}
`
	flags, edges := runFlagPass("typescript", "pick.ts", src)
	if f, ok := findFlag(flags, "checkout-redesign"); !ok || f.Subtype != "configcat" {
		t.Fatalf("expected feature:checkout-redesign configcat entity, got %v", flags)
	}
	if _, ok := findFlag(flags, "email"); ok {
		t.Errorf("formData.getValue(\"email\") must not be a flag, got flags=%v", flags)
	}
	g, ok := findGate(edges, "checkout-redesign")
	if !ok || g.From != "Function:pick" || g.To != "feature:checkout-redesign" {
		t.Errorf("edge = %+v ok=%v, want Function:pick -> feature:checkout-redesign", g, ok)
	}
	if len(flags) != 1 || len(edges) != 1 {
		t.Errorf("expected exactly one flag/edge, got flags=%v edges=%v", flags, edges)
	}
}

// ---------------------------------------------------------------------------
// Elixir (#3628 area #17) — FunWithFlags / Flippant / Unleash bare `enabled?`.
//
// Elixir flag keys are idiomatically atoms (`:flag`); the leading `:` is
// stripped so an atom key converges on the same `feature:<key>` node a string
// key of the same name produces (mirroring Ruby symbol normalization). Elixir
// has no enclosing-function index in indexEnclosingFunctions (orm_queries.go),
// so the GATED_BY edge is file-scope-anchored (`File:<path>`) — the same
// fallback used in any language where the enclosing def can't be resolved. The
// FunWithFlags / Flippant / Unleash module receivers are required so the bare
// `.enabled?` predicate (ubiquitous in Elixir) is only attributed when it is
// the flag SDK API.
// ---------------------------------------------------------------------------

// ELIXIR — FunWithFlags.enabled?(:new_checkout, for: user): atom key normalized
// (leading `:` stripped) → feature:new_checkout, SDK subtype "funwithflags".
func TestFeatureFlag_Elixir_FunWithFlags_atom_for_actor(t *testing.T) {
	src := `
defmodule Checkout do
  def run(cart, user) do
    if FunWithFlags.enabled?(:new_checkout, for: user) do
      new_flow(cart)
    else
      legacy(cart)
    end
  end
end
`
	flags, edges := runFlagPass("elixir", "checkout.ex", src)

	flag, ok := findFlag(flags, "new_checkout")
	if !ok {
		t.Fatalf("expected feature:new_checkout entity (atom normalized), got flags=%v", flags)
	}
	if flag.ID != "feature:new_checkout" {
		t.Errorf("flag ID = %q, want feature:new_checkout", flag.ID)
	}
	if flag.Subtype != "funwithflags" {
		t.Errorf("flag SDK subtype = %q, want funwithflags", flag.Subtype)
	}

	g, ok := findGate(edges, "new_checkout")
	if !ok {
		t.Fatalf("expected GATED_BY for new_checkout, got %v", edges)
	}
	if g.From != "File:checkout.ex" {
		t.Errorf("GATED_BY FromID = %q, want File:checkout.ex (no Elixir enclosing-func index)", g.From)
	}
	if g.To != "feature:new_checkout" {
		t.Errorf("GATED_BY ToID = %q, want feature:new_checkout", g.To)
	}
	if g.SDK != "funwithflags" {
		t.Errorf("GATED_BY sdk = %q, want funwithflags", g.SDK)
	}
}

// ELIXIR — FunWithFlags.enabled?("beta"): string-key form also fires.
func TestFeatureFlag_Elixir_FunWithFlags_string_key(t *testing.T) {
	src := `
defmodule Page do
  def render do
    FunWithFlags.enabled?("beta")
  end
end
`
	flags, edges := runFlagPass("elixir", "page.ex", src)
	flag, ok := findFlag(flags, "beta")
	if !ok || flag.Subtype != "funwithflags" {
		t.Fatalf("expected feature:beta funwithflags entity, got %v", flags)
	}
	if _, ok := findGate(edges, "beta"); !ok {
		t.Fatalf("expected GATED_BY for beta, got %v", edges)
	}
}

// ELIXIR — Flippant.enabled?("flag", actor): string key, receiver-gated.
func TestFeatureFlag_Elixir_Flippant_enabled(t *testing.T) {
	src := `
defmodule Gate do
  def call(actor) do
    Flippant.enabled?("search_v2", actor)
  end
end
`
	flags, edges := runFlagPass("elixir", "gate.ex", src)
	flag, ok := findFlag(flags, "search_v2")
	if !ok {
		t.Fatalf("expected feature:search_v2 entity, got %v", flags)
	}
	if flag.Subtype != "flippant" {
		t.Errorf("flag SDK = %q, want flippant", flag.Subtype)
	}
	g, ok := findGate(edges, "search_v2")
	if !ok {
		t.Fatalf("expected GATED_BY for search_v2, got %v", edges)
	}
	if g.SDK != "flippant" {
		t.Errorf("GATED_BY sdk = %q, want flippant", g.SDK)
	}
}

// ELIXIR — Unleash.enabled?("beta"): the bare `enabled?` predicate (vs the Ruby
// SDK's `is_enabled?`), receiver-gated on the Unleash module → SDK "unleash".
func TestFeatureFlag_Elixir_Unleash_bare_enabled(t *testing.T) {
	src := `
defmodule Feed do
  def show do
    if Unleash.enabled?("beta") do
      :new
    else
      :stable
    end
  end
end
`
	flags, edges := runFlagPass("elixir", "feed.ex", src)
	flag, ok := findFlag(flags, "beta")
	if !ok {
		t.Fatalf("expected feature:beta entity, got %v", flags)
	}
	if flag.Subtype != "unleash" {
		t.Errorf("flag SDK = %q, want unleash", flag.Subtype)
	}
	if g, ok := findGate(edges, "beta"); !ok || g.SDK != "unleash" {
		t.Errorf("expected GATED_BY beta sdk=unleash, got %+v ok=%v", g, ok)
	}
}

// ELIXIR NEGATIVE — a bare `.enabled?` on a NON-FF receiver must NOT be
// attributed: the FunWithFlags / Flippant / Unleash matchers each require their
// module receiver, so `record.enabled?(:foo)` on an arbitrary struct emits
// nothing (the ubiquitous Elixir predicate stays unattributed).
func TestFeatureFlag_Elixir_NonFlagReceiver_NoFabrication(t *testing.T) {
	src := `
defmodule View do
  def visible?(record) do
    SomeMod.enabled?(:foo) and record.enabled?
  end
end
`
	flags, edges := runFlagPass("elixir", "view.ex", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("non-FF .enabled? receiver should yield no output, got flags=%v edges=%v", flags, edges)
	}
}

// ELIXIR NEGATIVE — a dynamic (non-literal) flag key must NOT fabricate a flag
// entity or edge (honest-partial, mirrors the other languages).
func TestFeatureFlag_Elixir_DynamicKey_NoFabrication(t *testing.T) {
	src := `
defmodule Gate do
  def check(flag, user) do
    FunWithFlags.enabled?(flag, for: user)
  end
end
`
	flags, edges := runFlagPass("elixir", "dyn.ex", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("dynamic FunWithFlags key should yield no output, got flags=%v edges=%v", flags, edges)
	}
}

// PHP — Laravel Pennant facade: `Feature::active('new-checkout')` →
// feature:new-checkout, SDK subtype "laravel-pennant", attributed to the
// enclosing PHP function.
func TestFeatureFlag_PHP_Pennant_Active(t *testing.T) {
	src := `<?php
function checkout() {
    if (Feature::active('new-checkout')) {
        return newCheckout();
    }
    return legacyCheckout();
}
`
	flags, edges := runFlagPass("php", "checkout.php", src)
	flag, ok := findFlag(flags, "new-checkout")
	if !ok {
		t.Fatalf("expected feature:new-checkout entity, got %v", flags)
	}
	if flag.ID != "feature:new-checkout" {
		t.Errorf("flag ID = %q, want feature:new-checkout", flag.ID)
	}
	if flag.Subtype != "laravel-pennant" {
		t.Errorf("flag SDK = %q, want laravel-pennant", flag.Subtype)
	}
	g, ok := findGate(edges, "new-checkout")
	if !ok {
		t.Fatalf("expected GATED_BY for new-checkout, got %v", edges)
	}
	if g.From != "Function:checkout" || g.To != "feature:new-checkout" {
		t.Errorf("edge = %+v, want From=Function:checkout To=feature:new-checkout", g)
	}
	if g.SDK != "laravel-pennant" {
		t.Errorf("edge sdk = %q, want laravel-pennant", g.SDK)
	}
}

// PHP — Laravel Pennant `inactive` predicate and the scoped
// `Feature::for($u)->active('key')` form both attribute to the laravel-pennant
// subtype.
func TestFeatureFlag_PHP_Pennant_Inactive_And_Scoped(t *testing.T) {
	src := `<?php
function render($u) {
    if (Feature::inactive('old-ui')) {
        return null;
    }
    if (Feature::for($u)->active('beta-ui')) {
        return beta();
    }
}
`
	flags, edges := runFlagPass("php", "render.php", src)
	if f, ok := findFlag(flags, "old-ui"); !ok || f.Subtype != "laravel-pennant" {
		t.Fatalf("expected feature:old-ui laravel-pennant entity, got %v", flags)
	}
	if f, ok := findFlag(flags, "beta-ui"); !ok || f.Subtype != "laravel-pennant" {
		t.Fatalf("expected feature:beta-ui laravel-pennant entity, got %v", flags)
	}
	g1, ok := findGate(edges, "old-ui")
	if !ok || g1.From != "Function:render" || g1.To != "feature:old-ui" {
		t.Errorf("old-ui edge = %+v ok=%v, want Function:render -> feature:old-ui", g1, ok)
	}
	g2, ok := findGate(edges, "beta-ui")
	if !ok || g2.From != "Function:render" || g2.To != "feature:beta-ui" {
		t.Errorf("beta-ui edge = %+v ok=%v, want Function:render -> feature:beta-ui", g2, ok)
	}
}

// PHP — Laravel Pennant global helper: `feature('dark-mode')` →
// feature:dark-mode, laravel-pennant. The bare lowercase helper is distinct
// from the capital-`Feature::` facade and from the generic feature_enabled
// wrapper.
func TestFeatureFlag_PHP_Pennant_Helper(t *testing.T) {
	src := `<?php
function nav() {
    if (feature('dark-mode')) {
        return darkNav();
    }
    return lightNav();
}
`
	flags, edges := runFlagPass("php", "nav.php", src)
	flag, ok := findFlag(flags, "dark-mode")
	if !ok || flag.Subtype != "laravel-pennant" {
		t.Fatalf("expected feature:dark-mode laravel-pennant entity, got %v", flags)
	}
	g, ok := findGate(edges, "dark-mode")
	if !ok || g.From != "Function:nav" || g.To != "feature:dark-mode" {
		t.Errorf("edge = %+v ok=%v, want Function:nav -> feature:dark-mode", g, ok)
	}
	if g.SDK != "laravel-pennant" {
		t.Errorf("edge sdk = %q, want laravel-pennant", g.SDK)
	}
}

// PHP — Symfony Flagception: `$featureManager->isActive('promo')` →
// feature:promo, SDK subtype "flagception". Receiver-gated on a flag/feature
// receiver token.
func TestFeatureFlag_PHP_Flagception_IsActive(t *testing.T) {
	src := `<?php
class PromoController {
    public function show() {
        if ($this->featureManager->isActive('promo')) {
            return $this->promoView();
        }
    }
}
`
	flags, edges := runFlagPass("php", "promo.php", src)
	flag, ok := findFlag(flags, "promo")
	if !ok {
		t.Fatalf("expected feature:promo entity, got %v", flags)
	}
	if flag.Subtype != "flagception" {
		t.Errorf("flag SDK = %q, want flagception", flag.Subtype)
	}
	g, ok := findGate(edges, "promo")
	if !ok || g.To != "feature:promo" {
		t.Fatalf("expected GATED_BY for promo, got %v", edges)
	}
	if g.From != "Function:show" {
		t.Errorf("edge From = %q, want Function:show", g.From)
	}
	if g.SDK != "flagception" {
		t.Errorf("edge sdk = %q, want flagception", g.SDK)
	}
}

// PHP NEGATIVE — `$model->active('x')` (no Pennant `Feature::` facade) and
// `$model->isActive('x')` (no flag/feature receiver token) are ordinary
// model predicates, NOT feature-flag checks: no entity, no edge.
func TestFeatureFlag_PHP_NonFlagPredicates_NoFabrication(t *testing.T) {
	src := `<?php
function f($model, $user) {
    $a = $model->active('x');     // not Feature:: facade — skipped
    $b = $user->isActive('y');    // no flag/feature receiver — skipped
    $c = $repo->feature('z');     // member-call feature(), not the helper — skipped
}
`
	flags, edges := runFlagPass("php", "f.php", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("non-flag PHP predicates should yield no output, got flags=%v edges=%v", flags, edges)
	}
}

// PHP NEGATIVE — a dynamic (non-literal) flag key must NOT fabricate a flag
// entity or edge (honest-partial, mirrors the other languages).
func TestFeatureFlag_PHP_DynamicKey_NoFabrication(t *testing.T) {
	src := `<?php
function gate($flagName, $k) {
    if (Feature::active($flagName)) { return; }
    if (feature($k)) { return; }
}
`
	flags, edges := runFlagPass("php", "gate.php", src)
	if len(flags) != 0 || len(edges) != 0 {
		t.Errorf("dynamic PHP flag key should yield no output, got flags=%v edges=%v", flags, edges)
	}
}
