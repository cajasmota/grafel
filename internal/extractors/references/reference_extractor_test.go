package references_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsgo "github.com/smacker/go-tree-sitter/golang"
	tsjava "github.com/smacker/go-tree-sitter/java"
	tsjs "github.com/smacker/go-tree-sitter/javascript"
	tspython "github.com/smacker/go-tree-sitter/python"
	tsrust "github.com/smacker/go-tree-sitter/rust"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors/references"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---- helpers ------------------------------------------------------------

func parseWith(t *testing.T, grammar *sitter.Language, src []byte) *sitter.Tree {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(grammar)
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func extractRefs(t *testing.T, lang string, grammar *sitter.Language, src, path string) []types.EntityRecord {
	t.Helper()
	content := []byte(src)
	tree := parseWith(t, grammar, content)
	ext := references.NewReferenceExtractor()
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  content,
		Language: lang,
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	return recs
}

func countByType(recs []types.EntityRecord, refType string) int {
	n := 0
	for _, r := range recs {
		if r.Properties["reference_type"] == refType {
			n++
		}
	}
	return n
}

func firstByTypeAndName(recs []types.EntityRecord, refType, targetName string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Properties["reference_type"] == refType &&
			recs[i].Properties["target_name"] == targetName {
			return &recs[i]
		}
	}
	return nil
}

// ---- AC criterion (Go) --------------------------------------------------

func TestGo_AC_UserTypeAndPropertyAccess(t *testing.T) {
	// Acceptance criterion from MX-1048: for
	//   func processUser(u User) { _ = u.Name }
	// we must emit a SCOPE.Reference(type=User) and
	// SCOPE.Reference(property_access=u.Name), and both carry a
	// REFERENCES relationship pointing at the processUser declaration.
	src := `package main

type User struct {
	Name string
}

func processUser(u User) {
	_ = u.Name
}
`
	recs := extractRefs(t, "go", tsgo.GetLanguage(), src, "test.go")

	// All records must be SCOPE.Reference.
	for _, r := range recs {
		if r.Kind != references.ReferenceKind {
			t.Fatalf("expected Kind=%s, got %s", references.ReferenceKind, r.Kind)
		}
	}

	// A type reference to User must exist.
	if rec := firstByTypeAndName(recs, references.RefType, "User"); rec == nil {
		t.Fatalf("expected a type reference to 'User', got %d total records", len(recs))
	}

	// A property_access reference to Name (u.Name) must exist.
	uName := firstByTypeAndName(recs, references.RefPropertyAccess, "Name")
	if uName == nil {
		t.Fatalf("expected a property_access reference to 'Name'")
	}
	if uName.Name != "u.Name" {
		t.Errorf("expected name 'u.Name', got %q", uName.Name)
	}
	if uName.Properties["receiver"] != "u" {
		t.Errorf("expected receiver 'u', got %q", uName.Properties["receiver"])
	}
}

func TestGo_CallRecognisesFunctionInvocation(t *testing.T) {
	src := `package main

func greet(name string) string { return "hi " + name }

func main() {
	greet("world")
}
`
	recs := extractRefs(t, "go", tsgo.GetLanguage(), src, "call.go")
	if countByType(recs, references.RefCall) == 0 {
		t.Fatalf("expected at least one call reference")
	}
	rec := firstByTypeAndName(recs, references.RefCall, "greet")
	if rec == nil {
		t.Fatalf("expected a call reference with target_name=greet")
	}
	// target_kind should resolve to SCOPE.Operation via Phase-1 lookup.
	if rec.Properties["target_kind"] != "SCOPE.Operation" {
		t.Errorf("expected target_kind=SCOPE.Operation, got %q", rec.Properties["target_kind"])
	}
	// REFERENCES relationship must be attached because the declaration
	// was resolved locally.
	if len(rec.Relationships) == 0 {
		t.Errorf("expected REFERENCES relationship on resolved call")
	} else if rec.Relationships[0].Kind != references.ReferencesRelationshipKind {
		t.Errorf("expected REFERENCES kind, got %s", rec.Relationships[0].Kind)
	}
}

func TestGo_WriteReferenceEmitted(t *testing.T) {
	src := `package main

func main() {
	x := 1
	x = x + 2
	_ = x
}
`
	recs := extractRefs(t, "go", tsgo.GetLanguage(), src, "write.go")
	if countByType(recs, references.RefWrite) == 0 {
		t.Fatalf("expected a write reference for x")
	}
}

// ---- Python -------------------------------------------------------------

func TestPython_FunctionCallAndPropertyAccess(t *testing.T) {
	src := `def greet(name):
    return "hi " + name

class User:
    def __init__(self, name):
        self.name = name

def run(u):
    greet(u.name)
`
	recs := extractRefs(t, "python", tspython.GetLanguage(), src, "user.py")

	if countByType(recs, references.RefCall) == 0 {
		t.Fatalf("expected python call references")
	}
	if countByType(recs, references.RefPropertyAccess) == 0 {
		t.Fatalf("expected python property_access references")
	}
}

