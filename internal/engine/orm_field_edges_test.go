// Tests for the applyORMFieldEdges pass (#2279).
//
// Strategy mirrors orm_queries_test.go: build a small in-memory file,
// run the detector, then assert on the READS_FIELD / WRITES_FIELD edges
// emitted in DetectResult.Relationships. Field entities are emitted by
// the Python extractor at python/extractor.go:1411-1421; this test
// piggybacks on that real extractor pipeline so we don't double-stub
// the field-node convention.
//
// Coverage:
//
//	(a) simple `.filter(cognito_id="x")` → READS_FIELD edge
//	(b) lookup-suffix stripping `cognito_id__isnull=True` → READS_FIELD on cognito_id
//	(c) `.update(cognito_id="y")` → WRITES_FIELD
//	(d) unknown field → no edge, no panic, no dangling
package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// fieldEdge is the minimal projection of a READS_FIELD / WRITES_FIELD
// edge used by the assertions below.
type fieldEdge struct {
	From  string
	To    string
	Kind  string
	Field string
	Model string
	Op    string
}

// detectFieldEdges runs the full detector pipeline and returns only the
// field-access edges. All other relationships (CALLS, IMPORTS, QUERIES,
// …) are dropped so the assertion shape stays small.
func detectFieldEdges(t *testing.T, content string) []fieldEdge {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "users.py",
		Content:  []byte(content),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	var out []fieldEdge
	for _, r := range res.Relationships {
		if r.Kind != ormReadsFieldKind && r.Kind != ormWritesFieldKind {
			continue
		}
		out = append(out, fieldEdge{
			From:  r.FromID,
			To:    r.ToID,
			Kind:  r.Kind,
			Field: r.Properties.Get("field"),
			Model: r.Properties.Get("model"),
			Op:    r.Properties.Get("verb"),
		})
	}
	return out
}

func assertFieldEdge(t *testing.T, edges []fieldEdge, kind, model, field string) fieldEdge {
	t.Helper()
	for _, e := range edges {
		if e.Kind == kind && e.Model == model && e.Field == field {
			return e
		}
	}
	t.Errorf("missing field edge kind=%q model=%q field=%q\n  got: %+v",
		kind, model, field, edges)
	return fieldEdge{}
}

// (a) Simple Django filter on a known field emits READS_FIELD.
func TestORMFieldEdges_SimpleFilter(t *testing.T) {
	src := `from django.db import models

class User(models.Model):
    cognito_id = models.CharField(max_length=64)
    name = models.CharField(max_length=100)

def get_user(uid):
    return User.objects.get(cognito_id=uid)
`
	edges := detectFieldEdges(t, src)
	e := assertFieldEdge(t, edges, ormReadsFieldKind, "User", "cognito_id")
	if e.To != "Class:User.cognito_id" {
		t.Errorf("ToID = %q, want %q", e.To, "Class:User.cognito_id")
	}
	if e.From == "" {
		t.Errorf("FromID is empty: %+v", e)
	}
}

// (b) Django lookup suffix (__isnull) is stripped before field lookup.
func TestORMFieldEdges_LookupSuffixStripped(t *testing.T) {
	src := `from django.db import models

class User(models.Model):
    cognito_id = models.CharField(max_length=64)

def list_pending():
    return User.objects.filter(cognito_id__isnull=True)
`
	edges := detectFieldEdges(t, src)
	e := assertFieldEdge(t, edges, ormReadsFieldKind, "User", "cognito_id")
	if e.To != "Class:User.cognito_id" {
		t.Errorf("ToID = %q, want Class:User.cognito_id", e.To)
	}
	// Defensive: the *raw* lookup key should NOT appear as a field name on
	// any emitted edge — only the stripped local field.
	for _, ed := range edges {
		if ed.Field == "cognito_id__isnull" {
			t.Errorf("found unstripped lookup key in edge: %+v", ed)
		}
	}
}

// (c) Django update() emits WRITES_FIELD.
func TestORMFieldEdges_UpdateWrites(t *testing.T) {
	src := `from django.db import models

class User(models.Model):
    cognito_id = models.CharField(max_length=64)

def rotate_cognito(uid, new_id):
    User.objects.filter(id=uid).update(cognito_id=new_id)
`
	edges := detectFieldEdges(t, src)
	// .filter(id=...) does not reference a declared field on User
	// (no `id` field is defined in the toy model above) — so the only
	// emitted edge should be the WRITES_FIELD on cognito_id from the
	// terminal .update() call.
	assertFieldEdge(t, edges, ormWritesFieldKind, "User", "cognito_id")
	for _, e := range edges {
		if e.Kind == ormReadsFieldKind && e.Field == "id" {
			// `id` is implicit on Django models but not declared in
			// this test source — ensure we did not emit a dangling
			// READS_FIELD for it.
			t.Errorf("unexpected READS_FIELD on undeclared field id: %+v", e)
		}
	}
}

