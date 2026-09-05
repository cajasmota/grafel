package links

import (
	"os"
	"path/filepath"
	"testing"
)

// constant_propagation_call_site_6450_test.go — #6450 Task 2.
//
// The link pass used to re-run the engine's consumer-side dynamic_baseurl
// fold over graphs it had just loaded from disk. That copy was deleted:
// internal/engine.FoldConsumerHTTPBaseURLs already ran at index time and
// persisted url_kind="literal", so the link-pass copy had nothing left to
// fold. Nothing in the suite graded that call site — the pre-existing
// coverage called the helper directly, so removing the call site left it
// green. This file grades the pipeline instead: it runs RunAllPasses end
// to end and asserts the tier stamped on the emitted <group>-links.json.
//
// It is a two-directional pin. If the fold is reintroduced into the link
// pass, the consumer path is rewritten to a literal before runHTTPPass and
// the pair matches on the canonical (verb, path) id — extraction_confidence
// flips resolved and resolve_strategy clears, and this test fails. If the
// dynamic_suffix_match tier that now carries the pair regresses, the link
// disappears and this test fails.
//
// Scope of the claim: it observes the emitted links document only. It says
// nothing about entity Properties, about substrate_* (which has no
// production reader), and nothing about the engine-side fold.

// substrateFoldFixture writes a two-repo group whose consumer carries an
// unfolded dynamic_baseurl path AND a source file from which the substrate
// resolver could bind that base URL — i.e. the exact input the deleted link
// -pass fold would have consumed. Returns the graphs root and home dir.
func substrateFoldFixture(t *testing.T) (root, home string) {
	t.Helper()
	root = fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "ScheduleViewSet", "kind": "Controller", "source_file": "core/views.py"},
			{
				"id": "ep1", "name": "http:POST:/api/v1/schedule/import", "kind": "http_endpoint",
				"source_file": "core/views.py",
				"properties": map[string]any{
					"verb": "POST", "path": "/api/v1/schedule/import",
					"framework": "django", "pattern_type": "http_endpoint_synthesis",
				},
			},
		},
		Edges: []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "importSchedule", "kind": "Function", "source_file": "src/api.ts"},
			{
				"id": "ep2", "name": "http:POST:/{apiUrl}/schedule/import", "kind": "http_endpoint",
				"source_file": "src/api.ts",
				"properties": map[string]any{
					"verb": "POST", "path": "/{apiUrl}/schedule/import",
					"framework": "axios", "pattern_type": "http_endpoint_client_synthesis",
					"url_kind": "dynamic_baseurl", "dynamic_baseurl": "true",
					"caller_file": "src/api.ts", "source_caller": "Function:importSchedule",
				},
			},
		},
		Edges: []map[string]string{},
	})
	// FileRoot for a repo staged this way is <root>/<repo>, so this lands
	// at the source_file path the consumer entity declares.
	writeFile(t, filepath.Join(root, "frontend"), "src/api.ts",
		"const apiUrl = \"https://api.example.com/api/v1\";\n")
	home = filepath.Join(root, "ag-home")
	return root, home
}

