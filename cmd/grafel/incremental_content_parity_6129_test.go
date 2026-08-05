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
// Refs #6129, #6037, #6094, #6123, #6083, #6128.
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
//     (falcon/cherrypy match a bare `class X:`) and the #1613 fold collapses the
//     AST's generic SCOPE.Component node into that typed record. The incremental
//     path ran the language extractor alone and left the generic kind, so the
//     same class was `Controller` after a full rebuild and `SCOPE.Component`
//     after an incremental one — a one-entity divergence that dragged four
//     CONTAINS edges with it, since entity ids hash the kind. Fixed in
//     internal/extractors/incremental.go (see engine.FoldFrameworkClassKinds).
//     No class had ever appeared in this file's delta, which is the whole reason
//     the gate stayed green through it.
func cpDelta(t *testing.T, repo string, pass int) {
	t.Helper()
	dvWriteFile(t, repo, "cphandler_delta.py", fmt.Sprintf(`import cperrs_static
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
    def cp_probe_noop(self):
        return 1


class CpLeafBag:
    cp_owner: int = 1


def cp_leaf_call():
    return cp_owner()
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
	{
		Issue:          "#6129 (duplicate-row class of #6094 / #6037)",
		Why:            "The CONTAINS edge to the duplicated entity, duplicated with it.",
		Bucket:         cpEdgeMult,
		Contains:       []string{"→SCOPE.ExceptionType/exception:CpNotFound@<exception>:CONTAINS"},
		DetailContains: []string{"row count 1 in A (full rebuild) ≠ 2 in B (incremental)"},
	},
	// Re-keyed (not deleted) when the fixture grew for #6141/#6148: the
	// divergence still reproduces and is still the same one — the unassigned
	// community carries exactly ONE extra member, the duplicate row above — but
	// the key spells out ABSOLUTE membership counts, and those track the size of
	// the corpus. The load-bearing part of the key is the +1; the absolute pair
	// moves whenever a fixture file is added. Anything other than +1 is a
	// different divergence and must fail.
	{
		Issue:    "#6129 (duplicate-row class of #6094 / #6037)",
		Why:      "Downstream of the duplicated entity: the unassigned community carries one extra member.",
		Bucket:   cpCommunitySet,
		Contains: []string{"community ∅: 32 member(s) in A, 33 in B"},
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
