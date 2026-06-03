package php_test

import "testing"

// apiplatform_test.go — value-asserting tests for the API Platform (Symfony
// REST-resource) extractor. Record lang.php.framework.api-platform.
// Issue #3556 (epic #3505).

// TestAPIPlatform_DefaultCRUD asserts a bare #[ApiResource] on Book generates
// the full default CRUD endpoint set under /books and /books/{id}.
func TestAPIPlatform_DefaultCRUD(t *testing.T) {
	src := `<?php
namespace App\Entity;
use ApiPlatform\Metadata\ApiResource;

#[ApiResource]
class Book
{
    public int $id;
    public string $title;
}
`
	ents := extract(t, "custom_php_api_platform", fi("Book.php", "php", src))
	if len(ents) == 0 {
		t.Fatal("[api-platform] expected entities, got none")
	}

	for _, want := range []string{
		"GET /books",
		"POST /books",
		"GET /books/{id}",
		"PUT /books/{id}",
		"PATCH /books/{id}",
		"DELETE /books/{id}",
	} {
		if !containsEntity(ents, "SCOPE.Operation", want) {
			t.Errorf("expected generated endpoint %q", want)
		}
	}
}

// TestAPIPlatform_Pluralisation asserts the default-path pluraliser handles the
// -y → -ies case (Category → /categories) and the sibilant -s → -ses case.
func TestAPIPlatform_Pluralisation(t *testing.T) {
	src := `<?php
use ApiPlatform\Metadata\ApiResource;
#[ApiResource]
class Category {}
`
	ents := extract(t, "custom_php_api_platform", fi("Category.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /categories") {
		t.Error("expected GET /categories (y→ies pluralisation)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET /categories/{id}") {
		t.Error("expected GET /categories/{id}")
	}
}

// TestAPIPlatform_ExplicitOperations asserts that when explicit operation
// attributes are declared, ONLY those operations are emitted (not the full
// default CRUD set).
func TestAPIPlatform_ExplicitOperations(t *testing.T) {
	src := `<?php
use ApiPlatform\Metadata\ApiResource;
use ApiPlatform\Metadata\Get;
use ApiPlatform\Metadata\GetCollection;

#[ApiResource]
#[Get]
#[GetCollection]
class Author {}
`
	ents := extract(t, "custom_php_api_platform", fi("Author.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /authors") {
		t.Error("expected GET /authors collection op")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET /authors/{id}") {
		t.Error("expected GET /authors/{id} item op")
	}
	// POST/PUT/PATCH/DELETE were NOT declared, so they must NOT be emitted.
	for _, notWant := range []string{
		"POST /authors", "PUT /authors/{id}", "DELETE /authors/{id}",
	} {
		if containsEntity(ents, "SCOPE.Operation", notWant) {
			t.Errorf("undeclared operation leaked: %q", notWant)
		}
	}
}

// TestAPIPlatform_OperationsListAndUriTemplate asserts the `operations: [...]`
// argument form is honoured and a per-operation uriTemplate override wins over
// the derived default path.
func TestAPIPlatform_OperationsListAndUriTemplate(t *testing.T) {
	src := `<?php
use ApiPlatform\Metadata\ApiResource;
use ApiPlatform\Metadata\Get;
use ApiPlatform\Metadata\Post;

#[ApiResource(operations: [
    new Get(),
    new Post(uriTemplate: '/books/publish'),
])]
class Book {}
`
	ents := extract(t, "custom_php_api_platform", fi("Book.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /books/{id}") {
		t.Error("expected GET /books/{id} from operations: list")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /books/publish") {
		t.Error("expected POST /books/publish from uriTemplate override")
	}
	// Default Post path /books must NOT appear because uriTemplate overrode it.
	if containsEntity(ents, "SCOPE.Operation", "POST /books") {
		t.Error("default POST /books leaked despite uriTemplate override")
	}
}

// TestAPIPlatform_Filter asserts #[ApiFilter] declarations are captured as a
// filter-set schema entity on the resource.
func TestAPIPlatform_Filter(t *testing.T) {
	src := `<?php
use ApiPlatform\Metadata\ApiResource;
use ApiPlatform\Metadata\ApiFilter;
use ApiPlatform\Doctrine\Orm\Filter\SearchFilter;

#[ApiResource]
#[ApiFilter(SearchFilter::class, properties: ['title' => 'partial'])]
class Book {}
`
	ents := extract(t, "custom_php_api_platform", fi("Book.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "api_platform_filters:Book") {
		t.Error("expected api_platform_filters:Book filter-set entity")
	}
}

// TestAPIPlatform_PerOperationSecurity asserts a per-operation `security:`
// expression stamps the EXACT auth properties on that REST endpoint and that an
// operation WITHOUT security is left unsecured (auth parity with the
// api-platform-graphql sibling; epic #3872).
func TestAPIPlatform_PerOperationSecurity(t *testing.T) {
	src := `<?php
use ApiPlatform\Metadata\ApiResource;
use ApiPlatform\Metadata\Get;
use ApiPlatform\Metadata\Delete;

#[ApiResource(operations: [
    new Get(),
    new Delete(security: "is_granted('ROLE_ADMIN')"),
])]
class Book {}
`
	ents := extractRecords(t, "custom_php_api_platform", fi("Book.php", "php", src))
	del := findRecord(ents, "SCOPE.Operation", "DELETE /books/{id}")
	if del == nil {
		t.Fatal("expected DELETE /books/{id} endpoint")
	}
	if got := prop(t, del, "auth_required"); got != "true" {
		t.Errorf("auth_required = %q, want true", got)
	}
	if got := prop(t, del, "auth_roles"); got != "ROLE_ADMIN" {
		t.Errorf("auth_roles = %q, want ROLE_ADMIN", got)
	}
	if got := prop(t, del, "auth_method"); got != "expression" {
		t.Errorf("auth_method = %q, want expression", got)
	}
	if got := prop(t, del, "auth_expression"); got != "is_granted('ROLE_ADMIN')" {
		t.Errorf("auth_expression = %q, want is_granted('ROLE_ADMIN')", got)
	}
	// The Get() op has no security: → must NOT be auth_required (negative).
	get := findRecord(ents, "SCOPE.Operation", "GET /books/{id}")
	if get == nil {
		t.Fatal("expected GET /books/{id} endpoint")
	}
	if got := get.Properties["auth_required"]; got != "" {
		t.Errorf("unsecured GET leaked auth_required = %q", got)
	}
}

// TestAPIPlatform_ResourceWideSecurity asserts a resource-wide `security:` on
// #[ApiResource(...)] guards every generated default-CRUD endpoint, and that a
// per-operation security overrides the resource-wide expression.
func TestAPIPlatform_ResourceWideSecurity(t *testing.T) {
	src := `<?php
use ApiPlatform\Metadata\ApiResource;
use ApiPlatform\Metadata\Get;
use ApiPlatform\Metadata\Delete;

#[ApiResource(
    security: "is_granted('ROLE_USER')",
    operations: [
        new Get(),
        new Delete(security: "is_granted('ROLE_ADMIN', object)"),
    ]
)]
class Book {}
`
	ents := extractRecords(t, "custom_php_api_platform", fi("Book.php", "php", src))
	// Get inherits the resource-wide ROLE_USER expression.
	get := findRecord(ents, "SCOPE.Operation", "GET /books/{id}")
	if get == nil {
		t.Fatal("expected GET /books/{id} endpoint")
	}
	if got := prop(t, get, "auth_required"); got != "true" {
		t.Errorf("inherited auth_required = %q, want true", got)
	}
	if got := prop(t, get, "auth_roles"); got != "ROLE_USER" {
		t.Errorf("inherited auth_roles = %q, want ROLE_USER", got)
	}
	// Delete's per-operation security overrides the resource-wide one.
	del := findRecord(ents, "SCOPE.Operation", "DELETE /books/{id}")
	if del == nil {
		t.Fatal("expected DELETE /books/{id} endpoint")
	}
	if got := prop(t, del, "auth_roles"); got != "ROLE_ADMIN" {
		t.Errorf("per-op override auth_roles = %q, want ROLE_ADMIN", got)
	}
}

// TestAPIPlatform_NoMatch verifies the extractor is a no-op on plain PHP.
func TestAPIPlatform_NoMatch(t *testing.T) {
	src := `<?php class Plain { public function go() { return 1; } }`
	ents := extract(t, "custom_php_api_platform", fi("Plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities on plain PHP, got %d", len(ents))
	}
}

// TestAPIPlatform_WrongLanguage verifies the language gate.
func TestAPIPlatform_WrongLanguage(t *testing.T) {
	src := `#[ApiResource] class Book {}`
	ents := extract(t, "custom_php_api_platform", fi("Book.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-php language, got %d", len(ents))
	}
}
