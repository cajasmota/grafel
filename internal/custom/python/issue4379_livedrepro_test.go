package python_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"

	// Register the regex framework extractors under test (Django global wiring).
	_ "github.com/cajasmota/grafel/internal/custom/python"
	// Register the tree-sitter base Python extractor so the middleware/auth
	// classes get their real module-qualified QualifiedName, which is what the
	// settings dotted path resolves against.
	_ "github.com/cajasmota/grafel/internal/extractors/python"
)

// Issue #4379 LIVE-REPRO — Django global cross-cutting wiring.
//
// Byte-copies of the REAL acme_core legacy Django backend are committed under
// testdata/issue4379:
//
//   - settings.py — declares MIDDLEWARE (incl. custom
//     core.middleware.*.*Middleware classes) and REST_FRAMEWORK with
//     DEFAULT_AUTHENTICATION_CLASSES / DEFAULT_RENDERER_CLASSES as TUPLES.
//   - core/middleware/{token_authentication,current_user,performance_debug}.py —
//     the real custom middleware classes referenced by dotted path in MIDDLEWARE.
//
// PRE-FIX: MIDDLEWARE / AUTHENTICATION_BACKENDS / REST_FRAMEWORK default classes
// are string dotted paths producing NO edge — the custom middleware/auth classes
// have no inbound edge and look orphan / dead, and the app-wide scope is
// invisible. (The legacy DRF parser also silently missed the tuple form.)
//
// POST-FIX: a synthetic `django_settings` entity emits one global=true USES edge
// per bound class, ToID = the verbatim dotted path, which resolves through the
// REAL resolver QualifiedName index to the middleware class entity emitted by the
// base extractor.

func loadRepro4379(t *testing.T, base, repoPath string) extreg.FileInput {
	t.Helper()
	p := filepath.Join("testdata", "issue4379", base)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read repro %s: %v", p, err)
	}
	return extreg.FileInput{Path: repoPath, Language: "python", Content: b}
}

// runMerged4379 runs the base python extractor + every registered custom
// python_* extractor over one file and merges them the way the real pipeline
// does, assigning deterministic IDs.
func runMerged4379(t *testing.T, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	base, ok := extreg.Get("python")
	if !ok {
		t.Fatal("base python extractor not registered")
	}
	baseEnts, err := base.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("base extract %s: %v", file.Path, err)
	}
	customEnts, errs := extractors.RunCustomExtractors(context.Background(), file)
	for _, e := range errs {
		t.Fatalf("custom extract %s: %v", file.Path, e)
	}
	merged := extractors.MergeWithCustom(baseEnts, customEnts)
	for i := range merged {
		if merged[i].ID == "" {
			merged[i].ID = merged[i].ComputeID()
		}
	}
	return merged
}

func globalUsesEdges(ents []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindUses) && r.Properties.Get("global") == "true" {
				out = append(out, r)
			}
		}
	}
	return out
}

// inboundUses reports whether some entity declares a USES edge whose ToID names
// the given dotted path / target id.
func hasGlobalUses(ents []types.EntityRecord, dotted, role string) (types.RelationshipRecord, bool) {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != string(types.RelationshipKindUses) {
				continue
			}
			// Match on dotted_path (stable across the late-binding rewrite that
			// mutates ToID into a hex id), falling back to ToID for clarity.
			if (r.Properties.Get("dotted_path") == dotted || r.ToID == dotted) &&
				r.Properties.Get("global") == "true" &&
				(role == "" || r.Properties.Get("di_role") == role) {
				return r, true
			}
		}
	}
	return types.RelationshipRecord{}, false
}

