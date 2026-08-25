// Package main — incremental_mount_parity_6461_test.go
//
// CROSS-FILE MOUNT PARITY GATE (#6461).
//
// Why this file exists
// ────────────────────
// #6461: the daemon fast path (`internal/extractors/incremental.go`) prunes
// entities ONLY by `SourceFile` and never runs any Pass-2.6 cross-file
// composition pass. Any entity attributed to a file OTHER than the one that
// produced its content therefore goes stale and is never corrected — a
// composed endpoint on an unchanged mount file survives as a ghost alongside a
// freshly-extracted short one.
//
// `incremental_content_parity_6129_test.go` already asserts exactly the right
// property — graph(incremental → end state) ≡ graph(clean full rebuild), as a
// bidirectional content-keyed multiset, for Path A (daemon `TryIncremental`)
// and Path B (CLI `WithIncremental`) separately. Its fixture is Falcon/Flask
// only and contains NO cross-file mount, which is why this defect went
// unnoticed. This file adds the missing shape, reusing that file's comparator
// (`cpAssertParity`), allow-list type (`cpKnown`) and harness primitives
// (`dvFullRebuild` / `dvSeedManifest` / `dvIncremental` / `cfPathBIncremental`)
// verbatim rather than reimplementing them.
//
// Why FastAPI and not Django, even though the defect was found in Django's pass
// ──────────────────────────────────────────────────────────────────────────────
// `internal/indexer/diff/diff.go` cross-invalidates by BASENAME — `Filter`
// (:239-254) and `FilterWithGit` (:636-650, the one both paths actually call),
// with `moduleBase` (:780-787) as the stem without extension. Django's
// convention names the mount file and the route file BOTH `urls.py`, so editing
// either drags the other into the changed set. A Django case is therefore
// masked by a filename coincidence AND violates the 6129 fixture's
// unique-basename invariant.
//
// FastAPI's shape (`main.py` + `markets.py`) has distinct stems: no
// cross-invalidation, and the unique-basename constraint is satisfied
// naturally. That asymmetry is itself the proof of the masking.
//
// Both delta directions are exercised SEPARATELY, because they fail
// differently under a SourceFile-only prune:
//
//	direction ROUTE — edit only the route file. Its own entities are pruned and
//	  re-extracted; anything attributed to the UNCHANGED mount file is carried
//	  forward untouched. Stale-carry-forward is visible here.
//
//	direction MOUNT — edit only the mount file. Everything attributed to it is
//	  pruned; nothing re-derives what a full rebuild composes across the two.
//	  Vanishing composition is visible here.
//
// WHAT THIS GATE MEASURED WHEN IT WAS WRITTEN
// ────────────────────────────────────────────
// Every line below was RUN, not predicted. Command:
//
//	GOMAXPROCS=4 GRAFEL_TEST_6461=1 go test ./cmd/grafel/ -count=1 \
//	    -run 'TestMountParity_6461' -v
//
// FastAPI — the vehicle the gate is built on:
//
//	ROUTE / path A   GREEN.  0-entity delta.
//	MOUNT / path B   GREEN.  0-entity delta.
//	ROUTE / path B   was RED after the #6469 route-path RENAME, now GREEN and
//	                   UNGATED (#6482). The 1 divergence, edges full=54 inc=55,
//	                   was:
//	                   [EDGE-INVENTED] SCOPE.Process/http:GET:/conditions
//	                     → «unbound»proc:0c089ba065f57543 :RENAMED_FROM
//	                   — NOT a defect: the reference side (`mpEndState`) rebuilds
//	                   into a FRESH state dir, so pass 5.5 has no prior graph and
//	                   is a no-op by construction, while the Path-B side has one.
//	                   RENAMED_FROM is history-dependent, so it is scoped out of
//	                   the comparator (cpIgnoredRelKinds) and asserted positively
//	                   instead. See #6482 and the note on the test itself.
//	MOUNT / path A   was RED (1 divergence, edges full=54 inc=53):
//	                   [EDGE-LOST] Service/mp_app@mpmain_mount.py
//	                     → Route/mp_router@mpmarkets_route.py :ROUTES_TO
//	                   now GREEN and UNGATED. Step 7c in
//	                   internal/extractors/incremental.go re-offers the
//	                   Pass-2.5 standalone stubs the FILE-SCOPED binder refused
//	                   (`pass25_rels_bound=0 pass25_rels_dropped=4`) to a
//	                   corpus-wide, ambiguity-refusing `Kind:Name` index. Only
//	                   that one edge changed: the three Django diagnostics
//	                   below report byte-identical divergence lists before and
//	                   after.
//
// So #6461's GHOST does NOT reproduce on FastAPI today, and the reason is
// worth stating precisely rather than treating the gate as a failure: FastAPI
// COMPOSES NO PATH at all yet — that is #6414, still unbuilt. A full rebuild
// emits the short `http:GET:/terms path=/terms` on the route file plus a
// SEPARATE additive `http:ANY:/network:mount` synthetic on the mount file
// (#6385). Both are attributed to their own content source, so a SourceFile
// prune handles both correctly and there is no composed entity to go stale.
// The one thing that does break is the CROSS-FILE mount EDGE: when the mount
// file alone changes, Pass 2.5 re-runs file-scoped, cannot see `mp_router` in
// the unchanged route file, and drops the edge (`pass25_rels_dropped=4`).
// Same root cause — a cross-file derivation the daemon cannot rebuild — one
// level down, on an edge rather than an entity.
//
// Django — the issue's headline instance, kept as a DIAGNOSTIC below because
// it cannot live in the shared unique-basename fixture:
//
//	ROUTE-PATH RENAME / path A   RED, and it is the GHOST verbatim.
//	  full rebuild : http:ANY:/network/conditions @ mpsite/views.py
//	  incremental  : http:ANY:/network/terms      @ mpsite/views.py
//	  1 endpoint LOST + 1 endpoint INVENTED in the ENDPOINT CENSUS
//	  (mpLogEndpointDelta, which counts only http_endpoint/url_mount entities);
//	  the full parity report is wider — 3 ENTITY-LOST and 2 ENTITY-INVENTED
//	  across all kinds, 22 divergences total,
//	  edges full=38 inc=35, entities full=21 inc=20.
//	  Only `mpsite/urls.py` changed (`incremental: done changed=1`) — the
//	  composed endpoint is attributed to `mpsite/views.py`, which did not
//	  change, so it is carried forward verbatim and never recomposed.
//	  NOTE: basename cross-invalidation did NOT fire here (changed=1, not 2),
//	  so the masking #6414 describes is narrower than assumed.
//	ROUTE / path A   RED. 18 divergences; the composed
//	  http:ANY:/network/terms@mpsite/views.py is LOST entirely along with its
//	  SCOPE.Process subtree. edges full=38 inc=27, entities full=21 inc=18.
//	MOUNT / path A   RED. 5 divergences; the `http:ANY:/network:mount`
//	  synthetic on mpproj/urls.py is LOST. edges full=38 inc=35.
//
// The conclusion the issue lacked: the defect is REAL and reproduces on PATH A
// ONLY. It reproduces as a GHOST (stale composed endpoint coexisting with the
// fresh graph) exactly where #6461 says, but on Django, not on FastAPI —
// FastAPI reaches only the edge-level form of it until #6414 lands. Path B
// re-runs the pipeline over the merged slice and is clean on every case here.
//
// WHAT THE UNGATED (ALWAYS-RUN) SET ACTUALLY WATCHES
// ───────────────────────────────────────────────────
// FIVE run by default: ROUTE/path A, ROUTE/path B, MOUNT/path B, the rename
// assertion (`TestRenameParity_6482_PathBIncremental_EmitsRenamedFrom`), and —
// as of the MOUNT-direction fix — MOUNT/path A. Four of those were ungated by
// #6482; MOUNT/path A stayed gated because it still reproduced. It no longer
// does, so BOTH directions of BOTH paths are now live ratchets and a
// regression that drops an edge or entity attributed to the unchanged ROUTE
// file when the MOUNT file is edited turns the default suite red. Verified by
// mutation: disabling Step 7c fails TestMountParity_6461_MountEdit_PathA in
// the DEFAULT suite (GRAFEL_TEST_6461 unset).
//
// What GRAFEL_TEST_6461 still gates is the THREE Django diagnostics, and they
// are a DIFFERENT cause — the cross-extractor residual tracked as #6529, not
// this file's SourceFile-prune story. See mp6461Cause.
//
// Refs #6461, #6414, #6415, #6385, #6129, #6482, #6529.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/algorithms"
	"github.com/cajasmota/grafel/internal/graph"
)

