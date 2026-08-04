// Package main — incremental_carry_qualifiedname_6119_test.go
//
// #6119 — Path B's incremental carry-forward record omits QualifiedName.
//
// There are two incremental reindex paths in the tree. Path A is the daemon's
// fast in-place reindex (internal/extractors.TryIncremental) — that is what
// the dvIncremental harness drives. Path B is a SECOND
// Index(..., WithIncremental(stateDir)) run over an already-populated state
// dir: it diffs the manifest, keeps the changed files, and carries the
// previous graph's UNCHANGED-file entities forward as
// Indexer.incrementalCarryForwardEntities so the resolver index can still see
// them (cmd/grafel/index.go, the `cf = append(cf, types.EntityRecord{…})`
// record). Only Path B builds that record, so only Path B is exercised here —
// dvIncremental would make both cases vacuous.
//
// The record copied ID / Name / Kind / Subtype / SourceFile / Properties and
// omitted QualifiedName, which resolve.BuildIndex consumes to populate
// idx.byQualifiedName. Carried entities therefore contributed ZERO entries to
// that index, and the omission diverges in two directions:
//
//	Direction 1 (missed bind) — a stub whose only resolution tier is
//	byQualifiedName cannot bind to an entity in an unchanged file.
//	TestPathBIncremental_CarriedQualifiedName_MissedBind_6119.
//
//	Direction 2 (confident WRONG bind) — byQualifiedName carries an ambiguity
//	sentinel (internal/resolve/refs.go: a second entity with the same
//	QualifiedName blanks the entry so the stub is left alone). With carried
//	entities contributing nothing, a collision that straddles the
//	changed/unchanged boundary is invisible: the sentinel never fires and the
//	stub binds CONFIDENTLY to the changed-file entity, which is not the entity
//	a full rebuild binds.
//	TestPathBIncremental_CarriedQualifiedName_CollisionWrongBind_6119.
//
// Direction 2 is the reason this is a soundness defect rather than a missed
// bind, and it is the behaviour a naive one-line fix has to be checked
// against — hence it is pinned separately, and asserted on the BOUND TARGET's
// content (source file + kind), never on a dangling/unresolved count, which
// reads a mis-bind as an improvement (#6123).
//
// A third direction named in the issue — carried entities OVER-populating the
// Properties["ref"] → byQualifiedName tier because extractIndexableRef's
// `ref == e.QualifiedName` skip cannot fire on an empty QualifiedName — is
// LATENT, not active, and is deliberately not pinned with a fixture. The skip
// is gated on four ref prefixes (scope:endpoint:, scope:testcoverage:,
// scope:component:interface:<lang>:) and NO extractor that emits one of those
// refs sets QualifiedName at all: internal/extractors/cross/endpoint and
// .../cross/testmap stamp only Properties["ref"], and .../cross/hierarchy sets
// QualifiedName only on Python class entities, whose ref is
// scope:component:class: (not a gated prefix). Measured on the checked-in
// golden graph (internal/feedback/testdata/golden): 37 entities carry a gated
// ref prefix, 0 of them carry any QualifiedName. So `ref == e.QualifiedName`
// is unobtainable for exactly the refs the skip guards, and restoring
// QualifiedName changes nothing on that tier. A fixture forcing the shape
// would test a hand-built record, not the pipeline.
//
// Refs #6119, #6090, #6098, #6037.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// ─────────────────────────── Path B harness ───────────────────────────

