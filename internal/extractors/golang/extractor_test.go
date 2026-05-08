package golang_test

import (
	"context"
	"os"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsgo "github.com/smacker/go-tree-sitter/golang"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/golang" // trigger init()
	"github.com/cajasmota/archigraph/internal/types"
)

// ---- helpers ----------------------------------------------------------------

func parseGo(src []byte) *sitter.Tree {
	p := sitter.NewParser()
	p.SetLanguage(tsgo.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		panic("test helper: parse failed: " + err.Error())
	}
	return tree
}

func extractFrom(src string) ([]interface{}, error) {
	return extractFromPath(src, "test.go")
}

func extractFromPath(src, path string) ([]interface{}, error) {
	content := []byte(src)
	tree := parseGo(content)
	ext, ok := extractor.Get("go")
	if !ok {
		panic("go extractor not registered")
	}
	records, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  content,
		Language: "go",
		Tree:     tree,
	})
	if err != nil {
		return nil, err
	}
	out := make([]interface{}, len(records))
	for i, r := range records {
		out[i] = r
	}
	return out, nil
}

// ---- happy path tests -------------------------------------------------------

func TestExtractFunction(t *testing.T) {
	src := `package main

func hello(name string) string {
	return "Hello, " + name
}
`
	records, err := extractFrom(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(records))
	}
}

func TestExtractFunctionKindAndSubtype(t *testing.T) {
	src := `package main

func Compute(x int) int {
	return x * 2
}
`
	records, err := extractFrom(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(records))
	}

	// Access via extractor.FileInput return type
	ext, _ := extractor.Get("go")
	results, _ := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.go",
		Content:  []byte(src),
		Language: "go",
		Tree:     parseGo([]byte(src)),
	})

	r := results[0]
	if r.Kind != "SCOPE.Operation" {
		t.Errorf("expected Kind=SCOPE.Operation, got %q", r.Kind)
	}
	if r.Subtype != "function" {
		t.Errorf("expected Subtype=function, got %q", r.Subtype)
	}
	if r.Name != "Compute" {
		t.Errorf("expected Name=Compute, got %q", r.Name)
	}
}

func TestExtractMethodWithReceiver(t *testing.T) {
	src := `package main

type Store struct{}

func (s *Store) Save(item string) error {
	return nil
}
`
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.go",
		Content:  []byte(src),
		Language: "go",
		Tree:     parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have struct + method
	if len(results) != 2 {
		t.Fatalf("expected 2 entities (struct + method), got %d", len(results))
	}

	// Find the method
	var method *interface{}
	for i := range results {
		if results[i].Subtype == "method" {
			v := interface{}(results[i])
			method = &v
		}
	}
	if method == nil {
		t.Fatal("method entity not found")
	}

	r := results[0]
	if r.Subtype == "method" {
		// found at index 0
		if r.Kind != "SCOPE.Operation" {
			t.Errorf("method kind: expected SCOPE.Operation, got %q", r.Kind)
		}
		if r.Name != "Save" {
			t.Errorf("method name: expected Save, got %q", r.Name)
		}
		recv, _ := r.Metadata["receiver"].(string)
		if recv != "Store" {
			t.Errorf("receiver: expected Store, got %q", recv)
		}
		if r.QualifiedName != "Store.Save" {
			t.Errorf("qualified_name: expected Store.Save, got %q", r.QualifiedName)
		}
	} else {
		// method is at index 1
		r2 := results[1]
		if r2.Kind != "SCOPE.Operation" {
			t.Errorf("method kind: expected SCOPE.Operation, got %q", r2.Kind)
		}
		if r2.Name != "Save" {
			t.Errorf("method name: expected Save, got %q", r2.Name)
		}
		recv, _ := r2.Metadata["receiver"].(string)
		if recv != "Store" {
			t.Errorf("receiver: expected Store, got %q", recv)
		}
		if r2.QualifiedName != "Store.Save" {
			t.Errorf("qualified_name: expected Store.Save, got %q", r2.QualifiedName)
		}
	}
}