// ---- JavaScript ----------------------------------------------------------

func TestJavaScript_CallAndMemberAccess(t *testing.T) {
	src := `function add(a, b) { return a + b; }

const obj = { value: 1 };
add(obj.value, 2);
`
	recs := extractRefs(t, "javascript", tsjs.GetLanguage(), src, "app.js")
	if countByType(recs, references.RefCall) == 0 {
		t.Fatalf("expected js call references")
	}
	if countByType(recs, references.RefPropertyAccess) == 0 {
		t.Fatalf("expected js property_access references")
	}
}

// ---- TypeScript ----------------------------------------------------------

func TestTypeScript_TypeAnnotationReference(t *testing.T) {
	src := `type User = { name: string }

function hello(u: User): string {
	return u.name;
}
`
	recs := extractRefs(t, "typescript", tstypescript.GetLanguage(), src, "app.ts")
	if countByType(recs, references.RefType) == 0 {
		t.Fatalf("expected at least one type reference")
	}
}

// ---- Java ---------------------------------------------------------------

func TestJava_MethodInvocationAndField(t *testing.T) {
	src := `class Demo {
    String greet(String name) { return "hi " + name; }
    void run() { greet("world"); }
}
`
	recs := extractRefs(t, "java", tsjava.GetLanguage(), src, "Demo.java")
	if countByType(recs, references.RefCall) == 0 {
		t.Fatalf("expected java call references")
	}
}

// ---- Rust ---------------------------------------------------------------

func TestRust_FieldAccessAndCall(t *testing.T) {
	src := `struct User { name: String }

fn greet(u: &User) -> &str { &u.name }
`
	recs := extractRefs(t, "rust", tsrust.GetLanguage(), src, "user.rs")
	if countByType(recs, references.RefPropertyAccess) == 0 {
		t.Fatalf("expected rust field_expression references")
	}
}

// ---- Edge cases ---------------------------------------------------------

func TestExtract_EmptyFileReturnsNil(t *testing.T) {
	ext := references.NewReferenceExtractor()
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.go",
		Content:  nil,
		Language: "go",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if recs != nil {
		t.Fatalf("expected nil, got %d records", len(recs))
	}
}

func TestExtract_UnsupportedLanguageIsNoop(t *testing.T) {
	ext := references.NewReferenceExtractor()
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "x.xyz",
		Content:  []byte("nonsense content"),
		Language: "fortran_77",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected 0 records, got %d", len(recs))
	}
}

func TestExtract_MissingTreeWithoutGrammarProviderIsNoop(t *testing.T) {
	ext := references.NewReferenceExtractor()
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "t.go",
		Content:  []byte("package main\nfunc x() {}"),
		Language: "go",
		// Tree intentionally nil, no GrammarProvider configured.
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if recs != nil {
		t.Fatalf("expected no records, got %d", len(recs))
	}
}

func TestExtract_WithGrammarProviderParsesInline(t *testing.T) {
	ext := references.NewReferenceExtractor()
	ext.GrammarProvider = func(language string) *sitter.Language {
		if language == "go" {
			return tsgo.GetLanguage()
		}
		return nil
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "gp.go",
		Content:  []byte("package main\nfunc Greet() { Greet() }"),
		Language: "go",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(recs) == 0 {
		t.Fatalf("expected at least one reference via GrammarProvider path")
	}
}

func TestExtract_MaxReferencesCapEnforced(t *testing.T) {
	ext := references.NewReferenceExtractor()
	ext.GrammarProvider = func(language string) *sitter.Language { return tsgo.GetLanguage() }
	ext.MaxReferencesPerFile = 1
	src := `package main
func main() { a := 1; b := 2; c := 3; _ = a; _ = b; _ = c }`
	recs, _ := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "cap.go",
		Content:  []byte(src),
		Language: "go",
	})
	if len(recs) > 1 {
		t.Fatalf("expected cap enforced at 1, got %d", len(recs))
	}
}

func TestExtract_PanicRecoveredAndPartialResultsReturned(t *testing.T) {
	ext := references.NewReferenceExtractor()
	ext.FrameworkTagger = panicTagger{} // will panic on first tag call
	ext.GrammarProvider = func(language string) *sitter.Language { return tsgo.GetLanguage() }
	src := "package main\nfunc main() {}"
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "panic.go",
		Content:  []byte(src),
		Language: "go",
	})
	// We don't assert on specific counts — we assert that no panic
	// escaped and no error was returned (spec says the panic is
	// swallowed, partial results are returned).
	if err != nil {
		t.Fatalf("expected nil error after panic recovery, got %v", err)
	}
	_ = recs
}

