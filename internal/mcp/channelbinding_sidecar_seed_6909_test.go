package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/links"
)

// ---------------------------------------------------------------------------
// #6909 — SCOPE.ChannelBinding was a dead-code entry seed in internal/mcp and
// NOT in internal/links, and the sidecar is what decides.
//
// ADR-0025 §3 asks that a ChannelBinding — a config-side messaging declaration
// with no callers by design — is never reported as dead code, and #5782
// implemented that in internal/mcp's frameworkEntryKindsMCP. But handleDeadCode
// PREFERS the on-disk <group>-links-reachability.json sidecar and consults its
// own seed set only when `from` is set or the sidecar fails to load. The sidecar
// is written by internal/links' frameworkEntryKinds, which lacked the kind — so
// on the normal path a ChannelBinding was reported dead.
//
// WHY THIS TEST GOES THROUGH links.RunAllPasses AND NOT newTestServer ALONE:
// a test that hands handleDeadCode an in-memory graph with no sidecar exercises
// computeDeadCodeLive — the FALLBACK — which already honoured ADR-0025 and would
// pass with the defect fully present (#6902's TestDeadCode_BareRouteIsAFramework
// EntryPoint_6902 is exactly that shape). The defect lives on the path
// "link pass writes the sidecar → handleDeadCode reads it", so the test drives
// the real link pass and then asserts what the tool says. The
// `source == "sidecar"` assertion below is the guard that keeps it there: if the
// handler ever falls back to a recompute, this test stops testing the defect and
// says so instead of quietly passing.
//
// GRADING BOTH DIRECTIONS. Adding a seed is a WIDENING — it makes more entities
// reachable, i.e. produces FEWER dead-code reports, which can mask genuinely
// dead code. So the test names one entity that must STOP being reported (the
// ChannelBinding) and one that must STILL be reported (deadFn6909, an
// unexported, uncalled, unsniffable function with no inbound edges).
//
// POSITIVE CONTROL for the forbidden row: deadFn6909 has no inbound edge of any
// reachability kind, its kind (SCOPE.Function) is in neither seed set, and it is
// not a Go entry-point the pass's source sniffer recognises — it is not `main`,
// not `init`, not a test function. Nothing but a wrongly-widened seed set can
// make it reachable.
// ---------------------------------------------------------------------------

const (
	channelBindingName6909 = "mp.messaging.outgoing.orders-6909"
	deadFnName6909         = "reconcileLedger6909"
)

// deadCodeDoc6909 is the in-memory twin of the fixture graph written to disk
// below. handleDeadCode needs a loaded group to resolve at all; the VERDICT it
// returns comes from the sidecar, not from this document.
func deadCodeDoc6909() *graph.Document {
	return &graph.Document{
		Repo: "svc6909",
		Entities: []graph.Entity{
			{ID: "cb-6909", Name: channelBindingName6909, Kind: "SCOPE.ChannelBinding",
				SourceFile: "src/application.properties", StartLine: 1},
			{ID: "dead-6909", Name: deadFnName6909, Kind: "SCOPE.Function",
				SourceFile: "src/ledger.go", StartLine: 3},
		},
	}
}

// runLinkPassForDeadCode6909 writes a one-repo fixture graph plus its real
// source files, runs the actual link passes, and returns the group name. The
// reachability sidecar it leaves behind is the artefact handleDeadCode reads.
func runLinkPassForDeadCode6909(t *testing.T) string {
	t.Helper()

	// Isolate: the sidecar must land under a temp home, and handleDeadCode
	// resolves its own path from GRAFEL_HOME. Resolving the home through
	// links.GroupHome("") and handing THAT to RunAllPasses is what makes the
	// writer and the reader agree by construction rather than by convention.
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_DAEMON_ROOT", "")
	home, err := links.GroupHome("")
	if err != nil {
		t.Fatalf("GroupHome: %v", err)
	}

	graphsRoot := t.TempDir()
	repoRoot := filepath.Join(graphsRoot, "svc6909")
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(repoRoot, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	// Real source files: #6839 makes the pass DEGRADE a repo whose source root
	// or entry-point files it cannot read, and a degraded repo produces no dead
	// list at all — which would make every assertion below vacuous.
	write("src/ledger.go", "package svc\n\nfunc reconcileLedger6909() {}\n")
	write("src/application.properties", "mp.messaging.outgoing.orders-6909.topic=orders\n")

	doc := map[string]any{
		"version": 1,
		"repo":    "svc6909",
		"entities": []map[string]any{
			{"id": "cb-6909", "name": channelBindingName6909,
				"kind": "SCOPE.ChannelBinding", "source_file": "src/application.properties"},
			{"id": "dead-6909", "name": deadFnName6909,
				"kind": "SCOPE.Function", "source_file": "src/ledger.go"},
		},
		"relationships": []map[string]string{},
	}
	buf, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture graph: %v", err)
	}
	write(filepath.Join(".grafel", "graph.json"), string(buf))

	if _, err := links.RunAllPasses("test", graphsRoot, home); err != nil {
		t.Fatalf("RunAllPasses: %v", err)
	}

	sidecar, err := links.PassSidecarPath("", "test", "reachability")
	if err != nil {
		t.Fatalf("PassSidecarPath: %v", err)
	}
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("the link pass wrote no reachability sidecar at %s (%v); "+
			"handleDeadCode would fall back to a live recompute and this test "+
			"would grade the fallback, not the defect", sidecar, err)
	}
	return "test"
}

