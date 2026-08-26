package cli

// kind_column_runes_6686_test.go — #6686.
//
// Two distinct things are pinned here, and they are deliberately separate:
//
//  1. The Kind column's arithmetic. maxKindLen must size in RUNES because
//     fmt's %-*s pads in runes; it must apply the "Other" floor exactly when
//     the caller says an "Other" row will be printed and not otherwise; and
//     every padding site must use the width maxKindLen returned rather than
//     re-deriving one per row.
//
//  2. The INVARIANT that makes the entity and relationship tables safe today:
//     every kind constant declared in internal/types/kinds.go is ASCII, so
//     byte length and rune count coincide there and the unit mismatch is inert
//     for those two tables. That is a property of the real value set,
//     checkable against its source of truth, and it stops holding the day
//     someone declares a non-ASCII kind — which is exactly the moment the
//     column arithmetic starts mattering.
//
// The enrichment breakdown is NOT covered by (2): its kinds are free-form
// strings read out of enrichment-candidates.json with no registry behind them,
// so a non-ASCII enrichment kind is reachable in production TODAY. That is why
// the printer fixtures below put their byte/rune divergence in the enrichment
// table specifically, and use only ASCII kinds in the other two: it keeps
// every fixture to input the system can actually produce.

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cajasmota/grafel/internal/types"
)

