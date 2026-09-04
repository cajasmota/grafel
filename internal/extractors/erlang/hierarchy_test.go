package erlang_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/erlang"
	"github.com/cajasmota/grafel/internal/types"
)

// hierarchy_test.go — #6370: `-behaviour(gen_server).` → IMPLEMENTS.
//
// Every test here drives the real extractor end-to-end (extractor.Extract), so
// the assertions pin the EMIT SITE inside extractErlang rather than a helper
// that the emit site is free to stop calling.

// implementsEdges returns every IMPLEMENTS relationship in an extraction,
// paired with the name of the entity carrying it.
type implEdge struct {
	owner string // Name of the EntityRecord the edge is embedded on
	rel   types.RelationshipRecord
}

func extractErl(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extractor.Get("erlang")
	if !ok {
		t.Fatal("erlang extractor not registered")
	}
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return got
}

func implementsEdges(recs []types.EntityRecord) []implEdge {
	var out []implEdge
	for _, rec := range recs {
		for _, rel := range rec.Relationships {
			if rel.Kind == "IMPLEMENTS" {
				out = append(out, implEdge{owner: rec.Name, rel: rel})
			}
		}
	}
	return out
}

func propOf(rel types.RelationshipRecord, key string) string {
	for _, p := range rel.Properties {
		if p.K == key {
			return p.V
		}
	}
	return ""
}

