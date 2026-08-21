package engine

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6485 — a `Route` entity must never be the FromID of an endpoint IMPLEMENTS
// edge.
//
// The phase-2 handler index used to exclude only the three http_endpoint*
// kinds, so `Route` records were eligible handler targets. When an endpoint's
// handler ref failed to resolve to a real handler, a same-file lookup could
// bind the endpoint to the Route entity named after its own path and emit
//
//	Route:/foo --IMPLEMENTS--> http_endpoint_definition:http:ANY:/foo
//
// i.e. the graph asserting that route `/foo` is implemented by route `/foo`. A
// consumer asking *which handler serves this route* gets the route's own path
// back — a confidently wrong answer, which for an agent reading the graph is
// worse than a missing edge.
//
// These tests CONSTRUCT the shape directly instead of relying on a framework
// fixture. Three separate per-framework fixes (#6429 Java Spring, #6484
// Django DRF, and the residual Django `("Route", raw)` fallback) have each
// removed one producer, so every fixture that once emitted this shape has since
// been repaired and a fixture-based test would pass VACUOUSLY. The defect lives
// in the index, not in any one synthesizer.
//
// Both entry points are exercised, because they can reach different verdicts
// for an endpoint whose handler does not resolve:
//
//   - ResolveHTTPEndpointHandlersWithRepo — the CLI/full-rebuild path
//     (keepUnresolved=false), which drops an endpoint whose handler ref matches
//     nothing corpus-wide.
//   - ResolveHTTPEndpointHandlersFileScoped — the daemon/incremental path
//     (keepUnresolved=true), which keeps it edgeless instead (#6150).
//
// keepUnresolved guards only that one branch, though — `drop[i]` is honoured by
// the post-loop compaction on BOTH paths — so "the file-scoped path never
// deletes an endpoint" is false in general and must not be assumed here. The
// tests below assert the surviving or dropped endpoint on each arm explicitly
// rather than inferring it from keepUnresolved.

// routeImplementsFromKinds returns the FromID KIND distribution of every
// endpoint IMPLEMENTS edge in `recs` — the measurement this issue demands. A
// bare edge total is blind here: the bogus Route self-edge scores as a
// *success* today, so its absence would look like a regression tomorrow. Only
// the kind breakdown distinguishes "one fewer wrong edge" from "one fewer
// right edge".
func routeImplementsFromKinds(recs []types.EntityRecord) map[string]int {
	dist := map[string]int{}
	for i := range recs {
		for _, rel := range recs[i].Relationships {
			if rel.Kind != implementsEdgeKind {
				continue
			}
			if !strings.Contains(rel.ToID, "http_endpoint") {
				continue
			}
			kind := rel.FromID
			if c := strings.IndexByte(kind, ':'); c >= 0 {
				kind = kind[:c]
			}
			dist[kind]++
		}
	}
	return dist
}

// logDist renders the distribution deterministically, labelled by resolve path.
func logDist(t *testing.T, path string, dist map[string]int) {
	t.Helper()
	keys := make([]string, 0, len(dist))
	for k := range dist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		t.Logf("[%s] endpoint IMPLEMENTS FromID kind distribution: (none)", path)
		return
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+formatLine(dist[k]))
	}
	t.Logf("[%s] endpoint IMPLEMENTS FromID kind distribution: %s", path, strings.Join(parts, " "))
}

// routeSelfEdgeCorpus is the exact shape: a Route entity named after the path,
// and an endpoint synthetic in the SAME file whose source_handler names it.
// Same-file placement is deliberate — the same-file EXACT lookup
// (`idx[{hk, hn, lookupFile}]`) fires BEFORE the `hk == "Route"` drop, so a fix
// applied only at the drop site leaves this live with a green suite.
func routeSelfEdgeCorpus() []types.EntityRecord {
	return []types.EntityRecord{
		{
			Kind:       "Route",
			Name:       "/foo",
			SourceFile: "urls.py",
			Language:   "python",
			StartLine:  7,
		},
		{
			Kind:       httpEndpointKind,
			Name:       "http:ANY:/foo",
			SourceFile: "urls.py",
			Language:   "python",
			StartLine:  7,
			Properties: map[string]string{
				"source_handler": "Route:/foo",
				"framework":      "django",
			},
		},
	}
}

