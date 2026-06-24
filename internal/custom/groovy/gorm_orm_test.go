package groovy_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/custom/groovy"
)

func extractGorm(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_groovy_gorm_orm")
	if !ok {
		t.Fatal("custom_groovy_gorm_orm not registered")
	}
	ents, err := e.Extract(context.Background(), fi(path, "groovy", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func findByKindSub(ents []types.EntityRecord, kind, sub, name string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == kind && e.Subtype == sub && e.Name == name {
			return e
		}
	}
	return nil
}

func relOf(e *types.EntityRecord, kind, toID string) *types.RelationshipRecord {
	if e == nil {
		return nil
	}
	for i := range e.Relationships {
		r := &e.Relationships[i]
		if r.Kind == kind && r.ToID == toID {
			return r
		}
	}
	return nil
}

// A domain class under grails-app/domain/ yields model+table+columns, with
// hasMany/belongsTo associations and a belongsTo REFERENCES FK edge.
func TestGormORM_DomainModelTableColumnsAssoc(t *testing.T) {
	src := `package com.example

class Book {
    String title
    BigDecimal price
    Integer pages
    Date dateCreated

    static belongsTo = [author: Author]

    static constraints = {
        title blank: false
    }

    static mapping = {
        table 'books'
    }
}
`
	ents := extractGorm(t, "grails-app/domain/com/example/Book.groovy", src)

	model := findByKindSub(ents, "SCOPE.Schema", "model", "Book")
	if model == nil {
		t.Fatalf("expected model Book; got %v", names(ents))
	}
	if model.Properties["framework"] != "gorm" {
		t.Errorf("model framework=%q, want gorm", model.Properties["framework"])
	}

	// Explicit table name from static mapping.
	table := findByKindSub(ents, "SCOPE.Schema", "table", "books")
	if table == nil {
		t.Fatalf("expected table 'books'; got %v", names(ents))
	}
	if table.Properties["model"] != "Book" {
		t.Errorf("table model=%q, want Book", table.Properties["model"])
	}

	for _, want := range []string{"title", "price", "pages", "dateCreated"} {
		c := findByKindSub(ents, "SCOPE.Schema", "column", want)
		if c == nil {
			t.Errorf("expected column %q; got %v", want, names(ents))
			continue
		}
		if c.Properties["model"] != "Book" {
			t.Errorf("column %q model=%q, want Book", want, c.Properties["model"])
		}
	}
	if c := findByKindSub(ents, "SCOPE.Schema", "column", "price"); c != nil {
		if c.Properties["column_type"] != "BigDecimal" {
			t.Errorf("price column_type=%q, want BigDecimal", c.Properties["column_type"])
		}
	}

	// belongsTo association entity + REFERENCES FK edge model->Author.
	assoc := findByKindSub(ents, "SCOPE.Schema", "association", "author")
	if assoc == nil {
		t.Fatalf("expected association 'author'; got %v", names(ents))
	}
	if assoc.Properties["assoc_kind"] != "belongsTo" || assoc.Properties["target"] != "Author" {
		t.Errorf("association author kind=%q target=%q, want belongsTo/Author",
			assoc.Properties["assoc_kind"], assoc.Properties["target"])
	}
	if r := relOf(model, "REFERENCES", "Author"); r == nil {
		t.Errorf("expected REFERENCES edge Book->Author; model rels=%v", model.Relationships)
	} else if r.Properties["fk_field"] != "author" {
		t.Errorf("REFERENCES fk_field=%q, want author", r.Properties["fk_field"])
	}
}

// hasMany / hasOne map associations are recorded with their target classes.
func TestGormORM_HasManyHasOne(t *testing.T) {
	src := `class Author {
    String name
    static hasMany = [books: Book, awards: Award]
    static hasOne = [profile: Profile]
}
`
	ents := extractGorm(t, "grails-app/domain/Author.groovy", src)

	for _, tc := range []struct{ name, kind, target string }{
		{"books", "hasMany", "Book"},
		{"awards", "hasMany", "Award"},
		{"profile", "hasOne", "Profile"},
	} {
		a := findByKindSub(ents, "SCOPE.Schema", "association", tc.name)
		if a == nil {
			t.Errorf("expected association %q; got %v", tc.name, names(ents))
			continue
		}
		if a.Properties["assoc_kind"] != tc.kind || a.Properties["target"] != tc.target {
			t.Errorf("association %q kind=%q target=%q, want %s/%s",
				tc.name, a.Properties["assoc_kind"], a.Properties["target"], tc.kind, tc.target)
		}
	}
	// hasMany/hasOne are not FK-owning on this side: no REFERENCES edges.
	model := findByKindSub(ents, "SCOPE.Schema", "model", "Author")
	if r := relOf(model, "REFERENCES", "Book"); r != nil {
		t.Errorf("hasMany should not emit a REFERENCES edge, got %v", r)
	}
}

// Implicit table name = class name when no static mapping table is declared.
func TestGormORM_ImplicitTableName(t *testing.T) {
	src := `class Widget {
    String label
    static constraints = {}
}
`
	ents := extractGorm(t, "grails-app/domain/Widget.groovy", src)
	if findByKindSub(ents, "SCOPE.Schema", "table", "Widget") == nil {
		t.Fatalf("expected implicit table 'Widget'; got %v", names(ents))
	}
}

// A domain recognised OUTSIDE grails-app/domain/ via the static DSL marker.
func TestGormORM_RecognisedByStaticMarkerOutsideDomainDir(t *testing.T) {
	src := `class Account {
    String owner
    static hasMany = [transactions: Transaction]
}
`
	ents := extractGorm(t, "src/main/groovy/Account.groovy", src)
	if findByKindSub(ents, "SCOPE.Schema", "model", "Account") == nil {
		t.Fatalf("expected Account model via static marker; got %v", names(ents))
	}
}

// Query attribution: GORM finders/persistence verbs on a known domain emit
// QUERIES edges model->table; an unknown receiver is never attributed.
func TestGormORM_QueryAttribution(t *testing.T) {
	src := `class Book {
    String title
    static mapping = { table 'books' }
}

class BookService {
    def list() {
        def all = Book.findAllByTitleLike('%a%')
        def one = Book.get(1)
        def n = Book.count()
        Book.save(new Book(title: 't'))
        Book.delete(one)
        Unknown.list()
    }
}
`
	ents := extractGorm(t, "grails-app/domain/Book.groovy", src)
	model := findByKindSub(ents, "SCOPE.Schema", "model", "Book")
	if model == nil {
		t.Fatalf("expected Book model; got %v", names(ents))
	}
	for _, op := range []string{"select", "insert", "delete"} {
		found := false
		for _, r := range model.Relationships {
			if r.Kind == "QUERIES" && r.Properties["operation"] == op && r.ToID == "books" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected QUERIES %s edge Book->books; rels=%v", op, model.Relationships)
		}
	}
	// Unknown.list must NOT create a model/query for Unknown.
	if findByKindSub(ents, "SCOPE.Schema", "model", "Unknown") != nil {
		t.Error("Unknown.list() must not synthesise an Unknown model")
	}
}

// withTransaction block on a domain emits a transaction_boundary.
func TestGormORM_Transaction(t *testing.T) {
	src := `class Book {
    String title
    static mapping = { table 'books' }
}

class BookService {
    def reassign() {
        Book.withTransaction { status ->
            Book.get(1).save()
        }
    }
}
`
	ents := extractGorm(t, "grails-app/domain/Book.groovy", src)
	tx := findByKindSub(ents, "SCOPE.Pattern", "transaction_boundary", "Book.withTransaction")
	if tx == nil {
		t.Fatalf("expected transaction_boundary; got %v", names(ents))
	}
	if tx.Properties["transactional"] != "true" || tx.Properties["framework"] != "gorm" {
		t.Errorf("transaction props=%v", tx.Properties)
	}
}

// A non-domain Groovy file (no domain dir, no GORM marker) is a no-op.
func TestGormORM_NonDomainNoop(t *testing.T) {
	src := `class PlainHelper {
    String greet(String n) { "hi $n" }
}
`
	ents := extractGorm(t, "src/main/groovy/PlainHelper.groovy", src)
	if len(ents) != 0 {
		t.Fatalf("expected no entities for a non-domain class, got %d: %v", len(ents), names(ents))
	}
}

// Single-class belongsTo form: static belongsTo = Author.
func TestGormORM_SingleClassBelongsTo(t *testing.T) {
	src := `class Chapter {
    String heading
    static belongsTo = Book
}
`
	ents := extractGorm(t, "grails-app/domain/Chapter.groovy", src)
	model := findByKindSub(ents, "SCOPE.Schema", "model", "Chapter")
	if model == nil {
		t.Fatalf("expected Chapter model; got %v", names(ents))
	}
	if r := relOf(model, "REFERENCES", "Book"); r == nil {
		t.Errorf("expected REFERENCES Chapter->Book; rels=%v", model.Relationships)
	}
	if findByKindSub(ents, "SCOPE.Schema", "association", "book") == nil {
		t.Errorf("expected association 'book'; got %v", names(ents))
	}
}

// GORM event hooks (def beforeInsert/afterUpdate/…) become SCOPE.Operation
// callback entities stamped with callback_type + owning model.
func TestGormORM_LifecycleHooks(t *testing.T) {
	src := `class Book {
    String title
    Date lastUpdated

    def beforeInsert() {
        title = title?.trim()
    }
    def afterUpdate() {
        log.info "updated"
    }
}
`
	ents := extractGorm(t, "grails-app/domain/Book.groovy", src)
	for _, hook := range []string{"beforeInsert", "afterUpdate"} {
		e := findByKindSub(ents, "SCOPE.Operation", "function", "Book."+hook)
		if e == nil {
			t.Errorf("expected lifecycle hook Book.%s; got %v", hook, names(ents))
			continue
		}
		if e.Properties["callback_type"] != hook || e.Properties["model"] != "Book" {
			t.Errorf("hook %s props=%v", hook, e.Properties)
		}
		if e.Properties["framework"] != "gorm" {
			t.Errorf("hook %s framework=%q", hook, e.Properties["framework"])
		}
	}
}

func names(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		out = append(out, e.Subtype+":"+e.Name)
	}
	return out
}