// TestLinkPassDoesNotFoldConsumerBaseURL asserts that a resolvable
// dynamic_baseurl consumer path is NOT folded by the link pass: the
// consumer→producer pair is recovered by the dynamic_suffix_match tier at
// extraction_confidence=inferred, not by an exact canonical-id match.
func TestLinkPassDoesNotFoldConsumerBaseURL(t *testing.T) {
	root, home := substrateFoldFixture(t)
	if _, err := RunAllPasses("g6450-tier", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g6450-tier-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	hit := findLink(doc.Links, func(l Link) bool {
		return l.Method == MethodHTTP && l.Source == "frontend::fn1" && l.Target == "backend::h1"
	})
	if hit == nil {
		t.Fatalf("expected the dynamic_suffix_match cushion to still link "+
			"frontend::fn1 → backend::h1; got %+v", doc.Links)
	}
	if got := hit.Properties["resolve_strategy"]; got != "dynamic_suffix_match" {
		t.Errorf("resolve_strategy = %q, want dynamic_suffix_match "+
			"(an exact match here means the link pass folded the base URL again)", got)
	}
	if got := hit.Properties[EdgeConfidenceKey]; got != ConfidenceInferred {
		t.Errorf("%s = %q, want %q (the consumer path stays runtime-dynamic in the link pass)",
			EdgeConfidenceKey, got, ConfidenceInferred)
	}
}

// TestConstantPropagationPassEmitsResolvesToSidecar pins the part of
// runConstantPropagationPass that SURVIVES the #6450 deletion: the
// cross-file RESOLVES_TO sidecar. Before this test, mutating the pass to
// return immediately after buildResolver left the whole internal/links
// suite green — the existing substrate coverage calls buildResolver
// directly and never observes the pass's emitted document. It is also the
// only remaining reason buildResolver runs at link time (see #6823).
//
// Claim scope: it observes one cross-file binding reaching the emitted
// <group>-links-resolves-to.json with the right endpoints and resolved
// value. It does not grade confidence scoring, dedupe, or rejection.
func TestConstantPropagationPassEmitsResolvesToSidecar(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "c1", "name": "API_URL", "kind": "Variable", "source_file": "src/shared.ts"},
			{"id": "fn1", "name": "loadThings", "kind": "Function", "source_file": "src/app.ts"},
		},
		Edges: []map[string]string{},
	})
	writeFixture(t, root, fixtureGraph{
		Repo:     "backend",
		Entities: []map[string]any{{"id": "b1", "name": "Api", "kind": "Class", "source_file": "core/api.py"}},
		Edges:    []map[string]string{},
	})
	repo := filepath.Join(root, "frontend")
	writeFile(t, repo, "src/shared.ts", "export const API_URL = \"https://api.example.com\";\n")
	writeFile(t, repo, "src/app.ts",
		"import { API_URL } from \"./shared\";\nfetch(`${API_URL}/things`);\n")

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g6450-cp", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g6450-cp-links-resolves-to.json"))
	if err != nil {
		t.Fatal(err)
	}
	hit := findLink(doc.Links, func(l Link) bool {
		return l.Method == MethodConstantPropagation &&
			l.Source == "frontend::binding:src/app.ts::API_URL"
	})
	if hit == nil {
		t.Fatalf("expected a RESOLVES_TO link for the cross-file API_URL import; got %+v", doc.Links)
	}
	if hit.Target != "frontend::binding:src/shared.ts::API_URL" {
		t.Errorf("target = %q, want frontend::binding:src/shared.ts::API_URL", hit.Target)
	}
	if got := hit.Properties["resolved_value"]; got != "https://api.example.com" {
		t.Errorf("resolved_value = %q, want https://api.example.com", got)
	}
}

// httpPairEntities returns the producer-side and consumer-side entity sets for
// a POST /api/v1/schedule/import pair. Splitting them across two repos makes
// the cross-repo matcher fire; putting both in one repo is the single-repo
// case the len(graphs) < 2 bail is supposed to hold shut.
func httpPairEntities() (producer, consumer []map[string]any) {
	producer = []map[string]any{
		{"id": "h1", "name": "ScheduleViewSet", "kind": "Controller", "source_file": "core/views.py"},
		{
			"id": "ep1", "name": "http:POST:/api/v1/schedule/import", "kind": "http_endpoint",
			"source_file": "core/views.py",
			"properties": map[string]any{
				"verb": "POST", "path": "/api/v1/schedule/import",
				"framework": "django", "pattern_type": "http_endpoint_synthesis",
			},
		},
	}
	consumer = []map[string]any{
		{"id": "fn1", "name": "importSchedule", "kind": "Function", "source_file": "src/api.ts"},
		{
			"id": "ep2", "name": "http:POST:/api/v1/schedule/import", "kind": "http_endpoint",
			"source_file": "src/api.ts",
			"properties": map[string]any{
				"verb": "POST", "path": "/api/v1/schedule/import",
				"framework": "axios", "pattern_type": "http_endpoint_client_synthesis",
				"url_kind": "literal", "caller_file": "src/api.ts",
				"source_caller": "Function:importSchedule",
			},
		},
	}
	return producer, consumer
}

