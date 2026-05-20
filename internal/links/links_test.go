package links

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureGraph is a minimal helper to write a per-repo graph.json that
// loadAllGraphs can read.
type fixtureGraph struct {
	Repo     string
	Entities []map[string]any
	Edges    []map[string]string
}

func writeFixture(t *testing.T, root string, fg fixtureGraph) string {
	t.Helper()
	dir := filepath.Join(root, fg.Repo, ".archigraph")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{
		"version":       1,
		"repo":          fg.Repo,
		"entities":      fg.Entities,
		"relationships": fg.Edges,
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	gpath := filepath.Join(dir, "graph.json")
	if err := os.WriteFile(gpath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return gpath
}

// fixtureRoot creates a temp graphs root and returns its path.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// twoRepoGraphs writes two simple per-repo graphs into root.
func twoRepoGraphs(t *testing.T, root string) {
	t.Helper()
	writeFixture(t, root, fixtureGraph{
		Repo: "alpha",
		Entities: []map[string]any{
			{"id": "a1", "name": "OrderBook", "kind": "class", "source_file": "src/order.go"},
			{"id": "a2", "name": "helper", "kind": "function", "source_file": "src/util.go"},
		},
		Edges: []map[string]string{
			{"from_id": "a1", "to_id": "b1", "kind": "imports"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "beta",
		Entities: []map[string]any{
			{"id": "b1", "name": "OrderBookService", "kind": "interface", "source_file": "lib/order.py"},
			{"id": "b2", "name": "helper", "kind": "function", "source_file": "lib/util.py"},
		},
		Edges: []map[string]string{},
	})
}

func TestImportPass_EmitsCrossRepoEdges(t *testing.T) {
	root := fixtureRoot(t)
	twoRepoGraphs(t, root)
	home := filepath.Join(root, "ag-home")
	res, err := RunAllPasses("g1", root, home)
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalLinks == 0 {
		t.Fatalf("expected at least one link, got 0")
	}
	// Read links file.
	doc, err := readDoc(res.OutLinks)
	if err != nil {
		t.Fatal(err)
	}
	var foundImport bool
	for _, l := range doc.Links {
		if l.Method == MethodImport && l.Source == "alpha::a1" && l.Target == "beta::b1" {
			foundImport = true
			if l.Confidence != 1.0 {
				t.Errorf("import confidence: want 1.0, got %v", l.Confidence)
			}
			if l.Channel != nil {
				t.Errorf("import channel must be nil")
			}
		}
	}
	if !foundImport {
		t.Fatalf("expected import edge alpha::a1 → beta::b1, got %+v", doc.Links)
	}
}

func TestLabelPass_StoplistFilters(t *testing.T) {
	root := fixtureRoot(t)
	twoRepoGraphs(t, root)
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g1", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g1-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodLabelMatch && l.Identifier != nil && *l.Identifier == "helper" {
			t.Errorf("stop-listed label `helper` produced a link: %+v", l)
		}
	}
}

func TestLabelPass_HighConfidenceForRareLabel(t *testing.T) {
	root := fixtureRoot(t)
	// Create a corpus where one rare label appears in two repos and the
	// rest of the entity population is unique noise. This drives idf high.
	mkNoise := func(prefix string, n int) []map[string]any {
		out := []map[string]any{}
		for i := 0; i < n; i++ {
			out = append(out, map[string]any{
				"id":          prefix + "n" + itoa(i),
				"name":        prefix + "_unique_" + itoa(i),
				"kind":        "function",
				"source_file": "f.py",
			})
		}
		return out
	}
	a := append(mkNoise("a_", 30), map[string]any{
		"id": "a_special", "name": "PaymentReconciler", "kind": "class", "source_file": "p.py",
	})
	b := append(mkNoise("b_", 30), map[string]any{
		"id": "b_special", "name": "PaymentReconciler", "kind": "class", "source_file": "p.go",
	})
	writeFixture(t, root, fixtureGraph{Repo: "alpha", Entities: a})
	writeFixture(t, root, fixtureGraph{Repo: "beta", Entities: b})

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g2", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g2-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hit *Link
	for i, l := range doc.Links {
		if l.Method == MethodLabelMatch && l.Identifier != nil && *l.Identifier == "paymentreconciler" {
			hit = &doc.Links[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("expected high-confidence label match for `paymentreconciler`, got %+v", doc.Links)
	}
	if hit.Confidence < 0.5 {
		t.Errorf("expected confidence ≥ 0.5, got %v", hit.Confidence)
	}
}

func TestLabelPass_LineNumberKeyedLabelsDropped(t *testing.T) {
	// Regression for #511. Labels whose final segment is a bare integer
	// (e.g. `error_handling:try_catch:110`) are line-number-keyed and
	// must not produce cross-repo links — line-number coincidence is not
	// signal.
	root := fixtureRoot(t)
	mkNoise := func(prefix string, n int) []map[string]any {
		out := []map[string]any{}
		for i := 0; i < n; i++ {
			out = append(out, map[string]any{
				"id":          prefix + "n" + itoa(i),
				"name":        prefix + "_unique_" + itoa(i),
				"kind":        "function",
				"source_file": "f.py",
			})
		}
		return out
	}
	a := append(mkNoise("a_", 30),
		map[string]any{"id": "a1", "name": "error_handling:try_catch:110", "kind": "block", "source_file": "src/a.go"},
		map[string]any{"id": "a2", "name": "AGENTS.md", "kind": "file", "source_file": "AGENTS.md"},
		map[string]any{"id": "a3", "name": "route:/users/{id}", "kind": "route", "source_file": "src/r.go"},
	)
	b := append(mkNoise("b_", 30),
		map[string]any{"id": "b1", "name": "error_handling:try_catch:110", "kind": "block", "source_file": "lib/b.py"},
		map[string]any{"id": "b2", "name": "AGENTS.md", "kind": "file", "source_file": "AGENTS.md"},
		map[string]any{"id": "b3", "name": "route:/users/{id}", "kind": "route", "source_file": "lib/r.py"},
	)
	writeFixture(t, root, fixtureGraph{Repo: "alpha", Entities: a})
	writeFixture(t, root, fixtureGraph{Repo: "beta", Entities: b})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g511", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g511-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var keptFile, keptRoute bool
	for _, l := range doc.Links {
		if l.Method != MethodLabelMatch || l.Identifier == nil {
			continue
		}
		id := *l.Identifier
		if strings.HasPrefix(id, "error_handling:try_catch:") {
			t.Errorf("line-number-keyed label produced a link: %+v", l)
		}
		if id == "agents.md" {
			keptFile = true
		}
		if id == "route:/users/{id}" {
			keptRoute = true
		}
	}
	if !keptFile {
		t.Errorf("expected filename label `agents.md` to still produce a link, got %+v", doc.Links)
	}
	if !keptRoute {
		t.Errorf("expected structural label `route:/users/{id}` to still produce a link, got %+v", doc.Links)
	}
}

func TestNormalizeLabel_HardenedNoiseFilters_565(t *testing.T) {
	// Each entry: input label, expected normalized output ("" = filtered).
	cases := []struct {
		name string
		in   string
		want string
	}{
		// 1. Stdlib / builtins / single letters.
		{"js_array_filter", "filter", ""},
		{"js_array_map", "map", ""},
		{"js_promise_then", "then", ""},
		{"dom_addeventlistener", "addEventListener", ""},
		{"timer_cleartimeout", "clearTimeout", ""},
		{"react_useState", "useState", ""},
		{"py_isinstance", "isinstance", ""},
		{"single_letter_i", "i", ""},
		{"single_letter_k", "k", ""},
		// 2. Destructured React tuples.
		{"tuple_error", "[error, seterror]", ""},
		{"tuple_isLoading", "[isLoading, setIsLoading]", ""},
		{"tuple_open", "[open, setopen]", ""},
		{"obj_destructure_data", "{ data }", ""},
		{"obj_destructure_id", "{ id }", ""},
		{"obj_destructure_multi", "{ url, fields }", ""},
		{"arr_destructure_date", "[year, month, day]", ""},
		// Date/Number/JSON method names.
		{"date_getfullyear", "getFullYear", ""},
		{"date_toisostring", "toISOString", ""},
		{"num_parseint", "parseInt", ""},
		{"num_isnan", "isNaN", ""},
		// React-Query/RTK hook names.
		{"hook_usemutation", "useMutation", ""},
		{"hook_usequery", "useQuery", ""},
		// Event handler conventions.
		{"evt_onerror", "onError", ""},
		{"evt_handlesubmit", "handleSubmit", ""},
		// Boolean state conventions.
		{"bool_isactive", "isActive", ""},
		{"bool_isloading", "isLoading", ""},
		// 3. Generic field/var names.
		{"field_body", "body", ""},
		{"field_id", "id", ""},
		{"field_status", "status", ""},
		{"field_url", "url", ""},
		{"field_role", "role", ""},
		// 4. Length-after-strip < 4.
		{"too_short_abc", "abc", ""},
		{"too_short_alpha", "_a_", ""},
		// 5. NPM package roots.
		{"npm_axios", "axios", ""},
		// Positive cases — architectural concepts MUST survive.
		{"arch_auth", "auth", "auth"},
		{"arch_contact", "contact", "contact"},
		{"arch_viewset_word", "checklist", "checklist"},
		{"file_agentsmd", "AGENTS.md", "agents.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeLabel(tc.in); got != tc.want {
				t.Errorf("normalizeLabel(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeLabel_DropsLineNumberSuffix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"error_handling:try_catch:110", ""},
		{"error_handling:try_catch:1", ""},
		{"route:42", ""},
		{"agents.md", "agents.md"},
		{"route:/users/{id}", "route:/users/{id}"},
		{"OrderBook", "orderbook"},
	}
	for _, tc := range cases {
		if got := normalizeLabel(tc.in); got != tc.want {
			t.Errorf("normalizeLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestKindCompat_ClassInterface(t *testing.T) {
	if got := kindCompat("class", "interface"); got != 0.85 {
		t.Errorf("class↔interface: want 0.85, got %v", got)
	}
	if got := kindCompat("class", "class"); got != 1.0 {
		t.Errorf("class↔class: want 1.0, got %v", got)
	}
	if got := kindCompat("function", "class"); got != 0.5 {
		t.Errorf("function↔class: want 0.5, got %v", got)
	}
}

func TestStringPass_HTTPPath(t *testing.T) {
	root := fixtureRoot(t)
	// alpha repo: file containing /api/v1/orders/123
	mkRepo := func(name, src string) {
		dir := filepath.Join(root, name, "src")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(dir, "h.go")
		if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, root, fixtureGraph{
			Repo: name,
			Entities: []map[string]any{
				{"id": name + "_e", "name": "Handler", "kind": "function", "source_file": "src/h.go"},
			},
		})
	}
	mkRepo("alpha", "package main\nvar p = \"/api/v1/orders/{id}\"\n")
	mkRepo("beta", "package main\nvar p = \"/api/v1/orders/{id}\"\n")

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g3", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g3-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodString && l.Identifier != nil && *l.Identifier == "/api/v1/orders/{id}" {
			found = true
			if l.Channel == nil || *l.Channel != "http" {
				t.Errorf("string match channel: want http, got %v", l.Channel)
			}
		}
	}
	if !found {
		t.Fatalf("expected string match for /api/v1/orders/{id}, got %+v", doc.Links)
	}
}

func TestStringPass_WordXMLElementsNotExtractedAsHTTPPaths(t *testing.T) {
	// Issue #958: Word XML elements like ./w:tblBorders should not be
	// extracted as HTTP paths. Verify that python-docx-like code patterns
	// don't produce spurious path entities.
	root := fixtureRoot(t)
	mkRepo := func(name, src string) {
		dir := filepath.Join(root, name, "src")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(dir, "docx_handler.py")
		if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, root, fixtureGraph{
			Repo: name,
			Entities: []map[string]any{
				{"id": name + "_e", "name": "process_doc", "kind": "function", "source_file": "src/docx_handler.py"},
			},
		})
	}
	// Python code using python-docx library with Word XML elements
	pythonDocxCode := `from docx.oxml.ns import qn

def process_word_doc(elem):
    # Access Word XML elements with namespaced references
    borders = elem.find(qn('w:tblBorders'))
    cell_borders = elem.find(qn('w:tcBorders'))
    return borders, cell_borders
`
	mkRepo("app", pythonDocxCode)
	mkRepo("service", pythonDocxCode)

	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g4", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g4-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Verify that no links were created for Word XML element names.
	// These should NOT be classified as HTTP paths.
	for _, l := range doc.Links {
		if l.Method == MethodString && l.Identifier != nil {
			id := *l.Identifier
			if strings.Contains(id, "w:tblBorders") || strings.Contains(id, "w:tcBorders") ||
				strings.Contains(id, "w:") {
				t.Errorf("Word XML element %q should not be extracted as HTTP path link", id)
			}
		}
	}
}

func TestStringPass_CacheHits(t *testing.T) {
	root := fixtureRoot(t)
	src := filepath.Join(root, "alpha-repo")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(src, "h.go")
	if err := os.WriteFile(f, []byte(`package main
var p = "/api/v1/orders"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(root, "cache")
	first, err := scanFile(f, "h.go", cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("first scan: expected 1 extraction, got %d", len(first))
	}
	// Modify the file's contents on disk WITHOUT updating mtime/size: we
	// simulate the cache being hit by changing only contents and asking
	// for the same mtime. The simplest correct check is: when nothing
	// changes, second call returns the same result.
	second, err := scanFile(f, "h.go", cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].Value != first[0].Value {
		t.Fatalf("cached scan should return same result")
	}
	// Now overwrite the file with content that produces zero matches and
	// ensure cache invalidation kicks in (mtime/size differ).
	if err := os.WriteFile(f, []byte(`package main
// nothing
`), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := scanFile(f, "h.go", cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(third) != 0 {
		t.Fatalf("after edit, expected 0 extractions, got %d", len(third))
	}
}

func TestMethodSegregatedOverwrite(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.json")
	// Pre-seed with an import link.
	pre := &Document{Version: 1, Links: []Link{{
		ID: "abc12345", Source: "alpha::a1", Target: "beta::b1",
		Relation: RelationImports, Method: MethodImport, Confidence: 1.0,
		DiscoveredAt: "2026-05-08T00:00:00Z",
	}}}
	if err := writeDoc(path, pre); err != nil {
		t.Fatal(err)
	}
	// Run the label pass logic on top: a label-match link.
	added, _, err := replaceByMethod(path, newMethodSet(MethodLabelMatch), []Link{{
		ID: "00000001", Source: "alpha::a1", Target: "beta::b1",
		Relation: RelationSharedLabel, Method: MethodLabelMatch, Confidence: 0.9,
		DiscoveredAt: "2026-05-08T00:00:01Z",
	}}, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Errorf("expected 1 added, got %d", added)
	}
	doc, err := readDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Links) != 2 {
		t.Fatalf("expected import + label rows, got %d: %+v", len(doc.Links), doc.Links)
	}
}

func TestRejectionFile(t *testing.T) {
	tmp := t.TempDir()
	rejPath := filepath.Join(tmp, "rejects.json")
	// Pre-seed rejection.
	if err := writeDoc(rejPath, &Document{Version: 1, Links: []Link{{
		ID: "x", Source: "alpha::a1", Target: "beta::b1", Method: MethodImport,
		Resolution: &Resolution{At: "now", Reason: "false positive"},
	}}}); err != nil {
		t.Fatal(err)
	}
	rejects, err := loadRejections(rejPath)
	if err != nil {
		t.Fatal(err)
	}
	linksPath := filepath.Join(tmp, "links.json")
	added, skipped, err := replaceByMethod(linksPath, newMethodSet(MethodImport), []Link{{
		ID: "y", Source: "alpha::a1", Target: "beta::b1", Method: MethodImport,
		Relation: RelationImports, Confidence: 1.0,
	}}, rejects)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || skipped != 1 {
		t.Errorf("rejection should suppress: got added=%d skipped=%d", added, skipped)
	}
}

func TestPatternCatalog(t *testing.T) {
	cases := []struct {
		s       string
		want    extractionCategory
		wantNot bool // true: expect no match
	}{
		{"/webhooks/stripe", catWebhookPath, false},
		{"/api/v1/orders/123", catHTTPPath, false},
		{"s3://my-bucket/path/file.csv", catS3URI, false},
		{"user:profile:42", catRedisKey, false},
		{"orders.payments.completed", catKafkaTopic, false},
		{"orders.payments.>", catNATSSubject, false},
		{"feature_new_checkout", catFeatureFlag, false},
		{"arn:aws:sqs:us-east-1:123456789012:my-queue", catSQSARN, false},
		{"https://sqs.us-east-1.amazonaws.com/123456789012/my-queue", catSQSURL, false},
		{"arn:aws:sns:us-east-1:123456789012:topic.fifo", catSNSARN, false},
		{"arn:aws:lambda:us-east-1:123456789012:function:my-fn", catLambdaARN, false},
		{"arn:aws:events:us-east-1:123456789012:rule/my-rule", catEventbridgeARN, false},
		{"hello world", "", true},
		{"file.json", "", true}, // kafka TLD blocklist
		// Issue #958: Word XML element references should NOT match HTTP path pattern
		{"./w:tblBorders", "", true},          // Word XML table borders element
		{"./w:tcBorders", "", true},           // Word XML table cell borders element
		{"./xml:element", "", true},           // Generic XML namespace element
		{"/api/v1/w:something", "", true},     // XML-like path (namespace colon) should not match
		{"/api/v1/users", catHTTPPath, false}, // Valid HTTP path with no namespace colon
	}
	for _, c := range cases {
		got := classify(c.s)
		if c.wantNot {
			if got != "" {
				t.Errorf("classify(%q) want no match, got %s", c.s, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("classify(%q) want %s, got %s", c.s, c.want, got)
		}
	}
}

// itoa avoids fmt import in the test file's hot helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// keep strings import used.
var _ = strings.HasPrefix