// routeBareNameCorpus is the SECOND binding path onto a Route, independent of
// the handler ref's kind. sameFileBareIdx is keyed on (file, bare name) with NO
// kind, so a `Controller:` ref whose name happens to match a Route entity in
// the same file binds to that Route as well. Excluding Route at the drop site
// (which only inspects `hk`) cannot see this one at all; excluding it from
// index CONSTRUCTION closes both.
func routeBareNameCorpus() []types.EntityRecord {
	return []types.EntityRecord{
		{
			Kind:       "Route",
			Name:       "user_detail",
			SourceFile: "urls.py",
			Language:   "python",
			StartLine:  11,
		},
		{
			Kind:       httpEndpointKind,
			Name:       "http:GET:/users/{id}",
			SourceFile: "urls.py",
			Language:   "python",
			StartLine:  11,
			Properties: map[string]string{
				"source_handler": "Controller:user_detail",
				"framework":      "django",
			},
		},
	}
}

func endpointNames(recs []types.EntityRecord) map[string]bool {
	out := make(map[string]bool, len(recs))
	for _, r := range recs {
		if strings.HasPrefix(r.Kind, "http_endpoint") {
			out[r.Name] = true
		}
	}
	return out
}

// assertNoRouteEdge is the acceptance criterion proper, and it holds for every
// scenario and both entry points: no endpoint IMPLEMENTS edge may originate
// from a Route entity.
func assertNoRouteEdge(t *testing.T, path string, out []types.EntityRecord) map[string]int {
	t.Helper()
	dist := routeImplementsFromKinds(out)
	logDist(t, path, dist)
	if n := dist["Route"]; n != 0 {
		t.Errorf("[%s] %d endpoint IMPLEMENTS edge(s) originate from a Route entity; a route cannot implement itself (dist=%v)", path, n, dist)
	}
	return dist
}

// assertRoutePlaceholderKept is the paired NoHandlerProp keep-path decision for
// the `Route:<path>` PLACEHOLDER ref specifically. Both entry points must reach
// the same verdict: the endpoint SURVIVES, edgeless, attributed by its
// source_handler property.
//
// The reasoning is narrow on purpose. A `Route:<path>` ref never named a
// handler at all — it is the synthesizer saying "I did not have the method
// name". So the failure to bind is evidence about the REF, not about the route,
// and the route provably exists: a real registration produced it. That is
// scope-independent, hence identical on both paths. Contrast
// TestRouteBareNameNoLongerBinds_6485 below, where the ref names a handler that
// genuinely does not exist and the pre-existing #6150 scope-dependent verdict
// correctly applies instead.
func assertRoutePlaceholderKept(t *testing.T, path, endpointName string, out []types.EntityRecord, stats ResolveHTTPEndpointStats) {
	t.Helper()
	assertNoRouteEdge(t, path, out)
	if !endpointNames(out)[endpointName] {
		t.Errorf("[%s] endpoint %q vanished; a placeholder handler ref must leave the endpoint edgeless, not delete a real route (stats=%+v)", path, endpointName, stats)
	}
	if stats.HandlerResolved != 0 {
		t.Errorf("[%s] HandlerResolved=%d, want 0 — nothing legitimate resolved here (stats=%+v)", path, stats.HandlerResolved, stats)
	}
	if stats.HandlerDropped != 0 {
		t.Errorf("[%s] HandlerDropped=%d, want 0 — the route is real and must be kept (stats=%+v)", path, stats.HandlerDropped, stats)
	}
	if stats.NoHandlerProp != 1 {
		t.Errorf("[%s] NoHandlerProp=%d, want 1 — the endpoint is kept, attributed by property only (stats=%+v)", path, stats.NoHandlerProp, stats)
	}
}

func TestRouteIsNotAnEligibleHandler_CorpusScoped_6485(t *testing.T) {
	out, stats := ResolveHTTPEndpointHandlersWithRepo(routeSelfEdgeCorpus(), "repo")
	assertRoutePlaceholderKept(t, "CLI/corpus-scoped keepUnresolved=false", "http:ANY:/foo", out, stats)
}

func TestRouteIsNotAnEligibleHandler_FileScoped_6485(t *testing.T) {
	out, stats := ResolveHTTPEndpointHandlersFileScoped(routeSelfEdgeCorpus(), "repo")
	assertRoutePlaceholderKept(t, "daemon/file-scoped keepUnresolved=true", "http:ANY:/foo", out, stats)
}

