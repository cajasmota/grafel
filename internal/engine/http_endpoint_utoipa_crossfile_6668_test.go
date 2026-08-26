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
		// A flat group that does not name this handler at all. (The flat group
		// that DOES name it resolves — see
		// TestUtoipaCrossFile_FlatGroupLeafPaths_6668.)
		{"flat-group-other-handlers", "use utoipa_axum::routes;\nuse crate::{list_items, admin::purge};"},
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
		// TOTAL-COUNT BACKSTOP. requireNoUtoipaMarkerFor filters on
		// handler_name, so a marker with an EMPTY join key is invisible to it —
		// and an empty join key is exactly what dropping the `bound` guard
		// produces. Without this line the whole table passes while the #6150
		// "never guess a module" clause is deleted (scored: that mutant
		// SURVIVED before this assertion existed, and dies with it).
		if n := len(utoipaMarkers(ents)); n != 0 {
			t.Errorf("6668-unimported/%s: want 0 markers, got %d (%v)",
				tc.label, n, utoipaMarkerIDs(ents))
		}
	}
}

// TestUtoipaCrossFile_NestedUseGroupFabricatesNothing_6668 is the case that
// makes the nested-group rejection load-bearing rather than decorative.
//
// `use crate::{items::{create_item, purge}, admin};` — a real rustfmt output —
// contains a group INSIDE a group. A comma split of the outer body yields
// `items::{create_item`, `purge}`, `admin`; the naive scan that cuts the group
// at the FIRST `}` instead yields a bare `purge`, which passes leaf validation
// and is attributed to the OUTER base. The join key would then read
// `crate::purge` for a handler that really lives at `crate::items::purge` — an
// INVENTED key, and worse than a miss: #6669 resolves on
// (handler_module, handler_name), so a fabricated module MIS-JOINS rather than
// failing to join.
//
// The correct answer is zero markers: this file states no binding this pass can
// read, so the registration is left unenriched (#6150).
func TestUtoipaCrossFile_NestedUseGroupFabricatesNothing_6668(t *testing.T) {
	router := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
use crate::{items::{create_item, purge}, admin};

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(purge))
}
`
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", itemsModuleSrc},
		{"src/router.rs", router},
	})
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-nested-use-group")
	for _, e := range utoipaMarkers(ents) {
		t.Errorf("6668-nested-use-group: marker %s fabricated join key %s::%s from a nested `use` group; want no marker at all",
			e.ID, e.Properties["handler_module"], e.Properties["handler_name"])
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

// TestUtoipaCrossFile_FlatGroupLeafPaths_6668 pins the most common rustfmt
// grouping, `use crate::{items::create_item, admin::purge};`. An earlier
// revision resolved NEITHER leaf — restrictive rather than wrong, but it left
// the dominant real-world spelling silently unhandled, so the feature would have
// shipped near-inert on real routers.
func TestUtoipaCrossFile_FlatGroupLeafPaths_6668(t *testing.T) {
	router := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
use crate::{items::create_item, api::v1::admin::purge};

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .routes(routes!(create_item))
        .routes(routes!(purge))
}
`
	admin := `
#[utoipa::path(delete, path = "/admin/purge")]
pub async fn purge() -> &'static str { "" }
`
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", itemsModuleSrc},
		{"src/api/v1/admin.rs", admin},
		{"src/router.rs", router},
	})
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-flat-group-paths")
	requireUtoipaMarker(t, ents, "src/router.rs", "crate::items", "create_item", "", "6668-flat-group-paths")
	// A MULTI-segment leaf: every leading segment belongs to the module and only
	// the last names the item. Taking the first segment instead would yield
	// `crate::api` + `v1::admin::purge`, which is an invented key.
	requireUtoipaMarker(t, ents, "src/router.rs", "crate::api::v1::admin", "purge", "", "6668-flat-group-paths")
	if n := len(utoipaMarkers(ents)); n != 2 {
		t.Errorf("6668-flat-group-paths: want 2 markers, got %d (%v)", n, utoipaMarkerIDs(ents))
	}
}

// TestUtoipaCrossFile_RelativeUseRootRefused_6668 pins the ruling on `self::`
// and `super::`: both resolve only against the declaring file's own position in
// the module tree, which a per-file pass cannot know, so no marker is emitted
// rather than one carrying a key that can never match by identity (#6150).
// `crate::` is the control — it IS resolvable and must still be kept.
func TestUtoipaCrossFile_RelativeUseRootRefused_6668(t *testing.T) {
	mk := func(useDecl string) string {
		return `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
` + useDecl + `

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(create_item))
}
`
	}
	for _, tc := range []struct {
		label   string
		useDecl string
		want    int
	}{
		{"self", "use self::items::create_item;", 0},
		{"super", "use super::items::create_item;", 0},
		{"super-in-group", "use super::{items::create_item};", 0},
		{"crate-control", "use crate::items::create_item;", 1},
	} {
		ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
			{"src/items.rs", itemsModuleSrc},
			{"src/router.rs", mk(tc.useDecl)},
		})
		requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-relative-use/"+tc.label)
		if n := len(utoipaMarkers(ents)); n != tc.want {
			t.Errorf("6668-relative-use/%s: want %d marker(s), got %d (%v)",
				tc.label, tc.want, n, utoipaMarkerIDs(ents))
		}
	}
}

