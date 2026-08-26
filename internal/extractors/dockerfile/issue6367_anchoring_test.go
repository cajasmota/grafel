package dockerfile_test

import (
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// issue6367_anchoring_test.go — the dockerfile COPY --from USES edge must be
// anchored on the record that OWNS it, not on the source file's path.
//
// THE SITE (#6367, allow-list entry dockerfile:collectCopy:USES{1} in
// internal/extractors/file_anchored_rels_guard_test.go): collectCopy built USES
// with FromID: file.Path, but the record is attached to the single file-level
// SCOPE.Component whose Name is the BASENAME (buildDockerfileEntity strips
// everything up to the last '/'), so a path-valued FromID matches only when the
// path IS its own basename.
//
// WHAT WAS MEASURED, not inferred. The fixture below is extracted, id-stamped
// and pushed through the production resolver pipeline (ResolveImports →
// ReferencesEmbedded) at two paths:
//
//	nested "services/api/Dockerfile" → 3 of 3 USES DANGLING
//	root   "Dockerfile"             → 3 of 3 USES resolve, onto the file
//	                                  component — the right node BY ACCIDENT,
//	                                  since the component's Name is the BASENAME
//
// So dockerfile is DANGLING in the general case and correct only by accident at
// the repo root. Unlike hcl's DEPENDS_ON there is no MISANCHOR column here: the
// package emits exactly ONE entity per file and that entity IS the owner, so
// the accidental spelling happens to land on the right node. The nested path is
// therefore the only row that can fail, and it must not be dropped.
//
// The sibling CONTAINS edges already leave FromID empty and are asserted here
// too, so a regression that re-introduces a path anchor on them is caught.
//
// IMPORTS is deliberately untouched: #120 keeps the file path on IMPORTS edges,
// and TestDockerfile_ImportsKeepsFilePathAnchor below pins that at BOTH depths
// so a fix that blanks FromID too broadly is caught rather than silently
// accepted (the pre-existing TestDockerfile_Imports_FromInstruction pins only
// the root path, where the two spellings are indistinguishable).

// anchorSrcDockerfile exercises both COPY --from spellings: a --from=<alias>
// that IS in aliasToImage (rewritten to the base image) and a --from=<n> stage
// index that is NOT (kept verbatim). Both flow through the same FromID, but a
// fix applied inside only one branch would pass a single-spelling fixture.
const anchorSrcDockerfile = `FROM golang:1.22 AS builder
RUN go build ./...

FROM node:20 AS assets
RUN npm run build

FROM ubuntu:22.04 AS runtime
COPY --from=builder /src/app /app
COPY --from=assets /src/dist /dist
COPY --from=0 /etc/passwd /etc/passwd
`

type anchorEdge struct {
	kind string
	// ownerName, fromLabel and wantLabel are for the FAILURE MESSAGE only.
	// A label is Subtype+":"+Name, which is NOT unique. The assertion below
	// compares fromID against wantID — the resolved identities — and uses the
	// labels purely to say WHICH nodes those ids are.
	ownerName string
	fromLabel string
	wantLabel string
	// fromID is the endpoint the edge actually lands on after assembly's
	// substitution rule; wantID is the owning record's own id. resolved
	// reports whether fromID names a record that exists at all (a false here
	// is a DANGLING edge, not a misanchored one).
	fromID    string
	wantID    string
	resolved  bool
	rawFromID string
	toID      string
}

// measureDockerfileAnchoring extracts the fixture at path, replays graph
// assembly's "substitute the owner's id only when FromID is empty" rule, and
// returns the resolved FROM endpoint of every USES and CONTAINS edge together
// with the owner it should have landed on.
func measureDockerfileAnchoring(t *testing.T, path string) []anchorEdge {
	t.Helper()

	tree := parseForTest(t, anchorSrcDockerfile)
	recs := extractEntities(t, path, anchorSrcDockerfile, tree)
	if len(recs) == 0 {
		t.Fatalf("extract %s: no records", path)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6367", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}

	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))

	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	label := func(id string) string {
		if e := byID[id]; e != nil {
			return e.Subtype + ":" + e.Name
		}
		return "<UNRESOLVED:" + id + ">"
	}

	var out []anchorEdge
	for i := range recs {
		owner := &recs[i]
		for _, r := range owner.Relationships {
			if r.Kind != "USES" && r.Kind != "CONTAINS" {
				continue // IMPORTS keeps the file path on purpose (#120).
			}
			from := r.FromID
			if from == "" {
				from = owner.ID
			}
			_, ok := byID[from]
			out = append(out, anchorEdge{
				kind:      r.Kind,
				ownerName: owner.Name,
				fromLabel: label(from),
				wantLabel: label(owner.ID),
				fromID:    from,
				wantID:    owner.ID,
				resolved:  ok,
				rawFromID: r.FromID,
				toID:      r.ToID,
			})
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].kind != out[b].kind {
			return out[a].kind < out[b].kind
		}
		return out[a].toID < out[b].toID
	})
	return out
}

