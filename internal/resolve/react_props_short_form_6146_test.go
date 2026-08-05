package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6146 — the PLT #537 short-form structural tier (refs.go:2012, the
// `scope:schema:<file>#<member>` branch of lookupStructural) had ZERO coverage
// in this package until #6122 added one neo4j-shaped test. Measured: disable
// the tier, run ./internal/resolve/... -v, and out of ~2060 `=== RUN` entries
// exactly one top-level test fails — the neo4j one. Before that commit the
// tier could have been deleted outright and this suite stayed green.
//
// Its real consumer is internal/extractors/cross/react_props: every HAS_PROPS
// edge on a .tsx component resolves its ToID through this branch. (The
// resolver comment called that kind "USES_PROPS"; no such RelationshipKind
// exists — it is types.RelationshipKindHasProps = "HAS_PROPS", kinds.go:517.
// Corrected in the same change.)
//
// Everything below asserts what an endpoint BINDS TO, by kind / name /
// source_file, in both directions. Nothing here counts dangling edges: a
// count reads a mis-bind as an improvement, which is the whole subject of
// #6123 and #6122.
//
// The tier-level subtests call idx.lookupStructural directly rather than
// only driving ReferencesEmbedded. That is deliberate — #6123's lesson is
// that a guard can be covered twice and look alive when only one mechanism
// is doing the work, so the tier gets a test that no other mechanism can
// satisfy on its behalf.

const (
	propsFile6146 = "src/components/AdditionalInfoFields.tsx"
	propsType6146 = "AdditionalInfoFieldsProps"
	propsComp6146 = "AdditionalInfoFields"
	propsRef6146  = "scope:schema:" + propsFile6146 + "#" + propsType6146
	compRef6146   = "scope:operation:" + propsFile6146 + "#" + propsComp6146
)

// reactPropsFixture6146 mirrors what the pipeline actually produces for one
// .tsx file: the SCOPE.Operation component record (react_props
// buildComponentEntity) and the SCOPE.Schema props-interface record
// (buildPropsInterfaceEntity), plus a same-named props interface in a
// DIFFERENT file. That third entity is the point of the fixture — "binds
// correctly" is only meaningful when a wrong target of the identical name is
// available to bind to.
func reactPropsFixture6146() (comp, props, otherFileProps types.EntityRecord) {
	comp = types.EntityRecord{
		ID: "1a1a1a1a1a1a1a1a", Kind: "SCOPE.Operation", Subtype: "react_component",
		Name: propsComp6146, SourceFile: propsFile6146, Language: "typescript",
	}
	props = types.EntityRecord{
		ID: "2b2b2b2b2b2b2b2b", Kind: "SCOPE.Schema", Subtype: "react_props_interface",
		Name: propsType6146, SourceFile: propsFile6146, Language: "typescript",
	}
	otherFileProps = types.EntityRecord{
		ID: "3c3c3c3c3c3c3c3c", Kind: "SCOPE.Schema", Subtype: "react_props_interface",
		Name: propsType6146, SourceFile: "src/legacy/AdditionalInfoFields.tsx",
		Language: "typescript",
	}
	return comp, props, otherFileProps
}

