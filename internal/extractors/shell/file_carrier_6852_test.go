package shell_test

// file_carrier_6852_test.go — #6852, shell arm.
//
// makeImportStub (shell.go) stamps `FromID: file.Path` on the IMPORTS edge of
// every `source <path>` / `. <path>` stub. internal/resolve/refs.go has no
// path→entity index, so a path-valued FromID resolves if and only if some
// emitted node carries that exact string as its Name. Same defect #6815 fixed
// in erlang/nim/groovy and #6852 fixed in bicep (#6864), terraform (#6871),
// html (#6879) and fsharp (#6880); same fix, extractor.PrependFileCarrier.
//
// ONE ANCHORING SITE, N EDGES. makeImportStub is the only bare `FromID: path`
// in the whole package — buildScriptComponent's CONTAINS edges and
// extractCallRelationships' CALLS edges leave FromID EMPTY, and extractRegex
// (the TSTree == nil fallback) emits no relationships at all. But one stub is
// emitted per `source`/`.` command, so a script with four of them anchors four
// edges and must still gain exactly ONE carrier.
//
// THE DEPTH SPLIT IS SHELL'S OWN SHAPE, and it is neither terraform's nor
// html's. buildScriptComponent emits a file-level SCOPE.Component named
// BASENAME(file.Path) — so at a ROOT path that name already IS the path and the
// edge resolved by the accident #6367 documents, exactly as hcl's did. BUT it
// is emitted ONLY when the file declares at least one function
// (`if len(fnEntities) > 0`). So shell dangles at:
//
//   - every NESTED path, function or no function (basename != path); and
//   - the ROOT path of a script with NO function definitions, where no
//     file-level component is emitted at all.
//
// A shell script that only sources libraries and runs top-level commands is the
// ordinary case, not a corner, so the root hole is real rather than notional.
// Both holes are driven below, and the surviving accident (root + functions) is
// pinned as a clause-3 rejection rather than left to chance.
//
// CLAUSE 3 IS REACHED BY TWO ROUTES HERE, one of which no earlier arm had.
// FileCarrierFor clause 3 is `records[i].Name == path` (clause 1 is the
// empty-path guard, clause 2 the anchoring test). It fires for shell when:
//
//   - the SCRIPT COMPONENT is named the path — root depth only, as hcl's is; or
//   - an IMPORT STUB is named the path, because makeImportStub names the stub
//     after the SOURCED path verbatim, so a script that sources ITSELF by the
//     spelling of its own path mints a record named exactly the path. Unlike
//     fsharp — whose import marker provably can never be the path — shell's
//     CAN, at BOTH depths. graph.EntityID hashes (repo, kind, name, sourceFile)
//     and NOT Subtype (#6369 / PR #6480), so a carrier there would land a
//     second SCOPE.Component under the stub's id.
//
// GRADED IN BOTH DIRECTIONS. A recall-shaped assertion ("the carrier exists")
// licenses an UNCONDITIONAL carrier, which would mint one bare orphan node per
// .sh/.bash/.zsh/.ksh file across a whole repo — a change no recall-shaped
// assertion can see. The forbidden-row controls below are what forbid it.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/shell"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// runShell6852 drives the registered production extractor over src at path with
// a real bash parse tree.
func runShell6852(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("shell")
	if !ok {
		t.Fatal("shell extractor not registered")
	}
	in := extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "shell",
	}
	if src != "" {
		in.TSTree = parseForTest(t, src)
	}
	recs, err := ext.Extract(context.Background(), in)
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return recs
}

// shCarriers6852 returns every record that IS the file carrier for path — the
// SCOPE.Component/file record extractor.FileEntity mints. Subtype "file" is
// what separates it from buildScriptComponent's Subtype "script".
func shCarriers6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.Name == path {
			out = append(out, r)
		}
	}
	return out
}

// shPathAnchored6852 returns every relationship in recs whose FromID is exactly
// path — the shape whose FROM end has nothing to resolve onto.
func shPathAnchored6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.FromID == path {
				out = append(out, r)
			}
		}
	}
	return out
}

// shNamedExactly6852 returns every record whose Name or QualifiedName is path.
// This is the resolution question internal/resolve/refs.go actually asks: it
// has no path→entity index, so a path-valued FromID resolves if and only if
// such a record exists.
func shNamedExactly6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Name == path || r.QualifiedName == path {
			out = append(out, r)
		}
	}
	return out
}

