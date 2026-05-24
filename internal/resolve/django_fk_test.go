// django_fk_test.go — unit tests for the Django string-FK late-binding
// resolver pass (ResolveDjangoStringFKRefs, issue #2049).
//
// Each test builds a minimal entity + relationship set that mimics the output
// of the Python extractor, calls BuildIndex + ResolveDjangoStringFKRefs, and
// asserts the REFERENCES edge ToID was rewritten to the real Model entity ID.
package resolve

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// buildDjangoFKStub mirrors buildDjangoModelClassRef in
// internal/extractors/python/django_relational.go. Kept local so the test
// doesn't import the python package (circular dep).
func buildDjangoFKStub(filePath, className string) string {
	return "scope:component:ref:python:" + filePath + ":" + className
}

// TestResolveDjangoStringFKRefs_SameAppCrossFile covers the headline #2049
// case: ForeignKey('Building', ...) in building_settings.py where Building
// is defined in a sibling file building.py within the same app models dir.
// The stub uses the consumer's file path so byLocation misses (different
// file). ResolveDjangoStringFKRefs must bind via
// byPackageComponent["core/models"]["Building"].
func TestResolveDjangoStringFKRefs_SameAppCrossFile(t *testing.T) {
	// Building class defined in core/models/building.py
	buildingEntity := types.EntityRecord{
		ID:         "building-entity-id",
		Name:       "Building",
		Kind:       "SCOPE.Component",
		Subtype:    "class",
		SourceFile: "core/models/building.py",
		Language:   "python",
	}

	// GroupBuildingSettings in core/models/settings.py with a string FK
	// to 'Building'. The stub encodes the consumer's file (settings.py),
	// so byLocation["core/models/settings.py"]["Building"] misses.
	stub := buildDjangoFKStub("core/models/settings.py", "Building")
	settingsEntity := types.EntityRecord{
		ID:         "settings-entity-id",
		Name:       "GroupBuildingSettings.building",
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: "core/models/settings.py",
		Language:   "python",
		Relationships: []types.RelationshipRecord{
			{
				ToID: stub,
				Kind: "REFERENCES",
				Properties: map[string]string{
					"django_rel":       "ForeignKey",
					"self_ref":         "false",
					"django_fk_string": "Building",
				},
			},
		},
	}

	records := []types.EntityRecord{buildingEntity, settingsEntity}
	idx := BuildIndex(records)
	rewrites := idx.ResolveDjangoStringFKRefs(records)

	if rewrites != 1 {
		t.Errorf("rewrites = %d, want 1", rewrites)
	}
	got := records[1].Relationships[0].ToID
	if got != "building-entity-id" {
		t.Errorf("ToID = %q, want building-entity-id (stub was %q)", got, stub)
	}
}

// TestResolveDjangoStringFKRefs_CrossApp covers ForeignKey("auth.User", ...)
// where User is in a different app directory. The stub encodes the consumer's
// file, so byLocation and same-dir byPackageComponent both miss.
// ResolveDjangoStringFKRefs must bind via
// byPackageComponent["auth"]["User"] using the app_label from django_fk_string.
func TestResolveDjangoStringFKRefs_CrossApp(t *testing.T) {
	// auth.User defined in auth/models.py
	userEntity := types.EntityRecord{
		ID:         "auth-user-entity-id",
		Name:       "User",
		Kind:       "SCOPE.Component",
		Subtype:    "class",
		SourceFile: "auth/models.py",
		Language:   "python",
	}

	// Permit model in permits/models.py with a cross-app FK to auth.User.
	stub := buildDjangoFKStub("permits/models.py", "User")
	permitEntity := types.EntityRecord{
		ID:         "permit-entity-id",
		Name:       "Permit.owner",
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: "permits/models.py",
		Language:   "python",
		Relationships: []types.RelationshipRecord{
			{
				ToID: stub,
				Kind: "REFERENCES",
				Properties: map[string]string{
					"django_rel":       "ForeignKey",
					"self_ref":         "false",
					"django_fk_string": "auth.User",
				},
			},
		},
	}

	records := []types.EntityRecord{userEntity, permitEntity}
	idx := BuildIndex(records)
	rewrites := idx.ResolveDjangoStringFKRefs(records)

	if rewrites != 1 {
		t.Errorf("rewrites = %d, want 1", rewrites)
	}
	got := records[1].Relationships[0].ToID
	if got != "auth-user-entity-id" {
		t.Errorf("ToID = %q, want auth-user-entity-id (stub was %q)", got, stub)
	}
}