// TestIssue4379_RealAcmeSettings_GlobalWiring runs the REAL acme_core
// settings.py + its custom middleware classes through the actual merged
// extract+resolve pipeline.
//
// RED (pre-fix): no USES edge exists for the custom middleware dotted path, so
// TokenAuthenticationMiddleware / CurrentUserMiddleware / PerformanceDebugMiddleware
// are orphan-of-this-edge.
//
// GREEN (post-fix): the dotted path is USES-linked global=true and resolves to
// the real middleware class entity through resolve.BuildIndex.
func TestIssue4379_RealAcmeSettings_GlobalWiring(t *testing.T) {
	settings := loadRepro4379(t, "settings.py", "acme_core/settings.py")
	mw1 := loadRepro4379(t, "core/middleware/token_authentication.py", "core/middleware/token_authentication.py")
	mw2 := loadRepro4379(t, "core/middleware/current_user.py", "core/middleware/current_user.py")
	mw3 := loadRepro4379(t, "core/middleware/performance_debug.py", "core/middleware/performance_debug.py")

	var all []types.EntityRecord
	settingsEnts := runMerged4379(t, settings)
	all = append(all, settingsEnts...)
	all = append(all, runMerged4379(t, mw1)...)
	all = append(all, runMerged4379(t, mw2)...)
	all = append(all, runMerged4379(t, mw3)...)

	globals := globalUsesEdges(settingsEnts)
	if len(globals) == 0 {
		t.Fatal("RED: no global USES edges emitted from settings.py")
	}
	t.Logf("emitted %d global USES edges from acme_core settings.py", len(globals))

	// --- MIDDLEWARE: the three custom middleware classes must be linked AND
	// resolve to the real class entities through the symbol table. ---
	customMiddleware := []struct {
		dotted string
		class  string
	}{
		{"core.middleware.token_authentication.TokenAuthenticationMiddleware", "TokenAuthenticationMiddleware"},
		{"core.middleware.current_user.CurrentUserMiddleware", "CurrentUserMiddleware"},
		{"core.middleware.performance_debug.PerformanceDebugMiddleware", "PerformanceDebugMiddleware"},
	}

	idx := resolve.BuildIndex(all)

	// PRE-PASS (RED): the two MiddlewareMixin subclasses lose their
	// QualifiedName during MergeWithCustom (re-emitted as CBV endpoints), so the
	// dotted path does not resolve via byQualifiedName alone — proving the edge
	// would dangle without the late-binding pass.
	preResolved := 0
	for _, cm := range customMiddleware {
		if id, ok := idx.Lookup(cm.dotted); ok && id != "" {
			preResolved++
		}
	}
	t.Logf("pre-late-binding: %d/%d custom middleware dotted paths resolve via byQualifiedName",
		preResolved, len(customMiddleware))

	// Run the real late-binding resolver pass (the one wired into the pipeline)
	// over `all` so the merge-dropped-QualifiedName classes resolve by unique
	// leaf name. This mutates the USES edge ToIDs in `all` in place.
	rew := idx.ResolveDjangoGlobalWiringRefs(all)
	t.Logf("django-global-wiring late-binding rewrote %d edges", rew)
	if rew == 0 {
		t.Error("late-binding pass rewrote 0 edges (expected merge-dropped middleware to be rebound)")
	}

	resolvedBefore := 0
	for _, cm := range customMiddleware {
		// Edge metadata (read from the unmutated settingsEnts copy).
		r, ok := hasGlobalUses(settingsEnts, cm.dotted, "middleware")
		if !ok {
			t.Errorf("expected global middleware USES edge for %s", cm.dotted)
			continue
		}
		if r.Properties.Get("order") == "" {
			t.Errorf("%s: expected an order index on MIDDLEWARE edge", cm.dotted)
		}
		if r.Properties.Get("class_name") != cm.class {
			t.Errorf("%s: expected class_name=%s, got %q", cm.dotted, cm.class, r.Properties.Get("class_name"))
		}
		// Post-pass: the edge in `all` must now point at the real class entity's
		// hex ID, and that ID must belong to an entity named like the class.
		toID := globalEdgeToID(all, cm.dotted)
		if toID == "" || !isHexLike(toID) {
			t.Errorf("%s: dotted path did not bind to a real class entity after late-binding (toID=%q)", cm.dotted, toID)
			continue
		}
		if got := entityNameByID(all, toID); got != cm.class {
			t.Errorf("%s: bound to entity %q, want a node named %s", cm.dotted, got, cm.class)
			continue
		}
		resolvedBefore++
	}
	t.Logf("resolved %d/%d custom middleware dotted paths against real class entities",
		resolvedBefore, len(customMiddleware))
	if resolvedBefore != len(customMiddleware) {
		t.Errorf("only %d/%d custom middleware classes connected (want all)", resolvedBefore, len(customMiddleware))
	}

	// Built-in / third-party middleware are still linked (resolve to External
	// later in the pipeline); just assert the edge is present and ordered.
	if _, ok := hasGlobalUses(settingsEnts, "django.middleware.security.SecurityMiddleware", "middleware"); !ok {
		t.Error("expected global middleware USES edge for django.middleware.security.SecurityMiddleware")
	}

	// Commented-out middleware (`# "core.middleware.logging.LoggingMiddleware"`)
	// must NOT be emitted as live wiring.
	if _, ok := hasGlobalUses(settingsEnts, "core.middleware.logging.LoggingMiddleware", "middleware"); ok {
		t.Error("commented-out LoggingMiddleware must not produce a global USES edge")
	}

	// --- REST_FRAMEWORK DEFAULT_*_CLASSES (declared as TUPLES in this file). ---
	drfChecks := []struct {
		dotted string
		role   string
	}{
		{"rest_framework.authentication.TokenAuthentication", "authentication"},
		{"django_cognito_jwt.JSONWebTokenAuthentication", "authentication"},
		{"rest_framework.renderers.JSONRenderer", "renderer"},
	}
	for _, c := range drfChecks {
		if _, ok := hasGlobalUses(settingsEnts, c.dotted, c.role); !ok {
			t.Errorf("expected global %s USES edge for %s (DRF tuple bucket)", c.role, c.dotted)
		}
	}

	// MIDDLEWARE order must be monotonically increasing and the custom debug
	// middleware (first in the real list) must precede the security middleware.
	ordOf := func(dotted string) string {
		r, _ := hasGlobalUses(settingsEnts, dotted, "middleware")
		return r.Properties.Get("order")
	}
	if ordOf("core.middleware.performance_debug.PerformanceDebugMiddleware") >=
		ordOf("django.middleware.security.SecurityMiddleware") {
		t.Error("MIDDLEWARE order not preserved (PerformanceDebug should precede Security)")
	}
}

