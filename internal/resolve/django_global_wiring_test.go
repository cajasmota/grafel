// django_global_wiring_test.go — unit tests for the Django global-wiring
// late-binding resolver pass (ResolveDjangoGlobalWiringRefs, issue #4379) and
// for the laziness of its whole-corpus bare-Name index (issue #5986).
//
// The bare-Name index (byNameSrc) serves ONLY Strategy 3 (module-qualified leaf
// disambiguation). Building it eagerly cost ~42-75 MB live on a 427k-entity
// corpus even on repos that contain no Django global wiring at all. These tests
// pin two things at once:
//
//  1. the index is materialised only when Strategy 3 is actually reached, and
//     at most once across the whole pass (never per-edge);
//  2. when Strategy 3 IS reached, it still selects exactly the same entity ID
//     the eager implementation selected — including uniqueID's ambiguity
//     semantics and the SCOPE.Pattern de-prioritisation.
package resolve

import (
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// globalWiringUsesEdge builds a USES relationship shaped exactly like the ones
// the Django settings extractor emits for MIDDLEWARE / AUTHENTICATION_BACKENDS
// / REST_FRAMEWORK DEFAULT_*_CLASSES entries: ToID is the verbatim dotted path,
// Properties carry global=true, dotted_path and the leaf class_name.
func globalWiringUsesEdge(dotted, className string) types.RelationshipRecord {
	return types.RelationshipRecord{
		ToID: dotted,
		Kind: string(types.RelationshipKindUses),
		// types.Props is a binary-searched, key-SORTED slice: entries must be
		// listed in key order or Get misses.
		Properties: types.Props{
			{K: "class_name", V: className},
			{K: "dotted_path", V: dotted},
			{K: "global", V: "true"},
		},
	}
}

// djangoSettingsRecord wraps edges in the synthetic django_settings entity.
func djangoSettingsRecord(edges ...types.RelationshipRecord) types.EntityRecord {
	return types.EntityRecord{
		ID:            "django-settings-id",
		Name:          "django_settings",
		Kind:          "SCOPE.Configuration",
		SourceFile:    "config/settings.py",
		Language:      "python",
		Relationships: edges,
	}
}

// TestResolveDjangoGlobalWiringRefs_Strategy3ModuleQualified is the important
// test: it exercises the module-qualified leaf disambiguation for real. Two
// distinct classes share the leaf name AuthMiddleware in different Django apps,
// so the bare-Name lookup (Strategy 2) is ambiguous and must miss; neither
// carries a QualifiedName, so Strategy 1 misses too. Only Strategy 3 — matching
// the dotted path's module against the source-file-derived Python module — can
// pick the right one.
func TestResolveDjangoGlobalWiringRefs_Strategy3ModuleQualified(t *testing.T) {
	records := []types.EntityRecord{
		{
			ID: "id-app-a", Name: "AuthMiddleware", Kind: "SCOPE.Component",
			Subtype: "class", SourceFile: "apps/a/middleware.py", Language: "python",
		},
		{
			ID: "id-app-b", Name: "AuthMiddleware", Kind: "SCOPE.Component",
			Subtype: "class", SourceFile: "apps/b/middleware.py", Language: "python",
		},
		djangoSettingsRecord(globalWiringUsesEdge("apps.a.middleware.AuthMiddleware", "AuthMiddleware")),
	}

	idx := BuildIndex(records)

	// Guard the premise: Strategies 1 and 2 must both miss, otherwise this
	// test would silently stop covering Strategy 3.
	if id, ok := idx.Lookup("apps.a.middleware.AuthMiddleware"); ok && id != "" {
		t.Fatalf("premise broken: Strategy 1 resolved the dotted path to %q", id)
	}
	if id, ok := idx.Lookup("AuthMiddleware"); ok && id != "" {
		t.Fatalf("premise broken: Strategy 2 resolved the ambiguous leaf to %q", id)
	}

	rewrites, builds := idx.resolveDjangoGlobalWiringRefs(records)
	if rewrites != 1 {
		t.Errorf("rewrites = %d, want 1", rewrites)
	}
	if got := records[2].Relationships[0].ToID; got != "id-app-a" {
		t.Errorf("ToID = %q, want id-app-a", got)
	}
	if builds != 1 {
		t.Errorf("name-index builds = %d, want 1 (Strategy 3 was reached)", builds)
	}
}

// TestResolveDjangoGlobalWiringRefs_NoGlobalEdges_NeverBuildsNameIndex is the
// memory regression guard: a corpus with no Django global-wiring edges must
// never materialise the whole-corpus bare-Name index.
func TestResolveDjangoGlobalWiringRefs_NoGlobalEdges_NeverBuildsNameIndex(t *testing.T) {
	records := []types.EntityRecord{
		{
			ID: "id-1", Name: "Handler", Kind: "SCOPE.Component",
			SourceFile: "svc/handler.go", Language: "go",
			Relationships: []types.RelationshipRecord{
				// A plain USES edge with no global marker.
				{ToID: "id-2", Kind: string(types.RelationshipKindUses)},
				// A CALLS edge, wrong kind entirely.
				{ToID: "some.dotted.Path", Kind: string(types.RelationshipKindCalls)},
			},
		},
		{
			ID: "id-2", Name: "Store", Kind: "SCOPE.Component",
			SourceFile: "svc/store.go", Language: "go",
		},
	}

	idx := BuildIndex(records)
	rewrites, builds := idx.resolveDjangoGlobalWiringRefs(records)
	if rewrites != 0 {
		t.Errorf("rewrites = %d, want 0", rewrites)
	}
	if builds != 0 {
		t.Errorf("name-index builds = %d, want 0 (no global wiring edges exist)", builds)
	}
}

// TestResolveDjangoGlobalWiringRefs_EarlyStrategies_NeverBuildNameIndex checks
// that global-wiring edges resolvable by Strategy 1 (QualifiedName) or
// Strategy 2 (unique leaf name) short-circuit before the index is needed.
func TestResolveDjangoGlobalWiringRefs_EarlyStrategies_NeverBuildNameIndex(t *testing.T) {
	records := []types.EntityRecord{
		{
			ID: "id-qualified", Name: "TraceMiddleware", Kind: "SCOPE.Component",
			QualifiedName: "apps.obs.middleware.TraceMiddleware",
			SourceFile:    "apps/obs/middleware.py", Language: "python",
		},
		{
			ID: "id-unique-leaf", Name: "AuditBackend", Kind: "SCOPE.Component",
			SourceFile: "apps/audit/backends.py", Language: "python",
		},
		djangoSettingsRecord(
			// Strategy 1: resolves via QualifiedName.
			globalWiringUsesEdge("apps.obs.middleware.TraceMiddleware", "TraceMiddleware"),
			// Strategy 2: dotted path misses, but the leaf name is globally unique.
			globalWiringUsesEdge("thirdparty.vendored.AuditBackend", "AuditBackend"),
			// Skipped before any strategy: no dotted_path property.
			types.RelationshipRecord{
				ToID: "nope.Whatever", Kind: string(types.RelationshipKindUses),
				Properties: types.Props{{K: "global", V: "true"}},
			},
		),
	}

	idx := BuildIndex(records)
	rewrites, builds := idx.resolveDjangoGlobalWiringRefs(records)
	if rewrites != 2 {
		t.Errorf("rewrites = %d, want 2", rewrites)
	}
	if got := records[2].Relationships[0].ToID; got != "id-qualified" {
		t.Errorf("edge 0 ToID = %q, want id-qualified", got)
	}
	if got := records[2].Relationships[1].ToID; got != "id-unique-leaf" {
		t.Errorf("edge 1 ToID = %q, want id-unique-leaf", got)
	}
	if builds != 0 {
		t.Errorf("name-index builds = %d, want 0 (Strategy 3 never reached)", builds)
	}
}

// TestResolveDjangoGlobalWiringRefs_NameIndexBuiltAtMostOnce pins the O(entities)
// bound: a naive lazy build placed inside the edge loop would rebuild the map
// for every qualifying edge, turning a memory win into an O(edges × entities)
// time regression. Many Strategy-3 edges, exactly one build.
func TestResolveDjangoGlobalWiringRefs_NameIndexBuiltAtMostOnce(t *testing.T) {
	const apps = 12

	var records []types.EntityRecord
	var edges []types.RelationshipRecord
	for i := 0; i < apps; i++ {
		// Every app defines a class with the SAME leaf name, so the bare-name
		// lookup is ambiguous for all of them and every edge reaches Strategy 3.
		records = append(records, types.EntityRecord{
			ID:      fmt.Sprintf("id-app-%d", i),
			Name:    "SharedMiddleware",
			Kind:    "SCOPE.Component",
			Subtype: "class",
			// Also give each app a second, differently-named ambiguous class so
			// the corpus is not degenerate.
			SourceFile: fmt.Sprintf("apps/app%d/middleware.py", i),
			Language:   "python",
		})
		edges = append(edges, globalWiringUsesEdge(
			fmt.Sprintf("apps.app%d.middleware.SharedMiddleware", i), "SharedMiddleware"))
	}
	settingsIdx := len(records)
	records = append(records, djangoSettingsRecord(edges...))

	idx := BuildIndex(records)
	rewrites, builds := idx.resolveDjangoGlobalWiringRefs(records)
	if rewrites != apps {
		t.Errorf("rewrites = %d, want %d", rewrites, apps)
	}
	if builds != 1 {
		t.Errorf("name-index builds = %d, want exactly 1 across %d Strategy-3 edges", builds, apps)
	}
	for i := 0; i < apps; i++ {
		want := fmt.Sprintf("id-app-%d", i)
		if got := records[settingsIdx].Relationships[i].ToID; got != want {
			t.Errorf("edge %d ToID = %q, want %q", i, got, want)
		}
	}
}

// TestResolveDjangoGlobalWiringRefs_Strategy3AmbiguousSameModule pins uniqueID's
// ambiguity semantics: two DISTINCT entity IDs whose source files derive the
// same Python module leave the edge unresolved (External synthesis handles it
// downstream). The index is still built — Strategy 3 was reached — it just
// declines to pick.
func TestResolveDjangoGlobalWiringRefs_Strategy3AmbiguousSameModule(t *testing.T) {
	dotted := "apps.a.middleware.AuthMiddleware"
	records := []types.EntityRecord{
		{
			ID: "id-dup-1", Name: "AuthMiddleware", Kind: "SCOPE.Component",
			SourceFile: "apps/a/middleware.py", Language: "python",
		},
		{
			ID: "id-dup-2", Name: "AuthMiddleware", Kind: "SCOPE.Operation",
			SourceFile: "apps/a/middleware.py", Language: "python",
		},
		djangoSettingsRecord(globalWiringUsesEdge(dotted, "AuthMiddleware")),
	}

	idx := BuildIndex(records)
	if id, ok := idx.Lookup("AuthMiddleware"); ok && id != "" {
		t.Fatalf("premise broken: Strategy 2 resolved the leaf to %q", id)
	}
	rewrites, builds := idx.resolveDjangoGlobalWiringRefs(records)
	if rewrites != 0 {
		t.Errorf("rewrites = %d, want 0 (two distinct IDs in the same module is ambiguous)", rewrites)
	}
	if got := records[2].Relationships[0].ToID; got != dotted {
		t.Errorf("ToID = %q, want it left as the dotted path %q", got, dotted)
	}
	if builds != 1 {
		t.Errorf("name-index builds = %d, want 1 (Strategy 3 was reached and declined)", builds)
	}
}

// TestResolveDjangoGlobalWiringRefs_Strategy3PrefersDefinitionOverPattern pins
// the SCOPE.Pattern de-prioritisation: when the same module holds both a
// synthetic SCOPE.Pattern anchor and a real definition node for the leaf,
// the definition wins even though allIDs would be ambiguous.
func TestResolveDjangoGlobalWiringRefs_Strategy3PrefersDefinitionOverPattern(t *testing.T) {
	records := []types.EntityRecord{
		{
			ID: "id-pattern", Name: "CacheMiddleware", Kind: "SCOPE.Pattern",
			SourceFile: "apps/a/middleware.py", Language: "python",
		},
		{
			ID: "id-definition", Name: "CacheMiddleware", Kind: "SCOPE.Operation",
			SourceFile: "apps/a/middleware.py", Language: "python",
		},
		// A same-named class in another app keeps the bare-name lookup ambiguous.
		{
			ID: "id-other-app", Name: "CacheMiddleware", Kind: "SCOPE.Component",
			SourceFile: "apps/b/middleware.py", Language: "python",
		},
		djangoSettingsRecord(globalWiringUsesEdge("apps.a.middleware.CacheMiddleware", "CacheMiddleware")),
	}

	idx := BuildIndex(records)
	rewrites, builds := idx.resolveDjangoGlobalWiringRefs(records)
	if rewrites != 1 {
		t.Fatalf("rewrites = %d, want 1", rewrites)
	}
	if got := records[3].Relationships[0].ToID; got != "id-definition" {
		t.Errorf("ToID = %q, want id-definition (SCOPE.Pattern must not win)", got)
	}
	if builds != 1 {
		t.Errorf("name-index builds = %d, want 1", builds)
	}
}

// TestResolveDjangoGlobalWiringRefs_ExportedWrapperMatchesInternal guards the
// exported entry point used by the pipeline (cmd/grafel/index.go) against the
// internal build-counting variant drifting apart.
func TestResolveDjangoGlobalWiringRefs_ExportedWrapperMatchesInternal(t *testing.T) {
	mk := func() []types.EntityRecord {
		return []types.EntityRecord{
			{
				ID: "id-app-a", Name: "AuthMiddleware", Kind: "SCOPE.Component",
				SourceFile: "apps/a/middleware.py", Language: "python",
			},
			{
				ID: "id-app-b", Name: "AuthMiddleware", Kind: "SCOPE.Component",
				SourceFile: "apps/b/middleware.py", Language: "python",
			},
			djangoSettingsRecord(globalWiringUsesEdge("apps.b.middleware.AuthMiddleware", "AuthMiddleware")),
		}
	}

	viaExported := mk()
	gotExported := BuildIndex(viaExported).ResolveDjangoGlobalWiringRefs(viaExported)

	viaInternal := mk()
	gotInternal, _ := BuildIndex(viaInternal).resolveDjangoGlobalWiringRefs(viaInternal)

	if gotExported != gotInternal {
		t.Errorf("exported rewrites = %d, internal = %d", gotExported, gotInternal)
	}
	if got := viaExported[2].Relationships[0].ToID; got != "id-app-b" {
		t.Errorf("exported ToID = %q, want id-app-b", got)
	}
	if viaExported[2].Relationships[0].ToID != viaInternal[2].Relationships[0].ToID {
		t.Errorf("exported ToID = %q, internal ToID = %q",
			viaExported[2].Relationships[0].ToID, viaInternal[2].Relationships[0].ToID)
	}
}
