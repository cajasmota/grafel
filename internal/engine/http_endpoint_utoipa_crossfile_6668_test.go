package engine

import (
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6668 — the final arm of #6560 (@arthurgeron). A `routes!(h)` whose
// `#[utoipa::path]` attribute lives in ANOTHER file resolved to nothing, because
// the attribute map is file-scoped by the ruling on #6560. This pins the
// additive marker that now carries the join key, and the four cases that
// deliberately emit nothing.
//
// EVERY fixture here is genuinely MULTI-FILE. A single-file fixture cannot
// distinguish this arm from B2a's same-file composition, which is already
// merged: in one file the mint resolves the handler itself and no marker is ever
// reached.

// utoipaCrossFileFile is one file of a multi-file fixture.
type utoipaCrossFileFile struct {
	path string
	src  string
}

// runUtoipaCrossFile runs the detector over each file of a multi-file fixture
// and returns the concatenated entities, in file order. The detector is per-file
// by construction, so this mirrors what the indexer does — and is the only way
// to observe a cross-file layout at all.
func runUtoipaCrossFile(t *testing.T, files []utoipaCrossFileFile) []types.EntityRecord {
	t.Helper()
	var all []types.EntityRecord
	for _, f := range files {
		_, res := runDetect(t, "rust", f.path, f.src)
		all = append(all, res.Entities...)
	}
	return all
}

// utoipaMarkers returns every #6668 registration marker in the merged set, as a
// SLICE. Keying this by entity ID would silently collapse a duplicate emit —
// which is precisely the permissive failure these tests exist to observe, so
// the collection must preserve multiplicity.
func utoipaMarkers(entities []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range entities {
		if e.Kind == string(types.EntityKindRoute) && e.Subtype == utoipaCrossFileSubtype {
			out = append(out, e)
		}
	}
	return out
}

// requireUtoipaMarker asserts exactly ONE marker exists for (module, name), that
// it was emitted from relPath, and that its join key and mount prefix are the
// wanted ones. wantPrefix == "" means the `mount_prefix` property must be ABSENT
// (not empty) — a marker never states a prefix it did not see.
//
// The "exactly one" is the load-bearing half: a marker minted twice for one
// handler is the permissive failure this arm exists to avoid.
func requireUtoipaMarker(t *testing.T, entities []types.EntityRecord, relPath, module, name, wantPrefix, label string) {
	t.Helper()
	var found []types.EntityRecord
	for _, e := range utoipaMarkers(entities) {
		if e.Properties["handler_module"] == module && e.Properties["handler_name"] == name {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s: want exactly 1 marker for %s::%s, got %d (%v)",
			label, module, name, len(found), utoipaMarkerIDs(entities))
	}
	e := found[0]
	if e.SourceFile != relPath {
		t.Errorf("%s: marker source_file = %q, want %q", label, e.SourceFile, relPath)
	}
	if got := e.Properties["framework"]; got != "utoipa_axum" {
		t.Errorf("%s: marker framework = %q, want utoipa_axum", label, got)
	}
	if got := e.Properties["pattern_type"]; got != utoipaCrossFilePatternType {
		t.Errorf("%s: marker pattern_type = %q, want %q", label, got, utoipaCrossFilePatternType)
	}
	got, present := e.Properties["mount_prefix"]
	if wantPrefix == "" {
		if present {
			t.Errorf("%s: mount_prefix present as %q, want absent", label, got)
		}
	} else if !present || got != wantPrefix {
		t.Errorf("%s: mount_prefix = %q (present=%v), want %q", label, got, present, wantPrefix)
	}
	// The marker must NOT claim a path or a verb. Inventing either would make it
	// a pathless phantom in the endpoint family (#6150).
	for _, k := range []string{"path", "verb", "source_handler"} {
		if v, ok := e.Properties[k]; ok {
			t.Errorf("%s: marker carries %s=%q; it must claim no route identity", label, k, v)
		}
	}
}

// requireNoUtoipaMarkerFor asserts no marker names this handler at all.
func requireNoUtoipaMarkerFor(t *testing.T, entities []types.EntityRecord, name, label string) {
	t.Helper()
	for _, e := range utoipaMarkers(entities) {
		if e.Properties["handler_name"] == name {
			t.Errorf("%s: marker %s emitted for %q, want none", label, e.ID, name)
		}
	}
}

func utoipaMarkerIDs(entities []types.EntityRecord) []string {
	var ids []string
	for _, e := range utoipaMarkers(entities) {
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)
	return ids
}

// countDefsForHandlerIn is countDefsForHandler over a merged multi-file set.
func countDefsForHandlerIn(entities []types.EntityRecord, handler string) int {
	n := 0
	for _, e := range entities {
		if e.Kind == httpEndpointDefinitionKind && e.Properties["source_handler"] == "Controller:"+handler {
			n++
		}
	}
	return n
}

// requireUtoipaDefIn is requireUtoipaDef over a merged multi-file set: it is the
// PREMISE GUARD. Without it a fixture passes while observing synthesizeAxumRoutes
// or synthesizeRocket rather than the utoipa pass this arm extends.
func requireUtoipaDefIn(t *testing.T, entities []types.EntityRecord, id, handler, label string) {
	t.Helper()
	for _, e := range entities {
		if e.ID != id || e.Kind != httpEndpointDefinitionKind {
			continue
		}
		if e.Properties["framework"] != "utoipa_axum" {
			t.Errorf("%s: %s minted by framework=%q, want utoipa_axum — fixture is not observing the pass under test",
				label, id, e.Properties["framework"])
			return
		}
		if e.Properties["source_handler"] != "Controller:"+handler {
			t.Errorf("%s: %s has source_handler=%q, want Controller:%s",
				label, id, e.Properties["source_handler"], handler)
			return
		}
		return
	}
	var got []string
	for _, e := range entities {
		if e.Kind == httpEndpointDefinitionKind {
			got = append(got, e.ID)
		}
	}
	sort.Strings(got)
	t.Errorf("%s: want utoipa_axum http_endpoint_definition %s, got %v", label, id, got)
}

// itemsModule is the handler module shared by the fixtures below. `list_items`
// is registered in items.rs itself (so the mint runs and the premise guard has
// something to check); `create_item` is registered ONLY from router.rs, which is
// the cross-file case.
const itemsModuleSrc = `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
pub async fn list_items() -> &'static str { "[]" }

#[utoipa::path(post, path = "/items")]
pub async fn create_item() -> &'static str { "{}" }

pub fn local() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(list_items))
}
`

// TestUtoipaCrossFile_MarkerCarriesJoinKey_6668 is the layout from the issue:
// the attribute and the handler live in src/items.rs, the registration in
// src/router.rs. Before this arm router.rs emitted nothing at all.
func TestUtoipaCrossFile_MarkerCarriesJoinKey_6668(t *testing.T) {
	router := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
use crate::items::create_item;

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(create_item))
}
`
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", itemsModuleSrc},
		{"src/router.rs", router},
	})

	// Premise guard: the same-file half really is minted by THIS pass, so the
	// fixture is exercising the utoipa producer and not something else.
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-join-key")

	requireUtoipaMarker(t, ents, "src/router.rs", "crate::items", "create_item", "", "6668-join-key")

	// THE STATED BOUND, pinned: the marker is NOT a definition. `create_item`
	// still has no canonical http_endpoint_definition anywhere, and this test
	// asserts that rather than implying it. Redeeming the marker is the
	// consumer's job, filed separately.
	if got := countDefsForHandlerIn(ents, "create_item"); got != 0 {
		t.Errorf("6668-join-key: create_item has %d http_endpoint_definition(s); this arm mints none for a cross-file handler", got)
	}
}

// TestUtoipaCrossFile_MarkerNotEmittedTwice_6668 scores the PERMISSIVE
// direction: one handler registered by two `routes!` macros in one router file,
// and named again in a second macro, must still yield exactly ONE marker.
func TestUtoipaCrossFile_MarkerNotEmittedTwice_6668(t *testing.T) {
	router := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
use crate::items::create_item;

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .routes(routes!(create_item))
        .routes(routes!(create_item))
}
`
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", itemsModuleSrc},
		{"src/router.rs", router},
	})
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-once")
	requireUtoipaMarker(t, ents, "src/router.rs", "crate::items", "create_item", "", "6668-once")
	if n := len(utoipaMarkers(ents)); n != 1 {
		t.Errorf("6668-once: want 1 marker in total, got %d (%v)", n, utoipaMarkerIDs(ents))
	}
}