// ─────────────────────────── fixture ───────────────────────────
//
// EVERY BASENAME IS UNIQUE, for the reason the 6129 fixture documents at
// :64-67: `diff.FilterWithGit` cross-invalidates by basename, which would drag
// the "unchanged" file into the changed set and quietly turn a one-file delta
// into a full-vs-full no-op.

// mpFiller writes inert files that are NEVER touched. They exist so that the
// incremental run has something to CARRY FORWARD — `cfPathBIncremental`
// fails the test if the carry-forward record is empty, which would make the
// comparison vacuous.
func mpFiller(t *testing.T, repo string) {
	t.Helper()
	dvWriteFile(t, repo, "mputil_static.py", `def mp_helper(x):
    return x + 1


MP_UTIL = "mp"
`)
	dvWriteFile(t, repo, "mpmodels_static.py", `class MpThing:
    def mp_value(self):
        return 7
`)
}

// mpRouteFile writes the FastAPI ROUTE file — the one that owns the decorator
// path.
//
// `routePath` exists so the ROUTE direction can RENAME the decorator path, not
// merely bump an opaque counter (#6469 review). A pass-counter-only edit leaves
// the decorator path frozen at `/terms`, so a composed endpoint attributed to
// the unchanged MOUNT file would be byte-identical before and after — invisible
// to a content-keyed comparator, and the ratchet below would be silent for
// exactly the Django-shaped defect this file warns about. Renaming the path
// makes a stale carried-forward composition differ from the recomposed one.
// This mirrors TestMountParity_6461_Django_RoutePathRename_PathA.
//
// `routePass` additionally varies the function body so the file's own entities
// change too.
func mpRouteFile(t *testing.T, repo, routePath string, routePass int) {
	t.Helper()
	dvWriteFile(t, repo, "mpmarkets_route.py", fmt.Sprintf(`from fastapi import APIRouter

from mputil_static import mp_helper

mp_router = APIRouter()


@mp_router.get("%s")
def mp_read_terms(chain_id: int):
    return {"ok": mp_helper(%d)}


MP_ROUTE_PASS = %d
`, routePath, routePass, routePass))
}

// mpMountFile writes the FastAPI MOUNT file — the one that owns the prefix.
// It imports the router from the route file, so the two are genuinely
// cross-file. `mountPass` is the only thing that varies between baseline and
// end state in the MOUNT direction.
func mpMountFile(t *testing.T, repo string, mountPass int) {
	t.Helper()
	dvWriteFile(t, repo, "mpmain_mount.py", fmt.Sprintf(`from fastapi import FastAPI

from mpmarkets_route import mp_router

mp_app = FastAPI()
mp_app.include_router(mp_router, prefix="/network")


@mp_app.get("/mphealth")
def mp_health():
    return {"up": %d}


MP_MOUNT_PASS = %d
`, mountPass, mountPass))
}

// mpWrite lays down the whole corpus at the given (route, mount) pass state.
func mpWrite(t *testing.T, repo, routePath string, routePass, mountPass int) {
	t.Helper()
	mpFiller(t, repo)
	mpRouteFile(t, repo, routePath, routePass)
	mpMountFile(t, repo, mountPass)
}

// mpEndState builds a PRISTINE repo holding exactly the end state and returns a
// CLEAN FULL REBUILD of it — the reference side of every comparison. A separate
// repo dir keeps it uncontaminated by the baseline run's state.
func mpEndState(t *testing.T, routePath string, routePass, mountPass int) *graph.Document {
	t.Helper()
	repo := t.TempDir()
	mpWrite(t, repo, routePath, routePass, mountPass)
	return dvFullRebuild(t, repo, t.TempDir())
}

