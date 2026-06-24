package manifest

import "testing"

// realPaketDependencies is a representative paket.dependencies manifest. It
// exercises: top-level source/storage/framework config (ignored), Main-group
// runtime nuget deps with assorted constraints (~>, >=, exact, bare), a
// `group Test` (dev) block, a `group Build` (dev) block, a trailing per-package
// option on a constraint, and a `github` file reference (honest-skipped).
const realPaketDependencies = `source https://api.nuget.org/v3/index.json
storage: none
framework: net8.0

nuget FSharp.Core
nuget Giraffe ~> 5.0
nuget Microsoft.EntityFrameworkCore >= 7.0.0
nuget Newtonsoft.Json 13.0.3 prerelease

github fsprojects/Paket.Restore install.fsx

group Test
    source https://api.nuget.org/v3/index.json
    nuget Expecto ~> 9.0
    nuget FsUnit

group Build
    nuget Fake.Core.Target
`

func TestPaket_DependenciesRuntimeDeps(t *testing.T) {
	deps := depEntities(runExtract(t, "paket.dependencies", realPaketDependencies))
	// runtime: FSharp.Core, Giraffe, Microsoft.EntityFrameworkCore, Newtonsoft.Json (4)
	// dev: Expecto, FsUnit (Test), Fake.Core.Target (Build) (3)
	if len(deps) != 7 {
		t.Fatalf("expected 7 deps, got %d: %+v", len(deps), depNames(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "paket" {
			t.Errorf("%s: package_manager=%q want paket", d.Name, d.Properties["package_manager"])
		}
	}

	// Bare nuget → empty version, runtime.
	if d := depByName(deps, "FSharp.Core"); d == nil {
		t.Error("expected runtime dep FSharp.Core")
	} else if d.Properties["version"] != "" {
		t.Errorf("FSharp.Core version=%q want empty (bare)", d.Properties["version"])
	} else if d.Properties["is_dev"] != "false" {
		t.Error("FSharp.Core should be is_dev=false")
	}

	// ~> constraint preserved verbatim.
	if d := depByName(deps, "Giraffe"); d == nil || d.Properties["version"] != "~> 5.0" {
		t.Errorf("Giraffe version=%v want '~> 5.0'", d)
	}
	// >= constraint preserved.
	if d := depByName(deps, "Microsoft.EntityFrameworkCore"); d == nil || d.Properties["version"] != ">= 7.0.0" {
		t.Errorf("EFCore version=%v want '>= 7.0.0'", d)
	}
	// Exact version with a trailing `prerelease` option → option stripped.
	if d := depByName(deps, "Newtonsoft.Json"); d == nil || d.Properties["version"] != "13.0.3" {
		t.Errorf("Newtonsoft.Json version=%v want '13.0.3' (prerelease option stripped)", d)
	}

	// github file reference is NOT a NuGet package → skipped.
	if d := depByName(deps, "fsprojects/Paket.Restore"); d != nil {
		t.Error("github file reference should be skipped (not a NuGet coordinate)")
	}
}

func TestPaket_DependenciesGroupsAreDev(t *testing.T) {
	deps := depEntities(runExtract(t, "paket.dependencies", realPaketDependencies))

	for _, name := range []string{"Expecto", "FsUnit", "Fake.Core.Target"} {
		d := depByName(deps, name)
		if d == nil {
			t.Fatalf("expected group dep %s", name)
		}
		if d.Properties["is_dev"] != "true" {
			t.Errorf("%s should be is_dev=true (Test/Build group)", name)
		}
		if d.Properties["dependency_kind"] != "dev" {
			t.Errorf("%s dependency_kind=%q want dev", name, d.Properties["dependency_kind"])
		}
	}
}

func TestPaket_DependenciesRuntimeWinsOverDev(t *testing.T) {
	// A package declared in both Main (runtime) and a Test group keeps runtime.
	deps := depEntities(runExtract(t, "paket.dependencies",
		`nuget FSharp.Core ~> 8.0

group Test
    nuget FSharp.Core
`))
	d := depByName(deps, "FSharp.Core")
	if d == nil {
		t.Fatal("expected FSharp.Core")
	}
	if d.Properties["is_dev"] != "false" {
		t.Errorf("FSharp.Core declared in Main should win runtime over the Test re-declaration")
	}
	if d.Properties["version"] != "~> 8.0" {
		t.Errorf("FSharp.Core version=%q want '~> 8.0' (the runtime declaration)", d.Properties["version"])
	}
}

// realPaketLock is a representative paket.lock lockfile: a Main-group NUGET
// block with resolved versions and transitive constraint sub-lines, plus a
// GROUP Test block, plus a GITHUB section (honest-skipped).
const realPaketLock = `NUGET
  remote: https://api.nuget.org/v3/index.json
    FSharp.Core (8.0.100)
    Giraffe (5.0.0)
      FSharp.Core (>= 4.7)
      Microsoft.AspNetCore.Http.Abstractions (>= 2.2)
    Microsoft.EntityFrameworkCore (7.0.10)

GITHUB
  remote: fsprojects/Paket.Restore
    install.fsx (abcdef0123)

GROUP Test
NUGET
  remote: https://api.nuget.org/v3/index.json
    Expecto (9.0.4)
`

func TestPaket_LockResolvedDeps(t *testing.T) {
	deps := depEntities(runExtract(t, "paket.lock", realPaketLock))
	// Resolved NUGET packages recorded: FSharp.Core, Giraffe,
	// Microsoft.EntityFrameworkCore (resolved top-level entries with bare
	// versions), and Expecto (under GROUP Test). The transitive constraint
	// sub-lines (Giraffe→FSharp.Core (>= 4.7), Giraffe→Http.Abstractions (>= 2.2))
	// carry a constraint operator, not a resolved version, so they are NOT
	// recorded as resolved packages (honest: a constraint-only line is not a pin).
	for _, d := range deps {
		if d.Properties["package_manager"] != "paket" {
			t.Errorf("%s: package_manager=%q want paket", d.Name, d.Properties["package_manager"])
		}
		if d.Properties["dependency_kind"] != "locked" {
			t.Errorf("%s: dependency_kind=%q want locked", d.Name, d.Properties["dependency_kind"])
		}
	}

	// Resolved version captured.
	if d := depByName(deps, "FSharp.Core"); d == nil || d.Properties["version"] != "8.0.100" {
		t.Errorf("FSharp.Core version=%v want '8.0.100' (resolved)", d)
	}
	if d := depByName(deps, "Giraffe"); d == nil || d.Properties["version"] != "5.0.0" {
		t.Errorf("Giraffe version=%v want '5.0.0' (resolved)", d)
	}
	if d := depByName(deps, "Microsoft.EntityFrameworkCore"); d == nil || d.Properties["version"] != "7.0.10" {
		t.Errorf("EFCore version=%v want '7.0.10' (resolved)", d)
	}

	// A package that appears ONLY as a transitive constraint sub-line (>= 2.2),
	// with no resolved top-level entry, is not recorded as a resolved pin.
	if d := depByName(deps, "Microsoft.AspNetCore.Http.Abstractions"); d != nil {
		t.Error("constraint-only transitive line should not be recorded as a resolved package")
	}

	// GITHUB section file reference is skipped.
	if d := depByName(deps, "install.fsx"); d != nil {
		t.Error("GITHUB file reference should be skipped in paket.lock")
	}
}

func TestPaket_LockGroupTestIsDev(t *testing.T) {
	deps := depEntities(runExtract(t, "paket.lock", realPaketLock))
	d := depByName(deps, "Expecto")
	if d == nil {
		t.Fatal("expected resolved dep Expecto under GROUP Test")
	}
	if d.Properties["is_dev"] != "true" {
		t.Errorf("Expecto should be is_dev=true (GROUP Test)")
	}
}

func TestPaket_LockTransitiveConstraintDeduped(t *testing.T) {
	// FSharp.Core appears as both a resolved top-level entry (8.0.100) and a
	// transitive constraint sub-line (>= 4.7) under Giraffe — only the resolved
	// entry is recorded, the constraint line is de-duplicated.
	deps := depEntities(runExtract(t, "paket.lock", realPaketLock))
	count := 0
	for _, d := range deps {
		if d.Name == "FSharp.Core" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("FSharp.Core should appear once (resolved entry), got %d", count)
	}
	if d := depByName(deps, "FSharp.Core"); d != nil && d.Properties["version"] != "8.0.100" {
		t.Errorf("FSharp.Core recorded version=%q want resolved '8.0.100', not the constraint", d.Properties["version"])
	}
}

func TestPaket_DependsOnEdges(t *testing.T) {
	rels := dependsOnRels(runExtract(t, "paket.dependencies",
		`nuget Giraffe ~> 5.0
`))
	if len(rels) != 1 {
		t.Fatalf("expected 1 DEPENDS_ON edge, got %d", len(rels))
	}
	if rels[0].Properties["package_manager"] != "paket" {
		t.Errorf("edge package_manager=%q want paket", rels[0].Properties["package_manager"])
	}
}

func TestPaket_NoDependencies(t *testing.T) {
	deps := depEntities(runExtract(t, "paket.dependencies",
		`source https://api.nuget.org/v3/index.json
storage: none
framework: net8.0
`))
	if len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d: %+v", len(deps), depNames(deps))
	}
}