// TestUtoipaCrossFile_SameFileHandlerGetsNoMarker_6668 is the other permissive
// guard: a handler whose contract IS in this file is minted as a real
// definition, so a marker beside it would be a second claimant for one handler
// — #6530's hazard in miniature.
func TestUtoipaCrossFile_SameFileHandlerGetsNoMarker_6668(t *testing.T) {
	// items.rs imports its own handler name from elsewhere AND declares an
	// attribute for it. The same-file contract must win and suppress the marker.
	items := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
use crate::other::list_items;

#[utoipa::path(get, path = "/items")]
pub async fn list_items() -> &'static str { "[]" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(list_items))
}
`
	other := `
pub async fn unrelated() -> &'static str { "" }
`
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", items},
		{"src/other.rs", other},
	})
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-same-file-wins")
	requireNoUtoipaMarkerFor(t, ents, "list_items", "6668-same-file-wins")
	if n := len(utoipaMarkers(ents)); n != 0 {
		t.Errorf("6668-same-file-wins: want 0 markers, got %d (%v)", n, utoipaMarkerIDs(ents))
	}
}

// TestUtoipaCrossFile_RegisteredElsewhereGetsNoMarker_6668 pins the second
// suppression: the router file ALSO registers the handler with a literal
// `.route(...)`, which synthesizeAxumRoutes mints out of this same file. One
// producer per handler per file, marker included.
func TestUtoipaCrossFile_RegisteredElsewhereGetsNoMarker_6668(t *testing.T) {
	router := `
