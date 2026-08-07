// Package extractors_test — incremental_entity_dedupe_e2e_6161_test.go
//
// #6161 end-to-end gate over a REAL fixture, complementing the unit gates in
// incremental_entity_dedupe_6161_test.go.
//
// The unit tests hand-build the colliding records, which proves the seam
// behaves but not that anything in the tree actually produces the collision.
// This one runs a genuine Java source file with two method overloads through
// TryIncremental and reads the written graph back, so it also fails if the Java
// extractor ever stops naming both overloads "Over.go" — i.e. if the premise of
// the fix quietly evaporates.
//
// Measured before the fix, this exact fixture wrote:
//
//	ENT id=8b60de6936b08bc2 name="Over.go" lines=7-9  sig="void go(int a)"
//	ENT id=8b60de6936b08bc2 name="Over.go" lines=11-13 sig="void go(String a)"
//
// Two rows, one ID. SortDocumentForEmission (internal/graph/emission_order.go)
// documents "Entity IDs are unique, so ID alone is a total order", and the
// FlatBuffers `(key)` binary search behind LookupEntityByID relies on it, so the
// second row was permanently unreachable while still occupying a slot and a
// count.
//
// SCOPE: this test asserts BOTH halves of the contract (one row AND both
// overloads' CALLS edges), so it dies for either mutant. That is deliberate for
// an integration gate; the mutant-independence matrix is carried by the unit
// tests, which are each blind to the other's failure mode.
package extractors_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
)

// overloadJava declares `go` twice at different lines. Each overload calls a
// DIFFERENT private helper, so the two records carry disjoint edge sets and a
// fold that discards the duplicate instead of merging it loses one of them.
const overloadJava = `package p;

public class Over {
    void helperInt() {}
    void helperStr() {}

    public void go(int a) {
        helperInt();
    }

    public void go(String a) {
        helperStr();
    }
}
`

