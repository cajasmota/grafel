package lua_test

// file_carrier_6852_test.go — #6852, lua arm.
//
// makeImportRecord (lua.go) stamps `FromID: file.Path` on the IMPORTS edge of
// every `require(...)` record. internal/resolve/refs.go has no path→entity
// index, so a path-valued FromID resolves if and only if some emitted node
// carries that exact string as its Name. Same defect #6815 fixed in
// erlang/nim/groovy and #6852 fixed in bicep (#6864), terraform (#6871), html
// (#6879), fsharp (#6880) and shell (#6882); same fix,
// extractor.PrependFileCarrier.
//
// ONE ANCHORING SITE, N EDGES — fsharp's and shell's multiplicity shape, not
// bicep's. makeImportRecord is the only bare `FromID: path` in the whole
// package: oop.go's EXTENDS edge uses BuildComponentStructuralRef, and the
// CONTAINS edges walkLua appends and the CALLS edges extractCallRelationships
// builds leave FromID EMPTY. But one import record is emitted per `require`, so
// a module with four of them anchors four edges and must still gain exactly ONE
// carrier.
//
// BOTH DEPTHS DANGLED — html's and fsharp's shape, not terraform's or shell's.
// lua names its records after the REQUIRED module ("lib.logging"), the module
// TABLE variable ("M") or the FUNCTION ("run"); nothing is named after the file
// or its basename under any condition, so there is no depth at which the edge
// resolved by the #6367 accident. The condition axis the earlier arms had —
// terraform's unconditional basename component, shell's functions-only one —
// simply does not exist here, which is why the table below is two cells rather
// than three or four.
//
// CLAUSE 3 IS STILL REACHABLE, AT BOTH DEPTHS. FileCarrierFor clause 3 is
// `records[i].Name == path` (clause 1 is the empty-path guard, clause 2 the
// anchoring test). makeImportRecord names the record after the REQUIRED path
// verbatim, so `require("src/app/main.lua")` in src/app/main.lua mints a record
// named exactly the path — shell's self-sourcing route, reached here through
// require rather than through `source`. graph.EntityID hashes (repo, kind,
// name, sourceFile) and NOT Subtype (#6369 / PR #6480), so a carrier there
// would land a second SCOPE.Component under the import record's id.
//
// GRADED IN BOTH DIRECTIONS. A recall-shaped assertion ("the carrier exists")
// licenses an UNCONDITIONAL carrier, which would mint one bare orphan node per
// .lua/.rockspec file across a whole repo — a change no recall-shaped assertion
// can see. The forbidden-row controls below are what forbid it, and each is
// matched to a distinct return path of Extract.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/lua"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// runLua6852 drives the registered production extractor over src at path with a
// real lua parse tree.
func runLua6852(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("lua")
	if !ok {
		t.Fatal("lua extractor not registered")
	}
	in := extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "lua",
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

// luaCarriers6852 returns every record that IS the file carrier for path — the
// SCOPE.Component/file record extractor.FileEntity mints. Subtype "file" is
// what separates it from the import records (Subtype "") and the module tables
// (Subtype "module_table"/"class").
func luaCarriers6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.Name == path {
			out = append(out, r)
		}
	}
	return out
}

// luaPathAnchored6852 returns every relationship in recs whose FromID is
// exactly path — the shape whose FROM end has nothing to resolve onto.
func luaPathAnchored6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
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

// luaNamedExactly6852 returns every record whose Name or QualifiedName is path.
// This is the resolution question internal/resolve/refs.go actually asks: it has
// no path→entity index, so a path-valued FromID resolves if and only if such a
// record exists.
func luaNamedExactly6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Name == path || r.QualifiedName == path {
			out = append(out, r)
		}
	}
	return out
}

// luaRelOwners6852 returns the Name of EVERY record owning at least one
// relationship of kind k, in slice order. It returns the full list rather than
// the last match on purpose: a last-wins scan reports one owner however many
// there are, so a re-homed edge that leaves the original owner in place would
// read as correct. That is precisely the shape a mis-placed carrier produces.
func luaRelOwners6852(recs []types.EntityRecord, k string) []string {
	var out []string
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == k {
				out = append(out, r.Name)
				break
			}
		}
	}
	return out
}

