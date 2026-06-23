package rust_test

import (
	"context"
	"strings"
	"testing"

	tsrust "github.com/smacker/go-tree-sitter/rust"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/rust"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	"github.com/cajasmota/grafel/internal/types"
)

// parseForTest parses Rust source into a ts.Tree via the smacker adapter. Tests
// stamp the result on FileInput.TSTree (which the migrated Rust extractor
// consumes); the smacker adapter keeps these tests linkable in the default build
// while exercising the ts façade (B2 #5418).
func parseForTest(t *testing.T, src string) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsrust.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func TestRustExtractor_BasicExtraction(t *testing.T) {
	src := `
use std::collections::HashMap;
use serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize)]
struct User {
    id: u32,
    name: String,
}

trait Repository {
    fn find(&self, id: u32) -> Option<User>;
}

impl Repository for Vec<User> {
    fn find(&self, id: u32) -> Option<User> {
        self.iter().find(|u| u.id == id).cloned()
    }
}

fn create_user(name: String) -> User {
    User { id: 1, name }
}
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("rust")
	if !ok {
		t.Fatal("rust extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "main.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var structs, traits, impls, functions, imports int
	for _, e := range got {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "struct":
			structs++
		case e.Kind == "SCOPE.Component" && e.Subtype == "trait":
			traits++
		case e.Kind == "SCOPE.Component" && e.Subtype == "impl":
			impls++
		case e.Kind == "SCOPE.Operation":
			functions++
		case e.Kind == "SCOPE.Component" && len(e.Relationships) > 0:
			imports++
		}
	}

	if structs == 0 {
		t.Error("expected at least one struct entity")
	}
	if traits == 0 {
		t.Error("expected at least one trait entity")
	}
	if impls == 0 {
		t.Error("expected at least one impl entity")
	}
	if functions == 0 {
		t.Error("expected at least one function entity")
	}
	if imports == 0 {
		t.Error("expected at least one import entity")
	}
}

// extractRust is a probe helper: parses src and returns the rust extractor's
// entities, failing the test on any error.
func extractRust(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("rust")
	if !ok {
		t.Fatal("rust extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract %q: %v", path, err)
	}
	return got
}

// hasComponent reports whether ents contains a SCOPE.Component of the given
// subtype with the given name.
func hasComponent(ents []types.EntityRecord, subtype, name string) bool {
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == subtype && e.Name == name {
			return true
		}
	}
	return false
}

// TestRustExtractor_TypeSystem_PerFramework is the VALUE-ASSERTING probe for
// issue #3980. The rust extractor is registered unconditionally per-language
// ("rust") with no framework gating, so enum / trait / type_alias entities are
// emitted for every .rs file regardless of which Rust HTTP framework (if any)
// the file uses. This probe drives utoipa-, async-graphql-, and tonic-style
// source through the extractor and asserts the type-system entities genuinely
// fire — the evidence that backfills the per-framework Type System cells.
func TestRustExtractor_TypeSystem_PerFramework(t *testing.T) {
	cases := []struct {
		name      string
		path      string
		src       string
		enumName  string
		traitName string
		aliasName string
	}{
		{
			name: "utoipa",
			path: "utoipa_api.rs",
			src: `
use utoipa::ToSchema;

#[derive(ToSchema)]
enum Status {
    Active,
    Inactive,
}

trait Documented {
    fn schema_name(&self) -> &str;
}

type ApiResult = Result<Status, String>;
`,
			enumName:  "Status",
			traitName: "Documented",
			aliasName: "ApiResult",
		},
		{
			name: "async-graphql",
			path: "async_graphql_api.rs",
			src: `
use async_graphql::Enum;

#[derive(Enum, Copy, Clone, Eq, PartialEq)]
enum Episode {
    NewHope,
    Empire,
}

trait Resolver {
    fn resolve(&self) -> i32;
}

type GqlResult = async_graphql::Result<Episode>;
`,
			enumName:  "Episode",
			traitName: "Resolver",
			aliasName: "GqlResult",
		},
		{
			name: "tonic",
			path: "tonic_service.rs",
			src: `
use tonic::Request;

enum RpcMode {
    Unary,
    Streaming,
}

trait GreeterExt {
    fn greet(&self) -> String;
}

type GrpcStatus = Result<tonic::Response<()>, tonic::Status>;
`,
			enumName:  "RpcMode",
			traitName: "GreeterExt",
			aliasName: "GrpcStatus",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := extractRust(t, tc.path, tc.src)
			if !hasComponent(ents, "enum", tc.enumName) {
				t.Errorf("%s: expected enum entity %q (enum_extraction)", tc.name, tc.enumName)
			}
			if !hasComponent(ents, "trait", tc.traitName) {
				t.Errorf("%s: expected trait entity %q (interface_extraction)", tc.name, tc.traitName)
			}
			if !hasComponent(ents, "type_alias", tc.aliasName) {
				t.Errorf("%s: expected type_alias entity %q (type_alias_extraction)", tc.name, tc.aliasName)
			}
			// Value-assert the alias target is captured (not just the name).
			for _, e := range ents {
				if e.Subtype == "type_alias" && e.Name == tc.aliasName {
					if e.Properties["aliased_type"] == "" {
						t.Errorf("%s: type_alias %q missing aliased_type property", tc.name, tc.aliasName)
					}
				}
			}
		})
	}
}

func TestRustExtractor_StructEntity(t *testing.T) {
	src := `
struct Foo {
    id: u32,
    name: String,
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "foo.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Foo" && e.Kind == "SCOPE.Component" && e.Subtype == "struct" {
			found = true
			if e.SourceFile != "foo.rs" {
				t.Errorf("expected source_file foo.rs, got %s", e.SourceFile)
			}
			if e.Language != "rust" {
				t.Errorf("expected language rust, got %s", e.Language)
			}
			if e.StartLine == 0 {
				t.Error("expected non-zero start_line")
			}
		}
	}
	if !found {
		t.Error("expected entity Foo with Kind=SCOPE.Component Subtype=struct")
	}
}