// cfPathBIncremental runs a SECOND Index(..., WithIncremental(stateDir)) over
// the same state dir. That is Path B: the run diffs the manifest, re-extracts
// only the changed files, and builds the carry-forward record under test.
//
// Path B's too-many-changed / unloadable-baseline guards fall back to a FULL
// reindex silently (by putting every file back into the changed set), which
// would make every assertion below vacuous — a full reindex trivially has no
// carry-forward problem. There is no return value to inspect, so the run's own
// stderr is captured and the incremental decision is asserted from it: the
// "processing N of M files" line must be present with N < M, and no fallback
// line may appear.
func cfPathBIncremental(t *testing.T, repo, stateDir string) *graph.Document {
	t.Helper()
	t.Setenv("GRAFEL_DAEMON_ROOT", stateDir)

	realStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fbPath := filepath.Join(stateDir, "graph.fb")
	idxErr := Index(repo, fbPath, "test-repo", []string{"graph-algo"}, false, false,
		WithIncremental(stateDir))

	_ = w.Close()
	os.Stderr = realStderr
	logs := <-done
	_ = r.Close()

	if idxErr != nil {
		t.Fatalf("path-B Index: %v\n%s", idxErr, logs)
	}
	if strings.Contains(logs, "falling back to full reindex") {
		t.Fatalf("path-B run fell back to a FULL reindex — the carry-forward record "+
			"under test is never built, so the case would be vacuous:\n%s", logs)
	}
	if !strings.Contains(logs, "grafel: incremental — processing ") {
		t.Fatalf("path-B run did not take the incremental branch (no "+
			"'incremental — processing' line); every assertion below would be vacuous:\n%s", logs)
	}
	if !strings.Contains(logs, "grafel: incremental — carried forward ") {
		t.Fatalf("path-B run took the incremental branch but carried nothing forward; "+
			"the carry-forward record under test is empty:\n%s", logs)
	}

	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph after path-B incremental: %v", err)
	}
	return doc
}

// cfTarget describes the entity an edge actually resolved to. Kind+SourceFile
// (not a count, not an ID) is what distinguishes a correct bind from the
// wrong-file bind #6119 produces.
type cfTarget struct {
	Resolved   bool
	Name       string
	Kind       string
	SourceFile string
	Raw        string // verbatim ToID when unresolved
}

func (c cfTarget) String() string {
	if !c.Resolved {
		return "STUB:" + c.Raw
	}
	return fmt.Sprintf("%s[%s|%s]", c.Name, c.Kind, c.SourceFile)
}

// cfOutboundTargets returns the resolved-or-not target of every relationship of
// kind relKind whose FROM endpoint is the file-component entity for fromFile.
//
// Targets are returned as a LIST, not keyed by ToID: a resolved edge has had
// its ToID rewritten to the target's hex ID, so the verbatim stub is not a key
// that survives on both sides of the comparison.
func cfOutboundTargets(doc *graph.Document, fromFile, relKind string) []cfTarget {
	byID := make(map[string]graph.Entity, len(doc.Entities))
	for _, e := range doc.Entities {
		byID[e.ID] = e
	}
	// The FROM endpoint of an IMPORTS edge is the SCOPE.Component entity for
	// the file. Collect its ID(s).
	fromIDs := make(map[string]bool)
	for _, e := range doc.Entities {
		if e.SourceFile == fromFile && e.Kind == "SCOPE.Component" {
			fromIDs[e.ID] = true
		}
	}
	var out []cfTarget
	for _, r := range doc.Relationships {
		if r.Kind != relKind || !fromIDs[r.FromID] {
			continue
		}
		if to, ok := byID[r.ToID]; ok {
			out = append(out, cfTarget{Resolved: true, Name: to.Name, Kind: to.Kind, SourceFile: to.SourceFile})
			continue
		}
		out = append(out, cfTarget{Raw: r.ToID})
	}
	return out
}

// cfResolvedTargetSet is the set of "<kind>|<sourcefile>" strings the resolved
// edges of cfOutboundTargets point at. Comparing SETS OF BOUND TARGETS BY
// CONTENT (rather than counting stubs) is what makes a mis-bind visible: a
// wrong bind and a right bind both count as "resolved".
func cfResolvedTargetSet(m []cfTarget) map[string]bool {
	s := make(map[string]bool)
	for _, v := range m {
		if v.Resolved {
			s[v.Kind+"|"+v.SourceFile] = true
		}
	}
	return s
}

// ─────────────────────── Direction 1 — the missed bind ───────────────────────

// cfMissedBindUnchanged writes the files that must NOT be touched between the
// baseline and the incremental run. Every basename in the corpus is unique:
// diff.Filter cross-invalidates same-basename files, which would pull the
// "unchanged" file into the changed set and make the case vacuous.
func cfMissedBindUnchanged(t *testing.T, repo string) {
	t.Helper()
	dvWriteFile(t, repo, "errs_u.py", `class NotFoundU(Exception):
    pass


def raiser_u(x):
    raise NotFoundU("nope")
`)
	dvWriteFile(t, repo, "prodmod_u.py", `def prod_target_u(x):
    return x * 3


class ProdClassU:
    def method_u(self, y):
        return y + 1
`)
	dvWriteFile(t, repo, "cfgmod_u.py", `SETTING_U = "abc"
OTHER_U = 12
`)
}