// (d) Unknown field on the resolved model produces no edge.
func TestORMFieldEdges_UnknownFieldSkipped(t *testing.T) {
	src := `from django.db import models

class User(models.Model):
    cognito_id = models.CharField(max_length=64)

def buggy_query():
    # email is NOT declared on User -- must not emit an edge.
    return User.objects.filter(email="x@y.z")
`
	edges := detectFieldEdges(t, src)
	for _, e := range edges {
		if e.Field == "email" {
			t.Errorf("emitted edge for undeclared field email: %+v", e)
		}
	}
}

// Regression: ensure a non-ORM Python file emits zero field-access edges
// (byte-identical-on-non-ORM check, mirroring orm_queries_test.go).
func TestORMFieldEdges_NonORMFileNoChange(t *testing.T) {
	src := `def add(a, b):
    return a + b

class Calculator:
    def multiply(self, x, y):
        return x * y
`
	edges := detectFieldEdges(t, src)
	if len(edges) != 0 {
		t.Errorf("expected 0 field-access edges on non-ORM file, got %d: %+v",
			len(edges), edges)
	}
}

// Regression: relation-traversal kwargs (`author__name`) keep ONLY the
// first segment as the local field. The remainder crosses relations
// and is out of scope for Phase A.
func TestORMFieldEdges_RelationTraversalKeepsFirstSegment(t *testing.T) {
	src := `from django.db import models

class Article(models.Model):
    author = models.ForeignKey("User", on_delete=models.CASCADE)
    title = models.CharField(max_length=200)

def by_author_name(name):
    return Article.objects.filter(author__name=name)
`
	edges := detectFieldEdges(t, src)
	// We should see a READS_FIELD on Article.author (the local field) and
	// NOT on a non-existent Article.author__name.
	assertFieldEdge(t, edges, ormReadsFieldKind, "Article", "author")
	for _, e := range edges {
		if strings.Contains(e.Field, "__") {
			t.Errorf("emitted edge with un-split traversal field: %+v", e)
		}
	}
}

// Issue #2352: the plumbed Pass 1 side-channel takes precedence over
// the regex source-scan. To prove this, we feed Pass1Entities for a
// model that does NOT exist in the source — if the regex fallback were
// still load-bearing, no edges would emit (the regex would find the
// real model and reject "Phantom.id" as unknown). With the plumbed path
// active, "Phantom.id" is canonically known and the edge IS emitted.
func TestORMFieldEdges_PlumbedSideChannelTakesPrecedence(t *testing.T) {
	src := `from django.db import models

def fetch(uid):
    return Phantom.objects.filter(id=uid)
`
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "phantom.py",
		Content:  []byte(src),
		Language: "python",
		Pass1Entities: []types.EntityRecord{
			{
				Kind:       "SCOPE.Schema",
				Subtype:    "field",
				Name:       "Phantom.id",
				SourceFile: "phantom.py",
			},
		},
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	found := false
	for _, r := range res.Relationships {
		if r.Kind == ormReadsFieldKind && r.Properties.Get("model") == "Phantom" && r.Properties.Get("field") == "id" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected READS_FIELD edge on Phantom.id via plumbed side-channel; got %d rels", len(res.Relationships))
	}
}

