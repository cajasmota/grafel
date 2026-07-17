package feedback

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// makeEntity is a test helper that builds a minimal graph.Entity that speaks
// the SAME dialect as a real graph.fb-loaded entity: a canonical Kind
// (SCOPE.Function / SCOPE.Class / Model …) and StartLine ONLY. It deliberately
// does NOT set EndLine — the graph.fb schema has no end-line slot, so every
// FB-loaded entity has EndLine == 0 (see internal/graph/load.go
// fbEntityToGraphEntity). Pre-setting EndLine here was the fixture lie that let
// the D1 source-window bug pass unit tests while scoring 0.0% in production.
func makeEntity(id, name, kind, lang, srcFile string, startLine int) graph.Entity {
	return graph.Entity{
		ID:         id,
		Name:       name,
		Kind:       kind,
		Language:   lang,
		SourceFile: srcFile,
		StartLine:  startLine,
		Properties: map[string]string{},
	}
}

// makeDoc builds a minimal graph.Document from a slice of entities.
func makeDoc(entities []graph.Entity, rels []graph.Relationship) *graph.Document {
	return &graph.Document{
		Entities:      entities,
		Relationships: rels,
		Stats: graph.Stats{
			Entities:      len(entities),
			Relationships: len(rels),
		},
	}
}

// withSubtype returns a copy of e with Subtype set — used to build the
// field-leaf / container-terminal fixtures for the orphan/field-extraction
// classification tests.
func withSubtype(e graph.Entity, subtype string) graph.Entity {
	e.Subtype = subtype
	return e
}

// repeat produces n copies of e with unique IDs.
func repeat(e graph.Entity, n int) []graph.Entity {
	out := make([]graph.Entity, n)
	for i := range out {
		out[i] = e
		out[i].ID = e.ID + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
	}
	return out
}

func TestGenerate_SuppressedWhenTooFewEntities(t *testing.T) {
	// 10 entities — below the 50 minimum.
	entities := repeat(makeEntity("e1", "MyFunc", "SCOPE.Function", "go", "main.go", 1), 10)
	doc := makeDoc(entities, nil)

	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "test-group"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !r.IsSuppressed() {
		t.Error("expected report to be suppressed for < 50 entities")
	}
}

func TestGenerate_NotSuppressedAtThreshold(t *testing.T) {
	entities := repeat(makeEntity("e1", "MyFunc", "SCOPE.Function", "go", "main.go", 1), 50)
	doc := makeDoc(entities, nil)

	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "test-group"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.IsSuppressed() {
		t.Error("expected report to NOT be suppressed at exactly 50 entities")
	}
}

func TestGenerate_LanguageCountsSuppressed(t *testing.T) {
	// 5 Go entities + 50 Python entities. Go should be suppressed (< 10).
	var entities []graph.Entity
	entities = append(entities, repeat(makeEntity("g1", "GoFunc", "SCOPE.Function", "go", "main.go", 1), 5)...)
	entities = append(entities, repeat(makeEntity("p1", "PyFunc", "SCOPE.Function", "python", "main.py", 1), 50)...)
	doc := makeDoc(entities, nil)

	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, ok := r.EntitiesByLanguage["go"]; ok {
		t.Error("go with only 5 entities should be suppressed (< 10)")
	}
	if _, ok := r.EntitiesByLanguage["python"]; !ok {
		t.Error("python with 50 entities should be present")
	}
}

func TestGenerate_OrphanRateComputed(t *testing.T) {
	// 20 function entities, no outgoing semantic edges → all orphan.
	entities := repeat(makeEntity("f1", "DoWork", "SCOPE.Function", "go", "a.go", 1), 20)
	doc := makeDoc(entities, nil)

	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ks, ok := r.OrphanByKind["SCOPE.Function"]
	if !ok {
		t.Fatal("expected OrphanByKind[SCOPE.Function]")
	}
	if ks.OrphanCount != 20 {
		t.Errorf("expected 20 orphans, got %d", ks.OrphanCount)
	}
	if ks.OrphanPct != 100.0 {
		t.Errorf("expected 100%% orphan rate, got %.1f%%", ks.OrphanPct)
	}
}