// resolveShell6852 extracts src at path, stamps ids the way graph assembly
// does, runs the production resolver pipeline, and returns the records plus the
// id→record index. This asserts on the EMITTED ARTEFACT after resolution — the
// edge's FROM end — not on a helper's return value or a counter.
func resolveShell6852(t *testing.T, src, path string) ([]types.EntityRecord, map[string]*types.EntityRecord) {
	t.Helper()
	recs := runShell6852(t, src, path)
	if len(recs) == 0 {
		t.Fatalf("extract %s: no records", path)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6852", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))
	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	return recs, byID
}

// assertImportsResolve6852 fails for every IMPORTS edge whose FROM end names no
// record, and fails outright when the fixture produced no IMPORTS at all — a
// resolution assertion over an empty set is a no-op that reads like a guard.
func assertImportsResolve6852(t *testing.T, recs []types.EntityRecord, byID map[string]*types.EntityRecord) {
	t.Helper()
	seen := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			seen++
			if _, ok := byID[r.FromID]; !ok {
				t.Errorf("IMPORTS owned by %q: FROM end %q resolves to no record "+
					"(refs.go has no path→entity index; a path-valued FromID resolves "+
					"iff some record carries that exact string as its Name — emit a file "+
					"carrier, internal/extractor/file_carrier.go)", recs[i].Name, r.FromID)
			}
		}
	}
	if seen == 0 {
		t.Fatal("fixture produced no IMPORTS edges — this measurement is vacuous")
	}
}

// carrierSrcShellNoFns6852 sources two libraries and declares NO function, so
// buildScriptComponent's `len(fnEntities) > 0` guard emits no file-level
// component at EITHER depth. The top-level commands are there so the script is
// a real script rather than a bare pair of source lines.
const carrierSrcShellNoFns6852 = `#!/usr/bin/env bash
set -euo pipefail

source ./lib/logging.sh
. /etc/profile.d/env.sh

echo "starting"
exit 0
`

// carrierSrcShellWithFns6852 sources one library AND declares functions, so a
// file-level SCOPE.Component named BASENAME(path) IS emitted. It is the
// contrast fixture for the depth split.
const carrierSrcShellWithFns6852 = `#!/usr/bin/env bash

source ./lib/logging.sh

log() {
    echo "$*"
}

main() {
    log hi
}
`

// TestShell_SourceImportsFromEndResolves_6852 is the fix's behavioural test for
// the case shell has and terraform does not: a script that sources libraries
// and declares NO functions emits no file-level component at ALL, so its
// IMPORTS FROM end dangled at BOTH depths.
//
// Axis VARIED: path DEPTH (nested and root). HELD CONSTANT: the source, which
// declares no function at either depth.
func TestShell_SourceImportsFromEndResolves_6852(t *testing.T) {
	for _, path := range []string{"scripts/ci/deploy.sh", "deploy.sh"} {
		t.Run(path, func(t *testing.T) {
			recs, byID := resolveShell6852(t, carrierSrcShellNoFns6852, path)
			// Premise: the fixture really does declare no function, so the
			// accidental resolution of the next test cannot be what makes this
			// one pass.
			if n := len(shNamedExactly6852(runShell6852(t, carrierSrcShellNoFns6852, path), path)); n != 1 {
				t.Fatalf("premise: exactly 1 record may be named %q (the carrier), got %d", path, n)
			}
			assertImportsResolve6852(t, recs, byID)
		})
	}
}

// TestShell_ScriptWithFunctionsImportsFromEndResolves_6852 drives the OTHER
// half of the depth split. buildScriptComponent names its file-level component
// BASENAME(path), so at the ROOT path that name already IS the path and this
// edge resolved by the #6367 accident before the carrier existed; at a NESTED
// path it never did. Both are asserted through one code path so the accident
// cannot be mistaken for the fix, and so a carrier wired to some functions-only
// branch cannot pass the test above and leave this one dangling.
//
// Axis VARIED: path DEPTH. HELD CONSTANT: the source, which declares functions
// at both depths.
func TestShell_ScriptWithFunctionsImportsFromEndResolves_6852(t *testing.T) {
	for _, path := range []string{"scripts/ci/deploy.sh", "deploy.sh"} {
		t.Run(path, func(t *testing.T) {
			recs, byID := resolveShell6852(t, carrierSrcShellWithFns6852, path)
			assertImportsResolve6852(t, recs, byID)
		})
	}
}