// TestRouteBareNameNoLongerBinds_6485 covers the SECOND binding path, where the
// handler ref is `Controller:user_detail` and the only same-file entity with
// that bare name is a Route. The `hk == "Route"` branch cannot see this case at
// all — `hk` is "Controller" — which is precisely why the exclusion has to live
// in index construction.
//
// The verdict here is deliberately NOT the placeholder keep. This ref names a
// handler kind and a handler name, and once the Route coincidence is removed no
// such handler exists anywhere in `merged`. That is the ordinary "ref names a
// handler that does not exist" condition, and the established #6150 policy
// applies unchanged and scope-dependently: the corpus-scoped path, which can
// see the whole corpus, drops it as a genuine orphan; the file-scoped path,
// which cannot tell "absent" from "in another file", keeps it unenriched.
// Widening the placeholder keep to cover this case would silently repeal #6150
// for every orphaned handler ref in the codebase.
//
// Both arms are asserted so the difference is pinned from both sides rather
// than assumed, and neither emits a Route-sourced edge.
func TestRouteBareNameNoLongerBinds_6485(t *testing.T) {
	const name = "http:GET:/users/{id}"

	out, stats := ResolveHTTPEndpointHandlersWithRepo(routeBareNameCorpus(), "repo")
	path := "CLI/corpus-scoped keepUnresolved=false (bare-name index)"
	assertNoRouteEdge(t, path, out)
	if stats.HandlerResolved != 0 {
		t.Errorf("[%s] HandlerResolved=%d, want 0 — a Route must not satisfy a Controller ref (stats=%+v)", path, stats.HandlerResolved, stats)
	}
	if stats.HandlerDropped != 1 {
		t.Errorf("[%s] HandlerDropped=%d, want 1 — no such handler exists corpus-wide, so #6150 drops it (stats=%+v)", path, stats.HandlerDropped, stats)
	}
	if endpointNames(out)[name] {
		t.Errorf("[%s] endpoint %q survived; the corpus-scoped path drops a genuinely orphaned handler ref (#6150)", path, name)
	}

	out, stats = ResolveHTTPEndpointHandlersFileScoped(routeBareNameCorpus(), "repo")
	path = "daemon/file-scoped keepUnresolved=true (bare-name index)"
	assertNoRouteEdge(t, path, out)
	if stats.HandlerResolved != 0 {
		t.Errorf("[%s] HandlerResolved=%d, want 0 — a Route must not satisfy a Controller ref (stats=%+v)", path, stats.HandlerResolved, stats)
	}
	if stats.HandlerUnresolvedKept != 1 {
		t.Errorf("[%s] HandlerUnresolvedKept=%d, want 1 — a file-scoped caller keeps what it cannot see (#6150) (stats=%+v)", path, stats.HandlerUnresolvedKept, stats)
	}
	if !endpointNames(out)[name] {
		t.Errorf("[%s] endpoint %q vanished; the file-scoped path must keep it unenriched (#6150)", path, name)
	}
}

// TestRealHandlerStillResolves_6485 is the permissiveness counterweight: the
// exclusion must remove ONLY Route entities from the handler index. A genuine
// same-file Controller handler still resolves and still emits its IMPLEMENTS
// edge, even when a Route entity for the same path sits alongside it.
func TestRealHandlerStillResolves_6485(t *testing.T) {
	merged := []types.EntityRecord{
		{Kind: "Route", Name: "/foo", SourceFile: "urls.py", Language: "python", StartLine: 7},
		{Kind: "Controller", Name: "foo_view", SourceFile: "urls.py", Language: "python", StartLine: 20},
		{
			Kind:       httpEndpointKind,
			Name:       "http:GET:/foo",
			SourceFile: "urls.py",
			Language:   "python",
			StartLine:  7,
			Properties: map[string]string{"source_handler": "Controller:foo_view", "framework": "django"},
		},
	}
	out, stats := ResolveHTTPEndpointHandlersWithRepo(merged, "repo")
	dist := routeImplementsFromKinds(out)
	logDist(t, "CLI/corpus-scoped keepUnresolved=false (real handler)", dist)
	if stats.HandlerResolved != 1 {
		t.Errorf("HandlerResolved=%d, want 1 — a genuine Controller handler must still resolve (stats=%+v)", stats.HandlerResolved, stats)
	}
	if dist["Controller"] != 1 {
		t.Errorf("want exactly 1 Controller-sourced endpoint IMPLEMENTS edge, dist=%v", dist)
	}
	if dist["Route"] != 0 {
		t.Errorf("want 0 Route-sourced endpoint IMPLEMENTS edges, dist=%v", dist)
	}
}
