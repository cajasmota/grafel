// references_test.go — coverage for the REFERENCES-emission pass.

package java

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// hasJavaRef returns true when any entity named fromName carries a
// REFERENCES edge whose ToID contains substr.
func hasJavaRef(ents []types.EntityRecord, fromName, substr string) bool {
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

// Same-scope identifier resolution: a method body that uses the name of
// a sibling class declared in the same file emits a REFERENCES edge.
// The CALLS path takes precedence over REFERENCES when the identifier
// IS the callee of a method_invocation.
func TestJavaReferencesSameScopeIdentifier(t *testing.T) {
	src := `package com.demo;

class Helper {}

public class Caller {
    Object make() {
        Class<?> ref = Helper.class;
        return ref;
    }
}
`
	ents := runJavaExtract(t, src)
	if !hasJavaRef(ents, "Caller.make", "Helper") {
		t.Fatalf("expected REFERENCES Caller.make -> Helper, got: %s", javaRelsSummary(ents))
	}
}

// this.<field> resolution: a method body that uses `this.<field>` emits
// a REFERENCES edge to the field entity. The Java extractor emits
// fields with a bare Name today, so the binding lands via the bare
// symbol table fallback in handleFieldAccess.
func TestJavaReferencesThisField(t *testing.T) {
	src := `package com.demo;

public class Box {
    private int counter;

    public int bump() {
        return this.counter + 1;
    }
}
`
	ents := runJavaExtract(t, src)
	if !hasJavaRef(ents, "Box.bump", "counter") {
		t.Fatalf("expected REFERENCES Box.bump -> counter (this.counter), got: %s", javaRelsSummary(ents))
	}
}

// ClassName.staticMember resolution: a method body that uses
// `Helper.HELPER_METHOD` (where Helper is a class defined in the same
// file with a method HELPER_METHOD) emits a REFERENCES edge to the
// "Helper.HELPER_METHOD" entity via the dotted lookup. We use a
// non-call usage (assignment of a method reference).
func TestJavaReferencesStaticMember(t *testing.T) {
	src := `package com.demo;

class Helper {
    static int compute() { return 42; }
}

public class Caller {
    Runnable build() {
        Runnable r = Helper::compute;
        return r;
    }
}
`
	ents := runJavaExtract(t, src)
	// The method-reference syntax `Helper::compute` is parsed as a
	// `method_reference` node, not a method_invocation. The Helper
	// receiver appears as a type_identifier, so we expect a REFERENCES
	// to Helper at minimum.
	if !hasJavaRef(ents, "Caller.build", "Helper") {
		t.Fatalf("expected REFERENCES Caller.build -> Helper, got: %s", javaRelsSummary(ents))
	}
}

// Imported-name handling: a same-file use of an imported leaf must NOT
// produce a broken REFERENCES edge to the import-placeholder entity.
// The Java import entity is keyed by the top package segment ("com")
// not the imported leaf, so a target built from the placeholder Name
// would never bind via lookupStructural. The conservative bias is to
// skip imports in the file-scope symbol table; cross-file binding for
// imported names is the chain-fix-1 work item.
func TestJavaReferencesImportedNameSkipped(t *testing.T) {
	src := `package com.demo;

import com.acmecorp.users.UserService;

public class Caller {
    Object resolve() {
        Object ref = UserService.class;
        return ref;
    }
}
`
	ents := runJavaExtract(t, src)
	// Must NOT emit a REFERENCES whose ToID's tail is just the top
	// import segment (e.g. ":com"). Such an edge can never bind and
	// would inflate the orphan count.
	for _, e := range ents {
		if e.Name != "Caller.resolve" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != "REFERENCES" {
				continue
			}
			if strings.HasSuffix(r.ToID, ":com") {
				t.Fatalf("did not expect REFERENCES Caller.resolve -> import-top-segment %q", r.ToID)
			}
		}
	}
}

// Java reserved keywords / primitive type names must NOT produce
// REFERENCES edges. A method body that uses `int`, `void`, `return`,
// etc. should emit zero REFERENCES.
func TestJavaReferencesSkipsReserved(t *testing.T) {
	src := `package com.demo;

public class Plain {
    public int sum(int a, int b) {
        int r = a + b;
        return r;
    }
}
`
	ents := runJavaExtract(t, src)
	for _, e := range ents {
		if e.Name != "Plain.sum" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" {
				t.Fatalf("did not expect REFERENCES from Plain.sum (got %q)", r.ToID)
			}
		}
	}
}

// Self-reference filter: a method body that uses its own emitted name
// (e.g. via a method reference passed as an argument) does NOT emit a
// REFERENCES self-edge.
func TestJavaReferencesSelfFiltered(t *testing.T) {
	src := `package com.demo;

public class Recurser {
    public int recurse(int n) {
        if (n < 2) return n;
        // method-reference shape: this is a non-call use of recurse.
        Runnable r = this::recurse;
        return n;
    }
}
`
	ents := runJavaExtract(t, src)
	for _, e := range ents {
		if e.Name != "Recurser.recurse" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" && strings.Contains(r.ToID, ":recurse") {
				t.Fatalf("did not expect REFERENCES recurse -> recurse, got %q", r.ToID)
			}
		}
	}
}

// TestJavaReferencesInheritedFieldHint — Issue #667.
// When a subclass method accesses `this.<attr>` and the field is NOT
// declared in the same file (it's inherited from a parent class),
// the extractor should emit a cross-file hint stub so the resolver can
// follow EXTENDS edges to find the field.
func TestJavaReferencesInheritedFieldHint(t *testing.T) {
	// Child declares no "value" field — it would be inherited from a parent.
	// The extractor should emit a hint REFERENCES stub for this.value.
	src := `package com.demo;

public class Child {
    public int read() {
        return this.value;
    }
}
`
	ents := runJavaExtract(t, src)
	// Expect a REFERENCES edge from Child.read to a stub containing "value".
	if !hasJavaRef(ents, "Child.read", "value") {
		t.Fatalf("#667: expected cross-file hint REFERENCES Child.read -> *value*, got: %s", javaRelsSummary(ents))
	}
	// Verify the stub uses the enclosing class name (Child.value) so the
	// resolver can probe byPackageMember or global schema index.
	if !hasJavaRef(ents, "Child.read", "Child.value") {
		t.Fatalf("#667: expected hint stub to contain 'Child.value', got: %s", javaRelsSummary(ents))
	}
}

// javaRelsSummary stringifies every entity's REFERENCES edges for
// failure messages.
func javaRelsSummary(ents []types.EntityRecord) string {
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