// TestShell_NoCarrierWithoutASource_6852 is the OVER-FIRING control, and it is
// the half of the grade a "the edge now resolves" test cannot supply. Axis
// VARIED: the `source`/`.` directives (absent). HELD CONSTANT: a full record
// set — a script component and two functions with a CALLS edge between them —
// so the file still extracts plenty and still exercises every other pass; only
// the path-anchored edge is gone.
func TestShell_NoCarrierWithoutASource_6852(t *testing.T) {
	const src = `#!/usr/bin/env bash

log() {
    echo "$*"
}

main() {
    log hi
    docker build -t app .
}
`
	for _, path := range []string{"scripts/ci/deploy.sh", "deploy.sh"} {
		t.Run(path, func(t *testing.T) {
			recs := runShell6852(t, src, path)
			if len(recs) == 0 {
				t.Fatal("premise: fixture produced no records at all")
			}
			if n := len(shPathAnchored6852(recs, path)); n != 0 {
				t.Fatalf("premise: want 0 path-anchored relationships, got %d", n)
			}
			if n := len(shCarriers6852(recs, path)); n != 0 {
				t.Errorf("a shell script with nothing to carry must emit no file carrier, got %d — "+
					"an unconditional carrier mints one bare orphan node per shell file across a "+
					"whole repo, which no recall-shaped assertion can see", n)
			}
			// Forbidden-row form: a carrier smuggled in under a different Kind
			// or Subtype is caught too. Scoped to the record set that is
			// LEGITIMATELY empty here — at a ROOT path buildScriptComponent's
			// component is named the path on purpose, so the "nothing may be
			// named the path" row would be false for a reason unrelated to the
			// carrier and is asserted at the nested depth only.
			for _, r := range recs {
				if r.Subtype == "file" {
					t.Errorf("no Subtype=\"file\" record may exist here, got kind=%q name=%q",
						r.Kind, r.Name)
				}
			}
			if path == "scripts/ci/deploy.sh" {
				for _, r := range shNamedExactly6852(recs, path) {
					t.Errorf("no record may be named %q here, got kind=%q subtype=%q name=%q qname=%q",
						path, r.Kind, r.Subtype, r.Name, r.QualifiedName)
				}
			}
		})
	}
}

// TestShell_EmptyFileGetsNoCarrier_6852 drives Extract's FIRST return path:
// len(file.Content) == 0 returns nil before anything else runs. A carrier
// placed at the head of Extract rather than after the walk would mint a node
// for a file with no content whatsoever.
func TestShell_EmptyFileGetsNoCarrier_6852(t *testing.T) {
	const path = "scripts/ci/empty.sh"
	recs := runShell6852(t, "", path)
	if len(recs) != 0 {
		t.Fatalf("an empty shell file must extract no records at all, got %d (first: kind=%q name=%q)",
			len(recs), recs[0].Kind, recs[0].Name)
	}
}