// ─────────────────────── diagnostics ───────────────────────

// mpEndpointSet returns a sorted, human-readable census of every HTTP endpoint
// entity in a document: `name @ source_file (path=…)`. This is the "actual vs
// expected entity set" #6461 asks for; it is LOGGED on every run so that a
// failure carries the measured delta rather than a bare count.
func mpEndpointSet(d *graph.Document) []string {
	var out []string
	for _, e := range d.Entities {
		if !strings.Contains(strings.ToLower(e.Kind), "http_endpoint") &&
			!strings.Contains(strings.ToLower(e.Kind), "url_mount") {
			continue
		}
		p := ""
		if v, ok := e.PropLookup("path"); ok {
			p = " path=" + v
		}
		if v, ok := e.PropLookup("mount_prefix_applied"); ok {
			p += " mount_prefix_applied=" + v
		}
		out = append(out, fmt.Sprintf("%s|%s@%s%s", e.Kind, e.Name, e.SourceFile, p))
	}
	sort.Strings(out)
	return out
}

// mpEndpointDelta returns the symmetric difference of the two endpoint/mount
// censuses: `lost` is present in the full rebuild and absent from the
// incremental result, `invented` is the reverse.
//
// It is a FUNCTION rather than inlined into the logger because the census
// delta is the #6461 GHOST property on its own — a stale composed endpoint
// carried forward on an unchanged file shows up here as one LOST (the
// recomposed path) plus one INVENTED (the pre-edit path), with no reference to
// any other divergence class. Asserting it directly (see
// TestMountParity_6461_Django_RoutePathRename_PathA_EndpointCensus) gives a
// ratchet on the ghost that does not have to wait for the unrelated defects
// the full parity comparator also sees on this fixture.
func mpEndpointDelta(full, inc *graph.Document) (lost, invented []string) {
	fa, ib := mpEndpointSet(full), mpEndpointSet(inc)
	inFull := map[string]int{}
	for _, s := range fa {
		inFull[s]++
	}
	inInc := map[string]int{}
	for _, s := range ib {
		inInc[s]++
	}
	for s, n := range inFull {
		if inInc[s] < n {
			lost = append(lost, s)
		}
	}
	for s, n := range inInc {
		if inFull[s] < n {
			invented = append(invented, s)
		}
	}
	sort.Strings(lost)
	sort.Strings(invented)
	return lost, invented
}

// mpLogEndpointDelta logs the endpoint census on both sides plus the symmetric
// difference, so the measured entity delta is in the test output whether the
// gate passes or fails.
func mpLogEndpointDelta(t *testing.T, label string, full, inc *graph.Document) {
	t.Helper()
	fa, ib := mpEndpointSet(full), mpEndpointSet(inc)
	lost, invented := mpEndpointDelta(full, inc)
	t.Logf("%s: endpoint/mount census — full rebuild (%d):", label, len(fa))
	for _, s := range fa {
		t.Logf("      A %s", s)
	}
	t.Logf("%s: endpoint/mount census — incremental (%d):", label, len(ib))
	for _, s := range ib {
		t.Logf("      B %s", s)
	}
	t.Logf("%s: MEASURED DELTA — %d lost (in full, absent from incremental), %d invented "+
		"(in incremental, absent from full)", label, len(lost), len(invented))
	for _, s := range lost {
		t.Logf("      LOST     %s", s)
	}
	for _, s := range invented {
		t.Logf("      INVENTED %s", s)
	}
}

// ─────────────────────── the #6461 red-gate switch ───────────────────────

// mp6461Env is the switch that makes the remaining DIAGNOSTICS run. The branch
// must not land a red suite, so any direction/path pair that currently
// reproduces SKIPS with an explicit issue reference unless this is set. As of
// the MOUNT-direction fix only the three Django diagnostics are behind it;
// every FastAPI direction/path pair is a live, always-run ratchet.
//
// Reproduce on demand with:
//
//	GOMAXPROCS=4 GRAFEL_TEST_6461=1 go test ./cmd/grafel/ -count=1 \
//	    -run 'TestMountParity_6461' -v
const mp6461Env = "GRAFEL_TEST_6461"

// mp6461Cause is the ONE cause every pair still gated in this file reproduces.
// It is stated once, here, rather than being welded into the gate helper, so a
// gate can never again assert a cause the gated test does not exhibit.
//
// It WAS welded in, and it mis-attributed TestMountParity_6461_RouteEdit_PathB
// (#6482): that test runs the CLI path (`Index` + `WithIncremental`), not the
// daemon fast path, so neither `SourceFile` pruning nor cross-file composition
// had anything to do with its failure. The skip line still read plausibly,
// which is exactly why the mis-attribution survived review. That test is now
// ungated; the cause is a parameter so the mistake cannot recur silently.
// Updated with the MOUNT-direction fix: the daemon DOES now run cross-file
// composition (Step 7b, #6528) and cross-file Pass-2.5 stub binding (Step 7c),
// so the old wording — "prunes only by SourceFile and runs no cross-file
// composition pass" — would have been a cause the remaining gated pairs no
// longer exhibit, which is exactly the mis-attribution #6482 removed once.
// What the three Django diagnostics below still exhibit is the cross-extractor
// residual: TryIncremental runs no cross extractors, so Django's nested-URLConf
// composition is only partially reproducible on the fast path.
const mp6461Cause = "TryIncremental runs no cross extractors, so Django's " +
	"nested-URLConf composition is only partially reproducible on the fast " +
	"path (#6529)"

// mp6461Gate skips unless mp6461Env is set, naming the issue, the pair, and
// the exact command that reproduces. `cause` is a PARAMETER, not a constant
// baked into the body: every gated pair must state the cause it actually
// exhibits, and passing mp6461Cause is an explicit claim that this pair is a
// #6461 instance rather than something else that happens to be red.
func mp6461Gate(t *testing.T, what, cause string) {
	t.Helper()
	if os.Getenv(mp6461Env) == "" {
		t.Skipf("#6461 UNFIXED — %s. This assertion is expected to FAIL: %s. "+
			"Reproduce with: %s=1 go test ./cmd/grafel/ -count=1 "+
			"-run 'TestMountParity_6461' -v", what, cause, mp6461Env)
	}
}

