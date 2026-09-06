// Package zig_test — file_carrier_6852_test.go
//
// Issue #6852, arm 12 (zig) — the LAST arm of the series #6847 measured.
//
// THE DEFECT. buildImportEntities stamps `FromID: filePath` on the IMPORTS edge
// of every `@import("...")` stub. internal/resolve/refs.go has no path→entity
// index, so a path-valued FromID resolves if and only if some emitted record
// carries that exact string as its Name (zig sets no QualifiedName on any
// record, so the byQualifiedName tier — a DIFFERENT consumer, and one that has
// nothing to do with FileCarrierFor's clause 3 — cannot apply here at all).
// Nothing zig emits is named after the file as a rule, so before this arm the
// raw path reached the graph as the edge's FROM end.
//
// THE FIX is extractor.PrependFileCarrier, the CONDITIONAL carrier #6518 and
// #6815 settled on. Unconditional FileEntity would mint one bare orphan node
// per .zig file across a whole repo, which no recall-shaped assertion can see.
//
// ZIG HAS EXACTLY ONE FromID SITE AND EXACTLY FOUR Name SITES, and neither
// count is left as prose: TestZig_SourceSiteCountsAreWhatThisFileAssumes_6852
// reads them out of zig.go on every run, so a fifth Name site or a second
// path-anchored edge fails a test rather than ageing a comment (#6861's shape).
//
// THE CLOSURE THIS FILE RESTS ON, and why it survives concatenation. Three of
// the four Name sites assign a `(\w+)` capture VERBATIM — `\w` is
// [0-9A-Za-z_], so those Names hold neither '.' nor '/'. The fourth is
// importTopSegment, and it is not argued about at all: import_top_segment_-
// 6852_test.go BRUTE-FORCES all 5461 targets over {a _ . /} up to length 6 and
// establishes two properties — the result is always a SUBSTRING of the input
// (which is the anti-concatenation property itself, stated over the function
// rather than over a paragraph), and a '/'-bearing result always ENDS in '/'.
// So an import stub can be named after a ROOT path, which is DRIVEN below, and
// after a path ending in '/', which is also DRIVEN below rather than argued
// unreachable — four arms of #6852 shipped an unreachability claim a driven
// cell later disproved.
package zig_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/zig"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// runZig6852 drives the REGISTERED production extractor over src at path.
func runZig6852(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("zig")
	if !ok {
		t.Fatal("zig extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "zig",
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return recs
}

// zigCarriers6852 returns every record that IS the file carrier for path.
// Subtype "file" is what separates it from zig's other SCOPE.Component records:
// the struct records carry "struct" and the import stubs carry "import".
func zigCarriers6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.Name == path {
			out = append(out, r)
		}
	}
	return out
}

// zigNamedExactly6852 returns every record whose Name is path. Name only, and
// deliberately so: zig sets QualifiedName nowhere (pinned by
// TestZig_SourceSiteCountsAreWhatThisFileAssumes_6852), so adding the
// QualifiedName disjunct here would be a check that can never fire for this
// package and would read as coverage it does not have.
func zigNamedExactly6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Name == path {
			out = append(out, r)
		}
	}
	return out
}

// zigPathAnchored6852 returns every relationship in recs whose FromID is
// exactly path — the shape whose FROM end has nothing to resolve onto.
func zigPathAnchored6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
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