func TestGenerate_SemanticEdgeReducesOrphanRate(t *testing.T) {
	entities := repeat(makeEntity("f1", "Caller", "SCOPE.Function", "go", "a.go", 1), 20)
	// Give the first entity a semantic CALLS edge.
	rels := []graph.Relationship{
		{ID: "r1", FromID: "f1a", ToID: "f1b", Kind: "CALLS"},
	}
	doc := makeDoc(entities, rels)

	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ks, ok := r.OrphanByKind["SCOPE.Function"]
	if !ok {
		t.Fatal("expected OrphanByKind[SCOPE.Function]")
	}
	// f1a has a CALLS edge so it is not an orphan; 19 are orphans.
	if ks.OrphanCount != 19 {
		t.Errorf("expected 19 orphans (1 has CALLS edge), got %d", ks.OrphanCount)
	}
}

func TestGenerate_ContainsDeclaresDontReduceOrphan(t *testing.T) {
	entities := repeat(makeEntity("e1", "Thing", "SCOPE.Class", "java", "A.java", 1), 15)
	// CONTAINS and DECLARES edges should NOT count as semantic.
	rels := []graph.Relationship{
		{ID: "r1", FromID: "e1a", ToID: "e1b", Kind: "CONTAINS"},
		{ID: "r2", FromID: "e1c", ToID: "e1d", Kind: "DECLARES"},
	}
	doc := makeDoc(entities, rels)

	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ks := r.OrphanByKind["SCOPE.Class"]
	// All 15 are still orphans (CONTAINS/DECLARES don't reduce orphan count).
	if ks.OrphanCount != 15 {
		t.Errorf("expected 15 orphans (CONTAINS/DECLARES excluded), got %d", ks.OrphanCount)
	}
}

func TestGenerate_ResolutionDisposition(t *testing.T) {
	// Disposition is derived STRUCTURALLY from the ToID shape — the same
	// classification orient/grafel_stats uses — NOT from a Properties["resolution"]
	// tag the pipeline never writes. A 16-hex ToID is resolved, an ext:-prefixed
	// ToID is external-known, any other non-empty ToID is an unresolved stub.
	entities := repeat(makeEntity("e1", "X", "SCOPE.Function", "go", "x.go", 1), 50)
	rels := []graph.Relationship{
		{ID: "r1", FromID: "e1a", ToID: "aabb112233445566", Kind: "CALLS"}, // hex → resolved
		{ID: "r2", FromID: "e1c", ToID: "ext:react", Kind: "IMPORTS"},      // ext → external-known
		{ID: "r3", FromID: "e1e", ToID: "SomeBareStub", Kind: "CALLS"},     // stub → bug-extractor
		{ID: "r4", FromID: "e1g", ToID: "pkg.Unresolved", Kind: "CALLS"},   // stub → bug-extractor
	}
	doc := makeDoc(entities, rels)

	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.ResolutionTotal != 4 {
		t.Errorf("expected ResolutionTotal=4, got %d", r.ResolutionTotal)
	}
	if r.Resolution.ResolvedPct != 25.0 {
		t.Errorf("expected resolved 25%%, got %.1f%%", r.Resolution.ResolvedPct)
	}
	if r.Resolution.ExternalKnownPct != 25.0 {
		t.Errorf("expected external-known 25%%, got %.1f%%", r.Resolution.ExternalKnownPct)
	}
	if r.Resolution.BugExtractorPct != 50.0 {
		t.Errorf("expected bug-extractor 50%%, got %.1f%%", r.Resolution.BugExtractorPct)
	}
}

