package manifest

import "testing"

// realOpam is a representative *.opam manifest (shape taken from real published
// OCaml packages). It exercises the depends: list with version formulas, a
// {with-test} dev filter, a bare (unconstrained) dep, the `ocaml` compiler
// floor, and a depopts: optional list.
const realOpam = `opam-version: "2.0"
name: "my-service"
synopsis: "A small OCaml web service"
maintainer: "Jane Doe <jane@example.com>"
license: "MIT"
depends: [
  "ocaml" {>= "4.14"}
  "dune" {>= "3.0"}
  "dream"
  "caqti" {>= "1.9.0"}
  "caqti-driver-postgresql"
  "alcotest" {with-test}
]
depopts: [
  "lwt_ssl"
]
build: [
  ["dune" "build" "-p" name "-j" jobs]
]
`

func TestOpam_RuntimeDeps(t *testing.T) {
	deps := depEntities(runExtract(t, "my-service.opam", realOpam))
	// 5 runtime (ocaml, dune, dream, caqti, caqti-driver-postgresql)
	// + 1 dev (alcotest) + 1 optional (lwt_ssl) = 7.
	if len(deps) != 7 {
		t.Fatalf("expected 7 deps, got %d: %v", len(deps), depNames(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "opam" {
			t.Errorf("%s: package_manager=%q want opam", d.Name, d.Properties["package_manager"])
		}
	}

	// The ocaml compiler floor is a real runtime edge, with its constraint.
	if d := depByName(deps, "ocaml"); d == nil {
		t.Error("expected runtime dep 'ocaml' (compiler floor is a real edge)")
	} else if d.Properties["version"] != ">= 4.14" {
		t.Errorf("ocaml version=%q want '>= 4.14'", d.Properties["version"])
	} else if d.Properties["is_dev"] != "false" {
		t.Errorf("ocaml should be is_dev=false")
	}

	if d := depByName(deps, "caqti"); d == nil || d.Properties["version"] != ">= 1.9.0" {
		t.Errorf("caqti version=%v want '>= 1.9.0'", d)
	}

	// A bare dep has no version.
	if d := depByName(deps, "dream"); d == nil {
		t.Error("expected dep 'dream'")
	} else if d.Properties["version"] != "" {
		t.Errorf("dream version=%q want empty (bare)", d.Properties["version"])
	}

	// alcotest carries {with-test} → dev.
	if d := depByName(deps, "alcotest"); d == nil {
		t.Error("expected dev dep 'alcotest'")
	} else if d.Properties["is_dev"] != "true" {
		t.Errorf("alcotest is_dev=%q want true ({with-test})", d.Properties["is_dev"])
	} else if d.Properties["dependency_kind"] != "dev" {
		t.Errorf("alcotest dependency_kind=%q want dev", d.Properties["dependency_kind"])
	}

	// depopts entry is optional.
	if d := depByName(deps, "lwt_ssl"); d == nil {
		t.Error("expected optional dep 'lwt_ssl'")
	} else if d.Properties["dependency_kind"] != "optional" {
		t.Errorf("lwt_ssl dependency_kind=%q want optional", d.Properties["dependency_kind"])
	}
}

// realDuneProject is a dune-project using the generate_opam_files workflow with
// an inline (package (depends ...)) stanza — bare atoms, a constrained sub-list,
// and a :with-test dev filter.
const realDuneProject = `(lang dune 3.0)
(generate_opam_files true)

(package
 (name my-service)
 (synopsis "A small OCaml web service")
 (depends
  ocaml
  dune
  dream
  (caqti (>= 1.9.0))
  (alcotest :with-test)))
`

func TestDuneProject_Deps(t *testing.T) {
	deps := depEntities(runExtract(t, "dune-project", realDuneProject))
	// 4 runtime (ocaml, dune, dream, caqti) + 1 dev (alcotest) = 5.
	if len(deps) != 5 {
		t.Fatalf("expected 5 deps, got %d: %v", len(deps), depNames(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "dune" {
			t.Errorf("%s: package_manager=%q want dune", d.Name, d.Properties["package_manager"])
		}
	}
	if d := depByName(deps, "caqti"); d == nil || d.Properties["version"] != ">= 1.9.0" {
		t.Errorf("caqti version=%v want '>= 1.9.0'", d)
	}
	if d := depByName(deps, "dream"); d == nil {
		t.Error("expected bare dep 'dream'")
	}
	if d := depByName(deps, "alcotest"); d == nil {
		t.Error("expected dev dep 'alcotest'")
	} else if d.Properties["is_dev"] != "true" {
		t.Errorf("alcotest is_dev=%q want true (:with-test)", d.Properties["is_dev"])
	}
}

// A bare dune-project with no inline (package (depends ...)) declares no deps
// here (the *.opam file carries them) — a no-op, not a false-positive.
func TestDuneProject_NoInlineDepends_NoOp(t *testing.T) {
	const bare = "(lang dune 3.0)\n(name my_project)\n"
	deps := depEntities(runExtract(t, "dune-project", bare))
	if len(deps) != 0 {
		t.Fatalf("expected 0 deps for a bare dune-project, got %d: %v", len(deps), depNames(deps))
	}
}

func TestOpam_IsManifestDispatch(t *testing.T) {
	if !IsManifest("pkgs/my-service.opam") {
		t.Error("*.opam should be recognised as a manifest")
	}
	if !IsManifest("dune-project") {
		t.Error("dune-project should be recognised as a manifest")
	}
	if got := detectPackageManager("my-service.opam"); got != "opam" {
		t.Errorf("detectPackageManager(*.opam)=%q want opam", got)
	}
	if got := detectPackageManager("dune-project"); got != "dune" {
		t.Errorf("detectPackageManager(dune-project)=%q want dune", got)
	}
}