// TestErlangBehaviourEmitsImplementsEdgeAnchoredOnModule pins the whole shape
// of the single edge a one-behaviour module produces: its kind, its owner, its
// bare-name target, its empty FromID and its line property.
//
// Varies: nothing — this is the single positive base case.
// Holds constant: one module, one behaviour, no other attribute.
func TestErlangBehaviourEmitsImplementsEdgeAnchoredOnModule(t *testing.T) {
	src := `-module(cache_server).
-behaviour(gen_server).

-export([init/1]).

init([]) ->
    {ok, []}.
`
	got := extractErl(t, "cache_server.erl", src)
	edges := implementsEdges(got)
	if len(edges) != 1 {
		t.Fatalf("want exactly 1 IMPLEMENTS edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.owner != "cache_server" {
		t.Errorf("edge must be embedded on the MODULE entity, got owner %q", e.owner)
	}
	if e.rel.ToID != "gen_server" {
		t.Errorf("ToID = %q, want the bare behaviour atom %q", e.rel.ToID, "gen_server")
	}
	// FromID must stay empty so assembly stamps the owning module's entity id.
	// A non-empty non-hex FromID is rewritten onto the FILE entity (#6295/#6298)
	// and is rejected by internal/extractors/file_anchored_rels_guard_test.go.
	if e.rel.FromID != "" {
		t.Errorf("FromID = %q, want empty so the edge anchors on the module, not the file", e.rel.FromID)
	}
	if got, want := propOf(e.rel, "line"), "2"; got != want {
		t.Errorf("line property = %q, want %q (the -behaviour attribute's own line)", got, want)
	}
	// The sibling SUPERVISES edge stamps provenance=otp_child_spec and that is
	// pinned at extractor_test.go:769-771; this edge follows the same
	// convention and is pinned the same way.
	if got, want := propOf(e.rel, "provenance"), "otp_behaviour"; got != want {
		t.Errorf("provenance property = %q, want %q", got, want)
	}
}

// TestErlangModuleWithoutBehaviourEmitsNoImplementsEdge is the negative control
// for over-firing: recall tests cannot detect a predicate that fires too
// often, so a module with no -behaviour() attribute at all must produce zero
// IMPLEMENTS edges anywhere in the extraction — not merely none on the module.
//
// The two decoys below are `%% -behaviour(gen_server).` and an indented string
// literal containing the same text. Neither exercises comment- or
// string-awareness, because there is none: the ONLY thing excluding them is
// behaviourRE's `(?m)^` line anchor, since neither occurrence begins a line.
// TestErlangBehaviourInsideMultiLineStringOverFires_KnownDivergence shows what
// happens when an occurrence does begin a line inside a string, and
// TestErlangBehaviourAnchorIsLoadBearing_IndentedAttributeIsNotMatched pins the
// anchor that is doing all of the work here.
//
// Varies: the presence of a line-initial -behaviour attribute (removed).
// Holds constant: module name, export list, function bodies, and the literal
// text "-behaviour(gen_server)." still occurring twice in the file off-column.
func TestErlangModuleWithoutBehaviourEmitsNoImplementsEdge(t *testing.T) {
	src := `-module(plain_module).
%% -behaviour(gen_server).
%% this module documents the gen_server behaviour but does not adopt it.

-export([run/0]).

run() ->
    Doc = "-behaviour(gen_server).",
    {ok, Doc}.
`
	got := extractErl(t, "plain_module.erl", src)
	if edges := implementsEdges(got); len(edges) != 0 {
		t.Fatalf("want 0 IMPLEMENTS edges for a module with no -behaviour attribute, got %d: %+v",
			len(edges), edges)
	}
}

// TestErlangAmericanSpellingBehaviorEmitsImplements pins the deliberate
// decision that `-behavior(...)` (American) is treated identically to
// `-behaviour(...)`: the Erlang compiler accepts both, and behaviourRE's
// `behaviou?r` already matched both before this change — the edge must not
// narrow that.
//
// Varies: only the spelling of the attribute name.
// Holds constant: module, behaviour atom, everything else.
func TestErlangAmericanSpellingBehaviorEmitsImplements(t *testing.T) {
	src := `-module(cache_sup).
-behavior(supervisor).

-export([init/1]).

init([]) ->
    {ok, {{one_for_one, 1, 5}, []}}.
`
	got := extractErl(t, "cache_sup.erl", src)
	edges := implementsEdges(got)
	if len(edges) != 1 {
		t.Fatalf("want exactly 1 IMPLEMENTS edge for the American spelling, got %d: %+v",
			len(edges), edges)
	}
	if edges[0].rel.ToID != "supervisor" {
		t.Errorf("ToID = %q, want %q", edges[0].rel.ToID, "supervisor")
	}
	if edges[0].owner != "cache_sup" {
		t.Errorf("owner = %q, want %q", edges[0].owner, "cache_sup")
	}
}

// TestErlangMultipleBehavioursEmitOneEdgeEachAndDeduplicate covers both the
// fan-out and the dedup direction in one fixture: two distinct behaviours
// produce two edges, and a repeated declaration of one of them produces no
// third edge.
//
// Varies: the number of -behaviour attributes (3) and the number of distinct
// atoms among them (2).
// Holds constant: one module, one file.
func TestErlangMultipleBehavioursEmitOneEdgeEachAndDeduplicate(t *testing.T) {
	src := `-module(hybrid).
-behaviour(gen_server).
-behaviour(supervisor).
-behaviour(gen_server).

-export([init/1]).

init([]) ->
    {ok, []}.
`
	got := extractErl(t, "hybrid.erl", src)
	edges := implementsEdges(got)
	if len(edges) != 2 {
		t.Fatalf("want 2 IMPLEMENTS edges (one per distinct behaviour), got %d: %+v",
			len(edges), edges)
	}
	seen := map[string]bool{}
	for _, e := range edges {
		if seen[e.rel.ToID] {
			t.Errorf("duplicate IMPLEMENTS edge to %q", e.rel.ToID)
		}
		seen[e.rel.ToID] = true
	}
	for _, want := range []string{"gen_server", "supervisor"} {
		if !seen[want] {
			t.Errorf("missing IMPLEMENTS edge to %q (got %v)", want, seen)
		}
	}
	// collectBehaviourDecls documents "the first declaration wins, so the
	// reported line is the first one" — observe it. gen_server is declared on
	// line 2 and again on line 4; a last-wins dedup would report 4 and is
	// otherwise indistinguishable from first-wins, since the edge count and the
	// ToID set are identical either way.
	for _, e := range edges {
		if e.rel.ToID != "gen_server" {
			continue
		}
		if got, want := propOf(e.rel, "line"), "2"; got != want {
			t.Errorf("line for the twice-declared gen_server = %q, want %q (the FIRST declaration)", got, want)
		}
	}
}

// TestErlangHierarchyNoDuplicateComponents is the groovy-derived guard
// (TestGroovyHierarchy_NoDuplicateComponents): emitting from the language
// extractor rather than registering erlang in cross/hierarchy must not mint a
// second SCOPE.Component for the module, and must not mint a component for the
// behaviour target either — the behaviour lives in OTP, outside the tree, and
// a synthesised node for it would be an unresolvable orphan.
//
// Varies: nothing; this is a structural invariant over the base fixture.
// Holds constant: the same one-behaviour module as the positive base case.
func TestErlangHierarchyNoDuplicateComponents(t *testing.T) {
	src := `-module(cache_server).
-behaviour(gen_server).

-export([init/1]).

init([]) ->
    {ok, []}.
`
	got := extractErl(t, "cache_server.erl", src)
	components := map[string]int{}
	for _, rec := range got {
		if rec.Kind == "SCOPE.Component" {
			components[rec.Name]++
		}
	}
	if n := components["cache_server"]; n != 1 {
		t.Errorf("want exactly 1 SCOPE.Component named cache_server, got %d", n)
	}
	if n := components["gen_server"]; n != 0 {
		t.Errorf("want 0 SCOPE.Component minted for the behaviour target gen_server, got %d", n)
	}
}

// TestErlangBehaviourEdgeSuppressedWhenModuleIsItsOwnBehaviour pins the
// self-edge guard. A self-edge carries no topology and is the signature of a
// mis-attributed owner (#6369), so `-module(gen_server). -behaviour(gen_server).`
// must emit nothing rather than an edge from the node to itself.
//
// Varies: the module name (made equal to the behaviour atom).
// Holds constant: the behaviour atom and the rest of the file.
func TestErlangBehaviourEdgeSuppressedWhenModuleIsItsOwnBehaviour(t *testing.T) {
	src := `-module(gen_server).
-behaviour(gen_server).

-export([init/1]).

init([]) ->
    {ok, []}.
`
	got := extractErl(t, "gen_server.erl", src)
	if edges := implementsEdges(got); len(edges) != 0 {
		t.Fatalf("want 0 IMPLEMENTS edges for a self-referential behaviour, got %d: %+v",
			len(edges), edges)
	}
}

// TestErlangBehaviourWithoutModuleAttributeEmitsNoEdge pins the anchoring
// contract from the other side: the edge is embedded on the module entity, so
// a file that declares a behaviour but no -module attribute has nothing to
// anchor on and must emit no floating edge (which would otherwise be
// file-anchored — the #6295/#6298 defect).
//
// Varies: the presence of the -module attribute (removed).
// Holds constant: the -behaviour attribute and the function body.
func TestErlangBehaviourWithoutModuleAttributeEmitsNoEdge(t *testing.T) {
	src := `-behaviour(gen_server).

-export([init/1]).

init([]) ->
    {ok, []}.
`
	got := extractErl(t, "orphan.erl", src)
	if edges := implementsEdges(got); len(edges) != 0 {
		t.Fatalf("want 0 IMPLEMENTS edges with no module to anchor on, got %d: %+v",
			len(edges), edges)
	}
}

// TestErlangBehaviourEdgeCoexistsWithOTPPropertyStamping pins that turning the
// behaviour into an edge did not replace the pre-existing property/tag/subtype
// stamping — the property is what OTP subtype refinement and callback
// classification read, and #6370 is an ADDITION to it, not a migration.
//
// Varies: nothing.
// Holds constant: the two-behaviour module; both the edges and the property
// are read off the same extraction.
func TestErlangBehaviourEdgeCoexistsWithOTPPropertyStamping(t *testing.T) {
	src := `-module(cache_server).
-behaviour(gen_server).

-export([init/1]).

init([]) ->
    {ok, []}.
`
	got := extractErl(t, "cache_server.erl", src)
	var mod *types.EntityRecord
	for i := range got {
		if got[i].Kind == "SCOPE.Component" && got[i].Name == "cache_server" {
			mod = &got[i]
			break
		}
	}
	if mod == nil {
		t.Fatal("module entity cache_server not extracted")
	}
	if mod.Properties["otp_behaviour"] != "gen_server" {
		t.Errorf(`Properties["otp_behaviour"] = %q, want "gen_server"`, mod.Properties["otp_behaviour"])
	}
	if mod.Subtype != "gen_server_module" {
		t.Errorf("Subtype = %q, want gen_server_module", mod.Subtype)
	}
	if !strings.Contains(strings.Join(mod.Tags, ","), "otp:gen_server") {
		t.Errorf("Tags = %v, want to contain otp:gen_server", mod.Tags)
	}
	if n := len(implementsEdges(got)); n != 1 {
		t.Errorf("want the IMPLEMENTS edge alongside the property, got %d edges", n)
	}
}

// TestErlangBehaviourAnchorIsLoadBearing_IndentedAttributeIsNotMatched pins
// behaviourRE's `(?m)^` anchor as a deliberate, load-bearing choice rather than
// an incidental one, so that widening it is a decision someone has to make on
// purpose.
//
// Erlang permits leading whitespace before a module attribute, so an indented
// `-behaviour(gen_server).` IS a real declaration the compiler honours and this
// extractor misses. That miss is pre-existing — it already governed
// Properties["otp_behaviour"], the "otp" tags and the OTP subtype refinement,
// all of which this test also observes so the anchor cannot be widened for the
// edge alone. It is pinned here, and NOT fixed, because the obvious widening
// (`(?m)^[ \t]*-behaviou?r`) makes the over-fire in
// TestErlangBehaviourInsideMultiLineStringOverFires_KnownDivergence strictly
// worse: today a false positive needs a string whose content starts at column
// 0, and after the widening any indented line inside any string or comment
// block would do. Widening the anchor is therefore not a safe local change —
// it needs the comment/string scrubbing this extractor does not have, and that
// pass would change the property half of extraction too, on a fixture that
// currently reads 25/25.
//
// Varies: the indentation of the -behaviour attribute (four leading spaces).
// Holds constant: everything else in the module, including the attribute text.
func TestErlangBehaviourAnchorIsLoadBearing_IndentedAttributeIsNotMatched(t *testing.T) {
	src := `-module(indented_mod).
    -behaviour(gen_server).

-export([init/1]).

init([]) ->
    {ok, []}.
`
	got := extractErl(t, "indented_mod.erl", src)
	if edges := implementsEdges(got); len(edges) != 0 {
		t.Fatalf("indented -behaviour is not matched by behaviourRE's (?m)^ anchor, so it must "+
			"produce no edge; got %d: %+v.\nIf you widened the anchor, read this test's doc "+
			"comment first: the widening also widens the over-fire pinned by "+
			"TestErlangBehaviourInsideMultiLineStringOverFires_KnownDivergence", len(edges), edges)
	}
	// The same anchor governs the pre-existing property half. Asserting it here
	// means the anchor cannot be widened for the edge alone, and records that
	// the edge and the property agree about this input — the "single reader"
	// property collectBehaviourDecls' doc comment claims.
	for _, rec := range got {
		if rec.Kind == "SCOPE.Component" && rec.Name == "indented_mod" {
			if v, ok := rec.Properties["otp_behaviour"]; ok {
				t.Errorf(`Properties["otp_behaviour"] = %q; the anchor must govern the property half too`, v)
			}
			if rec.Subtype != "module" {
				t.Errorf("Subtype = %q, want plain %q — an unmatched attribute must not refine the subtype",
					rec.Subtype, "module")
			}
		}
	}
}

// TestErlangBehaviourInsideMultiLineStringOverFires_KnownDivergence records a
// FALSE POSITIVE that this extractor produces today, so that it is visible and
// so that the anchor-widening the sibling test warns about cannot be made
// without confronting it.
//
// A multi-line Erlang string literal whose content happens to begin a line with
// `-behaviour(...)` is matched: behaviourRE scans raw source with no lexical
// state, so it cannot tell a string body from a module attribute. The property
// half of this is pre-existing; the IMPLEMENTS edge half arrived with #6370 and
// is what this test exists to make observable.
//
// This test asserts the CURRENT, WRONG output on purpose. Making the extractor
// comment/string-aware is the real fix and would change this expectation — that
// is the point: whoever writes that fix is told by a failing test that they
// have also fixed this, rather than discovering it from a moved golden count.
//
// Varies: the column at which the decoy text starts (0, versus off-column in
// TestErlangModuleWithoutBehaviourEmitsNoImplementsEdge).
// Holds constant: the decoy is inside a string literal in both.
func TestErlangBehaviourInsideMultiLineStringOverFires_KnownDivergence(t *testing.T) {
	src := `-module(doc_mod).

-export([usage/0]).

usage() ->
    "to make this module an OTP server, add:
-behaviour(gen_server).
to the top of the file".
`
	got := extractErl(t, "doc_mod.erl", src)
	edges := implementsEdges(got)
	if len(edges) != 1 {
		t.Fatalf("KNOWN DIVERGENCE CHANGED: expected the documented false-positive edge "+
			"(1 IMPLEMENTS to gen_server from text inside a string literal), got %d: %+v.\n"+
			"If you made the extractor comment/string-aware, this is the fix — update this "+
			"test to assert 0 edges, and say so in the PR body", len(edges), edges)
	}
	if edges[0].rel.ToID != "gen_server" {
		t.Errorf("false-positive edge ToID = %q, want %q", edges[0].rel.ToID, "gen_server")
	}
}
