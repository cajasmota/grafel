// Tests for BuildFieldIndex (field_index.go, issue #2295).
//
// Coverage:
//
//	(a) scalar fields (CharField, IntegerField, BooleanField, etc.)
//	(b) FK / relation fields (ForeignKey, OneToOneField, ManyToManyField)
//	(c) custom / project-local field classes (MoneyField, etc.)
//	(d) multiple model classes in one file — fields stay on correct model
//	(e) abstract base class (no "models.Model" in parent, but "Model" present)
//	(f) module-scope assignments are NOT treated as fields
//	(g) empty / non-Django source returns empty map
//
// These tests exercise BuildFieldIndex in isolation — no detector pipeline
// needed. The integration tests in orm_field_edges_test.go cover the call
// site wiring through the full Detect path.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// (a) Scalar fields — basic smoke test.
func TestBuildFieldIndex_ScalarFields(t *testing.T) {
	src := `from django.db import models

class User(models.Model):
    cognito_id = models.CharField(max_length=64)
    email = models.EmailField(unique=True)
    is_active = models.BooleanField(default=True)
    created_at = models.DateTimeField(auto_now_add=True)
`
	idx := BuildFieldIndex(src)
	wantFields := []string{
		"User.cognito_id",
		"User.email",
		"User.is_active",
		"User.created_at",
	}
	for _, f := range wantFields {
		if !idx[f] {
			t.Errorf("BuildFieldIndex missing expected field %q; got index: %v", f, idx)
		}
	}
	if len(idx) != len(wantFields) {
		t.Errorf("BuildFieldIndex returned %d entries, want %d; index: %v", len(idx), len(wantFields), idx)
	}
}

// (b) FK / relation fields — ForeignKey, OneToOneField, ManyToManyField.
func TestBuildFieldIndex_FKFields(t *testing.T) {
	src := `from django.db import models

class Article(models.Model):
    author = models.ForeignKey("User", on_delete=models.CASCADE)
    reviewer = models.OneToOneField("User", on_delete=models.SET_NULL, null=True)
    tags = models.ManyToManyField("Tag", blank=True)
    title = models.CharField(max_length=200)
`
	idx := BuildFieldIndex(src)
	wantFields := []string{
		"Article.author",
		"Article.reviewer",
		"Article.tags",
		"Article.title",
	}
	for _, f := range wantFields {
		if !idx[f] {
			t.Errorf("BuildFieldIndex missing FK/relation field %q; got: %v", f, idx)
		}
	}
	if len(idx) != len(wantFields) {
		t.Errorf("BuildFieldIndex returned %d entries, want %d; index: %v", len(idx), len(wantFields), idx)
	}
}

// (c) Custom / project-local field classes (e.g. django-money MoneyField,
// phonenumber PhoneNumberField, etc.).
func TestBuildFieldIndex_CustomFields(t *testing.T) {
	src := `from django.db import models
from djmoney.models.fields import MoneyField

class Invoice(models.Model):
    amount = MoneyField(max_digits=14, decimal_places=2)
    notes = models.TextField(blank=True)
`
	idx := BuildFieldIndex(src)
	if !idx["Invoice.amount"] {
		t.Errorf("BuildFieldIndex should recognise custom MoneyField; got: %v", idx)
	}
	if !idx["Invoice.notes"] {
		t.Errorf("BuildFieldIndex missing Invoice.notes; got: %v", idx)
	}
}

// (d) Multiple model classes — fields stay on their own model, no cross-
// contamination.
func TestBuildFieldIndex_MultipleModels(t *testing.T) {
	src := `from django.db import models

class User(models.Model):
    email = models.EmailField()
    name = models.CharField(max_length=100)

class Post(models.Model):
    title = models.CharField(max_length=200)
    body = models.TextField()
    author = models.ForeignKey("User", on_delete=models.CASCADE)
`
	idx := BuildFieldIndex(src)
	userFields := []string{"User.email", "User.name"}
	postFields := []string{"Post.title", "Post.body", "Post.author"}

	for _, f := range userFields {
		if !idx[f] {
			t.Errorf("missing User field %q; got: %v", f, idx)
		}
	}
	for _, f := range postFields {
		if !idx[f] {
			t.Errorf("missing Post field %q; got: %v", f, idx)
		}
	}
	// Cross-contamination: Post fields must not appear under User and vice versa.
	if idx["User.title"] {
		t.Errorf("User.title should not exist (it belongs to Post); got: %v", idx)
	}
	if idx["Post.email"] {
		t.Errorf("Post.email should not exist (it belongs to User); got: %v", idx)
	}
}