// resolveLua6852 extracts src at path, stamps ids the way graph assembly does,
// runs the production resolver pipeline, and returns the records plus the
// id→record index. This asserts on the EMITTED ARTEFACT after resolution — the
// edge's FROM end — not on a helper's return value or a counter.
func resolveLua6852(t *testing.T, src, path string) ([]types.EntityRecord, map[string]*types.EntityRecord) {
	t.Helper()
	recs := runLua6852(t, src, path)
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

// assertLuaImportsResolve6852 fails for every IMPORTS edge whose FROM end names
// no record, and fails outright when the fixture produced no IMPORTS at all — a
// resolution assertion over an empty set is a no-op that reads like a guard.
func assertLuaImportsResolve6852(t *testing.T, recs []types.EntityRecord, byID map[string]*types.EntityRecord) {
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

// carrierSrcLua6852 is the canonical lua module: two requires (one bound to a
// local, one bare — the two branches of makeImportRecord's local_name
// fallback), a module table, and a method on it. It exercises every pass of
// Extract, so a carrier wired into any one of them would still be seen.
const carrierSrcLua6852 = `local log = require("lib.logging")
require "app.config"

local M = {}

function M.run(x)
    log.info(x)
    return x
end

return M
`

// TestLua_RequireImportsFromEndResolves_6852 is the fix's behavioural test, and
// it drives EVERY cell of lua's depth/condition table. That table is two cells
// wide, not four: lua emits nothing named after the file or its basename under
// any condition, so unlike terraform (unconditional basename component) and
// shell (basename component only when a function is declared) there is no
// second axis and no cell that resolved by accident. Both cells FAIL before the
// carrier.
//
// Axis VARIED: path DEPTH (nested and root). HELD CONSTANT: the source.
func TestLua_RequireImportsFromEndResolves_6852(t *testing.T) {
	for _, path := range []string{"src/app/main.lua", "main.lua"} {
		t.Run(path, func(t *testing.T) {
			// Premise: nothing lua emits is named after the file, so the only
			// record that can be named the path is the carrier itself. This
			// pins the "both depths dangled" claim rather than assuming it.
			pre := runLua6852(t, carrierSrcLua6852, path)
			named := luaNamedExactly6852(pre, path)
			if len(named) != 1 {
				t.Fatalf("premise: exactly 1 record may be named %q (the carrier), got %d", path, len(named))
			}
			if named[0].Subtype != "file" {
				t.Fatalf("premise: the one record named %q must be the carrier, got subtype %q — "+
					"lua names records after the required module, the module table or the "+
					"function, never after the file", path, named[0].Subtype)
			}
			recs, byID := resolveLua6852(t, carrierSrcLua6852, path)
			assertLuaImportsResolve6852(t, recs, byID)
		})
	}
}

// TestLua_NoCarrierWithoutARequire_6852 is the OVER-FIRING control for
// FileCarrierFor CLAUSE 2 — the half of the grade a "the edge now resolves"
// test cannot supply. Axis VARIED: the `require` calls (absent). HELD CONSTANT:
// a full record set — a module table, two functions and a CALLS edge between
// them — so the file still extracts plenty and still runs every pass; only the
// path-anchored edge is gone.
func TestLua_NoCarrierWithoutARequire_6852(t *testing.T) {
	const src = `local M = {}

local function helper(x)
    return x + 1
end

function M.run(x)
    return helper(x)
end

return M
`
	for _, path := range []string{"src/app/main.lua", "main.lua"} {
		t.Run(path, func(t *testing.T) {
			recs := runLua6852(t, src, path)
			if len(recs) == 0 {
				t.Fatal("premise: fixture produced no records at all")
			}
			if n := len(luaPathAnchored6852(recs, path)); n != 0 {
				t.Fatalf("premise: want 0 path-anchored relationships, got %d", n)
			}
			if n := len(luaCarriers6852(recs, path)); n != 0 {
				t.Errorf("a lua file with nothing to carry must emit no file carrier, got %d — "+
					"an unconditional carrier mints one bare orphan node per .lua file across a "+
					"whole repo, which no recall-shaped assertion can see", n)
			}
			// Forbidden-row form: a carrier smuggled in under a different Kind
			// or Subtype is caught too. Both rows are meaningful at BOTH depths
			// here, unlike shell's, because lua emits no basename-named record
			// that could make "nothing is named the path" false for an unrelated
			// reason.
			for _, r := range recs {
				if r.Subtype == "file" {
					t.Errorf("no Subtype=%q record may exist here, got kind=%q name=%q",
						"file", r.Kind, r.Name)
				}
			}
			for _, r := range luaNamedExactly6852(recs, path) {
				t.Errorf("no record may be named %q here, got kind=%q subtype=%q name=%q qname=%q",
					path, r.Kind, r.Subtype, r.Name, r.QualifiedName)
			}
		})
	}
}

// TestLua_EmptyFileGetsNoCarrier_6852 drives Extract's FIRST return path:
// `len(file.Content) == 0` returns nil before anything else runs. A carrier
// placed at the head of Extract rather than after the walk would mint a node
// for a file with no content whatsoever.
func TestLua_EmptyFileGetsNoCarrier_6852(t *testing.T) {
	const path = "src/app/empty.lua"
	recs := runLua6852(t, "", path)
	if len(recs) != 0 {
		t.Fatalf("an empty lua file must extract no records at all, got %d (first: kind=%q name=%q)",
			len(recs), recs[0].Kind, recs[0].Name)
	}
}

// TestLua_NilTreeGetsNoCarrier_6852 drives Extract's OTHER early return, the
// second disjunct of the SAME guard: `file.TSTree == nil`. lua has no regex
// fallback, so this returns nil with a fully-populated Content.
//
// SCORED IN ITS OWN DIRECTION rather than left as an absence over a set that
// can never contain the thing. The mutant it kills is an UNCONDITIONAL carrier
// hoisted above the TSTree nil check: that mutant leaves every test above green
// (the carrier still exists where it is wanted) and fails only here. The
// fixture deliberately contains a `require` line, so a carrier keyed on the
// source TEXT rather than on the emitted relationships would be caught too.
func TestLua_NilTreeGetsNoCarrier_6852(t *testing.T) {
	const path = "src/app/main.lua"
	ext, ok := extractor.Get("lua")
	if !ok {
		t.Fatal("lua extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(carrierSrcLua6852),
		Language: "lua",
		// TSTree deliberately nil.
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("a lua file with no parse tree must extract no records at all, got %d "+
			"(first: kind=%q subtype=%q name=%q)", len(recs), recs[0].Kind, recs[0].Subtype, recs[0].Name)
	}
}

// TestLua_OneCarrierPerFileNotPerRequire_6852 is the multiplicity control. Axis
// VARIED: the NUMBER of `require` calls (four, each its own import record with
// its own path-anchored IMPORTS). HELD CONSTANT: one file, one path, driven at
// both depths. The carrier is per-FILE, not per-EDGE; a per-edge carrier would
// put four nodes under one id.
func TestLua_OneCarrierPerFileNotPerRequire_6852(t *testing.T) {
	const src = `local a = require("lib.a")
local b = require("lib.b")
require "lib.c"
local d = require('lib.d')

return { a, b, d }
`
	for _, path := range []string{"src/app/main.lua", "main.lua"} {
		t.Run(path, func(t *testing.T) {
			recs := runLua6852(t, src, path)
			if n := len(luaPathAnchored6852(recs, path)); n != 4 {
				t.Fatalf("premise: want 4 path-anchored IMPORTS edges, got %d", n)
			}
			if n := len(luaCarriers6852(recs, path)); n != 1 {
				t.Errorf("4 require calls must still yield exactly 1 file carrier, got %d", n)
			}
			if n := len(luaNamedExactly6852(recs, path)); n != 1 {
				t.Errorf("exactly 1 record may be named %q, got %d", path, n)
			}
		})
	}
}

// TestLua_SelfRequiringImportRecordGetsNoSecondCarrier_6852 drives FileCarrierFor
// CLAUSE 3. makeImportRecord names the record after the REQUIRED path verbatim,
// so a module that requires itself by the spelling of its own path mints a
// record named exactly the path — at BOTH depths, since nothing here is
// shortened to a basename. graph.EntityID hashes (repo, kind, name, sourceFile)
// and NOT Subtype (#6369 / PR #6480), so a carrier there would land a second
// SCOPE.Component under the import record's id.
//
// The require target is the only content, so the ONLY record that can be named
// the path is the import record itself — the assertion cannot pass on some
// other path-named record.
func TestLua_SelfRequiringImportRecordGetsNoSecondCarrier_6852(t *testing.T) {
	for _, path := range []string{"src/app/main.lua", "main.lua"} {
		t.Run(path, func(t *testing.T) {
			src := "local self = require(\"" + path + "\")\nreturn self\n"
			recs := runLua6852(t, src, path)
			if n := len(luaPathAnchored6852(recs, path)); n == 0 {
				t.Fatal("premise: the fixture must still anchor an IMPORTS on the path")
			}
			named := luaNamedExactly6852(recs, path)
			if len(named) != 1 {
				t.Fatalf("exactly 1 record may be named %q, got %d — a carrier here would land a "+
					"second SCOPE.Component under the import record's graph.EntityID, which does "+
					"not hash Subtype (#6369/#6480)", path, len(named))
			}
			if named[0].Subtype == "file" {
				t.Errorf("the one record named %q must be the import record, not a file carrier", path)
			}
			if n := len(luaCarriers6852(recs, path)); n != 0 {
				t.Errorf("no file carrier may be minted when an import record already carries the "+
					"path as its Name, got %d", n)
			}
		})
	}
}

// TestLua_CarrierShape_6852 pins what the carrier IS: stamped lua, anchored on
// the file it names, and owning no relationships of its own — the import
// records still carry the IMPORTS edges, so re-homing them onto the carrier
// would double every edge.
func TestLua_CarrierShape_6852(t *testing.T) {
	const path = "src/app/main.lua"
	recs := runLua6852(t, carrierSrcLua6852, path)
	cs := luaCarriers6852(recs, path)
	if len(cs) != 1 {
		t.Fatalf("premise: want 1 carrier, got %d", len(cs))
	}
	if cs[0].Language != "lua" {
		t.Errorf("carrier Language = %q, want %q", cs[0].Language, "lua")
	}
	if cs[0].SourceFile != path {
		t.Errorf("carrier SourceFile = %q, want %q", cs[0].SourceFile, path)
	}
	if n := len(cs[0].Relationships); n != 0 {
		t.Errorf("the lua file carrier must own no relationships, got %d", n)
	}
	// The lang ARGUMENT is what is graded here, not the field it ends up in.
	// lua's Extract runs extractor.TagEntitiesLanguage, which fills an EMPTY
	// Language with "lua" — so Language alone cannot tell `"lua"` from `""`.
	// What does tell them apart is Properties: TagEntitiesLanguage only touches
	// a record whose Language is empty, and when it does it also stamps
	// Properties["language"]. Every OTHER lua record sets Language explicitly
	// and therefore carries no such key, so a carrier that acquired one would be
	// the single record in the extraction whose provenance differs from its
	// siblings' — proto's #6356 trap, which is the reason file_carrier.go takes
	// the token as a parameter at all.
	if v, ok := cs[0].Properties["language"]; ok {
		t.Errorf("carrier carries Properties[%q]=%q — it was language-tagged after the "+
			"fact rather than stamped by the lang argument, so it disagrees with every other "+
			"lua record, none of which carries that key", "language", v)
	}
	// THE PREMISE THAT ASSERTION RESTS ON, pinned rather than assumed. The check
	// above distinguishes lang="" from lang="lua" only while NO OTHER record
	// carries the key either. If some future lua record shipped without an
	// explicit Language, TagEntitiesLanguage would fill it and stamp the key —
	// and the check above would go on passing while quietly grading nothing,
	// with the empty-token mutant back to ALIVE and no test going red. Driven
	// over a record set that contains an import record, a module table and a
	// function, so the loop is not an absence asserted over one record shape.
	for _, r := range recs {
		if r.Subtype == "file" {
			continue
		}
		if v, ok := r.Properties["language"]; ok {
			t.Errorf("record kind=%q subtype=%q name=%q carries Properties[%q]=%q — "+
				"every lua record is meant to set Language explicitly, and the carrier's "+
				"empty-token mutant is only observable while none of them is language-tagged "+
				"after the fact", r.Kind, r.Subtype, r.Name, "language", v)
		}
	}
	if n := len(luaPathAnchored6852(recs, path)); n != 2 {
		t.Errorf("the two require IMPORTS edges must still be emitted, got %d", n)
	}
	// #577 convention: the file entity is the FIRST record.
	if recs[0].Name != path {
		t.Errorf("carrier must be record 0 (#577 convention), got %q at index 0", recs[0].Name)
	}
	// The carrier must not disturb the CONTAINS wiring walkLua builds through
	// moduleTableIdx, which indexes INTO the entity slice. Prepending before the
	// walk shifts every index by one and re-homes the CONTAINS edge onto the
	// wrong record; this asserts the edge is still owned by the module table.
	if owners := luaRelOwners6852(recs, "CONTAINS"); len(owners) != 1 || owners[0] != "M" {
		t.Errorf("the CONTAINS edge must be owned by the module table \"M\" and by nothing else, "+
			"got owners %q — walkLua indexes into the entity slice via moduleTableIdx, so a "+
			"carrier prepended before the walk re-homes it", owners)
	}
}

// TestLua_CarrierPlacementDoesNotShiftTheOOPPass_6852 grades the SECOND
// conjunct of the placement rule. moduleTableIdx has TWO consumers — walkLua
// (CONTAINS) and applyOOP (the class promotion and its EXTENDS edge) — and a
// carrier prepended BETWEEN them shifts the slice under applyOOP alone. The
// CONTAINS row in TestLua_CarrierShape_6852 grades the first consumer only, so
// on its own it leaves half the claim ungraded: scoring a compound predicate
// part by part is what surfaced this.
//
// The distinguishing input is the ordinary Lua OOP shape — a class module that
// REQUIRES its parent. Every pre-existing OOP fixture in this package happens to
// have no `require`, so no import record exists to shift, which is exactly what
// masks the defect. Under the between-passes placement the promotion lands on
// the IMPORT record: `lib.base` becomes subtype "class" and acquires the
// EXTENDS edge, while `Derived` falls back to "module_table" — a silent wrong
// answer reaching the emitted graph with nothing red.
//
// Both depths, because the shift is a slice-index property and owes nothing to
// the path; if it ever became depth-sensitive that would itself be news.
func TestLua_CarrierPlacementDoesNotShiftTheOOPPass_6852(t *testing.T) {
	const src = `local Base = require("lib.base")

local Derived = {}
Derived.__index = Derived
setmetatable(Derived, { __index = Base })

function Derived.run(x)
    return x
end

return Derived
`
	for _, path := range []string{"src/app/derived.lua", "derived.lua"} {
		t.Run(path, func(t *testing.T) {
			recs := runLua6852(t, src, path)
			// Premise: the carrier really is emitted here, so the assertions
			// below are made about a record set the carrier has shifted rather
			// than about one it never touched.
			if n := len(luaCarriers6852(recs, path)); n != 1 {
				t.Fatalf("premise: want exactly 1 carrier, got %d", n)
			}
			byName := make(map[string]types.EntityRecord, len(recs))
			for _, r := range recs {
				byName[r.Name] = r
			}
			derived, ok := byName["Derived"]
			if !ok {
				t.Fatal("premise: no record named \"Derived\"")
			}
			imp, ok := byName["lib.base"]
			if !ok {
				t.Fatal("premise: no import record named \"lib.base\"")
			}
			// applyOOP must have promoted the MODULE TABLE, not the record one
			// slot along.
			if derived.Subtype != "class" {
				t.Errorf("the module table \"Derived\" must be promoted to subtype \"class\", got %q — "+
					"applyOOP indexes the entity slice via moduleTableIdx, so a carrier prepended "+
					"between walkLua and applyOOP promotes the wrong record", derived.Subtype)
			}
			if imp.Subtype != "" {
				t.Errorf("the import record \"lib.base\" must keep subtype \"\", got %q — the OOP "+
					"promotion has been re-homed onto it by a shifted moduleTableIdx", imp.Subtype)
			}
			// The EXTENDS edge must hang off the class, and the import record
			// must own nothing but its IMPORTS.
			if owners := luaRelOwners6852(recs, "EXTENDS"); len(owners) != 1 || owners[0] != "Derived" {
				t.Errorf("the EXTENDS edge must be owned by \"Derived\" and by nothing else, got owners %q",
					owners)
			}
			if owners := luaRelOwners6852(recs, "CONTAINS"); len(owners) != 1 || owners[0] != "Derived" {
				t.Errorf("the CONTAINS edge must be owned by \"Derived\" and by nothing else, got owners %q",
					owners)
			}
			for _, rel := range imp.Relationships {
				if rel.Kind != "IMPORTS" {
					t.Errorf("the import record \"lib.base\" must own only IMPORTS, got a %q edge to %q",
						rel.Kind, rel.ToID)
				}
			}
		})
	}
}
