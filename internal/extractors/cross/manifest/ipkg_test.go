package manifest

import "testing"

// realIpkg is a representative Idris (Idris2) *.ipkg manifest. It exercises the
// `package` header, a multi-line comma-leading `depends` list (including the
// `base`/`contrib` stdlib floor), a multi-line `modules` list, and the
// main/executable/sourcedir scalar metadata fields.
const realIpkg = `package myproject

authors = "Jane Doe"
version = 0.1.0
sourcedir = "src"

depends = base
        , contrib
        , network

modules = Data.Foo
        , Data.Bar
        , Main

main = Main
executable = myproject
`

func TestIpkg_Dependencies(t *testing.T) {
	deps := depEntities(runExtract(t, "myproject.ipkg", realIpkg))
	// base, contrib, network = 3 runtime deps.
	if len(deps) != 3 {
		t.Fatalf("expected 3 deps, got %d: %+v", len(deps), depNames(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "idris2" {
			t.Errorf("%s: package_manager=%q want idris2", d.Name, d.Properties["package_manager"])
		}
		if d.Properties["is_dev"] != "false" {
			t.Errorf("%s: is_dev=%q want false", d.Name, d.Properties["is_dev"])
		}
		// ipkg depends has no version-constraint syntax → always empty (honest).
		if d.Properties["version"] != "" {
			t.Errorf("%s: version=%q want empty (ipkg has no constraint syntax)", d.Name, d.Properties["version"])
		}
	}
	// The stdlib floor (base/contrib) is kept as a real edge.
	if depByName(deps, "base") == nil {
		t.Error("expected stdlib-floor dep 'base' (kept as a real edge)")
	}
	if depByName(deps, "contrib") == nil {
		t.Error("expected dep 'contrib' from multi-line depends list")
	}
	if depByName(deps, "network") == nil {
		t.Error("expected dep 'network' from multi-line depends list")
	}
}

func TestIpkg_DependsOnEdges(t *testing.T) {
	rels := dependsOnRels(runExtract(t, "lib.ipkg",
		`package lib
depends = base, prelude
`))
	if len(rels) != 2 {
		t.Fatalf("expected 2 DEPENDS_ON edges, got %d", len(rels))
	}
	for _, r := range rels {
		if r.Properties.Get("package_manager") != "idris2" {
			t.Errorf("edge package_manager=%q want idris2", r.Properties.Get("package_manager"))
		}
	}
}

func TestIpkg_ConfigAnchor(t *testing.T) {
	records := runExtract(t, "myproject.ipkg", realIpkg)
	anchor := anchorRecord(records)
	if anchor == nil {
		t.Fatal("no project anchor emitted for myproject.ipkg")
	}
	cfg := anchor.Properties["ipkg_config"]
	if cfg == "" {
		t.Fatal("ipkg_config property is empty")
	}
	for _, want := range []string{
		"package=myproject",
		"main=Main",
		"executable=myproject",
		"sourcedir=src",
		"modules=Data.Foo Data.Bar Main",
	} {
		if !containsSub(cfg, want) {
			t.Errorf("ipkg_config=%q missing %q", cfg, want)
		}
	}
}

func TestIpkg_PkgsSynonym(t *testing.T) {
	// Some build setups use `pkgs` as a synonym for the dependency list.
	deps := depEntities(runExtract(t, "alt.ipkg",
		`package alt
pkgs = base, sop
`))
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps from pkgs field, got %d: %+v", len(deps), depNames(deps))
	}
	if depByName(deps, "sop") == nil {
		t.Error("expected dep 'sop' from pkgs field")
	}
}

func TestIpkg_IsManifest(t *testing.T) {
	if !IsManifest("foo/myproject.ipkg") {
		t.Error("IsManifest should recognise *.ipkg")
	}
	if detectPackageManager("myproject.ipkg") != "idris2" {
		t.Errorf("detectPackageManager(*.ipkg)=%q want idris2", detectPackageManager("myproject.ipkg"))
	}
}

func TestIpkg_NoDependencies(t *testing.T) {
	deps := depEntities(runExtract(t, "empty.ipkg",
		`package empty
version = 0.1.0
modules = Main
main = Main
`))
	if len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d: %+v", len(deps), depNames(deps))
	}
}
