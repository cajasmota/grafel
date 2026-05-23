package python_test

// Order-dedup regression tests for issue #1501.
//
// The polyglot-platform fixture has:
//   - libs/py-shared/py_shared/models.py  — class Order(BaseModel) [Pydantic]
//   - services/search-graphql/src/schema.graphql — type Order { … }
//
// Before the fix, running all three Python-side extractors produced 3 canonical
// "Order" nodes (SCOPE.Component/class from base Python + SCOPE.Schema from
// FastAPI + SCOPE.Schema from SQLAlchemy). The GraphQL extractor produced 1
// more (SCOPE.Schema/type), totalling 4 canonical "Order" entities — reported
// as 6 when GraphQL field sub-entities (Order.id, Order.status, Order.totalCents)
// are counted as well.
//
// After the fix, only 2 canonical "Order" entities exist (one per language):
//   - SCOPE.Component/class "Order" from the base Python extractor
//   - SCOPE.Schema/type     "Order" from the GraphQL extractor
//
// The 3 GraphQL field sub-entities (Order.*) are legitimate and are preserved.

import (
	"context"
	"os"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/python"
	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/graphql"
	_ "github.com/cajasmota/archigraph/internal/extractors/python"
)

const polyglotPlatformBase = "/Users/jorgecajas/Documents/Projects/polyglot-platform"

// TestOrderDedup_PythonNoFastAPIOrSQLAlchemyDuplicate asserts that the
// python_fastapi and python_sqlalchemy extractors do NOT emit standalone
// "Order" entities for the shared Pydantic models.py file.
// Issue #1501 — within-extractor dedup, fixes 1 and 2.
func TestOrderDedup_PythonNoFastAPIOrSQLAlchemyDuplicate(t *testing.T) {
	pyContent, err := os.ReadFile(polyglotPlatformBase + "/libs/py-shared/py_shared/models.py")
	if err != nil {
		t.Skip("polyglot-platform not found:", err)
	}

	fileInput := extractor.FileInput{
		Path:     "libs/py-shared/py_shared/models.py",
		Content:  pyContent,
		Language: "python",
	}

	for _, key := range []string{"python_fastapi", "python_sqlalchemy"} {
		ext, ok := extractor.Get(key)
		if !ok {
			t.Fatalf("extractor %q not registered", key)
		}
		ents, err := ext.Extract(context.Background(), fileInput)
		if err != nil {
			t.Fatalf("%s.Extract: %v", key, err)
		}
		for _, e := range ents {
			if e.Name == "Order" {
				t.Errorf("extractor %q must not emit entity for Pydantic class Order (issue #1501): "+
					"got Kind=%q Subtype=%q; the base Python extractor already emits SCOPE.Component/class",
					key, e.Kind, e.Subtype)
			}
			if e.Name == "OrderItem" {
				t.Errorf("extractor %q must not emit entity for Pydantic class OrderItem (issue #1501): "+
					"got Kind=%q Subtype=%q", key, e.Kind, e.Subtype)
			}
		}
	}
}

// TestOrderDedup_PythonBaseEmitsOneCanonicalOrder asserts that the base Python
// extractor emits exactly one SCOPE.Component/class entity named "Order" from
// models.py, with no SCOPE.Schema duplicate alongside it.
func TestOrderDedup_PythonBaseEmitsOneCanonicalOrder(t *testing.T) {
	pyContent, err := os.ReadFile(polyglotPlatformBase + "/libs/py-shared/py_shared/models.py")
	if err != nil {
		t.Skip("polyglot-platform not found:", err)
	}

	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "libs/py-shared/py_shared/models.py",
		Content:  pyContent,
		Language: "python",
	})
	if err != nil {
		t.Fatalf("python.Extract: %v", err)
	}

	var componentCount, schemaCount int
	for _, e := range ents {
		if e.Name == "Order" {
			switch e.Kind {
			case "SCOPE.Component":
				componentCount++
			case "SCOPE.Schema":
				schemaCount++
			}
		}
	}
	if componentCount != 1 {
		t.Errorf("expected exactly 1 SCOPE.Component/class entity for Order, got %d", componentCount)
	}
	if schemaCount != 0 {
		t.Errorf("expected 0 SCOPE.Schema entities for Order from base Python extractor, got %d", schemaCount)
	}
}