func TestGenerate_SourceWindowCompleteness(t *testing.T) {
	// The navigable-window anchor is StartLine > 0 alone. FB-loaded entities
	// never carry an EndLine (no schema slot), so the fixtures mirror that:
	// only StartLine distinguishes a windowed entity from an unwindowed one.
	entities := []graph.Entity{
		makeEntity("e1", "Good", "SCOPE.Function", "go", "a.go", 5), // has start line → window
		makeEntity("e2", "Bad", "SCOPE.Function", "go", "a.go", 0),  // no start line → no window
	}
	// Need at least 50 total — pad with start-line-bearing entities.
	for i := 2; i < 50; i++ {
		e := makeEntity("pad", "Fn", "SCOPE.Function", "go", "a.go", 1)
		e.ID = fmt.Sprintf("pad%d", i)
		entities = append(entities, e)
	}
	doc := makeDoc(entities, nil)

	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.SourceWindow.TotalEntities != 50 {
		t.Errorf("expected 50 total entities, got %d", r.SourceWindow.TotalEntities)
	}
	// e1 + 48 padding entities have a start line; only e2 (start 0) does not.
	if r.SourceWindow.TotalWithWindow != 49 {
		t.Errorf("expected 49 entities with a source window, got %d", r.SourceWindow.TotalWithWindow)
	}
}

