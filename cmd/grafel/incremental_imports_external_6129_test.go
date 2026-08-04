// Package main — incremental_imports_external_6129_test.go
//
// #6129 Path A (`internal/extractors.TryIncremental`, the daemon path — what MCP
// callers see between full reindexes).
//
// The defect: an IMPORTS edge naming an in-repo Python package binds to the
// `ext:<pkg>` / SCOPE.External placeholder instead of the real in-repo `Module`
// entity that a full rebuild binds it to. The incremental graph therefore
// asserts a dependency on an EXTERNAL package where the source imports a LOCAL
// one — a wrong answer to "what does this depend on?".
//
// Why no existing check caught it: the mis-bind is not a loss (that class was
// #6090). Both graphs have the same entity count, the same relationship count,
// no dangling endpoints, and the mis-bind actually IMPROVES the dangling metric
// (#6123) because `ext:pkgbeta` exists while a stub would not. Every count-shaped
// or one-directional comparison reports it as healthy. So the assertions below
// are on BOUND-TARGET CONTENT — the kind/name of the entity each edge actually
// points at — and never on counts.
package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// im6129Targets renders every IMPORTS / DEPENDS_ON edge as a single descriptor
// naming BOTH endpoints by content, and returns the MULTISET of those
// descriptors. Comparing the multiset between a full rebuild and an incremental
// run is a content comparison: an edge that resolved to a different target
// appears as one descriptor lost and one gained, and neither shows up in a
// count of edges, entities or dangling endpoints.
//
// Both design choices here are load-bearing, and the first was a review catch:
//
//   - THE TARGET IS PART OF THE KEY, not the value. An earlier version keyed on
//     (Kind, FromID) alone and stored the target as the map value, so two
//     imports out of the same file collapsed onto one entry and half the
//     assertions vanished silently. That is vacuity mode 3 in the list on the
//     test below — the fixture has one import today, and adding a second would
//     have disarmed it with no failure anywhere.
//
//   - MULTISET, not set. A duplicated row is the #6037 / #6094 class; a set
//     comparison cannot see it. Counting occurrences means a second copy of an
//     edge fails here rather than being folded away.
func im6129Targets(d *graph.Document) map[string]int {
	byID := make(map[string]graph.Entity, len(d.Entities))
	for _, e := range d.Entities {
		byID[e.ID] = e
	}
	describe := func(id string) string {
		if e, ok := byID[id]; ok {
			return fmt.Sprintf("%s/%s name=%q@%s", e.Kind, e.Subtype, e.Name, e.SourceFile)
		}
		return "«unbound»" + id
	}
	out := make(map[string]int)
	for _, r := range d.Relationships {
		if r.Kind != "IMPORTS" && r.Kind != "DEPENDS_ON" {
			continue
		}
		out[fmt.Sprintf("%s: %s → %s", r.Kind, describe(r.FromID), describe(r.ToID))]++
	}
	return out
}

// TestIncrementalImportsBindLocalModuleNotExternal_6129 pins Path A on a corpus
// whose only import names an in-repo package.
//
// THREE WAYS THIS FIXTURE CAN GO SILENTLY VACUOUS. All three were hit — two
// while grounding the defect, one caught in review — and none announces itself:
// the test just passes.
//
//  1. THE DELTA MUST TOUCH THE IMPORTING FILE. A delta on any OTHER file
//     produces no divergence at all, even pre-fix, because the mis-bind happens
//     when the scoped resolver RE-RESOLVES an IMPORTS edge — and it only does
//     that for edges out of a re-extracted file. The first probe written for
//     this defect edited a bystander file, saw full and incremental agree, and
//     very nearly concluded the issue did not reproduce. A future edit that
//     re-points the delta at `betaother.py` to "keep the fixture simple" would
//     silently disarm this test.
//
//  2. EVERY BASENAME MUST BE UNIQUE. `diff.Filter` cross-invalidates by
//     basename, so a repeated filename turns a 1-file delta into an N-file
//     change and trips the too-many-changed full-reindex fallback — testing a
//     full rebuild against a full rebuild. dvIncremental fatals on that
//     fallback, so the incremental path is at least asserted to have run.
//
//  3. THE COMPARISON KEY MUST NAME THE TARGET. See im6129Targets — an earlier
//     version keyed on (Kind, FromID) and stored the target as the value, so a
//     second import out of the same file would have collapsed onto the first
//     and silently dropped half the assertions. Fixed by folding both endpoints
//     into the key and comparing multisets.
//
// A fourth guard is explicit rather than structural: the precondition below
// fails loudly if the full rebuild stops producing the edge under test.
func TestIncrementalImportsBindLocalModuleNotExternal_6129(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	dvWriteFile(t, repo, "pkgbeta/__init__.py", "VALUE = 1\n")
	dvWriteFile(t, repo, "betamain.py", "import pkgbeta\n\ndef run():\n    return pkgbeta.VALUE\n")
	dvWriteFile(t, repo, "betaother.py", "def other():\n    return 1\n")

	dvFullRebuild(t, repo, stateDir)
	dvSeedManifest(t, repo, stateDir)

	// Re-extract the importing file. This is what makes the scoped resolver
	// re-bind its IMPORTS edge against a name index built over the PREVIOUS
	// PERSISTED graph — which, unlike the full resolver's index, already
	// contains the post-synthesis `ext:pkgbeta` placeholder.
	dvWriteFile(t, repo, "betamain.py", "import pkgbeta\n\ndef run():\n    return pkgbeta.VALUE + 1\n")

	b := dvIncremental(t, repo, stateDir)

	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	full := im6129Targets(c)
	inc := im6129Targets(b)

	// Guard against a vacuous pass: the full rebuild must actually bind an
	// IMPORTS edge to the in-repo Module, or there is no mis-bind to detect.
	sawLocalImport := false
	for descriptor := range full {
		if strings.HasPrefix(descriptor, "IMPORTS: ") &&
			strings.Contains(descriptor, `→ Module/package name="pkgbeta"`) {
			sawLocalImport = true
		}
	}
	if !sawLocalImport {
		t.Fatalf("precondition failed: the full rebuild bound no IMPORTS edge to the "+
			"in-repo Module \"pkgbeta\", so this case cannot detect the mis-bind.\n"+
			"full rebuild edges:%s", im6129Render(full))
	}

	keys := make(map[string]bool, len(full)+len(inc))
	for k := range full {
		keys[k] = true
	}
	for k := range inc {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)

	// A re-target shows up as one LOST plus one INVENTED naming both targets —
	// that pairing IS the mis-bind, and it is invisible to any count.
	for _, k := range ordered {
		switch fv, iv := full[k], inc[k]; {
		case fv > 0 && iv == 0:
			t.Errorf("edge present in the full rebuild but ABSENT from incremental:\n  %s", k)
		case fv == 0 && iv > 0:
			t.Errorf("SPURIOUS edge emitted only by the incremental path:\n  %s", k)
		case fv != iv:
			t.Errorf("edge row count differs: %d in the full rebuild, %d in incremental:\n  %s", fv, iv, k)
		}
	}
}

func im6129Render(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := ""
	for _, k := range keys {
		s += fmt.Sprintf("\n  ×%d %s", m[k], k)
	}
	return s
}