// mpKnown is the known-divergence allow-list for the mount fixture. It is
// EMPTY: this gate was written with no allowances so that whatever it reports
// is the measured, uncharacterised delta. It obeys the 6129 rule — an entry
// here needs a filed issue and a stated reason, and the list can only shrink.
var mpKnown []cpKnown

// ─────────────────────── direction ROUTE ───────────────────────
//
// Edit ONLY the route file. Under a SourceFile-only prune the route file's own
// entities are pruned and re-extracted, while anything a full rebuild
// attributes to the UNCHANGED mount file is carried forward from the baseline
// verbatim. If composition attributes a composed endpoint to the mount file,
// the carried copy is STALE and coexists with the fresh short one.

func TestMountParity_6461_RouteEdit_PathA(t *testing.T) {
	// MEASURED GREEN (see the header). NOT gated: FastAPI composes no path today,
	// so this is a live ratchet. The edit RENAMES the decorator path (see
	// mpRouteFile) so a composed endpoint attributed to the unchanged MOUNT file
	// would differ between the carried-forward copy and the recomposed one —
	// mutation-verified in #6469 review: with the pass-counter-only fixture a
	// #6414-shaped composition survived this test; with the rename it dies here.
	repo := t.TempDir()
	stateDir := t.TempDir()
	mpWrite(t, repo, "/terms", 0, 0)
	dvFullRebuild(t, repo, stateDir)

	full := mpEndState(t, "/conditions", 1, 0)

	dvSeedManifest(t, repo, stateDir)
	mpRouteFile(t, repo, "/conditions", 1) // ONLY the route file changes
	inc := dvIncremental(t, repo, stateDir)

	mpLogEndpointDelta(t, "ROUTE/path A", full, inc)
	cpAssertParity(t, "#6461 direction ROUTE, path A (extractors.TryIncremental)",
		full, inc, mpKnown)
}

func TestMountParity_6461_RouteEdit_PathB(t *testing.T) {
	// UNGATED as of #6482 — it now runs in the default suite. It was gated at
	// #6469 for a divergence that turned out to be a HARNESS artefact, not a
	// defect. Measured then, verbatim:
	//
	//	edges full=54 inc=55 | entities full=28 inc=28
	//	[EDGE-INVENTED] SCOPE.Process/http:GET:/conditions → mp_helper@...
	//	  →«unbound»proc:0c089ba065f57543 :RENAMED_FROM
	//
	// The word "invents" was wrong twice over, and #6482 records the check:
	//
	//  1. `DetectRenamesBounded` has ONE non-test call site (`index.go:876`),
	//     gated solely on a prior `graph.fb` existing in the output dir.
	//     Incrementality is never consulted. The delta is manufactured by the
	//     baseline: `mpEndState` full-rebuilds into a FRESH t.TempDir(), so
	//     `LoadGraphFromDir` fails, `prevDoc == nil`, and pass 5.5 is a no-op BY
	//     CONSTRUCTION — that baseline can never emit RENAMED_FROM. The Path-B
	//     side re-runs over the already-populated state dir, loads a prior, and
	//     detects the rename. The two sides were not the same experiment.
	//     A plain full rebuild DOES emit RENAMED_FROM — see
	//     TestRenameDetect_CompleteRunReportsNoTruncation, which runs Index twice
	//     into one state dir with NO WithIncremental and asserts renames != 0.
	//  2. The unbound target is the edge kind's CONTRACT, not a bug.
	//     `deleted` is "in prev, absent from new" and ToID points at it, so EVERY
	//     RENAMED_FROM edge is unbound in its own graph by design
	//     (`rename_detection.go:119-123`). "Fixing the dangling target" would
	//     have deleted a shipped feature.
	//
	// So the property, not the indexer, was wrong: parity was being asserted
	// over a HISTORY-DEPENDENT edge kind whose reference side structurally
	// cannot produce it. RENAMED_FROM is now scoped out of the comparator (see
	// cpIgnoredRelKinds) and asserted POSITIVELY instead, at Go level with a
	// cardinality the fixture rows cannot express, by
	// TestRenameParity_6482_PathBIncremental_EmitsRenamedFrom below. Scoping it
	// out without that positive assertion would be a silent blinding.
	repo := t.TempDir()
	stateDir := t.TempDir()
	mpWrite(t, repo, "/terms", 0, 0)
	dvFullRebuild(t, repo, stateDir)

	full := mpEndState(t, "/conditions", 1, 0)

	dvSeedManifest(t, repo, stateDir)
	mpRouteFile(t, repo, "/conditions", 1)
	inc := cfPathBIncremental(t, repo, stateDir)

	mpLogEndpointDelta(t, "ROUTE/path B", full, inc)
	cpAssertParity(t, "#6461 direction ROUTE, path B (Index + WithIncremental)",
		full, inc, mpKnown)
}

// ─────────────── #6482 — the positive rename assertion ───────────────

// mpEntityByID indexes a document's entities by ID.
func mpEntityByID(d *graph.Document) map[string]graph.Entity {
	m := make(map[string]graph.Entity, len(d.Entities))
	for _, e := range d.Entities {
		m[e.ID] = e
	}
	return m
}

// mpRenamedFromEdges returns every RENAMED_FROM relationship in d.
func mpRenamedFromEdges(d *graph.Document) []graph.Relationship {
	var out []graph.Relationship
	for _, r := range d.Relationships {
		if r.Kind == algorithms.RelKindRenamedFrom {
			out = append(out, r)
		}
	}
	return out
}

