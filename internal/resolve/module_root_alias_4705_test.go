// module_root_alias_4705_test.go — #4705: module-root / source-root alias
// resolution so rooted internal imports bind to the internal target BEFORE
// falling through to external_package.
//
//   - Python (#4705a): src-layout, namespace packages, and Django source
//     roots nested under a project container (`server/apps/users/...`).
//   - Java   (#4705b): canonical Maven/Gradle source roots, including
//     Gradle multi-module layouts where a container segment precedes the
//     source root (`lib/src/main/java/...`, `app/src/main/java/...`).
//
// The Go half (#4705c, go.mod local-path `replace`) is validated end-to-end
// in internal/extractors/golang/issue4705_replace_test.go.
package resolve

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func resolveDotted(t *testing.T, recs []types.EntityRecord, dotted string) (string, bool) {
	t.Helper()
	return BuildImportTable(recs).ResolveDottedImportTarget(dotted)
}

func TestModuleRootAlias4705_PythonSrcLayout(t *testing.T) {
	// src/myapp/models.py defines X; import `from myapp.models import X`.
	recs := []types.EntityRecord{
		{ID: "x", Name: "X", Kind: "SCOPE.Operation", SourceFile: "src/myapp/models.py", Language: "python"},
	}
	if id, ok := resolveDotted(t, recs, "myapp.models.X"); !ok || id != "x" {
		t.Errorf("src-layout: myapp.models.X => (%q,%v), want (x,true)", id, ok)
	}
}

func TestModuleRootAlias4705_PythonNamespacePackage(t *testing.T) {
	// Namespace package (no __init__.py) under src/.
	recs := []types.EntityRecord{
		{ID: "t", Name: "Thing", Kind: "SCOPE.Operation", SourceFile: "src/myns/sub/mod.py", Language: "python"},
	}
	if id, ok := resolveDotted(t, recs, "myns.sub.mod.Thing"); !ok || id != "t" {
		t.Errorf("namespace pkg: myns.sub.mod.Thing => (%q,%v), want (t,true)", id, ok)
	}
}

func TestModuleRootAlias4705_PythonDjangoAppsContainer(t *testing.T) {
	// Django apps under a nested `apps/` package: `from users.views import V`.
	recs := []types.EntityRecord{
		{ID: "v", Name: "V", Kind: "SCOPE.Operation", SourceFile: "server/apps/users/views.py", Language: "python"},
	}
	if id, ok := resolveDotted(t, recs, "users.views.V"); !ok || id != "v" {
		t.Errorf("django apps/: users.views.V => (%q,%v), want (v,true)", id, ok)
	}
}

func TestModuleRootAlias4705_JavaSourceRoot(t *testing.T) {
	recs := []types.EntityRecord{
		{ID: "bar", Name: "Bar", Kind: "SCOPE.Component", SourceFile: "src/main/java/com/acme/Bar.java", Language: "java"},
	}
	if id, ok := resolveDotted(t, recs, "com.acme.Bar"); !ok || id != "bar" {
		t.Errorf("java src root: com.acme.Bar => (%q,%v), want (bar,true)", id, ok)
	}
}

func TestModuleRootAlias4705_JavaGradleMultiModule(t *testing.T) {
	// Gradle multi-module: container segment precedes the source root.
	recs := []types.EntityRecord{
		{ID: "j1", Name: "Bar", Kind: "SCOPE.Component", SourceFile: "lib/src/main/java/com/acme/Bar.java", Language: "java"},
		{ID: "j2", Name: "Baz", Kind: "SCOPE.Component", SourceFile: "app/src/main/java/com/acme/Baz.java", Language: "java"},
	}
	tbl := BuildImportTable(recs)
	if id, ok := tbl.ResolveDottedImportTarget("com.acme.Bar"); !ok || id != "j1" {
		t.Errorf("gradle lib module: com.acme.Bar => (%q,%v), want (j1,true)", id, ok)
	}
	if id, ok := tbl.ResolveDottedImportTarget("com.acme.Baz"); !ok || id != "j2" {
		t.Errorf("gradle app module: com.acme.Baz => (%q,%v), want (j2,true)", id, ok)
	}
}

// stripAfterSourceRoot unit coverage: interior match must respect segment
// boundaries (a substring that is not segment-aligned must NOT match).
func TestStripAfterSourceRoot(t *testing.T) {
	cases := []struct {
		dotted, root, wantTail string
		wantOK                 bool
	}{
		{"src.main.java.com.acme", "src.main.java.", "com.acme", true},
		{"lib.src.main.java.com.acme", "src.main.java.", "com.acme", true},
		{"app.src.main.java.com.acme", "src.main.java.", "com.acme", true},
		{"com.acme.foo", "src.main.java.", "", false},
		// not segment-aligned: "xsrc.main.java" must not match "src.main.java."
		{"xsrc.main.java.com", "src.main.java.", "", false},
	}
	for _, c := range cases {
		tail, ok := stripAfterSourceRoot(c.dotted, c.root)
		if tail != c.wantTail || ok != c.wantOK {
			t.Errorf("stripAfterSourceRoot(%q,%q) = (%q,%v), want (%q,%v)",
				c.dotted, c.root, tail, ok, c.wantTail, c.wantOK)
		}
	}
}
