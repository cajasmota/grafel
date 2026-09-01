package types

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// adr0003Path is the taxonomy ADR whose "Decision" section names the
// relationship-kind vocabulary in prose.
const adr0003Path = "../../docs/adrs/0003-scope-entity-taxonomy.md"

var adr0003KindToken = regexp.MustCompile("`([A-Z][A-Z_]+)`")

// TestADR0003RelationshipKindsAreImplemented closes the contradiction found in
// #6741: ADR-0003's Decision section listed `CONSUMES_QUEUE`, `TRIGGERS_LAMBDA`,
// `READS_TABLE` and `WRITES_TABLE` inside what it called a closed enum, while
// TestIsValidRelationshipKind above asserted, in as many words, that those same
// four must NOT be valid. Both had been in the tree since they were written and
// neither observed the other: the ADR line was prose no test read.
//
// It reads the one line of the Decision section that enumerates edge kinds and
// requires every kind named there to be a real, registered RelationshipKind.
// A kind the ADR promises and the code does not implement fails here rather
// than surviving for four months as a documented guarantee (ADR-0028).
func TestADR0003RelationshipKindsAreImplemented(t *testing.T) {
	raw, err := os.ReadFile(adr0003Path)
	if err != nil {
		t.Fatalf("reading %s: %v (if the ADR moved, update adr0003Path — do not delete this test)", adr0003Path, err)
	}

	var line string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(l, "Edges use a ") && strings.Contains(l, "relationship kinds:") {
			if line != "" {
				t.Fatalf("%s has more than one edge-kind enumeration line; this test reads exactly one", adr0003Path)
			}
			line = l
		}
	}
	if line == "" {
		t.Fatalf("%s: no line starting %q found — the Decision section's edge-kind enumeration is the subject of this test", adr0003Path, "Edges use a ")
	}

	matches := adr0003KindToken.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		t.Fatalf("%s: edge-kind line names no backticked kinds: %q", adr0003Path, line)
	}
	for _, m := range matches {
		kind := m[1]
		if !IsValidRelationshipKind(kind) {
			t.Errorf("ADR-0003 names %q as a relationship kind, but IsValidRelationshipKind(%q) is false: "+
				"the ADR documents an edge kind no producer emits", kind, kind)
		}
	}
}