func TestExtractMethodQualifiedName(t *testing.T) {
	src := `package main

type Server struct{}

func (srv *Server) Run() error {
	return nil
}
`
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "server.go",
		Content:  []byte(src),
		Language: "go",
		Tree:     parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range results {
		if r.Subtype == "method" {
			if !strings.HasPrefix(r.QualifiedName, "Server.") {
				t.Errorf("expected qualified_name to start with Server., got %q", r.QualifiedName)
			}
			return
		}
	}
	t.Fatal("no method entity found")
}

func TestExtractStruct(t *testing.T) {
	src := `package main

type User struct {
	ID   int
	Name string
}
`
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.go",
		Content:  []byte(src),
		Language: "go",
		Tree:     parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(results))
	}
	r := results[0]
	if r.Kind != "SCOPE.Component" {
		t.Errorf("expected Kind=SCOPE.Component, got %q", r.Kind)
	}
	if r.Subtype != "struct" {
		t.Errorf("expected Subtype=struct, got %q", r.Subtype)
	}
	if r.Name != "User" {
		t.Errorf("expected Name=User, got %q", r.Name)
	}
}

func TestExtractInterface(t *testing.T) {
	src := `package main

type Reader interface {
	Read(p []byte) (int, error)
}
`
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.go",
		Content:  []byte(src),
		Language: "go",
		Tree:     parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(results))
	}
	r := results[0]
	if r.Kind != "SCOPE.Component" {
		t.Errorf("expected Kind=SCOPE.Component, got %q", r.Kind)
	}
	if r.Subtype != "interface" {
		t.Errorf("expected Subtype=interface, got %q", r.Subtype)
	}
	if r.Name != "Reader" {
		t.Errorf("expected Name=Reader, got %q", r.Name)
	}
}

func TestExtractFunctionAndStruct(t *testing.T) {
	src := `package main

type Config struct {
	Host string
	Port int
}

func NewConfig(host string, port int) *Config {
	return &Config{Host: host, Port: port}
}
`
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "config.go",
		Content:  []byte(src),
		Language: "go",
		Tree:     parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 entities (struct + function), got %d", len(results))
	}

	kinds := map[string]bool{}
	for _, r := range results {
		kinds[r.Kind] = true
	}
	// MX-1093 (revert MX-1045): Config struct must emit SCOPE.Component; NewConfig emits SCOPE.Operation.
	if !kinds["SCOPE.Component"] || !kinds["SCOPE.Operation"] {
		t.Errorf("expected both SCOPE.Component and SCOPE.Operation, got kinds: %v", kinds)
	}
}

// ---- empty file tests -------------------------------------------------------

func TestExtractEmptyFile(t *testing.T) {
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.go",
		Content:  []byte{},
		Language: "go",
	})
	if err != nil {
		t.Fatalf("unexpected error on empty file: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d", len(results))
	}
}

func TestExtractPackageDeclarationOnly(t *testing.T) {
	src := `package main
`
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "pkg.go",
		Content:  []byte(src),
		Language: "go",
		Tree:     parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 entities for package-only file, got %d", len(results))
	}
}

// ---- error path tests -------------------------------------------------------

func TestExtractMalformedGoFile(t *testing.T) {
	// Malformed Go — syntax errors present but parser is fault-tolerant.
	// Extractor must return whatever it can parse, not panic.
	src := `package main

func broken( {
	// syntax error
`
	ext, _ := extractor.Get("go")
	// Should not panic; may return 0 or 1 entities depending on error recovery.
	_, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "broken.go",
		Content:  []byte(src),
		Language: "go",
		Tree:     parseGo([]byte(src)),
	})
	// err must be nil (partial results, no crash)
	if err != nil {
		t.Errorf("expected nil error on malformed Go file, got: %v", err)
	}
}

func TestExtractBinaryContentDoesNotPanic(t *testing.T) {
	// Binary content misclassified as Go — must not panic.
	binary := make([]byte, 512)
	for i := range binary {
		binary[i] = byte(i % 256)
	}
	ext, _ := extractor.Get("go")
	_, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "binary.go",
		Content:  binary,
		Language: "go",
	})
	// No panic and no crash required — error is acceptable.
	_ = err
}

func TestExtractLargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}
	// Generate a file > 1MB with many functions.
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	// Each func body is ~60 bytes; need ~16700 funcs for 1MB.
	for i := range 20000 {
		sb.WriteString("func ")
		// Use integer formatted name
		name := "fn_"
		n := i
		if n == 0 {
			name += "0"
		} else {
			digits := []byte{}
			for n > 0 {
				digits = append([]byte{byte('0' + n%10)}, digits...)
				n /= 10
			}
			name += string(digits)
		}
		sb.WriteString(name)
		sb.WriteString("() {}\n")
	}
	content := []byte(sb.String())
	if len(content) < 1<<20 {
		t.Logf("note: generated %d bytes, target is 1MB", len(content))
	}

	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "large.go",
		Content:  content,
		Language: "go",
	})
	if err != nil {
		t.Fatalf("large file extraction failed: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected entities from large file")
	}
}

// ---- import relationship tests ----------------------------------------------

func TestExtractImportRelationships(t *testing.T) {
	src := `package main

import (
	"fmt"
	"net/http"
)

func main() {
	fmt.Println("hello")
}
`
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "main.go",
		Content:  []byte(src),
		Language: "go",
		Tree:     parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// At least one entity (main function) with IMPORTS relationships.
	if len(results) == 0 {
		t.Fatal("expected at least one entity")
	}

	var found bool
	for _, r := range results {
		for _, rel := range r.Relationships {
			if rel.Kind == "IMPORTS" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected IMPORTS relationships")
	}
}

// ---- source_file tests ------------------------------------------------------

func TestExtractSourceFileSet(t *testing.T) {
	src := `package main

func hello() {}
`
	path := "pkg/handler.go"
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "go",
		Tree:     parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one entity")
	}
	for _, r := range results {
		if r.SourceFile != path {
			t.Errorf("SourceFile: expected %q, got %q", path, r.SourceFile)
		}
	}
}

// ---- golden fixture test ----------------------------------------------------

func TestExtractGoldenFixture(t *testing.T) {
	fixturePath := "../../../fixtures/sources/go/sample_handler.go"
	content, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Skipf("golden fixture not found: %v", err)
	}

	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "fixtures/sources/go/sample_handler.go",
		Content:  content,
		Language: "go",
		Tree:     parseGo(content),
	})
	if err != nil {
		t.Fatalf("unexpected error on golden fixture: %v", err)
	}

	// We expect functions: main, handleGetUser, handleHealth, NewUserStore
	// methods: GetUser, SaveUser
	// structs: User, UserStore
	minExpected := 8
	if len(results) < minExpected {
		t.Errorf("expected at least %d entities from golden fixture, got %d", minExpected, len(results))
	}

	// Verify specific entities exist.
	names := make(map[string]bool)
	for _, r := range results {
		names[r.Name] = true
	}
	for _, want := range []string{"main", "User", "UserStore", "NewUserStore", "GetUser", "SaveUser"} {
		if !names[want] {
			t.Errorf("expected entity %q not found in extraction results", want)
		}
	}
}

// ---- import path extraction tests (MX-1046) ---------------------------------
//
// These tests lock the contract that import entity Name is the full module
// path exactly as it appears in the import statement — never truncated to the
// last path segment. Every Go import form is covered.

// collectImports filters Extract results down to import SCOPE.Component entries.
// Import entities are the only SCOPE.Component records with a non-empty
// Relationships slice whose first entry is IMPORTS.
func collectImports(results []interface{}) []map[string]interface{} {
	var imports []map[string]interface{}
	for _, raw := range results {
		r, ok := raw.(types.EntityRecord)
		if !ok {
			continue
		}
		if r.Kind != "SCOPE.Component" || len(r.Relationships) == 0 {
			continue
		}
		if r.Relationships[0].Kind != "IMPORTS" {
			continue
		}
		entry := map[string]interface{}{
			"name":     r.Name,
			"metadata": r.Metadata,
			"rel_from": r.Relationships[0].FromID,
			"rel_to":   r.Relationships[0].ToID,
		}
		imports = append(imports, entry)
	}
	return imports
}