// TestRenameParity_6482_PathBIncremental_EmitsRenamedFrom is the POSITIVE half
// of the #6482 fix, and it is deliberately UNGATED.
//
// cpAssertParity now scopes `RENAMED_FROM` out of the comparator, because the
// reference side (a full rebuild into a fresh state dir) has no prior graph and
// so structurally cannot emit that edge kind. An ignore entry on its own is a
// SILENT BLINDING: after it, a change that stopped emitting `RENAMED_FROM`
// entirely on the incremental path would make every parity gate in this file
// GREENER, not redder. So the edge is asserted here instead, directly, with a
// cardinality and an identity that the fixture-row allow-list has no field for
// (#6488) and could not express.
//
// Why the existing guard is not enough:
// `TestRenameDetect_CompleteRunReportsNoTruncation`
// (`rename_truncation_report_6087_test.go`) already fails if rename detection
// is suppressed — but it drives `Index` with NO `WithIncremental`. An
// incremental-ONLY suppression sails straight through it. This test drives the
// same fixture as TestMountParity_6461_RouteEdit_PathB through
// `cfPathBIncremental`, i.e. the CLI incremental path, which is the only path
// that runs Pass 5.5 at all (`extractors.TryIncremental` never calls it).
//
// It also PINS THE CONTRACT that #6482's title got backwards: the edge's ToID
// is an entity of the PREVIOUS graph that is absent from the new one, so a
// `RENAMED_FROM` edge is UNBOUND IN ITS OWN GRAPH BY DESIGN
// (`internal/algorithms/rename_detection.go:119-123`: "Consumers that care
// about history can follow the edge backwards to recover old enrichment").
// Anyone who reads "dangling edge" as a bug and rebinds ToID to the new entity
// breaks this test rather than shipping the removal of a feature.
func TestRenameParity_6482_PathBIncremental_EmitsRenamedFrom(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	// Baseline: the pre-rename corpus, full-rebuilt into stateDir. This is the
	// graph Pass 5.5 will load as `prevDoc` on the next run.
	mpWrite(t, repo, "/terms", 0, 0)
	base := dvFullRebuild(t, repo, stateDir)

	// The baseline itself must carry NO rename edges — otherwise the count
	// asserted below could be inherited rather than produced by the rename.
	if n := len(mpRenamedFromEdges(base)); n != 0 {
		t.Fatalf("#6482: baseline full rebuild into an EMPTY state dir emitted %d %s edge(s); "+
			"it must emit none (no prior graph → prevDoc == nil → Pass 5.5 is a no-op), "+
			"otherwise the post-rename count below proves nothing",
			n, algorithms.RelKindRenamedFrom)
	}

	// Rename the decorator path. This changes the endpoint's entity ID, so the
	// old process entity is DELETED and a new one is ADDED — the only shape
	// DetectRenamesBounded can act on. (A body-only edit leaves IDs identical,
	// `len(deleted) == 0`, and the pass returns early.)
	dvSeedManifest(t, repo, stateDir)
	mpRouteFile(t, repo, "/conditions", 1)
	inc := cfPathBIncremental(t, repo, stateDir)

	renames := mpRenamedFromEdges(inc)
	for _, r := range renames {
		t.Logf("#6482: %s edge %s → %s", algorithms.RelKindRenamedFrom, r.FromID, r.ToID)
	}
	// CARDINALITY. Exactly one: the renamed endpoint's process. Zero means the
	// rename pass was suppressed on the incremental path; more than one means
	// the pass started matching entities it should not, which is the
	// over-permissive direction and just as much a regression.
	if len(renames) != 1 {
		t.Fatalf("#6482: expected EXACTLY 1 %s edge on the Path-B incremental run "+
			"after renaming the route path /terms → /conditions, got %d. "+
			"0 means rename detection is suppressed on the incremental path — which "+
			"TestRenameDetect_CompleteRunReportsNoTruncation would NOT catch, since it "+
			"exercises only the non-incremental path. >1 means the matcher became "+
			"over-permissive.",
			algorithms.RelKindRenamedFrom, len(renames))
	}
	e := renames[0]

	incByID, baseByID := mpEntityByID(inc), mpEntityByID(base)

	// FROM = the NEW entity, present in the new graph and absent from the prior.
	from, ok := incByID[e.FromID]
	if !ok {
		t.Fatalf("#6482: %s FromID %q is not an entity of the graph that emitted it; "+
			"the edge must originate at the POST-rename entity",
			algorithms.RelKindRenamedFrom, e.FromID)
	}
	if _, wasThere := baseByID[e.FromID]; wasThere {
		t.Errorf("#6482: %s FromID %q already existed in the PRE-rename graph — "+
			"the edge must originate at a newly-ADDED entity, not a surviving one",
			algorithms.RelKindRenamedFrom, e.FromID)
	}
	if !strings.Contains(from.Name, "/conditions") {
		t.Errorf("#6482: %s originates at %s|%s@%s, whose name does not mention the NEW "+
			"path /conditions; the edge is not tracking the rename that was made",
			algorithms.RelKindRenamedFrom, from.Kind, from.Name, from.SourceFile)
	}

	// TO = the OLD entity: present in the PRIOR graph, absent from this one.
	// Both halves matter. "present in prior" proves it points at real history
	// rather than an arbitrary id; "absent from this one" is the contract, and
	// asserting it is what stops a future "fix the dangling target" from
	// retargeting ToID onto the new entity and silently deleting the feature.
	old, ok := baseByID[e.ToID]
	if !ok {
		t.Errorf("#6482: %s ToID %q is not an entity of the PRE-rename graph. The edge "+
			"must point at the entity that was deleted by the rename (rename_detection.go "+
			"builds `deleted` as \"in prev, absent from new\"), so a ToID that never "+
			"existed before means the pass matched the wrong thing.",
			algorithms.RelKindRenamedFrom, e.ToID)
	} else {
		if !strings.Contains(old.Name, "/terms") {
			t.Errorf("#6482: %s points back at %s|%s@%s, whose name does not mention the OLD "+
				"path /terms; the edge is not tracking the rename that was made",
				algorithms.RelKindRenamedFrom, old.Kind, old.Name, old.SourceFile)
		}
		if old.Kind != from.Kind {
			t.Errorf("#6482: %s links kinds %q → %q; rename detection matches within a kind, "+
				"so a cross-kind edge is a mis-match",
				algorithms.RelKindRenamedFrom, from.Kind, old.Kind)
		}
	}
	if bound, ok := incByID[e.ToID]; ok {
		t.Errorf("#6482: %s ToID %q resolves to %s|%s IN ITS OWN GRAPH. That target is "+
			"UNBOUND BY DESIGN — it names the pre-rename entity so consumers can follow the "+
			"edge backwards to recover old enrichment (rename_detection.go:119-123). If this "+
			"fires because someone rebound ToID to \"fix a dangling edge\", the fix removed a "+
			"shipped feature; #6482's title was wrong about this, not the indexer.",
			algorithms.RelKindRenamedFrom, e.ToID, bound.Kind, bound.Name)
	}
}

