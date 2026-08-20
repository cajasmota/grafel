// hierarchy.go — Groovy inheritance topology: `extends` → EXTENDS,
// `implements` → IMPLEMENTS (#6370).
//
// Before this, groovy emitted NO hierarchy edge by either of the two paths a
// language can get one: it is absent from `supportedLanguages` in
// `internal/extractors/cross/hierarchy/extractor.go`, and this extractor never
// looked at the clause. "What extends this type" returned empty, which is
// indistinguishable from "nothing does".
//
// # Why the edges are emitted HERE and not by registering groovy in cross/hierarchy
//
// That pass invents its own graph nodes: for every class it addEntity's a
// SCOPE.Component for the class AND another for each parent (see extractRuby at
// extractor.go:565-579, the same shape extractJTCSharp uses). `buildClass` in
// this package already emits one SCOPE.Component per groovy class, so
// registering groovy there would mint a duplicate component per type, with the
// edges anchored on the pass's own node rather than on the one the rest of the
// groovy graph uses. That is exactly why #6335 emitted F#'s edges from the F#
// extractor. TestGroovyHierarchy_NoDuplicateComponents guards it.
//
// # Why the header is parsed as TEXT and not walked as a CST
//
// The groovy grammar has no `implements` clause. Measured on the real grammar,
// `class A implements Runnable {}` parses as
//
//	class_definition[ class, ERROR[ identifier(A), identifier(implements) ],
//	                  identifier(Runnable), closure ]
//
// — the clause collapses into an ERROR node whose contents and position vary
// with the shape of the header (`extends` alone parses cleanly; `extends` with
// a dotted or generic base does not). Any child-walk over that is a walk over
// error-recovery detail. The header — everything before the class body's
// opening brace — is short, and reading it as text is stable across all of
// those shapes.
//
// That same ERROR node is why `className` lives here too: `buildClass` picked
// the first direct `identifier` child, which for `class A implements Runnable`
// is `Runnable` (A is swallowed by the ERROR node) — the class entity was
// literally named after its interface. An edge embedded on a record named after
// its own target is a self-edge, so naming had to be correct before the edge
// could be, per #6369. Fixed here for groovy only; the CST lookup remains the
// fallback for any header shape the regex does not recognise.
package groovy

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
)

var (
	// The declaration keyword and the declared name. `@interface` is Groovy's
	// annotation-type form; `\bclass` would match its `interface` tail, so the
	// alternation puts the longest form first and anchors on a word boundary.
	groovyTypeHeaderRE = regexp.MustCompile(`(?s)\b(@interface|class|interface|trait|enum)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	groovyExtendsRE    = regexp.MustCompile(`(?s)\bextends\b`)
	groovyImplementsRE = regexp.MustCompile(`(?s)\bimplements\b`)
)

// typeHeader returns the source text of a class_definition up to (excluding)
// its body, i.e. everything the `extends`/`implements` clauses can live in.
// Comments and string literals are blanked, preserving byte offsets so line
// stamping stays exact.
func typeHeader(node ts.Node, src []byte) string {
	end := node.EndByte()
	if body := childByType(node, "closure"); body != nil {
		end = body.StartByte()
	}
	start := node.StartByte()
	if int(end) > len(src) || start >= end {
		return ""
	}
	return scrubGroovy(string(src[start:end]))
}

// scrubGroovy blanks line comments, block comments and string literals, keeping
// every byte offset (and every newline) where it was.
func scrubGroovy(s string) string {
	out := []byte(s)
	blank := func(i, j int) {
		for ; i < j && i < len(out); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '/':
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				blank(i, len(s))
				return string(out)
			}
			blank(i, i+j)
			i += j
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '*':
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				blank(i, len(s))
				return string(out)
			}
			blank(i, i+2+j+2)
			i += 2 + j + 1
		case s[i] == '"' || s[i] == '\'':
			q := s[i]
			j := i + 1
			for j < len(s) && s[j] != q {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(s) {
				blank(i, len(s))
				return string(out)
			}
			blank(i, j+1)
			i = j
		}
	}
	return string(out)
}

// className returns the declared name from a scrubbed type header, or "" when
// the header has no recognisable declaration keyword.
func className(header string) string {
	m := groovyTypeHeaderRE.FindStringSubmatch(header)
	if m == nil {
		return ""
	}
	return m[2]
}

// splitTypeList splits an inheritance clause into its declared type names,
// erasing generic arguments: `Mixin<A, B>, Plain` → ["Mixin", "Plain"].
//
// Everything at angle-depth > 0 is dropped rather than accumulated, which is
// what makes the comma handling unconditional: the comma inside `<A, B>` splits
// an already-empty segment, and empty segments are discarded. Guarding the
// split on depth == 0 as well would be dead weight — a mutation removing that
// guard survived the suite, because it cannot change the result.
func splitTypeList(clause string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, t)
		}
		cur.Reset()
	}
	for i := 0; i < len(clause); i++ {
		switch c := clause[i]; c {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ',':
			flush()
		default:
			if depth == 0 {
				cur.WriteByte(c)
			}
		}
	}
	flush()
	return out
}

// collectHierarchyEdges returns the EXTENDS/IMPLEMENTS edges declared by one
// class_definition header.
//
// The records are meant to be EMBEDDED on the owning type's EntityRecord, not
// appended to a standalone slice: only resolve.ReferencesEmbedded supplies the
// parent's file and package dir, which the locality tiers rank on.
//
// FromID is deliberately left EMPTY. The assembly loop stamps the owning
// record's own entity id, the only value that anchors the edge on the TYPE.
// A non-empty non-hex FromID (e.g. the file path) would be rewritten by
// ReferencesEmbedded onto the FILE entity, merging every type in a multi-type
// file onto one node — the defect fixed in #6295 (Solidity) and #6298 (Verilog,
// Astro) and now guarded by internal/extractors/file_anchored_rels_guard_test.go
// (#6367).
//
// ToID is the bare written type name with generic arguments erased, matching
// the Solidity/Crystal/F# convention: a base type is usually declared in
// another file, so a file-pinned structural ref would never bind.
func collectHierarchyEdges(header, owner string, startLine int) []types.RelationshipRecord {
	if header == "" || owner == "" {
		return nil
	}
	ext, impl := "", ""
	if m := groovyImplementsRE.FindStringIndex(header); m != nil {
		impl = header[m[1]:]
		header = header[:m[0]]
	}
	if m := groovyExtendsRE.FindStringIndex(header); m != nil {
		ext = header[m[1]:]
	}

	var out []types.RelationshipRecord
	seen := map[string]bool{}
	add := func(kind, clause string) {
		for _, target := range splitTypeList(clause) {
			// A self-edge is never information; it is the signature of a
			// mis-attributed owner (#6369).
			if target == "" || target == owner {
				continue
			}
			key := kind + ":" + target
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, types.RelationshipRecord{
				// FromID intentionally empty — see the doc comment above.
				ToID: target,
				Kind: kind,
				Properties: types.Props{
					{K: "line", V: strconv.Itoa(startLine)},
				},
			})
		}
	}
	add("EXTENDS", ext)
	add("IMPLEMENTS", impl)
	return out
}

// classHierarchy is the seam buildClass uses: it returns the declared name (or
// "" to fall back to the CST lookup) and the header's inheritance edges.
func classHierarchy(node ts.Node, file extractor.FileInput) (string, []types.RelationshipRecord) {
	header := typeHeader(node, file.Content)
	name := className(header)
	if name == "" {
		return "", nil
	}
	return name, collectHierarchyEdges(header, name, int(node.StartPoint().Row)+1)
}
