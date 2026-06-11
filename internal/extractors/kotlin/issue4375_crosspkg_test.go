// issue4375_crosspkg_test.go — end-to-end live-pipeline validation for #4375.
//
// Bug: Kotlin cross-package calls reach a function/method through a qualifier on
// a navigation_expression — a fully-qualified
// `com.app.services.OrderService.place()`, an imported top-level function
// (`import com.app.services.placeOrder; placeOrder()`), an imported type member
// (`import com.app.services.Orders; Orders.place()`), an aliased import
// (`import com.app.services.OrderService as Svc; Svc.place()`), or a same-package
// companion/object member (`OrderService.create()`). The extractor's call target
// is the trailing simple_identifier of the navigation chain, so a multi-segment
// qualified call collapses to the bare leaf method name (`place`). The bare leaf
// resolves through the global byName index, which goes AMBIGUOUS the moment two
// packages define a same-named function/type (`OrderService.place` in both
// com.app.services and com.app.billing) — so the CALLS edge dropped and the
// callee package looked falsely uncalled. This is the Kotlin analogue of the Go
// cross-package (#4332), Rust cross-module (#4373), and C# cross-namespace
// (#4374) qualifier drops.
//
// These tests drive the REAL extraction + resolver passes — the same sequence
// cmd/archigraph/index.go runs (extract per file → stamp deterministic IDs →
// BuildIndex → ResolveKotlinCrossPackageCalls → ReferencesEmbedded) — on a
// faithful multi-package project that reproduces a cross-package call WITH a
// same-named type/function collision in two packages. The collision is
// essential: a globally-unique name would false-pass through the bare-name
// fallback (the exact trap #4332 documents).
package kotlin_test

import (
	"context"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tskotlin "github.com/smacker/go-tree-sitter/kotlin"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/kotlin"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"
)

func extractKotlinProjectForTest(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("kotlin")
	if !ok {
		t.Fatal("kotlin extractor not registered")
	}
	var merged []types.EntityRecord
	for rel, src := range files {
		parser := sitter.NewParser()
		parser.SetLanguage(tskotlin.GetLanguage())
		tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		ents, err := ext.Extract(context.Background(), extractor.FileInput{
			Path: rel, Language: "kotlin", Content: []byte(src), Tree: tree,
		})
		if err != nil {
			t.Fatalf("extract %s: %v", rel, err)
		}
		merged = append(merged, ents...)
	}
	for k := range merged {
		if merged[k].Name == "" {
			continue
		}
		merged[k].ID = graph.EntityID("acme/shop", merged[k].Kind,
			merged[k].Name, merged[k].SourceFile)
	}
	return merged
}

func ktIs16Hex(s string) bool {
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

func runKotlinResolve(merged []types.EntityRecord) int {
	idx := resolve.BuildIndex(merged)
	n := idx.ResolveKotlinCrossPackageCalls(merged)
	resolve.ReferencesEmbedded(merged, idx)
	return n
}

// ktCallEdge returns the ToID + props of the CALLS edge whose call_leaf == leaf
// (or bare ToID == leaf before resolution) emitted from the operation named
// `caller` in `srcFile`.
func ktCallEdge(merged []types.EntityRecord, srcFile, caller, leaf string) (string, map[string]string, bool) {
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
				return r.ToID, r.Properties, true
			}
		}
	}
	return "", nil, false
}

func ktEntID(merged []types.EntityRecord, srcFile, name string) string {
	for k := range merged {
		if merged[k].SourceFile == srcFile && merged[k].Name == name {
			return merged[k].ID
		}
	}
	return ""
}

// colliding callee packages: com.app.services.OrderService.place AND
// com.app.billing.OrderService.place — same Type AND same method in two distinct
// packages, PLUS a colliding top-level function placeOrder in both. Only the
// package qualifier on the call can pick the right one.
func ktCollidingCalleeFiles() map[string]string {
	return map[string]string{
		"src/services/OrderService.kt": "" +
			"package com.app.services\n" +
			"\n" +
			"class OrderService {\n" +
			"    fun place() {}\n" +
			"    fun create() {}\n" +
			"}\n" +
			"\n" +
			"fun placeOrder() {}\n",
		"src/billing/OrderService.kt": "" +
			"package com.app.billing\n" +
			"\n" +
			"class OrderService {\n" +
			"    fun place() {}\n" +
			"}\n" +
			"\n" +
			"fun placeOrder() {}\n",
	}
}

