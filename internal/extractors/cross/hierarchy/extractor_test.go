package hierarchy

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// migrationSrc is a minimal Django migration file body that matches pyClassRE.
const migrationSrc = `from django.db import migrations, models

class Migration(migrations.Migration):
    initial = True
    dependencies = []
    operations = [
        migrations.CreateModel(
            name='Device',
            fields=[('id', models.AutoField(primary_key=True))],
        ),
    ]
`

// TestMigrationFile_NoSCOPEComponentEntity verifies that by default (no
// ARCHIGRAPH_EMIT_MIGRATION_ENTITIES env var) the hierarchy extractor emits
// zero entities for Django migration files (issue #2603).
//
// Before the fix the pyClassRE matched `class Migration(migrations.Migration):`
// and emitted a SCOPE.Component/class entity named "Migration" — bypassing the
// prune that the Python extractor performs via its early-return gate.
func TestMigrationFile_NoSCOPEComponentEntity(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_MIGRATION_ENTITIES", "")

	got := runExtract(t, "python", "core/migrations/0001_initial.py", migrationSrc)

	if len(got) != 0 {
		t.Errorf("default-off: hierarchy extractor emitted %d entities for migration file, want 0", len(got))
		for _, e := range got {
			t.Logf("  - name=%q kind=%q subtype=%q", e.Name, e.Kind, e.Subtype)
		}
	}
}

// TestMigrationFile_OptInEmitsComponent verifies that when
// ARCHIGRAPH_EMIT_MIGRATION_ENTITIES=1 the hierarchy extractor DOES emit the
// Migration class entity (opt-in path, symmetric with the Python extractor).
func TestMigrationFile_OptInEmitsComponent(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_MIGRATION_ENTITIES", "1")

	got := runExtract(t, "python", "core/migrations/0001_initial.py", migrationSrc)

	hasMigration := false
	for _, e := range got {
		if e.Name == "Migration" && e.Kind == "SCOPE.Component" {
			hasMigration = true
			break
		}
	}
	if !hasMigration {
		t.Errorf("opt-in: hierarchy extractor did not emit Migration SCOPE.Component entity; got %v", entityNames(got))
	}
}

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

// TestExtends_CarriesBaseName is the #3839 regression: every EXTENDS /
// IMPLEMENTS edge must carry a `base_name` property with the base FQN as
// written, so the MRO walk and the cbv_inherited_methods consumer can resolve
// the defining class across files/modules without re-parsing the ToID.
func TestExtends_CarriesBaseName(t *testing.T) {
	cases := []struct {
		name, lang, path, src, wantKind, wantBase string
	}{
		{
			name: "python_extends_dotted", lang: "python", path: "app/views.py",
			src:      "class RoleViewSet(viewsets.ModelViewSet):\n    pass\n",
			wantKind: "EXTENDS", wantBase: "viewsets.ModelViewSet",
		},
		{
			name: "java_implements", lang: "java", path: "Impl.java",
			src:      "class Impl implements Runnable {\n}\n",
			wantKind: "IMPLEMENTS", wantBase: "Runnable",
		},
		{
			name: "go_embedding", lang: "go", path: "service.go",
			src:      "type Service struct {\n\t*BaseService\n}\n",
			wantKind: "EXTENDS", wantBase: "BaseService",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runExtract(t, tc.lang, tc.path, tc.src)
			var found bool
			for _, e := range got {
				for _, rel := range e.Relationships {
					if rel.Kind != tc.wantKind {
						continue
					}
					bn := rel.Properties["base_name"]
					if bn == "" {
						t.Errorf("%s edge missing base_name property", tc.wantKind)
					}
					if bn == tc.wantBase {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("expected %s edge with base_name=%q, got entities=%v", tc.wantKind, tc.wantBase, entityNames(got))
			}
		})
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
