package erlang_test

// #6815 — erlang emitted path-anchored IMPORTS edges (FromID = the .erl path)
// with no extractor.FileEntity carrying that path, so the FROM end of every
// include/-import edge resolved to nothing.
//
// THE CONTRACT PINNED HERE, in both directions:
//
//   - RECALL: a file that emits at least one file-anchored IMPORTS edge also
//     emits exactly one extractor.FileEntity (SCOPE.Component, subtype "file")
//     whose Name is the file path, so every such edge's FromID names a real
//     emitted entity.
//   - OVER-FIRING: a file that emits NO file-anchored IMPORTS edge emits NO
//     carrier. Without this direction the fix would mint a bare orphan node for
//     every .erl in a repo, which recall-shaped assertions cannot see. This is
//     the proto precedent (#6518): the carrier exists when there is something
//     for it to carry, and not otherwise.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/erlang"
	"github.com/cajasmota/grafel/internal/types"
)

const erlPath6815 = "cache_server.erl"

func extract6815(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("erlang")
	if !ok {
		t.Fatal("erlang extractor not registered")
	}
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     erlPath6815,
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return out
}

// carriers6815 returns every file-carrier record (the shape extractor.FileEntity
// mints) whose Name is path.
func carriers6815(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.Name == path {
			out = append(out, r)
		}
	}
	return out
}

// fileAnchoredImports6815 returns every IMPORTS edge in recs whose FromID is
// path — i.e. every edge that needs the carrier to exist.
func fileAnchoredImports6815(recs []types.EntityRecord, path string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "IMPORTS" && rel.FromID == path {
				out = append(out, rel)
			}
		}
	}
	return out
}

// Axis VARIED: the include attribute (present / absent is the neighbouring
// case below). HELD CONSTANT: no -import attribute, one module, one function.
func TestErlang_IncludeGetsAFileCarrier_6815(t *testing.T) {
	src := `-module(cache_server).
-include("cache.hrl").
-export([get/1]).

get(Key) -> Key.
`
	recs := extract6815(t, src)
	if n := len(fileAnchoredImports6815(recs, erlPath6815)); n != 1 {
		t.Fatalf("premise: want exactly 1 file-anchored IMPORTS edge, got %d", n)
	}
	if n := len(carriers6815(recs, erlPath6815)); n != 1 {
		t.Fatalf("an -include IMPORTS edge must have exactly 1 file carrier, got %d", n)
	}
}

// Axis VARIED: the import KIND — `-import(mod, [f/1])` instead of `-include`.
// HELD CONSTANT: no -include attribute. Without this case a fix that keyed the
// carrier on includeRE alone would pass the case above and still leave every
// -import edge dangling.
func TestErlang_FunctionImportGetsAFileCarrier_6815(t *testing.T) {
	src := `-module(cache_server).
-import(lists, [map/2]).
-export([go/1]).

go(L) -> map(fun(X) -> X end, L).
`
	recs := extract6815(t, src)
	if n := len(fileAnchoredImports6815(recs, erlPath6815)); n != 1 {
		t.Fatalf("premise: want exactly 1 file-anchored IMPORTS edge, got %d", n)
	}
	if n := len(carriers6815(recs, erlPath6815)); n != 1 {
		t.Fatalf("an -import IMPORTS edge must have exactly 1 file carrier, got %d", n)
	}
}

// OVER-FIRING control. Axis VARIED: imports absent. HELD CONSTANT: everything
// else about the first case — same module, same export, same function body.
// A module with no include and no -import must NOT mint a carrier.
func TestErlang_NoCarrierWithoutAnythingToCarry_6815(t *testing.T) {
	src := `-module(cache_server).
-export([get/1]).

get(Key) -> Key.
`
	recs := extract6815(t, src)
	if n := len(fileAnchoredImports6815(recs, erlPath6815)); n != 0 {
		t.Fatalf("premise: want 0 file-anchored IMPORTS edges, got %d", n)
	}
	if n := len(carriers6815(recs, erlPath6815)); n != 0 {
		t.Fatalf("a module with nothing to carry must emit no file carrier, got %d", n)
	}
	// And nothing else may stand in for it: no record at all may be named
	// after the path.
	for _, r := range recs {
		if r.Name == erlPath6815 {
			t.Fatalf("no record may be named %q here, got kind=%q subtype=%q",
				erlPath6815, r.Kind, r.Subtype)
		}
	}
}

// OVER-FIRING control on COUNT. Axis VARIED: the NUMBER of file-anchored
// IMPORTS edges (three, from two includes and one -import). HELD CONSTANT: one
// file. The carrier is per-FILE, not per-EDGE: a fix that appended one inside
// the include loop would emit three and pass every recall assertion above.
func TestErlang_OneCarrierPerFileNotPerImport_6815(t *testing.T) {
	src := `-module(cache_server).
-include("cache.hrl").
-include_lib("kernel/include/file.hrl").
-import(lists, [map/2]).
-export([go/1]).

go(L) -> map(fun(X) -> X end, L).
`
	recs := extract6815(t, src)
	if n := len(fileAnchoredImports6815(recs, erlPath6815)); n != 3 {
		t.Fatalf("premise: want 3 file-anchored IMPORTS edges, got %d", n)
	}
	if n := len(carriers6815(recs, erlPath6815)); n != 1 {
		t.Fatalf("3 import edges must still yield exactly 1 file carrier, got %d", n)
	}
}

// The carrier must not become an anchor for anything else: it carries no
// relationships of its own. An over-eager fix that hung the IMPORTS edges off
// the carrier record would DOUBLE every edge, since the per-import stubs still
// carry them.
func TestErlang_CarrierCarriesNoRelationshipsOfItsOwn_6815(t *testing.T) {
	src := `-module(cache_server).
-include("cache.hrl").
-export([get/1]).

get(Key) -> Key.
`
	recs := extract6815(t, src)
	cs := carriers6815(recs, erlPath6815)
	if len(cs) != 1 {
		t.Fatalf("premise: want 1 carrier, got %d", len(cs))
	}
	if n := len(cs[0].Relationships); n != 0 {
		t.Fatalf("the erlang file carrier must own no relationships, got %d", n)
	}
	if n := len(fileAnchoredImports6815(recs, erlPath6815)); n != 1 {
		t.Fatalf("the include edge must still be emitted exactly once, got %d", n)
	}
}

// The carrier must be stamped with the language every other erlang record
// carries; a record that disagrees is the one row that would be filtered out of
// a per-language view.
func TestErlang_CarrierIsLanguageTagged_6815(t *testing.T) {
	src := `-module(cache_server).
-include("cache.hrl").
`
	recs := extract6815(t, src)
	cs := carriers6815(recs, erlPath6815)
	if len(cs) != 1 {
		t.Fatalf("premise: want 1 carrier, got %d", len(cs))
	}
	if cs[0].Language != "erlang" {
		t.Fatalf("carrier Language = %q, want %q", cs[0].Language, "erlang")
	}
	if cs[0].SourceFile != erlPath6815 {
		t.Fatalf("carrier SourceFile = %q, want %q", cs[0].SourceFile, erlPath6815)
	}
}
