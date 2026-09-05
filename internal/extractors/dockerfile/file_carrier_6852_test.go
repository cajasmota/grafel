package dockerfile_test

// file_carrier_6852_test.go — #6852, dockerfile arm. Lands with #6854, which is
// what makes the CORPUS-DRIVEN guard in
// internal/extractors/file_anchored_carrier_runtime_6847_test.go able to see
// this defect at all: until the classifier learned the `*.Dockerfile` /
// `Dockerfile.<variant>` spellings, no docker file in this repo crossed the
// classifier, so that guard listed dockerfile among the languages its corpus
// never reaches.
//
// THE ORDERING CONSTRAINT IS ONE-DIRECTIONAL, AND THE OTHER DIRECTION WAS
// MEASURED RATHER THAN ASSUMED. An earlier draft of this header claimed the two
// halves were mutually inseparable. They are not, and one command disproves it:
// with the carrier line, this file and the carrier_caller_set_6861 row applied
// but classifier.go and the 6847 guard left at the parent commit,
// ./internal/extractors/, ./internal/extractors/dockerfile/ and
// ./internal/extractor/ are all GREEN and every mutant on the carrier still
// dies — because every test in THIS file drives Extract with a synthetic path
// and never needs a real docker file to be routed. What is true is the other
// direction: the CLASSIFIER half cannot land first, because it turns all three
// fixtures into new offenders and reddens that guard. They ship together
// because splitting them buys nothing, not because splitting them is
// impossible. (A false claim of impossibility is the same defect class this
// repo keeps finding: prose asserting something no test observes.)
//
// THE DEFECT. collectFrom (dockerfile.go) stamps `FromID: file.Path` on the
// IMPORTS edge of every FROM instruction. internal/resolve/refs.go has no
// path→entity index, so a path-valued FromID resolves if and only if some
// emitted record carries that exact string as its Name.
//
// ONE ANCHORING SITE, N EDGES, ONE CARRIER. collectFrom is the ONLY producer of
// a path-anchored FromID left in this package: #6367 already moved the
// USES and CONTAINS edges off the path — they leave FromID EMPTY so graph
// assembly stamps the owning record's own id — and left the FROM IMPORTS anchor
// in place under the #120 convention. But collectFrom runs once per FROM, so a
// multi-stage Dockerfile anchors N edges on one path and must still gain
// exactly ONE carrier.
//
// ROOT RESOLVED BY ACCIDENT; ONLY NESTED DANGLED — the terraform/hcl shape, not
// the fsharp/html one. buildDockerfileEntity names its single SCOPE.Component
// BASENAME(file.Path). At a ROOT path the basename IS the path, so the anchor
// resolved onto the component itself and looked correct; at any nested path it
// did not. #6367 measured exactly that: "3 of 3 DANGLING at
// services/api/Dockerfile, and correct only BY ACCIDENT at a root Dockerfile".
// So the carrier is due at nested paths and FORBIDDEN at root ones, where
// FileCarrierFor clause 3 rejects it — graph.EntityID hashes
// (repo, kind, name, sourceFile) and NOT Subtype (#6369/#6480), so a second
// SCOPE.Component named the path would land under the component's own id.
//
// MEASURED ON THE REPO'S OWN FIXTURES, at the paths the #6847 corpus walk
// hands the extractor. All three were offenders the moment the classifier let
// them through, and each carries a different FROM count:
//
//	src/fixtures/dockerfile/sample.Dockerfile              2 anchored IMPORTS
//	src/fixtures/sources/dockerfile/sample.Dockerfile      2 anchored IMPORTS
//	src/fixtures/real-world/docker/Dockerfile.multi_stage  3 anchored IMPORTS
//
// GRADED IN BOTH DIRECTIONS. "The edge now resolves" licenses an UNCONDITIONAL
// carrier, which would mint one bare orphan node per Dockerfile across a repo
// and be invisible to every recall-shaped assertion. The forbidden-row controls
// below — the root path, the FROM-less file, the empty file — are what forbid
// it, and they are what the permissive mutants die on.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// dfCarriers6852 returns every record that IS the file carrier for path — the
// SCOPE.Component/file record extractor.FileEntity mints. Subtype is what
// separates it from buildDockerfileEntity's own SCOPE.Component, whose subtype
// is "dockerfile"; at a root path the two would otherwise be indistinguishable
// by name, which is the whole reason clause 3 exists.
func dfCarriers6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.Name == path {
			out = append(out, r)
		}
	}
	return out
}