// (e) Abstract base class — still indexed if parent name contains "Model".
func TestBuildFieldIndex_AbstractBaseClass(t *testing.T) {
	src := `from django.db import models

class TimestampedModel(models.Model):
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)

    class Meta:
        abstract = True

class Widget(TimestampedModel):
    label = models.CharField(max_length=50)
`
	idx := BuildFieldIndex(src)
	if !idx["TimestampedModel.created_at"] {
		t.Errorf("BuildFieldIndex missing TimestampedModel.created_at; got: %v", idx)
	}
	if !idx["TimestampedModel.updated_at"] {
		t.Errorf("BuildFieldIndex missing TimestampedModel.updated_at; got: %v", idx)
	}
	if !idx["Widget.label"] {
		t.Errorf("BuildFieldIndex missing Widget.label; got: %v", idx)
	}
}

// (f) Module-scope assignments (not inside a class body) must NOT be indexed.
func TestBuildFieldIndex_ModuleScopeAssignmentsSkipped(t *testing.T) {
	src := `from django.db import models

# Module-scope helper — must NOT be indexed as a field.
User = get_user_model()

class Profile(models.Model):
    bio = models.TextField(blank=True)
`
	idx := BuildFieldIndex(src)
	// "User" at module scope looks like a module-level assignment, not a
	// field declaration, because it lacks leading indentation.
	if idx["Profile.User"] || idx["User.User"] {
		t.Errorf("module-scope assignment was incorrectly indexed as a field; got: %v", idx)
	}
	if !idx["Profile.bio"] {
		t.Errorf("BuildFieldIndex missing Profile.bio; got: %v", idx)
	}
}

// (g) Non-Django / empty source returns an empty map without panicking.
func TestBuildFieldIndex_EmptyOrNonDjango(t *testing.T) {
	cases := []string{
		"",
		"def helper(): pass\n",
		"class Foo:\n    x = 1\n",
	}
	for _, src := range cases {
		idx := BuildFieldIndex(src)
		if len(idx) != 0 {
			t.Errorf("BuildFieldIndex(%q) = %v, want empty map", src, idx)
		}
	}
}

// (h) buildPlumbedPythonORMFieldIndex — the Pass 1 side-channel introduced
// in issue #2352. The function (now a thin wrapper over buildPlumbedFieldIndex
// with the isPythonORMField predicate) takes the Pass 1 entity slice the
// Python extractor would have emitted at python/extractor.go:1411-1421 and
// returns the same `<Model>.<field>` presence set BuildFieldIndex would have
// produced from source. The two paths MUST be byte-identical on the key set,
// otherwise consumers (notably applyORMFieldEdges) will behave differently
// depending on which path served them.
func TestBuildPlumbedFieldIndex_HappyPath(t *testing.T) {
	pass1 := []types.EntityRecord{
		{Kind: "SCOPE.Schema", Subtype: "field", Name: "User.cognito_id", SourceFile: "users.py"},
		{Kind: "SCOPE.Schema", Subtype: "field", Name: "User.email", SourceFile: "users.py"},
		// Different file — must be filtered out.
		{Kind: "SCOPE.Schema", Subtype: "field", Name: "Order.total", SourceFile: "orders.py"},
		// Different kind — must be filtered out by isPythonORMField.
		{Kind: "Function", Name: "User.save", SourceFile: "users.py"},
		// Missing dot in Name — defensive skip.
		{Kind: "SCOPE.Schema", Subtype: "field", Name: "loose", SourceFile: "users.py"},
	}
	idx := buildPlumbedPythonORMFieldIndex("users.py", pass1)
	want := map[string]bool{
		"User.cognito_id": true,
		"User.email":      true,
	}
	if len(idx) != len(want) {
		t.Fatalf("buildPlumbedPythonORMFieldIndex returned %d entries, want %d; got %v", len(idx), len(want), idx)
	}
	for k := range want {
		if !idx[k] {
			t.Errorf("buildPlumbedPythonORMFieldIndex missing %q; got %v", k, idx)
		}
	}
}