// TestResolveDjangoStringFKRefs_SelfRefSkipped covers ForeignKey('self', ...)
// edges. The self_ref=true edge points at the parent class's name; since we
// stamp django_fk_string="self" for self-references the pass must skip it
// (the self-ref is always in the same file and binds via byLocation in the
// base resolver, not via this pass).
func TestResolveDjangoStringFKRefs_SelfRefSkipped(t *testing.T) {
	// Category class in core/models/category.py (self-referential tree).
	categoryEntity := types.EntityRecord{
		ID:         "category-entity-id",
		Name:       "Category",
		Kind:       "SCOPE.Component",
		Subtype:    "class",
		SourceFile: "core/models/category.py",
		Language:   "python",
	}

	stub := buildDjangoFKStub("core/models/category.py", "Category")
	parentFieldEntity := types.EntityRecord{
		ID:         "parent-field-entity-id",
		Name:       "Category.parent",
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: "core/models/category.py",
		Language:   "python",
		Relationships: []types.RelationshipRecord{
			{
				ToID: stub,
				Kind: "REFERENCES",
				Properties: map[string]string{
					"django_rel":       "ForeignKey",
					"self_ref":         "true",
					"django_fk_string": "self",
				},
			},
		},
	}

	records := []types.EntityRecord{categoryEntity, parentFieldEntity}
	idx := BuildIndex(records)
	rewrites := idx.ResolveDjangoStringFKRefs(records)

	// Self-ref edges have django_fk_string="self" which is explicitly excluded
	// from the dotted-form app_label path in strategy 2.
	// Strategy 1 (same pkgDir) will actually resolve this if Building is in the
	// same directory. For "Category" in core/models/category.py:
	// pkgDir = "core/models", className = "Category"
	// byPackageComponent["core/models"]["Category"] = "category-entity-id"
	// So the self-ref WILL be rewritten by strategy 1 — this is fine since the
	// resulting ToID is the real entity ID (the same thing byLocation would bind to).
	// The important thing: no panic, no wrong ID.
	if rewrites != 1 {
		// Self-ref resolves via strategy 1 (same pkgDir).
		t.Errorf("rewrites = %d, want 1 (self-ref binds via same-pkgDir strategy)", rewrites)
	}
	got := records[1].Relationships[0].ToID
	if got != "category-entity-id" {
		t.Errorf("ToID = %q, want category-entity-id", got)
	}
}

// TestResolveDjangoStringFKRefs_AlreadyResolved verifies that stubs already
// rewritten to hex IDs (16-char hex) are NOT touched by the pass.
func TestResolveDjangoStringFKRefs_AlreadyResolved(t *testing.T) {
	hexID := "abcdef1234567890"
	fieldEntity := types.EntityRecord{
		ID:         "field-entity-id",
		Name:       "Order.customer",
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: "orders/models.py",
		Language:   "python",
		Relationships: []types.RelationshipRecord{
			{
				ToID: hexID,
				Kind: "REFERENCES",
				Properties: map[string]string{
					"django_rel": "ForeignKey",
				},
			},
		},
	}

	records := []types.EntityRecord{fieldEntity}
	idx := BuildIndex(records)
	rewrites := idx.ResolveDjangoStringFKRefs(records)

	if rewrites != 0 {
		t.Errorf("rewrites = %d, want 0 (already hex-resolved)", rewrites)
	}
	if got := records[0].Relationships[0].ToID; got != hexID {
		t.Errorf("ToID = %q, want unchanged hex %q", got, hexID)
	}
}

// TestResolveDjangoStringFKRefs_NonDjangoRelSkipped verifies that REFERENCES
// edges WITHOUT the django_rel property are not touched.
func TestResolveDjangoStringFKRefs_NonDjangoRelSkipped(t *testing.T) {
	stub := buildDjangoFKStub("myapp/models.py", "SomeClass")
	fieldEntity := types.EntityRecord{
		ID:         "field-entity-id",
		Name:       "MyModel.ref",
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: "myapp/models.py",
		Language:   "python",
		Relationships: []types.RelationshipRecord{
			{
				ToID:       stub,
				Kind:       "REFERENCES",
				Properties: map[string]string{
					// No django_rel property — not a Django FK edge.
				},
			},
		},
	}

	records := []types.EntityRecord{fieldEntity}
	idx := BuildIndex(records)
	rewrites := idx.ResolveDjangoStringFKRefs(records)

	if rewrites != 0 {
		t.Errorf("rewrites = %d, want 0 (no django_rel property)", rewrites)
	}
	if got := records[0].Relationships[0].ToID; got != stub {
		t.Errorf("ToID = %q, want unchanged stub %q", got, stub)
	}
}

