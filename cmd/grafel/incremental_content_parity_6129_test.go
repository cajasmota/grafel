// Package main — incremental_content_parity_6129_test.go
//
// FULL-vs-INCREMENTAL CONTENT PARITY GATE (#6129, building on #6037).
//
// Why this file exists
// ────────────────────
// Four defects in quick succession shared one shape: an incremental run
// producing edges that resolve to a DIFFERENT OR SPURIOUS TARGET than a full
// rebuild, rather than producing FEWER edges.
//
//	#6037 — the parity comparator compared sets, not multisets, so a
//	        destroy-and-re-add was invisible.
//	#6094 — a one-directional comparison under-scoped the defect: the rows were
//	        present, but pointing at a name.
//	#6123 — a mis-bind IMPROVES the dangling-endpoint metric while making the
//	        graph wrong.
//	#6129 — Path A binds IMPORTS to SCOPE.External placeholders instead of the
//	        real in-repo Module entities (since FIXED — see the allow-list);
//	        Path B emits spurious DEPENDS_ON rows.
//
// Every metric used in this area historically — entity counts, relationship
// counts, dangling-endpoint rates, one-directional diffs — reports that shape
// as HEALTHY. At four instances the answer is an instrument, not a fifth point
// fix. This is that instrument.
//
// What it asserts
// ───────────────
// For each incremental path, over a fixture corpus with a one-file delta:
//
//	graph(incremental over baseline → end-state)  ≡
//	graph(clean full rebuild of the same end-state)
//
// compared as a BIDIRECTIONAL MULTISET on BOUND-TARGET CONTENT.
//
// Design decisions, and why each one is load-bearing:
//
//   - CONTENT, not ids (`parity.Options.ContentKeyedIdentity`, added for this).
//     An edge endpoint is keyed by the `kind/name@source_file` tuple of the
//     entity it binds to IN ITS OWN GRAPH, or by `«unbound»<raw>` when it binds
//     to nothing. An edge that resolves to a different entity therefore appears
//     as a LOST key and an INVENTED key naming both targets. Keying on hex ids
//     would report the same thing opaquely, and would not distinguish a
//     placeholder endpoint from a real one at all.
//
//   - BIDIRECTIONAL MULTISETS. LOST and INVENTED are reported separately and
//     with multiplicity. `internal/graph/parity` was fixed for exactly this in
//     #6037 and is REUSED here rather than reimplemented; the only extension is
//     the content keying above (a new Options field — no existing behaviour
//     changed, the default path still keys on ids).
//
//   - BOTH PATHS, NOT CONFLATED. `dvIncremental` drives Path A
//     (`internal/extractors.TryIncremental`, the daemon path — what MCP callers
//     see between full reindexes). `cfPathBIncremental` (#6128) drives Path B
//     (a SECOND `Index(..., WithIncremental(stateDir))` over a populated state
//     dir — the CLI path). They are DIFFERENT code with DIFFERENT defects;
//     using the wrong harness manufactures a divergence that belongs to the
//     other path. Each path gets its own test and its own allow-list.
//
//   - THE RUN MUST ACTUALLY BE INCREMENTAL. Both harnesses already fail loudly
//     on a fallback to full reindex (Path A on `res.Done`; Path B on the
//     captured "falling back to full reindex" / "incremental — processing"
//     stderr lines), which is the only thing standing between this gate and
//     total vacuity: a full-reindex fallback trivially satisfies full-vs-full
//     parity. The fixture keeps EVERY BASENAME UNIQUE because `diff.Filter`
//     cross-invalidates by basename, which turns a 1-file delta into an N-file
//     change and trips the too-many-changed fallback.
//
//   - GRAPHS ARE READ BACK FROM DISK via `LoadGraphFromDir`. Never from
//     run-log counts (#6094 was under-scoped by trusting those) and never by
//     comparing raw `graph.fb` bytes (indexing is nondeterministic at byte
//     level — #6083).
//
// Known-divergence allow-list
// ───────────────────────────
// The divergences characterised below are REAL. This gate landing red on them
// would be correct but useless as a ratchet, so each is characterised
// explicitly by its exact shape, issue number and reason. Anything else —
// a new divergence, or an allow-listed one that stops reproducing — fails.
// The comparison itself is NOT weakened: no tolerance profile, no ignored edge
// kind, no ignored property.
//
// The ratchet has since paid for itself. #6129's headline Path-A defect was
// fixed in internal/extractors/sresolver, and this gate — not the fixing
// change's own tests — is what established the exact scope of that fix: three
// allow entries went stale (both halves of the IMPORTS mis-bind and the
// `DEPENDS_ON → _external` weight over-count, which turned out to be a
// CONSEQUENCE of the mis-bind rather than a second defect), while a fourth was
// re-keyed rather than deleted because the fix moved its bound target without
// closing it. A fix's own tests could not have drawn that boundary.
//
// WHAT THIS GATE CANNOT SEE
// ─────────────────────────
// It is relied on as though it were total. It is not, and the list below is
// concrete rather than a disclaimer — each item has been demonstrated, not
// supposed. Read it before concluding "the gate is green, therefore the two
// paths agree".
//
//   - ENTITY IDS. Keying on CONTENT is the design (see above) and it means two
//     graphs whose entities carry DIFFERENT ids compare equal. That is not
//     hypothetical: the incremental path emitted an endpoint whose id was the
//     literal string "http:GET:/cpthings" where a full rebuild had a hex, and
//     this gate reported nothing. It surfaced only because the flow pass
//     embeds the id as TEXT in entry_id / chain / branches_dag, i.e. by
//     accident. Fixed in #6150 — but the blind spot is structural and remains.
//
//   - CANONICAL SORTEDNESS of the entity slice. parity.Compare is multiset-
//     based, so ORDER is invisible to it — while graph.fb's `(key)` binary
//     search behind LookupEntityByID requires ids in canonical order (#5974).
//     A producer that emits a correct set in the wrong order passes here and
//     silently breaks lookup. No gate covers this today.
//
//   - RelationshipID. Never compared; `relKey` is (endpoints, kind). Two graphs
//     whose edges carry different relationship ids compare equal.
//
//   - EVERYTHING ABOVE THE ENTITY/EDGE LEVEL. Document.Stats, SurpriseEdges,
//     AlgorithmStats, IndexerVersion, CoverageStatus and IndexedRef/SHA are not
//     compared at all. Community labels are checked only for entities present
//     on BOTH sides.
//
// Demonstrated, and the reason this section exists: a mutant that split the
// Pass-2.5 stub index key on its first colon — the #6105/#6123 mis-bind shape —
// PASSED this gate with an identical tolerated-divergence list. It was caught
// only by a unit test in internal/extractors. A content-keyed multiset over
// entities and edges is a strong instrument for one class of defect and blind
// to several others; pair it with unit tests at the seam being changed.
//
// Refs #6129, #6037, #6094, #6123, #6083, #6128, #6150, #5974.
package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/parity"
)