use axum::routing::post;
use axum::Router;
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
use crate::items::create_item;

pub fn plain() -> Router {
    Router::new().route("/api/items", post(create_item))
}

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(create_item))
}
`
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", itemsModuleSrc},
		{"src/router.rs", router},
	})
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-registered-elsewhere")
	requireNoUtoipaMarkerFor(t, ents, "create_item", "6668-registered-elsewhere")
}

// TestUtoipaCrossFile_UnimportedHandlerGetsNoMarker_6668 pins the third
// suppression, and it is the #6150 rule: with no `use` binding there is no join
// key, so the registration is left UNENRICHED rather than stamped with a guessed
// module. A glob import is the same case — it names no item.
func TestUtoipaCrossFile_UnimportedHandlerGetsNoMarker_6668(t *testing.T) {
	for _, tc := range []struct {
		label string
		uses  string
	}{
		{"no-binding", "use utoipa_axum::routes;"},
		{"glob", "use utoipa_axum::routes;\nuse crate::items::*;"},
		// A NESTED brace group is rejected whole: splitting its body on commas
		// would tear the braces apart and bind fragments that are not items.
		{"nested-group", "use utoipa_axum::routes;\nuse crate::{items::create_item, admin::purge};"},
	} {
		router := `
use utoipa_axum::router::OpenApiRouter;
` + tc.uses + `

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(create_item))
}
`
		ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
			{"src/items.rs", itemsModuleSrc},
			{"src/router.rs", router},
		})
		requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-unimported/"+tc.label)
		requireNoUtoipaMarkerFor(t, ents, "create_item", "6668-unimported/"+tc.label)
	}
}

// TestUtoipaCrossFile_BraceGroupAndAlias_6668 covers the two `use` spellings a
// real router file uses. Under `as`, the join key must be the name the DECLARING
// module knows — the alias is local to the router and would match nothing there.
func TestUtoipaCrossFile_BraceGroupAndAlias_6668(t *testing.T) {
	router := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
use crate::items::{create_item, list_items};
use crate::admin::purge as purge_all;

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .routes(routes!(create_item))
        .routes(routes!(purge_all))
}
`
	admin := `
#[utoipa::path(delete, path = "/admin/purge")]
pub async fn purge() -> &'static str { "" }
`
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", itemsModuleSrc},
		{"src/admin.rs", admin},
		{"src/router.rs", router},
	})
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-use-forms")
	requireUtoipaMarker(t, ents, "src/router.rs", "crate::items", "create_item", "", "6668-use-forms")
	// The marker is emitted from the REGISTRATION file, never from the declaring
	// module — src/admin.rs has the attribute but registers nothing.
	requireUtoipaMarker(t, ents, "src/router.rs", "crate::admin", "purge", "", "6668-use-forms")
	// `list_items` is imported but never registered in router.rs — an import is
	// not a registration, and a marker for it would be invented.
	if n := len(utoipaMarkers(ents)); n != 2 {
		t.Errorf("6668-use-forms: want exactly 2 markers, got %d (%v)", n, utoipaMarkerIDs(ents))
	}
}

