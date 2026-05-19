package hierarchy

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runExtract(t *testing.T, lang, path, source string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	records, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(source),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	return records
}

// TestPython_DeclaredClassHasSubtypeClass asserts the declared (child) class
// in `class Foo(Bar):` is emitted with Subtype="class" — preserves the
// behavior tightened by issue #46.
func TestPython_DeclaredClassHasSubtypeClass(t *testing.T) {
	src := "class UserSerializer(serializers.ModelSerializer):\n    pass\n"
	got := runExtract(t, "python", "app/serializers.py", src)

	var foundChild bool
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Name == "UserSerializer" {
			foundChild = true
			if e.Subtype != "class" {
				t.Errorf("UserSerializer Subtype=%q, want %q", e.Subtype, "class")
			}
		}
	}
	if !foundChild {
		t.Fatalf("expected UserSerializer entity, got entities=%v", entityNames(got))
	}
}

// TestPython_ExternalBaseDoesNotGetClassSubtype is the regression test for
// issue #74. External base references (e.g. `serializers.ModelSerializer`)
// must NOT be emitted as Subtype="class" entities — that conflated external
// references with declared classes. Per issue #74 we drop the placeholder
// entirely and let internal/external/synth.go (Pass 4.5) handle the
// unresolved EXTENDS endpoint.
func TestPython_ExternalBaseDoesNotGetClassSubtype(t *testing.T) {
	src := "class UserSerializer(serializers.ModelSerializer):\n    pass\n"
	got := runExtract(t, "python", "app/serializers.py", src)

	forbidden := map[string]bool{
		"ModelSerializer":             true,
		"serializers.ModelSerializer": true,
		"serializers":                 true,
	}
	for _, e := range got {
		if e.Kind != "SCOPE.Component" {
			continue
		}
		if e.Subtype != "class" {
			continue
		}
		if forbidden[e.Name] {
			t.Errorf("forbidden external-base entity emitted as Subtype=class: name=%q", e.Name)
		}
	}
}

// TestPython_ExtendsRelationshipStillEmitted ensures dropping the placeholder
// did NOT drop the EXTENDS relationship — the resolver and external
// synthesis pass still need it to wire up the graph edge.
func TestPython_ExtendsRelationshipStillEmitted(t *testing.T) {
	src := "class UserSerializer(serializers.ModelSerializer):\n    pass\n"
	got := runExtract(t, "python", "app/serializers.py", src)

	var hasExtends bool
	for _, e := range got {
		for _, rel := range e.Relationships {
			if rel.Kind == "EXTENDS" {
				hasExtends = true
			}
		}
	}
	if !hasExtends {
		t.Errorf("expected an EXTENDS relationship, got entities=%v", entityNames(got))
	}
}

// TestPython_DeclaredParentStillResolvable confirms that a base class
// declared in the same file (Child extends Base) still produces a valid
// EXTENDS edge even though we no longer synthesise a placeholder for the
// parent. The actual `Base` declaration comes from the python extractor;
// the cross-hierarchy extractor only emits the relationship.
func TestPython_DeclaredParentStillResolvable(t *testing.T) {
	src := "class Base:\n    pass\n\nclass Child(Base):\n    pass\n"
	got := runExtract(t, "python", "app/models.py", src)

	// Child must be emitted (it has a non-empty base list).
	var hasChild bool
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Name == "Child" && e.Subtype == "class" {
			hasChild = true
		}
	}
	if !hasChild {
		t.Errorf("expected Child class entity, got entities=%v", entityNames(got))
	}

	// EXTENDS relationship from Child -> Base must still be emitted.
	var hasExtends bool
	for _, e := range got {
		for _, rel := range e.Relationships {
			if rel.Kind == "EXTENDS" {
				hasExtends = true
			}
		}
	}
	if !hasExtends {
		t.Errorf("expected EXTENDS relationship Child->Base, got entities=%v", entityNames(got))
	}
}

// TestJava_InterfaceExtendsEmitsEdge is the regression for issue #612: a Java
// `interface Foo extends Bar<T>, Baz` declaration must emit an EXTENDS edge
// from Foo to each parent interface, with generic type arguments stripped.
func TestJava_InterfaceExtendsEmitsEdge(t *testing.T) {
	src := "public interface UserRepository extends JpaRepository<User, Long>, Serializable {\n" +
		"    Optional<User> findByEmail(String email);\n}\n"
	got := runExtract(t, "java", "repo/UserRepository.java", src)

	// Interface entity must be emitted with subtype="interface".
	var hasIface bool
	for _, e := range got {
		if e.Name == "UserRepository" && e.Subtype == "interface" {
			hasIface = true
		}
	}
	if !hasIface {
		t.Fatalf("expected UserRepository interface entity, got %v", entityNames(got))
	}

	// EXTENDS edges to JpaRepository and Serializable must both be present.
	wantTargets := map[string]bool{
		"JpaRepository": false,
		"Serializable":  false,
	}
	for _, e := range got {
		for _, rel := range e.Relationships {
			if rel.Kind != "EXTENDS" {
				continue
			}
			for tgt := range wantTargets {
				if rel.ToID == ifaceRef(tgt, "java") {
					wantTargets[tgt] = true
				}
			}
		}
	}
	for tgt, found := range wantTargets {
		if !found {
			t.Errorf("expected EXTENDS edge to %q, missing. entities=%v", tgt, entityNames(got))
		}
	}
}

// TestJava_InterfaceWithoutExtendsIsSkipped guards against emitting empty
// interface entities (no EXTENDS, no useful signal — the per-language
// extractor already covers entity emission).
func TestJava_InterfaceWithoutExtendsIsSkipped(t *testing.T) {
	src := "public interface Marker {\n}\n"
	got := runExtract(t, "java", "repo/Marker.java", src)
	for _, e := range got {
		if e.Name == "Marker" {
			t.Errorf("expected no Marker entity from hierarchy extractor (no extends), got %v", entityNames(got))
		}
	}
}

func entityNames(records []types.EntityRecord) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.Name+"["+r.Subtype+"]")
	}
	return out
}
