package manifest

import "testing"

// realAsd is a representative Common Lisp ASDF *.asd system definition. It
// exercises a multi-line `:depends-on` list mixing bare strings, a bare symbol,
// a `(:version "x" "v")` spec, a `(:feature :kw "x")` spec, a `(:require "x")`
// spec, a reader-conditional-guarded dep, a `;` line comment (commented-out
// dep), and a nested `:components` member list.
const realAsd = `;;;; my-app.asd  --- system definition

(defsystem "my-app"
  :version "1.2.0"
  :author "Jane Doe"
  :license "MIT"
  :depends-on ("alexandria"
               bordeaux-threads
               (:version "cl-ppcre" "2.0.0")
               (:feature :sbcl "sb-posix")
               (:require "uiop")
               #+quicklisp "drakma"
               ;; "commented-out-dep" should NOT be mined
               )
  :components ((:file "package")
               (:module "src"
                :components ((:file "core") (:file "util")))
               (:file "main")))
`

func TestAsd_Dependencies(t *testing.T) {
	deps := depEntities(runExtract(t, "my-app.asd", realAsd))
	// alexandria, bordeaux-threads, cl-ppcre, sb-posix, uiop, drakma = 6.
	want := []string{"alexandria", "bordeaux-threads", "cl-ppcre", "sb-posix", "uiop", "drakma"}
	if len(deps) != len(want) {
		t.Fatalf("expected %d deps, got %d: %+v", len(want), len(deps), depNames(deps))
	}
	for _, n := range want {
		if depByName(deps, n) == nil {
			t.Errorf("expected dep %q in %+v", n, depNames(deps))
		}
	}
	// The commented-out dep must NOT be present.
	if depByName(deps, "commented-out-dep") != nil {
		t.Error("commented-out dep was mined (line comment not stripped)")
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "asdf" {
			t.Errorf("%s: package_manager=%q want asdf", d.Name, d.Properties["package_manager"])
		}
		if d.Properties["is_dev"] != "false" {
			t.Errorf("%s: is_dev=%q want false", d.Name, d.Properties["is_dev"])
		}
	}
}

func TestAsd_VersionSpec(t *testing.T) {
	deps := depEntities(runExtract(t, "my-app.asd", realAsd))
	clppcre := depByName(deps, "cl-ppcre")
	if clppcre == nil {
		t.Fatal("expected cl-ppcre dep")
	}
	// (:version "cl-ppcre" "2.0.0") → version recorded.
	if got := clppcre.Properties["version"]; got != "2.0.0" {
		t.Errorf("cl-ppcre version=%q want 2.0.0", got)
	}
	// Bare-string / bare-symbol deps have no version constraint (honest).
	if got := depByName(deps, "alexandria").Properties["version"]; got != "" {
		t.Errorf("alexandria version=%q want empty", got)
	}
}

func TestAsd_DependsOnEdges(t *testing.T) {
	rels := dependsOnRels(runExtract(t, "lib.asd",
		`(defsystem "lib" :depends-on ("alexandria" "cl-fad"))`))
	if len(rels) != 2 {
		t.Fatalf("expected 2 DEPENDS_ON edges, got %d", len(rels))
	}
	for _, r := range rels {
		if r.Properties["package_manager"] != "asdf" {
			t.Errorf("edge package_manager=%q want asdf", r.Properties["package_manager"])
		}
	}
}

func TestAsd_ConfigAnchor(t *testing.T) {
	records := runExtract(t, "my-app.asd", realAsd)
	anchor := anchorRecord(records)
	if anchor == nil {
		t.Fatal("no project anchor emitted for my-app.asd")
	}
	cfg := anchor.Properties["asd_config"]
	if cfg == "" {
		t.Fatal("asd_config property is empty")
	}
	for _, want := range []string{
		"system=my-app",
		// Top-level component members (package, src module, main).
		"package",
		"src",
		"main",
	} {
		if !containsSub(cfg, want) {
			t.Errorf("asd_config=%q missing %q", cfg, want)
		}
	}
	if anchor.Properties["package_manager"] != "asdf" {
		t.Errorf("anchor package_manager=%q want asdf", anchor.Properties["package_manager"])
	}
}

func TestAsd_PackageQualifiedDefsystem(t *testing.T) {
	// asdf:defsystem (package-qualified) and a keyword system name should parse.
	deps := depEntities(runExtract(t, "foo.asd",
		`(asdf:defsystem :foo :depends-on (:alexandria "babel"))`))
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d: %+v", len(deps), depNames(deps))
	}
	if depByName(deps, "alexandria") == nil {
		t.Error("expected keyword-symbol dep :alexandria → alexandria")
	}
	if depByName(deps, "babel") == nil {
		t.Error("expected string dep babel")
	}
	// The system name itself must NOT be a dependency.
	if depByName(deps, "foo") != nil {
		t.Error("system name 'foo' was wrongly emitted as a dependency")
	}
}

func TestAsd_MultipleSystems(t *testing.T) {
	// A .asd with two defsystem forms (system + its test system) merges deps.
	src := `(defsystem "app" :depends-on ("alexandria"))
(defsystem "app/tests" :depends-on ("app" "fiveam"))`
	deps := depEntities(runExtract(t, "app.asd", src))
	for _, n := range []string{"alexandria", "app", "fiveam"} {
		if depByName(deps, n) == nil {
			t.Errorf("expected dep %q from multi-system file, got %+v", n, depNames(deps))
		}
	}
}

func TestAsd_NoDependencies(t *testing.T) {
	// A defsystem with no :depends-on emits the project anchor but no deps.
	records := runExtract(t, "bare.asd", `(defsystem "bare" :components ((:file "main")))`)
	if depEntities(records) != nil && len(depEntities(records)) != 0 {
		t.Errorf("expected 0 deps, got %+v", depNames(depEntities(records)))
	}
	if anchorRecord(records) == nil {
		t.Error("expected a project anchor even with no deps")
	}
}

func TestAsd_IsManifest(t *testing.T) {
	if !IsManifest("path/to/my-app.asd") {
		t.Error("*.asd should be recognised as a manifest")
	}
	if detectPackageManager("my-app.asd") != "asdf" {
		t.Errorf("detectPackageManager(*.asd)=%q want asdf", detectPackageManager("my-app.asd"))
	}
}
