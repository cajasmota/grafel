package links

// entity_props_diff_5954_test.go — the byte-identical-output gate for #5954
// slice 2 (entityNode.Properties: map[string]string -> types.Props).
//
// "The existing tests pass" is not evidence here. The existing tests assert
// individual facts about individual links; none of them assert that the FULL
// set of bytes the link pass writes is unchanged. The property representation
// swap touches ~14 passes, several of which stamp properties that end up
// serialised into the group link document and the per-pass sidecars, so the
// failure mode to guard is a subtle reordering or a dropped/duplicated key in
// one sidecar that no per-fact test happens to look at.
//
// The gate: run RunAllPasses over the fixture below, then compare EVERY byte
// of EVERY file the run writes under groups/ against goldens captured from the
// PRE-change (map-backed) implementation. Regenerate with:
//
//	GRAFEL_UPDATE_LINKOUT_GOLDEN=1 go test ./internal/links/ -run TestLinkPassOutput_ByteIdentical
//
// Regenerating is only legitimate when the output is INTENDED to change.
// Doing it to make this slice pass would defeat the entire point.
//
// HOW MUCH THIS GATE ACTUALLY COVERS — read before trusting it.
//
// It does NOT exercise every property-reading pass. Read the committed
// testdata/linkout_5954/g5954-link-pass-stats.json: of the 19 passes that run,
// only import (4 links), http (3 links), reachability (7 links / 8 candidates),
// label (4 candidates) and module_cycles (4 candidates) produce anything.
// constant_propagation, effect_propagation, taint_flow, openapi-spec, same_as,
// payload_drift, pure_functions, def_use, complexity and data_flow all report
// links_added=0, candidates=0 — they run and emit nothing, so their converted
// property sites contribute no bytes to compare.
//
// Concretely: this gate covers roughly 3 of the 14 passes whose property
// access was converted in #5954 slice 2. The remaining passes need real source
// files under FileRoot (the substrate passes read function bodies off disk) or
// richer entity graphs than a synthetic fixture supplies.
//
// Six converted Set calls, at four locations, are covered by NO test in this
// package — before or after this change:
//
//	def_use_pass.go:178-179        (chains / count stamp)
//	module_cycle_pass.go:145       (cycle tag stamp)
//	pure_function_pass.go:91       (the pure="false" branch)
//	dataflow_pass.go:552-553       (complexity-fallback branch)
//
// All are straight `m[k] = v` -> `p.Set(k, v)` rewrites with no branching
// introduced, and all were equally untested before the change. This note is
// disclosure of a pre-existing gap, not a claim that the gap is harmless.
//
// A demonstrated limit of this gate. Mutating types.Props.Set to insert a
// DUPLICATE at the correct sorted position instead of overwriting in place —
// i.e. breaking last-write-wins while keeping key order intact — was not caught
// by either golden test, nor by anything else in internal/links: the whole
// package stayed green. The only test that failed was TestPropsMatchesMapOracle
// in internal/types.
//
// The cruder shape of the same mutation (append at the end, breaking sort
// order as well) IS caught here, loudly, by ~11 tests including
// TestApplyResolverDynamicBaseURL and the effect-propagation suite. So the
// package's sensitivity to property-semantics bugs comes almost entirely from
// key ORDER being observable in output, not from key SEMANTICS being asserted.
// Byte-identity over a thin fixture is a weaker instrument than it looks, and
// internal/types' oracle test is doing more of the load-bearing work here than
// anything in this file.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const linkoutGoldenDir = "testdata/linkout_5954"