// ─────────────────────── direction MOUNT ───────────────────────
//
// Edit ONLY the mount file. Everything attributed to it is pruned; nothing on
// the daemon path re-derives what a full rebuild composes across the pair, so
// composed endpoints VANISH rather than going stale.

// UNGATED as of the MOUNT-direction fix. This was the last #6461 pair still
// behind GRAFEL_TEST_6461: editing ONLY the mount file pruned the mount file's
// own entities and re-extracted it FILE-SCOPED, so Pass 2.5's standalone
// `Service:mp_app → Route:mp_router` REGISTERED_ON/ROUTES_TO relationship —
// whose TARGET is attributed to the UNCHANGED route file — could not bind and
// was dropped (`pass25_rels_dropped=4`, `pass25_rels_bound=0`), losing
//
//	[EDGE-LOST] Service/mp_app@mpmain_mount.py
//	  → Route/mp_router@mpmarkets_route.py :ROUTES_TO
//
// The fix (Step 7c in internal/extractors/incremental.go) re-offers those
// dropped stubs to a GLOBALLY-unique `Kind:Name` index built over the whole
// post-prune entity set, which is the same evidence base the full path's
// corpus resolver uses. It is now a live ratchet, so a regression that drops
// entities/edges attributed to the unchanged ROUTE file when the MOUNT file is
// edited turns `go test ./cmd/grafel/` red.
func TestMountParity_6461_MountEdit_PathA(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	mpWrite(t, repo, "/terms", 0, 0)
	dvFullRebuild(t, repo, stateDir)

	full := mpEndState(t, "/terms", 0, 1)

	dvSeedManifest(t, repo, stateDir)
	mpMountFile(t, repo, 1) // ONLY the mount file changes
	inc := dvIncremental(t, repo, stateDir)

	mpLogEndpointDelta(t, "MOUNT/path A", full, inc)
	cpAssertParity(t, "#6461 direction MOUNT, path A (extractors.TryIncremental)",
		full, inc, mpKnown)
}

func TestMountParity_6461_MountEdit_PathB(t *testing.T) {
	// MEASURED GREEN. Path B re-runs the whole pipeline over the merged slice, so
	// it recomposes where Path A does not. NOT gated — live ratchet.
	repo := t.TempDir()
	stateDir := t.TempDir()
	mpWrite(t, repo, "/terms", 0, 0)
	dvFullRebuild(t, repo, stateDir)

	full := mpEndState(t, "/terms", 0, 1)

	dvSeedManifest(t, repo, stateDir)
	mpMountFile(t, repo, 1)
	inc := cfPathBIncremental(t, repo, stateDir)

	mpLogEndpointDelta(t, "MOUNT/path B", full, inc)
	cpAssertParity(t, "#6461 direction MOUNT, path B (Index + WithIncremental)",
		full, inc, mpKnown)
}

// ─────────────────── Django diagnostic (#6461's headline shape) ───────────────────
//
// #6461 names Django's `ApplyDjangoNestedURLConf` as the known instance: it
// attributes the COMPOSED endpoint to the MOUNT file
// (`internal/engine/django_urlconf_nested.go:227`, `SourceFile: relPath`),
// which is not the file that produced its content.
//
// Django cannot live in the shared unique-basename fixture — both files are
// named `urls.py`, so `diff.FilterWithGit` cross-invalidates them (`:636-650`,
// `moduleBase` `:780-787`) and a one-file delta becomes a two-file one. That is
// exactly the masking #6414 describes, and it is why FastAPI is the vehicle for
// the gate above. This test gets its OWN repo so the collision is contained,
// and exists to MEASURE whether the composed-endpoint divergence reproduces at
// all — it is diagnostic, not a ratchet.
func mpDjangoRouteUrls(t *testing.T, repo, routePath string, routePass int) {
	t.Helper()
	dvWriteFile(t, repo, "mpsite/urls.py", fmt.Sprintf(`from django.urls import path

from . import views

urlpatterns = [
    path("%s", views.mp_terms_view, name="mp_terms_%d"),
]
`, routePath, routePass))
}

func mpDjangoWrite(t *testing.T, repo, routePath string, routePass, mountPass int) {
	t.Helper()
	dvWriteFile(t, repo, "mpsite/views.py", fmt.Sprintf(`def mp_terms_view(request):
    return {"ok": %d}
`, routePass))
	mpDjangoRouteUrls(t, repo, routePath, routePass)
	dvWriteFile(t, repo, "mpproj/urls.py", fmt.Sprintf(`from django.urls import include, path

urlpatterns = [
    path("network/", include("mpsite.urls")),
]

MP_DJ_MOUNT_PASS = %d
`, mountPass))
}

func mpDjangoEndState(t *testing.T, routePath string, routePass, mountPass int) *graph.Document {
	t.Helper()
	repo := t.TempDir()
	mpDjangoWrite(t, repo, routePath, routePass, mountPass)
	return dvFullRebuild(t, repo, t.TempDir())
}