func TestGenerate_MultipleDocsAggregated(t *testing.T) {
	entities1 := repeat(makeEntity("e1", "GoFunc", "SCOPE.Function", "go", "a.go", 1), 30)
	entities2 := repeat(makeEntity("e2", "PyFunc", "SCOPE.Function", "python", "b.py", 1), 30)
	doc1 := makeDoc(entities1, nil)
	doc2 := makeDoc(entities2, nil)

	r, err := Generate(context.Background(), []*graph.Document{doc1, doc2}, Opts{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.TotalEntities != 60 {
		t.Errorf("expected 60 total entities, got %d", r.TotalEntities)
	}
	if r.IsSuppressed() {
		t.Error("expected report to NOT be suppressed with 60 entities")
	}
}

func TestRender_SuppressedReport(t *testing.T) {
	r := &Report{
		TotalEntities: 10,
		GeneratedAt:   mustParseTime("2026-05-27T00:00:00Z"),
		GroupName:     "tiny-group",
		suppressed:    true,
	}
	var sb strings.Builder
	if err := Render(&sb, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "suppressed") {
		t.Error("suppressed report should contain the word 'suppressed'")
	}
	if strings.Contains(out, "## 1.") {
		t.Error("suppressed report should not contain section headers")
	}
}

func TestRender_FullReport(t *testing.T) {
	r := &Report{
		TotalEntities:      100,
		TotalRelationships: 200,
		GeneratedAt:        mustParseTime("2026-05-27T00:00:00Z"),
		GroupName:          "test-group",
		Version:            "v1.0.0",
		Languages:          []string{"go", "typescript"},
		EntitiesByLanguage: map[string]int{"go": 80, "typescript": 20},
		EntityKindDist:     []EntityKindLang{{Kind: "function", Language: "go", Count: 50}},
		SourceWindow:       SourceWindowStats{TotalWithWindow: 90, TotalEntities: 100, PctComplete: 90.0},
		OrphanByKind:       map[string]KindStats{"function": {Total: 80, OrphanCount: 16, OrphanPct: 20.0}},
		Resolution: ResolutionVector{
			ResolvedPct:        70.0,
			ExternalKnownPct:   10.0,
			ExternalUnknownPct: 10.0,
			BugExtractorPct:    5.0,
			BugResolverPct:     4.0,
			DynamicPct:         1.0,
		},
		ResolutionTotal: 200,
		FrameworkHits:   map[string]int{"gin": 15},
		SanityResults:   []SanityResult{{Name: "minimum-entity-count", Passed: true}},
		Confidence:      100,
	}
	r.AnnotationCoverage.Total = 100
	r.AnnotationCoverage.TotalAnnotated = 15
	r.AnnotationCoverage.PctAnnotated = 15.0

	var sb strings.Builder
	if err := Render(&sb, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := sb.String()

	// Check all sections present.
	for _, section := range []string{
		"## 1. Extractor Coverage",
		"## 2. Orphan Rate",
		"## 3. Resolution Disposition",
		"## 4. Framework Recognition",
		"## 5. Cross-Stack Flows",
		"## 6. Docgen Quality",
		"## 7. Sanity Check Details",
	} {
		if !strings.Contains(out, section) {
			t.Errorf("expected section %q in output", section)
		}
	}

	// Phase 1 placeholders.
	if !strings.Contains(out, "(not in Phase 1)") {
		t.Error("expected Phase 1 placeholder text for cross-stack and docgen sections")
	}

	// Footer privacy note.
	if !strings.Contains(out, "ephemeral") {
		t.Error("expected privacy footer mentioning ephemeral salt")
	}

	// Framework hits.
	if !strings.Contains(out, "gin") {
		t.Error("expected framework 'gin' in output")
	}
}

// TestGenerate_FieldChildrenAndContainerTerminalsClassifiedCorrectly is the
// regression guard for the feedback-collector "orphan rate" / "zero fields"
// measurement bug (issue #5823 grounding, Fixes 1-3): the dominant Go/Java/
// Python producers emit fields as CHILD entities (Kind tail "schema",
// Subtype "field") linked to their parent by a structural CONTAINS edge —
// they never write Properties["field_count"] — and pure-container SCOPE.Component
// terminals (one per source file, module/import stubs, pattern-detector nodes)
// never source outbound semantic edges. Those were miscounted as defects
// (100% zero-fields, 100% orphan) before the fix.
//
// It ALSO guards the opposite direction: a class-subtype SCOPE.Component with
// zero outbound edges is NOT exempt — classes DO source EXTENDS/DEPENDS_ON in
// real graphs (python/crossfile.go, docgen/tier0.go), so a zero-edge class is
// a genuine resolver/extractor defect and must stay in the DEFECT OrphanByKind
// bucket where the sanity gate can catch it.
func TestGenerate_FieldChildrenAndContainerTerminalsClassifiedCorrectly(t *testing.T) {
	widget := makeEntity("widget", "Widget", "SCOPE.Class", "go", "widget.go", 10)
	field1 := withSubtype(makeEntity("widget.name", "Widget.name", "SCOPE.Schema", "go", "widget.go", 11), "field")
	field2 := withSubtype(makeEntity("widget.price", "Widget.price", "SCOPE.Schema", "go", "widget.go", 12), "field")
	fileComp := withSubtype(makeEntity("file1", "widget.go", "SCOPE.Component", "go", "widget.go", 1), "file")
	// A class-subtype Component with zero outbound edges — the masking-regression
	// canary. It must land in the DEFECT bucket, never the terminal bucket.
	classComp := withSubtype(makeEntity("class1", "OrderPlaced", "SCOPE.Component", "python", "order.py", 1), "class")

	entities := []graph.Entity{widget, field1, field2, fileComp, classComp}
	// Pad past the 50-entity / 10-per-kind reporting floors with orphan
	// SCOPE.Function entities that carry no structural or semantic edges —
	// they must NOT be affected by the field/container classification.
	entities = append(entities, repeat(makeEntity("fn", "DoWork", "SCOPE.Function", "go", "a.go", 1), 50)...)

	rels := []graph.Relationship{
		{ID: "r1", FromID: "widget", ToID: "widget.name", Kind: "CONTAINS"},
		{ID: "r2", FromID: "widget", ToID: "widget.price", Kind: "CONTAINS"},
	}
	// Pad SCOPE.Class, SCOPE.Component (file subtype), and SCOPE.Schema past
	// the N>=10 suppression floor with additional non-orphan instances so the
	// kinds under test appear in OrphanByKind/OrphanTerminalByKind.
	for i := 0; i < 10; i++ {
		w := makeEntity(fmt.Sprintf("w%d", i), "OtherWidget", "SCOPE.Class", "go", "w.go", 1)
		f := withSubtype(makeEntity(fmt.Sprintf("w%df", i), "OtherWidget.x", "SCOPE.Schema", "go", "w.go", 2), "field")
		fc := withSubtype(makeEntity(fmt.Sprintf("fc%d", i), "w.go", "SCOPE.Component", "go", "w.go", 1), "file")
		entities = append(entities, w, f, fc)
		rels = append(rels, graph.Relationship{
			ID: fmt.Sprintf("cr%d", i), FromID: w.ID, ToID: f.ID, Kind: "CONTAINS",
		})
	}

	doc := makeDoc(entities, rels)
	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// --- Fix 1: field-extraction rate reflects real field CHILDREN, not the
	// (never-set) Properties["field_count"] property. 11 classes, all with
	// >=1 field child → 0% zero-fields, not 100%.
	if r.FieldExtractionRate.ClassTotal != 11 {
		t.Errorf("ClassTotal = %d, want 11 (field-leaf terminals excluded from the class count)", r.FieldExtractionRate.ClassTotal)
	}
	if r.FieldExtractionRate.ZeroFieldsPct != 0.0 {
		t.Errorf("ZeroFieldsPct = %.1f%%, want 0.0%% (every class has >=1 real field child)", r.FieldExtractionRate.ZeroFieldsPct)
	}

	// --- Fix 2: field-leaf terminals (Subtype=="field") anchored by an
	// inbound CONTAINS edge are not orphans, even though they have zero
	// outbound edges.
	if ks, ok := r.OrphanByKind["SCOPE.Schema"]; ok && ks.OrphanCount != 0 {
		t.Errorf("SCOPE.Schema defect orphans = %d, want 0 (field leaves anchored by inbound CONTAINS)", ks.OrphanCount)
	}

	// --- Fix 3: only PURE-container SCOPE.Component subtypes (file/module/
	// import + pattern terminals) route to the expected/terminal bucket.
	// file1 + 10 padding file-subtype Components = 11 terminal orphans.
	tks, ok := r.OrphanTerminalByKind["SCOPE.Component"]
	if !ok {
		t.Fatal("expected OrphanTerminalByKind[SCOPE.Component] to be populated")
	}
	if tks.OrphanCount != 11 {
		t.Errorf("SCOPE.Component terminal orphans = %d, want 11 (file-subtype only)", tks.OrphanCount)
	}

	// --- Masking guard: the class-subtype Component with zero outbound edges
	// MUST be a DEFECT orphan (classes source EXTENDS/DEPENDS_ON in real
	// graphs; a zero-edge class is a resolver/extractor regression). It must
	// NOT be swallowed by the terminal bucket.
	cks, ok := r.OrphanByKind["SCOPE.Component"]
	if !ok {
		t.Fatal("expected OrphanByKind[SCOPE.Component] to be populated")
	}
	if cks.OrphanCount != 1 {
		t.Errorf("SCOPE.Component defect orphans = %d, want 1 (the zero-edge class-subtype Component)", cks.OrphanCount)
	}
}

func TestRender_ExpectedTerminalOrphansSection(t *testing.T) {
	r := &Report{
		TotalEntities: 100,
		GeneratedAt:   mustParseTime("2026-05-27T00:00:00Z"),
		GroupName:     "test-group",
		OrphanByKind:  map[string]KindStats{"SCOPE.Component": {Total: 20, OrphanCount: 0, OrphanPct: 0.0}},
		OrphanTerminalByKind: map[string]KindStats{
			"SCOPE.Component": {Total: 20, OrphanCount: 20, OrphanPct: 100.0},
		},
		FrameworkHits: map[string]int{},
		SanityResults: []SanityResult{{Name: "minimum-entity-count", Passed: true}},
		Confidence:    100,
	}
	var sb strings.Builder
	if err := Render(&sb, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "Expected/terminal orphans") {
		t.Error("expected 'Expected/terminal orphans' section when OrphanTerminalByKind is populated")
	}
	if !strings.Contains(out, "SCOPE.Component") {
		t.Error("expected SCOPE.Component row in the terminal-orphans table")
	}
}

// mustParseTime parses an RFC3339 time for use in tests.
func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
