package java_test

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/java"
)

// Issue #4359 LIVE-REPRO.
//
// Runs the ACTUAL registered Java custom extractor (`custom_java_patterns`,
// which performs framework detection + dispatches every ExtractXxx incl.
// ExtractJUnit5) AND the ACTUAL resolve.BuildIndex symbol table over faithful
// byte-source JUnit5 / JUnit4 / TestNG test classes paired with their
// production class, and asserts:
//
//	(a) the per-@Test / per-lifecycle / per-@Nested / per-@ExtendWith orphan
//	    nodes that dominated the Java test orphan ring are GONE — exactly ONE
//	    test_suite entity per test-class file, no per-method/per-lifecycle
//	    standalone SCOPE.Operation nodes;
//	(b) a TESTS edge is emitted from the suite to the production class under
//	    test, and that edge's `Class:<Subject>` stub RESOLVES against the real
//	    production entity in the symbol table (so the suite is not an orphan).
//
// The pre-fix behaviour emitted one SCOPE.Operation per @Test + per lifecycle
// method + one SCOPE.Component per @Nested + one SCOPE.Pattern per @ExtendWith,
// hung off a synthesised carrier, with ZERO edge to the SUT → orphan ring.

func javaCustomExtract(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_java_patterns")
	if !ok {
		t.Fatal("custom_java_patterns not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "java", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return ents
}

func testsEdges4359(ents []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindTests) {
				out = append(out, r)
			}
		}
	}
	return out
}

// assertNoOrphanNoise fails if any per-method/per-lifecycle/per-nested/per-
// extension standalone test node is still emitted, and returns the suite count.
func assertNoOrphanNoise4359(t *testing.T, ents []types.EntityRecord) int {
	t.Helper()
	suiteCount := 0
	for _, e := range ents {
		// The pre-fix provenances for the now-collapsed nodes.
		switch e.Properties["provenance"] {
		case "INFERRED_FROM_JUNIT5_NESTED",
			"INFERRED_FROM_JUNIT5_LIFECYCLE",
			"INFERRED_FROM_JUNIT5_EXTENSION":
			t.Errorf("orphan noise node still emitted: provenance=%s name=%q (#4359)",
				e.Properties["provenance"], e.Name)
		}
		// A standalone per-@Test SCOPE.Operation (the old test-method node).
		if e.Kind == "SCOPE.Operation" && e.Properties["test_annotation"] != "" {
			t.Errorf("orphan per-@Test SCOPE.Operation still emitted: name=%q (#4359)", e.Name)
		}
		if e.Subtype == "test_suite" {
			suiteCount++
		}
	}
	return suiteCount
}

// resolvesToProd asserts the `Class:<subject>` stub on the TESTS edge resolves
// to the production class entity via the real symbol table.
func resolvesToProd(t *testing.T, suiteEnts []types.EntityRecord, subject, prodSrcFile string) {
	t.Helper()
	wantStub := "Class:" + subject
	edges := testsEdges4359(suiteEnts)
	if len(edges) == 0 {
		t.Fatalf("no TESTS edge emitted (#4359) — suite node would be an orphan")
	}
	found := false
	for _, e := range edges {
		if e.ToID == wantStub {
			found = true
		}
	}
	if !found {
		t.Fatalf("TESTS edge to %q not found; edges=%v", wantStub, edges)
	}

	prod := types.EntityRecord{
		Name:       subject,
		Kind:       "SCOPE.Component",
		Subtype:    "class",
		SourceFile: prodSrcFile,
		Language:   "java",
		Properties: map[string]string{"kind": "SCOPE.Component", "subtype": "class"},
	}
	prod.ID = prod.ComputeID()

	idx := resolve.BuildIndex(append(append([]types.EntityRecord{}, suiteEnts...), prod))
	gotID, ok := idx.Lookup(wantStub)
	if !ok {
		t.Fatalf("symbol table did NOT resolve %q — TESTS edge would stay orphan", wantStub)
	}
	if gotID != prod.ID {
		t.Fatalf("resolved %q to %s, want production class %s", wantStub, gotID, prod.ID)
	}
}

// ── JUnit 5 + Mockito @InjectMocks SUT inference ────────────────────────────