// Empty input → empty (never-nil) map so callers can do an `if len == 0`
// triage to decide between plumbed and fallback paths.
func TestBuildPlumbedFieldIndex_EmptyInput(t *testing.T) {
	idx := buildPlumbedPythonORMFieldIndex("users.py", nil)
	if idx == nil {
		t.Fatal("buildPlumbedPythonORMFieldIndex returned nil map, want empty non-nil")
	}
	if len(idx) != 0 {
		t.Errorf("buildPlumbedPythonORMFieldIndex returned %d entries, want 0", len(idx))
	}
}

// (i) buildPlumbedFieldIndex with a generic predicate (issue #2431).
//
// Exercises the refactored generic core directly with a non-ORM predicate —
// here a predicate that accepts ANY entity whose Kind is "Function". This
// verifies that the predicate is evaluated and that path-filtering still
// works when the ORM-specific isPythonORMField predicate is NOT used.
//
// The test intentionally uses a contrived, non-ORM kind so it is clearly
// decoupled from the Python/Django domain and can serve as the template
// for future consumers (e.g. SQLAlchemy columns, GraphQL resolvers).
func TestBuildPlumbedFieldIndex_GenericPredicate(t *testing.T) {
	// acceptAnyFunction matches any entity with Kind == "Function",
	// regardless of Subtype. This is the "contrived filter" required by
	// issue #2431 to prove the generic predicate path works.
	acceptAnyFunction := func(e types.EntityRecord) bool {
		return e.Kind == "Function"
	}

	pass1 := []types.EntityRecord{
		// Should be included: Function kind, correct file, has dot.
		{Kind: "Function", Name: "Foo.bar", SourceFile: "foo.py"},
		{Kind: "Function", Name: "Foo.baz", SourceFile: "foo.py"},
		// Should be excluded: ORM field kind — predicate rejects it.
		{Kind: "SCOPE.Schema", Subtype: "field", Name: "Foo.col", SourceFile: "foo.py"},
		// Should be excluded: wrong file.
		{Kind: "Function", Name: "Bar.qux", SourceFile: "bar.py"},
		// Should be excluded: Name has no dot — defensive skip.
		{Kind: "Function", Name: "nodot", SourceFile: "foo.py"},
		// Should be included: empty SourceFile is treated as "any file" (defensive allowance).
		{Kind: "Function", Name: "Baz.method", SourceFile: ""},
	}

	idx := buildPlumbedFieldIndex("foo.py", pass1, acceptAnyFunction)

	want := map[string]bool{
		"Foo.bar":    true,
		"Foo.baz":    true,
		"Baz.method": true,
	}
	if len(idx) != len(want) {
		t.Fatalf("buildPlumbedFieldIndex(generic) returned %d entries, want %d; got %v", len(idx), len(want), idx)
	}
	for k := range want {
		if !idx[k] {
			t.Errorf("buildPlumbedFieldIndex(generic) missing %q; got %v", k, idx)
		}
	}
	// ORM record must not appear — predicate rejected it.
	if idx["Foo.col"] {
		t.Errorf("buildPlumbedFieldIndex(generic) should NOT include ORM field Foo.col; got %v", idx)
	}
}

// (j) buildPlumbedFieldIndex with nil predicate — nil is treated as
// "accept all" (after the path filter), so every record with a dotted
// Name on the target file is indexed regardless of Kind/Subtype.
func TestBuildPlumbedFieldIndex_NilPredicateAcceptsAll(t *testing.T) {
	pass1 := []types.EntityRecord{
		{Kind: "SCOPE.Schema", Subtype: "field", Name: "User.email", SourceFile: "x.py"},
		{Kind: "Function", Name: "User.save", SourceFile: "x.py"},
		{Kind: "Class", Name: "User.Meta", SourceFile: "x.py"},
		// No dot — must be skipped even with nil predicate.
		{Kind: "Function", Name: "helper", SourceFile: "x.py"},
	}
	idx := buildPlumbedFieldIndex("x.py", pass1, nil)
	if !idx["User.email"] {
		t.Errorf("nil pred: missing User.email; got %v", idx)
	}
	if !idx["User.save"] {
		t.Errorf("nil pred: missing User.save; got %v", idx)
	}
	if !idx["User.Meta"] {
		t.Errorf("nil pred: missing User.Meta; got %v", idx)
	}
	if idx["helper"] {
		t.Errorf("nil pred: 'helper' (no dot) should not be indexed; got %v", idx)
	}
}