// TestDockerfile_UsesAndContainsAnchoredOnOwner is the fix's behavioural test:
// at BOTH a nested and a root path, every USES and CONTAINS edge must land on
// the file component that carries it, which requires FromID to be empty.
func TestDockerfile_UsesAndContainsAnchoredOnOwner(t *testing.T) {
	// "services/api/Dockerfile" (nested) is the ONLY path that can fail. At
	// the root path FromID: file.Path equals the component's basename Name,
	// so the bad spelling resolves onto the very node the fix targets and the
	// subtest is STRUCTURALLY INCAPABLE of failing — it passes with or without
	// the fix. It is kept as an explicit no-regression row, NOT as a second
	// failure mode; drop the nested path "to simplify" and this whole test
	// goes vacuous while staying green.
	for _, path := range []string{"services/api/Dockerfile", "Dockerfile"} {
		t.Run(path, func(t *testing.T) {
			edges := measureDockerfileAnchoring(t, path)

			dangling := map[string]int{}
			misanchored := map[string]int{}
			total := map[string]int{}
			targets := map[string]bool{}
			for _, e := range edges {
				total[e.kind]++
				if e.kind == "USES" {
					targets[e.toID] = true
				}
				if e.fromID == e.wantID {
					continue
				}
				if !e.resolved {
					dangling[e.kind]++
				} else {
					misanchored[e.kind]++
				}
				t.Errorf("%s -> %q owned by %q: FROM = %s (id %q), want %s (id %q) "+
					"(FromID=%q must be empty so assembly stamps the owning record)",
					e.kind, e.toID, e.ownerName, e.fromLabel, e.fromID, e.wantLabel, e.wantID, e.rawFromID)
			}
			for _, kind := range []string{"USES", "CONTAINS"} {
				if total[kind] == 0 {
					t.Errorf("no %s edges produced by the fixture — the measurement is vacuous", kind)
				}
				if dangling[kind] != 0 || misanchored[kind] != 0 {
					t.Logf("MEASURED %s at %s: %d dangling, %d misanchored, of %d",
						kind, path, dangling[kind], misanchored[kind], total[kind])
				}
			}
			// Both --from spellings must be present, or a fix confined to one
			// branch of collectCopy would pass. The alias rows rewrite to the
			// base image; the stage-index row stays verbatim.
			if total["USES"] != 3 {
				t.Errorf("USES count = %d, want 3 (two aliased --from, one stage-index --from)", total["USES"])
			}
			for _, want := range []string{"golang:1.22", "node:20", "0"} {
				found := false
				for to := range targets {
					if len(to) >= len(want) && to[len(to)-len(want):] == want {
						found = true
					}
				}
				if !found {
					t.Errorf("no USES edge targeting %q — the fixture no longer covers that --from spelling", want)
				}
			}
		})
	}
}

// TestDockerfile_ImportsKeepsFilePathAnchor pins the OTHER direction at BOTH
// depths: the FROM IMPORTS edge deliberately keeps the file path as FromID
// (#120). The pre-existing TestDockerfile_Imports_FromInstruction pins only the
// root path, where "Dockerfile" is simultaneously the path and the entity Name,
// so it cannot distinguish a path anchor from a basename one — a fix that
// blanked FromID across the whole extractor would leave it green.
func TestDockerfile_ImportsKeepsFilePathAnchor(t *testing.T) {
	for _, path := range []string{"services/api/Dockerfile", "Dockerfile"} {
		t.Run(path, func(t *testing.T) {
			tree := parseForTest(t, anchorSrcDockerfile)
			recs := extractEntities(t, path, anchorSrcDockerfile, tree)
			seen := 0
			for i := range recs {
				for _, r := range recs[i].Relationships {
					if r.Kind != "IMPORTS" {
						continue
					}
					seen++
					if r.FromID != path {
						t.Errorf("IMPORTS -> %q: FromID = %q, want the file path %q (#120)",
							r.ToID, r.FromID, path)
					}
				}
			}
			if seen != 3 {
				t.Fatalf("fixture produced %d IMPORTS edges, want 3 (one per FROM) — this pin is vacuous", seen)
			}
		})
	}
}
