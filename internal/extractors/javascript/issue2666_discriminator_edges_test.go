// Package javascript_test — issue #2666: verifies that DISCRIMINATES_ON edges
// emitted by stampDiscriminators carry line + literal properties.
//
// Issue #2659 introduced the discriminator pattern detection but only stamped
// the comma-separated pair string on Properties["discriminators"]. #2666
// promotes the data into proper DISCRIMINATES_ON edges so archigraph_inspect
// can render a line-precise comparison table and archigraph_find can mix the
// literal values into the BM25 doc terms.
package javascript_test

import (
	"strings"
	"testing"
)

// TestDiscriminator_EmitsEdgeWithLineAndLiteral_TS verifies that
// `checklistType === 2` inside a function body emits one DISCRIMINATES_ON edge
// from the enclosing entity to "var:checklistType" with Properties["line"] set
// to the comparison's source line and Properties["literal"] = "2".
func TestDiscriminator_EmitsEdgeWithLineAndLiteral_TS(t *testing.T) {
	src := `
function processChecklist() {
  const isCat5 = checklistType === 2;
  return isCat5;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	e := findByNameRel(ents, "processChecklist")
	if e == nil {
		t.Fatal("entity 'processChecklist' not found")
	}

	var found bool
	for _, r := range e.Relationships {
		if r.Kind != "DISCRIMINATES_ON" {
			continue
		}
		if r.ToID != "var:checklistType" {
			continue
		}
		found = true
		if r.Properties == nil {
			t.Fatalf("DISCRIMINATES_ON edge has nil Properties; want line+literal")
		}
		if r.Properties["literal"] != "2" {
			t.Errorf("Properties[literal]=%q, want %q", r.Properties["literal"], "2")
		}
		if r.Properties["line"] == "" {
			t.Errorf("Properties[line] is empty, want non-empty 1-indexed line")
		}
		// The comparison sits on line 3 of the snippet (line 1 = leading blank,
		// line 2 = `function processChecklist() {`, line 3 = the const assignment).
		if r.Properties["line"] != "3" {
			t.Errorf("Properties[line]=%q, want %q", r.Properties["line"], "3")
		}
	}
	if !found {
		t.Logf("relationships on processChecklist:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Errorf("DISCRIMINATES_ON edge to var:checklistType not found")
	}
}

// TestDiscriminator_EmitsEdgePerComparison_TS verifies that multiple
// discriminator comparisons in one function body emit one edge per unique pair,
// each with its own line number.
func TestDiscriminator_EmitsEdgePerComparison_TS(t *testing.T) {
	src := `
function processChecklist() {
  const isCat5 = checklistType === 2;
  const isPeriodic = type === 'periodic';
  return isCat5 || isPeriodic;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	e := findByNameRel(ents, "processChecklist")
	if e == nil {
		t.Fatal("entity 'processChecklist' not found")
	}

	byTarget := map[string]map[string]string{}
	for _, r := range e.Relationships {
		if r.Kind == "DISCRIMINATES_ON" {
			byTarget[r.ToID] = r.Properties
		}
	}
	if len(byTarget) < 2 {
		t.Fatalf("expected ≥2 DISCRIMINATES_ON edges, got %d (%v)", len(byTarget), byTarget)
	}
	if p := byTarget["var:checklistType"]; p == nil || p["literal"] != "2" || p["line"] == "" {
		t.Errorf("var:checklistType edge bad: %v", p)
	}
	if p := byTarget["var:type"]; p == nil || p["literal"] != "periodic" || p["line"] == "" {
		t.Errorf("var:type edge bad: %v", p)
	}
	// Lines must differ between the two comparisons.
	if byTarget["var:checklistType"]["line"] == byTarget["var:type"]["line"] {
		t.Errorf("expected distinct line numbers; both = %q",
			byTarget["var:checklistType"]["line"])
	}
}

// TestDiscriminator_NoEdgesWhenAbsent_TS verifies that a function body with no
// discriminator patterns emits zero DISCRIMINATES_ON edges.
func TestDiscriminator_NoEdgesWhenAbsent_TS(t *testing.T) {
	src := `
function noDiscriminator(a, b) {
  return a + b;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	e := findByNameRel(ents, "noDiscriminator")
	if e == nil {
		t.Fatal("entity 'noDiscriminator' not found")
	}
	for _, r := range e.Relationships {
		if strings.EqualFold(r.Kind, "DISCRIMINATES_ON") {
			t.Errorf("unexpected DISCRIMINATES_ON edge: %+v", r)
		}
	}
}