func TestRustExtractor_EnumEntity(t *testing.T) {
	src := `
enum Color {
    Red,
    Green,
    Blue,
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "color.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Color" && e.Kind == "SCOPE.Component" && e.Subtype == "enum" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity Color with Kind=SCOPE.Component Subtype=enum")
	}
}

func TestRustExtractor_TraitEntity(t *testing.T) {
	src := `
trait Animal {
    fn speak(&self) -> String;
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "animal.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "Animal" && e.Kind == "SCOPE.Component" && e.Subtype == "trait" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity Animal with Kind=SCOPE.Component Subtype=trait")
	}
}

func TestRustExtractor_FunctionEntity(t *testing.T) {
	src := `
fn create_user(name: String) -> User {
    User { id: 1, name }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "func.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Name == "create_user" && e.Kind == "SCOPE.Operation" && e.Subtype == "function" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity create_user with Kind=SCOPE.Operation Subtype=function")
	}
}

func TestRustExtractor_ImportRelationship(t *testing.T) {
	src := `
use std::collections::HashMap;
use serde::{Deserialize, Serialize};
use actix_web::web;

fn main() {}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "imports.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	importTargets := map[string]bool{}
	for _, e := range got {
		for _, rel := range e.Relationships {
			if rel.Kind == "IMPORTS" {
				importTargets[rel.ToID] = true
			}
		}
	}

	if !importTargets["std::collections::HashMap"] {
		t.Error("expected IMPORTS for std::collections::HashMap")
	}
	if !importTargets["actix_web::web"] {
		t.Error("expected IMPORTS for actix_web::web")
	}
}

func TestRustExtractor_EmptyFile(t *testing.T) {
	tree := parseForTest(t, "")
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.rs",
		Content:  []byte(""),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for empty file, got %d", len(got))
	}
}

