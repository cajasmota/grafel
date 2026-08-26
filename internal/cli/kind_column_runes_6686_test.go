package cli

// kind_column_runes_6686_test.go — #6686.
//
// Two distinct things are pinned here, and they are deliberately separate:
//
//  1. The Kind column's arithmetic. maxKindLen must size in RUNES because
//     fmt's %-*s pads in runes, and every padding site must use the width
//     maxKindLen returned rather than re-deriving one per row.
//
//  2. The INVARIANT that makes the production path safe today: every entity
//     and relationship kind constant is ASCII, so byte length and rune count
//     coincide and the unit mismatch is currently inert. That is a property
//     of the real value set, checkable against its source of truth, and it
//     stops holding the day someone adds a non-ASCII kind — which is exactly
//     the moment the column arithmetic starts mattering.
//
// (1) is a contract test on the width helper and the printer. (2) is why no
// end-to-end fixture injects a synthetic non-ASCII kind through
// PrintRebuildSummary: production cannot currently produce one, so such a
// fixture would assert behaviour for unreachable input.

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cajasmota/grafel/internal/types"
)

// isASCII reports whether s contains only bytes below utf8.RuneSelf, i.e.
// whether len(s) == utf8.RuneCountInString(s) is guaranteed.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// (1a) maxKindLen sizes in runes, not bytes.
// ---------------------------------------------------------------------------

