// Package resolve — M5 production-parity guard.
//
// Issue #4331 investigated wiring BuildIndexFromModules (M5, #2182/#2184)
// into the production index pipeline (cmd/archigraph/index.go) in place of
// BuildIndex. The investigation found a CONCRETE edge-set divergence on the
// platform-variant code path, so M5 was NOT wired. This file is the
// regression guard: it pins the known divergence so any future wiring attempt
// must first make this test assert parity (not divergence) before flipping the
// production call site.
//
// # THE DIVERGENCE
//
// BuildIndex consumes entities in production extraction order (the order of the
// `merged` slice). BuildIndexFromModules re-sorts: modules by ModuleKey, then
// entities WITHIN each module by entity ID (BuildModuleSymbols sorts by ID for
// O(N) collision detection). The platform-variant merge in byPackageOperation /
// byPackageComponent (issue #1818) is ORDER-SENSITIVE for 3+ mutually-exclusive
// variants of the same (pkgDir, name): the pairwise canonical-chaining produces
// a different PlatformVariants topology depending on which variant is seen
// first. PlatformVariants is consumed by ReferencesEmbeddedWithAllowlist
// (refs.go) to CLONE CALLS edges onto every non-canonical sibling, so a
// different topology = a different output edge set. This is a correctness
// divergence, not just a speed difference.
//
// The pre-existing TestBuildIndexFromModules_Parity does NOT catch this: its
// synthetic fixture uses globally-unique names, so no collision — and therefore
// no platform-variant merge — ever fires.
package resolve

import (
	"reflect"
	"sort"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// platformVariantTriple builds three mutually-exclusive GOOS variants of one
// top-level operation "Run" in package dir "svc/". Entity IDs are deliberately
// NOT in source-file order, so BuildModuleSymbols' sort-by-ID reorders them
// relative to the production extraction order used by the returned `merged`.
func platformVariantTriple() (merged []types.EntityRecord, modules map[ModuleKey][]types.EntityRecord) {
	eDarwin := types.EntityRecord{ID: "cccccccccccccccc", Kind: "SCOPE.Operation", Name: "Run",
		SourceFile: "svc/run_darwin.go", Properties: map[string]string{"build_tag": "darwin"}}
	eLinux := types.EntityRecord{ID: "aaaaaaaaaaaaaaaa", Kind: "SCOPE.Operation", Name: "Run",
		SourceFile: "svc/run_linux.go", Properties: map[string]string{"build_tag": "linux"}}
	eWindows := types.EntityRecord{ID: "bbbbbbbbbbbbbbbb", Kind: "SCOPE.Operation", Name: "Run",
		SourceFile: "svc/run_windows.go", Properties: map[string]string{"build_tag": "windows"}}

	// Production extraction order (arbitrary, as a real extractor would emit).
	merged = []types.EntityRecord{eWindows, eDarwin, eLinux}
	// All three live in the SAME package dir -> SAME M5 module.
	modules = map[ModuleKey][]types.EntityRecord{"svc": {eWindows, eDarwin, eLinux}}
	return merged, modules
}

func normalizePV(m map[string][]string) map[string][]string {
	out := make(map[string][]string, len(m))
	for k, v := range m {
		cp := append([]string(nil), v...)
		sort.Strings(cp)
		out[k] = cp
	}
	return out
}

// TestM5_PlatformVariantParity_KnownDivergence pins the #4331 finding: on a
// 3-way platform-variant collision, BuildIndex and BuildIndexFromModules
// produce DIFFERENT PlatformVariants topologies. While this asserts divergence,
// M5 must remain UNWIRED.
//
// TO WIRE M5: make BuildModuleSymbols / the merge preserve production
// extraction order for the platform-variant side-tables (or make the
// platform-variant merge order-independent), then flip the two asserts below
// from NotEqual to Equal and update cmd/archigraph/index.go.
func TestM5_PlatformVariantParity_KnownDivergence(t *testing.T) {
	merged, modules := platformVariantTriple()

	flat := BuildIndex(merged)
	mod := BuildIndexFromModules(modules, 0)

	// Canonical winner DOES agree (chosen by lexicographic SourceFile, which is
	// order-independent): both pick svc/run_darwin.go's entity.
	flatWinner := flat.byPackageOperation["svc"]["Run"]
	modWinner := mod.byPackageOperation["svc"]["Run"]
	if flatWinner != modWinner {
		t.Errorf("unexpected: byPackageOperation winner already diverges (flat=%q mod=%q)",
			flatWinner, modWinner)
	}

	// But the PlatformVariants fan-out topology DIVERGES. This is the blocker.
	flatPV := normalizePV(flat.PlatformVariants)
	modPV := normalizePV(mod.PlatformVariants)
	if reflect.DeepEqual(flatPV, modPV) {
		t.Fatalf("EXPECTED DIVERGENCE IS GONE — M5 may now be parity-safe to wire.\n"+
			"flat=%v mod=%v\nIf so: flip this assert to require equality and proceed with wiring (#4331).",
			flatPV, modPV)
	}
	t.Logf("confirmed #4331 divergence (M5 stays unwired):\n  BuildIndex          PlatformVariants=%v\n  BuildIndexFromModules PlatformVariants=%v",
		flat.PlatformVariants, mod.PlatformVariants)
}