func TestRustExtractor_NilTree(t *testing.T) {
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "nil.rs",
		Content:  []byte("fn main() {}"),
		Language: "rust",
		TSTree:   nil,
	})
	if err != nil {
		t.Fatalf("unexpected error on nil tree: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero entities for nil tree, got %d", len(got))
	}
}

func TestRustExtractor_MalformedFile(t *testing.T) {
	src := `
struct GoodStruct {
    id: u32,
}

fn good_function() -> u32 { 1 }

fn bad_function(x: u32
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "malformed.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error on malformed file: %v", err)
	}

	var foundGood bool
	for _, e := range got {
		if e.Name == "GoodStruct" {
			foundGood = true
		}
	}
	if !foundGood {
		t.Error("expected GoodStruct to be extracted from malformed file")
	}
}

func TestRustExtractor_UnregisteredLanguage(t *testing.T) {
	_, ok := extractor.Get("fortran")
	if ok {
		t.Error("expected false for unregistered language fortran")
	}
}

// Issue #101: pub-modifier and intra-crate prefixes must not produce
// IMPORTS edges that would later become bug-extractor entries.
func TestRustExtractor_ImportPrefixes(t *testing.T) {
	src := `
use tokio::net::TcpListener;
pub use serde::Deserialize;
pub(crate) use anyhow::Result;
use crate::module::LocalThing;
use self::sibling::Helper;
use super::parent::Other;
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "imports.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	importTops := map[string]bool{}
	for _, e := range got {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				importTops[r.ToID] = true
			}
		}
	}
	// External crates must be emitted with their canonical "<crate>::..."
	// shape (no leading "pub " modifier).
	want := []string{
		"tokio::net::TcpListener",
		"serde::Deserialize",
		"anyhow::Result",
	}
	for _, w := range want {
		if !importTops[w] {
			t.Errorf("missing IMPORTS ToID %q; got: %v", w, importTops)
		}
	}
	// Intra-crate paths must NOT produce IMPORTS edges.
	for tid := range importTops {
		if tid == "crate::module::LocalThing" ||
			tid == "self::sibling::Helper" ||
			tid == "super::parent::Other" {
			t.Errorf("unexpected intra-crate IMPORTS edge: %q", tid)
		}
	}
}

// Issue #101: a bare root-only `use tokio;` (no `::` segment) must still
// be emitted as an IMPORTS edge with ToID == "tokio" so synth maps it to
// "ext:tokio" and the resolver classifies it as ExternalKnown — not
// dropped, not bug-extractor.
func TestRustExtractor_ImportRootOnly(t *testing.T) {
	src := `use tokio;
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "root_only.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var foundToID string
	for _, e := range got {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				foundToID = r.ToID
			}
		}
	}
	if foundToID != "tokio" {
		t.Fatalf("root-only `use tokio;` should emit IMPORTS ToID=%q, got %q", "tokio", foundToID)
	}
}

// Issue #615 — fn names inside impl blocks must be qualified as "TypeName.fnName".
func TestRustExtractor_ImplFnQualified(t *testing.T) {
	src := `
struct Foo {}
impl Foo {
    fn bar(&self) {}
    fn new() -> Foo { Foo {} }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "foo.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var foundBar, foundNew bool
	for _, e := range got {
		if e.Kind == "SCOPE.Operation" && e.Name == "Foo.bar" {
			foundBar = true
		}
		if e.Kind == "SCOPE.Operation" && e.Name == "Foo.new" {
			foundNew = true
		}
		// Bare names must NOT appear as SCOPE.Operation.
		if e.Kind == "SCOPE.Operation" && (e.Name == "bar" || e.Name == "new") {
			t.Errorf("bare function name %q leaked — should be qualified as Foo.bar/Foo.new", e.Name)
		}
	}
	if !foundBar {
		t.Error("expected SCOPE.Operation Name=Foo.bar")
	}
	if !foundNew {
		t.Error("expected SCOPE.Operation Name=Foo.new")
	}

	// The Foo impl entity must have CONTAINS edges pointing to qualified refs.
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Subtype == "impl" && e.Name == "Foo" {
			var barRef, newRef bool
			for _, r := range e.Relationships {
				if r.Kind == "CONTAINS" && strings.Contains(r.ToID, "Foo.bar") {
					barRef = true
				}
				if r.Kind == "CONTAINS" && strings.Contains(r.ToID, "Foo.new") {
					newRef = true
				}
			}
			if !barRef {
				t.Error("Foo impl CONTAINS edge missing ref to Foo.bar")
			}
			if !newRef {
				t.Error("Foo impl CONTAINS edge missing ref to Foo.new")
			}
		}
	}
}

