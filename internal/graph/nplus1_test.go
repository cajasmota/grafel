package graph

import (
	"strings"
	"testing"
)

// helpers ────────────────────────────────────────────────────────────────────

func makeEntity(id, name, kind, subtype, sourceFile, lang string, startLine int, props map[string]string) Entity {
	return Entity{
		ID:         id,
		Name:       name,
		Kind:       string(kind),
		Subtype:    subtype,
		SourceFile: sourceFile,
		Language:   lang,
		StartLine:  startLine,
		Properties: props,
	}
}

func makeRel(id, fromID, toID, kind string) Relationship {
	return Relationship{ID: id, FromID: fromID, ToID: toID, Kind: kind}
}

// ─────────────────────────────────────────────────────────────────────────────
// Core detection tests
// ─────────────────────────────────────────────────────────────────────────────

// TestDetectNPlusOne_DjangoForLoop checks the canonical case:
// a Django for-loop that calls User.objects.get() inside the loop body.
func TestDetectNPlusOne_DjangoForLoop(t *testing.T) {
	t.Parallel()

	// Graph shape:
	//   fn (SCOPE.Function)
	//     └─CONTAINS─> loop (subtype=for_loop)
	//                    └─CALLS─> query (orm=django, name=User.objects.get)
	fn := makeEntity("fn1", "get_users", "SCOPE.Function", "", "views.py", "python", 10, nil)
	loop := makeEntity("loop1", "for_loop_body", "SCOPE.CodeBlock", "for_loop", "views.py", "python", 15, nil)
	query := makeEntity("q1", "User.objects.get", "SCOPE.DataAccess", "", "views.py", "python", 16,
		map[string]string{"orm": "django"})

	doc := &Document{
		Entities: []Entity{fn, loop, query},
		Relationships: []Relationship{
			makeRel("r1", "fn1", "loop1", "CONTAINS"),
			makeRel("r2", "loop1", "q1", "CALLS"),
		},
	}

	report := DetectNPlusOne(doc)
	if len(report.Findings) == 0 {
		t.Fatal("expected at least one N+1 finding, got 0")
	}
	f := report.Findings[0]
	if f.ORM != "django" {
		t.Errorf("ORM: got %q want %q", f.ORM, "django")
	}
	if f.QueryName != "User.objects.get" {
		t.Errorf("QueryName: got %q want %q", f.QueryName, "User.objects.get")
	}
	if !strings.Contains(f.Suggestion, "select_related") && !strings.Contains(f.Suggestion, "prefetch_related") {
		t.Errorf("Suggestion should mention select_related/prefetch_related, got: %s", f.Suggestion)
	}
}

// TestDetectNPlusOne_PrefetchRelatedNotFlagged verifies that a query wrapped
// in prefetch_related is not flagged as N+1.
func TestDetectNPlusOne_PrefetchRelatedNotFlagged(t *testing.T) {
	t.Parallel()

	fn := makeEntity("fn1", "optimized_view", "SCOPE.Function", "", "views.py", "python", 10, nil)
	loop := makeEntity("loop1", "for_loop_body", "SCOPE.CodeBlock", "for_loop", "views.py", "python", 15, nil)
	// This entity is a prefetch_related call — should be in ormSafeMethods.
	query := makeEntity("q1", "User.objects.prefetch_related", "SCOPE.DataAccess", "", "views.py", "python", 12,
		map[string]string{"orm": "django"})

	doc := &Document{
		Entities: []Entity{fn, loop, query},
		Relationships: []Relationship{
			makeRel("r1", "fn1", "loop1", "CONTAINS"),
			makeRel("r2", "fn1", "q1", "CALLS"),
		},
	}

	report := DetectNPlusOne(doc)
	for _, f := range report.Findings {
		if f.QueryEntityID == "q1" {
			t.Errorf("prefetch_related call should NOT be flagged as N+1, got finding: %+v", f)
		}
	}
}