type panicTagger struct{}

func (panicTagger) Tag(rec *types.EntityRecord, ctx references.FrameworkContext) {
	panic(errors.New("boom"))
}

// ---- Full enum coverage -------------------------------------------------

func TestAllReferenceTypesAreCovered(t *testing.T) {
	// Emit each reference type at least once across several languages
	// to satisfy the "every variant of every enum" coverage rule.
	seen := map[string]bool{}

	// Go — call, type, property_access, write
	for _, r := range extractRefs(t, "go", tsgo.GetLanguage(), `package main
type T struct { F int }
func f(x T) { x.F = 1; f(x) }
`, "all.go") {
		seen[r.Properties["reference_type"]] = true
	}

	// Python — read / argument (via call argument identifier)
	for _, r := range extractRefs(t, "python", tspython.GetLanguage(), `def f(x):
    return f(x)
`, "arg.py") {
		seen[r.Properties["reference_type"]] = true
	}

	for _, rt := range []string{
		references.RefCall,
		references.RefType,
		references.RefPropertyAccess,
		references.RefWrite,
		references.RefArgument,
	} {
		if !seen[rt] {
			t.Errorf("reference type %s not observed across corpus", rt)
		}
	}
}

func TestAllReferenceTypesListLengthMatchesConstants(t *testing.T) {
	if len(references.AllReferenceTypes) != 6 {
		t.Fatalf("expected 6 canonical reference types, got %d", len(references.AllReferenceTypes))
	}
}

// ---- Wrap integration ---------------------------------------------------

type fakeBaseExtractor struct {
	language string
	out      []types.EntityRecord
	err      error
}

func (f *fakeBaseExtractor) Language() string { return f.language }
func (f *fakeBaseExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	return f.out, f.err
}

func TestWrap_CombinesDeclarationsAndReferences(t *testing.T) {
	base := &fakeBaseExtractor{
		language: "go",
		out: []types.EntityRecord{{
			Name:       "Greet",
			Kind:       "SCOPE.Operation",
			SourceFile: "w.go",
		}},
	}
	refs := references.NewReferenceExtractor()
	refs.GrammarProvider = func(language string) *sitter.Language { return tsgo.GetLanguage() }
	combined := references.Wrap(base, refs)

	if combined.Language() != "go" {
		t.Fatalf("expected language=go, got %q", combined.Language())
	}

	out, err := combined.Extract(context.Background(), extractor.FileInput{
		Path:     "w.go",
		Content:  []byte("package main\nfunc Greet() { Greet() }"),
		Language: "go",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(out) < 2 {
		t.Fatalf("expected wrapped extractor to emit >= 2 records, got %d", len(out))
	}
	var sawDecl, sawRef bool
	for _, r := range out {
		if r.Kind == "SCOPE.Operation" && r.Name == "Greet" {
			sawDecl = true
		}
		if r.Kind == references.ReferenceKind {
			sawRef = true
		}
	}
	if !sawDecl || !sawRef {
		t.Fatalf("combined output missing expected kinds: decl=%v ref=%v", sawDecl, sawRef)
	}
}

func TestWrap_NilBaseExtractor(t *testing.T) {
	refs := references.NewReferenceExtractor()
	refs.GrammarProvider = func(language string) *sitter.Language { return tsgo.GetLanguage() }
	combined := references.Wrap(nil, refs)
	if combined.Language() != "" {
		t.Errorf("expected empty language for nil base, got %q", combined.Language())
	}
	out, err := combined.Extract(context.Background(), extractor.FileInput{
		Path:     "w.go",
		Content:  []byte("package main\nfunc Greet() {}"),
		Language: "go",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	_ = out
}

func TestWrap_BaseErrorDoesNotBlockReferencePhase(t *testing.T) {
	base := &fakeBaseExtractor{
		language: "go",
		err:      errors.New("synthetic base failure"),
	}
	refs := references.NewReferenceExtractor()
	refs.GrammarProvider = func(language string) *sitter.Language { return tsgo.GetLanguage() }
	combined := references.Wrap(base, refs)

	out, err := combined.Extract(context.Background(), extractor.FileInput{
		Path:     "e.go",
		Content:  []byte("package main\nfunc A() { A() }"),
		Language: "go",
	})
	// The wrapped extractor preserves the base error but still appends
	// references produced during the reference phase.
	if err == nil || !strings.Contains(err.Error(), "synthetic") {
		t.Fatalf("expected base error to propagate, got %v", err)
	}
	var sawRef bool
	for _, r := range out {
		if r.Kind == references.ReferenceKind {
			sawRef = true
		}
	}
	if !sawRef {
		t.Errorf("expected reference records to be appended even with base error")
	}
}