// ─────────────────────────── divergence model ───────────────────────────

// cpBucket names the dimension a divergence was found on. LOST (present in the
// full rebuild, absent from the incremental result) and INVENTED (the reverse)
// are kept as SEPARATE buckets: a one-directional comparison is what
// under-scoped #6094, and a mis-bind shows up as one of each.
type cpBucket string

const (
	cpEntityLost      cpBucket = "ENTITY-LOST"     // in full rebuild, not in incremental
	cpEntityInvented  cpBucket = "ENTITY-INVENTED" // in incremental, not in full rebuild
	cpEntityMult      cpBucket = "ENTITY-MULTIPLICITY"
	cpEntityFields    cpBucket = "ENTITY-FIELDS"
	cpEdgeLost        cpBucket = "EDGE-LOST"
	cpEdgeInvented    cpBucket = "EDGE-INVENTED"
	cpEdgeMult        cpBucket = "EDGE-MULTIPLICITY"
	cpEdgeProps       cpBucket = "EDGE-PROPS"
	cpCommunityAssign cpBucket = "COMMUNITY-ASSIGNMENT"
	cpCommunitySet    cpBucket = "COMMUNITY-MEMBERSHIP"
)

// cpDivergence is one row of the flattened parity report.
type cpDivergence struct {
	Bucket cpBucket
	Key    string
	Detail string
}

func (d cpDivergence) String() string {
	if d.Detail == "" {
		return fmt.Sprintf("[%s] %s", d.Bucket, d.Key)
	}
	return fmt.Sprintf("[%s] %s — %s", d.Bucket, d.Key, d.Detail)
}