// unparen strips redundant parentheses from a type or value expression, so
// `(EntityKind)` is recognised as EntityKind rather than dropped as an
// unrecognised shape.
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

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
// (1a) maxKindLen: rune sizing, and the floor applied exactly when asked.
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
			name:      "divergence in the rune-longest kind, floor not reached",
			kinds:     []string{"asciixx", "日本語x", "café"},
			withOther: false,
			want:      7,
			wantBytes: 10,
		},
		{
			// The divergence is carried by a kind that is NOT the rune-longest
			// (3 runes against the 4-rune maximum) yet whose byte length (5)
			// still exceeds that maximum. A width that leaks bytes only for
			// short or non-maximal kinds is invisible to the case above, which
			// concentrates all the divergence in the longest string.
			name:      "divergence in a shorter, non-maximal kind",
			kinds:     []string{"abcd", "ééa"},
			withOther: false,
			want:      4,
			wantBytes: 5,
		},
		{
			// Every kind is shorter than the "Other" floor in runes but the
			// byte-counted maximum overshoots it, so this pins the floor and
			// the unit together: a byte-sized width silently escapes the floor.
			name:      "floor binds under rune counting but not under byte counting",
			kinds:     []string{"é", "日本"},
			withOther: true,
			want:      5, // utf8.RuneCountInString("Other")
			wantBytes: 6,
		},
		{
			// The same kinds with withOther=false. The floor must NOT be
			// applied when no "Other" row will be printed: a width of 5 here
			// would pad every row three columns past the longest kind. This is
			// the permissive direction — a floor applied unconditionally looks
			// harmless and is invisible to the case above, which asks for it.
			name:      "floor must not apply when no Other row is printed",
			kinds:     []string{"é", "日本"},
			withOther: false,
			want:      2,
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
// (1b) the printer: width propagation, and the floor through the real path.
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

type kindColCase struct {
	name    string
	summary *RebuildSummary
	// wantCol maps a kind to the absolute rune column its count must start at.
	wantCol map[string]int
	// wantOther maps an "Other" row's count text to its expected column. The
	// three overflow totals are kept distinct per fixture so a row is
	// identified by its count alone — no indentation heuristic, which would
	// misroute rows under exactly the indent mutants this pins.
	wantOther map[string]int
}

func TestRebuildKindColumnPaddingUsesTheComputedWidth(t *testing.T) {
	cases := []kindColCase{
		{
			// Wide columns: every table's longest kind is far above the
			// 5-rune "Other" floor, so the floor never binds. This case's
			// subject is width PROPAGATION — that each of the six %-*s sites
			// takes the width its table computed instead of re-deriving one.
			name: "floor far below the longest kind",
			summary: &RebuildSummary{
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
			},
			wantCol: map[string]int{
				// Entities — width 12, 2-space indent, 2-space gap.
				"HTTPEndpoint": 2 + 12 + 2,
				"Function":     2 + 12 + 2,
				"Variable":     2 + 12 + 2,
				"Module":       2 + 12 + 2,
				"Schema":       2 + 12 + 2,
				// Relationships — width 13.
				"INHERITS_FROM": 2 + 13 + 2,
				"REFERENCES":    2 + 13 + 2,
				"DEPENDS_ON":    2 + 13 + 2,
				"IMPORTS":       2 + 13 + 2,
				"CALLS":         2 + 13 + 2,
				// Enrichment breakdown — width 15, 4-space indent.
				"describe_entity": 4 + 15 + 2,
				"describe_role":   4 + 15 + 2,
				"classify_domain": 4 + 15 + 2,
				"describe_module": 4 + 15 + 2,
				"summarise_flow":  4 + 15 + 2,
			},
			wantOther: map[string]int{
				"15": 2 + 12 + 2, // entities:      Class 10 + Route 5
				"30": 2 + 13 + 2, // relationships: TESTS 20 + USES 10
				"18": 4 + 15 + 2, // enrichment:    tag_pattern 12 + link_doc 6
			},
		},
		{
			// Every kind in every table is SHORTER than "Other", and every
			// table overflows, so all three widths come from the floor rather
			// than from any kind. This is the case where a dropped floor
			// genuinely raggeds a table: the "Other" row is not among the rows
			// maxKindLen sees, so it would overflow a sub-5 column while its
			// siblings sat padded to the smaller width.
			//
			// The enrichment table additionally carries the byte/rune
			// divergence, on a kind that is below the floor: "ééaa" is 4 runes
			// and 6 bytes, so a byte-sized width escapes the floor (6) where a
			// rune-sized one is held at it (5).
			name: "floor binds in all three tables",
			summary: &RebuildSummary{
				Group:         "g",
				TotalEntities: 280,
				EntityByKind: map[string]int{
					"fn":  70, // 2 runes
					"cls": 60, // 3
					"var": 50, // 3
					"mod": 40, // 3
					"sch": 30, // 3
					"tag": 20, // pushed into Other
					"doc": 10, // pushed into Other
				},
				TotalRelationships: 287,
				RelByKind: map[string]int{
					"USES": 71, // 4 runes
					"HAS":  61, // 3
					"OWNS": 51, // 4
					"GETS": 41, // 4
					"SETS": 31, // 4
					"ADDS": 21, // pushed into Other
					"DELS": 11, // pushed into Other
				},
				EnrichmentCandidates: 260,
				EnrichmentActions:    296,
				EnrichmentByKind: map[string]int{
					"ééaa": 72, // 4 runes, 6 BYTES — the divergence, below the floor
					"abcd": 62, // 4 runes, 4 bytes — ties it in runes, not in bytes
					"tag":  52, // 3
					"doc":  42, // 3
					"fix":  32, // 3
					"cat":  22, // pushed into Other
					"nom":  12, // pushed into Other
				},
			},
			wantCol: map[string]int{
				// Entities — floor 5 (longest kind is 3).
				"fn": 2 + 5 + 2, "cls": 2 + 5 + 2, "var": 2 + 5 + 2,
				"mod": 2 + 5 + 2, "sch": 2 + 5 + 2,
				// Relationships — floor 5 (longest kind is 4).
				"USES": 2 + 5 + 2, "HAS": 2 + 5 + 2, "OWNS": 2 + 5 + 2,
				"GETS": 2 + 5 + 2, "SETS": 2 + 5 + 2,
				// Enrichment — floor 5 (longest kind is 4 runes / 6 bytes).
				"ééaa": 4 + 5 + 2, "abcd": 4 + 5 + 2, "tag": 4 + 5 + 2,
				"doc": 4 + 5 + 2, "fix": 4 + 5 + 2,
			},
			wantOther: map[string]int{
				"30": 2 + 5 + 2, // entities:      tag 20 + doc 10
				"32": 2 + 5 + 2, // relationships: ADDS 21 + DELS 11
				"34": 4 + 5 + 2, // enrichment:    cat 22 + nom 12
			},
		},
		{
			// No table overflows, so no "Other" row is printed anywhere and
			// the floor must NOT be applied — every kind is shorter than 5
			// runes, so an unconditional floor is visible as three columns of
			// dead space. This is the permissive direction of the floor and is
			// invisible to the case above, which asks for the floor.
			//
			// The enrichment table carries the byte/rune divergence on a
			// NON-MAXIMAL kind: "ééa" is 3 runes against the 4-rune maximum,
			// but 5 bytes — above it. A width that leaks bytes only for short
			// kinds is caught here and by nothing else in the print path.
			name: "no Other row anywhere, so the floor must not apply",
			summary: &RebuildSummary{
				Group:         "g",
				TotalEntities: 60,
				EntityByKind: map[string]int{
					"fn":  30, // 2 runes
					"cls": 20, // 3 — the maximum
					"var": 10, // 3
				},
				TotalRelationships: 30,
				RelByKind: map[string]int{
					"USES": 20, // 4 — the maximum
					"HAS":  10, // 3
				},
				EnrichmentCandidates: 50,
				EnrichmentActions:    60,
				EnrichmentByKind: map[string]int{
					"abcd": 30, // 4 runes, 4 bytes — the rune maximum
					"ééa":  20, // 3 runes, 5 BYTES — non-maximal in runes, over it in bytes
					"tag":  10, // 3
				},
			},
			wantCol: map[string]int{
				"fn": 2 + 3 + 2, "cls": 2 + 3 + 2, "var": 2 + 3 + 2,
				"USES": 2 + 4 + 2, "HAS": 2 + 4 + 2,
				"abcd": 4 + 4 + 2, "ééa": 4 + 4 + 2, "tag": 4 + 4 + 2,
			},
			wantOther: map[string]int{}, // none may be printed
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintRebuildSummary(&buf, tc.summary)
			lines := strings.Split(buf.String(), "\n")

			// Absolute columns, not cross-line agreement. Agreement already
			// holds on unfixed code, and a mutant that widens the indent at
			// every site of one table keeps every line in agreement while
			// moving the whole column.
			for kind, want := range tc.wantCol {
				found := false
				for _, line := range lines {
					col, ok := countColumn(line, kind)
					if !ok {
						continue
					}
					found = true
					if col != want {
						t.Errorf("row %q: count starts at rune column %d, want %d\n  line: %q",
							kind, col, want, line)
					}
				}
				if !found {
					t.Errorf("row %q not found in output:\n%s", kind, buf.String())
				}
			}

			seenOther := map[string]bool{}
			for _, line := range lines {
				col, ok := countColumn(line, "Other")
				if !ok {
					continue
				}
				count := strings.Fields(line)[1]
				want, known := tc.wantOther[count]
				if !known {
					t.Errorf("unexpected Other row %q — this fixture expects overflow "+
						"totals %v and no others", line, tc.wantOther)
					continue
				}
				seenOther[count] = true
				if col != want {
					t.Errorf("Other row (count %s): count starts at rune column %d, want %d\n  line: %q",
						count, col, want, line)
				}
			}
			for c := range tc.wantOther {
				if !seenOther[c] {
					t.Errorf("Other row with count %s not found in output:\n%s", c, buf.String())
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// (2) the invariant: every declared kind constant is ASCII.
// ---------------------------------------------------------------------------

// kindConstantsFromSource parses internal/types/kinds.go — the source of truth
// for the kinds the entity and relationship tables render — and returns every
// value declared as an EntityKind or RelationshipKind constant, keyed by
// constant name.
//
// WHAT THIS ACTUALLY PINS. Written by reading the switch arms below and
// describing each one, because the previous four attempts at this comment were
// each falsified by a probe within a round.
//
// Each const spec is classified by a four-arm switch. In order:
//
//  1. `resolved && (typeName == "EntityKind" || typeName == "RelationshipKind")`
//     — the type resolved, through parentheses and file-local aliases and
//     definitions, to a kind type. The value is evaluated and checked for
//     ASCII, or the test FAILS naming the const if the value is not a plain
//     string literal.
//
//  2. `resolved && typeName != "" && nonStringLiteral(values)` — some other
//     NAMED type, and every value is a literal provably not a string.
//     SKIPPED SILENTLY, on two independent grounds.
//
//  3. `nonStringLiteral(values)` — no usable type name (untyped, or a type
//     expression that did not reduce to an identifier), but every value is
//     provably not a string. SKIPPED SILENTLY, on that one ground. This is
//     the arm that lets a plain `maxEntityKindNameLen = 64` sit in the
//     EntityKind block without complaint.
//
//  4. default — everything else: no explicit type with a string-ish value, an
//     unresolvable type expression with a string-ish value, or another named
//     type with a string-ish value. The test FAILS naming the const.
//
// So TWO arms skip in silence, 2 and 3, and both require the value to be
// provably a non-string literal. No spec with a string or string-ish value is
// ever skipped in silence. Coverage does not depend on the kind reaching
// types.AllEntityKinds().
//
// OUTSIDE this bound, and covered by nothing here: kinds declared in any other
// file, kinds that come from YAML rule files, and enrichment kinds read from
// JSON. See TestEngineTaxonomyKindLiteralsAreASCII and the note on maxKindLen.
//
// Each arm was added after a probe found the hole it closes, not by design:
// the value check in arm 1 after a Sprintf-valued const passed; arm 4's
// untyped case after `EntityKindFoo = "SCOPE.Foo"` passed; the parenthesis and
// alias resolution feeding arm 1 after `(EntityKind)` and a `= EntityKind`
// alias both passed.
func kindConstantsFromSource(t *testing.T) map[string]string {
	t.Helper()
	const path = "../types/kinds.go"

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	// nonStringLiteral reports whether every value in a spec is a literal that
	// is provably NOT a string (an int, float, rune or imaginary). Only such a
	// spec may be skipped without an explicit type: it cannot be a kind.
	nonStringLiteral := func(values []ast.Expr) bool {
		for _, e := range values {
			lit, ok := e.(*ast.BasicLit)
			if !ok || lit.Kind == token.STRING {
				return false
			}
		}
		return len(values) > 0
	}

	// Type aliases and definitions declared in this same file, so a const
	// written against `type aliasEK = EntityKind` resolves to EntityKind
	// instead of falling into a skip branch. Both `= T` (alias) and `T`
	// (definition) are followed: a defined type over EntityKind carries the
	// same string values and the same column arithmetic.
	aliasOf := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if id, ok := unparen(ts.Type).(*ast.Ident); ok {
				aliasOf[ts.Name.Name] = id.Name
			}
		}
	}
	// resolveKindType walks a type expression to the kind type it ultimately
	// names, following parentheses and file-local aliases. It returns the
	// resolved name and whether the expression could be resolved to an
	// identifier at all.
	resolveKindType := func(e ast.Expr) (string, bool) {
		id, ok := unparen(e).(*ast.Ident)
		if !ok {
			return "", false
		}
		name := id.Name
		for i := 0; i < 16; i++ { // bounded: a cycle must not hang the test
			if name == "EntityKind" || name == "RelationshipKind" {
				return name, true
			}
			next, ok := aliasOf[name]
			if !ok || next == name {
				break
			}
			name = next
		}
		return name, true
	}

	out := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		// Implicit repetition: a spec with neither a type nor values repeats
		// the previous spec's type AND value list. A spec that HAS values does
		// not inherit anything, whether or not it declares a type — carrying
		// the type across such a spec is what made a plain `n = 64` in this
		// block get reported as an EntityKind.
		var lastType string
		var lastValues []ast.Expr
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			typeName := ""
			resolved := true
			values := vs.Values
			switch {
			case vs.Type != nil:
				typeName, resolved = resolveKindType(vs.Type)
				lastType, lastValues = typeName, values
			case len(values) == 0:
				typeName, values = lastType, lastValues
			default:
				typeName = "" // untyped, or typed by its own expression
				lastType, lastValues = "", values
			}

			switch {
			case resolved && (typeName == "EntityKind" || typeName == "RelationshipKind"):
				// fall through to the value check below
			case resolved && typeName != "" && nonStringLiteral(values):
				// Explicitly some other type AND provably not a string. Two
				// independent reasons it cannot be a kind; skipping is safe.
				continue
			case nonStringLiteral(values):
				// Untyped, but provably not a string. Cannot be a kind.
				continue
			default:
				// Reached three ways, all with a string-ish value: no explicit
				// type, a type expression that did not reduce to a name, or
				// some other named type. An untyped string const such as
				// `EntityKindFoo = "SCOPE.Foo"` is a kind for every practical
				// purpose, reaches the graph like any other, and was covered by
				// nothing until this arm existed. Refuse to guess.
				shown := typeName
				if !resolved {
					shown = "<a type expression this test cannot resolve to a name>"
				} else if shown == "" {
					shown = "<no explicit type>"
				}
				for _, name := range vs.Names {
					t.Errorf("%s: const %s in %s has type %s, which this test cannot confirm "+
						"is NOT an EntityKind/RelationshipKind, and its value is not provably "+
						"a non-string literal — so it may be an unchecked kind. Write the type "+
						"as a plain EntityKind or RelationshipKind identifier (every kind in "+
						"this file does today), or give it a non-string value. Do NOT leave a "+
						"possible kind unchecked.",
						fset.Position(name.Pos()), name.Name, path, shown)
				}
				continue
			}

			for i, name := range vs.Names {
				if i >= len(values) {
					t.Errorf("%s: const %s (%s) has no value expression this test can read; "+
						"extend kindConstantsFromSource rather than leaving the kind unchecked",
						fset.Position(name.Pos()), name.Name, typeName)
					continue
				}
				lit, ok := values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Errorf("%s: const %s (%s) is not declared as a plain string literal, so "+
						"this test cannot evaluate it and cannot confirm it is ASCII. Either "+
						"declare it as a string literal or extend kindConstantsFromSource to "+
						"evaluate this shape — do NOT leave the kind unchecked.",
						fset.Position(name.Pos()), name.Name, typeName)
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

	// Vacuity floor, derived rather than hand-maintained: the registry
	// accessors enumerate the kinds this parse must at minimum have found.
	// A parse that narrows to a subset of the const blocks fails here, not
	// merely a parse that finds nothing at all.
	minKinds := len(types.AllEntityKinds()) + len(types.AllRelationshipKinds())
	if len(out) < minKinds {
		t.Fatalf("parsed %d EntityKind/RelationshipKind constants from %s, but the registry "+
			"accessors alone list %d — the declaration shape changed and this test is no "+
			"longer reading the source of truth", len(out), path, minKinds)
	}
	return out
}

func TestKindConstantsAreASCII(t *testing.T) {
	consts := kindConstantsFromSource(t)

	// Guard the parse from the other direction: every kind the registry
	// accessors return must have been seen by the parser. If the const block
	// is split across files or reshaped so the parse stops covering it, this
	// fails instead of silently checking a subset.
	//
	// This guard is NOT the coverage story on its own. types.AllEntityKinds()
	// is a hand-maintained slice (internal/types/kinds.go) and nothing
	// enforces that every declared const appears in it, so a const missing
	// from the registry would not be caught here. It is caught by the parse
	// above, which reads declarations directly and refuses shapes it cannot
	// evaluate.
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
				"relies on entity and relationship kinds being ASCII for byte length "+
				"and rune count to coincide. A non-ASCII kind is legal — but before "+
				"adding one, confirm the column arithmetic in rebuild_summary.go, and "+
				"note that it targets RUNE COUNT, not terminal display width (a CJK "+
				"ideograph is one rune and two columns).", name, v)
		}
	}
}

// TestEngineTaxonomyKindLiteralsAreASCII covers grafel's SECOND entity-kind
// taxonomy. internal/types/producer_kinds_test.go records that internal/engine
// emits unprefixed kinds ("Route", "Component", "Config") that are
// deliberately outside AllEntityKinds and therefore outside
// TestKindConstantsAreASCII — yet they reach RebuildSummary.EntityByKind
// through normaliseEntityKind's default branch just like any other kind.
//
// Bound, stated rather than implied: this reads `Kind: "..."` string literals
// out of the engine's Go source. Kinds this taxonomy takes from YAML rule
// files, and enrichment kinds read from enrichment-candidates.json, are NOT
// covered by any invariant in this file — which is precisely why the column
// arithmetic has to be correct rather than merely lucky.
// engineCensus counts, by an INDEPENDENT traversal, the non-test .go files
// directly under dir. It exists so the walk below has a denominator it cannot
// narrow along with its numerator.
//
// This deliberately uses filepath.WalkDir rather than the os.ReadDir the walk
// itself uses. A `parsed == len(eligible)` check where both sides come from
// one filtered listing is tautological: a line added to the filter moves the
// numerator and the denominator together and the test stays green. Measured —
// adding `if name > "n" { continue }` to that filter dropped 65 of 204 files
// (32%) and 7 of 53 literals with the suite still passing.
//
// An independent denominator is necessary but not sufficient: it only pins
// what the numerator actually counts. While the numerator was recorded on
// file OPEN, a skip placed between the record and ast.Inspect bypassed the
// census entirely without either traversal moving. The record now sits after
// inspection for that reason.
//
// The literal floor below is a backstop, not the guard — it cannot be the
// guard on the file axis, because 53 literals sit in only 21 of 204 files, so
// dropping most of the population costs almost no literals.
func engineCensus(t *testing.T, dir string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == dir {
				return nil
			}
			return fs.SkipDir // direct children only
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		out[name] = true
		return nil
	})
	if err != nil {
		t.Fatalf("census walk of %s: %v", dir, err)
	}
	return out
}