// TestMountParity_6461_Django_RouteEdit_PathA edits ONLY the Django route file
// and measures the daemon path against a clean full rebuild.
func TestMountParity_6461_Django_RouteEdit_PathA(t *testing.T) {
	mp6461Gate(t, "Django direction ROUTE, path A (daemon extractors.TryIncremental)", mp6461Cause)
	repo := t.TempDir()
	stateDir := t.TempDir()
	mpDjangoWrite(t, repo, "terms/", 0, 0)
	dvFullRebuild(t, repo, stateDir)

	full := mpDjangoEndState(t, "terms/", 1, 0)

	dvSeedManifest(t, repo, stateDir)
	mpDjangoWrite(t, repo, "terms/", 1, 0) // route file + its views change; mount file does not
	inc := dvIncremental(t, repo, stateDir)

	mpLogEndpointDelta(t, "DJANGO-ROUTE/path A", full, inc)
	cpAssertParity(t, "#6461 Django direction ROUTE, path A (extractors.TryIncremental)",
		full, inc, mpKnown)
}

// TestMountParity_6461_Django_MountEdit_PathA edits ONLY the Django mount file.
func TestMountParity_6461_Django_MountEdit_PathA(t *testing.T) {
	mp6461Gate(t, "Django direction MOUNT, path A (daemon extractors.TryIncremental)", mp6461Cause)
	repo := t.TempDir()
	stateDir := t.TempDir()
	mpDjangoWrite(t, repo, "terms/", 0, 0)
	dvFullRebuild(t, repo, stateDir)

	full := mpDjangoEndState(t, "terms/", 0, 1)

	dvSeedManifest(t, repo, stateDir)
	mpDjangoWrite(t, repo, "terms/", 0, 1)
	inc := dvIncremental(t, repo, stateDir)

	mpLogEndpointDelta(t, "DJANGO-MOUNT/path A", full, inc)
	cpAssertParity(t, "#6461 Django direction MOUNT, path A (extractors.TryIncremental)",
		full, inc, mpKnown)
}

// TestMountParity_6461_Django_RoutePathRename_PathA is the issue's HEADLINE
// shape: the GHOST.
//
// It renames the route PATH by editing `mpsite/urls.py` and NOTHING else. The
// composed endpoint a full rebuild produces is attributed to `mpsite/views.py`
// — the handler's file, which did NOT change — so the daemon's SourceFile-only
// prune leaves the baseline's composed endpoint in place, and no composition
// pass re-derives the correct one. #6461 predicts a stale composed endpoint
// surviving as a ghost alongside whatever the fresh extraction produced.
func TestMountParity_6461_Django_RoutePathRename_PathA(t *testing.T) {
	mp6461Gate(t, "Django route-path RENAME, path A (daemon extractors.TryIncremental) — the GHOST shape", mp6461Cause)
	repo := t.TempDir()
	stateDir := t.TempDir()
	mpDjangoWrite(t, repo, "terms/", 0, 0)
	dvFullRebuild(t, repo, stateDir)

	full := mpDjangoEndState(t, "conditions/", 0, 0)

	dvSeedManifest(t, repo, stateDir)
	mpDjangoRouteUrls(t, repo, "conditions/", 0) // ONLY the route urls.py changes
	inc := dvIncremental(t, repo, stateDir)

	mpLogEndpointDelta(t, "DJANGO-RENAME/path A", full, inc)
	cpAssertParity(t, "#6461 Django route-path RENAME, path A (extractors.TryIncremental)",
		full, inc, mpKnown)
}

// TestMountParity_6461_Django_RoutePathRename_PathA_EndpointCensus is the
// UNGATED ratchet for #6461's GHOST, and it is deliberately NARROWER than the
// gated full-parity test directly above it.
//
// It drives the SAME fixture and the SAME edit — rename the route path by
// editing `mpsite/urls.py` and nothing else — and then asserts ONLY the
// endpoint/mount census: the set of `http_endpoint*` / `url_mount` entities,
// keyed by kind|name@source_file plus `path`. That set is exactly where the
// ghost lives. `ApplyDjangoNestedURLConf` composes `/network/<route>` and the
// handler-resolution pass rebinds the result onto `mpsite/views.py`
// (`internal/engine/http_endpoint_resolve.go` bridgeEndpointToHandler, #2678),
// which is NOT the file that changed — so a `SourceFile`-only prune carries the
// pre-edit composition forward verbatim and no pass re-derives the new one.
//
// MEASURED BEFORE THE FIX (in the DEFAULT suite, GRAFEL_TEST_6461 unset):
//
//	LOST     http_endpoint_definition|http:ANY:/network/conditions@mpsite/views.py
//	INVENTED http_endpoint_definition|http:ANY:/network/terms@mpsite/views.py
//
// WHY NOT SIMPLY UN-GATE THE FULL-PARITY TEST ABOVE:
// that test is red on this fixture for THREE independent reasons, only one of
// which is #6461's ghost. The other two are measured and are NOT addressed
// here:
//
//   - `[ENTITY-LOST] SCOPE.Operation|GET /conditions||mpsite/urls.py` — an
//     entity missing from the file that DID change and WAS re-extracted. It is
//     emitted by a CROSS extractor (`internal/extractors/cross/endpoint`), and
//     `TryIncremental` runs no cross extractors at all (see the note in
//     `entityRecordToGraphEntity`: "TryIncremental runs no cross extractors at
//     all"). This is the second, uninvestigated defect the #6461 thread flags.
//   - the `IMPORTS` edge out of `mpsite/urls.py` binds to `Module/mpsite.views`
//     on the incremental path and to `SCOPE.Component/mpsite/views.py` on a full
//     rebuild — a scoped-resolver binding difference, unrelated to composition.
//
// Folding those into this ratchet would make it fail for reasons the fix it
// guards cannot control, so the census assertion is the honest scope. The full
// comparator stays gated behind GRAFEL_TEST_6461 until those two are fixed.
func TestMountParity_6461_Django_RoutePathRename_PathA_EndpointCensus(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	mpDjangoWrite(t, repo, "terms/", 0, 0)
	dvFullRebuild(t, repo, stateDir)

	full := mpDjangoEndState(t, "conditions/", 0, 0)

	dvSeedManifest(t, repo, stateDir)
	mpDjangoRouteUrls(t, repo, "conditions/", 0) // ONLY the route urls.py changes
	inc := dvIncremental(t, repo, stateDir)

	mpLogEndpointDelta(t, "DJANGO-RENAME/path A (census)", full, inc)

	lost, invented := mpEndpointDelta(full, inc)
	if len(lost) != 0 || len(invented) != 0 {
		t.Fatalf("#6461 GHOST: the daemon incremental endpoint census diverges from a clean "+
			"full rebuild after renaming a route path in mpsite/urls.py — %d lost %v, %d invented %v. "+
			"An INVENTED entry carrying the PRE-edit path is the ghost itself: the composed "+
			"endpoint is attributed to mpsite/views.py, which did not change, so the "+
			"SourceFile-only prune in internal/extractors/incremental.go left it in place and "+
			"no cross-file composition pass re-derived the correct one.",
			len(lost), lost, len(invented), invented)
	}
}