// dfPathAnchored6852 returns every relationship whose FromID is exactly path —
// the shape whose FROM end has nothing to resolve onto.
func dfPathAnchored6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
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

// dfNamedExactly6852 returns every record whose Name or QualifiedName is path.
// This is the resolution question refs.go actually asks.
func dfNamedExactly6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Name == path || r.QualifiedName == path {
			out = append(out, r)
		}
	}
	return out
}

// carrierSrcDockerfile6852 has ONE FROM, so exactly one path-anchored IMPORTS,
// plus enough other instructions that the record is not degenerate — the "no
// carrier" controls must not be able to pass because the fixture extracted
// nothing.
const carrierSrcDockerfile6852 = `FROM golang:1.22-alpine

WORKDIR /app
RUN go build ./...
EXPOSE 8080
ENV APP_ENV=production
ENTRYPOINT ["/app/server"]
`

// multiStageSrcDockerfile6852 has THREE FROMs and a COPY --from, so it anchors
// three IMPORTS on one path while also exercising the #6367 USES edge that must
// stay off the path.
const multiStageSrcDockerfile6852 = `FROM golang:1.22-alpine AS builder
WORKDIR /build
RUN go build -o /out/server ./cmd/server

FROM alpine:3.19 AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
COPY --from=builder /out/server /server
ENTRYPOINT ["/server"]
`

// extract6852 is the package's ordinary extraction path, spelled once so every
// test below drives the same production code the corpus walk does.
func extract6852(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	return extractEntities(t, path, src, parseForTest(t, src))
}

// resolveDockerfile6852 extracts src at path, stamps ids the way graph assembly
// does, runs the production resolver pipeline, and returns the records plus the
// id→record index. It asserts on the EMITTED ARTEFACT after resolution — the
// edge's FROM end — not on a helper's return value or a counter.
func resolveDockerfile6852(t *testing.T, src, path string) ([]types.EntityRecord, map[string]*types.EntityRecord) {
	t.Helper()
	recs := extract6852(t, path, src)
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

// TestDockerfile_FromImportsFromEndResolves_6852 is the fix's behavioural test.
//
// Axis VARIED: path DEPTH. HELD CONSTANT: the source. The two depths are not
// interchangeable here and the asymmetry is the point — the root case passed
// BEFORE this change (the component's BASENAME name is the path) and the nested
// case did not, so a test at root depth alone would have graded nothing at all.
// Both are asserted so the accident is pinned as behaviour rather than left to
// be re-derived.
func TestDockerfile_FromImportsFromEndResolves_6852(t *testing.T) {
	for _, path := range []string{"services/api/Dockerfile", "Dockerfile"} {
		t.Run(path, func(t *testing.T) {
			recs, byID := resolveDockerfile6852(t, carrierSrcDockerfile6852, path)

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
							"iff some record carries that exact string as its Name — emit a "+
							"file carrier, internal/extractor/file_carrier.go)",
							recs[i].Name, r.FromID)
					}
				}
			}
			if seen == 0 {
				t.Fatal("fixture produced no IMPORTS edges — this measurement is vacuous")
			}
		})
	}
}

// TestDockerfile_FixtureSpellingsResolve_6852 varies the FILE NAME rather than
// the depth, over the three spellings this repo's own fixtures actually use —
// the spellings #6854 taught the classifier. A carrier wired to a
// basename-shaped condition (`== "Dockerfile"`) would pass every test above and
// leave every real fixture in the tree dangling.
//
// HELD CONSTANT: nested depth and the one-FROM source. The multi_stage NAME is
// driven here with the ordinary source on purpose; its three-FROM content is a
// different axis, graded separately by the multiplicity test.
func TestDockerfile_FixtureSpellingsResolve_6852(t *testing.T) {
	for _, path := range []string{
		"src/fixtures/dockerfile/sample.Dockerfile",
		"src/fixtures/sources/dockerfile/sample.Dockerfile",
		"src/fixtures/real-world/docker/Dockerfile.multi_stage",
		"deploy/Containerfile",
		"deploy/api.Containerfile",
	} {
		t.Run(path, func(t *testing.T) {
			// The premise is read BEFORE resolution: ReferencesEmbedded is
			// what rewrites a path-valued FromID onto the carrier's id, so
			// asking for path-anchored edges afterwards would find none —
			// which is the fix working, not the fixture being empty.
			raw := extract6852(t, path, carrierSrcDockerfile6852)
			if n := len(dfPathAnchored6852(raw, path)); n == 0 {
				t.Fatalf("premise: %q anchors no relationship on its own path", path)
			}
			if n := len(dfCarriers6852(raw, path)); n != 1 {
				t.Errorf("want exactly 1 file carrier for %q, got %d", path, n)
			}

			recs, byID := resolveDockerfile6852(t, carrierSrcDockerfile6852, path)
			for i := range recs {
				for _, r := range recs[i].Relationships {
					if r.Kind == "IMPORTS" {
						if _, ok := byID[r.FromID]; !ok {
							t.Errorf("IMPORTS FROM end %q resolves to no record", r.FromID)
						}
					}
				}
			}
		})
	}
}