func importByName(imports []map[string]interface{}, name string) map[string]interface{} {
	for _, imp := range imports {
		if imp["name"] == name {
			return imp
		}
	}
	return nil
}

func TestImportExtraction_StandardLibrary(t *testing.T) {
	src := `package main

import "fmt"
`
	results, err := extractFrom(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	imports := collectImports(results)
	imp := importByName(imports, "fmt")
	if imp == nil {
		t.Fatalf("expected import %q, got %+v", "fmt", imports)
	}
	meta := imp["metadata"].(map[string]interface{})
	if meta["import_type"] != "standard" {
		t.Errorf("import_type: expected %q, got %v", "standard", meta["import_type"])
	}
	if _, ok := meta["alias"]; ok {
		t.Errorf("standard import must not carry alias metadata, got %v", meta["alias"])
	}
}

func TestImportExtraction_NestedStandardLibrary(t *testing.T) {
	src := `package main

import "os/exec"
`
	results, _ := extractFrom(src)
	imports := collectImports(results)
	imp := importByName(imports, "os/exec")
	if imp == nil {
		t.Fatalf("expected nested stdlib import %q, got %+v", "os/exec", imports)
	}
	if imp["rel_to"] != "os/exec" {
		t.Errorf("relationship ToID: expected %q, got %q", "os/exec", imp["rel_to"])
	}
}

func TestImportExtraction_FullModulePath(t *testing.T) {
	// The headline case from MX-1046: the full github.com/... path must be
	// preserved. The bug was splitting on "/" and keeping only "v1".
	src := `package main

import "github.com/example.com/proto/v1"
`
	results, _ := extractFrom(src)
	imports := collectImports(results)

	want := "github.com/example.com/proto/v1"
	if imp := importByName(imports, want); imp == nil {
		t.Fatalf("expected import %q (full path), got %+v", want, imports)
	}
	// Truncated name MUST NOT be present.
	if imp := importByName(imports, "v1"); imp != nil {
		t.Fatalf("import extractor regressed — found truncated name %q", "v1")
	}
}

func TestImportExtraction_AliasedImport(t *testing.T) {
	src := `package main

import neo "github.com/neo4j/neo4j-go-driver/v5/neo4j"
`
	results, _ := extractFrom(src)
	imports := collectImports(results)

	want := "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	imp := importByName(imports, want)
	if imp == nil {
		t.Fatalf("expected aliased import %q (full path), got %+v", want, imports)
	}
	meta := imp["metadata"].(map[string]interface{})
	if meta["import_type"] != "aliased" {
		t.Errorf("import_type: expected %q, got %v", "aliased", meta["import_type"])
	}
	if meta["alias"] != "neo" {
		t.Errorf("alias: expected %q, got %v", "neo", meta["alias"])
	}
	// Alias must NOT be used as Name.
	if importByName(imports, "neo") != nil {
		t.Errorf("alias %q must not appear as Name", "neo")
	}
}

func TestImportExtraction_DotImport(t *testing.T) {
	src := `package main

import . "errors"
`
	results, _ := extractFrom(src)
	imports := collectImports(results)

	imp := importByName(imports, "errors")
	if imp == nil {
		t.Fatalf("expected dot import %q, got %+v", "errors", imports)
	}
	meta := imp["metadata"].(map[string]interface{})
	if meta["import_type"] != "dot" {
		t.Errorf("import_type: expected %q, got %v", "dot", meta["import_type"])
	}
	if _, ok := meta["alias"]; ok {
		t.Errorf("dot import must not carry alias metadata, got %v", meta["alias"])
	}
}

func TestImportExtraction_BlankImport(t *testing.T) {
	src := `package main

import _ "github.com/lib/pq"
`
	results, _ := extractFrom(src)
	imports := collectImports(results)

	want := "github.com/lib/pq"
	imp := importByName(imports, want)
	if imp == nil {
		t.Fatalf("expected blank import %q, got %+v", want, imports)
	}
	meta := imp["metadata"].(map[string]interface{})
	if meta["import_type"] != "blank" {
		t.Errorf("import_type: expected %q, got %v", "blank", meta["import_type"])
	}
	if _, ok := meta["alias"]; ok {
		t.Errorf("blank import must not carry alias metadata, got %v", meta["alias"])
	}
}

func TestImportExtraction_GroupedImports(t *testing.T) {
	// Covers all four forms simultaneously to guard against cross-contamination
	// between sibling import_spec nodes inside a single import_declaration.
	src := `package main

import (
	"fmt"
	"os/exec"
	"github.com/example.com/proto/v1"
	neo "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	_ "github.com/lib/pq"
	. "errors"
)

func main() {}
`
	results, _ := extractFrom(src)
	imports := collectImports(results)

	// All six imports must be present with full paths.
	wantNames := []string{
		"fmt",
		"os/exec",
		"github.com/example.com/proto/v1",
		"github.com/neo4j/neo4j-go-driver/v5/neo4j",
		"github.com/lib/pq",
		"errors",
	}
	for _, want := range wantNames {
		if importByName(imports, want) == nil {
			t.Errorf("expected import %q, got %d imports: %+v", want, len(imports), imports)
		}
	}
	// Per-entry import_type correctness.
	cases := map[string]string{
		"fmt":                             "standard",
		"os/exec":                         "standard",
		"github.com/example.com/proto/v1": "standard",
		"github.com/neo4j/neo4j-go-driver/v5/neo4j": "aliased",
		"github.com/lib/pq":                         "blank",
		"errors":                                    "dot",
	}
	for name, wantType := range cases {
		imp := importByName(imports, name)
		if imp == nil {
			continue
		}
		meta := imp["metadata"].(map[string]interface{})
		if meta["import_type"] != wantType {
			t.Errorf("%s: import_type = %v, want %q", name, meta["import_type"], wantType)
		}
	}
	// Aliased import must carry the alias in metadata.
	if imp := importByName(imports, "github.com/neo4j/neo4j-go-driver/v5/neo4j"); imp != nil {
		meta := imp["metadata"].(map[string]interface{})
		if meta["alias"] != "neo" {
			t.Errorf("aliased import alias = %v, want %q", meta["alias"], "neo")
		}
	}
}

func TestImportExtraction_RelationshipFromAndTo(t *testing.T) {
	src := `package main

import "github.com/example.com/proto/v1"
`
	results, err := extractFromPath(src, "internal/server/sample.go")
	if err != nil {
		t.Fatal(err)
	}
	imports := collectImports(results)
	want := "github.com/example.com/proto/v1"
	imp := importByName(imports, want)
	if imp == nil {
		t.Fatalf("expected import %q", want)
	}
	if imp["rel_from"] != "internal/server/sample.go" {
		t.Errorf("FromID: expected %q, got %q", "internal/server/sample.go", imp["rel_from"])
	}
	if imp["rel_to"] != want {
		t.Errorf("ToID: expected %q, got %q", want, imp["rel_to"])
	}
}

func TestImportExtraction_NoImports(t *testing.T) {
	src := `package main

func main() {}
`
	results, _ := extractFrom(src)
	imports := collectImports(results)
	if len(imports) != 0 {
		t.Errorf("expected zero import entities, got %d: %+v", len(imports), imports)
	}
}

func TestImportExtraction_DoesNotPanicOnEmptyImportBlock(t *testing.T) {
	// Empty `import ()` block is syntactically valid. Must not panic.
	src := `package main

import (
)

func main() {}
`
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("extractor panicked on empty import block: %v", r)
		}
	}()
	if _, err := extractFrom(src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---- MX-1043 relationship tests --------------------------------------------
//
// These tests lock the contract that the Go language extractor emits
// RelationshipRecord values for CALLS, IMPORTS, DEPENDS_ON, and IMPLEMENTS
// edges. The emission is AST-based and intra-file.

// extractRecords returns []types.EntityRecord directly, bypassing the
// []interface{} conversion used by older tests. Eliminates boilerplate type
// assertions when a test needs to inspect Relationships.
func extractRecords(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	content := []byte(src)
	tree := parseGo(content)
	ext, _ := extractor.Get("go")
	records, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  content,
		Language: "go",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return records
}

// findEntity returns the first EntityRecord matching name, or nil.
func findEntity(records []types.EntityRecord, name string) *types.EntityRecord {
	for i := range records {
		if records[i].Name == name {
			return &records[i]
		}
	}
	return nil
}

// hasRelationship reports whether rec has any Relationship matching kind and
// to-target. The from-target is implied by the record's own Name.
func hasRelationship(rec *types.EntityRecord, kind, to string) bool {
	if rec == nil {
		return false
	}
	for _, r := range rec.Relationships {
		if r.Kind == kind && r.ToID == to {
			return true
		}
	}
	return false
}

// ---- CALLS ------------------------------------------------------------------

func TestCallsRelationship_LocalFunctionCall(t *testing.T) {
	src := `package main

func helper() int { return 42 }

func main() {
	_ = helper()
}
`
	records := extractRecords(t, src, "main.go")
	main := findEntity(records, "main")
	if main == nil {
		t.Fatal("main function not found")
	}
	if !hasRelationship(main, "CALLS", "helper") {
		t.Errorf("expected CALLS → helper on main, got %+v", main.Relationships)
	}
}

func TestCallsRelationship_SelectorCallUsesBareName(t *testing.T) {
	src := `package main

import "fmt"

func greet() {
	fmt.Println("hi")
}
`
	records := extractRecords(t, src, "main.go")
	greet := findEntity(records, "greet")
	if greet == nil {
		t.Fatal("greet function not found")
	}
	if !hasRelationship(greet, "CALLS", "Println") {
		t.Errorf("expected CALLS → Println on greet, got %+v", greet.Relationships)
	}
}

func TestCallsRelationship_DedupByTarget(t *testing.T) {
	src := `package main

import "fmt"

func loud() {
	fmt.Println("a")
	fmt.Println("b")
	fmt.Println("c")
}
`
	records := extractRecords(t, src, "main.go")
	loud := findEntity(records, "loud")
	if loud == nil {
		t.Fatal("loud function not found")
	}
	count := 0
	for _, r := range loud.Relationships {
		if r.Kind == "CALLS" && r.ToID == "Println" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 deduplicated CALLS → Println, got %d: %+v", count, loud.Relationships)
	}
}

func TestCallsRelationship_SkipSelfRecursion(t *testing.T) {
	src := `package main

func fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}
`
	records := extractRecords(t, src, "main.go")
	fact := findEntity(records, "fact")
	if fact == nil {
		t.Fatal("fact function not found")
	}
	if hasRelationship(fact, "CALLS", "fact") {
		t.Errorf("self-recursive call must be dropped: %+v", fact.Relationships)
	}
}

func TestCallsRelationship_UnknownExternalCall(t *testing.T) {
	// MX-1043 rule #6: unknown/external callees are emitted with the bare
	// function name rather than being dropped.
	src := `package main

func caller() {
	mystery()
}
`
	records := extractRecords(t, src, "main.go")
	caller := findEntity(records, "caller")
	if caller == nil {
		t.Fatal("caller function not found")
	}
	if !hasRelationship(caller, "CALLS", "mystery") {
		t.Errorf("expected CALLS → mystery (unknown target), got %+v", caller.Relationships)
	}
}

func TestCallsRelationship_NoBodyNoEdges(t *testing.T) {
	src := `package main

type Shape interface {
	Area() float64
}
`
	records := extractRecords(t, src, "shape.go")
	// Interface has no function_declaration/method_declaration children,
	// so there should be no CALLS edges anywhere.
	for _, r := range records {
		for _, rel := range r.Relationships {
			if rel.Kind == "CALLS" {
				t.Errorf("unexpected CALLS edge on %s: %+v", r.Name, rel)
			}
		}
	}
}

// ---- IMPORTS ----------------------------------------------------------------

func TestImportsRelationship_KindIsIMPORTS(t *testing.T) {
	src := `package main

import "fmt"

func main() { fmt.Println("x") }
`
	records := extractRecords(t, src, "main.go")
	var found bool
	for _, r := range records {
		for _, rel := range r.Relationships {
			if rel.Kind == "IMPORTS" && rel.ToID == "fmt" && rel.FromID == "main.go" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected IMPORTS relationship with FromID=main.go ToID=fmt")
	}
}

// ---- DEPENDS_ON: method → receiver -----------------------------------------

func TestDependsOnRelationship_MethodToReceiver(t *testing.T) {
	src := `package main

type Store struct{}

func (s *Store) Save() error { return nil }
`
	records := extractRecords(t, src, "store.go")
	save := findEntity(records, "Save")
	if save == nil {
		t.Fatal("Save method not found")
	}
	if !hasRelationship(save, "DEPENDS_ON", "Store") {
		t.Errorf("expected DEPENDS_ON → Store on Save method, got %+v", save.Relationships)
	}
}

func TestDependsOnRelationship_FunctionHasNoReceiverEdge(t *testing.T) {
	// Plain functions (no receiver) must not emit DEPENDS_ON edges to
	// anything other than what their body references.
	src := `package main

func plain() {}
`
	records := extractRecords(t, src, "plain.go")
	plain := findEntity(records, "plain")
	if plain == nil {
		t.Fatal("plain function not found")
	}
	for _, r := range plain.Relationships {
		if r.Kind == "DEPENDS_ON" {
			t.Errorf("plain function must not have DEPENDS_ON edges, got %+v", r)
		}
	}
}

// ---- DEPENDS_ON: struct field types ----------------------------------------

func TestDependsOnRelationship_StructFieldReferencesOtherStruct(t *testing.T) {
	src := `package main

type Inner struct{}

type Outer struct {
	inner *Inner
}
`
	records := extractRecords(t, src, "shapes.go")
	outer := findEntity(records, "Outer")
	if outer == nil {
		t.Fatal("Outer struct not found")
	}
	if !hasRelationship(outer, "DEPENDS_ON", "Inner") {
		t.Errorf("expected DEPENDS_ON → Inner on Outer, got %+v", outer.Relationships)
	}
}

func TestDependsOnRelationship_StructFieldNoEdgeForPrimitives(t *testing.T) {
	// Primitives (int, string) must not produce DEPENDS_ON edges, only
	// references to other file-declared types.
	src := `package main

type Counter struct {
	n int
	s string
}
`
	records := extractRecords(t, src, "counter.go")
	counter := findEntity(records, "Counter")
	if counter == nil {
		t.Fatal("Counter struct not found")
	}
	for _, r := range counter.Relationships {
		if r.Kind == "DEPENDS_ON" {
			t.Errorf("primitive field must not produce DEPENDS_ON: %+v", r)
		}
	}
}

func TestDependsOnRelationship_StructFieldNoEdgeForExternalType(t *testing.T) {
	// External types (not declared in this file) must be filtered out.
	// Otherwise edges to unresolved package identifiers pollute the graph.
	src := `package main

import "net/http"

type Handler struct {
	w http.ResponseWriter
}
`
	records := extractRecords(t, src, "handler.go")
	handler := findEntity(records, "Handler")
	if handler == nil {
		t.Fatal("Handler struct not found")
	}
	for _, r := range handler.Relationships {
		if r.Kind == "DEPENDS_ON" && r.ToID == "ResponseWriter" {
			t.Errorf("external type must not produce DEPENDS_ON: %+v", r)
		}
	}
}

// ---- IMPLEMENTS -------------------------------------------------------------

func TestImplementsRelationship_StructImplementsSameFileInterface(t *testing.T) {
	src := `package main

type Greeter interface {
	Hello() string
}

type Friendly struct{}

func (f *Friendly) Hello() string { return "hi" }
`
	records := extractRecords(t, src, "greet.go")
	friendly := findEntity(records, "Friendly")
	if friendly == nil {
		t.Fatal("Friendly struct not found")
	}
	if !hasRelationship(friendly, "IMPLEMENTS", "Greeter") {
		t.Errorf("expected IMPLEMENTS → Greeter on Friendly, got %+v", friendly.Relationships)
	}
}

func TestImplementsRelationship_NoEdgeWhenMethodSetIncomplete(t *testing.T) {
	src := `package main

type Writer interface {
	Write(p []byte) (int, error)
	Close() error
}

type Partial struct{}

func (p *Partial) Write(data []byte) (int, error) { return 0, nil }
// no Close — must NOT implement Writer
`
	records := extractRecords(t, src, "partial.go")
	partial := findEntity(records, "Partial")
	if partial == nil {
		t.Fatal("Partial struct not found")
	}
	if hasRelationship(partial, "IMPLEMENTS", "Writer") {
		t.Errorf("Partial must not implement Writer (missing Close): %+v", partial.Relationships)
	}
}

func TestImplementsRelationship_SkipsEmptyInterface(t *testing.T) {
	// An empty interface is implemented by every type. Emitting edges for
	// it would cause N^2 blow-up in files containing `interface{}` aliases
	// or marker interfaces.
	src := `package main

type Any interface{}

type Thing struct{}

func (t *Thing) Do() {}
`
	records := extractRecords(t, src, "any.go")
	thing := findEntity(records, "Thing")
	if thing == nil {
		t.Fatal("Thing struct not found")
	}
	if hasRelationship(thing, "IMPLEMENTS", "Any") {
		t.Error("must not emit IMPLEMENTS edge to empty interface")
	}
}

func TestImplementsRelationship_MultipleInterfaces(t *testing.T) {
	src := `package main

type Reader interface {
	Read() string
}

type Closer interface {
	Close() error
}

type File struct{}

func (f *File) Read() string { return "" }
func (f *File) Close() error { return nil }
`
	records := extractRecords(t, src, "file.go")
	file := findEntity(records, "File")
	if file == nil {
		t.Fatal("File struct not found")
	}
	if !hasRelationship(file, "IMPLEMENTS", "Reader") {
		t.Errorf("expected IMPLEMENTS → Reader, got %+v", file.Relationships)
	}
	if !hasRelationship(file, "IMPLEMENTS", "Closer") {
		t.Errorf("expected IMPLEMENTS → Closer, got %+v", file.Relationships)
	}
}

func TestImplementsRelationship_StructWithNoMethodsNoEdges(t *testing.T) {
	src := `package main

type Named interface {
	Name() string
}

type Empty struct{}
`
	records := extractRecords(t, src, "empty.go")
	empty := findEntity(records, "Empty")
	if empty == nil {
		t.Fatal("Empty struct not found")
	}
	for _, r := range empty.Relationships {
		if r.Kind == "IMPLEMENTS" {
			t.Errorf("struct with no methods must not emit IMPLEMENTS: %+v", r)
		}
	}
}

// ---- full mix — golden fixture assertion -----------------------------------

func TestRelationships_SampleHandlerFixture(t *testing.T) {
	// End-to-end check against the MX-1043 golden fixture. Verifies every
	// relationship kind fires at least once on a realistic file.
	fixturePath := "../../../fixtures/sources/go/sample_handler.go"
	content, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Skipf("golden fixture not found: %v", err)
	}
	ext, _ := extractor.Get("go")
	records, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "fixtures/sources/go/sample_handler.go",
		Content:  content,
		Language: "go",
		Tree:     parseGo(content),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	kinds := make(map[string]int)
	for _, r := range records {
		for _, rel := range r.Relationships {
			kinds[rel.Kind]++
		}
	}

	for _, wantKind := range []string{"CALLS", "IMPORTS", "DEPENDS_ON"} {
		if kinds[wantKind] == 0 {
			t.Errorf("expected at least one %s edge in sample_handler fixture, got kinds=%v", wantKind, kinds)
		}
	}
}