func TestIncremental_JavaOverloads_OneEntityRowBothEdges(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	// Seed a graph + manifest against a placeholder Over.java, then rewrite it
	// so the incremental pass sees exactly one changed file to re-extract.
	writeFile(t, repo, "Other.java", "package p;\npublic class Other { void x() {} }\n")
	writeFile(t, repo, "Over.java", "package p;\npublic class Over {}\n")

	other := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Class", "Other", "Other.java"),
		Name: "Other", Kind: "SCOPE.Class", SourceFile: "Other.java", Language: "java",
	}
	buildMinimalGraph(t, stateDir, []graph.Entity{other}, nil)
	seedManifest(t, repo, stateDir)

	writeFile(t, repo, "Over.java", overloadJava)

	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("TryIncremental fell back (%s) — the seam under test never ran", res.FallbackReason)
	}

	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}

	// (1) The written graph must satisfy the uniqueness invariant that
	//     emission_order.go and LookupEntityByID both assume.
	byID := make(map[string]int, len(doc.Entities))
	for _, e := range doc.Entities {
		byID[e.ID]++
	}
	for _, e := range doc.Entities {
		if byID[e.ID] > 1 {
			t.Fatalf("entity ID %q written %d times (kind=%s name=%q file=%s) — entity IDs must be "+
				"unique or one row is unreachable through LookupEntityByID (#6161)",
				e.ID, byID[e.ID], e.Kind, e.Name, e.SourceFile)
		}
	}

	// (2) The overloaded method is one row, and it is the one the extractor
	//     really produced (guards against the fixture silently going stale).
	overloadID := ""
	rows := 0
	for _, e := range doc.Entities {
		if e.SourceFile == "Over.java" && e.Name == "Over.go" {
			rows++
			overloadID = e.ID
		}
	}
	if rows != 1 {
		t.Fatalf("entity Over.go appears %d time(s) in Over.java, want exactly 1 — if 0, the Java "+
			"extractor no longer names both overloads identically and this fixture no longer "+
			"reproduces the #6161 collision", rows)
	}

	// (3) BOTH overloads' call edges survive, anchored to the surviving row.
	//     This is what separates a fold from a deletion: `go(int)` calls
	//     helperInt and `go(String)` calls helperStr, and a gate that skips the
	//     duplicate record loses the second call entirely.
	helper := func(name string) string {
		for _, e := range doc.Entities {
			if e.SourceFile == "Over.java" && e.Name == name {
				return e.ID
			}
		}
		t.Fatalf("fixture entity %q not extracted", name)
		return ""
	}
	for _, target := range []string{"Over.helperInt", "Over.helperStr"} {
		tid := helper(target)
		found := false
		for _, r := range doc.Relationships {
			if r.FromID == overloadID && r.ToID == tid && r.Kind == "CALLS" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("CALLS edge Over.go → %s is missing — folding two overloads into one entity row "+
				"must UNION their relationships, not discard the dropped record's (#6161)", target)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// #6161, the UNBOUNDED half: a placeholder-sourced synthetic entity
// ─────────────────────────────────────────────────────────────────────────────

// Both files raise/catch the SAME exception, so each contributes a copy of the
// "<exception>"-sourced entity.
//
// WHY "<exception>" AND NOT "<config>". There are seven synthetic-source
// sentinels ("<config>", "<exception>", "<external-service>",
// "<translation-key>", "<template>", "<package>", "<panache-dsl-runtime>") and
// mergeEntitiesDeduped is kind-agnostic across all of them, but only some are
// reachable from a live extractor. "<exception>" is; "<config>" is NOT —
// extractor.ConfigKeyEntity has zero production callers today (the language
// extractors emit DEPENDS_ON_CONFIG edges, not config-key entities, and
// internal/enrichers/config_consumer_extractor.go is an unwired port). Driving a
// "<config>" row through this fixture would therefore assert nothing.
// Kind-agnosticism is pinned directly instead, on seeded rows, in
// TestIncremental_LegacyDuplicateRows_RepairedOnNextPass below.
const excErrs = `class CpNotFound(Exception):
    pass


def cp_raise(x):
    raise CpNotFound("nope")
`

func excHandler(n int) string {
	return fmt.Sprintf(`import cperrs


def cp_handle(x):
    try:
        return cperrs.cp_raise(x) + %d
    except cperrs.CpNotFound:
        return 0
`, n)
}

// TestIncremental_SyntheticEntity_MultiplicityStableAcrossPasses pins the worst
// form of #6161 — the one the entity fold inside convertExtractedRecords CANNOT
// reach on its own.
//
// A SCOPE.ExceptionType entity is synthesised with a PLACEHOLDER source file
// ("<exception>"), not a real one. graph.EntityID therefore does not distinguish
// it per file, so:
//
//   - two different files in one re-extraction batch derive the SAME id, which a
//     per-file fold cannot see; and, far worse,
//   - the copy in the PREVIOUS graph is never evicted (the eviction is keyed on
//     entities sourced from a CHANGED file, and "<exception>" is not a file), so
//     every pass appended another copy.
//
// Measured before the merge-side fold, editing one handler repeatedly:
//
//	pass 1 → CpNotFound x2      pass 2 → x3      pass 3 → x4 …
//
// That is the #6094 accumulation shape, on entities rather than edges: monotone,
// unbounded, and only ever reset by a full reindex. It is also why this fold has
// to be survivor-aware — a batch-scoped fold collapses the x2 and leaves the
// growth untouched.
func TestIncremental_SyntheticEntity_MultiplicityStableAcrossPasses(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	writeFile(t, repo, "cperrs.py", excErrs)
	writeFile(t, repo, "handler.py", excHandler(0))
	writeFile(t, repo, "seed.py", "def seed():\n    pass\n")

	seed := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "seed", "seed.py"),
		Name: "seed", Kind: "SCOPE.Operation", SourceFile: "seed.py", Language: "python",
	}
	buildMinimalGraph(t, stateDir, []graph.Entity{seed}, nil)
	seedManifest(t, repo, stateDir)

	prevTotal := -1
	for pass := 1; pass <= 3; pass++ {
		// Pass 1 also perturbs cperrs.py so BOTH files land in one batch — that
		// is the cross-file half. Later passes touch only the handler, which is
		// the survivor-vs-new half.
		if pass == 1 {
			writeFile(t, repo, "cperrs.py", excErrs+"\n# touched\n")
		}
		writeFile(t, repo, "handler.py", excHandler(pass))

		res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
		if !res.Done {
			t.Fatalf("pass %d: TryIncremental fell back (%s) — the merge under test never ran",
				pass, res.FallbackReason)
		}

		doc, err := graph.LoadGraphFromDir(stateDir)
		if err != nil {
			t.Fatalf("pass %d: load graph: %v", pass, err)
		}
		counts := make(map[string]int, len(doc.Entities))
		for _, e := range doc.Entities {
			counts[e.ID]++
		}
		for _, e := range doc.Entities {
			if counts[e.ID] > 1 {
				t.Fatalf("pass %d: entity ID %q written %d times (kind=%s name=%q file=%q) — entity "+
					"IDs must be unique; a placeholder-sourced synthetic accumulated one extra copy "+
					"per pass (#6161)", pass, e.ID, counts[e.ID], e.Kind, e.Name, e.SourceFile)
			}
		}
		// The synthetic must still BE there — a fold is not permission to drop a
		// row. Checked every pass so the test fails loudly if a fixture edit
		// stops reaching the sentinel, rather than passing vacuously.
		var haveExc bool
		for _, e := range doc.Entities {
			if e.Kind == "SCOPE.ExceptionType" && e.Name == "exception:CpNotFound" {
				haveExc = true
			}
		}
		if !haveExc {
			t.Fatalf("pass %d: the synthesised CpNotFound exception entity is gone — the fold must "+
				"MERGE the duplicate, not discard the row", pass)
		}
		if prevTotal >= 0 && len(doc.Entities) > prevTotal {
			t.Fatalf("pass %d: entity count grew %d → %d across passes with no new code (#6161)",
				pass, prevTotal, len(doc.Entities))
		}
		prevTotal = len(doc.Entities)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// #6161 — repair on load: the surviving set is folded too
// ─────────────────────────────────────────────────────────────────────────────

// TestIncremental_LegacyDuplicateRows_RepairedOnNextPass pins the one behaviour
// in this change that MUTATES DATA IN AN EXISTING USER GRAPH on upgrade.
//
// mergeEntitiesDeduped folds the SURVIVING entity set, not only the incoming
// one. Without that, the accumulation stops but the damage stays: a graph
// written by any earlier build still carries its duplicate rows, and the
// uniqueness invariant SortDocumentForEmission and LookupEntityByID depend on is
// still false on the very next write. Every pre-fix graph in the wild is in
// exactly that state — this is not a hypothetical input.
//
// WHY DELETING A PERSISTED ROW IS SAFE HERE. Two rows sharing one EntityID is a
// state a full rebuild can never produce (buildDocument has gated on the id
// since #4406), so collapsing them strictly converges the incremental graph
// toward full-rebuild output rather than inventing a third behaviour. And no
// edge can be orphaned by the collapse, because the two rows share the id that
// every edge endpoint refers to.
//
// The assertions therefore cover BOTH halves. A test that only counted rows
// would pass a fold that dropped the loser's edges or its base-only fields,
// which is the failure mode that would actually cost a user data.
//
// The seeded rows are "<config>"-sourced on purpose. Sentinel-sourced entities
// are where duplicates accumulate, and this is the only place "<config>" can be
// exercised: extractor.ConfigKeyEntity has no production callers, so no live
// extractor can produce one. Seeding it directly pins that the fold is
// kind-agnostic across the seven sentinels rather than special-cased to the
// "<exception>" shape the fixtures above happen to reach.
func TestIncremental_LegacyDuplicateRows_RepairedOnNextPass(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	writeFile(t, repo, "legacy.py", "def t1():\n    pass\n\n\ndef t2():\n    pass\n")
	writeFile(t, repo, "churn.py", "def churn0():\n    pass\n")

	// The colliding pair: same kind/name/source-file, hence one EntityID. The
	// first row is the impoverished one, the second carries the base-only state
	// — so a fold that keeps the first and discards the second WITHOUT
	// gap-filling loses QualifiedName and Signature.
	dupID := graph.EntityID("test-repo", "SCOPE.Config", "config:APP_TIMEOUT", "<config>")
	dupPoor := graph.Entity{
		ID: dupID, Name: "config:APP_TIMEOUT", Kind: "SCOPE.Config",
		SourceFile: "<config>", Language: "python",
	}
	dupRich := graph.Entity{
		ID: dupID, Name: "config:APP_TIMEOUT", Kind: "SCOPE.Config",
		SourceFile: "<config>", Language: "python",
		QualifiedName: "config:APP_TIMEOUT", Signature: "config:APP_TIMEOUT",
	}
	t1 := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "t1", "legacy.py"),
		Name: "t1", Kind: "SCOPE.Operation", SourceFile: "legacy.py", Language: "python",
	}
	t2 := graph.Entity{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "t2", "legacy.py"),
		Name: "t2", Kind: "SCOPE.Operation", SourceFile: "legacy.py", Language: "python",
	}

	// Three edges touching the duplicated id: two outbound (one per notional
	// "side" of the pair) and one inbound, which also proves the collapse does
	// not trip the inbound-dangling prune into treating the id as removed.
	out1 := graph.Relationship{
		ID:     graph.RelationshipID(dupID, t1.ID, "REFERENCES"),
		FromID: dupID, ToID: t1.ID, Kind: "REFERENCES",
	}
	out2 := graph.Relationship{
		ID:     graph.RelationshipID(dupID, t2.ID, "REFERENCES"),
		FromID: dupID, ToID: t2.ID, Kind: "REFERENCES",
	}
	in1 := graph.Relationship{
		ID:     graph.RelationshipID(t1.ID, dupID, "DEPENDS_ON_CONFIG"),
		FromID: t1.ID, ToID: dupID, Kind: "DEPENDS_ON_CONFIG",
	}

	buildMinimalGraph(t, stateDir,
		[]graph.Entity{dupPoor, dupRich, t1, t2},
		[]graph.Relationship{out1, out2, in1})
	seedManifest(t, repo, stateDir)

	// Touch a file that has nothing to do with the duplicated rows. The repair
	// must happen anyway — it is a property of the merge, not of the delta.
	writeFile(t, repo, "churn.py", "def churn1():\n    pass\n")

	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("TryIncremental fell back (%s) — the merge under test never ran", res.FallbackReason)
	}

	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}

	var rows []graph.Entity
	for _, e := range doc.Entities {
		if e.ID == dupID {
			rows = append(rows, e)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("the pre-existing duplicate pair survived as %d rows, want 1 — the fold must cover "+
			"the SURVIVING set, or every graph written before #6161 keeps its duplicates and stays "+
			"in a state a full rebuild can never produce", len(rows))
	}

	// Gap-fill across the two legacy rows: the survivor was the impoverished one.
	surv := rows[0]
	if surv.QualifiedName != "config:APP_TIMEOUT" {
		t.Errorf("QualifiedName = %q, want %q carried over from the dropped legacy row — collapsing "+
			"the pair must MERGE it, not pick one and delete the other's state",
			surv.QualifiedName, "config:APP_TIMEOUT")
	}
	if surv.Signature != "config:APP_TIMEOUT" {
		t.Errorf("Signature = %q, want %q carried over from the dropped legacy row",
			surv.Signature, "config:APP_TIMEOUT")
	}

	// The union of both rows' edges must still be reachable from the survivor.
	// This is the half a row count cannot see, and the half that would cost a
	// user real data.
	for _, want := range []graph.Relationship{out1, out2, in1} {
		var found bool
		for _, r := range doc.Relationships {
			if r.FromID == want.FromID && r.ToID == want.ToID && r.Kind == want.Kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("edge %s→%s:%s was lost when the duplicate rows were collapsed — the two rows "+
				"share the id every edge endpoint refers to, so folding them cannot orphan an edge",
				want.FromID, want.ToID, want.Kind)
		}
	}
}