// TestUtoipaCrossFile_SameFileNestSuppliesMountPrefix_6668 checks the one piece
// of path information the router file DOES own: its own `.nest("/api", …)`.
// Cross-file nests still yield no prefix.
func TestUtoipaCrossFile_SameFileNestSuppliesMountPrefix_6668(t *testing.T) {
	router := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
use crate::items::create_item;

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .nest("/api", OpenApiRouter::new().routes(routes!(create_item)))
}
`
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", itemsModuleSrc},
		{"src/router.rs", router},
	})
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-nest")
	requireUtoipaMarker(t, ents, "src/router.rs", "crate::items", "create_item", "/api", "6668-nest")
}

// TestUtoipaCrossFile_TwoRoutersRegisterOneHandler_6668 pins that the marker ID
// keys on the REGISTRATION FILE as well as the join key. Two routers mounting
// one handler at two prefixes are two registrations, and collapsing them onto a
// single record would silently discard one mount point — the join key alone is
// not an identity.
func TestUtoipaCrossFile_TwoRoutersRegisterOneHandler_6668(t *testing.T) {
	mk := func(prefix string) string {
		return `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
use crate::items::create_item;

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .nest("` + prefix + `", OpenApiRouter::new().routes(routes!(create_item)))
}
`
	}
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", itemsModuleSrc},
		{"src/api_v1.rs", mk("/v1")},
		{"src/api_v2.rs", mk("/v2")},
	})
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-two-routers")
	markers := utoipaMarkers(ents)
	if len(markers) != 2 {
		t.Fatalf("6668-two-routers: want 2 markers (one per registration file), got %d (%v)",
			len(markers), utoipaMarkerIDs(ents))
	}
	// Distinct IDs are the point: one shared ID for two registrations is one ID
	// with two claimants, the #6530 shape this arm must not reproduce. The
	// per-file detector cannot observe the post-merge collision, so the identity
	// is asserted here directly.
	if markers[0].ID == markers[1].ID {
		t.Errorf("6668-two-routers: both markers share ID %q; the registration file must be part of the identity",
			markers[0].ID)
	}
	byFile := map[string]string{}
	for _, e := range markers {
		if e.Properties["handler_module"] != "crate::items" || e.Properties["handler_name"] != "create_item" {
			t.Errorf("6668-two-routers: marker %s has join key %s::%s, want crate::items::create_item",
				e.ID, e.Properties["handler_module"], e.Properties["handler_name"])
		}
		byFile[e.SourceFile] = e.Properties["mount_prefix"]
	}
	for file, want := range map[string]string{"src/api_v1.rs": "/v1", "src/api_v2.rs": "/v2"} {
		if got, ok := byFile[file]; !ok || got != want {
			t.Errorf("6668-two-routers: %s mount_prefix = %q (present=%v), want %q", file, got, ok, want)
		}
	}
}

// TestUtoipaCrossFile_PathsAgreeStillDoubleMints_6668 states the paths-agree
// verdict EXPLICITLY rather than leaving it implied: it is NOT fixed here, and
// it lands on #6530.
//
// items.rs mints http:GET:/items from its own `#[utoipa::path]` + `routes!`;
// main.rs mints the same ID from a literal `.route("/items", get(...))`. Two
// files, one synthetic ID, two claimants. Neither file emits a marker — items.rs
// has the same-file contract and main.rs has no `routes!` — so this arm neither
// fixes nor worsens it, and this test pins that it is unchanged.
func TestUtoipaCrossFile_PathsAgreeStillDoubleMints_6668(t *testing.T) {
	main := `
use axum::routing::get;
use axum::Router;
use crate::items::list_items;

pub fn app() -> Router {
    Router::new().route("/items", get(list_items))
}
`
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", itemsModuleSrc},
		{"src/main.rs", main},
	})
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-paths-agree")
	if got := countDefsForHandlerIn(ents, "list_items"); got != 2 {
		t.Errorf("6668-paths-agree: want 2 definitions for list_items (the unfixed #6530 duplicate), got %d — "+
			"if this now reports 1, #6530 has been fixed elsewhere and this pin should move there", got)
	}
	if n := len(utoipaMarkers(ents)); n != 0 {
		t.Errorf("6668-paths-agree: want 0 markers in the paths-agree layout, got %d (%v)", n, utoipaMarkerIDs(ents))
	}
}

// TestUtoipaCrossFile_LineCommentedUseIsNotABinding_6668 is the last permissive
// guard: a `//`-commented `use` binds nothing, so the registration it appears to
// resolve must stay unenriched.
func TestUtoipaCrossFile_LineCommentedUseIsNotABinding_6668(t *testing.T) {
	router := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
// use crate::items::create_item;

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(create_item))
}
`
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", itemsModuleSrc},
		{"src/router.rs", router},
	})
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-commented-use")
	requireNoUtoipaMarkerFor(t, ents, "create_item", "6668-commented-use")
}
