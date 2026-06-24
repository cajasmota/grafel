package manifest

import "testing"

// realCM is a representative SML/NJ *.cm group/library file. It exercises the
// `Library ( <exports> ) is ... end` shape, the SML/NJ stdlib-anchor floor
// ($/basis.cm, $/smlnj-lib.cm) kept as real deps, a nested .cm library include,
// plain .sml/.sig source members in subdirs, and a (* ... *) comment.
const realCM = `(* myproject.cm — the project library *)
Library
  structure Foo
  signature BAR
is
  $/basis.cm
  $/smlnj-lib.cm
  util/util.cm
  foo.sml
  bar/bar.sig
  baz.fun
end
`

func TestCM_Dependencies(t *testing.T) {
	deps := depEntities(runExtract(t, "myproject.cm", realCM))
	// Library refs only: $/basis.cm, $/smlnj-lib.cm, util/util.cm = 3 deps.
	// Source files (foo.sml/bar.sig/baz.fun) are NOT deps.
	if len(deps) != 3 {
		t.Fatalf("expected 3 deps, got %d: %+v", len(deps), depNames(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "smlnj_cm" {
			t.Errorf("%s: package_manager=%q want smlnj_cm", d.Name, d.Properties["package_manager"])
		}
		// CM has no version-constraint syntax → always empty (honest).
		if d.Properties["version"] != "" {
			t.Errorf("%s: version=%q want empty (CM has no constraint syntax)", d.Name, d.Properties["version"])
		}
	}
	// The SML/NJ stdlib floor is kept as a real edge (verbatim anchor name).
	if depByName(deps, "$/basis.cm") == nil {
		t.Error("expected stdlib-floor dep '$/basis.cm' (kept as a real edge)")
	}
	if depByName(deps, "$/smlnj-lib.cm") == nil {
		t.Error("expected stdlib-floor dep '$/smlnj-lib.cm'")
	}
	// A relative .cm include converges to its basename.
	if depByName(deps, "util.cm") == nil {
		t.Error("expected library include 'util.cm' from util/util.cm")
	}
	// Source files must NOT appear as deps.
	if depByName(deps, "foo.sml") != nil || depByName(deps, "baz.fun") != nil {
		t.Error("source files must not be emitted as deps")
	}
}

func TestCM_ConfigAnchor(t *testing.T) {
	anchor := anchorRecord(runExtract(t, "myproject.cm", realCM))
	if anchor == nil {
		t.Fatal("no project anchor emitted for myproject.cm")
	}
	cfg := anchor.Properties["cm_config"]
	if cfg == "" {
		t.Fatal("cm_config property is empty")
	}
	for _, want := range []string{
		"export=library",
		"sources=foo.sml bar/bar.sig baz.fun",
	} {
		if !containsSub(cfg, want) {
			t.Errorf("cm_config=%q missing %q", cfg, want)
		}
	}
}

func TestCM_GroupExport(t *testing.T) {
	anchor := anchorRecord(runExtract(t, "g.cm",
		`Group is
  $/basis.cm
  main.sml
end
`))
	if anchor == nil {
		t.Fatal("no anchor for g.cm")
	}
	if !containsSub(anchor.Properties["cm_config"], "export=group") {
		t.Errorf("cm_config=%q want export=group", anchor.Properties["cm_config"])
	}
}

// realMLB is a representative MLton *.mlb ML Basis file. It exercises the MLton
// stdlib basis floor ($(SML_LIB)/basis/basis.mlb) kept as a real dep, a nested
// .mlb include, a local/in/end block, .sml/.sig source members, an `ann "..."`
// annotation that must be ignored, and a (* ... *) comment.
const realMLB = `(* sources.mlb *)
$(SML_LIB)/basis/basis.mlb
$(SML_LIB)/smlnj-lib/Util/smlnj-lib.mlb
ann "redundantMatch warn" in
  local
    util/util.mlb
    foo.sml
  in
    bar.sig
    main.sml
  end
end
`

func TestMLB_Dependencies(t *testing.T) {
	deps := depEntities(runExtract(t, "sources.mlb", realMLB))
	// .mlb includes only: basis.mlb, smlnj-lib.mlb, util.mlb = 3 deps.
	if len(deps) != 3 {
		t.Fatalf("expected 3 deps, got %d: %+v", len(deps), depNames(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "mlton_mlb" {
			t.Errorf("%s: package_manager=%q want mlton_mlb", d.Name, d.Properties["package_manager"])
		}
		if d.Properties["version"] != "" {
			t.Errorf("%s: version=%q want empty (MLB has no constraint syntax)", d.Name, d.Properties["version"])
		}
	}
	// The MLton stdlib basis floor is kept as a real edge (basename-converged).
	if depByName(deps, "basis.mlb") == nil {
		t.Error("expected stdlib-floor dep 'basis.mlb' from $(SML_LIB)/basis/basis.mlb")
	}
	if depByName(deps, "smlnj-lib.mlb") == nil {
		t.Error("expected dep 'smlnj-lib.mlb'")
	}
	if depByName(deps, "util.mlb") == nil {
		t.Error("expected nested include 'util.mlb'")
	}
	// Source files / keywords / annotation strings must NOT be deps.
	for _, bad := range []string{"foo.sml", "bar.sig", "ann", "local", "redundantMatch"} {
		if depByName(deps, bad) != nil {
			t.Errorf("%q must not be emitted as a dep", bad)
		}
	}
}

func TestMLB_ConfigAnchor(t *testing.T) {
	anchor := anchorRecord(runExtract(t, "sources.mlb", realMLB))
	if anchor == nil {
		t.Fatal("no project anchor emitted for sources.mlb")
	}
	cfg := anchor.Properties["mlb_config"]
	if cfg == "" {
		t.Fatal("mlb_config property is empty")
	}
	// Source members, in declaration order, comments/annotations excluded.
	if !containsSub(cfg, "sources=foo.sml bar.sig main.sml") {
		t.Errorf("mlb_config=%q missing source list", cfg)
	}
}

func TestMLB_DependsOnEdges(t *testing.T) {
	rels := dependsOnRels(runExtract(t, "x.mlb",
		`$(SML_LIB)/basis/basis.mlb
lib/dep.mlb
main.sml
`))
	if len(rels) != 2 {
		t.Fatalf("expected 2 DEPENDS_ON edges, got %d", len(rels))
	}
	for _, r := range rels {
		if r.Properties["package_manager"] != "mlton_mlb" {
			t.Errorf("edge package_manager=%q want mlton_mlb", r.Properties["package_manager"])
		}
	}
}

func TestSML_IsManifest(t *testing.T) {
	for _, p := range []string{"foo/myproject.cm", "sources.mlb"} {
		if !IsManifest(p) {
			t.Errorf("IsManifest should recognise %q", p)
		}
	}
	if detectPackageManager("myproject.cm") != "smlnj_cm" {
		t.Errorf("detectPackageManager(*.cm)=%q want smlnj_cm", detectPackageManager("myproject.cm"))
	}
	if detectPackageManager("sources.mlb") != "mlton_mlb" {
		t.Errorf("detectPackageManager(*.mlb)=%q want mlton_mlb", detectPackageManager("sources.mlb"))
	}
}

func TestSML_NoDependencies(t *testing.T) {
	// A CM group of pure local sources (no library includes) emits no deps.
	deps := depEntities(runExtract(t, "leaf.cm",
		`Group is
  a.sml
  b.sml
end
`))
	if len(deps) != 0 {
		t.Fatalf("expected 0 deps for a source-only group, got %d: %+v", len(deps), depNames(deps))
	}
}