func TestIssue4359_JUnit5_InjectMocks_NoOrphan_AndTestsEdge(t *testing.T) {
	const src = `
package com.example.order;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import static org.junit.jupiter.api.Assertions.*;

@ExtendWith(MockitoExtension.class)
class OrderServiceTest {

    @Mock OrderRepository repo;
    @InjectMocks OrderService subject;

    @BeforeEach
    void setUp() {}

    @Test
    void placeReturnsSaved() {
        assertNotNull(subject);
        assertEquals(1, 1);
    }

    @Test
    void placePersists() {
        assertTrue(true);
    }

    @Nested
    class WhenInvalid {
        @Test
        void rejects() {}
    }
}
`
	ents := javaCustomExtract(t, "src/test/java/com/example/order/OrderServiceTest.java", src)
	for _, e := range ents {
		t.Logf("ENTITY kind=%s subtype=%s name=%q prov=%s rels=%d",
			e.Kind, e.Subtype, e.Name, e.Properties["provenance"], len(e.Relationships))
	}

	if n := assertNoOrphanNoise4359(t, ents); n != 1 {
		t.Errorf("expected exactly 1 test_suite, got %d (#4359)", n)
	}
	resolvesToProd(t, ents, "OrderService",
		"src/main/java/com/example/order/OrderService.java")
}

// ── JUnit 4 + `new SUT(...)` SUT inference ──────────────────────────────────

func TestIssue4359_JUnit4_NewSUT_NoOrphan_AndTestsEdge(t *testing.T) {
	const src = `
package com.example.order;

import org.junit.Test;
import org.junit.Before;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import static org.junit.Assert.*;

@RunWith(JUnit4.class)
public class OrderServiceTest {

    private OrderService subject;

    @Before
    public void setUp() {
        subject = new OrderService(null);
    }

    @Test
    public void placeReturnsSaved() {
        assertNotNull(subject);
    }

    @Test
    public void placePersists() {
        assertEquals(1, 1);
    }
}
`
	ents := javaCustomExtract(t, "src/test/java/com/example/order/OrderServiceTest.java", src)
	if n := assertNoOrphanNoise4359(t, ents); n != 1 {
		t.Errorf("expected exactly 1 test_suite, got %d (#4359)", n)
	}
	resolvesToProd(t, ents, "OrderService",
		"src/main/java/com/example/order/OrderService.java")
}

// ── TestNG + `new SUT(...)` SUT inference ───────────────────────────────────

func TestIssue4359_TestNG_NewSUT_NoOrphan_AndTestsEdge(t *testing.T) {
	const src = `
package com.example.order;

import org.testng.annotations.Test;
import org.testng.annotations.BeforeMethod;
import static org.testng.Assert.*;

public class OrderServiceTest {

    private OrderService subject;

    @BeforeMethod
    public void setUp() {
        subject = new OrderService(null);
    }

    @Test
    public void placeReturnsSaved() {
        assertNotNull(subject);
    }

    @Test(groups = "fast")
    public void placePersists() {
        assertEquals(1, 1);
    }
}
`
	ents := javaCustomExtract(t, "src/test/java/com/example/order/OrderServiceTest.java", src)
	if n := assertNoOrphanNoise4359(t, ents); n != 1 {
		t.Errorf("expected exactly 1 test_suite, got %d (#4359)", n)
	}
	resolvesToProd(t, ents, "OrderService",
		"src/main/java/com/example/order/OrderService.java")
}

// ── Conservatism: no SUT reference → no (mis-)TESTS edge ────────────────────

func TestIssue4359_NoReference_NoTestsEdge(t *testing.T) {
	// FooTest name-affinity-maps to Foo, but Foo is never constructed/injected
	// here — only an unrelated helper. The edge must NOT fire (conservative).
	const src = `
package com.example;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class FooTest {
    @Test
    void doesThing() {
        Helper h = new Helper();
        assertNotNull(h);
    }
}
`
	ents := javaCustomExtract(t, "src/test/java/com/example/FooTest.java", src)
	if n := assertNoOrphanNoise4359(t, ents); n != 1 {
		t.Errorf("expected exactly 1 test_suite, got %d (#4359)", n)
	}
	if edges := testsEdges4359(ents); len(edges) != 0 {
		t.Errorf("expected NO TESTS edge (Foo never referenced), got %v (#4359)", edges)
	}
}

// ── Suite-name namespacing avoids resolver collision with the prod symbol ───

func TestIssue4359_SuiteNameNamespaced(t *testing.T) {
	const src = `
package com.example.order;

import org.junit.jupiter.api.Test;

class OrderServiceTest {
    @Test
    void t() { OrderService s = new OrderService(null); }
}
`
	ents := javaCustomExtract(t, "src/test/java/com/example/order/OrderServiceTest.java", src)
	for _, e := range ents {
		if e.Subtype == "test_suite" {
			// The suite must NOT be named bare "OrderService" (would collide
			// with the prod class in the by-name index and re-orphan both).
			if e.Name == "OrderService" {
				t.Errorf("suite entity name collides with prod symbol %q (#4359)", e.Name)
			}
			if ref := e.Properties["ref"]; !strings.Contains(ref, "junit5_suite") {
				t.Errorf("suite ref not namespaced: %q (#4359)", ref)
			}
		}
	}
}