// TestHTTPPassBailHoldsForSingleRepoGroup pins the option #6450 did NOT take.
// The consumer base-URL fold was deleted from the link pass instead of
// relaxing runHTTPPass's `len(graphs) < 2` bail; relaxing it would switch the
// whole cross-repo matcher — including MethodHTTPSelf intra-repo self-call
// emission — on for every single-repo group. Nothing observed that bail: the
// entire package stayed green with it disabled, so the decision was held by
// prose alone and a later reimplementation of the rejected option would get a
// green suite.
//
// The positive control runs the SAME producer/consumer entities split across
// two repos and requires a MethodHTTP link, so the single-repo assertion
// cannot pass merely because the fixture is unmatchable.
//
// Claim scope: it observes the emitted <group>-links.json for matcher-owned
// methods (MethodHTTP / MethodHTTPSelf) only. It says nothing about the other
// passes' links, which a single-repo group still legitimately produces.
func TestHTTPPassBailHoldsForSingleRepoGroup(t *testing.T) {
	producer, consumer := httpPairEntities()

	// Positive control: two repos, matcher runs, link exists.
	ctlRoot := fixtureRoot(t)
	writeFixture(t, ctlRoot, fixtureGraph{Repo: "backend", Entities: producer,
		Edges: []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}}})
	writeFixture(t, ctlRoot, fixtureGraph{Repo: "frontend", Entities: consumer,
		Edges: []map[string]string{}})
	ctlHome := filepath.Join(ctlRoot, "ag-home")
	if _, err := RunAllPasses("g6450-bail-ctl", ctlRoot, ctlHome); err != nil {
		t.Fatal(err)
	}
	ctlDoc, err := readDoc(filepath.Join(ctlHome, "groups", "g6450-bail-ctl-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	if findLink(ctlDoc.Links, func(l Link) bool { return l.Method == MethodHTTP }) == nil {
		t.Fatalf("positive control: these entities must match across two repos, "+
			"otherwise the single-repo assertion below is vacuous; got %+v", ctlDoc.Links)
	}

	// The case under test: the same entities in ONE repo.
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo:     "monolith",
		Entities: append(append([]map[string]any{}, producer...), consumer...),
		Edges:    []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g6450-bail", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g6450-bail-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodHTTP || l.Method == MethodHTTPSelf {
			t.Errorf("single-repo group emitted a matcher-owned link (%s: %s → %s); "+
				"runHTTPPass must bail at len(graphs) < 2 — see #6450, where relaxing "+
				"this bail was the rejected alternative to deleting the link-pass fold",
				l.Method, l.Source, l.Target)
		}
	}
}

// TestHTTPPassBailCleansUpAfterGroupShrinks pins the OTHER half of the same
// five lines: the bail still runs the method-segregated overwrite so a group
// that shrank from >= 2 repos to 1 drops its previously-emitted HTTP and
// http_self rows. Deleting that replaceByMethod call also left the whole
// package green, so both halves of the bail were unobserved.
//
// The first run is its own positive control: it fails if the link the second
// run must remove was never written.
//
// Claim scope: it observes matcher-owned methods (MethodHTTP /
// MethodHTTPSelf) in the emitted <group>-links.json across two runs into the
// same home. It does not grade the rejection set or the other passes' rows.
func TestHTTPPassBailCleansUpAfterGroupShrinks(t *testing.T) {
	producer, consumer := httpPairEntities()
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{Repo: "backend", Entities: producer,
		Edges: []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}}})
	writeFixture(t, root, fixtureGraph{Repo: "frontend", Entities: consumer,
		Edges: []map[string]string{}})
	home := filepath.Join(root, "ag-home")
	linksPath := filepath.Join(home, "groups", "g6450-shrink-links.json")

	if _, err := RunAllPasses("g6450-shrink", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(linksPath)
	if err != nil {
		t.Fatal(err)
	}
	if findLink(doc.Links, func(l Link) bool { return l.Method == MethodHTTP }) == nil {
		t.Fatalf("precondition: the two-repo run must emit an HTTP link for the "+
			"shrink below to have anything to clean up; got %+v", doc.Links)
	}

	// The group shrinks to a single repo.
	if err := os.RemoveAll(filepath.Join(root, "frontend")); err != nil {
		t.Fatal(err)
	}
	if _, err := RunAllPasses("g6450-shrink", root, home); err != nil {
		t.Fatal(err)
	}
	doc2, err := readDoc(linksPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc2.Links {
		if l.Method == MethodHTTP || l.Method == MethodHTTPSelf {
			t.Errorf("stale matcher-owned link survived the shrink to one repo "+
				"(%s: %s → %s); the len(graphs) < 2 bail must still run the "+
				"method-segregated overwrite", l.Method, l.Source, l.Target)
		}
	}
}
