// issue4373_crossmod_test.go — end-to-end live-pipeline validation for #4373.
//
// Bug: Rust cross-module / cross-crate calls reach a function through a path
// qualifier on a scoped_identifier (`crate::services::order::place_order()`,
// `self::`/`super::`, an aliased `use ... as ord; ord::place_order()`, or an
// associated `OrderService::new()`). The extractor collapsed the callee to the
// rightmost identifier (`place_order`), dropping the whole path qualifier. The
// bare leaf resolves through the global byName index, which goes AMBIGUOUS the
// moment two modules define a same-named symbol — so the CALLS edge dropped and
// the callee module looked falsely uncalled. This is the Rust analogue of the
// Go cross-package qualifier-drop fixed in #4332.
//
// These tests drive the REAL extraction + resolver passes — the same sequence
// cmd/grafel/index.go runs (extract per file → stamp deterministic IDs →
// BuildIndex → ResolveRustCrossModuleCalls → ReferencesEmbedded) — on a
// faithful multi-module crate (src/lib.rs + src/services/order.rs + …) that
// reproduces a cross-module call WITH a same-named symbol collision in two
// modules. The collision is essential: a globally-unique name would false-pass
// through the bare-name fallback (the exact trap #4332 documents).
package rust_test

import (
	"context"
	"testing"

	tsrust "github.com/smacker/go-tree-sitter/rust"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/rust"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	"github.com/cajasmota/grafel/internal/types"
)

// extractRustCrateForTest extracts every (relPath, src) file with the real
// Rust extractor (real tree-sitter parse + RepoRoot-relative paths so module
// keying fires), stamps deterministic entity IDs like the indexer, and returns
// the merged record slice.
func extractRustCrateForTest(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("rust")
	if !ok {
		t.Fatal("rust extractor not registered")
	}
	var merged []types.EntityRecord
	for rel, src := range files {
		parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsrust.GetLanguage()))
		if err != nil {
			t.Fatalf("parser init %s: %v", rel, err)
		}
		tree, err := parser.Parse([]byte(src))
		if err != nil {
			parser.Close()
			t.Fatalf("parse %s: %v", rel, err)
		}
		ents, err := ext.Extract(context.Background(), extractor.FileInput{
			Path: rel, Language: "rust", Content: []byte(src), TSTree: tree,
		})
		parser.Close()
		if err != nil {
			t.Fatalf("extract %s: %v", rel, err)
		}
		merged = append(merged, ents...)
	}
	for k := range merged {
		if merged[k].Name == "" {
			continue
		}
		merged[k].ID = graph.EntityID("acme/widgets", merged[k].Kind,
			merged[k].Name, merged[k].SourceFile)
	}
	return merged
}

