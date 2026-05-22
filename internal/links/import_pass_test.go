package links

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestImportPass_SkipsBareNameExt verifies the issue #509 fix:
// two repos that each independently reference `ext:filter` (a bare-name
// external placeholder for a built-in like `[].filter()`) must NOT
// produce a cross-repo link. Bare-name ext:* placeholders are each
// repo's own unresolved use of a built-in, not a shared symbol.
func TestImportPass_SkipsBareNameExt(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "alpha",
		Entities: []map[string]any{
			{"id": "a_local", "name": "doStuff", "kind": "function", "source_file": "src/a.js"},
			{"id": "ext:filter", "name": "filter", "kind": "External", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "a_local", "to_id": "ext:filter", "kind": "calls"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "beta",
		Entities: []map[string]any{
			{"id": "b_local", "name": "doMore", "kind": "function", "source_file": "src/b.js"},
			{"id": "ext:filter", "name": "filter", "kind": "External", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "b_local", "to_id": "ext:filter", "kind": "calls"},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g509", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g509-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			t.Errorf("expected zero import-method links for bare-name ext:filter, got %+v", l)
		}
	}
}

// TestImportPass_EmitsQualifiedExt was the converse of #509: qualified
// "ext:<module>:<name>" placeholders (e.g. `ext:react:useState`) were
// eligible for cross-repo linking when two repos referenced the same ID.
//
// Issue #1507 supersedes this: any ext:* ID that appears in more than one
// repo is a shared external placeholder with no stable repo owner; emitting
// a directional cross-repo link between the two consuming repos (e.g.
// beta::b_comp → alpha::ext:react:useState) implies beta imports from alpha
// when in reality both repos independently depend on the react library.
// Guard B in runImportPass removes all multi-repo ext:* IDs from entRepo,
// so zero import-method links are emitted in this scenario.
func TestImportPass_EmitsQualifiedExt(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "alpha",
		Entities: []map[string]any{
			{"id": "a_comp", "name": "Component", "kind": "function", "source_file": "src/a.tsx"},
			{"id": "ext:react:useState", "name": "useState", "kind": "External", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "a_comp", "to_id": "ext:react:useState", "kind": "calls"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "beta",
		Entities: []map[string]any{
			{"id": "b_comp", "name": "Other", "kind": "function", "source_file": "src/b.tsx"},
			{"id": "ext:react:useState", "name": "useState", "kind": "External", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "b_comp", "to_id": "ext:react:useState", "kind": "calls"},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g509q", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g509q-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Guard B (#1507): ext:react:useState appears in both repos → removed
	// from entRepo → no import-method link is emitted between the two repos.
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			t.Errorf("expected zero import-method links for shared ext:react:useState (#1507 Guard B); got %+v", l)
		}
	}
}

// TestImportPass_EmitsSharedPackageExt was the issue #566 fix:
// real npm packages emitted by external-synth as `ext:<package>`
// (single colon, subtype="package") produced cross-repo links when two
// repos shared the placeholder.
//
// Issue #1507 supersedes this: the directional cross-repo link
// (beta::b_local → alpha::ext:axios) was misleading — it implied beta
// imports from alpha when both repos independently depend on axios.
// Guard B in runImportPass removes all multi-repo ext:* IDs from entRepo,
// so zero import-method links are emitted for shared external packages.
//
// The isBuiltinExt gate (#509 fix) remains: bare-name ext:* with no
// subtype=package is still filtered. This test now documents that
// subtype=package shared ext:* links are also suppressed by Guard B.
func TestImportPass_EmitsSharedPackageExt(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "alpha",
		Entities: []map[string]any{
			{"id": "a_local", "name": "useAxios", "kind": "function", "source_file": "src/a.ts"},
			{"id": "ext:axios", "name": "axios", "kind": "SCOPE.External", "subtype": "package", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "a_local", "to_id": "ext:axios", "kind": "imports"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "beta",
		Entities: []map[string]any{
			{"id": "b_local", "name": "wrapAxios", "kind": "function", "source_file": "src/b.ts"},
			{"id": "ext:axios", "name": "axios", "kind": "SCOPE.External", "subtype": "package", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "b_local", "to_id": "ext:axios", "kind": "imports"},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g566pkg", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g566pkg-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Guard B (#1507): ext:axios appears in both repos → removed from
	// entRepo → no import-method link is emitted between alpha and beta.
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			t.Errorf("expected zero import-method links for shared ext:axios (#1507 Guard B); got %+v", l)
		}
	}
}

// TestImportPass_BareNameStillFiltered_NoSubtype covers case (b) in the
// #566 plan: a bare ext:<name> with no module qualifier and no
// subtype="package" tag (e.g. the dynamic-dispatch `ext:filter`
// placeholder from #509) still produces zero cross-repo links.
func TestImportPass_BareNameStillFiltered_NoSubtype(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "alpha",
		Entities: []map[string]any{
			{"id": "a_local", "name": "doStuff", "kind": "function", "source_file": "src/a.js"},
			{"id": "ext:filter", "name": "filter", "kind": "SCOPE.External", "subtype": "function", "source_file": ""},
		},
		Edges: []map[string]string{{"from_id": "a_local", "to_id": "ext:filter", "kind": "calls"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "beta",
		Entities: []map[string]any{
			{"id": "b_local", "name": "doMore", "kind": "function", "source_file": "src/b.js"},
			{"id": "ext:filter", "name": "filter", "kind": "SCOPE.External", "subtype": "function", "source_file": ""},
		},
		Edges: []map[string]string{{"from_id": "b_local", "to_id": "ext:filter", "kind": "calls"}},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g566bare", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g566bare-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			t.Errorf("expected zero import-method links for bare-name ext:filter (subtype=function), got %+v", l)
		}
	}
}

// TestImportPass_DifferentModuleSameName covers case (c) in the #566
// plan: two repos with different qualified ext:<module>:<name>
// placeholders that happen to share the bare name MUST NOT link.
// Distinct IDs naturally fail the join key — this asserts that.
func TestImportPass_DifferentModuleSameName(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "alpha",
		Entities: []map[string]any{
			{"id": "a_local", "name": "useReactState", "kind": "function", "source_file": "src/a.tsx"},
			{"id": "ext:react:useState", "name": "useState", "kind": "SCOPE.External", "subtype": "function", "source_file": ""},
		},
		Edges: []map[string]string{{"from_id": "a_local", "to_id": "ext:react:useState", "kind": "calls"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "beta",
		Entities: []map[string]any{
			{"id": "b_local", "name": "usePreactState", "kind": "function", "source_file": "src/b.tsx"},
			{"id": "ext:preact:useState", "name": "useState", "kind": "SCOPE.External", "subtype": "function", "source_file": ""},
		},
		Edges: []map[string]string{{"from_id": "b_local", "to_id": "ext:preact:useState", "kind": "calls"}},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g566diffmod", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g566diffmod-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			t.Errorf("expected zero import-method links for different-module same-name, got %+v", l)
		}
	}
}

// TestImportPass_SameModuleDifferentName covers case (d) in the #566
// plan: two repos each reference a different symbol from the SAME
// module (`ext:react:useState` vs `ext:react:useEffect`). Distinct IDs
// — must not link.
func TestImportPass_SameModuleDifferentName(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "alpha",
		Entities: []map[string]any{
			{"id": "a_local", "name": "useS", "kind": "function", "source_file": "src/a.tsx"},
			{"id": "ext:react:useState", "name": "useState", "kind": "SCOPE.External", "subtype": "function", "source_file": ""},
		},
		Edges: []map[string]string{{"from_id": "a_local", "to_id": "ext:react:useState", "kind": "calls"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "beta",
		Entities: []map[string]any{
			{"id": "b_local", "name": "useE", "kind": "function", "source_file": "src/b.tsx"},
			{"id": "ext:react:useEffect", "name": "useEffect", "kind": "SCOPE.External", "subtype": "function", "source_file": ""},
		},
		Edges: []map[string]string{{"from_id": "b_local", "to_id": "ext:react:useEffect", "kind": "calls"}},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g566samemoddiffname", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g566samemoddiffname-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			t.Errorf("expected zero import-method links for same-module different-name, got %+v", l)
		}
	}
}

// TestImportPass_ScopedNpmPackage covers scoped npm packages like
// `@tanstack/react-query` emitted as `ext:@tanstack/react-query`.
// Pre-#566 the second-colon test dropped these. Post-#1507 Guard B
// suppresses the cross-repo link when both repos share the placeholder,
// because the directional link was misleading (implies alpha imports from
// beta when both independently depend on the package). Guard B removes any
// ext:* ID that appears in more than one repo from entRepo.
func TestImportPass_ScopedNpmPackage(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "alpha",
		Entities: []map[string]any{
			{"id": "a_local", "name": "useQ", "kind": "function", "source_file": "src/a.ts"},
			{"id": "ext:@tanstack/react-query", "name": "@tanstack/react-query", "kind": "SCOPE.External", "subtype": "package", "source_file": ""},
		},
		Edges: []map[string]string{{"from_id": "a_local", "to_id": "ext:@tanstack/react-query", "kind": "imports"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "beta",
		Entities: []map[string]any{
			{"id": "b_local", "name": "useQ2", "kind": "function", "source_file": "src/b.ts"},
			{"id": "ext:@tanstack/react-query", "name": "@tanstack/react-query", "kind": "SCOPE.External", "subtype": "package", "source_file": ""},
		},
		Edges: []map[string]string{{"from_id": "b_local", "to_id": "ext:@tanstack/react-query", "kind": "imports"}},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g566scoped", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g566scoped-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Guard B (#1507): ext:@tanstack/react-query appears in both repos →
	// removed from entRepo → no import-method link is emitted.
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			t.Errorf("expected zero import-method links for shared scoped npm package (#1507 Guard B); got %+v", l)
		}
	}
}

// TestIsBuiltinExt is unit coverage for the predicate that drives the
// #566 admission rule. Exercises the full decision matrix.
func TestIsBuiltinExt(t *testing.T) {
	subtypes := map[string]string{
		"ext:axios":                 "package",
		"ext:@tanstack/react-query": "package",
		"ext:filter":                "function",
		"ext:react:useState":        "function",
		"ext:django:models.Model":   "function",
		"ext:nosub":                 "",
	}
	cases := []struct {
		in   string
		want bool
	}{
		{"ext:axios", false},                 // subtype=package → admit
		{"ext:@tanstack/react-query", false}, // subtype=package, scoped npm → admit
		{"ext:filter", true},                 // subtype=function, bare → skip
		{"ext:react:useState", false},        // qualified → admit
		{"ext:django:models.Model", false},   // qualified → admit
		{"ext:", true},                       // pathological → skip
		{"ext:nosub", true},                  // missing subtype + bare → skip (conservative)
		{"a_local", false},                   // non-ext → not subject to gate
		{"", false},
		{"react:useState", false}, // no ext: prefix
	}
	for _, c := range cases {
		if got := isBuiltinExt(c.in, subtypes); got != c.want {
			t.Errorf("isBuiltinExt(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIsBareNameExt(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"ext:filter", true},
		{"ext:split", true},
		{"ext:", true}, // pathological — treat as bare
		{"ext:react:useState", false},
		{"ext:django:models.Model", false},
		{"ext:click.command", true}, // dot-qualified but no second colon — still bare per spec
		{"a_local", false},
		{"", false},
		{"react:useState", false}, // no ext: prefix
	}
	for _, c := range cases {
		if got := isBareNameExt(c.in); got != c.want {
			t.Errorf("isBareNameExt(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestImportPass_MatchesHTTPEndpointByName verifies #534 Phase 2 Track B:
// two repos whose http_endpoint entities share the canonical
// `http:<METHOD>:<path>` Name are linked via the import-method pass even
// when there is no edge between them. The synthetic emission gives both
// sides a deterministic Name; the linker keys on Name (not stamped ID,
// which incorporates the repo tag and source file and therefore differs
// per repo).
func TestImportPass_MatchesHTTPEndpointByName(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "backend_ep_id", "name": "http:GET:/users/{id}", "kind": "http_endpoint", "source_file": "app/routes.py"},
		},
		Edges: []map[string]string{},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "frontend_ep_id", "name": "http:GET:/users/{id}", "kind": "http_endpoint", "source_file": "src/api.ts"},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g534p2", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g534p2-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range doc.Links {
		if l.Method != MethodImport {
			continue
		}
		// Both endpoint keys must reference the http_endpoint stamped IDs
		// from the two fixtures.
		if (l.Source == "backend::backend_ep_id" && l.Target == "frontend::frontend_ep_id") ||
			(l.Source == "frontend::frontend_ep_id" && l.Target == "backend::backend_ep_id") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected cross-repo http_endpoint import link by shared Name; got links: %+v", doc.Links)
	}
}

// TestImportPass_HTTPEndpointDoesNotMatchAcrossDifferentNames verifies
// the safety contract: distinct http_endpoint Names in two repos do NOT
// produce a cross-repo link.
func TestImportPass_HTTPEndpointDoesNotMatchAcrossDifferentNames(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "backend_ep_id", "name": "http:GET:/users/{id}", "kind": "http_endpoint", "source_file": "app/routes.py"},
		},
		Edges: []map[string]string{},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "frontend_ep_id", "name": "http:POST:/orders", "kind": "http_endpoint", "source_file": "src/api.ts"},
		},
		Edges: []map[string]string{},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g534p2-neg", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g534p2-neg-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			t.Errorf("expected no import links for mismatched http_endpoint Names; got %+v", l)
		}
	}
}