// ─────────── #6528 — DRF coverage, BOTH layouts (the regression this
// pass created, and the layout that never had it) ───────────
//
// `dropDRFCovered` in internal/extractors/incremental_django_compose.go decides
// whether a composed ANY endpoint is superseded by per-verb
// `drf_router_expanded` entries by reading those entries out of the GRAPH,
// because `ApplyDjangoDRFRoutes` has exactly one caller (cmd/grafel/index.go)
// and re-running it per tick would mean reading every .py file in the repo.
//
// That is sound only while the entries are still IN the graph, and there is one
// layout where they are not. Both are exercised here, in ONE test, because a
// fix that suppressed composition everywhere would pass a test that only
// covered the reproducing layout:
//
//	ORDINARY  — ViewSet in views.py. `bridgeEndpointToHandler` re-attributes
//	  every drf_router_expanded entity onto views.py, which the urls.py edit
//	  does not touch, so the coverage survives Step 5 and dropDRFCovered
//	  reaches the right verdict unaided. MEASURED lost=0 invented=0 both
//	  before and after the #6528 fix — it must STAY that way.
//
//	SAME-FILE — ViewSet declared in the edited urls.py. Those entities are
//	  attributed to that file, Step 5 prunes them, nothing re-derives them, and
//	  coverage reads ABSENT when it is merely INVISIBLE. MEASURED lost=6
//	  invented=1 (`http:ANY:/api/widgets`) — the 1 INVENTED introduced purely by
//	  the composition pass, confirmed against the same fixture with the pass
//	  disabled (lost=6 invented=0). After the fix: lost=6 invented=0.
//
// `lost=6` in the SAME-FILE case is asserted as-is rather than fixed: it is
// #6529 (drf_router_expanded entities are never re-derived on the incremental
// path), it is identical with this pass disabled, and papering over it here
// would hide a defect that belongs to a different change.
func mpDRFOrdinary(t *testing.T, repo string, pass int) {
	t.Helper()
	dvWriteFile(t, repo, "mpdrfapp/views.py", `from rest_framework import viewsets


class WidgetViewSet(viewsets.ModelViewSet):
    queryset = []
`)
	dvWriteFile(t, repo, "mpdrfapp/urls.py", fmt.Sprintf(`from rest_framework import routers

from . import views

router = routers.DefaultRouter()
router.register(r"widgets", views.WidgetViewSet)

urlpatterns = router.urls

MP_DRF_PASS = %d
`, pass))
	dvWriteFile(t, repo, "mpdrfproj/urls.py", `from django.urls import include, path

urlpatterns = [
    path("api/", include("mpdrfapp.urls")),
]
`)
}

func mpDRFSameFile(t *testing.T, repo string, pass int) {
	t.Helper()
	dvWriteFile(t, repo, "mpsameapp/urls.py", fmt.Sprintf(`from rest_framework import routers, viewsets


class WidgetViewSet(viewsets.ModelViewSet):
    queryset = []


router = routers.DefaultRouter()
router.register(r"widgets", WidgetViewSet)

urlpatterns = router.urls

MP_SAME_PASS = %d
`, pass))
	dvWriteFile(t, repo, "mpsameproj/urls.py", `from django.urls import include, path

urlpatterns = [
    path("api/", include("mpsameapp.urls")),
]
`)
}

func TestMountParity_6461_DRFCoverage_PathA_BothLayouts(t *testing.T) {
	cases := []struct {
		label    string
		write    func(*testing.T, string, int)
		wantLost int // #6529, pre-existing, asserted not fixed
	}{
		{"ordinary (ViewSet in views.py)", mpDRFOrdinary, 0},
		{"same-file (ViewSet in the edited urls.py)", mpDRFSameFile, 6},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			repo := t.TempDir()
			stateDir := t.TempDir()
			tc.write(t, repo, 0)
			dvFullRebuild(t, repo, stateDir)

			endRepo := t.TempDir()
			tc.write(t, endRepo, 1)
			full := dvFullRebuild(t, endRepo, t.TempDir())

			dvSeedManifest(t, repo, stateDir)
			tc.write(t, repo, 1) // only the DRF urls.py content changes
			inc := dvIncremental(t, repo, stateDir)

			mpLogEndpointDelta(t, "DRF/"+tc.label, full, inc)
			lost, invented := mpEndpointDelta(full, inc)

			// THE ASSERTION THIS TEST EXISTS FOR. An INVENTED endpoint is one
			// the daemon put in the graph that a full rebuild does not have —
			// here, a composed ANY that DeduplicateNestedURLConfDRF removes on
			// the full path. It must be zero in BOTH layouts.
			if len(invented) != 0 {
				t.Errorf("#6528 %s: the incremental graph INVENTED %d endpoint(s) a clean full "+
					"rebuild does not have: %v. A composed ANY endpoint survives here only because "+
					"the pass could not see the drf_router_expanded coverage that supersedes it — "+
					"see drfUnknownCoveragePaths, which must abstain from composing under a "+
					"router.register() prefix declared in a file that changed this tick.",
					tc.label, len(invented), invented)
			}
			if len(lost) != tc.wantLost {
				t.Errorf("#6529 %s: expected exactly %d LOST endpoint(s) (the pre-existing "+
					"never-re-derived drf_router_expanded entities, identical with the composition "+
					"pass disabled), got %d: %v. A DIFFERENT number means either that defect moved "+
					"or the #6528 abstention over-reached and started suppressing live routes.",
					tc.label, tc.wantLost, len(lost), lost)
			}
		})
	}
}