func is16HexRust(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// runRustResolve runs the resolver sequence the indexer uses for cross-module
// call binding.
func runRustResolve(merged []types.EntityRecord) int {
	idx := resolve.BuildIndex(merged)
	n := idx.ResolveRustCrossModuleCalls(merged)
	resolve.ReferencesEmbedded(merged, idx)
	return n
}

// callToID returns the ToID of the CALLS edge for `leaf` emitted from the
// operation named `caller` in file `srcFile`.
func callToID(merged []types.EntityRecord, srcFile, caller, leaf string) (string, bool) {
	for k := range merged {
		e := merged[k]
		if e.SourceFile != srcFile || e.Name != caller {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != "CALLS" {
				continue
			}
			if r.Properties["call_leaf"] == leaf || r.ToID == leaf {
				return r.ToID, true
			}
		}
	}
	return "", false
}

func findEntityID(merged []types.EntityRecord, srcFile, name string) string {
	for k := range merged {
		if merged[k].SourceFile == srcFile && merged[k].Name == name {
			return merged[k].ID
		}
	}
	return ""
}

// TestIssue4373_CrateAbsoluteCall_NameCollision is the core regression. The
// caller invokes `crate::services::order::place_order()`, and a SECOND module
// (services::invoice) ALSO defines `place_order`. Before the fix the qualifier
// was dropped and the bare name went ambiguous → the CALLS edge bound nowhere.
// After the fix it binds to exactly services::order::place_order via the
// stamped module directory.
func TestIssue4373_CrateAbsoluteCall_NameCollision(t *testing.T) {
	// mod.rs layout so the two colliding `place_order` definitions live in
	// DISTINCT package directories (src/services/order vs src/services/invoice)
	// — only the path qualifier can pick the right one; the bare leaf is
	// ambiguous across the two dirs.
	files := map[string]string{
		"src/lib.rs":                  "pub mod services;\n",
		"src/services/mod.rs":         "pub mod order;\npub mod invoice;\n",
		"src/services/order/mod.rs":   "pub fn place_order() -> i32 { 1 }\n",
		"src/services/invoice/mod.rs": "pub fn place_order() -> i32 { 99 }\n",
		"src/handlers.rs": "pub fn handle() -> i32 {\n" +
			"    crate::services::order::place_order()\n}\n",
	}
	merged := extractRustCrateForTest(t, files)

	orderID := findEntityID(merged, "src/services/order/mod.rs", "place_order")
	invoiceID := findEntityID(merged, "src/services/invoice/mod.rs", "place_order")
	if orderID == "" || invoiceID == "" {
		t.Fatalf("missing place_order entities: order=%q invoice=%q", orderID, invoiceID)
	}

	// Confirm the extractor stamped the module qualifier on the cross-module
	// CALLS edge — the structural carrier the resolver consumes.
	stamped := false
	var stampedDirs, stampedLeaf string
	for k := range merged {
		if merged[k].SourceFile != "src/handlers.rs" {
			continue
		}
		for _, r := range merged[k].Relationships {
			if r.Kind == "CALLS" && r.Properties["call_leaf"] == "place_order" {
				stamped = true
				stampedDirs = r.Properties["rust_call_pkg_dirs"]
				stampedLeaf = r.Properties["call_leaf"]
			}
		}
	}
	if !stamped {
		t.Fatal("extractor did not stamp rust_call_pkg_dirs/call_leaf on crate::…::place_order() CALL")
	}
	t.Logf("stamped rust_call_pkg_dirs=%q call_leaf=%q", stampedDirs, stampedLeaf)

	// BEFORE: the edge ToID is the bare leaf (unresolved/ambiguous).
	before, _ := callToID(merged, "src/handlers.rs", "handle", "place_order")
	if is16HexRust(before) {
		t.Fatalf("precondition: edge already a hex id before resolve (%q)", before)
	}
	t.Logf("cross-module CALL place_order ToID before=%q", before)

	n := runRustResolve(merged)
	t.Logf("ResolveRustCrossModuleCalls rewrote=%d", n)

	after, _ := callToID(merged, "src/handlers.rs", "handle", "place_order")
	t.Logf("cross-module CALL place_order ToID after=%q (order=%q invoice=%q)",
		after, orderID, invoiceID)
	switch after {
	case orderID:
		// correct
	case invoiceID:
		t.Fatal("CALL bound to services::invoice::place_order (wrong module — qualifier ignored)")
	default:
		t.Fatalf("cross-module CALL place_order() unresolved/ambiguous after fix, ToID=%q", after)
	}
}

// TestIssue4373_SelfSuperAndAlias covers the self::/super:: relative roots and
// the `use ... as` alias paths, each against a same-named-symbol collision.
func TestIssue4373_SelfSuperAndAlias(t *testing.T) {
	// mod.rs layout: each colliding `run` lives in its own package dir, so the
	// path qualifier (super::invoice vs alias→order vs self::inner) is the only
	// disambiguator. `dispatch` is itself a directory module (dispatch/mod.rs)
	// so self::inner names dispatch's own child module dispatch::inner.
	files := map[string]string{
		"src/lib.rs":             "pub mod app;\n",
		"src/app/mod.rs":         "pub mod order;\npub mod invoice;\npub mod dispatch;\n",
		"src/app/order/mod.rs":   "pub fn run() -> i32 { 1 }\n",  // collision A
		"src/app/invoice/mod.rs": "pub fn run() -> i32 { 99 }\n", // collision B
		"src/app/dispatch/mod.rs": "use crate::app::order as ord;\n" +
			"pub mod inner;\n" +
			"pub fn via_self() -> i32 { self::inner::run() }\n" +
			"pub fn via_super() -> i32 { super::invoice::run() }\n" +
			"pub fn via_alias() -> i32 { ord::run() }\n",
		"src/app/dispatch/inner/mod.rs": "pub fn run() -> i32 { 7 }\n", // collision C
	}
	merged := extractRustCrateForTest(t, files)

	orderRun := findEntityID(merged, "src/app/order/mod.rs", "run")
	invoiceRun := findEntityID(merged, "src/app/invoice/mod.rs", "run")
	innerRun := findEntityID(merged, "src/app/dispatch/inner/mod.rs", "run")
	if orderRun == "" || invoiceRun == "" || innerRun == "" {
		t.Fatalf("missing run entities order=%q invoice=%q inner=%q",
			orderRun, invoiceRun, innerRun)
	}

	runRustResolve(merged)

	// self::inner::run — dispatch's module is app::dispatch (dispatch/mod.rs),
	// so self::inner = app::dispatch::inner = src/app/dispatch/inner.
	gotSelf, _ := callToID(merged, "src/app/dispatch/mod.rs", "via_self", "run")
	t.Logf("self::inner::run ToID=%q want=%q", gotSelf, innerRun)
	if gotSelf != innerRun {
		t.Errorf("self::inner::run() did not bind to app::dispatch::inner::run (got %q)", gotSelf)
	}

	gotSuper, _ := callToID(merged, "src/app/dispatch/mod.rs", "via_super", "run")
	t.Logf("super::invoice::run ToID=%q want=%q", gotSuper, invoiceRun)
	if gotSuper != invoiceRun {
		t.Errorf("super::invoice::run() did not bind to app::invoice::run (got %q)", gotSuper)
	}

	gotAlias, _ := callToID(merged, "src/app/dispatch/mod.rs", "via_alias", "run")
	t.Logf("ord::run (alias of app::order) ToID=%q want=%q", gotAlias, orderRun)
	if gotAlias != orderRun {
		t.Errorf("aliased ord::run() did not bind to app::order::run (got %q)", gotAlias)
	}
}

// TestIssue4373_NoFalseStampOnBareOrReceiverCall guards against over-eager
// stamping: bare calls (`helper()`), receiver/method calls (`self.method()`,
// `repo.find()`) and macro calls must NOT carry rust_call_pkg_dirs.
func TestIssue4373_NoFalseStampOnBareOrReceiverCall(t *testing.T) {
	files := map[string]string{
		"src/lib.rs": "pub mod m;\n",
		"src/m.rs": "pub struct R;\n" +
			"impl R {\n" +
			"  pub fn build(&self) -> i32 { 1 }\n" +
			"  pub fn use_it(&self) -> i32 {\n" +
			"    let x = helper();\n" + // bare call
			"    let y = self.build();\n" + // receiver call
			"    println!(\"{}\", x + y);\n" + // macro call
			"    x + y\n" +
			"  }\n" +
			"}\n" +
			"fn helper() -> i32 { 0 }\n",
	}
	merged := extractRustCrateForTest(t, files)
	for k := range merged {
		for _, r := range merged[k].Relationships {
			if r.Kind == "CALLS" && r.Properties["rust_call_pkg_dirs"] != "" {
				t.Errorf("non-path call %q wrongly stamped rust_call_pkg_dirs=%q",
					r.Properties["call_leaf"], r.Properties["rust_call_pkg_dirs"])
			}
		}
	}
}

// TestIssue4373_TypeAssocCall binds an associated `Type::method()` call across
// modules, with a same-named method collision on a different type.
func TestIssue4373_TypeAssocCall(t *testing.T) {
	files := map[string]string{
		"src/lib.rs":     "pub mod svc;\n",
		"src/svc/mod.rs": "pub mod a;\npub mod b;\n",
		// OrderService::new in module svc::a; a same-named `new` exists on a
		// different type in svc::b — the collision that breaks bare-name bind.
		"src/svc/a.rs": "pub struct OrderService;\n" +
			"impl OrderService { pub fn new() -> OrderService { OrderService } }\n",
		"src/svc/b.rs": "pub struct InvoiceService;\n" +
			"impl InvoiceService { pub fn new() -> InvoiceService { InvoiceService } }\n",
		"src/svc/run.rs": "use crate::svc::a::OrderService;\n" +
			"pub fn go() -> i32 { let _ = OrderService::new(); 0 }\n",
	}
	merged := extractRustCrateForTest(t, files)

	orderNew := findEntityID(merged, "src/svc/a.rs", "OrderService.new")
	invoiceNew := findEntityID(merged, "src/svc/b.rs", "InvoiceService.new")
	if orderNew == "" || invoiceNew == "" {
		t.Fatalf("missing assoc-fn entities order=%q invoice=%q", orderNew, invoiceNew)
	}

	// Stamp check: scope must be the Type.
	scopeStamped := ""
	for k := range merged {
		if merged[k].SourceFile != "src/svc/run.rs" {
			continue
		}
		for _, r := range merged[k].Relationships {
			if r.Kind == "CALLS" && r.Properties["call_leaf"] == "new" {
				scopeStamped = r.Properties["rust_call_scope"]
			}
		}
	}
	if scopeStamped != "OrderService" {
		t.Fatalf("assoc call did not stamp rust_call_scope=OrderService (got %q)", scopeStamped)
	}

	runRustResolve(merged)

	got, _ := callToID(merged, "src/svc/run.rs", "go", "new")
	t.Logf("OrderService::new ToID=%q want=%q (invoice=%q)", got, orderNew, invoiceNew)
	switch got {
	case orderNew:
		// correct
	case invoiceNew:
		t.Fatal("OrderService::new() bound to InvoiceService::new (wrong type/module)")
	default:
		t.Fatalf("OrderService::new() unresolved/ambiguous, ToID=%q", got)
	}
}