// TestIssue4375_FullyQualified — `com.app.services.OrderService.place()`.
func TestIssue4375_FullyQualified(t *testing.T) {
	files := ktCollidingCalleeFiles()
	files["src/app/Caller.kt"] = "" +
		"package com.app.app\n" +
		"\n" +
		"class Caller {\n" +
		"    fun run() {\n" +
		"        com.app.services.OrderService.place()\n" +
		"    }\n" +
		"}\n"
	merged := extractKotlinProjectForTest(t, files)

	// BEFORE: extractor stamped the qualifier; the bare leaf alone is ambiguous.
	_, props, ok := ktCallEdge(merged, "src/app/Caller.kt", "run", "place")
	if !ok {
		t.Fatal("CALLS edge to place not found")
	}
	if props["kotlin_call_pkg"] != "com.app.services" || props["kotlin_call_type"] != "OrderService" {
		t.Fatalf("expected stamped pkg=com.app.services type=OrderService, got pkg=%q type=%q",
			props["kotlin_call_pkg"], props["kotlin_call_type"])
	}

	n := runKotlinResolve(merged)
	if n < 1 {
		t.Fatalf("expected >=1 kotlin cross-package rewrite, got %d", n)
	}
	want := ktEntID(merged, "src/services/OrderService.kt", "place")
	collide := ktEntID(merged, "src/billing/OrderService.kt", "place")
	got, _, _ := ktCallEdge(merged, "src/app/Caller.kt", "run", "place")
	if !ktIs16Hex(got) {
		t.Fatalf("ToID not resolved to a hex id: %q", got)
	}
	if got != want {
		t.Fatalf("resolved to wrong package: got %q want %q (collision=%q)", got, want, collide)
	}
	if got == collide {
		t.Fatal("resolved to the COLLIDING billing package — the bug")
	}
}

// TestIssue4375_ImportedTopLevelFunc — `import com.app.services.placeOrder; placeOrder()`.
func TestIssue4375_ImportedTopLevelFunc(t *testing.T) {
	files := ktCollidingCalleeFiles()
	files["src/app/Caller.kt"] = "" +
		"package com.app.app\n" +
		"\n" +
		"import com.app.services.placeOrder\n" +
		"\n" +
		"class Caller {\n" +
		"    fun run() {\n" +
		"        placeOrder()\n" +
		"    }\n" +
		"}\n"
	merged := extractKotlinProjectForTest(t, files)

	_, props, ok := ktCallEdge(merged, "src/app/Caller.kt", "run", "placeOrder")
	if !ok {
		t.Fatal("CALLS edge to placeOrder not found")
	}
	if props["kotlin_call_pkg"] != "com.app.services" {
		t.Fatalf("expected stamped pkg=com.app.services, got %q", props["kotlin_call_pkg"])
	}
	if props["kotlin_call_type"] != "" {
		t.Fatalf("top-level fn must NOT stamp a type, got %q", props["kotlin_call_type"])
	}

	n := runKotlinResolve(merged)
	if n < 1 {
		t.Fatalf("expected >=1 rewrite, got %d", n)
	}
	want := ktEntID(merged, "src/services/OrderService.kt", "placeOrder")
	collide := ktEntID(merged, "src/billing/OrderService.kt", "placeOrder")
	got, _, _ := ktCallEdge(merged, "src/app/Caller.kt", "run", "placeOrder")
	if got != want {
		t.Fatalf("resolved to wrong package: got %q want %q", got, want)
	}
	if got == collide {
		t.Fatal("resolved to the COLLIDING billing placeOrder — the bug")
	}
}

// TestIssue4375_ImportedTypeMember — `import com.app.services.OrderService; OrderService.place()`.
func TestIssue4375_ImportedTypeMember(t *testing.T) {
	files := ktCollidingCalleeFiles()
	files["src/app/Caller.kt"] = "" +
		"package com.app.app\n" +
		"\n" +
		"import com.app.services.OrderService\n" +
		"\n" +
		"class Caller {\n" +
		"    fun run() {\n" +
		"        OrderService.place()\n" +
		"    }\n" +
		"}\n"
	merged := extractKotlinProjectForTest(t, files)

	_, props, ok := ktCallEdge(merged, "src/app/Caller.kt", "run", "place")
	if !ok {
		t.Fatal("CALLS edge to place not found")
	}
	if props["kotlin_call_type"] != "OrderService" {
		t.Fatalf("expected stamped type=OrderService, got %q", props["kotlin_call_type"])
	}

	n := runKotlinResolve(merged)
	if n < 1 {
		t.Fatalf("expected >=1 rewrite, got %d", n)
	}
	want := ktEntID(merged, "src/services/OrderService.kt", "place")
	collide := ktEntID(merged, "src/billing/OrderService.kt", "place")
	got, _, _ := ktCallEdge(merged, "src/app/Caller.kt", "run", "place")
	if got != want {
		t.Fatalf("resolved to wrong package: got %q want %q", got, want)
	}
	if got == collide {
		t.Fatal("resolved to the COLLIDING billing package — the bug")
	}
}