// TestDeadCode_ChannelBindingIsSeededOnTheSidecarPath_6909 is the whole issue:
// the link pass's seed set decides, via the sidecar, what grafel_dead_code says.
func TestDeadCode_ChannelBindingIsSeededOnTheSidecarPath_6909(t *testing.T) {
	group := runLinkPassForDeadCode6909(t)

	srv := newTestServer(t, deadCodeDoc6909())
	out := callFlowTool(t, srv.handleDeadCode, map[string]any{
		"group": group,
		"limit": float64(200),
	})

	// 0. PATH GUARD. Everything below only grades #6909 if the verdict came
	//    from the sidecar the link pass just wrote.
	if src, _ := out["source"].(string); src != "sidecar" {
		t.Fatalf("source = %q, want \"sidecar\". #6909 only exists on the sidecar "+
			"path — the live-recompute fallback already seeds SCOPE.ChannelBinding, "+
			"so on any other path this test grades nothing. Response: %v", src, out)
	}

	items, _ := out["dead_code"].([]any)
	names := make([]string, 0, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		n, _ := m["name"].(string)
		names = append(names, n)
	}
	has := func(want string) bool {
		for _, n := range names {
			if n == want {
				return true
			}
		}
		return false
	}

	// 1. VACUITY GUARD / the forbidden direction. Seeding a new kind is a
	//    widening; a widening that also silences genuinely dead code is a
	//    regression, and it is also the failure mode that would make the
	//    absence assertion in (2) free.
	if !has(deadFnName6909) {
		t.Fatalf("dead_code = %v does not report %q, an uncalled unexported "+
			"function with no inbound edges. Either the widening in #6909 leaked "+
			"beyond SCOPE.ChannelBinding, or the pass produced no verdicts at all "+
			"(a degraded repo) — in both cases the assertion below is vacuous. "+
			"Response: %v", names, deadFnName6909, out)
	}

	// 2. The #6909 direction: ADR-0025 on the path the tool actually uses.
	if has(channelBindingName6909) {
		t.Errorf("dead_code = %v reports the SCOPE.ChannelBinding %q as dead. "+
			"internal/links' frameworkEntryKinds is what wrote this sidecar; if it "+
			"does not seed SCOPE.ChannelBinding then ADR-0025's guarantee exists "+
			"only on handleDeadCode's fallback path, which is precisely the path "+
			"that does not run once a group has had link passes.",
			names, channelBindingName6909)
	}
}

