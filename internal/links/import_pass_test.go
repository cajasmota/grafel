package links

import (
	"path/filepath"
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

// TestImportPass_EmitsQualifiedExt verifies the converse: qualified
// "ext:<module>:<name>" placeholders (e.g. `ext:react:useState`) DO
// carry real module identity and remain eligible for cross-repo
// linking when two repos point at the same qualified ID.
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
	var count int
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 import-method link for ext:react:useState, got %d (%+v)", count, doc.Links)
	}
}

// TestImportPass_EmitsSharedPackageExt is the issue #566 fix:
// real npm packages emitted by external-synth as `ext:<package>`
// (single colon, subtype="package") MUST produce cross-repo links when
// two repos share the placeholder. #509 was over-aggressive and
// dropped these alongside true bare-name built-ins.
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
	var imports []Link
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			imports = append(imports, l)
		}
	}
	if len(imports) == 0 {
		t.Fatalf("expected ≥1 import-method links for shared ext:axios (subtype=package), got 0; links=%+v", doc.Links)
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

// TestImportPass_ScopedNpmPackage covers case (e) of #566:
// scoped npm packages like `@tanstack/react-query` are emitted as
// `ext:@tanstack/react-query` — a single-colon ID containing a slash.
// Pre-#566 the second-colon test dropped these; the subtype="package"
// admission restores them.
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
	var imports int
	for _, l := range doc.Links {
		if l.Method == MethodImport {
			imports++
		}
	}
	if imports == 0 {
		t.Fatalf("expected ≥1 import-method links for shared scoped npm package, got 0; links=%+v", doc.Links)
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