// bindingOf6146 returns the fixture entity an endpoint landed on, so
// assertions can be phrased in kind/name/source_file terms rather than in
// opaque IDs. Producers salt IDs and (from, to, kind) is not a unique
// relationship key, so identity has to be read off the entity record.
func bindingOf6146(t *testing.T, ents []types.EntityRecord, id string) types.EntityRecord {
	t.Helper()
	for _, e := range ents {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("endpoint %q matches no entity in the fixture — it was neither bound "+
		"nor left verbatim as a recognisable stub", id)
	return types.EntityRecord{}
}

func assertBinding6146(t *testing.T, got types.EntityRecord, wantKind, wantName, wantFile, what string) {
	t.Helper()
	if got.Kind != wantKind || got.Name != wantName || got.SourceFile != wantFile {
		t.Fatalf("%s bound to {kind=%q name=%q source_file=%q}, want {kind=%q name=%q source_file=%q}",
			what, got.Kind, got.Name, got.SourceFile, wantKind, wantName, wantFile)
	}
}

// TestReactPropsHasPropsBindsBothEndpointsByContent6146 is the end-to-end arm:
// the HAS_PROPS edge as react_props emits it (embedded on the component
// record, both endpoints structural stubs) driven through ReferencesEmbedded,
// asserted bidirectionally on entity content.
func TestReactPropsHasPropsBindsBothEndpointsByContent6146(t *testing.T) {
	comp, props, otherFileProps := reactPropsFixture6146()
	all := []types.EntityRecord{comp, props, otherFileProps}
	idx := BuildIndex(all)

	recs := []types.EntityRecord{{
		ID: comp.ID, Kind: comp.Kind, Name: comp.Name,
		SourceFile: comp.SourceFile, Language: comp.Language,
		Relationships: []types.RelationshipRecord{{
			FromID: compRef6146,
			ToID:   propsRef6146,
			Kind:   string(types.RelationshipKindHasProps),
			Properties: types.Props{
				{K: "props_type", V: propsType6146},
			},
		}},
	}}
	ReferencesEmbedded(recs, idx)
	edge := recs[0].Relationships[0]

	assertBinding6146(t, bindingOf6146(t, all, edge.ToID),
		"SCOPE.Schema", propsType6146, propsFile6146,
		"HAS_PROPS ToID (the PLT #537 short-form tier)")
	assertBinding6146(t, bindingOf6146(t, all, edge.FromID),
		"SCOPE.Operation", propsComp6146, propsFile6146,
		"HAS_PROPS FromID")

	if edge.ToID == otherFileProps.ID {
		t.Fatalf("HAS_PROPS ToID took the same-named props interface in %q — the "+
			"byLocation probe must be file-scoped", otherFileProps.SourceFile)
	}
}

// TestReactPropsShortFormTierBindsInPackage6146 is the same fact asserted
// AT THE TIER, not through the pipeline. If the branch at refs.go:2012 is
// removed, `scope:schema:<file>#<name>` has three colon-segments, so the
// SplitN at refs.go:2129 yields len(parts) != 6 and lookupStructural returns
// ("", statusUnmatched, true) — this subtest fails on the returned ID, which
// is the intended assertion and not a compile error or a collateral one.
func TestReactPropsShortFormTierBindsInPackage6146(t *testing.T) {
	comp, props, otherFileProps := reactPropsFixture6146()
	idx := BuildIndex([]types.EntityRecord{comp, props, otherFileProps})

	id, status, handled := idx.lookupStructural(propsRef6146)
	if !handled {
		t.Fatalf("lookupStructural did not claim %q (handled=false)", propsRef6146)
	}
	if status != statusRewritten {
		t.Fatalf("lookupStructural(%q) status=%d, want statusRewritten=%d — the PLT "+
			"#537 short-form branch did not fire", propsRef6146, status, statusRewritten)
	}
	if id != props.ID {
		t.Fatalf("lookupStructural(%q) = %q, want the same-file SCOPE.Schema %q "+
			"(%s in %s)", propsRef6146, id, props.ID, props.Name, props.SourceFile)
	}
}

// TestReactPropsShortFormTierIsFileScoped6146 pins the byLocation[file] probe
// itself: the props type exists, but only in another file. The tier must
// decline rather than reach across files, and the stub must survive verbatim
// so the edge dangles honestly.
func TestReactPropsShortFormTierIsFileScoped6146(t *testing.T) {
	comp, _, otherFileProps := reactPropsFixture6146()
	idx := BuildIndex([]types.EntityRecord{comp, otherFileProps})

	id, status, handled := idx.lookupStructural(propsRef6146)
	if !handled {
		t.Fatalf("lookupStructural did not claim %q (handled=false)", propsRef6146)
	}
	if id != "" || status != statusUnmatched {
		t.Fatalf("lookupStructural(%q) = (%q, status=%d) with the props type present "+
			"only in %q; want (\"\", statusUnmatched=%d). A cross-file bind here is a "+
			"confident wrong answer, not a recovery",
			propsRef6146, id, status, otherFileProps.SourceFile, statusUnmatched)
	}

	recs := []types.EntityRecord{{
		ID: comp.ID, Kind: comp.Kind, Name: comp.Name,
		SourceFile: comp.SourceFile, Language: comp.Language,
		Relationships: []types.RelationshipRecord{{
			FromID: compRef6146, ToID: propsRef6146,
			Kind: string(types.RelationshipKindHasProps),
		}},
	}}
	ReferencesEmbedded(recs, idx)
	if got := recs[0].Relationships[0].ToID; got != propsRef6146 {
		t.Fatalf("HAS_PROPS ToID was rewritten to %q; want the stub %q left verbatim",
			got, propsRef6146)
	}
}

// TestReactPropsShortFormTierAmbiguousSameFile6146 covers the other decline
// path: two entities share (file, name), so BuildIndex blanks the byLocation
// entry (refs.go:1189-1197) and records ambigLocation. The tier's probe misses
// and the stub survives. Without this the tier would pick whichever record
// BuildIndex saw last.
//
// Honest note on redundancy (#6123's lesson runs both ways): this behaviour is
// protected TWICE. Making byLocation last-writer-wins fails this test AND
// TestReferences_StructuralLocationAmbiguous. Kept anyway because that other
// test exercises the six-segment Format A path and would not notice a
// regression confined to the three-segment branch — but a pass here is not
// independent evidence that this branch is what is doing the work.
func TestReactPropsShortFormTierAmbiguousSameFile6146(t *testing.T) {
	comp, props, _ := reactPropsFixture6146()
	twin := props
	twin.ID = "4d4d4d4d4d4d4d4d"
	twin.Kind = "SCOPE.Component"
	idx := BuildIndex([]types.EntityRecord{comp, props, twin})

	id, _, handled := idx.lookupStructural(propsRef6146)
	if !handled {
		t.Fatalf("lookupStructural did not claim %q (handled=false)", propsRef6146)
	}
	if id == props.ID || id == twin.ID {
		t.Fatalf("lookupStructural(%q) picked %q out of an ambiguous (file, name) "+
			"pair — it must decline, not choose", propsRef6146, id)
	}
	if id != "" {
		t.Fatalf("lookupStructural(%q) = %q, want \"\"", propsRef6146, id)
	}
}

// TestSchemaShortFormColonGuardKeepsFormatBIntact6146 pins the OUTCOME the
// `!strings.Contains(filePath, ":")` guard exists to protect: the six-segment
// Format B `scope:schema:column:sql:<file>:<table>#<column>` refs minted by
// internal/extractor/structural_ref.go must still reach the Format B path and
// land on the column entity, not be swallowed by this three-segment branch.
//
// Measured, and worth recording because it is not what the guard's placement
// suggests: deleting `!strings.Contains(filePath, ":")` on its own does NOT
// break anything. The tier would then claim the ref with filePath =
// "column:sql:migrations/001_init.sql:Pet", miss byLocation, and — because a
// miss falls out of the block instead of returning — continue to the
// six-segment SplitN, which resolves it correctly anyway. That single-token
// mutation survives this test and the whole resolve suite. The guard is
// defence-in-depth and an early exit, not the load-bearing mechanism.
//
// What IS load-bearing is the fall-through-on-miss, and this test does catch
// its removal: guard deleted AND the branch made to `return "",
// statusUnmatched, true` on a byLocation miss fails here (and in the #6122
// neo4j test). Recorded so a future reader does not mistake a surviving
// mutation for an inert test.
func TestSchemaShortFormColonGuardKeepsFormatBIntact6146(t *testing.T) {
	const sqlFile = "migrations/001_init.sql"
	column := types.EntityRecord{
		ID: "5e5e5e5e5e5e5e5e", Kind: "SCOPE.Schema", Subtype: "column",
		Name: "Pet.name", SourceFile: sqlFile, Language: "sql",
	}
	all := []types.EntityRecord{column}
	idx := BuildIndex(all)

	// Exactly the shape internal/extractor.SQLColumnRef mints.
	const ref = "scope:schema:column:sql:" + sqlFile + ":Pet#name"
	id, status, handled := idx.lookupStructural(ref)
	if !handled || status != statusRewritten {
		t.Fatalf("lookupStructural(%q) = (status=%d, handled=%v); the six-segment "+
			"Format B path must still claim and resolve it", ref, status, handled)
	}
	assertBinding6146(t, bindingOf6146(t, all, id),
		"SCOPE.Schema", "Pet.name", sqlFile,
		"SQL column Format B ref")
}