// TestIssue4375_AliasedImport — `import com.app.services.OrderService as Svc; Svc.place()`.
func TestIssue4375_AliasedImport(t *testing.T) {
	files := ktCollidingCalleeFiles()
	files["src/app/Caller.kt"] = "" +
		"package com.app.app\n" +
		"\n" +
		"import com.app.services.OrderService as Svc\n" +
		"\n" +
		"class Caller {\n" +
		"    fun run() {\n" +
		"        Svc.place()\n" +
		"    }\n" +
		"}\n"
	merged := extractKotlinProjectForTest(t, files)

	_, props, ok := ktCallEdge(merged, "src/app/Caller.kt", "run", "place")
	if !ok {
		t.Fatal("CALLS edge to place not found")
	}
	if props["kotlin_call_type"] != "OrderService" {
		t.Fatalf("aliased import should map Svc->OrderService, got type=%q", props["kotlin_call_type"])
	}
	if !strings.Contains(props["kotlin_call_pkg"], "com.app.services") {
		t.Fatalf("aliased import should include com.app.services candidate, got pkg=%q", props["kotlin_call_pkg"])
	}

	n := runKotlinResolve(merged)
	if n < 1 {
		t.Fatalf("expected >=1 rewrite, got %d", n)
	}
	want := ktEntID(merged, "src/services/OrderService.kt", "place")
	collide := ktEntID(merged, "src/billing/OrderService.kt", "place")
	got, _, _ := ktCallEdge(merged, "src/app/Caller.kt", "run", "place")
	if got != want {
		t.Fatalf("resolved to wrong package: got %q want %q", got, want)
	}
	if got == collide {
		t.Fatal("resolved to the COLLIDING billing package — the bug")
	}
}

// TestIssue4375_SamePackageObjectMember — a companion/object member call where
// the object lives in the SAME package as the caller, recovered via the file
// package even with no import. Uses an `object` so the member is statically
// reachable as `Registry.lookup()`.
func TestIssue4375_SamePackageObjectMember(t *testing.T) {
	files := map[string]string{
		// Same name `Registry.lookup` collides across two packages.
		"src/services/Registry.kt": "" +
			"package com.app.services\n" +
			"\n" +
			"object Registry {\n" +
			"    fun lookup() {}\n" +
			"}\n",
		"src/billing/Registry.kt": "" +
			"package com.app.billing\n" +
			"\n" +
			"object Registry {\n" +
			"    fun lookup() {}\n" +
			"}\n",
		// Caller is in com.app.services — same package as the intended Registry.
		"src/services/Caller.kt": "" +
			"package com.app.services\n" +
			"\n" +
			"class Caller {\n" +
			"    fun run() {\n" +
			"        Registry.lookup()\n" +
			"    }\n" +
			"}\n",
	}
	merged := extractKotlinProjectForTest(t, files)

	_, props, ok := ktCallEdge(merged, "src/services/Caller.kt", "run", "lookup")
	if !ok {
		t.Fatal("CALLS edge to lookup not found")
	}
	if props["kotlin_call_type"] != "Registry" {
		t.Fatalf("expected stamped type=Registry, got %q", props["kotlin_call_type"])
	}

	n := runKotlinResolve(merged)
	if n < 1 {
		t.Fatalf("expected >=1 rewrite, got %d", n)
	}
	want := ktEntID(merged, "src/services/Registry.kt", "lookup")
	collide := ktEntID(merged, "src/billing/Registry.kt", "lookup")
	got, _, _ := ktCallEdge(merged, "src/services/Caller.kt", "run", "lookup")
	if got != want {
		t.Fatalf("same-package object member resolved wrong: got %q want %q", got, want)
	}
	if got == collide {
		t.Fatal("resolved to the COLLIDING billing package — the bug")
	}
}