// TestUtoipaCrossFile_CommentedRoutesMacroStillMarks_6668 documents an
// ASYMMETRY rather than leaving it implied: a `//`-commented `use` binds
// nothing, but a `//`-commented `routes!(...)` IS still read as a registration.
//
// THE RULING: keep the asymmetry, and pin it. `utoipaAttrIsLineCommented` exists
// in this package and could suppress the marker — but synthesizeUtoipaAxumRoutes
// and synthesizeAxumRoutes both read a commented-out registration as real, by
// long-standing house behaviour. Making the MARKER stricter than the MINT that
// shares its regex would mean one `routes!` occurrence is a registration for one
// pass and not for the other, which is a worse defect than the false positive it
// removes. If this is fixed it must be fixed for the whole Rust route family at
// once, not here.
func TestUtoipaCrossFile_CommentedRoutesMacroStillMarks_6668(t *testing.T) {
	router := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
use crate::items::create_item;

pub fn router() -> OpenApiRouter {
    // OpenApiRouter::new().routes(routes!(create_item))
    OpenApiRouter::new()
}
`
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", itemsModuleSrc},
		{"src/router.rs", router},
	})
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-commented-macro")
	// Asserted as-is: this is the documented house behaviour, not an endorsement.
	requireUtoipaMarker(t, ents, "src/router.rs", "crate::items", "create_item", "", "6668-commented-macro")
}

// TestUtoipaCrossFile_AmbiguousUseBindingDropped_6668 pins the collision policy,
// and it exists because the previous revision's justification for having NO
// policy was false in the language.
//
// The claim was that two `use` declarations binding one local name is E0252 and
// cannot compile. E0252 is per-SCOPE. Both shapes below compile, both reached
// the binding map, and because the map was last-wins the SECOND won — so a
// `mod tests` block, which by convention sits at the bottom of a file, silently
// redirected the join key to `crate::mocks`. #6669 resolves on
// (handler_module, handler_name), so that MIS-JOINS rather than missing.
//
// The policy is DROP, not first-wins: this pass models neither scopes nor `cfg`
// evaluation, so it cannot know which binding is live at the registration site,
// and #6150's answer to that is to leave the endpoint unenriched. The `crate`
// control proves the drop is scoped to genuine ambiguity.
func TestUtoipaCrossFile_AmbiguousUseBindingDropped_6668(t *testing.T) {
	for _, tc := range []struct {
		label string
		uses  string
		want  int
	}{
		{"mod-tests-scope", `use crate::items::create_item;

mod tests {
    use crate::mocks::create_item;
}`, 0},
		{"cfg-feature-pair", `#[cfg(feature = "x")]
use crate::real::create_item;
#[cfg(not(feature = "x"))]
use crate::stub::create_item;`, 0},
		// A third declaration must not resurrect a poisoned name.
		{"third-declaration-after-collision", `use crate::items::create_item;
mod tests {
    use crate::mocks::create_item;
}
mod more {
    use crate::items::create_item;
}`, 0},
		// The SAME binding declared twice names one item and is not ambiguous.
		{"identical-duplicate-kept", `use crate::items::create_item;
mod tests {
    use crate::items::create_item;
}`, 1},
		{"single-binding-control", `use crate::items::create_item;`, 1},
	} {
		router := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
` + tc.uses + `

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(create_item))
}
`
		ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
			{"src/items.rs", itemsModuleSrc},
			{"src/router.rs", router},
		})
		requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-ambiguous-use/"+tc.label)
		got := utoipaMarkers(ents)
		if len(got) != tc.want {
			t.Errorf("6668-ambiguous-use/%s: want %d marker(s), got %d (%v)",
				tc.label, tc.want, len(got), utoipaMarkerIDs(ents))
			continue
		}
		for _, e := range got {
			if m := e.Properties["handler_module"]; m != "crate::items" {
				t.Errorf("6668-ambiguous-use/%s: handler_module = %q, want crate::items",
					tc.label, m)
			}
		}
	}
}

// TestUtoipaCrossFile_MalformedUsePathRefused_6668 pins rustUsePathRe, which is
// the ONLY defence between the `module + "::" + name[:cut]` concatenation in
// rustAddUseLeaf and a published garbage join key. Deleting it scored vet 0 /
// test 0 — SURVIVED — before this test existed, and it is not equivalent: each
// input below yields a marker with a malformed module under the mutant.
func TestUtoipaCrossFile_MalformedUsePathRefused_6668(t *testing.T) {
	for _, tc := range []struct{ label, useDecl string }{
		{"empty-segment", "use crate::{::create_item};"},
		{"double-colon-run", "use crate::{a::::create_item};"},
		{"digit-leading-segment", "use crate::{1bad::create_item};"},
	} {
		router := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
` + tc.useDecl + `

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(create_item))
}
`
		ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
			{"src/items.rs", itemsModuleSrc},
			{"src/router.rs", router},
		})
		requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-malformed-path/"+tc.label)
		for _, e := range utoipaMarkers(ents) {
			t.Errorf("6668-malformed-path/%s: emitted marker with handler_module=%q; a malformed path must yield no join key",
				tc.label, e.Properties["handler_module"])
		}
	}
}

// TestUtoipaCrossFile_PathLeafWithAlias_6668 covers `use crate::{items::x as
// mk};` — a path leaf AND an alias in one leaf. This resolved to NOTHING before
// review round 2, because the alias regex accepted only a bare identifier on its
// left, so the `::` split ran on the text `create_item as mk` and the result
// failed identifier validation. Restrictive, therefore invisible.
//
// The join key must be the DECLARING module's pair (`crate::items`,
// `create_item`); `mk` is local to the router and would match nothing there.
func TestUtoipaCrossFile_PathLeafWithAlias_6668(t *testing.T) {
	router := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
use crate::{items::create_item as mk};

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(mk))
}
`
	ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
		{"src/items.rs", itemsModuleSrc},
		{"src/router.rs", router},
	})
	requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6668-path-leaf-alias")
	requireUtoipaMarker(t, ents, "src/router.rs", "crate::items", "create_item", "", "6668-path-leaf-alias")
}