// cpFlatten turns a parity.Report into a flat, sorted divergence list. Every
// field of the report is covered; a new report field left unhandled here would
// silently drop divergences, so the mapping is exhaustive by construction.
func cpFlatten(r parity.Report) []cpDivergence {
	var out []cpDivergence
	add := func(b cpBucket, keys []string) {
		for _, k := range keys {
			out = append(out, cpDivergence{Bucket: b, Key: k})
		}
	}
	addF := func(b cpBucket, fds []parity.FieldDiff) {
		for _, f := range fds {
			out = append(out, cpDivergence{Bucket: b, Key: f.Key, Detail: f.Detail})
		}
	}
	add(cpEntityLost, r.EntitiesOnlyInA)
	add(cpEntityInvented, r.EntitiesOnlyInB)
	addF(cpEntityMult, r.EntityMultiplicityDiffs)
	addF(cpEntityFields, r.EntityFieldDiffs)
	add(cpEdgeLost, r.RelsOnlyInA)
	add(cpEdgeInvented, r.RelsOnlyInB)
	addF(cpEdgeMult, r.RelMultiplicityDiffs)
	addF(cpEdgeProps, r.RelPropDiffs)
	addF(cpCommunityAssign, r.CommunityAssignmentDiffs)
	add(cpCommunitySet, r.CommunitySetDiff)

	sort.Slice(out, func(i, j int) bool {
		if out[i].Bucket != out[j].Bucket {
			return out[i].Bucket < out[j].Bucket
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// ───────────────────────────── allow-list ─────────────────────────────

// cpKnown characterises ONE known-and-unfixed divergence shape. It is keyed to
// the specific shape — bucket plus every substring that must appear in the
// divergence key — so it cannot accidentally absorb an unrelated regression in
// the same bucket. Issue and Why are mandatory: an allow entry without a filed
// issue and a stated reason is an ignored bug.
type cpKnown struct {
	Issue  string   // the issue tracking the divergence
	Why    string   // why it is tolerated TODAY (never "to make the test pass")
	Bucket cpBucket // the dimension it was found on

	// Contains: ALL must be substrings of the divergence key.
	Contains []string
	// DetailContains: ALL must be substrings of the divergence detail. Used on
	// the multiplicity / property buckets so that a CHANGE in the magnitude of a
	// known divergence (a row duplicated 3× instead of 2×, a weight drifting
	// from 5 to 9) is a NEW divergence and fails, rather than being absorbed.
	DetailContains []string
}

func (k cpKnown) matches(d cpDivergence) bool {
	if d.Bucket != k.Bucket {
		return false
	}
	for _, s := range k.Contains {
		if !strings.Contains(d.Key, s) {
			return false
		}
	}
	for _, s := range k.DetailContains {
		if !strings.Contains(d.Detail, s) {
			return false
		}
	}
	return true
}

// cpAssertParity is the gate. It compares the two graphs bidirectionally as
// content-keyed multisets, then partitions the divergences into
// known-and-allow-listed vs everything else.
//
// Three ways it fails, all deliberate:
//
//  1. an UNEXPECTED divergence — a new full-vs-incremental defect, or an
//     existing one changing shape;
//  2. a STALE allow entry that matched nothing — the divergence was fixed (or
//     the fixture stopped reaching it) and the entry must be deleted, so the
//     allow-list can only ever shrink;
//  3. an INERT comparison — the graphs carry no edges at all, which would make
//     "equivalent" meaningless.
func cpAssertParity(t *testing.T, path string, full, inc *graph.Document, known []cpKnown) {
	t.Helper()

	// (3) Inertness guard. A comparison of two empty (or edge-free) graphs is
	// trivially equivalent and proves nothing.
	if len(full.Entities) == 0 || len(full.Relationships) == 0 {
		t.Fatalf("%s: fixture is inert — the full rebuild produced %d entities / %d edges; "+
			"a parity comparison over that measures nothing",
			path, len(full.Entities), len(full.Relationships))
	}

	// The two aggregate metrics this class of defect historically hid behind,
	// logged on every run so the failure output shows for itself that they are
	// NOT sufficient: a mis-bind moves neither of them.
	t.Logf("%s: edges full=%d inc=%d | entities full=%d inc=%d | unbound endpoints full=%d inc=%d",
		path, len(full.Relationships), len(inc.Relationships),
		len(full.Entities), len(inc.Entities),
		cpUnboundEndpoints(full), cpUnboundEndpoints(inc))

	rep := parity.CompareWithOptions(full, inc, parity.Options{ContentKeyedIdentity: true})
	divs := cpFlatten(rep)

	hits := make([]int, len(known))
	var unexpected []cpDivergence
	for _, d := range divs {
		matched := false
		for i, k := range known {
			if k.matches(d) {
				hits[i]++
				matched = true
				break
			}
		}
		if !matched {
			unexpected = append(unexpected, d)
		}
	}

	// (1) New / reshaped divergence.
	if len(unexpected) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%s: %d full-vs-incremental divergence(s) NOT on the known list.\n\n", path, len(unexpected))
		b.WriteString("A = full rebuild of the end state (reference); B = incremental result.\n")
		b.WriteString("Endpoints are keyed by the CONTENT of the entity they bind to, so an edge\n")
		b.WriteString("that resolved to a DIFFERENT target appears as one EDGE-LOST plus one\n")
		b.WriteString("EDGE-INVENTED naming both targets — the shape counts and dangling rates miss.\n\n")
		for _, d := range unexpected {
			fmt.Fprintf(&b, "  %s\n", d)
		}
		if n := len(divs) - len(unexpected); n > 0 {
			fmt.Fprintf(&b, "\n(%d further divergence(s) matched the known-issue allow-list and are not shown.)\n", n)
		}
		t.Errorf("%s", b.String())
	}

	// (2) Stale allow entry — the ratchet.
	for i, k := range known {
		if hits[i] == 0 {
			t.Errorf("%s: allow-list entry for %s (%v %v) matched NOTHING.\n"+
				"  reason it was allowed: %s\n"+
				"Either the divergence was fixed — delete this entry, the allow-list only shrinks — "+
				"or the fixture no longer reaches it, in which case the gate has gone quiet on a live defect.",
				path, k.Issue, k.Bucket, k.Contains, k.Why)
		}
	}

	if !t.Failed() && len(divs) > 0 {
		t.Logf("%s: %d known divergence(s) tolerated (all filed):", path, len(divs))
		for _, d := range divs {
			t.Logf("    %s", d)
		}
	}
}

// cpUnboundEndpoints counts relationship endpoints that bind to no entity in
// their own document — the "dangling rate" numerator. It exists here purely to
// be LOGGED alongside the real comparison, as standing evidence of why it is
// not a sufficient gate: an edge rebound to a different EXISTING entity leaves
// this number untouched, and a mis-bind onto a placeholder that does exist can
// even IMPROVE it (#6123).
func cpUnboundEndpoints(d *graph.Document) int {
	live := make(map[string]bool, len(d.Entities))
	for _, e := range d.Entities {
		live[e.ID] = true
	}
	n := 0
	for _, r := range d.Relationships {
		if !live[r.FromID] {
			n++
		}
		if !live[r.ToID] {
			n++
		}
	}
	return n
}

// ────────────────────────────── fixture ──────────────────────────────
//
// A small Python corpus with cross-file imports — the construct #6129's Path-A
// divergence rides on (IMPORTS edges that must bind to in-repo Module entities
// rather than SCOPE.External placeholders), and which also drives the
// module-aggregation DEPENDS_ON edges Path B gets wrong.
//
// EVERY BASENAME IS UNIQUE across the corpus. diff.Filter cross-invalidates
// files sharing a basename, so a repeated one would drag the "unchanged" files
// into the changed set, trip the too-many-changed guard, and silently turn the
// whole gate into a full-vs-full comparison.

// cpStatic writes the files that are NEVER touched between the baseline and the
// incremental pass. They must stay out of the changed set for the run to be
// genuinely incremental.
func cpStatic(t *testing.T, repo string) {
	t.Helper()
	dvWriteFile(t, repo, "cperrs_static.py", `class CpNotFound(Exception):
    pass


def cp_raise(x):
    raise CpNotFound("nope")
`)
	dvWriteFile(t, repo, "cpprod_static.py", `def cp_target(x):
    return x * 3


class CpProducer:
    def cp_method(self, y):
        return y + 1
`)
	dvWriteFile(t, repo, "cpcfg_static.py", `CP_SETTING = "abc"
CP_OTHER = 12
`)
	// #6141 — the leaf-name tier shape. `CpSibling.cp_owner` is an OPERATION
	// in a SIBLING file of the same package directory (pkgDirOf returns "."
	// for every repo-root file, so the package-scoped tier is live here).
	// The delta file below declares a same-leaf-named FIELD in the CALLER's
	// OWN file (CpLeafBag.cp_owner) plus a bare `cp_owner()` call.
	//
	// That triple is what separates the two possible tier orderings, and
	// nothing in this corpus reached it before: every other fixture puts the
	// competing member in a different file from the caller.
	//
	//	within-tier  (fileOp, fileAny, pkgOp, pkgAny) -> the caller-file FIELD
	//	across-tiers (fileOp, pkgOp, fileAny, pkgAny) -> the sibling OPERATION
	//
	// internal/resolve does the former. When internal/extractors/sresolver
	// did the latter, a full rebuild and an incremental run bound this call
	// to DIFFERENT entities — and this gate passed anyway, because the shape
	// was absent. It is here so it cannot pass blind again.
	dvWriteFile(t, repo, "cpleaf_static.py", `class CpSibling:
    def cp_owner(self):
        return 1
`)
	// #6150 — the CROSS-FILE handler half of the endpoint shape. The route is
	// registered in cproutes_delta.js (the delta); the handler it names lives
	// HERE. Every framework in the corpus that dispatches from a different
	// module than it implements in — Express + imported controller, Flask
	// add_url_rule + imported function, DRF router + ViewSet — has this shape,
	// and the Falcon delta cannot reach it because a Falcon responder is a
	// method on the class the route registers.
	//
	// It is the shape that separates "unresolved" from "DELETED". Per-file
	// Pass 2.5 stamps `source_handler="Controller:cpListUsers"` on the
	// synthetic; engine.ResolveHTTPEndpointHandlers DROPS a synthetic whose
	// source_handler resolves to nothing in the slice it is handed
	// (`stats.HandlerDropped++`), and a file-scoped call makes "no candidate in
	// the corpus" and "no candidate in this file" the same condition. Running
	// that pass file-scoped without a keep guard therefore destroys every
	// cross-file endpoint on the incremental path — strictly worse than not
	// running it at all, and invisible to a Falcon-only fixture.
	dvWriteFile(t, repo, "cpctl_static.js", `function cpListUsers(req, res) {
  return res.json({ ok: 1 });
}

module.exports = { cpListUsers };
`)
	// #6150 — the CROSS-FILE REBIND shape, and the one that separates the two
	// wrong answers to an unresolvable endpoint synthetic.
	//
	// cpwsgi_delta.py registers `/cpview` against a view IMPORTED from here. A
	// full rebuild resolves that handler corpus-wide and `bridgeEndpointToHandler`
	// REBINDS the endpoint's source_file onto the handler's body — so the
	// endpoint ends up anchored HERE, in an unchanged file, and survives the
	// delta untouched. The re-extracted registration file then contributes a
	// SECOND, unresolved copy at its own coordinates unless something notices.
	//
	// Measured: full rebuild ONE endpoint at cpview_static.py; incremental TWO.
	// That duplicate predates #6150 — it reproduces identically with the
	// endpoint-resolve pass disabled — and it is what
	// pruneSupersededEndpointSynthetics closes. Nothing in this corpus reached
	// it before, because Falcon responders are methods on the class the route
	// registers and Express handlers here resolve to a local binding.
	dvWriteFile(t, repo, "cpview_static.py", `def cp_view_handler(req):
    return {"ok": 1}
`)
}

// cpDelta writes the SINGLE file that differs between the baseline pass
// (pass 0) and the end state (pass 1). Its imports reach into every static file
// above, so the delta's blast radius genuinely crosses file boundaries.
//
// Two of its declarations exist only to reach a shape this gate was blind to.
// Both are in the DELTA file on purpose — the incremental path re-extracts only
// these, so a construct placed in a static file is carried forward from the
// baseline full rebuild and compares equal no matter what the re-extraction
// would have made of it. That is precisely why both defects below hid here:
//
//   - CpLeafBag.cp_owner + cp_leaf_call — the #6141 leaf-name tier triple. See
//     the cpleaf_static.py block in cpStatic for what it separates. The field
//     must be a CLASS-LEVEL annotated attribute: `self.cp_owner = 1` in
//     __init__ emits no field entity at all, so the competing member would be
//     absent and the shape only apparently reached.
//
//   - CpPlainProbe — #6148. A class with no decorator, no base class, no route
//     annotation and no naming convention: nothing classification-worthy. The
//     full rebuild still TYPES it, because Pass 2.5 runs the YAML rule sets
//     (falcon/cherrypy match a bare `class X:` with no framework gate at all —
//     #6152) and the #1613 fold collapses the
//     AST's generic SCOPE.Component node into that typed record. The incremental
//     path ran the language extractor alone and left the generic kind, so the
//     same class was `Controller` after a full rebuild and `SCOPE.Component`
//     after an incremental one — a one-entity divergence that dragged four
//     CONTAINS edges with it, since entity ids hash the kind. Fixed in
//     internal/extractors/incremental.go (see engine.FoldFrameworkClassKinds).
//     No class had ever appeared in this file's delta, which is the whole reason
//     the gate stayed green through it.
//
//   - `import falcon` + `cp_app = falcon.App()` + `cp_app.add_route(...)` +
//     the `on_get` responder — #6150. This is the FULL Falcon registration
//     shape, and every part of it is load-bearing; the fixture reached a
//     weaker version of it and the gate went quiet on six entities.
//
//     `on_get` (not some inert method name) is what makes the responder a
//     ROUTE and gives the endpoint a verb. `add_route` is what fires falcon's
//     YAML `relationship_rules` — the ONLY thing in this corpus that produces
//     a Pass-2.5 STANDALONE relationship, which is a different producer from
//     the record-embedded edges everything else here exercises. Together they
//     are what make Pass 2.5 emit an `http_endpoint_definition`, which is what
//     the flow pass needs to build the `SCOPE.Process` node and its four
//     dependent edges. `import falcon` reaches the third-party-import path
//     (#6156).
//
//     Before #6150 this shape diverged TWELVE ways from a full rebuild with at
//     least six distinct causes, and the previous fixture could not see any of
//     them: naming the probe method something inert and omitting the app
//     object is functionally an allow-list entry with no filed issue, and the
//     stale-entry ratchet below structurally CANNOT detect that dodge. Do not
//     weaken any of these four lines.
func cpDelta(t *testing.T, repo string, pass int) {
	t.Helper()
	dvWriteFile(t, repo, "cphandler_delta.py", fmt.Sprintf(`import falcon
import cperrs_static
import cpprod_static
from cpprod_static import CpProducer
from cpcfg_static import CP_SETTING


def cp_handle(x):
    try:
        return cperrs_static.cp_raise(x) + %d
    except cperrs_static.CpNotFound:
        return cpprod_static.cp_target(x)


def cp_use_class(y):
    c = CpProducer()
    return c.cp_method(y) + len(CP_SETTING)


class CpPlainProbe:
    def on_get(self, req, resp):
        return 1


cp_app = falcon.App()
cp_app.add_route("/cpthings", CpPlainProbe())


class CpLeafBag:
    cp_owner: int = 1


def cp_leaf_call():
    return cp_owner()
`, pass))

	// The SECOND delta file (#6150). Express router whose handler is
	// `require`d from cpctl_static.js — see the cpctl_static.js block in
	// cpStatic for why this shape has to be in the DELTA (a construct in a
	// static file is carried forward from the baseline full rebuild and
	// compares equal no matter what re-extraction would have made of it).
	//
	// Two changed files still runs incremental: the too-many-changed guard's
	// limit is far above 2, and dvIncremental fails the test if the run falls
	// back regardless, so this cannot silently turn the gate vacuous.
	// THIRD delta file (#6150) — the cross-file REBIND shape. See the
	// cpview_static.py block in cpStatic for what it separates and why the
	// registration has to be in the delta.
	dvWriteFile(t, repo, "cpwsgi_delta.py", fmt.Sprintf(`from flask import Flask
from cpview_static import cp_view_handler

cp_wsgi = Flask(__name__)
cp_wsgi.add_url_rule("/cpview", "cp_view_handler", cp_view_handler)

CP_WSGI_PASS = %d
`, pass))

	dvWriteFile(t, repo, "cproutes_delta.js", fmt.Sprintf(`const express = require("express");
const { cpListUsers } = require("./cpctl_static");

const cpRouter = express.Router();
cpRouter.get("/cpusers", cpListUsers);

const cpPass = %d;

module.exports = { cpRouter, cpPass };
`, pass))
}

// cpEndState builds a pristine repo holding exactly the end state and returns a
// CLEAN FULL REBUILD of it — the reference side of every comparison. A separate
// repo dir keeps it uncontaminated by the baseline run's state.
func cpEndState(t *testing.T) *graph.Document {
	t.Helper()
	repo := t.TempDir()
	cpStatic(t, repo)
	cpDelta(t, repo, 1)
	return dvFullRebuild(t, repo, t.TempDir())
}

// ──────────────────────────── Path A gate ────────────────────────────

// cpKnownPathA characterises the #6129 Path-A divergences.
//
// Path A is internal/extractors.TryIncremental — the daemon's in-place reindex,
// and therefore what MCP callers see between full reindexes.
//
// Every entry below was OBSERVED, not predicted: the gate was written with an
// empty allow-list, run, and each divergence it reported was characterised.
// None of them is a comparator artefact — the comparison is unweakened and the
// divergence set is reproducible run to run.
//
// Nothing here is fixed by this change. Deliberately: the point of a gate is to
// pin today's state so tomorrow's regression is visible, and #6129 is another
// agent's to take.
var cpKnownPathA = []cpKnown{
	// ── #6129, headline defect: IMPORTS bound to the wrong KIND of target ──
	//
	// FIXED — the two entries that stood here (the INVENTED
	// `…→SCOPE.External/…:IMPORTS` half and the LOST `…→Module/…:IMPORTS` half)
	// went STALE and were removed by the ratchet below.
	//
	// `import cperrs_static` / `import cpprod_static` name modules that EXIST in
	// the repo, and Path A bound each IMPORTS edge to a `SCOPE.External`
	// placeholder instead of the in-repo `Module` — asserting a dependency on a
	// third-party package where the source imports a local one. Root cause: the
	// scoped resolver indexes the PREVIOUS PERSISTED graph, which is
	// post-`external.Synthesize`, so the previous run's `ext:` placeholder
	// competed as an ordinary name candidate and won on last-writer-wins. A full
	// rebuild's index can never hold one, because Synthesize runs AFTER
	// resolution. Fixed by a precedence rank in
	// internal/extractors/sresolver/scoped.go (`externalPlaceholderRank`).
	//
	// This was the pair that motivated the whole gate, and it is worth keeping
	// the reason recorded now that it no longer reproduces: the edge was PRESENT
	// and RESOLVED on both sides, so entity counts, edge counts and
	// dangling-endpoint rates were identical and all reported "healthy". Only
	// comparing the CONTENT of the bound target separated them.

	// ── #6129 family: a from-import left as a verbatim stub ──
	//
	// `from cpcfg_static import CP_SETTING` — the full rebuild binds the IMPORTS
	// edge to cpcfg_static.py's file component; the incremental run leaves the
	// raw "cpcfg_static.CP_SETTING" text as the endpoint. Newly characterised by
	// this gate (not named in #6129's text) and reproduced on BOTH paths, which
	// is why it is listed on both allow-lists rather than folded into one.
	//
	// Unlike the entry above this one is a LOSS, not a mis-bind, so it is the
	// class a dangling-endpoint metric would have caught — it is listed
	// separately precisely so the two classes never get conflated.
	{
		Issue:    "#6129",
		Why:      "from-import of a module constant is left as a verbatim stub by the incremental resolver. Unfixed.",
		Bucket:   cpEdgeInvented,
		Contains: []string{"→«unbound»cpcfg_static.CP_SETTING", ":IMPORTS"},
	},
	{
		Issue:    "#6129",
		Why:      "The LOST half of the same missed bind: the file component the full rebuild binds.",
		Bucket:   cpEdgeLost,
		Contains: []string{"→SCOPE.Component/cpcfg_static.py@cpcfg_static.py", ":IMPORTS"},
	},

	// ── #6131: REFERENCES edges the full run's import-placeholder-prune orphaned ──
	//
	// FIXED — the two entries that stood here (the INVENTED
	// `…→Module/…:REFERENCES` half and the LOST `…→«unbound»<hex>:REFERENCES`
	// half) went STALE and were removed by the ratchet below.
	//
	// The history is worth keeping because this divergence is the reason the
	// ratchet's "re-key, don't delete" rule earned its place: #6129's fix moved
	// what these edges bound to on the incremental side (SCOPE.External → the
	// real in-repo Module) WITHOUT closing the divergence, so the entries were
	// updated in place and kept covering a live defect.
	//
	// It was also the one case in this file where the INCREMENTAL answer was the
	// better one. Full pruned the import placeholder and left the REFERENCES
	// endpoint pointing at a hex id no entity carried; incremental never pruned
	// and kept a live Module binding. Parity could have been restored in either
	// direction — teaching incremental to prune (worse but consistent) or
	// teaching full not to orphan (better, larger blast radius). Grounding
	// PruneImportPlaceholders decided it: its own doc comment asserts the
	// placeholders "have no incoming edges" by the time it runs, and the only
	// previous falsification of that premise (#642, JS/TS relative IMPORTS) was
	// answered by rewriting the edge rather than accepting the dangle. The
	// orphaning was incidental, not intentional, so the FULL path was fixed —
	// re-pointing each incoming edge at the entity the import resolver had
	// ALREADY bound the same whole-module import to. See the #6131 block in
	// internal/resolve/imports.go and `ref_repoints` in the prune's log line.

	// ── #6129 family: builtin CALLS endpoint blanked vs retained ──
	//
	// `len(...)` is a Python builtin with no in-repo definition. The full
	// rebuild finishes with an EMPTY ToID; the incremental run leaves the name
	// "len". Both are unbound, so no dangling-count moves — but they are not the
	// same graph, and a query joining on the endpoint sees different rows.
	{
		Issue:    "#6129",
		Why:      "Incremental leaves the builtin's name on the CALLS endpoint where the full run blanks it. Unfixed.",
		Bucket:   cpEdgeInvented,
		Contains: []string{"SCOPE.Operation/cp_use_class@cphandler_delta.py→«unbound»len:CALLS"},
	},
	{
		Issue:    "#6129",
		Why:      "The LOST half: the full rebuild's blank-endpoint form of the same builtin call.",
		Bucket:   cpEdgeLost,
		Contains: []string{"SCOPE.Operation/cp_use_class@cphandler_delta.py→«unbound»:CALLS"},
	},

	// ── #6094 family: a duplicated row persisted into graph.fb ──
	//
	// The exception-type entity synthesised for `CpNotFound` is written TWICE by
	// the incremental run, and its CONTAINS edge with it. This is the exact
	// class #6037 taught the comparator to see (a set comparison cannot); it is
	// pinned here with its magnitude so a 3rd copy would fail.
	{
		Issue:          "#6129 (duplicate-row class of #6094 / #6037)",
		Why:            "Incremental persists a second copy of the synthesised exception-type entity. Unfixed.",
		Bucket:         cpEntityMult,
		Contains:       []string{"SCOPE.ExceptionType|exception:CpNotFound"},
		DetailContains: []string{"row count 1 in A (full rebuild) ≠ 2 in B (incremental)"},
	},
	// The CONTAINS edge to that duplicated entity used to be duplicated WITH it,
	// and had its own entry here. FIXED — the entry went STALE and was removed
	// by the ratchet.
	//
	// It was collateral of the record→graph seam honouring EntityRecord.ID
	// instead of deriving one (the #6150 fix in entityRecordToGraphEntity): the
	// two copies of the exception record carried different ids, so their
	// CONTAINS edges had different RelationshipIDs and the seenRel guard could
	// not collapse them. Deriving the id makes both edges the same edge and one
	// of them is dropped, matching the full rebuild exactly.
	//
	// The ENTITY-MULTIPLICITY entry above still reproduces: the duplicate
	// exception ROW is a separate defect from the id it carried, and only the
	// second one is closed.
	// Re-keyed (not deleted) when the fixture grew for #6141/#6148/#6150: the
	// divergence still reproduces and is still the same one — the unassigned
	// community carries exactly ONE extra member, the duplicate row above — but
	// the key spells out ABSOLUTE membership counts, and those track the size of
	// the corpus. The load-bearing part of the key is the +1; the absolute pair
	// moves whenever a fixture file is added. Anything other than +1 is a
	// different divergence and must fail.
	//
	// This entry has now been re-keyed five times for fixture growth and zero
	// times for a change in the defect, which is a smell in the KEY, not in the
	// entry: it is the only allow entry keyed on an absolute count rather than on
	// a shape. Keying it on the DELTA would end the churn and would still fail on
	// a magnitude change — but the delta is not in the divergence string today
	// (parity.Report renders "N member(s) in A, M in B"), so it needs a
	// comparator change to reach, not an allow-list edit. Worth doing the next
	// time this file is opened for anything else.
	{
		Issue:    "#6129 (duplicate-row class of #6094 / #6037)",
		Why:      "Downstream of the duplicated entity: the unassigned community carries one extra member.",
		Bucket:   cpCommunitySet,
		Contains: []string{"community ∅: 60 member(s) in A, 61 in B"},
	},

	// ── #6156: the full rebuild orphans a THIRD-PARTY import's IMPORTS edge ──
	//
	// The only divergence the #6150 fixture reaches where the INCREMENTAL answer
	// is the correct one, which is why it is allow-listed rather than "fixed":
	// closing it on this side would mean teaching Path A to dangle.
	//
	// `import falcon` names a package with no in-repo definition. BOTH graphs
	// contain the `SCOPE.External|falcon` entity (measured — `external.Synthesize`
	// runs on both paths). The FULL rebuild's IMPORTS edge points at the hex id
	// of the import PLACEHOLDER that PruneImportPlaceholders has already removed,
	// so it dangles; Path A binds the same edge to the live `ext:falcon` node.
	//
	// #6131's repoint does not cover it: it re-points at the entity the pipeline
	// already resolved that import to, and for a third-party module there is
	// nothing resolved at prune time — the SCOPE.External node is synthesised
	// afterwards. See #6156 for the log line (`rels_orphaned=2`) and the fix
	// direction.
	{
		Issue:    "#6156",
		Why:      "Path A binds the third-party import to the live SCOPE.External node; the FULL rebuild dangles on the pruned placeholder. Incremental is the correct side. Unfixed, and not fixable from the incremental path.",
		Bucket:   cpEdgeInvented,
		Contains: []string{"→SCOPE.External/falcon@", ":IMPORTS"},
	},
	{
		Issue:    "#6156",
		Why:      "The LOST half of the same orphan: the full rebuild's dangling endpoint on the pruned import placeholder.",
		Bucket:   cpEdgeLost,
		Contains: []string{"SCOPE.Component/cphandler_delta.py@cphandler_delta.py→«unbound»", ":IMPORTS"},
	},
	{
		// A CONSEQUENCE of the row above, not a second defect: module
		// aggregation skips any edge whose endpoint resolves to no entity, so
		// the full rebuild's orphaned IMPORTS edge is not counted and Path A's
		// bound one is. Recorded as such because #6129 filed the same shape as
		// "an extra DEPENDS_ON row" and it was a consequence then too.
		Issue:          "#6156",
		Why:            "Downstream of the orphaned IMPORTS edges: module aggregation cannot count an edge with a dangling endpoint, so the full rebuild's weight is lower by exactly the number of orphaned third-party imports (2 here: falcon and express).",
		Bucket:         cpEdgeProps,
		Contains:       []string{"Module/test-repo@→Module/_external@:DEPENDS_ON"},
		DetailContains: []string{`weight "5"≠"7"`},
	},
	// The SAME #6156 defect on the second third-party import in the corpus.
	// Listed separately rather than folded into a looser key on the entries
	// above: one entry matching both would go on matching if only one of them
	// stopped reproducing, which is precisely the blindness the ratchet exists
	// to prevent.
	{
		Issue:    "#6156",
		Why:      "Second instance, JS: Path A binds the express require to the live SCOPE.External node; the FULL rebuild dangles on the pruned placeholder.",
		Bucket:   cpEdgeInvented,
		Contains: []string{"→SCOPE.External/express@", ":IMPORTS"},
	},
	{
		Issue:    "#6156",
		Why:      "The LOST half of the express orphan.",
		Bucket:   cpEdgeLost,
		Contains: []string{"SCOPE.Component/cproutes_delta.js@cproutes_delta.js→«unbound»", ":IMPORTS"},
	},

	// ── #6159: a cross-file handler leaves its endpoint unenriched ──
	//
	// The residual of #6150's file-scoped enrichment, and NOT a regression:
	// before #6150 this path ran neither enrichment pass, so the endpoint
	// carried none of these properties either. What changed is that the gate
	// can now see it.
	//
	// `cpRouter.get("/cpusers", cpListUsers)` names a handler defined in
	// cpctl_static.js. engine.ApplyResponseShapesCorpus needs the HANDLER'S
	// BODY to extract the response shape, and the reader this path supplies
	// serves only the file being re-extracted — so the endpoint survives
	// (#6150's keep guard) but without response_keys / status_codes /
	// response_keys_known.
	//
	// The endpoint itself is present and correctly identified on both sides:
	// this is a property gap, not a loss, and it is listed as ENTITY-FIELDS
	// rather than ENTITY-LOST for exactly that reason.
	{
		Issue:          "#6159",
		Why:            "File-scoped response-shape extraction cannot read a handler body in another file. Unfixed; needs the previous graph's entities as candidates.",
		Bucket:         cpEntityFields,
		Contains:       []string{"http_endpoint_definition|http:GET:/cpusers"},
		DetailContains: []string{"response_keys_known"},
	},

	// ── #6129 / #6098 family: over-counted DEPENDS_ON weight ──
	//
	// FIXED — the entry that stood here went STALE and was removed by the
	// ratchet.
	//
	// #6129 reported "an extra DEPENDS_ON test-repo → _external row" on Path A.
	// On this fixture it landed as a WEIGHT over-count on the single aggregated
	// row (5 where the full rebuild computes 1) rather than a second row, because
	// module aggregation folds duplicates into the weight property.
	//
	// It was never an independent defect: module-aggregation was faithfully
	// counting the mis-bound IMPORTS edges from the headline entry above, each of
	// which pointed into `_external`. Fixing the bind removed the input, and the
	// weight fell to the full rebuild's 1 with no change to the aggregation code.
	// Recorded because "extra DEPENDS_ON row" reads like a separate bug in the
	// issue text and is not one.
}

// TestContentParity_PathA_6129 runs the gate over Path A.
func TestContentParity_PathA_6129(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	cpStatic(t, repo)
	cpDelta(t, repo, 0)
	dvFullRebuild(t, repo, stateDir) // baseline graph + diff manifest

	full := cpEndState(t)

	dvSeedManifest(t, repo, stateDir)
	cpDelta(t, repo, 1)
	// dvIncremental fails the test if TryIncremental falls back to a full
	// reindex, which is what keeps this from becoming a full-vs-full no-op.
	inc := dvIncremental(t, repo, stateDir)

	cpAssertParity(t, "path A (extractors.TryIncremental)", full, inc, cpKnownPathA)
}

// ──────────────────────────── Path B gate ────────────────────────────

// cpKnownPathB characterises the #6129 Path-B divergences.
//
// Path B is a SECOND Index(..., WithIncremental(stateDir)) over a populated
// state dir — the CLI path. It is DIFFERENT code from Path A with DIFFERENT
// defects; the two allow-lists are deliberately not shared.
var cpKnownPathB = []cpKnown{
	// ── #6129, headline Path-B defect: a DEPENDS_ON SELF-EDGE ──
	//
	// The incremental run emits `CpProducer → CpProducer` — a module-aggregation
	// dependency from an entity onto itself, which a full rebuild never
	// produces and which is meaningless as a dependency. (#6129 records the same
	// shape as `ProdClassU → ProdClassU` on the #6119 fixture.) Edge counts move
	// by one in a direction that reads as "more resolved".
	{
		Issue:    "#6129",
		Why:      "Path B emits a spurious DEPENDS_ON self-edge a full rebuild does not. Unfixed.",
		Bucket:   cpEdgeInvented,
		Contains: []string{"Controller/CpProducer@cpprod_static.py→Controller/CpProducer@cpprod_static.py:DEPENDS_ON"},
	},

	// ── #6129, second Path-B defect: duplicated file→external DEPENDS_ON ──
	//
	// The `scope:component:file:* → *[SCOPE.External]` rows #6129 names are not
	// merely present — they are present TWICE where the full rebuild has one.
	// A set comparison cannot see this at all (#6037); a count comparison sees
	// "more edges" and cannot say which. Magnitude pinned.
	{
		Issue:          "#6129",
		Why:            "Path B duplicates the file→SCOPE.External DEPENDS_ON rows. Unfixed.",
		Bucket:         cpEdgeMult,
		Contains:       []string{"«unbound»scope:component:file:cphandler_delta.py→SCOPE.External/", ":DEPENDS_ON"},
		DetailContains: []string{"row count 1 in A (full rebuild) ≠ 2 in B (incremental)"},
	},

	// ── Shared with Path A: the from-import left as a verbatim stub ──
	//
	// Identical shape to the Path-A entry of the same name. Listed separately
	// rather than shared because the two paths are different code: if one is
	// fixed and the other is not, the stale-entry check must fail on exactly the
	// path that was fixed.
	{
		Issue:    "#6129",
		Why:      "from-import of a module constant is left as a verbatim stub by the incremental resolver. Unfixed.",
		Bucket:   cpEdgeInvented,
		Contains: []string{"→«unbound»cpcfg_static.CP_SETTING", ":IMPORTS"},
	},
	{
		Issue:    "#6129",
		Why:      "The LOST half of the same missed bind: the file component the full rebuild binds.",
		Bucket:   cpEdgeLost,
		Contains: []string{"→SCOPE.Component/cpcfg_static.py@cpcfg_static.py", ":IMPORTS"},
	},

	// ── #6129, more instances of the SAME duplicated file→external row ──
	//
	// The entry above this block pins the shape on cphandler_delta.py. The
	// #6150 fixture growth added two more registration files, and each
	// reproduces it against its own imports. Same defect, more instances — so
	// they carry the same issue number, but they are separate ENTRIES rather
	// than one loosened key, because a fix that repaired only one of the three
	// must fail the ratchet on the other two.
	{
		Issue:          "#6129",
		Why:            "Same duplicated file→external DEPENDS_ON, from the JS registration file.",
		Bucket:         cpEdgeMult,
		Contains:       []string{"«unbound»scope:component:file:cproutes_delta.js→"},
		DetailContains: []string{"row count 1 in A (full rebuild) ≠ 2 in B (incremental)"},
	},
	{
		Issue:          "#6129",
		Why:            "Same duplicated file→external DEPENDS_ON, from the Flask registration file.",
		Bucket:         cpEdgeMult,
		Contains:       []string{"«unbound»scope:component:file:cpwsgi_delta.py→"},
		DetailContains: []string{"row count 1 in A (full rebuild) ≠ 2 in B (incremental)"},
	},
	{
		Issue:          "#6129",
		Why:            "Same duplication reaching a framework-typed importer rather than a file component.",
		Bucket:         cpEdgeMult,
		Contains:       []string{"«unbound»Model:Flask→SCOPE.External/Flask@:DEPENDS_ON"},
		DetailContains: []string{"row count 1 in A (full rebuild) ≠ 2 in B (incremental)"},
	},

	// ── #6160: a REBOUND endpoint and its whole flow subtree, twice ──
	//
	// engine.bridgeEndpointToHandler rebinds a resolved synthetic's source_file
	// onto the handler's BODY (#2678). An endpoint registered in a CHANGED file
	// but resolved to a handler in an UNCHANGED one therefore ends up anchored
	// in the unchanged file — and is both carried forward from the previous
	// graph AND re-produced by this run. Both copies derive the same id, so it
	// is one entity emitted twice, and the flow pass builds a second
	// SCOPE.Process on top of it.
	//
	// Path A is clean on the identical fixture (it dedupes by reEmittedIDs and
	// seenNewRel), which is why this is on Path B's list alone. Reproduced
	// byte-identically at 617aeba6c — #6150 made the fixture reach it, it did
	// not cause it.
	{
		Issue:          "#6160",
		Why:            "Path B emits the rebound endpoint's IMPLEMENTS bridge twice — once in its synthesis-time form, once resolved. Unfixed.",
		Bucket:         cpEdgeMult,
		Contains:       []string{"→http_endpoint_definition/http:GET:/cpview@cpview_static.py:IMPLEMENTS"},
		DetailContains: []string{"row count 1 in A (full rebuild) ≠ 2 in B (incremental)"},
	},
	{
		Issue:          "#6160",
		Why:            "The property evidence for the row above: the duplicate pair is the same bridge at two pipeline stages (synthesis_time_bridge + synthesis_resolved).",
		Bucket:         cpEdgeProps,
		Contains:       []string{"→http_endpoint_definition/http:GET:/cpview@cpview_static.py:IMPLEMENTS"},
		DetailContains: []string{"http_endpoint_synthesis_time_bridge"},
	},
	{
		Issue:          "#6160",
		Why:            "The process-flow node built on the duplicated endpoint, duplicated with it.",
		Bucket:         cpEntityMult,
		Contains:       []string{"SCOPE.Process|http:GET:/cpview → cp_view_handler"},
		DetailContains: []string{"row count 1 in A (full rebuild) ≠ 2 in B (incremental)"},
	},
	{
		Issue:          "#6160",
		Why:            "Its STEP_IN_PROCESS edge to the handler operation.",
		Bucket:         cpEdgeMult,
		Contains:       []string{"SCOPE.Process/http:GET:/cpview → cp_view_handler@cpview_static.py→SCOPE.Operation/cp_view_handler@cpview_static.py:STEP_IN_PROCESS"},
		DetailContains: []string{"row count 1 in A (full rebuild) ≠ 2 in B (incremental)"},
	},
	{
		Issue:          "#6160",
		Why:            "Its STEP_IN_PROCESS edge to the endpoint.",
		Bucket:         cpEdgeMult,
		Contains:       []string{"SCOPE.Process/http:GET:/cpview → cp_view_handler@cpview_static.py→http_endpoint_definition/"},
		DetailContains: []string{"row count 1 in A (full rebuild) ≠ 2 in B (incremental)"},
	},
	{
		Issue:          "#6160",
		Why:            "Its ENTRY_POINT_OF edge.",
		Bucket:         cpEdgeMult,
		Contains:       []string{":ENTRY_POINT_OF"},
		DetailContains: []string{"row count 1 in A (full rebuild) ≠ 2 in B (incremental)"},
	},
	{
		Issue:    "#6160",
		Why:      "Downstream of the duplicated Process entity: the unassigned community carries one extra member. Keyed on the absolute pair, so it moves with fixture size — see the same note on Path A's copy.",
		Bucket:   cpCommunitySet,
		Contains: []string{"community ∅: 60 member(s) in A, 61 in B"},
	},

	// ── #6159, shared with Path A: cross-file handler, unenriched endpoint ──
	//
	// Listed separately from Path A's entry because the two paths are different
	// code: if one is fixed and the other is not, the stale-entry check must
	// fail on exactly the path that was fixed. Reproduced at 617aeba6c, so it
	// is a residual of file-scoped enrichment on BOTH paths, not a regression.
	{
		Issue:          "#6159",
		Why:            "Response-shape extraction does not reach a handler body in another file. Unfixed.",
		Bucket:         cpEntityFields,
		Contains:       []string{"http_endpoint_definition|http:GET:/cpusers"},
		DetailContains: []string{"response_keys_known"},
	},
}

// TestContentParity_PathB_6129 runs the gate over Path B.
func TestContentParity_PathB_6129(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	cpStatic(t, repo)
	cpDelta(t, repo, 0)
	dvFullRebuild(t, repo, stateDir)

	full := cpEndState(t)

	dvSeedManifest(t, repo, stateDir)
	cpDelta(t, repo, 1)
	// cfPathBIncremental asserts from the run's own stderr that the incremental
	// branch was taken and that no full-reindex fallback fired.
	inc := cfPathBIncremental(t, repo, stateDir)

	cpAssertParity(t, "path B (Index + WithIncremental)", full, inc, cpKnownPathB)
}
