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
