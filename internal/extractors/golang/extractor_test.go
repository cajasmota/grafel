package golang_test

import (
	"context"
	"os"
	"strings"
	"testing"

	tsgo "github.com/smacker/go-tree-sitter/golang"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/golang" // trigger init()
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	"github.com/cajasmota/grafel/internal/types"
)

// ---- helpers ----------------------------------------------------------------

// parseGo parses Go source into the binding-agnostic ts.Tree via the smacker
// adapter. Tests stamp the result on FileInput.TSTree (which the Go extractor
// consumes). Using the smacker adapter keeps these tests linkable in the default
// build (no official runtime co-linked) while still exercising the ts façade.
func parseGo(src []byte) ts.Tree {
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsgo.GetLanguage()))
	if err != nil {
		panic("test helper: parser init failed: " + err.Error())
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
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
		TSTree:   tree,
	})
	if err != nil {
		return nil, err
	}
	records = stripFileEntity(records)
	out := make([]interface{}, len(records))
	for i, r := range records {
		out[i] = r
	}
	return out, nil
}

// stripFileEntity drops the file-level SCOPE.Component (subtype="file")
// entity that the extractor emits for every source file (#577) so legacy
// tests that count semantic entities (functions, structs, …) remain
// stable. Only the file-level entity is filtered.
func stripFileEntity(records []types.EntityRecord) []types.EntityRecord {
	out := records[:0:0]
	for _, e := range records {
		if e.Kind == "SCOPE.Component" && e.Subtype == "file" {
			continue
		}
		out = append(out, e)
	}
	return out
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
		TSTree:   parseGo([]byte(src)),
	})
	results = stripFileEntity(results)

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
		TSTree:   parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	results = stripFileEntity(results)
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
		if r.Name != "Store.Save" {
			t.Errorf("method name: expected Store.Save (issue #66), got %q", r.Name)
		}
		recv, _ := r.Metadata["receiver"].(string)
		if recv != "Store" {
			t.Errorf("receiver: expected Store, got %q", recv)
		}
		if r.QualifiedName != "" {
			t.Errorf("qualified_name: expected empty (issue #80), got %q", r.QualifiedName)
		}
	} else {
		// method is at index 1
		r2 := results[1]
		if r2.Kind != "SCOPE.Operation" {
			t.Errorf("method kind: expected SCOPE.Operation, got %q", r2.Kind)
		}
		if r2.Name != "Store.Save" {
			t.Errorf("method name: expected Store.Save (issue #66), got %q", r2.Name)
		}
		recv, _ := r2.Metadata["receiver"].(string)
		if recv != "Store" {
			t.Errorf("receiver: expected Store, got %q", recv)
		}
		if r2.QualifiedName != "" {
			t.Errorf("qualified_name: expected empty (issue #80), got %q", r2.QualifiedName)
		}
	}
}