// TestShell_RegexFallbackGetsNoCarrier_6852 drives Extract's SECOND return
// path: TSTree == nil falls back to extractRegex, which recovers function
// records only and emits no relationships — so nothing anchors on the path and
// no carrier is due.
//
// SCORED IN ITS OWN DIRECTION rather than left as an absence over a set that
// can never contain the thing. The mutant it kills is an UNCONDITIONAL carrier
// placed at the head of Extract, above the TSTree nil check: that mutant leaves
// every test above green (the carrier still exists where it is wanted) and
// fails only here. The fixture deliberately contains a `source` line, so a
// carrier keyed on the TEXT rather than on the emitted relationships would be
// caught too.
func TestShell_RegexFallbackGetsNoCarrier_6852(t *testing.T) {
	const src = `#!/usr/bin/env bash
source ./lib/logging.sh

log() {
    echo "$*"
}
`
	const path = "scripts/ci/deploy.sh"
	ext, ok := extractor.Get("shell")
	if !ok {
		t.Fatal("shell extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "shell",
		// TSTree deliberately nil — the extractRegex fallback.
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("premise: the regex fallback produced no records, so this control grades nothing")
	}
	if n := len(shPathAnchored6852(recs, path)); n != 0 {
		t.Fatalf("premise: the regex fallback must emit no path-anchored relationship, got %d", n)
	}
	if n := len(shCarriers6852(recs, path)); n != 0 {
		t.Errorf("the regex fallback must emit no file carrier, got %d", n)
	}
	for _, r := range shNamedExactly6852(recs, path) {
		t.Errorf("no record may be named %q on the regex fallback, got kind=%q subtype=%q",
			path, r.Kind, r.Subtype)
	}
}

// TestShell_OneCarrierPerFileNotPerSource_6852 is the multiplicity control.
// Axis VARIED: the NUMBER of `source`/`.` directives (four, each its own stub
// with its own path-anchored IMPORTS). HELD CONSTANT: one file, one path,
// driven at both depths. The carrier is per-FILE, not per-EDGE; a per-edge
// carrier would put four nodes under one id.
func TestShell_OneCarrierPerFileNotPerSource_6852(t *testing.T) {
	const src = `#!/usr/bin/env bash
source ./lib/a.sh
source ./lib/b.sh
. ./lib/c.sh
. /etc/profile.d/d.sh
echo done
`
	for _, path := range []string{"scripts/ci/deploy.sh", "deploy.sh"} {
		t.Run(path, func(t *testing.T) {
			recs := runShell6852(t, src, path)
			if n := len(shPathAnchored6852(recs, path)); n != 4 {
				t.Fatalf("premise: want 4 path-anchored IMPORTS edges, got %d", n)
			}
			if n := len(shCarriers6852(recs, path)); n != 1 {
				t.Errorf("4 source directives must still yield exactly 1 file carrier, got %d", n)
			}
			if n := len(shNamedExactly6852(recs, path)); n != 1 {
				t.Errorf("exactly 1 record may be named %q, got %d", path, n)
			}
		})
	}
}

// TestShell_ScriptComponentNamedLikeThePathGetsNoSecondCarrier_6852 drives
// FileCarrierFor CLAUSE 3 by the route hcl reached it: buildScriptComponent
// names the file-level SCOPE.Component BASENAME(file.Path), which at a ROOT
// path already IS the path. graph.EntityID hashes (repo, kind, name,
// sourceFile) and NOT Subtype, so a carrier there would land a second
// SCOPE.Component under the script component's id (#6369 / PR #6480).
//
// The nested subtest is the contrast, not decoration: at scripts/ci/deploy.sh
// the same source's component is named "deploy.sh", which is NOT the path, so
// clause 3 does not fire and the carrier is minted. Without it this test would
// pass for a carrier that was never emitted at all.
func TestShell_ScriptComponentNamedLikeThePathGetsNoSecondCarrier_6852(t *testing.T) {
	t.Run("root path — the script component name IS the path", func(t *testing.T) {
		const path = "deploy.sh"
		recs := runShell6852(t, carrierSrcShellWithFns6852, path)
		if n := len(shPathAnchored6852(recs, path)); n == 0 {
			t.Fatal("premise: the fixture must still anchor an IMPORTS on the path")
		}
		named := shNamedExactly6852(recs, path)
		if len(named) != 1 {
			t.Fatalf("exactly 1 record may be named %q, got %d — clause 3 must reject a second "+
				"carrier when the extractor already minted a path-named record", path, len(named))
		}
		if named[0].Subtype != "script" {
			t.Errorf("the one record named %q must be buildScriptComponent's file-level "+
				"component, got subtype %q", path, named[0].Subtype)
		}
		if n := len(shCarriers6852(recs, path)); n != 0 {
			t.Errorf("no file carrier may be minted when the script component already carries "+
				"the path as its Name, got %d", n)
		}
	})

	t.Run("nested path — the same source DOES get a carrier", func(t *testing.T) {
		const path = "scripts/ci/deploy.sh"
		recs := runShell6852(t, carrierSrcShellWithFns6852, path)
		if n := len(shCarriers6852(recs, path)); n != 1 {
			t.Fatalf("want exactly 1 carrier at a nested path, got %d — without this the root "+
				"subtest above would pass for a carrier that is never emitted anywhere", n)
		}
	})
}

// TestShell_SelfSourcingImportStubGetsNoSecondCarrier_6852 drives clause 3 by
// the route that is SHELL'S OWN, and that no earlier arm had. makeImportStub
// names the stub after the SOURCED path verbatim, so a script that sources
// itself by the spelling of its own path mints a record named exactly the path
// — and does so at BOTH depths, unlike the script component, whose name is a
// basename and can only coincide with a root path. fsharp's import marker
// provably can never be the path; shell's can.
//
// The fixture declares no function, so buildScriptComponent emits nothing and
// the ONLY path-named record is the stub — otherwise the root subtest could
// pass on the script component and grade nothing about the stub.
func TestShell_SelfSourcingImportStubGetsNoSecondCarrier_6852(t *testing.T) {
	for _, path := range []string{"scripts/ci/deploy.sh", "deploy.sh"} {
		t.Run(path, func(t *testing.T) {
			src := "#!/usr/bin/env bash\nsource " + path + "\necho done\n"
			recs := runShell6852(t, src, path)
			if n := len(shPathAnchored6852(recs, path)); n == 0 {
				t.Fatal("premise: the fixture must still anchor an IMPORTS on the path")
			}
			named := shNamedExactly6852(recs, path)
			if len(named) != 1 {
				t.Fatalf("exactly 1 record may be named %q, got %d — a carrier here would land a "+
					"second SCOPE.Component under the import stub's graph.EntityID, which does "+
					"not hash Subtype (#6369/#6480)", path, len(named))
			}
			if named[0].Subtype == "file" {
				t.Errorf("the one record named %q must be the import stub, not a file carrier", path)
			}
			if n := len(shCarriers6852(recs, path)); n != 0 {
				t.Errorf("no file carrier may be minted when an import stub already carries the "+
					"path as its Name, got %d", n)
			}
		})
	}
}

// TestShell_CarrierShape_6852 pins what the carrier IS: stamped shell, anchored
// on the file it names, and owning no relationships of its own — the import
// stubs still carry the IMPORTS edges, so re-homing them onto the carrier would
// double every edge.
func TestShell_CarrierShape_6852(t *testing.T) {
	const path = "scripts/ci/deploy.sh"
	recs := runShell6852(t, carrierSrcShellNoFns6852, path)
	cs := shCarriers6852(recs, path)
	if len(cs) != 1 {
		t.Fatalf("premise: want 1 carrier, got %d", len(cs))
	}
	if cs[0].Language != "shell" {
		t.Errorf("carrier Language = %q, want %q", cs[0].Language, "shell")
	}
	if cs[0].SourceFile != path {
		t.Errorf("carrier SourceFile = %q, want %q", cs[0].SourceFile, path)
	}
	if n := len(cs[0].Relationships); n != 0 {
		t.Errorf("the shell file carrier must own no relationships, got %d", n)
	}
	// The lang ARGUMENT is what is graded here, not the field it ends up in.
	// shell's Extract runs extractor.TagEntitiesLanguage, which fills an EMPTY
	// Language with "shell" — so Language alone cannot tell `"shell"` from `""`.
	// What does tell them apart is Properties: TagEntitiesLanguage only touches
	// a record whose Language is empty, and when it does it also stamps
	// Properties["language"]. Every OTHER shell record sets Language explicitly
	// and therefore carries no such key, so a carrier that acquired one would be
	// the single record in the extraction whose provenance differs from its
	// siblings' — proto's #6356 trap, which is the reason file_carrier.go takes
	// the token as a parameter at all.
	if v, ok := cs[0].Properties["language"]; ok {
		t.Errorf("carrier carries Properties[\"language\"]=%q — it was language-tagged after the "+
			"fact rather than stamped by the lang argument, so it disagrees with every other "+
			"shell record, none of which carries that key", v)
	}
	// THE PREMISE THAT ASSERTION RESTS ON, pinned rather than assumed. The check
	// above distinguishes lang="" from lang="shell" only while NO OTHER record
	// carries the key either. If some future shell record shipped without an
	// explicit Language, TagEntitiesLanguage would fill it and stamp the key —
	// and the check above would go on passing while quietly grading nothing,
	// with the empty-token mutant back to ALIVE and no test going red. Driven
	// over a record set that DOES contain a script component and functions, so
	// the loop is not an absence asserted over two stubs.
	for _, r := range runShell6852(t, carrierSrcShellWithFns6852, path) {
		if v, ok := r.Properties["language"]; ok {
			t.Errorf("record kind=%q subtype=%q name=%q carries Properties[\"language\"]=%q — "+
				"every shell record is meant to set Language explicitly, and the carrier's "+
				"empty-token mutant is only observable while none of them is language-tagged "+
				"after the fact", r.Kind, r.Subtype, r.Name, v)
		}
	}
	if n := len(shPathAnchored6852(recs, path)); n != 2 {
		t.Errorf("the two source IMPORTS edges must still be emitted, got %d", n)
	}
	// #577 convention: the file entity is the FIRST record.
	if recs[0].Name != path {
		t.Errorf("carrier must be record 0 (#577 convention), got %q at index 0", recs[0].Name)
	}
}
