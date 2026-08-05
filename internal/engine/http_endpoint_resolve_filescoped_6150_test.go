package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6150 — the FILE-SCOPED entry point to the endpoint→handler resolve.
//
// ResolveHTTPEndpointHandlersWithRepo is written for the full rebuild, where
// `merged` is the WHOLE corpus. On that input, "no record anywhere matches this
// synthetic's source_handler" genuinely means "this endpoint names a handler
// that does not exist", and dropping the synthetic is right: keeping it would
// leave an orphan http_endpoint node in the graph.
//
// The incremental path hands it ONE re-extracted file's records. That makes
// "no candidate in the corpus" and "no candidate in this file" the SAME
// condition — and a handler in another file is the ordinary case, not the
// exception (Express router + imported controller, Flask add_url_rule +
// imported function, DRF router + ViewSet). Running the unmodified pass on that
// input DELETES those endpoints rather than leaving them unenriched, which is
// strictly worse than not running the pass at all.
//
// The keep branch is not new policy. The same function already refuses to drop
// for exactly this reason twice — #2851 (a handler_file hint that matched
// nothing in that file) and #3426 (every global candidate lives in a
// non-app/tooling file) — and both say the same thing: the handler IS
// attributed via the property, it is simply not cross-linked, so preserve the
// route. A caller that cannot see the rest of the corpus is in that position
// for EVERY unresolved synthetic.
//
// These tests drive the real pass, not a copy of its logic.

// fsHandler is a handler record the resolver can bind an endpoint to.
func fsHandler(kind, name, file string) types.EntityRecord {
	return types.EntityRecord{Kind: kind, Name: name, SourceFile: file, Language: "javascript"}
}

// fsEndpoint is an http_endpoint synthetic naming `handlerRef` as its handler.
func fsEndpoint(name, file, handlerRef string) types.EntityRecord {
	return types.EntityRecord{
		Kind:       "http_endpoint",
		Name:       name,
		SourceFile: file,
		Language:   "javascript",
		Properties: map[string]string{"source_handler": handlerRef},
	}
}

// fsNames keys on NAME ALONE, deliberately. The pass performs the #1217
// http_endpoint → http_endpoint_definition kind migration on the way through,
// so a kind-qualified key would report a surviving endpoint as absent and turn
// this file into a test of the migration rather than of the drop verdict.
func fsNames(recs []types.EntityRecord) map[string]bool {
	out := make(map[string]bool, len(recs))
	for _, r := range recs {
		out[r.Name] = true
	}
	return out
}

// TestResolveHTTPEndpointHandlersFileScoped_KeepsCrossFileHandler is the Sev-1
// regression. One file's records: a route file carrying an endpoint whose
// handler lives in a DIFFERENT file, which is therefore absent from the slice.
//
// The corpus-scoped entry point DELETES it — that is correct for its own
// caller and is asserted here so the difference between the two entry points is
// pinned from both sides, not assumed.
func TestResolveHTTPEndpointHandlersFileScoped_KeepsCrossFileHandler(t *testing.T) {
	build := func() []types.EntityRecord {
		return []types.EntityRecord{
			fsHandler("SCOPE.Operation", "cpRouterSetup", "routes.js"),
			fsEndpoint("http:GET:/users", "routes.js", "Controller:cpListUsers"),
		}
	}

	corpus, corpusStats := ResolveHTTPEndpointHandlersWithRepo(build(), "test-repo")
	if fsNames(corpus)["http:GET:/users"] {
		t.Fatalf("corpus-scoped entry point kept the unresolvable synthetic; " +
			"this test's premise (that it drops) no longer holds")
	}
	if corpusStats.HandlerDropped != 1 {
		t.Errorf("corpus HandlerDropped = %d, want 1", corpusStats.HandlerDropped)
	}

	scoped, scopedStats := ResolveHTTPEndpointHandlersFileScoped(build(), "test-repo")
	if !fsNames(scoped)["http:GET:/users"] {
		t.Errorf("file-scoped entry point DELETED the endpoint whose handler lives in another file; " +
			"it must keep it (unenriched), not destroy it")
	}
	if scopedStats.HandlerDropped != 0 {
		t.Errorf("file-scoped HandlerDropped = %d, want 0", scopedStats.HandlerDropped)
	}
	if scopedStats.HandlerUnresolvedKept != 1 {
		t.Errorf("file-scoped HandlerUnresolvedKept = %d, want 1", scopedStats.HandlerUnresolvedKept)
	}
	// The property that names the handler must SURVIVE: it is the whole
	// attribution the endpoint has left, and a later corpus-wide pass (or the
	// next full rebuild) resolves it from there.
	for _, r := range scoped {
		if r.Name == "http:GET:/users" && r.Properties["source_handler"] != "Controller:cpListUsers" {
			t.Errorf("source_handler = %q, want it preserved on the kept synthetic",
				r.Properties["source_handler"])
		}
	}
}