func TestEngineTaxonomyKindLiteralsAreASCII(t *testing.T) {
	const dir = "../engine"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	// Pass 1 — establish the population this walk will cover.
	var eligible []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		eligible = append(eligible, filepath.Join(dir, name))
	}

	// Loose floor on the population. internal/engine holds ~204 non-test files;
	// the exact count churns weekly so it is not pinned. This is a backstop for
	// the directory being emptied or moved — the census below, not this floor,
	// is what catches a narrowed enumeration.
	const minEngineFiles = 100
	if len(eligible) < minEngineFiles {
		t.Fatalf("found %d non-test .go files under %s, want at least %d — the engine "+
			"package moved or this walk is no longer enumerating it",
			len(eligible), dir, minEngineFiles)
	}

	// Pass 2 — parse, recording WHICH files were actually read.
	fset := token.NewFileSet()
	parsed := map[string]bool{}
	seen := 0
	for _, path := range eligible {
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Kind" {
				return true
			}
			lit, ok := kv.Value.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			seen++
			if !isASCII(v) {
				t.Errorf("%s: engine kind literal %q is not ASCII — it reaches the Kind "+
					"column through normaliseEntityKind's pass-through branch",
					fset.Position(lit.Pos()), v)
			}
			return true
		})
		// Recorded AFTER ast.Inspect, not after ParseFile. Recording it on
		// open pins which files were OPENED, and a narrowing placed one line
		// below the record — `if strings.Contains(path, "_edges") { continue }`
		// — then moves the numerator alone, invisibly to the census. Measured:
		// that mutant dropped 46 of 204 files (22.5%) with every guard here
		// green. This line marks a file covered only once it has actually been
		// inspected.
		parsed[filepath.Base(path)] = true
	}

	// The real guard: every file the INDEPENDENT census found must have been
	// inspected. The census re-walks the directory with a different API, so a
	// narrowing of the eligibility filter or of the parse loop moves only the
	// numerator and is caught here. What this does NOT protect against is a
	// bypass placed after the record above — hence the record sits last.
	census := engineCensus(t, dir)
	var missing []string
	for name := range census {
		if !parsed[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		show := missing
		if len(show) > 10 {
			show = show[:10]
		}
		t.Errorf("%d of %d non-test files under %s were never INSPECTED, so they are silently "+
			"outside this invariant: %v%s", len(missing), len(census), dir, show,
			map[bool]string{true: " …", false: ""}[len(missing) > len(show)])
	}
	// And nothing may be parsed that the census does not know about, so the
	// two enumerations are pinned to each other in both directions.
	for name := range parsed {
		if !census[name] {
			t.Errorf("inspected %s under %s, which the independent census did not find — "+
				"the two enumerations disagree", name, dir)
		}
	}

	// Loose floor on the literals: 53 today, measured, not guessed — most
	// engine producers reference a types.EntityKind* constant rather than
	// writing a bare string, which is why the count is far below the file
	// count. Set at 40 rather than 25: 25 tolerated losing more than half the
	// population, while 40 still leaves room for the literal→typed-constant
	// conversions that are an improvement and must not fail this test.
	const minKindLiterals = 40
	if seen < minKindLiterals {
		t.Fatalf(`collected %d Kind: "..." literals from %d files under %s, want at least %d — `+
			`the producer shape moved and this test is no longer reading it`,
			seen, len(parsed), dir, minKindLiterals)
	}
}

