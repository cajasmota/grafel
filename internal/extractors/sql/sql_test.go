package sql_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/sql"
	"github.com/cajasmota/archigraph/internal/types"
)

// repoRootSQL walks parent directories to find the go.mod root.
func repoRootSQL(t *testing.T) string {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

func extractSQLContent(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("sql")
	if !ok {
		t.Fatal("sql extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "sql",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return entities
}

func TestSQLExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("sql")
	if !ok {
		t.Fatal("sql extractor not registered")
	}
}

func TestSQLExtractor_CreateTable(t *testing.T) {
	src := `CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE posts (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    title TEXT NOT NULL
);
`
	ext, _ := extractor.Get("sql")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "schema.sql",
		Content:  []byte(src),
		Language: "sql",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tables := make(map[string]bool)
	for _, e := range entities {
		if e.Subtype == "table" {
			tables[e.Name] = true
			if e.Kind != "SCOPE.Datastore" {
				t.Errorf("entity %q: expected Kind=SCOPE.Datastore, got %q", e.Name, e.Kind)
			}
		}
	}
	for _, want := range []string{"users", "posts"} {
		if !tables[want] {
			t.Errorf("expected table %q to be extracted", want)
		}
	}
}

func TestSQLExtractor_CreateView(t *testing.T) {
	src := `CREATE VIEW active_users AS
    SELECT * FROM users WHERE active = true;

CREATE OR REPLACE VIEW user_stats AS
    SELECT user_id, COUNT(*) as post_count FROM posts GROUP BY user_id;
`
	ext, _ := extractor.Get("sql")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "views.sql",
		Content:  []byte(src),
		Language: "sql",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	views := make(map[string]bool)
	for _, e := range entities {
		if e.Subtype == "view" {
			views[e.Name] = true
		}
	}
	for _, want := range []string{"active_users", "user_stats"} {
		if !views[want] {
			t.Errorf("expected view %q to be extracted", want)
		}
	}
}

func TestSQLExtractor_CreateIndex(t *testing.T) {
	src := `CREATE INDEX idx_users_email ON users (email);
CREATE UNIQUE INDEX idx_users_username ON users (username);
`
	ext, _ := extractor.Get("sql")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "indexes.sql",
		Content:  []byte(src),
		Language: "sql",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	indexes := make(map[string]bool)
	for _, e := range entities {
		if e.Subtype == "index" {
			indexes[e.Name] = true
		}
	}
	for _, want := range []string{"idx_users_email", "idx_users_username"} {
		if !indexes[want] {
			t.Errorf("expected index %q to be extracted", want)
		}
	}
}

func TestSQLExtractor_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("sql")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.sql",
		Content:  []byte{},
		Language: "sql",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestSQLExtractor_CaseInsensitive(t *testing.T) {
	src := `create table products (id int);
CREATE TABLE orders (id int);
`
	ext, _ := extractor.Get("sql")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "mixed.sql",
		Content:  []byte(src),
		Language: "sql",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tables := make(map[string]bool)
	for _, e := range entities {
		if e.Subtype == "table" {
			tables[e.Name] = true
		}
	}
	for _, want := range []string{"products", "orders"} {
		if !tables[want] {
			t.Errorf("expected table %q to be extracted (case-insensitive)", want)
		}
	}
}

// ---------------------------------------------------------------------------
// Acceptance criteria: dbt model extraction
// ---------------------------------------------------------------------------

const dbtModelFixture = `{{
  config(
    materialized='incremental',
    schema='marts',
    unique_key='order_id'
  )
}}

with source_orders as (
  select * from {{ source('raw', 'orders') }}
),

stg_customers as (
  select * from {{ ref('stg_customers') }}
),

stg_payments as (
  select * from {{ ref('stg_payments') }}
)

select * from source_orders
`

func TestMX1059_Dbt_AtLeast3Entities(t *testing.T) {
	entities := extractSQLContent(t, dbtModelFixture, "models/orders.sql")
	if len(entities) < 3 {
		t.Errorf("AC5: expected ≥3 entities for dbt model, got %d", len(entities))
		for _, e := range entities {
			t.Logf("  [%s/%s] %s", e.Kind, e.Subtype, e.Name)
		}
	}
}

func TestMX1059_Dbt_Ref_IsComponent(t *testing.T) {
	entities := extractSQLContent(t, dbtModelFixture, "models/orders.sql")
	var refs []types.EntityRecord
	for _, e := range entities {
		if e.Subtype == "dbt_ref" {
			refs = append(refs, e)
		}
	}
	if len(refs) < 2 {
		t.Errorf("AC5: expected ≥2 dbt_ref entities, got %d", len(refs))
	}
	for _, r := range refs {
		if r.Kind != "SCOPE.Component" {
			t.Errorf("AC5: ref %q kind=%q, want SCOPE.Component", r.Name, r.Kind)
		}
	}
	// Specific ref names
	names := make(map[string]bool)
	for _, r := range refs {
		names[r.Name] = true
	}
	if !names["stg_customers"] {
		t.Error("AC5: expected dbt_ref 'stg_customers'")
	}
	if !names["stg_payments"] {
		t.Error("AC5: expected dbt_ref 'stg_payments'")
	}
}

func TestMX1059_Dbt_Source_IsDatastore(t *testing.T) {
	entities := extractSQLContent(t, dbtModelFixture, "models/orders.sql")
	var sources []types.EntityRecord
	for _, e := range entities {
		if e.Subtype == "dbt_source" {
			sources = append(sources, e)
		}
	}
	if len(sources) == 0 {
		t.Fatal("AC5: expected at least one dbt_source entity")
	}
	for _, s := range sources {
		if s.Kind != "SCOPE.Datastore" {
			t.Errorf("AC5: source %q kind=%q, want SCOPE.Datastore", s.Name, s.Kind)
		}
	}
	names := make(map[string]bool)
	for _, s := range sources {
		names[s.Name] = true
	}
	if !names["raw.orders"] {
		t.Error("AC5: expected dbt_source 'raw.orders'")
	}
}

func TestMX1059_Dbt_Config_IsComponent(t *testing.T) {
	entities := extractSQLContent(t, dbtModelFixture, "models/orders.sql")
	var configs []types.EntityRecord
	for _, e := range entities {
		if e.Subtype == "dbt_config" {
			configs = append(configs, e)
		}
	}
	if len(configs) == 0 {
		t.Fatal("AC5: expected at least one dbt_config entity")
	}
	for _, c := range configs {
		if c.Kind != "SCOPE.Component" {
			t.Errorf("AC5: config %q kind=%q, want SCOPE.Component", c.Name, c.Kind)
		}
	}
	names := make(map[string]bool)
	for _, c := range configs {
		names[c.Name] = true
	}
	if !names["materialized"] {
		t.Error("AC5: expected dbt_config 'materialized'")
	}
}

func TestMX1059_Dbt_QualityScore(t *testing.T) {
	entities := extractSQLContent(t, dbtModelFixture, "models/orders.sql")
	for _, e := range entities {
		if e.QualityScore < 0.6 {
			t.Errorf("AC5: entity %q (subtype=%q) quality_score=%.2f < 0.6", e.Name, e.Subtype, e.QualityScore)
		}
	}
}

func TestMX1059_Dbt_NonDbtSQL_NoJinjaEntities(t *testing.T) {
	// Regular SQL without Jinja should not produce dbt entities.
	src := `CREATE TABLE users (id INT, name TEXT);`
	entities := extractSQLContent(t, src, "schema.sql")
	for _, e := range entities {
		if e.Subtype == "dbt_ref" || e.Subtype == "dbt_source" || e.Subtype == "dbt_config" {
			t.Errorf("non-dbt SQL should not produce dbt entities, got %q (subtype=%q)", e.Name, e.Subtype)
		}
	}
}

func TestMX1059_Dbt_RealWorldFixture_AtLeast3Entities(t *testing.T) {
	root := repoRootSQL(t)
	src, err := os.ReadFile(filepath.Join(root, "testdata/fixtures/real-world/dbt/orders_model.sql"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ext, _ := extractor.Get("sql")
	entities, extractErr := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "models/orders_model.sql",
		Content:  src,
		Language: "sql",
	})
	if extractErr != nil {
		t.Fatalf("extract: %v", extractErr)
	}
	if len(entities) < 3 {
		t.Errorf("AC5 real-world: expected ≥3 entities, got %d", len(entities))
	}
}

func TestMX1059_Dbt_AllowlistCompliant(t *testing.T) {
	allowed := map[string]bool{
		"SCOPE.Service":       true,
		"SCOPE.Component":     true,
		"SCOPE.Operation":     true,
		"SCOPE.Pattern":       true,
		"SCOPE.Evolution":     true,
		"SCOPE.Datastore":     true,
		"SCOPE.ExternalAPI":   true,
		"SCOPE.Event":         true,
		"SCOPE.Queue":         true,
		"SCOPE.Schema":        true,
		"SCOPE.ScopeUnknown":  true,
		"SCOPE.Stylesheet":    true,
		"SCOPE.UIComponent":   true,
		"SCOPE.InfraResource": true,
	}
	entities := extractSQLContent(t, dbtModelFixture, "models/orders.sql")
	for _, e := range entities {
		if !allowed[e.Kind] {
			t.Errorf("entity %q has non-allowlisted kind %q", e.Name, e.Kind)
		}
	}
}