// cfMissedBindChanged writes the single file that is edited between passes.
// Its IMPORTS stubs ("errs_u", "prodmod_u", "prodmod_u.ProdClassU") are the
// QualifiedName of entities that live only in the UNCHANGED files above, so
// byQualifiedName is the tier that has to bind them.
func cfMissedBindChanged(t *testing.T, repo string, pass int) {
	t.Helper()
	dvWriteFile(t, repo, "handler_c.py", fmt.Sprintf(`import errs_u
import prodmod_u
from prodmod_u import ProdClassU


def handle_c(x):
    try:
        return errs_u.raiser_u(x) + %d
    except errs_u.NotFoundU:
        return prodmod_u.prod_target_u(x)


def use_class_c(y):
    c = ProdClassU()
    return c.method_u(y)
`, pass))
}

// TestPathBIncremental_CarriedQualifiedName_MissedBind_6119 pins direction 1.
//
// Pre-fix the three IMPORTS edges out of handler_c.py are left as verbatim
// stubs ("errs_u", "prodmod_u", "prodmod_u.ProdClassU") because the only
// entities carrying those QualifiedNames live in unchanged files and were
// carried forward without one.
func TestPathBIncremental_CarriedQualifiedName_MissedBind_6119(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	cfMissedBindUnchanged(t, repo)
	cfMissedBindChanged(t, repo, 0)
	dvFullRebuild(t, repo, stateDir)

	// End-state reference: a clean full rebuild of exactly what the
	// incremental run will land on.
	fullRepo := t.TempDir()
	cfMissedBindUnchanged(t, fullRepo)
	cfMissedBindChanged(t, fullRepo, 1)
	full := dvFullRebuild(t, fullRepo, t.TempDir())

	dvSeedManifest(t, repo, stateDir)
	cfMissedBindChanged(t, repo, 1)
	inc := cfPathBIncremental(t, repo, stateDir)

	wantT := cfOutboundTargets(full, "handler_c.py", "IMPORTS")
	gotT := cfOutboundTargets(inc, "handler_c.py", "IMPORTS")

	// Positive baseline — without this the assertions below are satisfied by a
	// graph that never held the edges at all.
	want := cfResolvedTargetSet(wantT)
	if len(want) == 0 {
		t.Fatalf("fixture is inert: the full rebuild resolved no IMPORTS edge out of "+
			"handler_c.py at all (targets=%v)", wantT)
	}
	for _, sf := range []string{"errs_u.py", "prodmod_u.py"} {
		found := false
		for k := range want {
			if strings.HasSuffix(k, "|"+sf) {
				found = true
			}
		}
		if !found {
			t.Fatalf("fixture is inert: the full rebuild binds no IMPORTS edge out of "+
				"handler_c.py into the unchanged file %s (targets=%v)", sf, wantT)
		}
	}

	got := cfResolvedTargetSet(gotT)
	for k := range want {
		if !got[k] {
			t.Errorf("incremental (path B) lost the IMPORTS bind to %s.\n"+
				"  full rebuild targets: %v\n"+
				"  incremental targets:  %v\n"+
				"The target lives in an UNCHANGED file, so it reaches the resolver only "+
				"through the carry-forward record — which omitted QualifiedName, leaving "+
				"idx.byQualifiedName with zero entries from unchanged files (#6119).",
				k, cfSorted(wantT), cfSorted(gotT))
		}
	}
}

// ──────────────── Direction 2 — the confident WRONG bind ────────────────

// cfCollisionUnchanged writes the unchanged half of the QualifiedName
// collision: module "zeta_u.beta_u" with a module-level `gamma_u`, giving the
// function QualifiedName "zeta_u.beta_u.gamma_u".
func cfCollisionUnchanged(t *testing.T, repo string) {
	t.Helper()
	dvWriteFile(t, repo, "zeta_u/beta_u.py", `def gamma_u(x):
    return x + 41
`)
}