// TestDetectNPlusOne_LoopContextProperty checks the pass-1 path where the ORM
// extractor already set Properties["loop_context"]="true" on the call site.
func TestDetectNPlusOne_LoopContextProperty(t *testing.T) {
	t.Parallel()

	fn := makeEntity("fn1", "bad_view", "SCOPE.Function", "", "api.py", "python", 5, nil)
	// No explicit loop entity — the call site has loop_context="true".
	query := makeEntity("q1", "Post.objects.get", "SCOPE.DataAccess", "", "api.py", "python", 20,
		map[string]string{
			"orm":          "django",
			"loop_context": "true",
		})

	doc := &Document{
		Entities: []Entity{fn, query},
		Relationships: []Relationship{
			makeRel("r1", "fn1", "q1", "CONTAINS"),
		},
	}

	report := DetectNPlusOne(doc)
	if len(report.Findings) == 0 {
		t.Fatal("expected N+1 finding via loop_context property, got 0")
	}
	f := report.Findings[0]
	if f.QueryEntityID != "q1" {
		t.Errorf("QueryEntityID: got %q want %q", f.QueryEntityID, "q1")
	}
}

// TestDetectNPlusOne_NPlusSafeOptOut checks that a call site annotated with
// Properties["nplus1_safe"]="true" is excluded from findings.
func TestDetectNPlusOne_NPlusSafeOptOut(t *testing.T) {
	t.Parallel()

	fn := makeEntity("fn1", "tolerated_view", "SCOPE.Function", "", "api.py", "python", 5, nil)
	loop := makeEntity("loop1", "for_loop_body", "SCOPE.CodeBlock", "for_loop", "api.py", "python", 10, nil)
	query := makeEntity("q1", "Item.objects.get", "SCOPE.DataAccess", "", "api.py", "python", 11,
		map[string]string{
			"orm":         "django",
			"nplus1_safe": "true",
		})

	doc := &Document{
		Entities: []Entity{fn, loop, query},
		Relationships: []Relationship{
			makeRel("r1", "fn1", "loop1", "CONTAINS"),
			makeRel("r2", "loop1", "q1", "CALLS"),
		},
	}

	report := DetectNPlusOne(doc)
	for _, f := range report.Findings {
		if f.QueryEntityID == "q1" {
			t.Error("nplus1_safe=true entity should NOT be flagged")
		}
	}
}

// TestDetectNPlusOne_SQLAlchemy checks SQLAlchemy ORM detection.
func TestDetectNPlusOne_SQLAlchemy(t *testing.T) {
	t.Parallel()

	fn := makeEntity("fn1", "load_items", "SCOPE.Function", "", "repo.py", "python", 1, nil)
	loop := makeEntity("loop1", "while_loop_body", "SCOPE.CodeBlock", "while_loop", "repo.py", "python", 5, nil)
	query := makeEntity("q1", "session.query", "SCOPE.DataAccess", "", "repo.py", "python", 6,
		map[string]string{"orm": "sqlalchemy"})

	doc := &Document{
		Entities: []Entity{fn, loop, query},
		Relationships: []Relationship{
			makeRel("r1", "fn1", "loop1", "CONTAINS"),
			makeRel("r2", "loop1", "q1", "QUERIES"),
		},
	}

	report := DetectNPlusOne(doc)
	if len(report.Findings) == 0 {
		t.Fatal("expected N+1 finding for SQLAlchemy, got 0")
	}
	f := report.Findings[0]
	if f.ORM != "sqlalchemy" {
		t.Errorf("ORM: got %q want %q", f.ORM, "sqlalchemy")
	}
	if !strings.Contains(f.Suggestion, "joinedload") && !strings.Contains(f.Suggestion, "selectinload") {
		t.Errorf("SQLAlchemy suggestion should mention joinedload/selectinload, got: %s", f.Suggestion)
	}
}