// normaliseEntityKindLiterals returns every string literal appearing in
// normaliseEntityKind's switch — both the raw kinds it matches on and the
// display names it returns — read out of rebuild_summary.go's own AST.
//
// Read rather than hand-copied on purpose: an earlier version of this test
// carried the raw-kind list inline, which is the defect class where the test
// and the code drift apart silently.
func normaliseEntityKindLiterals(t *testing.T) (cases, results []string) {
	t.Helper()
	const path = "rebuild_summary.go"

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "normaliseEntityKind" {
			fn = fd
			break
		}
	}
	if fn == nil {
		t.Fatalf("normaliseEntityKind not found in %s — this test no longer reads it", path)
	}

	str := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(lit.Value)
		return v, err == nil
	}

	// Per-clause accounting, not a global non-empty check. A bare
	// `len(cases) == 0` guard would stay green if the reader started skipping
	// all but one clause — the same vacuity shape as a narrowed file walk.
	clauses := 0
	ast.Inspect(fn, func(n ast.Node) bool {
		cc, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		if cc.List == nil { // the default clause: `return kind`, no literals
			return true
		}
		clauses++
		gotCase, gotResult := 0, 0
		for _, e := range cc.List {
			if s, ok := str(e); ok {
				cases = append(cases, s)
				gotCase++
			}
		}
		for _, stmt := range cc.Body {
			ret, ok := stmt.(*ast.ReturnStmt)
			if !ok {
				continue
			}
			for _, e := range ret.Results {
				if s, ok := str(e); ok {
					results = append(results, s)
					gotResult++
				}
			}
		}
		if gotCase == 0 || gotResult == 0 {
			t.Errorf("%s: normaliseEntityKind clause at this position yielded %d case "+
				"literals and %d returned literals; this reader no longer covers it",
				fset.Position(cc.Pos()), gotCase, gotResult)
		}
		return true
	})

	// Derived floor: every non-default clause must have contributed. A reader
	// that finds fewer clauses than the switch has is not reading the switch.
	wantClauses := 0
	ast.Inspect(fn, func(n ast.Node) bool {
		if cc, ok := n.(*ast.CaseClause); ok && cc.List != nil {
			wantClauses++
		}
		return true
	})
	if clauses != wantClauses || wantClauses == 0 {
		t.Fatalf("read %d of %d non-default clauses in normaliseEntityKind — "+
			"the switch shape changed and this test is no longer reading it",
			clauses, wantClauses)
	}
	if len(cases) < wantClauses || len(results) < wantClauses {
		t.Fatalf("read %d case literals and %d returned literals across %d clauses — "+
			"expected at least one of each per clause", len(cases), len(results), wantClauses)
	}
	return cases, results
}

// TestNormalisedEntityKindsAreASCII closes the display-name half: the Kind
// values the entity table actually renders are normaliseEntityKind's output,
// which is either one of its own display names or a pass-through of the raw
// kind.
func TestNormalisedEntityKindsAreASCII(t *testing.T) {
	// Every registered kind, through the function.
	for _, k := range types.AllEntityKinds() {
		if got := normaliseEntityKind(string(k)); !isASCII(got) {
			t.Errorf("normaliseEntityKind(%q) = %q is not ASCII", k, got)
		}
	}
	// Every raw kind the function itself names, and every display name it can
	// return — both read from its own source, not hand-copied here.
	cases, results := normaliseEntityKindLiterals(t)
	for _, k := range cases {
		if got := normaliseEntityKind(k); !isASCII(got) {
			t.Errorf("normaliseEntityKind(%q) = %q is not ASCII", k, got)
		}
	}
	for _, r := range results {
		if !isASCII(r) {
			t.Errorf("normaliseEntityKind can return %q, which is not ASCII", r)
		}
	}
}