// cfCollisionChanged writes the changed half. zeta_u/__init__.py is module
// "zeta_u" and declares `class beta_u` with a method `gamma_u`, so the METHOD's
// QualifiedName is also "zeta_u.beta_u.gamma_u" — a genuine collision that
// straddles the changed/unchanged boundary (a class in a package's __init__.py
// sharing a name with a sibling submodule). consumer_c.py's
// `from zeta_u.beta_u import gamma_u` emits an IMPORTS edge whose ToID is
// exactly that colliding QualifiedName.
//
// Basenames are unique across the corpus (`beta_u.py`, `__init__.py`,
// `consumer_c.py`) so diff.Filter's same-basename cross-invalidation cannot
// drag zeta_u/beta_u.py into the changed set and dissolve the straddle.
func cfCollisionChanged(t *testing.T, repo string, pass int) {
	t.Helper()
	dvWriteFile(t, repo, "zeta_u/__init__.py", fmt.Sprintf(`class beta_u:
    def gamma_u(self, x):
        return x + %d
`, pass))
	dvWriteFile(t, repo, "consumer_c.py", fmt.Sprintf(`from zeta_u.beta_u import gamma_u


def drive_c(x):
    return gamma_u(x) + %d
`, pass))
}

// TestPathBIncremental_CarriedQualifiedName_CollisionWrongBind_6119 pins
// direction 2 — the soundness half.
//
// Pre-fix, the incremental run binds consumer_c.py's IMPORTS edge CONFIDENTLY
// to `beta_u.gamma_u` in zeta_u/__init__.py — a different entity, in a
// different file, from the one the full rebuild binds (`gamma_u` in
// zeta_u/beta_u.py). The colliding partner sat in an unchanged file and was
// carried forward without a QualifiedName, so the ambiguity sentinel never
// fired.
//
// The assertion is deliberately "never a DIFFERENT resolved entity", not
// "identical to the full rebuild": restoring QualifiedName makes the sentinel
// fire and the stub is then correctly left alone, whereas the full rebuild
// recovers the right target through a later import-aware tier that only sees
// freshly-extracted records. Unresolved is honest; wrongly-resolved is not.
func TestPathBIncremental_CarriedQualifiedName_CollisionWrongBind_6119(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	cfCollisionUnchanged(t, repo)
	cfCollisionChanged(t, repo, 0)
	dvFullRebuild(t, repo, stateDir)

	fullRepo := t.TempDir()
	cfCollisionUnchanged(t, fullRepo)
	cfCollisionChanged(t, fullRepo, 1)
	full := dvFullRebuild(t, fullRepo, t.TempDir())

	dvSeedManifest(t, repo, stateDir)
	cfCollisionChanged(t, repo, 1)
	inc := cfPathBIncremental(t, repo, stateDir)

	const qname = "zeta_u.beta_u.gamma_u"
	wantT := cfOutboundTargets(full, "consumer_c.py", "IMPORTS")
	gotT := cfOutboundTargets(inc, "consumer_c.py", "IMPORTS")

	// Positive baseline: consumer_c.py has exactly one import, and the full
	// rebuild must bind it to the module-level function in the UNCHANGED file.
	if len(wantT) != 1 {
		t.Fatalf("fixture is inert: the full rebuild emits %d IMPORTS edge(s) out of "+
			"consumer_c.py, want exactly 1 (targets=%v)", len(wantT), cfSorted(wantT))
	}
	ref := wantT[0]
	if !ref.Resolved || ref.SourceFile != "zeta_u/beta_u.py" {
		t.Fatalf("fixture is inert: the full rebuild binds consumer_c.py's IMPORTS edge to "+
			"%s, expected the module-level function in zeta_u/beta_u.py — the collision "+
			"straddle is not set up as intended", ref)
	}

	if len(gotT) != 1 {
		t.Fatalf("incremental emits %d IMPORTS edge(s) out of consumer_c.py, want exactly 1 "+
			"(targets=%v) — the case cannot measure a mis-bind", len(gotT), cfSorted(gotT))
	}
	got := gotT[0]
	if got.Resolved && got.SourceFile != ref.SourceFile {
		t.Errorf("incremental (path B) bound consumer_c.py's IMPORTS edge CONFIDENTLY to the "+
			"WRONG entity: got %s, full rebuild binds %s.\n"+
			"Both entities carry QualifiedName %q; the colliding one lives in an UNCHANGED "+
			"file and was carried forward without a QualifiedName, so idx.byQualifiedName "+
			"never saw the collision, the ambiguity sentinel never fired, and the stub bound "+
			"to the changed-file entity with no signal (#6119).",
			got, ref, qname)
	}
}

// cfSorted renders a target list deterministically for failure messages.
func cfSorted(m []cfTarget) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v.String())
	}
	// small maps; insertion-sort keeps the helper dependency-free
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
