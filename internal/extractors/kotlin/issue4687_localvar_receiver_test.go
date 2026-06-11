// issue4687_localvar_receiver_test.go — exact-mirror fixtures for the Kotlin
// slice of epic #4615 (all-framework test→endpoint coverage linkage).
//
// Generalises the TS/JS (#4680), Python (#4716), Java (#4682/#4717), Go
// (#4683/#4718), Ruby (#4719), C# (#4720) and PHP (#4721) local-variable
// receiver-typing + test→CALLS→handler coverage wins to Kotlin.
//
// PROBE pre-state (RED): `val c = XController(svc); c.getCounts()` in a `@Test
// fun` emitted a CALLS edge carrying NO kotlin_call_type — the navigation
// resolver's instance-receiver guard dropped the typed local, so the bare
// `getCounts` could not bind cross-file and the controller stayed uncovered.
// Kotest specs (`class FooSpec : StringSpec({ … })`) emitted ZERO CALLS for the
// whole spec because the example logic lives in an anonymous constructor lambda,
// not a `@Test fun`. JUnit5 `@Test fun` ARE named operations already mined by
// walk(), so only the typed-receiver binding (gap 1/2) and the Kotest
// anonymous-lambda owner (gap 3) were RED.
//
// These tests drive the REAL extraction + resolver passes (the same sequence
// cmd/archigraph/index.go runs) on a faithful multi-package project that
// reproduces a cross-file SUT call WITH a same-named method collision in two
// packages — so a globally-unique name cannot false-pass through the bare-name
// fallback.
package kotlin_test

import "testing"

// Two controllers in different packages both declare getCounts() — only the
// receiver TYPE picks the right one.
func ktTwoControllerFiles() map[string]string {
	return map[string]string{
		"src/web/XController.kt": "" +
			"package com.app.web\n" +
			"class XController(val svc: Svc) {\n" +
			"    fun getCounts(): Int { return 1 }\n" +
			"}\n",
		"src/admin/XController.kt": "" +
			"package com.app.admin\n" +
			"class XController {\n" +
			"    fun getCounts(): Int { return 2 }\n" +
			"}\n",
	}
}

// A: `val c = XController(svc); c.getCounts()` in a @Test fun → CALLS to the
// imported web controller's getCounts (NOT the admin one).
func TestIssue4687_LocalVarCtorReceiver(t *testing.T) {
	files := ktTwoControllerFiles()
	files["src/test/XControllerTest.kt"] = "" +
		"package com.app.test\n" +
		"import com.app.web.XController\n" +
		"class XControllerTest {\n" +
		"    @Test fun getCountsWorks() {\n" +
		"        val c = XController(Svc())\n" +
		"        c.getCounts()\n" +
		"    }\n" +
		"}\n"
	merged := extractKotlinProjectForTest(t, files)

	_, props, ok := ktCallEdge(merged, "src/test/XControllerTest.kt", "getCountsWorks", "getCounts")
	if !ok {
		t.Fatal("CALLS edge to getCounts not found")
	}
	if props["kotlin_call_type"] != "XController" {
		t.Fatalf("expected typed receiver XController, got type=%q", props["kotlin_call_type"])
	}
	runKotlinResolve(merged)
	toID, _, _ := ktCallEdge(merged, "src/test/XControllerTest.kt", "getCountsWorks", "getCounts")
	want := ktEntID(merged, "src/web/XController.kt", "getCounts")
	if toID != want || !ktIs16Hex(toID) {
		t.Fatalf("CALLS did not bind to web XController.getCounts: got %q want %q", toID, want)
	}
}

// A': explicit type annotation `val c: XController = makeIt()` types the same way.
func TestIssue4687_LocalVarTypeAnnotationReceiver(t *testing.T) {
	files := ktTwoControllerFiles()
	files["src/test/XControllerTest.kt"] = "" +
		"package com.app.test\n" +
		"import com.app.web.XController\n" +
		"class XControllerTest {\n" +
		"    @Test fun getCountsWorks() {\n" +
		"        val c: XController = makeIt()\n" +
		"        c.getCounts()\n" +
		"    }\n" +
		"}\n"
	merged := extractKotlinProjectForTest(t, files)
	runKotlinResolve(merged)
	toID, _, _ := ktCallEdge(merged, "src/test/XControllerTest.kt", "getCountsWorks", "getCounts")
	want := ktEntID(merged, "src/web/XController.kt", "getCounts")
	if toID != want {
		t.Fatalf("type-annotation receiver did not bind: got %q want %q", toID, want)
	}
}