// resolveZig6852 extracts src at path, stamps ids the way graph assembly does,
// runs the production resolver pipeline, and returns the records plus the
// id→record index. The assertion that follows is on the EMITTED ARTEFACT after
// resolution — the edge's FROM end — not on a counter the code keeps about
// itself.
func resolveZig6852(t *testing.T, src, path string) ([]types.EntityRecord, map[string]*types.EntityRecord) {
	t.Helper()
	recs := runZig6852(t, src, path)
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

// assertZigImportsResolve6852 fails for every IMPORTS edge whose FROM end names
// no record, and fails OUTRIGHT when the fixture produced no IMPORTS at all — a
// resolution assertion over an empty set is a no-op that reads like a guard.
func assertZigImportsResolve6852(t *testing.T, recs []types.EntityRecord, byID map[string]*types.EntityRecord) {
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

// carrierSrcZig6852 exercises EVERY pass of extractZig: the pub-fn pass, the
// private-fn pass (with a call, so CALLS edges exist), the struct pass (with a
// method, so a CONTAINS edge exists), and the import pass with two distinct
// targets — a stdlib token and a relative path, the two shapes importTopSegment
// treats differently. A carrier wired into any one of those passes would still
// be seen by the tests below.
const carrierSrcZig6852 = `const std = @import("std");
const util = @import("./util/helper.zig");

pub fn main() void {
    helper();
}

fn helper() void {
    std.debug.print("x", .{});
}

const Handler = struct {
    pub fn handle(self: *Handler) void {}
};
`

// TestZig_ImportsFromEndResolves_6852 is the fix's behavioural test. It drives
// the cells in which the edge DANGLES — root and nested depth, both of which
// reach clause 2 and neither of which reaches clause 3. The cells where the
// source itself spells the path are driven by
// TestZig_ImportStubNamedLikeThePath_6852 below.
func TestZig_ImportsFromEndResolves_6852(t *testing.T) {
	for _, path := range []string{"main.zig", "src/app/main.zig"} {
		t.Run(path, func(t *testing.T) {
			recs, byID := resolveZig6852(t, carrierSrcZig6852, path)

			// The PRE-resolution records are what carry the shape this arm
			// exists for: after ReferencesEmbedded rewrites the FROM end, no
			// FromID is the raw path any more, so the premise has to be taken
			// from a second, unresolved extraction of the same input.
			pre := runZig6852(t, carrierSrcZig6852, path)
			if got := len(zigPathAnchored6852(pre, path)); got != 2 {
				t.Fatalf("fixture emits %d path-anchored relationships at %s, want 2 "+
					"(one per @import target) — the cell under test is not being reached",
					got, path)
			}

			carriers := zigCarriers6852(recs, path)
			if len(carriers) != 1 {
				t.Fatalf("want exactly 1 file carrier named %q, got %d", path, len(carriers))
			}
			if recs[0].Name != path || recs[0].Subtype != "file" {
				t.Errorf("carrier is not at index 0: recs[0] = {Name:%q Subtype:%q}. "+
					"#577's convention is that the file entity is the first record, and "+
					"python/re_exports.go and python/prune_import_placeholders.go rely on it.",
					recs[0].Name, recs[0].Subtype)
			}
			assertZigImportsResolve6852(t, recs, byID)
		})
	}
}

// TestZig_NoCarrierWithoutAnImport_6852 is the OVER-FIRING control for
// FileCarrierFor's clause 2, and it is the assertion an UNCONDITIONAL carrier
// fails. Recall-shaped tests only ever ask whether the carrier EXISTS, so
// nothing else in this package can see the difference between conditional and
// unconditional — one bare orphan node per .zig file is invisible to every
// other check here (#6518, #6815).
//
// The fixture varies exactly ONE axis against carrierSrcZig6852: it has no
// `@import` directive. Every other pass — pub fn, private fn with a call,
// struct with a method — is held constant, so a failure here cannot be read as
// "the file was too empty to extract anything".
func TestZig_NoCarrierWithoutAnImport_6852(t *testing.T) {
	const src = `pub fn main() void {
    helper();
}

fn helper() void {
    other();
}

const Handler = struct {
    pub fn handle(self: *Handler) void {}
};
`
	const path = "src/app/main.zig"
	recs := runZig6852(t, src, path)

	if len(recs) == 0 {
		t.Fatal("no records — the fixture extracts nothing, so the absence of a carrier " +
			"below would prove nothing about clause 2")
	}
	if got := len(zigPathAnchored6852(recs, path)); got != 0 {
		t.Fatalf("fixture unexpectedly emits %d path-anchored relationships — the premise "+
			"of this over-firing control is that NOTHING anchors on the path", got)
	}
	if got := zigCarriers6852(recs, path); len(got) != 0 {
		t.Errorf("a file with no path-anchored edge got %d carrier(s) — the carrier is "+
			"CONDITIONAL, and an unconditional one mints a bare orphan node per source "+
			"file across an entire repo (#6518, #6815)", len(got))
	}
	if got := len(zigNamedExactly6852(recs, path)); got != 0 {
		t.Errorf("%d record(s) named after the path in a file that anchors nothing", got)
	}
}

// TestZig_EmptyFileGetsNoCarrier_6852 drives Extract's FIRST return path — the
// `len(file.Content) == 0` guard, which returns before extractZig is called at
// all, so the carrier call placed inside extractZig is never reached.
func TestZig_EmptyFileGetsNoCarrier_6852(t *testing.T) {
	const path = "src/empty.zig"
	if got := runZig6852(t, "", path); len(got) != 0 {
		t.Errorf("an empty .zig file produced %d record(s), want 0", len(got))
	}
}

// TestZig_EmptyPathGetsNoCarrier_6852 drives FileCarrierFor CLAUSE 1, which
// rejects on its own and rejects a record set that DOES anchor: an empty FromID
// trivially equals an empty path, so clause 2 is satisfied and only clause 1
// stands between that input and a blank carrier node.
//
// The fixture is carrierSrcZig6852 unchanged — the path is the only axis that
// varies against TestZig_ImportsFromEndResolves_6852.
func TestZig_EmptyPathGetsNoCarrier_6852(t *testing.T) {
	recs := runZig6852(t, carrierSrcZig6852, "")
	if len(recs) == 0 {
		t.Fatal("no records at an empty path — the absence of a carrier would prove nothing")
	}
	anchored := zigPathAnchored6852(recs, "")
	if len(anchored) == 0 {
		t.Fatal("no relationship has an empty FromID at an empty path — clause 2 is NOT " +
			"satisfied here, so this fixture does not isolate clause 1")
	}
	for _, r := range recs {
		if r.Subtype == "file" {
			t.Errorf("an empty path produced a file carrier %+v — a nameless carrier "+
				"resolves nothing and adds a blank node", r)
		}
	}
}

// TestZig_OneCarrierPerFileNotPerImport_6852 is the multiplicity control. The
// anchor lives on EVERY import stub, so a carrier minted inside
// buildImportEntities' loop — or a FileCarrierFor that kept scanning after the
// first anchor — would produce one node per import, all under a single
// graph.EntityID.
//
// Varies exactly one axis against carrierSrcZig6852: the number of DISTINCT
// import targets (five, not two). The passes, the path and the depth are held
// constant.
func TestZig_OneCarrierPerFileNotPerImport_6852(t *testing.T) {
	const src = `const std = @import("std");
const a = @import("./a.zig");
const b = @import("./b.zig");
const c = @import("../lib/c.zig");
const d = @import("builtin");

pub fn main() void {
    helper();
}
`
	const path = "src/app/main.zig"
	recs := runZig6852(t, src, path)

	if got := len(zigPathAnchored6852(recs, path)); got != 5 {
		t.Fatalf("fixture emits %d path-anchored relationships, want 5 — the multiplicity "+
			"this test exists to measure is not present", got)
	}
	if got := zigCarriers6852(recs, path); len(got) != 1 {
		t.Errorf("want exactly 1 carrier for 5 anchored imports, got %d — every one of them "+
			"would land under the same graph.EntityID", len(got))
	}
	if got := len(zigNamedExactly6852(recs, path)); got != 1 {
		t.Errorf("%d records named after the path, want 1", got)
	}
}

// TestZig_ImportStubNamedLikeThePath_6852 drives FileCarrierFor CLAUSE 3 — the
// clause that declines a carrier when some record is ALREADY named the path.
//
// THIS IS A DRIVEN CELL, NOT A CLOSURE. graph.EntityID does not hash Subtype,
// so a carrier minted beside a record of the same Kind and Name lands a SECOND
// node under one id (the #6369/#6480 hazard) and makes the rewrite target
// ambiguous. zig reaches that state through importTopSegment: it trims exactly
// ONE trailing extension, so in a ROOT file main.zig the target
// `@import("main.zig.zig")` names the import stub exactly `main.zig`.
//
// The nested subtest is the CONTRAST that stops the root subtest passing on a
// carrier that is never emitted at all: same source, same import target, one
// axis changed (the path's depth), and there the carrier IS due.
//
// The third subtest is the case four earlier arms would have written a closure
// about. Nothing in zig's Name sites forbids a nested clause-3 hit outright;
// what the enumeration in import_top_segment_6852_test.go establishes is that
// such a Name must END in '/'. So the cell is DRIVEN with a path that ends in
// '/' — clause 3 fires there too, at depth. It is not production-reachable (no
// file on disk has a path ending in '/'), and that is stated as a property of
// the INPUT rather than as a property of the code.
func TestZig_ImportStubNamedLikeThePath_6852(t *testing.T) {
	cases := []struct {
		name string
		// varies names the ONE axis that changes against the root case.
		varies      string
		path        string
		src         string
		wantCarrier bool
	}{{
		name:        "root_path_spelled_by_a_double_extension_target",
		varies:      "nothing — this is the base case",
		path:        "main.zig",
		src:         "const self = @import(\"main.zig.zig\");\n\npub fn main() void {}\n",
		wantCarrier: false,
	}, {
		name:        "nested_path_the_same_target_cannot_spell",
		varies:      "the path's DEPTH; the source and the import target are byte-identical",
		path:        "src/main.zig",
		src:         "const self = @import(\"main.zig.zig\");\n\npub fn main() void {}\n",
		wantCarrier: true,
	}, {
		name:        "path_ending_in_a_slash",
		varies:      "the path ends in '/', the one nested shape importTopSegment can return",
		path:        "src/",
		src:         "const self = @import(\"src/\");\n\npub fn main() void {}\n",
		wantCarrier: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs := runZig6852(t, tc.src, tc.path)

			if got := len(zigPathAnchored6852(recs, tc.path)); got != 1 {
				t.Fatalf("fixture emits %d path-anchored relationships, want 1 — clause 2 "+
					"is not satisfied, so clause 3 is never consulted and this cell measures "+
					"nothing", got)
			}
			named := zigNamedExactly6852(recs, tc.path)
			if len(named) != 1 {
				t.Fatalf("want exactly 1 record named %q, got %d — two nodes under one "+
					"graph.EntityID is the #6369/#6480 hazard clause 3 exists to prevent",
					tc.path, len(named))
			}
			carriers := zigCarriers6852(recs, tc.path)
			if tc.wantCarrier {
				if len(carriers) != 1 {
					t.Fatalf("want a carrier at %q, got %d — the contrast case is what stops "+
						"the clause-3 cases passing on a carrier that is never emitted at all",
						tc.path, len(carriers))
				}
				return
			}
			if len(carriers) != 0 {
				t.Fatalf("clause 3 did not decline at %q: got %d carrier(s) beside the "+
					"import stub already named after the path", tc.path, len(carriers))
			}
			if named[0].Subtype != "import" {
				t.Fatalf("the one record named %q is %+v — clause 3 should have declined in "+
					"favour of the IMPORT STUB importTopSegment already named after the path. "+
					"If this record is the carrier, the premise of this cell has broken: the "+
					"offender importTopSegment can spell no longer exists.", tc.path, named[0])
			}
		})
	}
}

// TestZig_WordCaptureNameSitesCannotSpellAPath_6852 OBSERVES the half of the
// closure the enumeration does not cover: the three Name sites that are
// verbatim `(\w+)` regex captures (pub fn, private fn, struct).
//
// Observed by driving rather than argued from the regex source, because an
// argument from a pattern is exactly what clojure's character-set closure was
// when `.clj-kondo/hooks/foo.clj` disproved it (#6897). The fixture is
// adversarial on purpose: every declaration is spelled with the path's own
// bytes around it, so if any of these three sites carried its surrounding text
// into Name it would show up here.
func TestZig_WordCaptureNameSitesCannotSpellAPath_6852(t *testing.T) {
	const path = "src/app/main.zig"
	const src = `const std = @import("std");

pub fn src_app_main_zig() void {}

fn main_zig() void {}

const src = struct {
    pub fn app(self: *src) void {}
};

const main_zig_t = struct {};
`
	recs := runZig6852(t, src, path)

	checked := 0
	for _, r := range recs {
		if r.Subtype != "function" && r.Subtype != "struct" {
			continue
		}
		checked++
		if strings.ContainsAny(r.Name, "./") {
			t.Errorf("a %s record is named %q — a `(\\w+)` capture has stopped being one, "+
				"and the closure this arm rests on (three of four Name sites hold neither "+
				"'.' nor '/') no longer holds", r.Subtype, r.Name)
		}
	}
	if checked < 5 {
		t.Fatalf("only %d function/struct records were checked, want at least 5 — the "+
			"fixture stopped reaching the sites this test grades", checked)
	}
	if got := len(zigNamedExactly6852(recs, path)); got != 1 {
		t.Fatalf("want exactly 1 record named %q (the carrier), got %d", path, got)
	}
	if got := zigCarriers6852(recs, path); len(got) != 1 {
		t.Fatalf("the one record named after the path is not the carrier: %d carrier(s)", len(got))
	}
}

// TestZig_CarrierShape_6852 pins what the carrier IS, and grades the "zig"
// token passed to PrependFileCarrier.
//
// HOW THE TOKEN IS GRADED HERE, stated because the mechanism is not the same as
// for every caller. extractZig sets Language: "zig" EXPLICITLY on all four of
// its record shapes, so Extract's TagEntitiesLanguage is a no-op for every zig
// record — it only fills a record whose Language is EMPTY, and stamps
// Properties["language"] when it does. The carrier is therefore the ONE record
// in a zig extraction that TagEntitiesLanguage could ever touch, and the
// carrier call sits INSIDE extractZig, i.e. before that tagging runs. So an
// empty token would be filled to "zig" on the Language field and would be
// indistinguishable there — but it would arrive carrying
// Properties["language"], which no other zig record has. Both halves are
// asserted below: the token on Language, and the ABSENCE of
// Properties["language"] that is the premise for reading Language as evidence
// at all.
func TestZig_CarrierShape_6852(t *testing.T) {
	const path = "src/app/main.zig"
	recs := runZig6852(t, carrierSrcZig6852, path)

	carriers := zigCarriers6852(recs, path)
	if len(carriers) != 1 {
		t.Fatalf("want exactly 1 carrier, got %d", len(carriers))
	}
	c := carriers[0]
	if c.Kind != "SCOPE.Component" || c.Subtype != "file" {
		t.Errorf("carrier Kind/Subtype = %q/%q, want SCOPE.Component/file", c.Kind, c.Subtype)
	}
	if c.Name != path || c.SourceFile != path {
		t.Errorf("carrier Name/SourceFile = %q/%q, want both %q", c.Name, c.SourceFile, path)
	}
	if c.Language != "zig" {
		t.Errorf("carrier Language = %q, want %q — the token is stamped explicitly so the "+
			"carrier cannot become the one record in an extraction that disagrees with the "+
			"classifier token every other record carries (proto's #6356 trap)", c.Language, "zig")
	}
	if _, ok := c.Properties["language"]; ok {
		t.Errorf("carrier carries Properties[\"language\"] — that is TagEntitiesLanguage's " +
			"provenance stamp, and its presence means the carrier reached Extract's tagging " +
			"call with an EMPTY Language. The Language assertion above is then evidence of " +
			"the tagger's fill, not of the token this extractor passed.")
	}
	if len(c.Relationships) != 0 {
		t.Errorf("carrier owns %d relationship(s), want 0 — zig's path-anchored records "+
			"carry the IMPORTS edges themselves, so re-homing them onto the carrier would "+
			"DOUBLE every import edge", len(c.Relationships))
	}
	// The carrier must not displace what the other passes emit.
	for _, want := range []string{"main", "helper", "Handler", "std", "helper"} {
		found := false
		for _, r := range recs {
			if r.Name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("record %q disappeared once the carrier was added", want)
		}
	}
}

// TestZig_SourceSiteCountsAreWhatThisFileAssumes_6852 reads zig.go and pins the
// two counts every closure in this file depends on: ONE `FromID:` site and FOUR
// `Name:` sites, plus ZERO `QualifiedName` sites.
//
// This exists because #6861 recorded the same lesson three times over: a count
// stated in prose is a measurement that ages silently. A fifth Name site, or a
// second path-anchored relationship, changes what this arm has graded — and
// fails here rather than leaving a paragraph quietly wrong.
func TestZig_SourceSiteCountsAreWhatThisFileAssumes_6852(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	const rel = "internal/extractors/zig/zig.go"
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	src := string(body)
	if len(src) < 8000 {
		t.Fatalf("read only %d bytes of %s — the file is far larger, so the counts below "+
			"would be measured over a PREFIX", len(src), rel)
	}
	for _, tc := range []struct {
		tok  string
		want int
		why  string
	}{
		{"FromID:", 1, "buildImportEntities' IMPORTS edge is the only path-anchored " +
			"relationship this arm fixed; a second site is a second offender"},
		{"Name:", 4, "pub fn / private fn / struct / import stub — three `(\\w+)` captures " +
			"and importTopSegment. A fifth site is a Name shape no closure here covers"},
		{"QualifiedName:", 0, "zig sets no QualifiedName, which is why the resolution " +
			"question for this package is a Name question only"},
	} {
		if got := strings.Count(src, tc.tok); got != tc.want {
			t.Errorf("%s contains %d occurrences of %q, want %d — %s", rel, got, tc.tok, tc.want, tc.why)
		}
	}
	if !strings.Contains(src, "extractor.PrependFileCarrier(") {
		t.Errorf("%s no longer calls extractor.PrependFileCarrier — the fix this whole file "+
			"grades has been removed", rel)
	}
}