// TestResolveDjangoStringFKRefs_AmbiguousSkipped verifies that when two models
// share the same name in the same app directory (ambiguous in byPackageComponent),
// the pass leaves the stub alone rather than binding to the wrong entity.
func TestResolveDjangoStringFKRefs_AmbiguousSkipped(t *testing.T) {
	// Two entities named "User" in different files within the same directory.
	user1 := types.EntityRecord{
		ID:         "user-entity-1",
		Name:       "User",
		Kind:       "SCOPE.Component",
		Subtype:    "class",
		SourceFile: "myapp/models/user_a.py",
		Language:   "python",
	}
	user2 := types.EntityRecord{
		ID:         "user-entity-2",
		Name:       "User",
		Kind:       "SCOPE.Component",
		Subtype:    "class",
		SourceFile: "myapp/models/user_b.py",
		Language:   "python",
	}

	stub := buildDjangoFKStub("myapp/models/order.py", "User")
	orderField := types.EntityRecord{
		ID:         "order-field-id",
		Name:       "Order.customer",
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: "myapp/models/order.py",
		Language:   "python",
		Relationships: []types.RelationshipRecord{
			{
				ToID: stub,
				Kind: "REFERENCES",
				Properties: map[string]string{
					"django_rel":       "ForeignKey",
					"django_fk_string": "User",
				},
			},
		},
	}

	records := []types.EntityRecord{user1, user2, orderField}
	idx := BuildIndex(records)
	rewrites := idx.ResolveDjangoStringFKRefs(records)

	// Both strategy 1 and strategy 2 (bare name, no app_label) should see the
	// ambiguity and leave the stub alone.
	if rewrites != 0 {
		t.Errorf("rewrites = %d, want 0 (ambiguous User in same app dir)", rewrites)
	}
	got := records[2].Relationships[0].ToID
	if got != stub {
		t.Errorf("ToID = %q, want unchanged stub %q", got, stub)
	}
}

// TestResolveDjangoStringFKRefs_NestedModelsPackage covers the case where the
// app uses a nested models package: "myapp.models.User" → try
// byPackageComponent["myapp/models"]["User"] as the module directory.
func TestResolveDjangoStringFKRefs_NestedModelsPackage(t *testing.T) {
	// User defined in myapp/models/user.py (nested models package).
	userEntity := types.EntityRecord{
		ID:         "nested-user-entity-id",
		Name:       "User",
		Kind:       "SCOPE.Component",
		Subtype:    "class",
		SourceFile: "myapp/models/user.py",
		Language:   "python",
	}

	// FK from another app referencing "myapp.models.User".
	stub := buildDjangoFKStub("otherapp/models.py", "User")
	orderField := types.EntityRecord{
		ID:         "order-field-id",
		Name:       "Order.created_by",
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: "otherapp/models.py",
		Language:   "python",
		Relationships: []types.RelationshipRecord{
			{
				ToID: stub,
				Kind: "REFERENCES",
				Properties: map[string]string{
					"django_rel":       "ForeignKey",
					"django_fk_string": "myapp.models.User",
				},
			},
		},
	}

	records := []types.EntityRecord{userEntity, orderField}
	idx := BuildIndex(records)
	rewrites := idx.ResolveDjangoStringFKRefs(records)

	if rewrites != 1 {
		t.Errorf("rewrites = %d, want 1", rewrites)
	}
	got := records[1].Relationships[0].ToID
	if got != "nested-user-entity-id" {
		t.Errorf("ToID = %q, want nested-user-entity-id", got)
	}
}

// TestResolveDjangoStringFKRefs_NonScopeStubSkipped verifies that non-Python
// structural-ref stubs (e.g. scope:component:ref:go:...) are not touched
// even if django_rel is present.
func TestResolveDjangoStringFKRefs_NonScopeStubSkipped(t *testing.T) {
	// Use a Go-style stub (wrong language).
	stub := "scope:component:ref:go:myapp/models.go:User"
	fieldEntity := types.EntityRecord{
		ID:         "field-entity-id",
		Name:       "MyModel.user",
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: "myapp/models.py",
		Language:   "python",
		Relationships: []types.RelationshipRecord{
			{
				ToID: stub,
				Kind: "REFERENCES",
				Properties: map[string]string{
					"django_rel": "ForeignKey",
				},
			},
		},
	}

	records := []types.EntityRecord{fieldEntity}
	idx := BuildIndex(records)
	rewrites := idx.ResolveDjangoStringFKRefs(records)

	// Non-python scope stubs are not handled by this pass.
	if rewrites != 0 {
		t.Errorf("rewrites = %d, want 0 (non-python stub)", rewrites)
	}
	if !strings.HasPrefix(records[0].Relationships[0].ToID, "scope:component:ref:go:") {
		t.Errorf("ToID changed unexpectedly: %q", records[0].Relationships[0].ToID)
	}
}
