package resolve

import "testing"

// refs_bicep_disposition_6852_test.go — #6852, bicep arm.
//
// The bicep language was picked out of the twelve path-anchored-IMPORTS
// offenders on ONE premise: `.bicep` is absent from sourceFileExtensions, so
// looksLikeSourceFilePath does NOT recognise a .bicep path, classifyDispositionLang
// does not take its DispositionDynamic branch, and a dangling .bicep FROM end
// therefore counts as DispositionBugExtractor — the headline metric — instead
// of being masked the way the other eleven languages' paths are.
//
// This test GROUNDS that premise instead of leaving it in prose. It is a
// CHARACTERISATION row, not a requirement: adding `.bicep` to the allowlist is
// a separate, deliberate decision (the carrier fix does not need it, and it
// would silently reclassify existing dangling edges). If someone makes that
// decision, this row goes red and should be updated with the reason, not
// deleted.
func TestBicepPathIsNotMaskedAsDynamic6852(t *testing.T) {
	for _, p := range []string{"infra/envs/prod/main.bicep", "main.bicep"} {
		if looksLikeSourceFilePath(p) {
			t.Errorf("looksLikeSourceFilePath(%q) = true — `.bicep` is now in "+
				"sourceFileExtensions, which masks dangling bicep FROM ends as "+
				"DispositionDynamic. That is a separate decision from the #6852 "+
				"carrier fix; update this characterisation row deliberately.", p)
		}
	}
	// Positive control: without it, a helper that returned false for
	// everything would leave the rows above vacuously green.
	for _, p := range []string{"infra/main.tf", "src/app.go", "index.html"} {
		if !looksLikeSourceFilePath(p) {
			t.Errorf("positive control: looksLikeSourceFilePath(%q) = false, want true", p)
		}
	}
}

// TestBicepModuleTargetsAreNeverSourceFilePaths6852 grounds the OTHER half of
// the extension decision: adding `.bicep` to sourceFileExtensions would not
// change how bicep's own emissions are classified, so the two defects really
// are separable and the carrier fix does not create a reason to touch the
// allowlist.
//
// bicep's module IMPORTS edge has exactly two endpoints. The FROM end is the
// file path, and after the #6852 carrier it resolves to a real record, so it
// never reaches classifyDispositionLang's fallback at all. The TO end is a
// structural ref — `scope:component:file:bicep:<modPath>` for a local module,
// `scope:component:external:bicep:<scheme>:<ref>` for a registry one — and
// looksLikeSourceFilePath rejects ANY string containing ':' before it ever
// consults the extension list. So a `.bicep`-suffixed ToID is not a candidate
// for the masking branch whether or not `.bicep` is in the allowlist.
//
// THE LAST ROW IS THE ONE THAT GRADES THE GUARD, and it is here because the
// first three do NOT. Measured: deleting the ':' from the ContainsAny guard
// (refs.go, `strings.ContainsAny(s, ": \\")`) leaves rows 1-3 green — every one
// of them ends in `.bicep` or `:v1`, neither of which is in
// sourceFileExtensions, so the extension loop rejects them on its own and the
// ':' guard is never exercised. A test green for the reason it claims
// independence from is not a test. The `…:go:src/app.go` row ends in `.go`,
// which the extension list DOES accept, so it can only be rejected by the ':'
// guard — and that mutant is dead only because this row exists.
func TestBicepModuleTargetsAreNeverSourceFilePaths6852(t *testing.T) {
	for _, to := range []string{
		"scope:component:file:bicep:./modules/network.bicep",
		"scope:component:file:bicep:modules/database.bicep",
		"scope:component:external:bicep:br:contoso.azurecr.io/bicep/modules/storage:v1",
		// Same structural-ref SHAPE, but with a tail the extension allowlist
		// accepts. This is the only row whose verdict depends on the ':' guard.
		"scope:component:file:go:src/app.go",
	} {
		if looksLikeSourceFilePath(to) {
			t.Errorf("looksLikeSourceFilePath(%q) = true — a structural ref is not a "+
				"source path; the ':' guard must reject it BEFORE the extension "+
				"allowlist is consulted, which is what the #6852 sourceFileExtensions "+
				"decision rests on", to)
		}
	}
}