// MockK: `@InjectMockKs val controller = XController()` field + `val c =
// mockk<XController>()` local both type the receiver to the controller class.
func TestIssue4687_MockKReceiverTyping(t *testing.T) {
	files := ktTwoControllerFiles()
	files["src/test/XControllerTest.kt"] = "" +
		"package com.app.test\n" +
		"import com.app.web.XController\n" +
		"class XControllerTest {\n" +
		"    @InjectMockKs val controller = XController(Svc())\n" +
		"    @Test fun fieldReceiver() {\n" +
		"        controller.getCounts()\n" +
		"    }\n" +
		"    @Test fun mockkLocal() {\n" +
		"        val c = mockk<XController>()\n" +
		"        c.getCounts()\n" +
		"    }\n" +
		"}\n"
	merged := extractKotlinProjectForTest(t, files)
	runKotlinResolve(merged)
	want := ktEntID(merged, "src/web/XController.kt", "getCounts")

	fieldID, _, _ := ktCallEdge(merged, "src/test/XControllerTest.kt", "fieldReceiver", "getCounts")
	if fieldID != want {
		t.Fatalf("@InjectMockKs field receiver did not bind: got %q want %q", fieldID, want)
	}
	mockID, _, _ := ktCallEdge(merged, "src/test/XControllerTest.kt", "mockkLocal", "getCounts")
	if mockID != want {
		t.Fatalf("mockk<XController>() local receiver did not bind: got %q want %q", mockID, want)
	}
}

// Kotest StringSpec: example logic in the anonymous constructor lambda is owned
// by a test_scope SCOPE.Operation; its receiver-typed CALLS bind cross-file.
func TestIssue4687_KotestScopeOwner(t *testing.T) {
	files := ktTwoControllerFiles()
	files["src/test/CountSpec.kt"] = "" +
		"package com.app.test\n" +
		"import io.kotest.core.spec.style.StringSpec\n" +
		"import com.app.web.XController\n" +
		"class CountSpec : StringSpec({\n" +
		"    \"getCounts works\" {\n" +
		"        val c = XController(Svc())\n" +
		"        c.getCounts()\n" +
		"    }\n" +
		"})\n"
	merged := extractKotlinProjectForTest(t, files)

	var owner bool
	for _, e := range merged {
		if e.Subtype == "test_scope" && e.Name == "CountSpec" {
			owner = true
			if e.Properties["framework"] != "kotest" {
				t.Errorf("expected framework=kotest, got %q", e.Properties["framework"])
			}
		}
	}
	if !owner {
		t.Fatal("no Kotest test_scope owner emitted")
	}
	runKotlinResolve(merged)
	toID, _, _ := ktCallEdge(merged, "src/test/CountSpec.kt", "CountSpec", "getCounts")
	want := ktEntID(merged, "src/web/XController.kt", "getCounts")
	if toID != want {
		t.Fatalf("Kotest scope CALLS did not bind to web XController.getCounts: got %q want %q", toID, want)
	}
}

// NEGATIVE: a factory-returning receiver (`val c = makeController()`) whose class
// is not statically recoverable stays BARE — no kotlin_call_type stamp, honest
// exclusion (the resolver must not guess a type).
func TestIssue4687_FactoryReceiverStaysBare(t *testing.T) {
	files := ktTwoControllerFiles()
	files["src/test/XControllerTest.kt"] = "" +
		"package com.app.test\n" +
		"import com.app.web.XController\n" +
		"class XControllerTest {\n" +
		"    @Test fun viaFactory() {\n" +
		"        val c = makeController()\n" +
		"        c.getCounts()\n" +
		"    }\n" +
		"}\n"
	merged := extractKotlinProjectForTest(t, files)
	_, props, ok := ktCallEdge(merged, "src/test/XControllerTest.kt", "viaFactory", "getCounts")
	if !ok {
		t.Fatal("CALLS edge to getCounts not found")
	}
	if props["kotlin_call_type"] != "" {
		t.Fatalf("factory receiver must NOT be typed, got type=%q", props["kotlin_call_type"])
	}
}

// NEGATIVE: a bare `mockk()` with no static type argument stays bare.
func TestIssue4687_UntypedMockkStaysBare(t *testing.T) {
	files := ktTwoControllerFiles()
	files["src/test/XControllerTest.kt"] = "" +
		"package com.app.test\n" +
		"import com.app.web.XController\n" +
		"class XControllerTest {\n" +
		"    @Test fun viaUntypedMock() {\n" +
		"        val c = mockk()\n" +
		"        c.getCounts()\n" +
		"    }\n" +
		"}\n"
	merged := extractKotlinProjectForTest(t, files)
	_, props, _ := ktCallEdge(merged, "src/test/XControllerTest.kt", "viaUntypedMock", "getCounts")
	if props["kotlin_call_type"] != "" {
		t.Fatalf("untyped mockk() receiver must NOT be typed, got type=%q", props["kotlin_call_type"])
	}
}

// NEGATIVE: a shape-only Kotest spec (no production calls) emits NO scope owner.
func TestIssue4687_ShapeOnlyKotestNoOwner(t *testing.T) {
	files := map[string]string{
		"src/test/EmptySpec.kt": "" +
			"package com.app.test\n" +
			"import io.kotest.core.spec.style.StringSpec\n" +
			"class EmptySpec : StringSpec({\n" +
			"    \"adds\" {\n" +
			"        (1 + 1) shouldBe 2\n" +
			"    }\n" +
			"})\n",
	}
	merged := extractKotlinProjectForTest(t, files)
	for _, e := range merged {
		if e.Subtype == "test_scope" {
			t.Fatalf("shape-only spec must not emit a test_scope owner, got %q", e.Name)
		}
	}
}