// TestReachabilitySeedSetsAreNotDuplicated_6909 pins the DURABLE half. The two
// seed sets were hand-maintained copies in two packages and diverged twice — on
// bare "Route" (#6902) and on SCOPE.ChannelBinding (this issue). The fix deletes
// the copy rather than asserting equality between two things that can drift
// together, so what is pinned here is that internal/mcp reads the link pass's
// own predicate and agrees with it on both of the kinds that have gone wrong.
//
// The expected values are INDEPENDENT literals, not one map read against the
// other: SCOPE.ChannelBinding and Route must be seeds (ADR-0025, #6902), and
// SCOPE.Class must not be, or the dead-code tool reports nothing at all.
func TestReachabilitySeedSetsAreNotDuplicated_6909(t *testing.T) {
	// ENUMERATION, not sampling. The review of this PR scored three mutants
	// ALIVE against the positive/negative loops below: deleting the
	// pre-existing seed "SCOPE.GrpcMethod" (every gRPC method then reports as
	// dead code), adding a plausible new seed "SCOPE.Interface", and adding an
	// empty-string key. A list of SOME members grades only those members, and
	// nothing tells a reader which half is covered — so the whole set is
	// asserted against a literal written out BY HAND here. It is deliberately
	// not derived from links.FrameworkEntryKinds(): a want list computed from
	// the set under test agrees with any deletion.
	//
	// Changing the seed set intentionally? Edit this literal in the same
	// commit, and say in the message which kind moved and why.
	wantSeeds := []string{
		"Route",                    // #6902, bare spelling
		"SCOPE.ChannelBinding",     // #5782 / ADR-0025, #6909
		"SCOPE.Endpoint",           //
		"SCOPE.EventBusEvent",      //
		"SCOPE.GrpcMethod",         //
		"SCOPE.MessageTopic",       // #5781
		"SCOPE.Route",              // #6902, prefixed spelling
		"SCOPE.ServerlessFunction", //
		"http_endpoint",            //
		"http_endpoint_definition", //
	}
	gotSeeds := links.FrameworkEntryKinds()
	if !slices.Equal(gotSeeds, wantSeeds) {
		t.Errorf("links.FrameworkEntryKinds() has drifted from the set this "+
			"repo intends to seed the dead-code BFS from.\n got:  %q\n want: %q\n"+
			"A kind REMOVED here is reported as dead code from the next link pass "+
			"on; a kind ADDED makes it and its whole transitive closure "+
			"unconditionally reachable, silencing genuine findings. If the change "+
			"is intended, edit wantSeeds in the same commit.", gotSeeds, wantSeeds)
	}

	// The two loops below are kept for the diagnosis they give: the exact-set
	// assertion says "drifted", these say which direction and why it matters.
	for _, kind := range []string{
		"SCOPE.ChannelBinding", // #5782 / ADR-0025
		"Route",                // #6902
		"SCOPE.Route",
		"SCOPE.MessageTopic",
		"http_endpoint_definition",
	} {
		if !links.IsFrameworkEntryKind(kind) {
			t.Errorf("links.IsFrameworkEntryKind(%q) = false; grafel_dead_code seeds "+
				"its BFS from this predicate on both its sidecar and recompute paths, "+
				"so a missing kind is reported as dead code.", kind)
		}
	}
	for _, kind := range []string{"SCOPE.Class", "SCOPE.Function", "SCOPE.Variable"} {
		if links.IsFrameworkEntryKind(kind) {
			t.Errorf("links.IsFrameworkEntryKind(%q) = true. Seeding an ordinary "+
				"code kind makes every entity of that kind unconditionally reachable "+
				"and silences the dead-code tool.", kind)
		}
	}
	// The SECOND exported predicate, enumerated the same way and for a sharper
	// reason: this set is the BFS TRAVERSAL FILTER, so a member leaving it makes
	// entities reachable only through that edge kind report as DEAD — the tool
	// names live code for deletion. Review of this PR scored deleting "IMPORTS"
	// ALIVE against every test in the repo. Hand-written, not derived from the
	// map under test, for the same reason as wantSeeds above.
	wantEdgeKinds := []string{
		"CALLS", "CONSUMES", "CONTAINS", "DEPENDS_ON", "DISCRIMINATES_ON",
		"ENTRY_POINT_OF", "EXTENDS", "FETCHES", "HANDLES", "HANDLES_SIGNAL",
		"IMPLEMENTS", "IMPORTS", "NAVIGATES_TO", "PRODUCES", "REFERENCES",
		"REGISTERS", "RENDERS", "RESOLVES_TO", "ROUTES_TO", "STEP_IN_PROCESS",
		"TESTS", "UNRESOLVED_FETCH", "USES", "USES_HOOK",
	}
	gotEdgeKinds := links.ReachabilityEdgeKinds()
	if !slices.Equal(gotEdgeKinds, wantEdgeKinds) {
		t.Errorf("links.ReachabilityEdgeKinds() has drifted from the set this repo "+
			"intends to propagate reachability along.\n got:  %q\n want: %q\n"+
			"A kind REMOVED here makes every entity reachable only via that edge "+
			"report as dead code — the false-positive direction, which sends users "+
			"to delete working code. A kind ADDED lights up entities nothing really "+
			"reaches. If the change is intended, edit wantEdgeKinds in the same "+
			"commit. (internal/coverage has an identically-named set answering test "+
			"reachability — it is a name collision, not this set.)",
			gotEdgeKinds, wantEdgeKinds)
	}

	if !links.IsReachabilityEdgeKind("CALLS") || !links.IsReachabilityEdgeKind("CONTAINS") {
		t.Errorf("links.IsReachabilityEdgeKind must accept CALLS and CONTAINS; "+
			"got CALLS=%v CONTAINS=%v",
			links.IsReachabilityEdgeKind("CALLS"), links.IsReachabilityEdgeKind("CONTAINS"))
	}
	if links.IsReachabilityEdgeKind("SAME_AS") {
		t.Error("links.IsReachabilityEdgeKind(\"SAME_AS\") = true; a similarity edge " +
			"does not propagate reachability.")
	}
}