// TestImportPass_NoSpuriousLinksViaSharedExtPlaceholder is the regression
// test for issue #1507. Four repos all import the same Python shared-lib
// (`py_shared`), producing `ext:py_shared` entities in each. The linker
// must NOT emit cross-repo links between the consuming services (orders,
// analytics, order-saga, workers) — those services don't import each other.
//
// Guard B (multi-origin): ext:py_shared appears in 4 repos → removed from
// entRepo → no spurious import links are emitted.
func TestImportPass_NoSpuriousLinksViaSharedExtPlaceholder(t *testing.T) {
	root := fixtureRoot(t)
	services := []string{"orders", "analytics", "order-saga", "workers"}
	for _, svc := range services {
		writeFixture(t, root, fixtureGraph{
			Repo: svc,
			Entities: []map[string]any{
				{"id": svc + "_local", "name": "handler", "kind": "function", "source_file": "app/main.py"},
				// Each service emits an ext:py_shared entity (subtype=package)
				// when it resolves `import py_shared` through the external synth.
				{"id": "ext:py_shared", "name": "py_shared", "kind": "SCOPE.External", "subtype": "package", "source_file": ""},
			},
			Edges: []map[string]string{
				{"from_id": svc + "_local", "to_id": "ext:py_shared", "kind": "imports"},
			},
		})
	}
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g1507-multiservice", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g1507-multiservice-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method != MethodImport {
			continue
		}
		srcRepo := l.Source
		if i := strings.Index(srcRepo, "::"); i >= 0 {
			srcRepo = srcRepo[:i]
		}
		tgtRepo := l.Target
		if i := strings.Index(tgtRepo, "::"); i >= 0 {
			tgtRepo = tgtRepo[:i]
		}
		// None of the services import each other — any import link between
		// two service repos is spurious.
		for _, s1 := range services {
			for _, s2 := range services {
				if s1 != s2 && srcRepo == s1 && tgtRepo == s2 {
					t.Errorf("spurious import link %s → %s (should be zero); link=%+v", s1, s2, l)
				}
			}
		}
	}
}