// TestDockerfile_RootPathGetsNoSecondCarrier_6852 drives FileCarrierFor CLAUSE 3
// in production. buildDockerfileEntity names its SCOPE.Component
// BASENAME(file.Path), so at a root path that name IS the path and a carrier
// would put a second SCOPE.Component under the same graph.EntityID — Subtype is
// not hashed (#6369/#6480).
//
// The nested subtest is the contrast, not decoration: without it this test would
// pass identically for a carrier that is never emitted anywhere.
func TestDockerfile_RootPathGetsNoSecondCarrier_6852(t *testing.T) {
	t.Run("root path — the component name IS the path", func(t *testing.T) {
		const path = "Dockerfile"
		recs := extract6852(t, path, carrierSrcDockerfile6852)
		if n := len(dfPathAnchored6852(recs, path)); n == 0 {
			t.Fatal("premise: the fixture must still anchor an IMPORTS on the path")
		}
		named := dfNamedExactly6852(recs, path)
		if len(named) != 1 {
			t.Fatalf("exactly 1 record may be named %q, got %d — clause 3 must reject a "+
				"second carrier when the extractor already minted a path-named record",
				path, len(named))
		}
		if named[0].Subtype != "dockerfile" {
			t.Errorf("the one record named %q must be the DOCKERFILE component, got subtype %q",
				path, named[0].Subtype)
		}
		if n := len(dfCarriers6852(recs, path)); n != 0 {
			t.Errorf("no file carrier may be minted when the component already carries the "+
				"path as its Name, got %d", n)
		}
	})

	t.Run("nested path — the same source DOES get a carrier", func(t *testing.T) {
		const path = "services/api/Dockerfile"
		recs := extract6852(t, path, carrierSrcDockerfile6852)
		if n := len(dfCarriers6852(recs, path)); n != 1 {
			t.Fatalf("want exactly 1 carrier at a nested path, got %d — without this the root "+
				"subtest above would pass for a carrier that is never emitted anywhere", n)
		}
	})

	t.Run("root Containerfile — the accident is not Dockerfile-specific", func(t *testing.T) {
		const path = "Containerfile"
		recs := extract6852(t, path, carrierSrcDockerfile6852)
		if n := len(dfCarriers6852(recs, path)); n != 0 {
			t.Errorf("no carrier may be minted at root %q either, got %d", path, n)
		}
		if n := len(dfNamedExactly6852(recs, path)); n != 1 {
			t.Errorf("exactly 1 record may be named %q, got %d", path, n)
		}
	})
}

// TestDockerfile_OneCarrierPerFileNotPerFrom_6852 is the multiplicity control.
// Axis VARIED: the NUMBER of FROM instructions (three, each anchoring its own
// IMPORTS on the same path). HELD CONSTANT: one file, one nested path. The
// carrier is per-FILE, not per-EDGE; a per-edge carrier would put three nodes
// under one id.
func TestDockerfile_OneCarrierPerFileNotPerFrom_6852(t *testing.T) {
	const path = "services/api/Dockerfile"
	recs := extract6852(t, path, multiStageSrcDockerfile6852)
	if n := len(dfPathAnchored6852(recs, path)); n != 3 {
		t.Fatalf("premise: want 3 path-anchored IMPORTS edges, got %d", n)
	}
	if n := len(dfCarriers6852(recs, path)); n != 1 {
		t.Errorf("3 FROM instructions must still yield exactly 1 file carrier, got %d", n)
	}
	if n := len(dfNamedExactly6852(recs, path)); n != 1 {
		t.Errorf("exactly 1 record may be named %q, got %d", path, n)
	}
	// #6367's edges must not have moved back onto the path while this change
	// was made: a USES or CONTAINS with FromID == path would also be "anchored"
	// and would make the premise above pass for the wrong reason.
	for _, r := range dfPathAnchored6852(recs, path) {
		if r.Kind != "IMPORTS" {
			t.Errorf("only IMPORTS may anchor on the path (#6367), got kind %q", r.Kind)
		}
	}
}