// globalEdgeToID returns the (post-pass) ToID of the global USES edge whose
// dotted_path matches dotted, or "" if absent.
func globalEdgeToID(ents []types.EntityRecord, dotted string) string {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindUses) &&
				r.Properties.Get("global") == "true" && r.Properties.Get("dotted_path") == dotted {
				return r.ToID
			}
		}
	}
	return ""
}

func entityNameByID(ents []types.EntityRecord, id string) string {
	for _, e := range ents {
		if e.ID == id {
			return e.Name
		}
	}
	return ""
}

// isHexLike reports whether s looks like a resolved hex entity ID (all
// lowercase hex digits, non-trivial length) rather than a dotted-path stub.
func isHexLike(s string) bool {
	if len(s) < 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// TestIssue4379_NonSettingsFile_NoGlobalEdges ensures a plain Django module
// (not a settings file) does not fabricate global wiring.
func TestIssue4379_NonSettingsFile_NoGlobalEdges(t *testing.T) {
	mw := loadRepro4379(t, "core/middleware/token_authentication.py", "core/middleware/token_authentication.py")
	ents := runMerged4379(t, mw)
	if g := globalUsesEdges(ents); len(g) != 0 {
		t.Errorf("middleware module should not emit global wiring edges, got %d", len(g))
	}
	for _, e := range ents {
		if e.Name == "django_settings" {
			t.Error("non-settings module should not emit a django_settings entity")
		}
	}
}