// TestIssue4375_NegativeInstanceReceiver — an instance receiver whose type is
// NOT statically recoverable must NOT be stamped as a cross-package static
// qualifier (no false bind).
//
// NOTE (#4687): the original form of this test used `val order = OrderService();
// order.place()` and asserted NO stamp. That case is now DELIBERATELY upgraded —
// a local constructed via a ctor call IS receiver-typed (the test→endpoint
// coverage-linkage win, validated in TestIssue4375_LocalCtorReceiverNowTyped and
// issue4687_localvar_receiver_test.go). The genuine negative is a receiver whose
// class can't be statically recovered: a factory-returned local stays bare.
func TestIssue4375_NegativeInstanceReceiver(t *testing.T) {
	files := ktCollidingCalleeFiles()
	files["src/app/Caller.kt"] = "" +
		"package com.app.app\n" +
		"\n" +
		"import com.app.services.OrderService\n" +
		"\n" +
		"class Caller {\n" +
		"    fun run() {\n" +
		"        val order = makeOrder()\n" +
		"        order.place()\n" +
		"    }\n" +
		"}\n"
	merged := extractKotlinProjectForTest(t, files)

	_, props, ok := ktCallEdge(merged, "src/app/Caller.kt", "run", "place")
	if !ok {
		t.Fatal("CALLS edge to place not found")
	}
	if props["kotlin_call_pkg"] != "" || props["kotlin_call_type"] != "" {
		t.Fatalf("factory-returned instance receiver must NOT be stamped, got pkg=%q type=%q",
			props["kotlin_call_pkg"], props["kotlin_call_type"])
	}
}

// TestIssue4375_LocalCtorReceiverNowTyped — #4687: a local constructed via a
// Kotlin ctor call (`val order = OrderService(); order.place()`) IS now
// receiver-typed and binds to the imported OrderService's method (the
// com.app.services one, NOT the colliding com.app.billing one). This is the
// instance-receiver case the original #4375 negative test guarded — deliberately
// upgraded for the test→CALLS→handler→endpoint coverage-linkage program.
func TestIssue4375_LocalCtorReceiverNowTyped(t *testing.T) {
	files := ktCollidingCalleeFiles()
	files["src/app/Caller.kt"] = "" +
		"package com.app.app\n" +
		"\n" +
		"import com.app.services.OrderService\n" +
		"\n" +
		"class Caller {\n" +
		"    fun run() {\n" +
		"        val order = OrderService()\n" +
		"        order.place()\n" +
		"    }\n" +
		"}\n"
	merged := extractKotlinProjectForTest(t, files)

	_, props, ok := ktCallEdge(merged, "src/app/Caller.kt", "run", "place")
	if !ok {
		t.Fatal("CALLS edge to place not found")
	}
	if props["kotlin_call_type"] != "OrderService" {
		t.Fatalf("ctor-local receiver should be typed OrderService, got type=%q", props["kotlin_call_type"])
	}
	runKotlinResolve(merged)
	toID, _, _ := ktCallEdge(merged, "src/app/Caller.kt", "run", "place")
	want := ktEntID(merged, "src/services/OrderService.kt", "place")
	if toID != want || !ktIs16Hex(toID) {
		t.Fatalf("ctor-local receiver did not bind to com.app.services OrderService.place: got %q want %q", toID, want)
	}
}

// TestIssue4375_NegativeStarImport — a star import makes the type non-unique;
// an unqualified `Type.method()` under a star import must NOT be stamped
// (conservative skip, #4334 follow-up).
func TestIssue4375_NegativeStarImport(t *testing.T) {
	files := ktCollidingCalleeFiles()
	files["src/app/Caller.kt"] = "" +
		"package com.app.app\n" +
		"\n" +
		"import com.app.services.*\n" +
		"\n" +
		"class Caller {\n" +
		"    fun run() {\n" +
		"        OrderService.place()\n" +
		"    }\n" +
		"}\n"
	merged := extractKotlinProjectForTest(t, files)

	_, props, ok := ktCallEdge(merged, "src/app/Caller.kt", "run", "place")
	if !ok {
		t.Fatal("CALLS edge to place not found")
	}
	if props["kotlin_call_pkg"] != "" {
		t.Fatalf("star-import unqualified call must NOT be stamped, got pkg=%q", props["kotlin_call_pkg"])
	}
}