// TestDockerfile_NoFromGetsNoCarrier_6852 is the OVER-FIRING control and the
// half of the grade a "the edge now resolves" test cannot supply. A Dockerfile
// with no FROM extracts nothing at all, so it anchors nothing and must gain
// nothing. Axis VARIED: the FROM instructions (absent). HELD CONSTANT: a file
// with real content — RUN, ENV, EXPOSE — at both depths.
func TestDockerfile_NoFromGetsNoCarrier_6852(t *testing.T) {
	const src = `RUN echo hello
ENV APP_ENV=production
EXPOSE 8080
`
	for _, path := range []string{"services/api/Dockerfile", "Dockerfile"} {
		t.Run(path, func(t *testing.T) {
			recs := extract6852(t, path, src)
			if len(recs) != 0 {
				t.Fatalf("a FROM-less Dockerfile must extract no records at all, got %d "+
					"(first: kind=%q subtype=%q name=%q) — an unconditional carrier mints "+
					"one bare orphan node per Dockerfile across a whole repo, which no "+
					"recall-shaped assertion can see",
					len(recs), recs[0].Kind, recs[0].Subtype, recs[0].Name)
			}
		})
	}
}

// TestDockerfile_EmptyFileGetsNoCarrier_6852 drives Extract's OTHER return
// path: len(file.Content) == 0 returns before the tree walk runs at all. A
// carrier placed near the top of Extract rather than at its final return could
// mint a node for a file with no content whatsoever.
func TestDockerfile_EmptyFileGetsNoCarrier_6852(t *testing.T) {
	const path = "services/api/Dockerfile"
	recs := extractEntities(t, path, "", nil)
	if len(recs) != 0 {
		t.Fatalf("an empty Dockerfile must extract no records at all, got %d (first: kind=%q name=%q)",
			len(recs), recs[0].Kind, recs[0].Name)
	}
}

// TestDockerfile_CarrierShape_6852 pins what the carrier IS: stamped
// dockerfile, anchored on the file it names, owning no relationships of its own
// (the component still carries the IMPORTS edges, so re-homing them onto the
// carrier would double every edge), and first in the slice per the #577
// convention.
func TestDockerfile_CarrierShape_6852(t *testing.T) {
	const path = "services/api/Dockerfile"
	recs := extract6852(t, path, carrierSrcDockerfile6852)
	cs := dfCarriers6852(recs, path)
	if len(cs) != 1 {
		t.Fatalf("premise: want 1 carrier, got %d", len(cs))
	}
	// The lang ARGUMENT is directly observable for this caller, unlike one whose
	// carrier passes through TagEntitiesLanguage. That call sits ABOVE the
	// PrependFileCarrier line in Extract and only fills an EMPTY Language, so a
	// carrier prepended afterwards keeps whatever token the argument gave it:
	// `""` stays `""` and this assertion sees it.
	if cs[0].Language != "dockerfile" {
		t.Errorf("carrier Language = %q, want %q", cs[0].Language, "dockerfile")
	}
	// The premise THAT rests on, pinned rather than assumed: if the carrier were
	// moved above TagEntitiesLanguage, an empty token would be filled in and the
	// assertion above would go on passing while grading nothing. A tagged record
	// also acquires Properties["language"], which the component does not carry.
	if v, ok := cs[0].Properties["language"]; ok {
		t.Errorf("carrier carries Properties[%q]=%q — it was language-tagged after the fact "+
			"rather than stamped by the lang argument, so the empty-token mutant is no "+
			"longer observable on Language", "language", v)
	}
	if cs[0].SourceFile != path {
		t.Errorf("carrier SourceFile = %q, want %q", cs[0].SourceFile, path)
	}
	if n := len(cs[0].Relationships); n != 0 {
		t.Errorf("the dockerfile file carrier must own no relationships, got %d", n)
	}
	if n := len(dfPathAnchored6852(recs, path)); n != 1 {
		t.Errorf("the FROM IMPORTS edge must still be emitted exactly once, got %d", n)
	}
	if recs[0].Name != path {
		t.Errorf("carrier must be record 0 (#577 convention), got %q at index 0", recs[0].Name)
	}
	if n := len(recs); n != 2 {
		t.Errorf("want exactly 2 records (carrier + component), got %d", n)
	}
}