// TestImportPass_NoSpuriousLinkWhenExtNameMatchesGroupedRepo verifies Guard A
// (issue #1507): when the base name of an ext:* ID matches a repo slug in the
// indexed group, the entity is removed from entRepo and no spurious cross-repo
// import links are produced between the consuming services.
//
// Scenario: "py-shared" is an indexed repo. "orders" and "analytics" both
// emit `ext:py_shared` (guard A normalises hyphens to underscores). The
// linker must not create orders→analytics or analytics→orders links.
func TestImportPass_NoSpuriousLinkWhenExtNameMatchesGroupedRepo(t *testing.T) {
	root := fixtureRoot(t)
	// The actual shared-lib repo (no edges pointing at ext:py_shared).
	writeFixture(t, root, fixtureGraph{
		Repo: "py-shared",
		Entities: []map[string]any{
			{"id": "pyshared_order_model", "name": "Order", "kind": "SCOPE.Component", "source_file": "py_shared/models.py"},
		},
		Edges: []map[string]string{},
	})
	// Consuming services — both emit ext:py_shared as an external placeholder.
	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			{"id": "orders_handler", "name": "create_order", "kind": "function", "source_file": "app/routes.py"},
			{"id": "ext:py_shared", "name": "py_shared", "kind": "SCOPE.External", "subtype": "package", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "orders_handler", "to_id": "ext:py_shared", "kind": "imports"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "analytics",
		Entities: []map[string]any{
			{"id": "analytics_main", "name": "process", "kind": "function", "source_file": "analytics/main.py"},
			{"id": "ext:py_shared", "name": "py_shared", "kind": "SCOPE.External", "subtype": "package", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "analytics_main", "to_id": "ext:py_shared", "kind": "imports"},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g1507-reponame", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g1507-reponame-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method != MethodImport {
			continue
		}
		// No import-method links should exist between the consuming services.
		srcRepo := l.Source
		if i := strings.Index(srcRepo, "::"); i >= 0 {
			srcRepo = srcRepo[:i]
		}
		tgtRepo := l.Target
		if i := strings.Index(tgtRepo, "::"); i >= 0 {
			tgtRepo = tgtRepo[:i]
		}
		if (srcRepo == "orders" && tgtRepo == "analytics") ||
			(srcRepo == "analytics" && tgtRepo == "orders") {
			t.Errorf("spurious import link between consuming services %s→%s; link=%+v", srcRepo, tgtRepo, l)
		}
	}
}