func TestRebuildMaxKindLenCountsRunes(t *testing.T) {
	cases := []struct {
		name      string
		kinds     []string
		withOther bool
		want      int // rune-counted width
		wantBytes int // what a byte-counted width would produce
	}{
		{
			// Longest by runes is the 7-rune ASCII kind; longest by bytes is
			// the 10-byte CJK kind. The two disagree, so this separates the
			// units. Neither non-ASCII kind's byte length equals the wanted
			// width, so a per-row len() padding mutant cannot reach the right
			// column by luck.
			name:      "rune-longest differs from byte-longest",
			kinds:     []string{"asciixx", "日本語x", "café"},
			withOther: false,
			want:      7,
			wantBytes: 10,
		},
		{
			// Every kind is shorter than the "Other" floor in runes but the
			// byte-counted maximum overshoots it, so this pins the floor and
			// the unit together: a byte-sized width silently escapes the floor.
			name:      "floor binds under rune counting but not under byte counting",
			kinds:     []string{"é", "日本"},
			withOther: true,
			want:      5, // len("Other")
			wantBytes: 6,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.want == tc.wantBytes {
				t.Fatalf("premise broken: rune width %d equals byte width %d, "+
					"this case cannot distinguish the two units", tc.want, tc.wantBytes)
			}
			rows := make([]kindRow, 0, len(tc.kinds))
			for i, k := range tc.kinds {
				rows = append(rows, kindRow{Kind: k, Count: i + 1})
			}
			if got := maxKindLen(rows, tc.withOther); got != tc.want {
				t.Errorf("maxKindLen(%q, %v) = %d, want %d (a byte-counted width gives %d)",
					tc.kinds, tc.withOther, got, tc.want, tc.wantBytes)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// (1b) every padding site uses the computed width.
// ---------------------------------------------------------------------------

// countColumn returns the rune column at which the trailing count field of a
// two-field table row begins, and true if line is such a row for kind.
//
// Rows are located by their first whitespace-separated field, never by an
// indentation prefix: a HasPrefix(line, "    ") guard is inert on a padded
// table because the padding absorbs a deleted indent space.
func countColumn(line, kind string) (int, bool) {
	f := strings.Fields(line)
	if len(f) != 2 || f[0] != kind {
		return 0, false
	}
	i := strings.LastIndex(line, f[1])
	if i < 0 {
		return 0, false
	}
	return utf8.RuneCountInString(line[:i]), true
}

func TestRebuildKindColumnPaddingUsesTheComputedWidth(t *testing.T) {
	s := &RebuildSummary{
		Group:         "g",
		TotalEntities: 220,
		EntityByKind: map[string]int{
			"HTTPEndpoint": 60, // 12 runes — sets the entity column width
			"Function":     50, // 8
			"Variable":     40, // 8
			"Module":       30, // 6
			"Schema":       20, // 6
			"Class":        10, // pushed into Other
			"Route":        5,  // pushed into Other
		},
		TotalRelationships: 280,
		RelByKind: map[string]int{
			"INHERITS_FROM": 70, // 13 runes — sets the relationship column width
			"REFERENCES":    60, // 10
			"DEPENDS_ON":    50, // 10
			"IMPORTS":       40, // 7
			"CALLS":         30, // 5
			"TESTS":         20, // pushed into Other
			"USES":          10, // pushed into Other
		},
		EnrichmentCandidates: 200,
		EnrichmentActions:    215,
		EnrichmentByKind: map[string]int{
			"describe_entity": 60, // 15 runes — sets the enrichment column width
			"describe_role":   50, // 13
			"classify_domain": 40, // 15
			"describe_module": 30, // 15
			"summarise_flow":  20, // 14
			"tag_pattern":     12, // pushed into Other
			"link_doc":        6,  // pushed into Other
		},
	}

	var buf bytes.Buffer
	PrintRebuildSummary(&buf, s)
	lines := strings.Split(buf.String(), "\n")

	// Absolute columns, not cross-line agreement. Agreement already holds on
	// unfixed code, and a mutant that widens the indent at every site of one
	// table keeps every line in agreement while moving the whole column.
	//
	//   entities/relationships: 2-space indent + width + 2-space gap
	//   enrichment breakdown:   4-space indent + width + 2-space gap
	want := []struct {
		kind string
		col  int
	}{
		// Entities — width 12.
		{"HTTPEndpoint", 2 + 12 + 2},
		{"Function", 2 + 12 + 2},
		{"Variable", 2 + 12 + 2},
		{"Module", 2 + 12 + 2},
		{"Schema", 2 + 12 + 2},
		// Relationships — width 13.
		{"INHERITS_FROM", 2 + 13 + 2},
		{"REFERENCES", 2 + 13 + 2},
		{"DEPENDS_ON", 2 + 13 + 2},
		{"IMPORTS", 2 + 13 + 2},
		{"CALLS", 2 + 13 + 2},
		// Enrichment breakdown — width 15, deeper indent.
		{"describe_entity", 4 + 15 + 2},
		{"describe_role", 4 + 15 + 2},
		{"classify_domain", 4 + 15 + 2},
		{"describe_module", 4 + 15 + 2},
		{"summarise_flow", 4 + 15 + 2},
	}

	for _, w := range want {
		found := false
		for _, line := range lines {
			col, ok := countColumn(line, w.kind)
			if !ok {
				continue
			}
			found = true
			if col != w.col {
				t.Errorf("row %q: count starts at rune column %d, want %d\n  line: %q",
					w.kind, col, w.col, line)
			}
		}
		if !found {
			t.Errorf("row %q not found in output:\n%s", w.kind, buf.String())
		}
	}

	// All three tables emit an "Other" row sharing their table's width
	// variable. The three overflow totals are deliberately distinct, so each
	// row is identified by its count alone — no indentation heuristic, which
	// would misroute rows under exactly the indent mutants this pins.
	otherWant := map[string]int{
		"15": 2 + 12 + 2, // entities:      Class 10 + Route 5
		"30": 2 + 13 + 2, // relationships: TESTS 20 + USES 10
		"18": 4 + 15 + 2, // enrichment:    tag_pattern 12 + link_doc 6
	}
	seenOther := map[string]bool{}
	for _, line := range lines {
		col, ok := countColumn(line, "Other")
		if !ok {
			continue
		}
		count := strings.Fields(line)[1]
		wantCol, known := otherWant[count]
		if !known {
			t.Errorf("unexpected Other row %q — the three overflow totals must stay "+
				"distinct for this test to tell the tables apart", line)
			continue
		}
		seenOther[count] = true
		if col != wantCol {
			t.Errorf("Other row (count %s): count starts at rune column %d, want %d\n  line: %q",
				count, col, wantCol, line)
		}
	}
	for c := range otherWant {
		if !seenOther[c] {
			t.Errorf("Other row with count %s not found in output:\n%s", c, buf.String())
		}
	}
}

// ---------------------------------------------------------------------------
// (2) the invariant: every kind constant is ASCII.
// ---------------------------------------------------------------------------

// kindConstantsFromSource parses internal/types/kinds.go — the source of truth
// for the kinds this table renders — and returns every string value declared
// as an EntityKind or RelationshipKind constant, keyed by constant name.
//
// It reads the declarations rather than a hand-copied list, and asserts a
// superset relation against the registry accessors below rather than a
// hand-maintained count, so a newly added kind is covered the moment it is
// declared without anyone remembering to update this test.
func kindConstantsFromSource(t *testing.T) map[string]string {
	t.Helper()
	const path = "../types/kinds.go"

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	out := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		lastType := ""
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if id, ok := vs.Type.(*ast.Ident); ok {
				lastType = id.Name
			}
			if lastType != "EntityKind" && lastType != "RelationshipKind" {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s = %s: %v", name.Name, lit.Value, err)
				}
				out[name.Name] = v
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("parsed no EntityKind/RelationshipKind constants from %s — "+
			"the declaration shape changed and this test is no longer reading "+
			"the source of truth", path)
	}
	return out
}

func TestKindConstantsAreASCII(t *testing.T) {
	consts := kindConstantsFromSource(t)

	// Guard the parse: every kind the registry accessors return must have been
	// seen by the parser. If the const block is split or reshaped so the parse
	// stops covering it, this fails instead of silently checking a subset.
	declared := map[string]bool{}
	for _, v := range consts {
		declared[v] = true
	}
	for _, k := range types.AllEntityKinds() {
		if !declared[string(k)] {
			t.Errorf("entity kind %q is returned by types.AllEntityKinds() but was not "+
				"found by the source parse — the parse no longer covers the registry", k)
		}
	}
	for _, k := range types.AllRelationshipKinds() {
		if !declared[string(k)] {
			t.Errorf("relationship kind %q is returned by types.AllRelationshipKinds() but "+
				"was not found by the source parse — the parse no longer covers the registry", k)
		}
	}

	// The invariant itself.
	for name, v := range consts {
		if !isASCII(v) {
			t.Errorf("kind constant %s = %q is not ASCII.\n"+
				"This is not a style complaint: PrintRebuildSummary's Kind column "+
				"relies on kinds being ASCII for byte length and rune count to "+
				"coincide. A non-ASCII kind is legal — but before adding one, "+
				"confirm the column arithmetic in rebuild_summary.go, and note "+
				"that it targets RUNE COUNT, not terminal display width (a CJK "+
				"ideograph is one rune and two columns).", name, v)
		}
	}
}

// TestNormalisedEntityKindsAreASCII closes the display-name half: the Kind
// values the table actually renders are normaliseEntityKind's output, which is
// either one of its own display names or a pass-through of the raw kind.
func TestNormalisedEntityKindsAreASCII(t *testing.T) {
	for _, k := range types.AllEntityKinds() {
		if got := normaliseEntityKind(string(k)); !isASCII(got) {
			t.Errorf("normaliseEntityKind(%q) = %q is not ASCII", k, got)
		}
	}
	// The raw HTTP-endpoint spellings are not EntityKind constants; they reach
	// normaliseEntityKind from the extractors as free-form strings.
	for _, k := range []string{"function", "method", "class", "struct", "interface",
		"variable", "constant", "field", "http_endpoint",
		"http_endpoint_definition", "http_endpoint_call"} {
		if got := normaliseEntityKind(k); !isASCII(got) {
			t.Errorf("normaliseEntityKind(%q) = %q is not ASCII", k, got)
		}
	}
}