// TestOrderDedup_GraphQLStillEmitsOrderType asserts that the GraphQL extractor
// still emits the canonical SCOPE.Schema/type "Order" entity from schema.graphql.
// This is a DIFFERENT source language from the Python model and must be preserved.
func TestOrderDedup_GraphQLStillEmitsOrderType(t *testing.T) {
	gqlContent, err := os.ReadFile(polyglotPlatformBase + "/services/search-graphql/src/schema.graphql")
	if err != nil {
		t.Skip("polyglot-platform not found:", err)
	}

	ext, ok := extractor.Get("graphql")
	if !ok {
		t.Fatal("graphql extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "services/search-graphql/src/schema.graphql",
		Content:  gqlContent,
		Language: "graphql",
	})
	if err != nil {
		t.Fatalf("graphql.Extract: %v", err)
	}

	found := false
	for _, e := range ents {
		if e.Name == "Order" && e.Kind == "SCOPE.Schema" && e.Subtype == "type" {
			found = true
			break
		}
	}
	if !found {
		t.Error("graphql extractor must still emit SCOPE.Schema/type Order from schema.graphql (legitimate cross-language instance)")
	}
}

// TestOrderDedup_TotalCanonicalOrderCount verifies the end-to-end canonical
// "Order" entity count across all extractors applied to the polyglot-platform
// fixture. The target is 2: one per language (Python + GraphQL).
// GraphQL field sub-entities (Order.id, Order.status, Order.totalCents) are
// NOT counted here as they have names like "Order.id", not "Order".
func TestOrderDedup_TotalCanonicalOrderCount(t *testing.T) {
	pyContent, err := os.ReadFile(polyglotPlatformBase + "/libs/py-shared/py_shared/models.py")
	if err != nil {
		t.Skip("polyglot-platform not found:", err)
	}
	gqlContent, err := os.ReadFile(polyglotPlatformBase + "/services/search-graphql/src/schema.graphql")
	if err != nil {
		t.Skip("polyglot-platform not found:", err)
	}

	type extractorCase struct {
		key     string
		lang    string
		content []byte
		path    string
	}
	cases := []extractorCase{
		{"python", "python", pyContent, "libs/py-shared/py_shared/models.py"},
		{"python_fastapi", "python", pyContent, "libs/py-shared/py_shared/models.py"},
		{"python_sqlalchemy", "python", pyContent, "libs/py-shared/py_shared/models.py"},
		{"graphql", "graphql", gqlContent, "services/search-graphql/src/schema.graphql"},
	}

	var canonicalOrder int
	for _, tc := range cases {
		ext, ok := extractor.Get(tc.key)
		if !ok {
			t.Fatalf("extractor %q not registered", tc.key)
		}
		ents, err := ext.Extract(context.Background(), extractor.FileInput{
			Path:     tc.path,
			Content:  tc.content,
			Language: tc.lang,
		})
		if err != nil {
			t.Fatalf("%s.Extract: %v", tc.key, err)
		}
		for _, e := range ents {
			if e.Name == "Order" {
				canonicalOrder++
			}
		}
	}

	// Expected: 2 canonical "Order" nodes (Python SCOPE.Component/class + GraphQL SCOPE.Schema/type).
	// Before fix: 4 (Python + FastAPI + SQLAlchemy + GraphQL).
	if canonicalOrder != 2 {
		t.Errorf("expected 2 canonical Order entities across all extractors (issue #1501), got %d", canonicalOrder)
	}
}