// rfc3339Stamp matches the "2026-07-27T21:14:55.855987Z" wall-clock values the
// pass writes into discovered_at / written_at.
var rfc3339Stamp = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`)

// propsDiffFixture writes a three-repo group carrying the property shapes the
// converted passes key off. Which of them actually FIRE is a separate question
// from which are represented here — see the coverage note at the top of this
// file, and check g5954-link-pass-stats.json rather than this list.
//
// Properties present in the fixture, and the observed outcome:
//
//   - http_pass:             pattern_type / verb / path / framework /
//     url_prefix / lookup_url_kwarg / source_caller, plus a URL mount point.
//     FIRES — 3 links.
//   - import/label/reachability/module_cycles: cross-repo import edges.
//     FIRE — 4 / 4 / 7 / 4 links or candidates.
//   - openapi_pass:          method+path (spec side), verb+path (code side).
//     Present but emits nothing — the pass needs a real spec artefact.
//   - constant_propagation:  url_kind=dynamic_baseurl + caller_file + path.
//     Present but emits nothing — no resolvable substrate binding on disk.
//   - effect_propagation:    handler. Emits nothing — needs source bodies.
//   - sameas_pass:           fields. Emits nothing — needs matching model
//     shapes the heuristic will accept.
//
// The four "present but silent" entries are kept deliberately: they cost
// nothing, and they are the fixture rows to extend first if someone widens
// this gate later.
func propsDiffFixture(t *testing.T, root string) {
	t.Helper()

	writeFixture(t, root, fixtureGraph{
		Repo: "acme-core",
		Entities: []map[string]any{
			{"id": "view1", "name": "OrderViewSet", "kind": "Controller",
				"source_file": "core/views/orders.py",
				"properties":  map[string]any{"handler": "OrderViewSet", "framework": "django"}},
			{"id": "ep-prod-1", "name": "http:GET:/api/v1/orders", "kind": "http_endpoint",
				"source_file": "core/views/orders.py",
				"properties": map[string]any{
					"verb": "GET", "path": "/api/v1/orders", "framework": "django",
					"pattern_type": "http_endpoint_synthesis",
				}},
			{"id": "ep-prod-2", "name": "http:POST:/api/v1/orders/{id}", "kind": "http_endpoint",
				"source_file": "core/views/orders.py",
				"properties": map[string]any{
					"verb": "POST", "path": "/api/v1/orders/{id}", "framework": "django",
					"pattern_type": "http_endpoint_synthesis", "lookup_url_kwarg": "order_id",
				}},
			{"id": "mount1", "name": "urls", "kind": "http_endpoint",
				"source_file": "core/urls.py",
				"properties": map[string]any{
					"pattern_type": patternTypeURLMountPoint, "url_prefix": "/api/v1",
					"path": "/api/v1",
				}},
			{"id": "model1", "name": "Order", "kind": "Model",
				"source_file": "core/models.py",
				"properties":  map[string]any{"fields": "id,total,status,created_at"}},
			{"id": "spec1", "name": "openapi:GET:/api/v1/orders", "kind": "openapi_operation",
				"source_file": "openapi.yaml",
				"properties":  map[string]any{"method": "get", "path": "/api/v1/orders"}},
		},
		Edges: []map[string]string{
			{"from_id": "view1", "to_id": "ep-prod-1", "kind": "IMPLEMENTS"},
			{"from_id": "view1", "to_id": "ep-prod-2", "kind": "IMPLEMENTS"},
			{"from_id": "view1", "to_id": "model1", "kind": "CALLS"},
		},
	})

	writeFixture(t, root, fixtureGraph{
		Repo: "acme-web",
		Entities: []map[string]any{
			{"id": "svc1", "name": "OrderService", "kind": "Class",
				"source_file": "src/api/orders.ts",
				"properties":  map[string]any{"language": "typescript"}},
			{"id": "ep-cons-1", "name": "http:GET:/api/v1/orders", "kind": "http_endpoint",
				"source_file": "src/api/orders.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "/api/v1/orders", "framework": "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Class:OrderService", "url_kind": "literal",
				}},
			{"id": "ep-cons-2", "name": "http:POST:/orders/{orderId}", "kind": "http_endpoint",
				"source_file": "src/api/orders.ts",
				"properties": map[string]any{
					"verb": "POST", "path": "/orders/{orderId}", "framework": "axios",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Class:OrderService",
				}},
			{"id": "ep-cons-3", "name": "http:GET:/api/v1/orders", "kind": "http_endpoint",
				"source_file": "src/api/dyn.ts",
				"properties": map[string]any{
					"verb": "GET", "path": "${BASE}/api/v1/orders", "framework": "fetch",
					"pattern_type": "http_endpoint_client_synthesis",
					"url_kind":     "dynamic_baseurl", "caller_file": "src/api/dyn.ts",
				}},
			{"id": "model2", "name": "Order", "kind": "Interface",
				"source_file": "src/models/order.ts",
				"properties":  map[string]any{"fields": "id,total,status,created_at"}},
			{"id": "spec2", "name": "openapi:POST:/api/v1/orders/{id}", "kind": "openapi_operation",
				"source_file": "spec/openapi.yaml",
				"properties":  map[string]any{"method": "post", "path": "/api/v1/orders/{id}"}},
		},
		Edges: []map[string]string{
			{"from_id": "svc1", "to_id": "ep-cons-1", "kind": "CALLS"},
			{"from_id": "svc1", "to_id": "ep-cons-2", "kind": "CALLS"},
			{"from_id": "svc1", "to_id": "acme-core::model1", "kind": "IMPORTS"},
		},
	})

	writeFixture(t, root, fixtureGraph{
		Repo: "acme-worker",
		Entities: []map[string]any{
			{"id": "job1", "name": "OrderReconcileJob", "kind": "Function",
				"source_file": "worker/jobs/reconcile.go",
				"properties":  map[string]any{"language": "go", "line": "42"}},
			{"id": "ep-cons-4", "name": "http:GET:/api/v1/orders", "kind": "http_endpoint",
				"source_file": "worker/jobs/reconcile.go",
				"properties": map[string]any{
					"verb": "GET", "path": "/api/v1/orders", "framework": "nethttp",
					"pattern_type":  "http_endpoint_client_synthesis",
					"source_caller": "Function:OrderReconcileJob",
				}},
			{"id": "pkg1", "name": "OrderBook", "kind": "class",
				"source_file": "worker/pkg/book.go",
				"properties":  map[string]any{"language": "go"}},
		},
		Edges: []map[string]string{
			{"from_id": "job1", "to_id": "ep-cons-4", "kind": "CALLS"},
			{"from_id": "job1", "to_id": "model1", "kind": "imports"},
		},
	})
}

// collectLinkOutputs runs the fixture and returns filename -> file bytes for
// every file the pass wrote under <home>/groups, with the (per-run, temporary)
// root and home paths scrubbed so the bytes are comparable across machines.
func collectLinkOutputs(t *testing.T) map[string]string {
	t.Helper()

	root := t.TempDir()
	home := t.TempDir()
	propsDiffFixture(t, root)

	if _, err := RunAllPasses("g5954", root, home); err != nil {
		t.Fatalf("RunAllPasses: %v", err)
	}

	groups := filepath.Join(home, "groups")
	ents, err := os.ReadDir(groups)
	if err != nil {
		t.Fatalf("read groups dir: %v", err)
	}
	// Resolve symlinks too: on macOS t.TempDir() lives under /var, which is a
	// symlink to /private/var, and the pass records the resolved form.
	scrub := []string{root, home}
	for _, p := range []string{root, home} {
		if rp, err := filepath.EvalSymlinks(p); err == nil && rp != p {
			scrub = append(scrub, rp)
		}
	}

	out := map[string]string{}
	for _, de := range ents {
		if de.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(groups, de.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", de.Name(), err)
		}
		s := string(b)
		for _, p := range scrub {
			s = strings.ReplaceAll(s, p, "<TMP>")
		}
		// discovered_at / written_at are wall-clock stamps; they vary between
		// two runs of the SAME implementation and so carry no information
		// about the property representation. Everything else is compared byte
		// for byte. TestLinkPassOutput_GoldenIsDeterministic proves this is
		// the ONLY varying field — if a second source of nondeterminism ever
		// appears it fails there rather than being silently absorbed here.
		s = rfc3339Stamp.ReplaceAllString(s, "<TS>")
		out[de.Name()] = s
	}
	if len(out) == 0 {
		t.Fatal("the fixture produced no output files — the gate would assert nothing")
	}
	return out
}

// TestLinkPassOutput_ByteIdenticalToPreCompactionGolden is the gate described
// at the top of this file.
func TestLinkPassOutput_ByteIdenticalToPreCompactionGolden(t *testing.T) {
	got := collectLinkOutputs(t)

	if os.Getenv("GRAFEL_UPDATE_LINKOUT_GOLDEN") == "1" {
		if err := os.RemoveAll(linkoutGoldenDir); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(linkoutGoldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range got {
			if err := os.WriteFile(filepath.Join(linkoutGoldenDir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Fatalf("regenerated %d goldens in %s — rerun without GRAFEL_UPDATE_LINKOUT_GOLDEN to verify",
			len(got), linkoutGoldenDir)
	}

	goldenEntries, err := os.ReadDir(linkoutGoldenDir)
	if err != nil {
		t.Fatalf("read goldens (regenerate with GRAFEL_UPDATE_LINKOUT_GOLDEN=1): %v", err)
	}
	want := map[string]string{}
	for _, de := range goldenEntries {
		b, err := os.ReadFile(filepath.Join(linkoutGoldenDir, de.Name()))
		if err != nil {
			t.Fatal(err)
		}
		want[de.Name()] = string(b)
	}

	names := map[string]bool{}
	for n := range want {
		names[n] = true
	}
	for n := range got {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	for _, n := range sorted {
		w, okW := want[n]
		g, okG := got[n]
		switch {
		case !okW:
			t.Errorf("%s: the run emitted a file the pre-change implementation did not", n)
		case !okG:
			t.Errorf("%s: the pre-change implementation emitted this file and the run did not", n)
		case w != g:
			t.Errorf("%s: output differs from the pre-change implementation\n--- want (%d bytes)\n%s\n--- got (%d bytes)\n%s",
				n, len(w), firstDiffContext(w, g), len(g), firstDiffContext(g, w))
		}
	}
}

// TestLinkPassOutput_GoldenIsDeterministic guards the gate itself: if the pass
// output varied run to run (map iteration order leaking into a sidecar, a
// timestamp), the byte-comparison above would flake rather than catch anything,
// and someone would "fix" it by loosening the comparison.
func TestLinkPassOutput_GoldenIsDeterministic(t *testing.T) {
	a := collectLinkOutputs(t)
	b := collectLinkOutputs(t)
	if len(a) != len(b) {
		t.Fatalf("run emitted %d files then %d — output file set is not deterministic", len(a), len(b))
	}
	for n, av := range a {
		bv, ok := b[n]
		if !ok {
			t.Errorf("%s present in the first run only", n)
			continue
		}
		if av != bv {
			t.Errorf("%s differs between two identical runs — the pass output is not deterministic\n%s",
				n, firstDiffContext(av, bv))
		}
	}
}

// firstDiffContext returns a short window of a around the first byte at which
// it diverges from b, so a failure message is readable for multi-KB JSON.
func firstDiffContext(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	lo := i - 120
	if lo < 0 {
		lo = 0
	}
	hi := i + 120
	if hi > len(a) {
		hi = len(a)
	}
	return "…" + a[lo:hi] + "…"
}