// TestExtractMethodQualifiedNameEmpty verifies that Go methods leave
// QualifiedName empty (issue #80). The dotted Receiver.method form lives
// on Name (issue #66), so QualifiedName would be redundant.
func TestExtractMethodQualifiedNameEmpty(t *testing.T) {
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
		TSTree:   parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range results {
		if r.Subtype == "method" {
			if r.QualifiedName != "" {
				t.Errorf("expected qualified_name to be empty (issue #80), got %q", r.QualifiedName)
			}
			if !strings.HasPrefix(r.Name, "Server.") {
				t.Errorf("expected name to start with Server. (issue #66), got %q", r.Name)
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
		TSTree:   parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	results = stripFileEntity(results)
	// Issue #4850 — the struct now also emits a SCOPE.Schema/field entity per
	// field (ID, Name), so the expected set is: struct Component + 2 fields.
	if len(results) != 3 {
		t.Fatalf("expected 3 entities (struct + 2 fields), got %d", len(results))
	}
	var r *types.EntityRecord
	fieldNames := map[string]bool{}
	for i := range results {
		switch {
		case results[i].Kind == "SCOPE.Component":
			r = &results[i]
		case results[i].Kind == "SCOPE.Schema" && results[i].Subtype == "field":
			fieldNames[results[i].Name] = true
		}
	}
	if r == nil {
		t.Fatalf("expected a SCOPE.Component struct entity")
	}
	if r.Subtype != "struct" {
		t.Errorf("expected Subtype=struct, got %q", r.Subtype)
	}
	if r.Name != "User" {
		t.Errorf("expected Name=User, got %q", r.Name)
	}
	if !fieldNames["User.ID"] || !fieldNames["User.Name"] {
		t.Errorf("expected User.ID and User.Name field entities, got %v", fieldNames)
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
		TSTree:   parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	results = stripFileEntity(results)
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
		TSTree:   parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	results = stripFileEntity(results)
	// Issue #4850 — Config struct now also emits a SCOPE.Schema/field entity
	// per field (Host, Port): struct Component + 2 fields + NewConfig func.
	if len(results) != 4 {
		t.Fatalf("expected 4 entities (struct + 2 fields + function), got %d", len(results))
	}

	kinds := map[string]bool{}
	for _, r := range results {
		kinds[r.Kind] = true
	}
	// Config struct must emit SCOPE.Component; NewConfig emits SCOPE.Operation;
	// the struct fields emit SCOPE.Schema.
	if !kinds["SCOPE.Component"] || !kinds["SCOPE.Operation"] || !kinds["SCOPE.Schema"] {
		t.Errorf("expected SCOPE.Component, SCOPE.Operation and SCOPE.Schema, got kinds: %v", kinds)
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
		TSTree:   parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	results = stripFileEntity(results)
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
		TSTree:   parseGo([]byte(src)),
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
		TSTree:   parseGo([]byte(src)),
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
		TSTree:   parseGo([]byte(src)),
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
	fixturePath := "../../../testdata/fixtures/sources/go/sample_handler/sample_handler.go"
	content, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Skipf("golden fixture not found: %v", err)
	}

	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "testdata/fixtures/sources/go/sample_handler/sample_handler.go",
		Content:  content,
		Language: "go",
		TSTree:   parseGo(content),
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
	// issue #66: method entities are emitted as "<Receiver>.<method>".
	for _, want := range []string{"main", "User", "UserStore", "NewUserStore", "UserStore.GetUser", "UserStore.SaveUser"} {
		if !names[want] {
			t.Errorf("expected entity %q not found in extraction results", want)
		}
	}
}

// ---- import path extraction tests ---------------------------------
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
	// IMPORTS ToIDs for known external Go packages are rewritten to
	// the `ext:<root>` form by resolveImportToIDs (Track B). `os/exec`
	// collapses onto the `os` stdlib allowlist entry.
	if imp["rel_to"] != "ext:os" {
		t.Errorf("relationship ToID: expected %q, got %q", "ext:os", imp["rel_to"])
	}
}

func TestImportExtraction_FullModulePath(t *testing.T) {
	// The headline case: the full github.com/... path must be
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

// relationship tests --------------------------------------------
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
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return stripFileEntity(records)
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
	// Refs #44 follow-up: identifier-form CALLS edges (no selector) emit a
	// Format A structural-ref ToID so bare names like `helper` don't collide
	// across packages in multi-binary repos. Same-file callees resolve via
	// byLocation in the resolver.
	if !hasRelationship(main, "CALLS", "scope:operation:method:go:main.go:helper") {
		t.Errorf("expected CALLS → structural-ref(helper) on main, got %+v", main.Relationships)
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
	// rule #6: unknown/external callees are emitted rather than being
	// dropped. Refs #44 follow-up — identifier-form (bare) calls get a
	// Format A structural-ref ToID so unresolved targets land in
	// Dynamic via isHeuristicScopeStub rather than bug-resolver.
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
	if !hasRelationship(caller, "CALLS", "scope:operation:method:go:main.go:mystery") {
		t.Errorf("expected CALLS → structural-ref(mystery), got %+v", caller.Relationships)
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

// TestCallsRelationship_ReceiverTypeStampedOnSelfMethodCall covers the same-
// package method-dispatch fix for issue #148. When a method body calls another
// method on its OWN receiver (e.g. `mx.handle(...)` inside `(mx *Mux) Mount`),
// the resulting CALLS edge must carry a `receiver_type` property so the
// resolver can bind it to the local `Mux.handle` entity. Without this stamp
// the resolver falls back to bare-name lookup and ambiguity drops the edge
// into the bug-resolver bucket — which is the residual go-chi failure mode.
func TestCallsRelationship_ReceiverTypeStampedOnSelfMethodCall(t *testing.T) {
	src := `package chi

type Mux struct{}

func (mx *Mux) handle(pattern string) {}

func (mx *Mux) Mount(pattern string) {
	mx.handle(pattern)
}
`
	records := extractRecords(t, src, "mux.go")
	mount := findEntity(records, "Mux.Mount")
	if mount == nil {
		t.Fatal("Mux.Mount method not found")
	}
	var hit *types.RelationshipRecord
	for i := range mount.Relationships {
		r := &mount.Relationships[i]
		if r.Kind == "CALLS" && r.ToID == "handle" {
			hit = r
			break
		}
	}
	if hit == nil {
		t.Fatalf("expected CALLS → handle on Mux.Mount, got %+v", mount.Relationships)
	}
	if hit.Properties == nil || hit.Properties["receiver_type"] != "Mux" {
		t.Errorf("expected Properties[receiver_type]=Mux on self-method CALLS edge, got %+v", hit.Properties)
	}
}

// TestCallsRelationship_ReceiverTypeStampedFromParamType (issue #364)
// asserts that a selector_expression whose operand is a function parameter
// with a known static type acquires a `receiver_type` stamp set to that
// type's canonical name. Pointer types are stripped (`*Mux` → `Mux`) so the
// stamp matches the resolver's same-package member index keys, allowing the
// resolver to bind `other.handle` to the local `Mux.handle` entity.
//
// This supersedes the pre-#364 conservative-stamp test, which asserted the
// stamp was absent on foreign selectors. With param-type tracking the stamp
// is now both safe and correct: the operand's type IS a same-package type,
// so the resolver should bind to it.
func TestCallsRelationship_ReceiverTypeStampedFromParamType(t *testing.T) {
	src := `package chi

type Mux struct{}

func (mx *Mux) Mount(other *Mux, pattern string) {
	other.handle(pattern)
}

func (mx *Mux) handle(pattern string) {}
`
	records := extractRecords(t, src, "mux.go")
	mount := findEntity(records, "Mux.Mount")
	if mount == nil {
		t.Fatal("Mux.Mount method not found")
	}
	var hit *types.RelationshipRecord
	for i := range mount.Relationships {
		r := &mount.Relationships[i]
		if r.Kind == "CALLS" && r.ToID == "handle" {
			hit = r
			break
		}
	}
	if hit == nil {
		t.Fatal("expected CALLS edge to handle on Mux.Mount")
	}
	if hit.Properties == nil || hit.Properties["receiver_type"] != "Mux" {
		t.Errorf("expected receiver_type=Mux from param-type stamp, got %+v", hit.Properties)
	}
}

// TestCallsRelationship_StdlibInterfaceParamType (issue #364) verifies that
// calls dispatched on a parameter whose static type is a Go-stdlib package-
// qualified type (`*http.Request`, `http.ResponseWriter`) acquire a
// receiver_type stamp set to the package-qualified type name (with leading
// `*` stripped). The synth pass uses this stamp to route stdlib-interface
// methods like `Write` and `Method` to the correct ext:net/http placeholder.
func TestCallsRelationship_StdlibInterfaceParamType(t *testing.T) {
	src := `package handlers

import "net/http"

func handler(w http.ResponseWriter, r *http.Request) {
	_, _ = r.Cookie("session")
	w.Write([]byte("ok"))
	w.WriteHeader(200)
}

var _ = http.HandlerFunc(handler)
`
	records := extractRecords(t, src, "handlers.go")
	h := findEntity(records, "handler")
	if h == nil {
		t.Fatal("handler function not found")
	}
	want := map[string]string{
		"Cookie":      "http.Request",
		"Write":       "http.ResponseWriter",
		"WriteHeader": "http.ResponseWriter",
	}
	got := map[string]string{}
	for _, r := range h.Relationships {
		if r.Kind != "CALLS" || r.Properties == nil {
			continue
		}
		if rt := r.Properties["receiver_type"]; rt != "" {
			got[r.ToID] = rt
		}
	}
	for tgt, ty := range want {
		if got[tgt] != ty {
			t.Errorf("CALLS %s: receiver_type=%q want %q (full got=%v)", tgt, got[tgt], ty, got)
		}
	}
}

// TestCallsRelationship_StampShortVarDecl (issue #364) verifies that a
// short-var declaration whose RHS is a composite literal of a recognisable
// type stamps the resulting variable's static type onto downstream calls.
// `x := &Foo{}` followed by `x.Bar()` produces a CALLS edge with
// receiver_type=Foo. Routing decisions (stdlib vs user type) happen later
// in synth.go and this test only asserts the stamp shape.
func TestCallsRelationship_StampShortVarDecl(t *testing.T) {
	src := `package main

type Foo struct{}

func (f *Foo) Bar() {}

func driver() {
	x := &Foo{}
	x.Bar()
}
`
	records := extractRecords(t, src, "main.go")
	d := findEntity(records, "driver")
	if d == nil {
		t.Fatal("driver function not found")
	}
	var hit *types.RelationshipRecord
	for i := range d.Relationships {
		r := &d.Relationships[i]
		if r.Kind == "CALLS" && r.ToID == "Bar" {
			hit = r
			break
		}
	}
	if hit == nil {
		t.Fatal("expected CALLS edge to Bar on driver")
	}
	if hit.Properties == nil || hit.Properties["receiver_type"] != "Foo" {
		t.Errorf("expected receiver_type=Foo from short-var-decl, got %+v", hit.Properties)
	}
}

// TestCallsRelationship_StampChiNewRouter (issue #364) verifies the
// goConstructorReturnTypes table fires for `r := chi.NewRouter()` so the
// downstream `r.Get("/", h)` call acquires receiver_type=chi.Mux. Synth
// then routes this to ext:github.com/go-chi/chi.
func TestCallsRelationship_StampChiNewRouter(t *testing.T) {
	src := `package main

import "github.com/go-chi/chi"

func setup() {
	r := chi.NewRouter()
	r.Get("/", nil)
	r.Use(nil)
}
`
	records := extractRecords(t, src, "main.go")
	s := findEntity(records, "setup")
	if s == nil {
		t.Fatal("setup function not found")
	}
	for _, want := range []string{"Get", "Use"} {
		var hit *types.RelationshipRecord
		for i := range s.Relationships {
			r := &s.Relationships[i]
			if r.Kind == "CALLS" && r.ToID == want {
				hit = r
				break
			}
		}
		if hit == nil {
			t.Fatalf("expected CALLS edge to %s on setup", want)
		}
		if hit.Properties == nil || hit.Properties["receiver_type"] != "chi.Mux" {
			t.Errorf("CALLS %s: expected receiver_type=chi.Mux, got %+v", want, hit.Properties)
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
			// IMPORTS ToIDs for known external Go packages are
			// rewritten to `ext:<root>` by resolveImportToIDs (Track B).
			if rel.Kind == "IMPORTS" && rel.ToID == "ext:fmt" && rel.FromID == "main.go" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected IMPORTS relationship with FromID=main.go ToID=ext:fmt")
	}
}

// ---- DEPENDS_ON: method → receiver -----------------------------------------

func TestDependsOnRelationship_MethodToReceiver(t *testing.T) {
	src := `package main

type Store struct{}

func (s *Store) Save() error { return nil }
`
	records := extractRecords(t, src, "store.go")
	// issue #66: method entity Name is "<Receiver>.<method>".
	save := findEntity(records, "Store.Save")
	if save == nil {
		t.Fatal("Store.Save method not found")
	}
	if !hasRelationship(save, "DEPENDS_ON", "Store") {
		t.Errorf("expected DEPENDS_ON → Store on Store.Save method, got %+v", save.Relationships)
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
	// End-to-end check against the golden fixture. Verifies every
	// relationship kind fires at least once on a realistic file.
	fixturePath := "../../../testdata/fixtures/sources/go/sample_handler/sample_handler.go"
	content, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Skipf("golden fixture not found: %v", err)
	}
	ext, _ := extractor.Get("go")
	records, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "testdata/fixtures/sources/go/sample_handler/sample_handler.go",
		Content:  content,
		Language: "go",
		TSTree:   parseGo(content),
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

// ---- issue #66: receiver-qualified method entity IDs -----------------------

// TestExtract_DuplicateMethodNamesAcrossReceivers asserts that two structs in
// the same file each declaring same-named methods produce four distinct
// entities with distinct ComputeIDs. Before issue #66, the Go extractor set
// EntityRecord.Name to the bare identifier — so `Save` on `*UserStore` and
// `Save` on `*OrderStore` collapsed under ComputeID(Source+Kind+Name).
//
// Fix: methods are emitted with Name="<Receiver>.<method>" using the bare
// receiver type name (pointer-vs-value collapsed) so the dotted form is
// canonical and matches resolve.Index.byMember's first-dot split.
func TestExtract_DuplicateMethodNamesAcrossReceivers(t *testing.T) {
	src := `package store

type UserStore struct{}

func (s *UserStore) Save(item string) error { return nil }

func (s *UserStore) Load(id int) (string, error) { return "", nil }

type OrderStore struct{}

func (s *OrderStore) Save(item string) error { return nil }

func (s OrderStore) Load(id int) (string, error) { return "", nil }
`
	records := extractRecords(t, src, "store.go")

	// Collect the four method entities.
	methods := make(map[string]*types.EntityRecord)
	for i := range records {
		if records[i].Subtype != "method" {
			continue
		}
		r := &records[i]
		methods[r.Name] = r
	}

	want := []string{"UserStore.Save", "UserStore.Load", "OrderStore.Save", "OrderStore.Load"}
	if len(methods) != 4 {
		t.Fatalf("expected 4 distinct method entities, got %d: %v", len(methods), keysOf(methods))
	}
	for _, n := range want {
		if methods[n] == nil {
			t.Errorf("expected method entity %q not found; got %v", n, keysOf(methods))
		}
	}

	// Distinct ComputeIDs — the regression guard.
	ids := make(map[string]string)
	for name, rec := range methods {
		// SourceFile must be set for ComputeID to differentiate entries; the
		// extractor populates it from FileInput.Path.
		rec.SourceFile = "store.go"
		id := rec.ComputeID()
		if existing, dup := ids[id]; dup {
			t.Errorf("ComputeID collision: %q and %q both hash to %s", existing, name, id)
		}
		ids[id] = name
	}

	// Pointer vs value receiver should normalize to the same receiver type
	// name (no leading * in the qualifier).
	if m := methods["OrderStore.Load"]; m != nil {
		recv, _ := m.Metadata["receiver"].(string)
		if recv != "OrderStore" {
			t.Errorf("value receiver should yield receiver=OrderStore (no pointer), got %q", recv)
		}
	}
	if m := methods["UserStore.Save"]; m != nil {
		recv, _ := m.Metadata["receiver"].(string)
		if recv != "UserStore" {
			t.Errorf("pointer receiver should yield receiver=UserStore (pointer stripped), got %q", recv)
		}
	}
}

// TestExtract_DuplicateMethodsFromFixture parses the fixture file directly to
// guard against drift between the inline-source assertion above and the
// canonical fixture used by snapshot/golden flows.
func TestExtract_DuplicateMethodsFromFixture(t *testing.T) {
	content, err := os.ReadFile("testdata/duplicate_methods.go.fixture")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ext, _ := extractor.Get("go")
	records, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "testdata/duplicate_methods.go.fixture",
		Content:  content,
		Language: "go",
		TSTree:   parseGo(content),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	wantNames := map[string]bool{
		"UserStore.Save":  false,
		"UserStore.Load":  false,
		"OrderStore.Save": false,
		"OrderStore.Load": false,
	}
	for _, r := range records {
		if r.Subtype != "method" {
			continue
		}
		if _, ok := wantNames[r.Name]; ok {
			wantNames[r.Name] = true
		}
	}
	for name, found := range wantNames {
		if !found {
			t.Errorf("fixture: missing method entity %q", name)
		}
	}
}

// ---- issue #79: generic method receiver names ----------------------------

// TestExtract_GenericMethodReceiverStripsTypeParams asserts that methods
// declared on parameterised types collapse their type parameter list in
// the entity Name. Before the fix, `(s *Set[T]) Add` produced
// Name="Set[T].Add" — every instantiation of Set looked like a separate
// receiver to resolve.Index.byMember (which splits Name on the first '.').
// The fix unwraps generic_type AST nodes to their bare type_identifier so
// pointer (*Set[T]) and value (Cache[K, V]) receivers both yield the
// canonical Name "<Type>.<method>".
func TestExtract_GenericMethodReceiverStripsTypeParams(t *testing.T) {
	src := `package store

type Set[T comparable] struct{}

func (s *Set[T]) Add(item T) {}

func (s *Set[T]) Has(item T) bool { return false }

type Cache[K comparable, V any] struct{}

func (c Cache[K, V]) Get(k K) (V, bool) {
	var zero V
	return zero, false
}
`
	records := extractRecords(t, src, "store.go")

	methods := make(map[string]*types.EntityRecord)
	for i := range records {
		if records[i].Subtype != "method" {
			continue
		}
		r := &records[i]
		methods[r.Name] = r
	}

	// Stripped receiver names — no "[T]", no "[K, V]".
	want := []string{"Set.Add", "Set.Has", "Cache.Get"}
	for _, n := range want {
		if methods[n] == nil {
			t.Errorf("expected method entity %q with stripped type params; got %v", n, keysOf(methods))
		}
	}

	// Regression guard: the un-stripped forms must NOT appear.
	for _, bad := range []string{"Set[T].Add", "Set[T].Has", "Cache[K, V].Get"} {
		if methods[bad] != nil {
			t.Errorf("unexpected method entity with un-stripped type params: %q", bad)
		}
	}

	// receiver metadata should also be the bare type name.
	if m := methods["Set.Add"]; m != nil {
		recv, _ := m.Metadata["receiver"].(string)
		if recv != "Set" {
			t.Errorf("Set.Add receiver metadata: want %q, got %q", "Set", recv)
		}
	}
	if m := methods["Cache.Get"]; m != nil {
		recv, _ := m.Metadata["receiver"].(string)
		if recv != "Cache" {
			t.Errorf("Cache.Get receiver metadata: want %q, got %q", "Cache", recv)
		}
	}

	// Single-dot guard: the qualified Name must contain exactly one '.'
	// so resolve.Index.byMember's IndexByte('.', ...) split (introduced
	// in the issue #66 fix) recovers the correct receiver/member halves.
	for name := range methods {
		if strings.Count(name, ".") != 1 {
			t.Errorf("method Name %q must contain exactly one '.' for byMember split", name)
		}
	}
}

// TestExtract_GenericAndNonGenericSameMethodCollidesByName asserts the
// design contract: in a single file, a non-generic struct `Set` and a
// generic struct `Set[T]` both declaring `Add` resolve to the same Name
// "Set.Add" — they ARE the same canonical entity. ComputeID hashes
// Source+Kind+Name, so the per-file source path differentiates entries
// across files; within one file the two methods collapse, which matches
// Go's own rule that you cannot declare both a generic and non-generic
// type named `Set` in the same package.
func TestExtract_GenericAndNonGenericSameMethodSingleFile(t *testing.T) {
	// Note: this is intentionally invalid Go (duplicate type Set) — we
	// only feed it through the tree-sitter extractor, never compile it.
	// The point is to verify Name canonicalisation.
	src := `package store

type Set struct{}

func (s *Set) Add(item string) {}

type SetG[T comparable] struct{}

func (s *SetG[T]) Add(item T) {}
`
	records := extractRecords(t, src, "store.go")

	methods := make(map[string]int)
	for _, r := range records {
		if r.Subtype != "method" {
			continue
		}
		methods[r.Name]++
	}

	if methods["Set.Add"] != 1 {
		t.Errorf("expected exactly one Set.Add entity, got %d (methods=%v)", methods["Set.Add"], methods)
	}
	if methods["SetG.Add"] != 1 {
		t.Errorf("expected exactly one SetG.Add entity, got %d (methods=%v)", methods["SetG.Add"], methods)
	}
}

// TestExtract_GenericMethodsFromFixture parses the duplicate_methods
// fixture and asserts the issue #79 generic receivers appear with
// stripped type parameter lists.
func TestExtract_GenericMethodsFromFixture(t *testing.T) {
	content, err := os.ReadFile("testdata/duplicate_methods.go.fixture")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ext, _ := extractor.Get("go")
	records, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "testdata/duplicate_methods.go.fixture",
		Content:  content,
		Language: "go",
		TSTree:   parseGo(content),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	wantNames := map[string]bool{
		"Set.Add":   false,
		"Set.Has":   false,
		"Cache.Get": false,
	}
	for _, r := range records {
		if r.Subtype != "method" {
			continue
		}
		if _, ok := wantNames[r.Name]; ok {
			wantNames[r.Name] = true
		}
		// Negative guard against un-stripped names.
		if strings.Contains(r.Name, "[") {
			t.Errorf("fixture: method Name %q still carries type parameter list", r.Name)
		}
	}
	for name, found := range wantNames {
		if !found {
			t.Errorf("fixture: missing generic method entity %q", name)
		}
	}
}

func keysOf(m map[string]*types.EntityRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestExtractClassContains_TwoStructsSameMethod verifies issue #145:
// two structs in the same file each declaring a method with the same
// bare name produce distinct method entities AND each struct carries
// a CONTAINS edge whose ToID is a Format-A structural-ref keyed on
// the source file + the dotted Receiver.method Name.
func TestExtractClassContains_TwoStructsSameMethod(t *testing.T) {
	src := `package main

type UserStore struct{}
type OrderStore struct{}

func (s *UserStore) Get(id string) string  { return "" }
func (s *OrderStore) Get(id string) string { return "" }
`
	results, err := extractFromPath(src, "stores.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	// Distinct method entity IDs (issue #66 dotted Name +
	// SourceFile/Kind/Name → ComputeID hash).
	methodIDs := map[string]string{}
	for _, r := range results {
		e := r.(types.EntityRecord)
		if e.Kind != "SCOPE.Operation" {
			continue
		}
		if sub, _ := e.Metadata["subtype"].(string); sub != "method" {
			continue
		}
		id := e.ComputeID()
		if existing, dup := methodIDs[id]; dup {
			t.Errorf("method ID collision: %q and %q both compute to %s",
				existing, e.Name, id)
		}
		methodIDs[id] = e.Name
	}
	if len(methodIDs) != 2 {
		t.Fatalf("expected 2 distinct method entities, got %d (%v)",
			len(methodIDs), methodIDs)
	}

	// Each struct must own a CONTAINS edge whose ToID is the
	// Format-A structural-ref for its receiver method.
	wantContains := map[string]string{
		"UserStore":  extractor.BuildOperationStructuralRef("go", "stores.go", "UserStore.Get"),
		"OrderStore": extractor.BuildOperationStructuralRef("go", "stores.go", "OrderStore.Get"),
	}
	for _, r := range results {
		e := r.(types.EntityRecord)
		if e.Kind != "SCOPE.Component" {
			continue
		}
		want, expected := wantContains[e.Name]
		if !expected {
			continue
		}
		var got []string
		for _, rel := range e.Relationships {
			if rel.Kind == "CONTAINS" {
				got = append(got, rel.ToID)
			}
		}
		if len(got) != 1 {
			t.Errorf("struct %s: expected 1 CONTAINS edge, got %d (%v)", e.Name, len(got), got)
			continue
		}
		if got[0] != want {
			t.Errorf("struct %s: CONTAINS ToID = %q, want %q", e.Name, got[0], want)
		}
		delete(wantContains, e.Name)
	}
	for name := range wantContains {
		t.Errorf("struct %s: not found in extracted entities", name)
	}
}

// TestMainAndInitEmitStructuralFromID verifies Refs #44 #472: top-level `main`
// and `init` functions emit CALLS edges with a Format A structural-ref
// FromID instead of the bare name. Two files declaring `func main` would
// otherwise both emit FromID="main", colliding in the resolver's byName
// index and forcing every such edge into bug-resolver.
func TestMainAndInitEmitStructuralFromID(t *testing.T) {
	cases := []struct {
		path    string
		src     string
		fnName  string
		wantSub string // substring expected in the structural-ref FromID
	}{
		{
			path: "examples/helloworld/greeter_server/main.go",
			src: `package main

import "fmt"

func main() {
	fmt.Println("a")
}
`,
			fnName:  "main",
			wantSub: "scope:operation:method:go:examples/helloworld/greeter_server/main.go:main",
		},
		{
			path: "examples/route_guide/server/main.go",
			src: `package main

import "fmt"

func main() {
	fmt.Println("b")
}

func init() {
	fmt.Println("c")
}
`,
			fnName:  "main",
			wantSub: "scope:operation:method:go:examples/route_guide/server/main.go:main",
		},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			results, err := extractFromPath(tc.src, tc.path)
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			found := false
			for _, r := range results {
				e := r.(types.EntityRecord)
				if e.Kind != "SCOPE.Operation" || e.Name != tc.fnName {
					continue
				}
				found = true
				if len(e.Relationships) == 0 {
					t.Fatalf("entity %q: expected at least one CALLS edge, got 0", tc.fnName)
				}
				for _, rel := range e.Relationships {
					if rel.Kind != "CALLS" {
						continue
					}
					if rel.FromID != tc.wantSub {
						t.Errorf("CALLS FromID = %q, want %q", rel.FromID, tc.wantSub)
					}
				}
			}
			if !found {
				t.Fatalf("did not find entity %q in extracted records", tc.fnName)
			}
		})
	}
}

// TestMainFromIDsDistinctAcrossFiles verifies that the structural-ref form
// produced for `func main` in two different files differs, so a resolver
// indexing CALLS edges keyed by (FromID) sees two distinct sources rather
// than one ambiguous bucket.
func TestMainFromIDsDistinctAcrossFiles(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	fmt.Println("x")
}
`
	a, err := extractFromPath(src, "cmd/server-a/main.go")
	if err != nil {
		t.Fatalf("extract a: %v", err)
	}
	b, err := extractFromPath(src, "cmd/server-b/main.go")
	if err != nil {
		t.Fatalf("extract b: %v", err)
	}
	pick := func(rs []interface{}) string {
		for _, r := range rs {
			e := r.(types.EntityRecord)
			if e.Kind != "SCOPE.Operation" || e.Name != "main" {
				continue
			}
			for _, rel := range e.Relationships {
				if rel.Kind == "CALLS" {
					return rel.FromID
				}
			}
		}
		return ""
	}
	fa, fb := pick(a), pick(b)
	if fa == "" || fb == "" {
		t.Fatalf("missing FromID: a=%q b=%q", fa, fb)
	}
	if fa == fb {
		t.Errorf("FromIDs collided across files: %q", fa)
	}
}

// TestInitTopLevelStructuralFromID verifies that a top-level init() function
// receives the same structural-ref FromID treatment as main().
func TestInitTopLevelStructuralFromID(t *testing.T) {
	src := `package widgets

import "fmt"

func init() {
	fmt.Println("setup")
}
`
	results, err := extractFromPath(src, "internal/widgets/setup.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	wantFrom := "scope:operation:method:go:internal/widgets/setup.go:init"
	for _, r := range results {
		e := r.(types.EntityRecord)
		if e.Kind != "SCOPE.Operation" || e.Name != "init" {
			continue
		}
		for _, rel := range e.Relationships {
			if rel.Kind == "CALLS" && rel.FromID != wantFrom {
				t.Errorf("init CALLS FromID = %q, want %q", rel.FromID, wantFrom)
			}
		}
		return
	}
	t.Fatalf("init entity not found")
}

// TestTopLevelFunctionsAllGetStructuralFromID verifies that the widened
// fix (Refs #44 #472) emits Format A structural-ref FromIDs for every
// top-level function, not just `main`/`init`. Common names like `Run`,
// `Setup`, `Handle`, `New` collide just as often across packages in
// multi-binary repos.
func TestTopLevelFunctionsAllGetStructuralFromID(t *testing.T) {
	src := `package svc

import "fmt"

func Run() {
	fmt.Println("ok")
}
`
	results, err := extractFromPath(src, "svc/run.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	wantFrom := "scope:operation:method:go:svc/run.go:Run"
	for _, r := range results {
		e := r.(types.EntityRecord)
		if e.Kind != "SCOPE.Operation" || e.Name != "Run" {
			continue
		}
		for _, rel := range e.Relationships {
			if rel.Kind == "CALLS" && rel.FromID != wantFrom {
				t.Errorf("Run CALLS FromID = %q, want %q", rel.FromID, wantFrom)
			}
		}
		return
	}
	t.Fatalf("Run entity not found")
}

// TestMethodFromIDStillBare verifies that methods (entities with a
// receiver) keep their dotted `<Receiver>.<method>` Name as the FromID.
// The receiver-qualified Name already disambiguates across files via the
// byMember index (issue #66), so methods don't need the structural-ref
// FromID treatment that top-level functions do.
func TestMethodFromIDStillBare(t *testing.T) {
	src := `package svc

import "fmt"

type S struct{}

func (s *S) Save() {
	fmt.Println("ok")
}
`
	results, err := extractFromPath(src, "svc/store.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, r := range results {
		e := r.(types.EntityRecord)
		if e.Kind != "SCOPE.Operation" || e.Name != "S.Save" {
			continue
		}
		for _, rel := range e.Relationships {
			if rel.Kind == "CALLS" && rel.FromID != "S.Save" {
				t.Errorf("S.Save CALLS FromID = %q, want %q", rel.FromID, "S.Save")
			}
		}
		return
	}
	t.Fatalf("S.Save entity not found")
}

// TestBareCallToIDEmitsStructuralRef verifies Refs #44 / Refs #476 follow-up:
// CALLS edges whose target is a bare identifier (a same-file/same-package
// function call like `valid(md)` or `callUnaryEcho(rgc, "x")`) emit a Format A
// structural-ref ToID that pins the edge to the caller's file. Without this
// the bare name collides with same-named functions in sibling packages
// (e.g. 14 `func callUnaryEcho` across grpc-go-examples/examples/*) and
// every such CALLS edge lands in the bug-resolver bucket. The structural-ref
// resolves via byLocation[<file>][<name>] when the callee lives in the same
// file (the dominant case), and falls through to Dynamic via
// isHeuristicScopeStub otherwise — never to bug-resolver.
func TestBareCallToIDEmitsStructuralRef(t *testing.T) {
	src := `package main

func valid(s string) bool { return s != "" }

func ensureValidToken(s string) bool {
	if !valid(s) {
		return false
	}
	return true
}
`
	results, err := extractFromPath(src, "examples/features/authentication/server/main.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	wantTo := "scope:operation:method:go:examples/features/authentication/server/main.go:valid"
	for _, r := range results {
		e := r.(types.EntityRecord)
		if e.Kind != "SCOPE.Operation" || e.Name != "ensureValidToken" {
			continue
		}
		for _, rel := range e.Relationships {
			if rel.Kind != "CALLS" {
				continue
			}
			if rel.ToID == wantTo {
				return
			}
		}
		t.Fatalf("ensureValidToken: no CALLS edge with ToID=%q; got rels=%+v", wantTo, e.Relationships)
	}
	t.Fatalf("ensureValidToken entity not found")
}

// TestBareCallToIDsDistinctAcrossFiles verifies that two files in distinct
// packages declaring `callUnaryEcho` and calling it from `main` no longer
// emit colliding bare-name ToIDs. Each file's CALLS edge points at its own
// structural-ref so the resolver binds to the file-local function via
// byLocation rather than tripping the bare-name ambiguity guard.
func TestBareCallToIDsDistinctAcrossFiles(t *testing.T) {
	src := `package main

func callUnaryEcho() {}

func main() {
	callUnaryEcho()
}
`
	a, err := extractFromPath(src, "examples/features/authentication/client/main.go")
	if err != nil {
		t.Fatalf("extract a: %v", err)
	}
	b, err := extractFromPath(src, "examples/features/encryption/TLS/client/main.go")
	if err != nil {
		t.Fatalf("extract b: %v", err)
	}
	pick := func(rs []interface{}) string {
		for _, r := range rs {
			e := r.(types.EntityRecord)
			if e.Kind != "SCOPE.Operation" || e.Name != "main" {
				continue
			}
			for _, rel := range e.Relationships {
				if rel.Kind == "CALLS" && rel.ToID != "" {
					return rel.ToID
				}
			}
		}
		return ""
	}
	ta, tb := pick(a), pick(b)
	if ta == "" || tb == "" {
		t.Fatalf("missing ToID: a=%q b=%q", ta, tb)
	}
	if ta == tb {
		t.Errorf("bare ToIDs collided across files: %q", ta)
	}
	const wantPrefix = "scope:operation:method:go:"
	if !strings.HasPrefix(ta, wantPrefix) || !strings.HasPrefix(tb, wantPrefix) {
		t.Errorf("expected structural-ref ToIDs; got a=%q b=%q", ta, tb)
	}
}

// TestQualifiedCallToIDStaysBare verifies that selector-form calls (e.g.
// `pkg.Func()` or `recv.Method()`) keep their bare member-name ToID. Only
// the identifier-form (no selector) needs the structural-ref rewrite —
// selector-form calls already resolve via byMember / package-member /
// external-package lookups.
func TestQualifiedCallToIDStaysBare(t *testing.T) {
	src := `package widgets

import "fmt"

func Run() {
	fmt.Println("ok")
}
`
	results, err := extractFromPath(src, "widgets/run.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, r := range results {
		e := r.(types.EntityRecord)
		if e.Kind != "SCOPE.Operation" || e.Name != "Run" {
			continue
		}
		for _, rel := range e.Relationships {
			if rel.Kind != "CALLS" {
				continue
			}
			// Selector-form Println keeps its bare ToID for the external/byName path.
			if rel.ToID != "Println" {
				t.Errorf("fmt.Println CALLS ToID = %q, want bare %q", rel.ToID, "Println")
			}
		}
		return
	}
	t.Fatalf("Run entity not found")
}

// Issue #614 — interface-field dispatch stamp. The extractor must:
//  1. Record struct field types so `h.Store` resolves to `store.Store`.
//  2. Detect `<recvVar>.<Field>.<method>()` call shape inside method bodies.
//  3. Stamp Properties["interface_dispatch_type"] = "<fieldType>" on the
//     CALLS edge so the resolver can fan out IMPLEMENTS edges to the
//     implementing struct's method.
//  4. NOT rewrite the ToID to a per-file structural ref (which would bury
//     the bare member name the resolver keys on).
func TestExtractInterfaceFieldDispatchStamp_Issue614(t *testing.T) {
	src := `package handlers

import "example.com/demo/store"

type UsersHandler struct {
	Store store.Store
}

func (h *UsersHandler) List() {
	h.Store.List()
}
`
	results, err := extractFromPath(src, "handlers/users.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	var found bool
	for _, r := range results {
		e := r.(types.EntityRecord)
		if e.Kind != "SCOPE.Operation" || e.Name != "UsersHandler.List" {
			continue
		}
		for _, rel := range e.Relationships {
			if rel.Kind != "CALLS" || rel.ToID != "List" {
				continue
			}
			found = true
			gotType := rel.Properties["interface_dispatch_type"]
			if gotType != "store.Store" {
				t.Errorf("interface_dispatch_type = %q, want %q", gotType, "store.Store")
			}
			if strings.HasPrefix(rel.ToID, "scope:") {
				t.Errorf("ToID = %q, must stay bare for dispatch lookup", rel.ToID)
			}
		}
	}
	if !found {
		t.Fatalf("expected a CALLS edge to bare-name 'List' from UsersHandler.List; got none")
	}
}

// Issue #614 — self-recursion suppression must NOT fire on a same-name
// cross-receiver dispatch. `(h *UsersHandler).List` calling `h.Store.List()`
// shares the bare method name but the receiver chain is different, so the
// edge MUST be emitted.
func TestExtractCrossReceiverSameName_Issue614(t *testing.T) {
	src := `package handlers

import "example.com/demo/store"

type UsersHandler struct {
	Store store.Store
}

func (h *UsersHandler) List() {
	h.Store.List()
}
`
	results, err := extractFromPath(src, "handlers/users.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, r := range results {
		e := r.(types.EntityRecord)
		if e.Name != "UsersHandler.List" {
			continue
		}
		var hasCall bool
		for _, rel := range e.Relationships {
			if rel.Kind == "CALLS" && rel.ToID == "List" {
				hasCall = true
			}
		}
		if !hasCall {
			t.Fatalf("expected CALLS edge to 'List' (cross-receiver dispatch), got none")
		}
		return
	}
	t.Fatalf("UsersHandler.List entity not found")
}

// Issue #614 — self-recursion suppression MUST still fire on a real
// self-receiver call. `(h *Foo).Bar` calling `h.Bar()` is genuine
// recursion and must not produce an edge.
func TestExtractSelfReceiverRecursionStillSuppressed_Issue614(t *testing.T) {
	src := `package main

type Foo struct{}

func (h *Foo) Bar() {
	h.Bar()
}
`
	results, err := extractFromPath(src, "main.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, r := range results {
		e := r.(types.EntityRecord)
		if e.Name != "Foo.Bar" {
			continue
		}
		for _, rel := range e.Relationships {
			if rel.Kind == "CALLS" && rel.ToID == "Bar" {
				t.Errorf("self-recursion edge leaked: %+v", rel)
			}
		}
		return
	}
	t.Fatalf("Foo.Bar entity not found")
}

// Issue #614 — top-level self-recursion (no receiver) MUST still be
// suppressed. `func loop() { loop() }` is a self-recursive direct call.
func TestExtractTopLevelSelfRecursionStillSuppressed_Issue614(t *testing.T) {
	src := `package main

func loop() {
	loop()
}
`
	results, err := extractFromPath(src, "main.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, r := range results {
		e := r.(types.EntityRecord)
		if e.Name != "loop" {
			continue
		}
		for _, rel := range e.Relationships {
			if rel.Kind == "CALLS" && rel.ToID == "loop" {
				t.Errorf("self-recursion edge leaked: %+v", rel)
			}
			if rel.Kind == "CALLS" && strings.HasSuffix(rel.ToID, ":loop") {
				t.Errorf("self-recursion edge leaked (structural ref): %+v", rel)
			}
		}
		return
	}
	t.Fatalf("loop entity not found")
}

// ---------------------------------------------------------------------------
// type_alias_extraction — dedicated fixture-asserting tests (#3348).
//
// extractTypes emits SCOPE.Schema (subtype=type_alias) for every non-struct,
// non-interface type_spec. These tests prove all four idiomatic Go alias/
// definition shapes: named primitive, qualified alias, pointer type, and
// function-type definition. Previously the implementation existed but had no
// dedicated fixture asserting the path; this closes the partial→full gap.
// ---------------------------------------------------------------------------

func TestExtract_TypeAliasNamedPrimitive(t *testing.T) {
	// `type contextKey int` — a named type over a built-in primitive.
	src := `package x

type contextKey int
`
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.go",
		Content:  []byte(src),
		Language: "go",
		TSTree:   parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	results = stripFileEntity(results)
	var alias *types.EntityRecord
	for i := range results {
		if results[i].Subtype == "type_alias" {
			alias = &results[i]
			break
		}
	}
	if alias == nil {
		t.Fatalf("expected a type_alias entity, got: %+v", results)
	}
	if alias.Name != "contextKey" {
		t.Errorf("Name=%q, want contextKey", alias.Name)
	}
	if alias.Kind != "SCOPE.Schema" {
		t.Errorf("Kind=%q, want SCOPE.Schema", alias.Kind)
	}
}

func TestExtract_TypeAliasAssignAlias(t *testing.T) {
	// `type T = U` — proper assignment alias (Go >=1.9).
	src := `package x

type MyError = error
`
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.go",
		Content:  []byte(src),
		Language: "go",
		TSTree:   parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	results = stripFileEntity(results)
	found := false
	for _, r := range results {
		if r.Subtype == "type_alias" && r.Name == "MyError" {
			found = true
			if r.Kind != "SCOPE.Schema" {
				t.Errorf("Kind=%q, want SCOPE.Schema for assignment alias", r.Kind)
			}
		}
	}
	if !found {
		t.Fatalf("expected type_alias entity MyError, got %+v", results)
	}
}

func TestExtract_TypeAliasPointerType(t *testing.T) {
	// `type NodePtr = *Node` — pointer-type definition.
	src := `package x

type Node struct{ val int }
type NodePtr = *Node
`
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.go",
		Content:  []byte(src),
		Language: "go",
		TSTree:   parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	results = stripFileEntity(results)
	found := false
	for _, r := range results {
		if r.Subtype == "type_alias" && r.Name == "NodePtr" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected type_alias entity NodePtr, got %+v", results)
	}
}

func TestExtract_TypeAliasFunctionType(t *testing.T) {
	// `type HandlerFunc func(http.ResponseWriter, *http.Request)` — function-type def.
	src := `package x

type HandlerFunc func(w interface{}, r interface{})
`
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.go",
		Content:  []byte(src),
		Language: "go",
		TSTree:   parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	results = stripFileEntity(results)
	found := false
	for _, r := range results {
		if r.Subtype == "type_alias" && r.Name == "HandlerFunc" {
			found = true
			if r.Kind != "SCOPE.Schema" {
				t.Errorf("Kind=%q, want SCOPE.Schema for function-type alias", r.Kind)
			}
		}
	}
	if !found {
		t.Fatalf("expected type_alias entity HandlerFunc, got %+v", results)
	}
}

func TestExtract_TypeAliasMultipleInOneFile(t *testing.T) {
	// A file with struct + interface + two type aliases — all four shapes
	// in one extraction pass; proves the extractor emits one entity per
	// type_spec regardless of the surrounding struct/interface count.
	src := `package x

type contextKey int
type ID = string
type Store struct{ name string }
type Namer interface{ Name() string }
`
	ext, _ := extractor.Get("go")
	results, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.go",
		Content:  []byte(src),
		Language: "go",
		TSTree:   parseGo([]byte(src)),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	results = stripFileEntity(results)
	var aliases []string
	for _, r := range results {
		if r.Subtype == "type_alias" {
			aliases = append(aliases, r.Name)
		}
	}
	if len(aliases) != 2 {
		t.Fatalf("expected 2 type_alias entities, got %d: %v", len(aliases), aliases)
	}
	wantAliases := map[string]bool{"contextKey": false, "ID": false}
	for _, a := range aliases {
		wantAliases[a] = true
	}
	for name, found := range wantAliases {
		if !found {
			t.Errorf("missing type_alias entity %q", name)
		}
	}
	// Struct and interface must still appear under their correct subtypes.
	var structs, ifaces int
	for _, r := range results {
		if r.Subtype == "struct" {
			structs++
		}
		if r.Subtype == "interface" {
			ifaces++
		}
	}
	if structs != 1 {
		t.Errorf("expected 1 struct entity, got %d", structs)
	}
	if ifaces != 1 {
		t.Errorf("expected 1 interface entity, got %d", ifaces)
	}
}

// TestExtract_TypeAliasFrameworkSpecific proves gin-context file type aliases
// (a common Go-HTTP pattern: `type contextKey int` as a typed iota constant
// for request-context keys) are extracted correctly by the base Go extractor.
// This is the "framework-specific type-alias taxonomy" from issue #3348 —
// the taxonomy is language-level (all frameworks share the same extraction
// path via extractTypes), so one cross-framework test is representative.
func TestExtract_TypeAliasFrameworkSpecific(t *testing.T) {
	// Pattern: gin-context key types, echo middleware key types, fiber-ctx
	// key types are all `type T int` or `type T string`. One test covers all.
	aliases := []struct {
		typeName, baseType string
	}{
		{"ginContextKey", "int"},
		{"echoCtxKey", "string"},
		{"fiberCtxKey", "uint8"},
		{"chiCtxKey", "int"},
	}
	for _, a := range aliases {
		src := "package x\ntype " + a.typeName + " " + a.baseType + "\n"
		ext, _ := extractor.Get("go")
		results, err := ext.Extract(context.Background(), extractor.FileInput{
			Path:     "test.go",
			Content:  []byte(src),
			Language: "go",
			TSTree:   parseGo([]byte(src)),
		})
		if err != nil {
			t.Fatalf("%s: extract: %v", a.typeName, err)
		}
		results = stripFileEntity(results)
		found := false
		for _, r := range results {
			if r.Subtype == "type_alias" && r.Name == a.typeName {
				found = true
				if r.Kind != "SCOPE.Schema" {
					t.Errorf("%s: Kind=%q, want SCOPE.Schema", a.typeName, r.Kind)
				}
			}
		}
		if !found {
			t.Errorf("missing type_alias entity %q (baseType=%s)", a.typeName, a.baseType)
		}
	}
}