// TestImportPass_NoSpuriousLinkTwoReposSharedExt verifies that Guard B
// suppresses cross-repo links even for the two-repo shared-external case.
// Previously #566 emitted a link when exactly two repos shared ext:axios;
// issue #1507 removes this: the directional link (bff::fn → frontend::ext:axios)
// implies bff imports from frontend when both simply depend on the axios library.
func TestImportPass_NoSpuriousLinkTwoReposSharedExt(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fe_comp", "name": "FetchUser", "kind": "function", "source_file": "src/api.ts"},
			{"id": "ext:axios", "name": "axios", "kind": "SCOPE.External", "subtype": "package", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "fe_comp", "to_id": "ext:axios", "kind": "imports"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "bff",
		Entities: []map[string]any{
			{"id": "bff_comp", "name": "ProxyRequest", "kind": "function", "source_file": "src/proxy.ts"},
			{"id": "ext:axios", "name": "axios", "kind": "SCOPE.External", "subtype": "package", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "bff_comp", "to_id": "ext:axios", "kind": "imports"},
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g1507-twoaxios", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g1507-twoaxios-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Guard B (#1507): ext:axios in both repos → no cross-repo import link.
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			t.Errorf("expected zero import-method links for two-repo shared ext:axios; got %+v", l)
		}
	}
}
