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
		// The synthetic must still BE there — a fold is not permission to drop it.
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