// Issue #615 — multiple impl blocks must not cross-contaminate names.
func TestRustExtractor_ImplMultipleTypes(t *testing.T) {
	src := `
struct A {}
struct B {}
impl A { fn hello(&self) {} }
impl B { fn world(&self) {} }
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "multi.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var foundAHello, foundBWorld bool
	for _, e := range got {
		if e.Kind == "SCOPE.Operation" && e.Name == "A.hello" {
			foundAHello = true
		}
		if e.Kind == "SCOPE.Operation" && e.Name == "B.world" {
			foundBWorld = true
		}
		// Bare names must NOT appear.
		if e.Kind == "SCOPE.Operation" && (e.Name == "hello" || e.Name == "world") {
			t.Errorf("bare function name %q leaked from impl block", e.Name)
		}
	}
	if !foundAHello {
		t.Error("expected SCOPE.Operation Name=A.hello")
	}
	if !foundBWorld {
		t.Error("expected SCOPE.Operation Name=B.world")
	}
}

// Issue #616 — self.method() inside an impl block should resolve to TypeName.method.
func TestRustExtractor_DynReceiverSelf(t *testing.T) {
	src := `
trait Processor { fn process(&self, x: u32) -> u32; }
impl Processor for MyImpl {
    fn process(&self, x: u32) -> u32 {
        self.transform(x)
    }
    fn transform(&self, x: u32) -> u32 { x * 2 }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "processor.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the "MyImpl.process" function entity and check its CALLS edges.
	var callsTransform bool
	for _, e := range got {
		if e.Kind == "SCOPE.Operation" && e.Name == "MyImpl.process" {
			for _, r := range e.Relationships {
				if r.Kind == "CALLS" && strings.Contains(r.ToID, "transform") {
					callsTransform = true
				}
			}
		}
	}
	if !callsTransform {
		t.Error("expected MyImpl.process to have a CALLS edge containing 'transform' (should be MyImpl.transform)")
	}
}

// Issue #616 — typed dyn-receiver parameter calls should resolve to TraitName.method.
func TestRustExtractor_DynParamReceiver(t *testing.T) {
	src := `
trait Repo { fn find(&self, id: u32) -> u32; }
fn use_repo(r: &dyn Repo) {
    r.find(1);
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "repo.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var foundRepoFind bool
	for _, e := range got {
		if e.Kind == "SCOPE.Operation" && e.Name == "use_repo" {
			for _, r := range e.Relationships {
				if r.Kind == "CALLS" && r.ToID == "Repo.find" {
					foundRepoFind = true
				}
			}
		}
	}
	if !foundRepoFind {
		t.Error("expected use_repo to have CALLS edge with ToID=Repo.find")
	}
}

// Issue #615 negative — trait method bodies must NOT be owner-qualified.
// Only impl methods get the "TypeName." prefix; trait methods remain bare.
func TestRustExtractor_TraitFnNotQualified(t *testing.T) {
	src := `
trait MyTrait {
    fn method(&self) {}
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "trait.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, e := range got {
		if e.Kind == "SCOPE.Operation" && e.Name == "MyTrait.method" {
			t.Error("trait method should NOT be qualified — expected Name=method, got MyTrait.method")
		}
	}
	var foundBare bool
	for _, e := range got {
		if e.Kind == "SCOPE.Operation" && e.Name == "method" {
			foundBare = true
		}
	}
	if !foundBare {
		t.Error("expected trait method Name=method (bare, unqualified)")
	}
}

func TestRustExtractor_LineNumbers(t *testing.T) {
	src := `struct Alpha {
    id: u32,
}

fn method1() -> u32 { 1 }
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "lines.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Name == "Alpha" {
			if e.StartLine < 1 {
				t.Errorf("expected StartLine >= 1, got %d", e.StartLine)
			}
			if e.EndLine < e.StartLine {
				t.Errorf("expected EndLine >= StartLine, got start=%d end=%d", e.StartLine, e.EndLine)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// type_alias_extraction — issue #3269
// ---------------------------------------------------------------------------

func TestRustExtractor_TypeAlias(t *testing.T) {
	src := `
type MyResult<T> = Result<T, MyError>;
type UserId = u64;
pub type BoxFuture<'a, T> = Pin<Box<dyn Future<Output = T> + Send + 'a>>;
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("rust")
	if !ok {
		t.Fatal("rust extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "types.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var aliases []string
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Subtype == "type_alias" {
			aliases = append(aliases, e.Name)
		}
	}

	wantAliases := []string{"MyResult", "UserId", "BoxFuture"}
	for _, want := range wantAliases {
		found := false
		for _, got := range aliases {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected type_alias entity %q; got: %v", want, aliases)
		}
	}
}

// ---------------------------------------------------------------------------
// Type System deep extraction — issue #3411
// ---------------------------------------------------------------------------

// findRustEntity returns the first entity matching subtype+name, or nil.
func findRustEntity(t *testing.T, src, subtype, name string) map[string]string {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "ts.rs", Content: []byte(src), Language: "rust", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range got {
		if e.Subtype == subtype && e.Name == name {
			return e.Properties
		}
	}
	t.Fatalf("entity subtype=%s name=%s not found", subtype, name)
	return nil
}

// enum_extraction: assert specific variant names (tuple, struct, discriminant)
// and derive macros are captured.
func TestRustExtractor_EnumVariants_Deep(t *testing.T) {
	src := `
#[derive(Debug, Clone, PartialEq)]
enum Shape {
    Circle(f64),
    Rect { w: u8, h: u8 },
    Unit = 1,
    Empty,
}
`
	props := findRustEntity(t, src, "enum", "Shape")
	if props["variants"] != "Circle, Rect, Unit, Empty" {
		t.Errorf("variants = %q, want %q", props["variants"], "Circle, Rect, Unit, Empty")
	}
	if props["derives"] != "Debug, Clone, PartialEq" {
		t.Errorf("derives = %q, want %q", props["derives"], "Debug, Clone, PartialEq")
	}
}

func TestRustExtractor_EnumGenerics_Deep(t *testing.T) {
	src := `
enum Either<L, R> {
    Left(L),
    Right(R),
}
`
	props := findRustEntity(t, src, "enum", "Either")
	if props["variants"] != "Left, Right" {
		t.Errorf("variants = %q, want %q", props["variants"], "Left, Right")
	}
	if props["generics"] != "L, R" {
		t.Errorf("generics = %q, want %q", props["generics"], "L, R")
	}
}

// interface_extraction (trait): assert method names (required + default),
// supertraits, EXTENDS edges, and generics.
func TestRustExtractor_TraitMethods_Deep(t *testing.T) {
	src := `
trait Animal: Eq + Clone {
    fn speak(&self) -> String;
    fn legs(&self) -> u32 { 4 }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")
	got, _ := ext.Extract(context.Background(), extractor.FileInput{
		Path: "animal.rs", Content: []byte(src), Language: "rust", TSTree: tree,
	})

	var props map[string]string
	var extendsTargets []string
	for _, e := range got {
		if e.Subtype == "trait" && e.Name == "Animal" {
			props = e.Properties
			for _, r := range e.Relationships {
				if r.Kind == "EXTENDS" {
					extendsTargets = append(extendsTargets, r.ToID)
				}
			}
		}
	}
	if props == nil {
		t.Fatal("trait Animal not found")
	}
	if props["methods"] != "speak, legs" {
		t.Errorf("methods = %q, want %q", props["methods"], "speak, legs")
	}
	if props["supertraits"] != "Eq, Clone" {
		t.Errorf("supertraits = %q, want %q", props["supertraits"], "Eq, Clone")
	}
	wantExtends := map[string]bool{"Eq": true, "Clone": true}
	for _, tt := range extendsTargets {
		delete(wantExtends, tt)
	}
	if len(wantExtends) != 0 {
		t.Errorf("missing EXTENDS edges %v; got %v", wantExtends, extendsTargets)
	}
}

func TestRustExtractor_TraitGenerics_Deep(t *testing.T) {
	src := `
trait Container<T> {
    fn get(&self) -> T;
    fn put(&mut self, item: T);
}
`
	props := findRustEntity(t, src, "trait", "Container")
	if props["methods"] != "get, put" {
		t.Errorf("methods = %q, want %q", props["methods"], "get, put")
	}
	if props["generics"] != "T" {
		t.Errorf("generics = %q, want %q", props["generics"], "T")
	}
}

// type_alias_extraction: assert alias name, aliased type, and generic params.
func TestRustExtractor_TypeAliasGenerics_Deep(t *testing.T) {
	src := `type Pair<T> = (T, T);`
	props := findRustEntity(t, src, "type_alias", "Pair")
	if props["generics"] != "T" {
		t.Errorf("generics = %q, want %q", props["generics"], "T")
	}
	if props["aliased_type"] != "(T, T)" {
		t.Errorf("aliased_type = %q, want %q", props["aliased_type"], "(T, T)")
	}
}

// type_extraction (struct): assert named field idents, generics, derives.
func TestRustExtractor_StructFields_Deep(t *testing.T) {
	src := `
#[derive(Serialize, Deserialize)]
struct User<T> {
    id: u32,
    name: String,
    extra: T,
}
`
	props := findRustEntity(t, src, "struct", "User")
	if props["fields"] != "id, name, extra" {
		t.Errorf("fields = %q, want %q", props["fields"], "id, name, extra")
	}
	if props["generics"] != "T" {
		t.Errorf("generics = %q, want %q", props["generics"], "T")
	}
	if props["derives"] != "Serialize, Deserialize" {
		t.Errorf("derives = %q, want %q", props["derives"], "Serialize, Deserialize")
	}
}

// type_extraction: tuple struct fields are positional indices; unit struct has none.
func TestRustExtractor_TupleAndUnitStruct_Deep(t *testing.T) {
	src := `
struct Point(i32, i32);
struct Marker;
`
	tuple := findRustEntity(t, src, "struct", "Point")
	if tuple["fields"] != "0, 1" {
		t.Errorf("tuple fields = %q, want %q", tuple["fields"], "0, 1")
	}
	unit := findRustEntity(t, src, "struct", "Marker")
	if unit["fields"] != "" {
		t.Errorf("unit struct should have no fields, got %q", unit["fields"])
	}
}

func TestRustExtractor_TypeAlias_Properties(t *testing.T) {
	src := `type UserId = u64;`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("rust")
	if !ok {
		t.Fatal("rust extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "types.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Subtype == "type_alias" && e.Name == "UserId" {
			if e.Properties["aliased_type"] != "u64" {
				t.Errorf("expected aliased_type=u64, got %q", e.Properties["aliased_type"])
			}
			return
		}
	}
	t.Error("did not find UserId type_alias entity")
}