// TestResolveHTTPEndpointHandlersFileScoped_StillResolvesCoLocated: the keep
// guard must not cost the resolution the pass exists for. When the handler IS
// in the slice, the file-scoped entry point behaves exactly like the corpus one
// — the IMPLEMENTS bridge is emitted and source_handler is cleared.
func TestResolveHTTPEndpointHandlersFileScoped_StillResolvesCoLocated(t *testing.T) {
	recs := []types.EntityRecord{
		fsHandler("SCOPE.Operation", "cpListUsers", "routes.js"),
		fsEndpoint("http:GET:/users", "routes.js", "SCOPE.Operation:cpListUsers"),
	}
	out, stats := ResolveHTTPEndpointHandlersFileScoped(recs, "test-repo")
	if stats.HandlerResolved != 1 {
		t.Fatalf("HandlerResolved = %d, want 1 — the keep guard must not suppress a real bind", stats.HandlerResolved)
	}
	if stats.HandlerUnresolvedKept != 0 {
		t.Errorf("HandlerUnresolvedKept = %d, want 0", stats.HandlerUnresolvedKept)
	}
	var bridged bool
	for _, r := range out {
		if r.Kind != "SCOPE.Operation" {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind == implementsEdgeKind {
				bridged = true
			}
		}
	}
	if !bridged {
		t.Error("no IMPLEMENTS bridge emitted from the co-located handler")
	}
}

// TestResolveHTTPEndpointHandlersFileScoped_StillDropsMalformedRef: the keep
// guard is scoped to the ONE drop branch whose verdict depends on how much of
// the corpus the caller could see. A source_handler that does not parse as
// `Kind:Name` is malformed no matter how wide the slice is, and dropping it
// stops a bad reference leaking into the graph — that branch is unchanged.
func TestResolveHTTPEndpointHandlersFileScoped_StillDropsMalformedRef(t *testing.T) {
	recs := []types.EntityRecord{
		fsHandler("SCOPE.Operation", "cpRouterSetup", "routes.js"),
		fsEndpoint("http:GET:/users", "routes.js", "no-colon-here"),
	}
	out, stats := ResolveHTTPEndpointHandlersFileScoped(recs, "test-repo")
	if fsNames(out)["http:GET:/users"] {
		t.Error("a malformed source_handler must still be dropped: its verdict does not depend on slice scope")
	}
	if stats.HandlerDropped != 1 {
		t.Errorf("HandlerDropped = %d, want 1", stats.HandlerDropped)
	}
}

// TestResolveHTTPEndpointHandlersFileScoped_CanonicalOrderMakesTheBindStable
// is the Sev-2 evidence: the resolver's answer DEPENDS on the order of the
// slice it is handed, so "`merged` MUST already be sorted in canonical order
// (#481)" is a real precondition and not documentation hygiene.
//
// Measured on the orderings below, WITHOUT the sort: the pass binds the handler
// at line 10 when that record comes first and the one at line 50 when it does,
// then rebinds the endpoint's coordinates onto whichever won — so the two
// answers differ in the graph, not just in the edge. The index behind that
// choice (globalIdx / globalMulti / sameFileBareIdx) is first-writer-wins over
// slice order.
//
// What is asserted is the INVARIANT that makes the caller safe rather than the
// order-dependence itself: sort-then-resolve gives the same answer for any
// input permutation. That is the property the incremental path relies on when
// it hands over FoldFrameworkClassKinds' output, which is explicitly "the
// extractor's records first, then Detect's" — not canonical order.
func TestResolveHTTPEndpointHandlersFileScoped_CanonicalOrderMakesTheBindStable(t *testing.T) {
	handler := func(line int) types.EntityRecord {
		return types.EntityRecord{
			Kind: "SCOPE.Operation", Name: "cpDup", SourceFile: "routes.js",
			Language: "javascript", StartLine: line, EndLine: line + 2,
		}
	}
	endpoint := func() types.EntityRecord {
		r := fsEndpoint("http:GET:/dup", "routes.js", "SCOPE.Operation:cpDup")
		r.StartLine = 1
		return r
	}

	orders := map[string][]types.EntityRecord{
		"canonical":      {handler(10), handler(50), endpoint()},
		"reversed":       {handler(50), handler(10), endpoint()},
		"endpoint_first": {endpoint(), handler(50), handler(10)},
	}

	var first string
	for _, name := range []string{"canonical", "reversed", "endpoint_first"} {
		recs := append([]types.EntityRecord(nil), orders[name]...)
		types.SortEntityRecordsCanonical(recs)
		out, _ := ResolveHTTPEndpointHandlersFileScoped(recs, "test-repo")

		var got string
		for _, r := range out {
			if r.Name == "http:GET:/dup" {
				got = r.SourceFile + ":" + itoaSmall(r.StartLine)
			}
		}
		if got == "" {
			t.Fatalf("%s: endpoint vanished", name)
		}
		if first == "" {
			first = got
			continue
		}
		if got != first {
			t.Errorf("%s: endpoint rebound to %s, but the canonical ordering gives %s — "+
				"sort-then-resolve must be permutation-independent", name, got, first)
		}
	}
}
