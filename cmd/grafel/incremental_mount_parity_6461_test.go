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
//	ROUTE / path B   RED after the #6469 route-path RENAME (below); it was GREEN
//	                   with the earlier pass-counter-only fixture, which is
//	                   precisely why that fixture could not see a composition
//	                   defect. 1 divergence, edges full=54 inc=55:
//	                   [EDGE-INVENTED] SCOPE.Process/http:GET:/conditions
//	                     → «unbound»proc:0c089ba065f57543 :RENAMED_FROM
//	                   — a rename artefact of the incremental path, unrelated to
//	                   cross-file composition.
//	MOUNT / path A   RED.    1 divergence, edges full=54 inc=53:
//	                   [EDGE-LOST] Service/mp_app@mpmain_mount.py
//	                     → Route/mp_router@mpmarkets_route.py :ROUTES_TO
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
// Two pairs run by default: ROUTE/path A and MOUNT/path B. So the
// ungated ratchet covers Path A for the ROUTE direction ONLY — the MOUNT
// direction's Path A (`TestMountParity_6461_MountEdit_PathA`) is gated behind
// GRAFEL_TEST_6461 because it reproduces #6461 today, which means a regression
// that only drops entities attributed to the unchanged ROUTE file when the
// MOUNT file is edited is NOT caught by the default suite. ROUTE/path B is
// gated too, for a DIFFERENT and newly-measured reason (a RENAMED_FROM edge the
// incremental path invents — see the note on that test). Setting the env var
// runs the full seven.
//
// Refs #6461, #6414, #6415, #6385, #6129.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

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

// mpLogEndpointDelta logs the endpoint census on both sides plus the symmetric
// difference, so the measured entity delta is in the test output whether the
// gate passes or fails.
func mpLogEndpointDelta(t *testing.T, label string, full, inc *graph.Document) {
	t.Helper()
	fa, ib := mpEndpointSet(full), mpEndpointSet(inc)
	inFull := map[string]int{}
	for _, s := range fa {
		inFull[s]++
	}
	inInc := map[string]int{}
	for _, s := range ib {
		inInc[s]++
	}
	var lost, invented []string
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

// mp6461Env is the switch that makes the #6461 gate run. The branch must not
// land a red suite, so any direction/path pair that currently reproduces the
// defect SKIPS with an explicit issue reference unless this is set.
//
// Reproduce on demand with:
//
//	GOMAXPROCS=4 GRAFEL_TEST_6461=1 go test ./cmd/grafel/ -count=1 \
//	    -run 'TestMountParity_6461' -v
const mp6461Env = "GRAFEL_TEST_6461"

// mp6461Gate skips unless mp6461Env is set, naming the issue and the exact
// command that reproduces. Applied ONLY to pairs measured to reproduce.
func mp6461Gate(t *testing.T, what string) {
	t.Helper()
	if os.Getenv(mp6461Env) == "" {
		t.Skipf("#6461 UNFIXED — %s. This assertion is expected to FAIL: the daemon "+
			"fast path prunes only by SourceFile and runs no cross-file composition "+
			"pass. Reproduce with: %s=1 go test ./cmd/grafel/ -count=1 "+
			"-run 'TestMountParity_6461' -v", what, mp6461Env)
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
	// MEASURED RED once the route path is RENAMED rather than merely re-passed
	// (#6469 review). The divergence is NOT #6461 and NOT the composition
	// concern this file is about — measured, verbatim:
	//
	//	edges full=54 inc=55 | entities full=28 inc=28
	//	[EDGE-INVENTED] SCOPE.Process/http:GET:/conditions → mp_helper@...
	//	  →«unbound»proc:0c089ba065f57543 :RENAMED_FROM
	//
	// i.e. the Path-B incremental run detects the endpoint's rename and emits a
	// RENAMED_FROM edge pointing at the OLD process id, which a clean full
	// rebuild of the end state has no counterpart for. Deterministic: 3/3 runs.
	// Gated rather than allow-listed so the assertion stays whole, and reported
	// separately as its own defect.
	mp6461Gate(t, "direction ROUTE, path B (Index + WithIncremental) — a RENAMED_FROM edge "+
		"the incremental run invents for the renamed endpoint's process, absent from the full rebuild")
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

// ─────────────────────── direction MOUNT ───────────────────────
//
// Edit ONLY the mount file. Everything attributed to it is pruned; nothing on
// the daemon path re-derives what a full rebuild composes across the pair, so
// composed endpoints VANISH rather than going stale.

func TestMountParity_6461_MountEdit_PathA(t *testing.T) {
	mp6461Gate(t, "direction MOUNT, path A (daemon extractors.TryIncremental)")
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
	mp6461Gate(t, "Django direction ROUTE, path A (daemon extractors.TryIncremental)")
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
	mp6461Gate(t, "Django direction MOUNT, path A (daemon extractors.TryIncremental)")
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
	mp6461Gate(t, "Django route-path RENAME, path A (daemon extractors.TryIncremental) — the GHOST shape")
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