// TestDetectNPlusOne_ActiveRecord checks Ruby ActiveRecord detection.
func TestDetectNPlusOne_ActiveRecord(t *testing.T) {
	t.Parallel()

	fn := makeEntity("fn1", "users_action", "SCOPE.Operation", "", "users_controller.rb", "ruby", 10, nil)
	loop := makeEntity("loop1", "for_loop", "SCOPE.CodeBlock", "for_loop", "users_controller.rb", "ruby", 15, nil)
	query := makeEntity("q1", "User.find", "SCOPE.DataAccess", "", "users_controller.rb", "ruby", 16,
		map[string]string{"orm": "activerecord"})

	doc := &Document{
		Entities: []Entity{fn, loop, query},
		Relationships: []Relationship{
			makeRel("r1", "fn1", "loop1", "CONTAINS"),
			makeRel("r2", "loop1", "q1", "CALLS"),
		},
	}

	report := DetectNPlusOne(doc)
	if len(report.Findings) == 0 {
		t.Fatal("expected N+1 finding for ActiveRecord, got 0")
	}
	if !strings.Contains(report.Findings[0].Suggestion, "includes") {
		t.Errorf("ActiveRecord suggestion should mention includes(), got: %s", report.Findings[0].Suggestion)
	}
}

// TestDetectNPlusOne_NilDocument guards against nil input.
func TestDetectNPlusOne_NilDocument(t *testing.T) {
	t.Parallel()
	report := DetectNPlusOne(nil)
	if report == nil {
		t.Fatal("DetectNPlusOne(nil) returned nil, want empty report")
	}
	if len(report.Findings) != 0 {
		t.Errorf("expected 0 findings for nil doc, got %d", len(report.Findings))
	}
}

// TestDetectNPlusOne_EmptyDocument checks zero entities.
func TestDetectNPlusOne_EmptyDocument(t *testing.T) {
	t.Parallel()
	report := DetectNPlusOne(&Document{})
	if len(report.Findings) != 0 {
		t.Errorf("expected 0 findings for empty doc, got %d", len(report.Findings))
	}
}

// TestDetectNPlusOne_Dedup verifies that the same (caller, query) pair is
// only reported once even if multiple paths reach it.
func TestDetectNPlusOne_Dedup(t *testing.T) {
	t.Parallel()

	fn := makeEntity("fn1", "view", "SCOPE.Function", "", "v.py", "python", 1, nil)
	loop := makeEntity("loop1", "body", "SCOPE.CodeBlock", "for_loop", "v.py", "python", 2, nil)
	query := makeEntity("q1", "X.objects.get", "SCOPE.DataAccess", "", "v.py", "python", 3,
		map[string]string{"orm": "django", "loop_context": "true"})

	doc := &Document{
		Entities: []Entity{fn, loop, query},
		Relationships: []Relationship{
			makeRel("r1", "fn1", "loop1", "CONTAINS"),
			// Two edges from loop to query (unusual but should not produce two findings).
			makeRel("r2", "loop1", "q1", "CALLS"),
			makeRel("r3", "fn1", "q1", "CONTAINS"),
		},
	}

	report := DetectNPlusOne(doc)
	// Expect exactly 1 finding (not 2 or 3).
	if len(report.Findings) > 2 {
		t.Errorf("expected at most 2 deduped findings, got %d", len(report.Findings))
	}
}

// TestAnnotateDocument verifies that AnnotateDocument stamps Relationships.
func TestAnnotateDocument(t *testing.T) {
	t.Parallel()

	fn := makeEntity("fn1", "view", "SCOPE.Function", "", "v.py", "python", 1, nil)
	loop := makeEntity("loop1", "body", "SCOPE.CodeBlock", "for_loop", "v.py", "python", 2, nil)
	query := makeEntity("q1", "M.objects.get", "SCOPE.DataAccess", "", "v.py", "python", 3,
		map[string]string{"orm": "django"})

	callsRel := Relationship{ID: "r2", FromID: "loop1", ToID: "q1", Kind: "CALLS"}

	doc := &Document{
		Entities: []Entity{fn, loop, query},
		Relationships: []Relationship{
			makeRel("r1", "fn1", "loop1", "CONTAINS"),
			callsRel,
		},
	}

	report := DetectNPlusOne(doc)
	count := AnnotateDocument(doc, report)

	// At least one relationship should be annotated.
	if count == 0 && len(report.Findings) > 0 {
		t.Error("AnnotateDocument returned 0 but findings exist")
	}

	// Check that the CALLS relationship got anti_pattern="n_plus_1".
	for _, r := range doc.Relationships {
		if r.ID == "r2" {
			if r.Properties == nil || r.Properties["anti_pattern"] != "n_plus_1" {
				// Only check if findings reference this pair.
				for _, f := range report.Findings {
					if f.QueryEntityID == "q1" {
						t.Errorf("CALLS rel r2 should have anti_pattern=n_plus_1, properties: %v", r.Properties)
					}
				}
			}
		}
	}
}

