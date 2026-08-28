// references_test.go — coverage for the REFERENCES-emission pass
// (analog of #641/#650/#670 for Kotlin).

package kotlin

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// hasKotlinRef returns true when any entity named fromName carries a
// REFERENCES edge whose ToID contains substr.
func hasKotlinRef(ents []types.EntityRecord, fromName, substr string) bool {
	for _, e := range ents {
		if e.Name != fromName {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" && strings.Contains(r.ToID, substr) {
				return true
			}
		}
	}
	return false
}

// kotlinRelsSummary stringifies every entity's REFERENCES edges for
// failure messages.
func kotlinRelsSummary(ents []types.EntityRecord) string {
	var b strings.Builder
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" {
				b.WriteString(e.Name)
				b.WriteString(" -> ")
				b.WriteString(r.ToID)
				b.WriteString("; ")
			}
		}
	}
	if b.Len() == 0 {
		return "(no REFERENCES)"
	}
	return b.String()
}

// Same-scope identifier resolution: a function body that uses the
// name of a sibling class declared in the same file emits a
// REFERENCES edge. The CALLS path takes precedence over REFERENCES
// when the identifier IS the callee of a call_expression.
func TestKotlinReferencesSameScopeIdentifier(t *testing.T) {
	src := `package com.demo

class Helper

class Caller {
    fun make(): Any {
        val ref: Helper = Helper()
        return ref
    }
}
`
	ents := runKotlinExtract(t, src)
	if !hasKotlinRef(ents, "Caller.make", "Helper") {
		t.Fatalf("expected REFERENCES make -> Helper, got: %s", kotlinRelsSummary(ents))
	}
}

// this.<property> resolution: a function body that uses
// `this.counter` emits a REFERENCES edge. The Kotlin extractor's
// primary pass does not currently emit property entities, so this
// test asserts that the navigation_expression handler at least does
// not crash and that it does not emit a malformed edge. When the
// extractor later emits properties, the dottedSymbols/bareSymbols
// table will pick them up and this test will assert a positive edge.
func TestKotlinReferencesThisProperty(t *testing.T) {
	src := `package com.demo

class Box {
    val counter: Int = 0

    fun bump(): Int {
        return this.counter + 1
    }
}
`
	// Smoke: no panic, primary entities still emitted.
	ents := runKotlinExtract(t, src)
	if len(ents) == 0 {
		t.Fatalf("expected at least the file carrier entity, got 0")
	}
	// Defensive: if a REFERENCES edge IS emitted to "counter", it must
	// land on the bump operation, not on Box or the file carrier.
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" && strings.HasSuffix(r.ToID, ":counter") && e.Name != "Box.bump" {
				t.Errorf("REFERENCES :counter must originate from Box.bump, got from %q", e.Name)
			}
		}
	}
}

// Companion method resolution: a function body inside a companion
// object that references a class-level type emits a REFERENCES edge
// to the enclosing class. The companion's body walks under the
// enclosing class's parentClass so bare-name resolution finds class-
// level members.
func TestKotlinReferencesCompanionMethod(t *testing.T) {
	src := `package com.demo

class Outer {
    companion object {
        fun build(): Outer {
            val x: Outer = Outer()
            return x
        }
    }
}
`
	ents := runKotlinExtract(t, src)
	if !hasKotlinRef(ents, "Outer.build", "Outer") {
		t.Fatalf("expected REFERENCES build -> Outer (companion → enclosing class), got: %s",
			kotlinRelsSummary(ents))
	}
}

// Imported-name handling: a same-file use of an imported leaf must
// NOT produce a broken REFERENCES edge to the import-placeholder
// entity. The Kotlin import entity is keyed by the FULL dotted path
// (not the imported leaf), so a target built from the placeholder
// Name would never bind via lookupStructural. The conservative bias
// is to skip imports in the file-scope symbol table; cross-file
// binding for imported names is the chain-fix-1 work item.
func TestKotlinReferencesImportedNameSkipped(t *testing.T) {
	src := `package com.demo

import com.acmecorp.users.UserService

class Caller {
    fun resolve(): Any {
        val ref: UserService = UserService()
        return ref
    }
}
`
	ents := runKotlinExtract(t, src)
	// Must NOT emit a REFERENCES whose ToID tail is the full dotted
	// import path or the bare leaf bound to the import-placeholder.
	for _, e := range ents {
		if e.Name != "resolve" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != "REFERENCES" {
				continue
			}
			if strings.Contains(r.ToID, "com.acmecorp.users.UserService") {
				t.Errorf("did not expect REFERENCES resolve -> import-entity %q", r.ToID)
			}
		}
	}
}

// Kotlin reserved keywords must NOT produce REFERENCES edges. A
// function body that uses `val`, `var`, `return`, `null`, `it`, etc.
// should emit zero REFERENCES.
func TestKotlinReferencesSkipsReserved(t *testing.T) {
	src := `package com.demo

class Plain {
    fun sum(a: Int, b: Int): Int {
        val r = a + b
        return r
    }
}
`
	ents := runKotlinExtract(t, src)
	for _, e := range ents {
		if e.Name != "sum" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" {
				t.Fatalf("did not expect REFERENCES from sum (got %q)", r.ToID)
			}
		}
	}
}

// Self-reference filter: a function body that uses its own emitted
// name (e.g. via a function reference) does NOT emit a REFERENCES
// self-edge.
func TestKotlinReferencesSelfFiltered(t *testing.T) {
	src := `package com.demo

class Recurser {
    fun recurse(n: Int): Int {
        if (n < 2) return n
        val ref = ::recurse
        return n
    }
}
`
	ents := runKotlinExtract(t, src)
	for _, e := range ents {
		if e.Name != "recurse" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" && strings.HasSuffix(r.ToID, ":recurse") {
				t.Fatalf("did not expect REFERENCES recurse -> recurse, got %q", r.ToID)
			}
		}
	}
}

// Top-level function: a top-level function body that uses a sibling
// top-level class name emits a REFERENCES edge. No enclosing class
// is present — the bare-name symbol-table path handles it.
func TestKotlinReferencesTopLevelFunction(t *testing.T) {
	src := `package com.demo

class Widget

fun build(): Widget {
    val w: Widget = Widget()
    return w
}
`
	ents := runKotlinExtract(t, src)
	if !hasKotlinRef(ents, "build", "Widget") {
		t.Fatalf("expected REFERENCES build -> Widget (top-level), got: %s",
			kotlinRelsSummary(ents))
	}
}

// Object declaration: a function body inside an `object` declaration
// can reference sibling classes the same way a class method body
// does.
func TestKotlinReferencesInsideObject(t *testing.T) {
	src := `package com.demo

class Helper

object Factory {
    fun make(): Helper {
        val h: Helper = Helper()
        return h
    }
}
`
	ents := runKotlinExtract(t, src)
	if !hasKotlinRef(ents, "Factory.make", "Helper") {
		t.Fatalf("expected REFERENCES make -> Helper (from object), got: %s",
			kotlinRelsSummary(ents))
	}
}