// Issue #2448 / Phase B: cross-file resolution. The model definition
// lives in models.py; views.py imports it and queries it via the ORM.
// The intra-file regex / Pass1Entities branch on views.py CANNOT see
// the model — only the CrossFileFields closure can. Verifies that with
// the closure attached, READS_FIELD edges land on User.cognito_id from
// the views.py call sites; and that WITHOUT the closure, no edges land
// (the byte-identical legacy behaviour).
func TestORMFieldEdges_CrossFileResolution_Phase_B(t *testing.T) {
	viewsSrc := `from .models import User

def get_user(uid):
    return User.objects.filter(cognito_id=uid)

def rotate(uid, new_id):
    User.objects.filter(id=uid).update(cognito_id=new_id)
`
	modelsFields := []types.EntityRecord{
		{
			Kind:       "SCOPE.Schema",
			Subtype:    "field",
			Name:       "User.cognito_id",
			SourceFile: "models.py",
		},
		{
			Kind:       "SCOPE.Schema",
			Subtype:    "field",
			Name:       "User.id",
			SourceFile: "models.py",
		},
	}
	lookup := BuildCrossFileFieldLookup(modelsFields)
	if lookup == nil {
		t.Fatalf("BuildCrossFileFieldLookup returned nil for non-empty input")
	}
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)

	// With cross-file lookup attached: edges should land.
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:            "views.py",
		Content:         []byte(viewsSrc),
		Language:        "python",
		CrossFileFields: lookup,
	})
	if err != nil {
		t.Fatalf("Detect (with cross-file): %v", err)
	}
	readsCog := 0
	writesCog := 0
	for _, r := range res.Relationships {
		if r.Properties.Get("model") != "User" || r.Properties.Get("field") != "cognito_id" {
			continue
		}
		if r.Properties.Get("resolution") != "cross_file" {
			t.Errorf("expected resolution=cross_file on %+v", r)
		}
		switch r.Kind {
		case ormReadsFieldKind:
			readsCog++
		case ormWritesFieldKind:
			writesCog++
		}
	}
	if readsCog < 1 {
		t.Errorf("expected at least 1 cross-file READS_FIELD on User.cognito_id, got 0")
	}
	if writesCog < 1 {
		t.Errorf("expected at least 1 cross-file WRITES_FIELD on User.cognito_id, got 0")
	}

	// Without the closure: legacy behaviour — no edges land for the
	// cross-file model.
	res2, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "views.py",
		Content:  []byte(viewsSrc),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect (no cross-file): %v", err)
	}
	for _, r := range res2.Relationships {
		if r.Kind == ormReadsFieldKind || r.Kind == ormWritesFieldKind {
			t.Errorf("unexpected field edge without cross-file lookup: %+v", r)
		}
	}
}

// Phase-B regression: the intra-file branch still wins when both are
// available — verify resolution=intra_file when the model IS defined
// in the current file, even with a cross-file closure attached.
func TestORMFieldEdges_IntraFileWinsOverCrossFile(t *testing.T) {
	src := `from django.db import models

class User(models.Model):
    cognito_id = models.CharField(max_length=64)

def get_user(uid):
    return User.objects.get(cognito_id=uid)
`
	// Cross-file closure also "knows" about User — intra-file must still win.
	lookup := BuildCrossFileFieldLookup([]types.EntityRecord{
		{Kind: "SCOPE.Schema", Subtype: "field", Name: "User.cognito_id", SourceFile: "models.py"},
	})
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:            "users.py",
		Content:         []byte(src),
		Language:        "python",
		CrossFileFields: lookup,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	found := false
	for _, r := range res.Relationships {
		if r.Kind != ormReadsFieldKind {
			continue
		}
		if r.Properties.Get("model") != "User" || r.Properties.Get("field") != "cognito_id" {
			continue
		}
		found = true
		if r.Properties.Get("resolution") != "intra_file" {
			t.Errorf("resolution = %q, want intra_file", r.Properties.Get("resolution"))
		}
	}
	if !found {
		t.Fatalf("expected intra-file READS_FIELD on User.cognito_id")
	}
}

// BuildCrossFileFieldLookup edge cases.
func TestBuildCrossFileFieldLookup_NilOnEmpty(t *testing.T) {
	if BuildCrossFileFieldLookup(nil) != nil {
		t.Error("nil input should return nil closure")
	}
	if BuildCrossFileFieldLookup([]types.EntityRecord{
		{Kind: "Class", Name: "User", SourceFile: "models.py"},
	}) != nil {
		t.Error("input with no SCOPE.Schema(subtype=field) records should return nil")
	}
}

// Symmetrical regression: when Pass1Entities is empty the regex
// fallback MUST still serve (existing test fixtures that construct
// FileInput directly continue working). This is implicitly covered by
// every other test in this file, but assert it explicitly with a Model
// the source DOES contain.
func TestORMFieldEdges_FallbackWhenSideChannelEmpty(t *testing.T) {
	src := `from django.db import models

class User(models.Model):
    cognito_id = models.CharField(max_length=64)

def get_user(uid):
    return User.objects.get(cognito_id=uid)
`
	edges := detectFieldEdges(t, src) // no Pass1Entities
	assertFieldEdge(t, edges, ormReadsFieldKind, "User", "cognito_id")
}