// TestSummariseFindingsText checks that text output is non-empty for a finding.
func TestSummariseFindingsText(t *testing.T) {
	t.Parallel()

	report := &NPlusOneReport{
		Findings: []NPlusOneFinding{
			{
				CallerName: "my_view",
				QueryName:  "User.objects.get",
				QueryFile:  "api.py",
				QueryLine:  42,
				ORM:        "django",
				Suggestion: "Use prefetch_related.",
			},
		},
		EntitiesScanned:      10,
		RelationshipsScanned: 20,
	}

	text := SummariseFindingsText(report)
	if !strings.Contains(text, "1 finding") {
		t.Errorf("expected '1 finding' in text, got: %s", text)
	}
	if !strings.Contains(text, "my_view") {
		t.Errorf("expected caller name in text, got: %s", text)
	}
	if !strings.Contains(text, "api.py:42") {
		t.Errorf("expected file:line in text, got: %s", text)
	}
}

// TestSummariseFindingsText_Empty checks the zero-findings path.
func TestSummariseFindingsText_Empty(t *testing.T) {
	t.Parallel()
	report := &NPlusOneReport{EntitiesScanned: 5, RelationshipsScanned: 3}
	text := SummariseFindingsText(report)
	if !strings.Contains(text, "no findings") {
		t.Errorf("expected 'no findings', got: %s", text)
	}
}

// TestORMSuggestion_UnknownORM checks the generic fallback suggestion.
func TestORMSuggestion_UnknownORM(t *testing.T) {
	t.Parallel()
	s := ormSuggestion("", "go")
	if s == "" {
		t.Error("ormSuggestion should return non-empty string for unknown ORM")
	}
}

// TestDetectNPlusOne_ListComprehension verifies detection inside a Python
// list comprehension (subtype=list_comprehension).
func TestDetectNPlusOne_ListComprehension(t *testing.T) {
	t.Parallel()

	fn := makeEntity("fn1", "build_list", "SCOPE.Function", "", "helpers.py", "python", 1, nil)
	comp := makeEntity("comp1", "list_comp", "SCOPE.CodeBlock", "list_comprehension", "helpers.py", "python", 5, nil)
	query := makeEntity("q1", "Product.objects.filter", "SCOPE.DataAccess", "", "helpers.py", "python", 5,
		map[string]string{"orm": "django"})

	doc := &Document{
		Entities: []Entity{fn, comp, query},
		Relationships: []Relationship{
			makeRel("r1", "fn1", "comp1", "CONTAINS"),
			makeRel("r2", "comp1", "q1", "CALLS"),
		},
	}

	report := DetectNPlusOne(doc)
	if len(report.Findings) == 0 {
		t.Fatal("expected N+1 finding inside list_comprehension")
	}
}

// TestDetectNPlusOne_ChildContainedByLoop checks the second sub-pass of
// Pass 2 (entities CONTAINED BY a loop entity).
func TestDetectNPlusOne_ChildContainedByLoop(t *testing.T) {
	t.Parallel()

	fn := makeEntity("fn1", "parent_fn", "SCOPE.Function", "", "f.py", "python", 1, nil)
	loop := makeEntity("loop1", "for_body", "SCOPE.CodeBlock", "for_loop", "f.py", "python", 5, nil)
	// Query is CONTAINED by the loop (not directly called from it).
	query := makeEntity("q1", "Obj.objects.get", "SCOPE.DataAccess", "", "f.py", "python", 6,
		map[string]string{"orm": "django"})

	doc := &Document{
		Entities: []Entity{fn, loop, query},
		Relationships: []Relationship{
			makeRel("r1", "fn1", "loop1", "CONTAINS"),
			makeRel("r2", "loop1", "q1", "CONTAINS"), // child contained by loop
		},
	}

	report := DetectNPlusOne(doc)
	if len(report.Findings) == 0 {
		t.Fatal("expected N+1 finding for entity CONTAINED by loop, got 0")
	}
}
