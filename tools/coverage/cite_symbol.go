package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Line citations inside `notes` prose — the convention, and why it is
// checked here (#6673).
//
// A capability cell's `cites` list is validated by path (see
// validateCapabilityCell). The `file.go:N` references embedded in the
// cell's `notes` prose were, until #6673, hand-typed text that no
// generator emitted and no check read. An audit of the 53 such
// citations in registry.json measured 21 wrong (40%) — and every single
// failure was drift onto a *different, plausible* line. Zero cited
// files were dead and zero cited lines were out of range, so the
// obvious cheap gate ("assert the cited line exists") would have caught
// 0 of 21 and gone green on a registry that was 40% wrong. It was
// rejected on that measurement.
//
// What is enforced instead: a line citation must name the symbol it
// points at, and the checker validates the *symbol*, not the number.
// Drift then fails loudly, because the symbol's declaration moves with
// the code while the hand-typed number does not.
//
// # The citation convention — two rules, not one
//
// Both rules were established by the #6673 audit and both are enforced
// by declLinesForSymbol/validateCiteSymbols below. Recording only the
// first one is what produced a wrong cleanup argument in #6671.
//
//  1. A SINGLE-LINE citation sits on the EXACT declaration line:
//
//     `synthesizeUtoipaAxumRoutes` (internal/engine/http_endpoint_utoipa_axum.go:445)
//
//     Citing the last line of the symbol's doc comment (the `-1` rot
//     #6671 corrected six instances of) is a defect, not a house style:
//     that position carries no meaning and doc-comment blocks vary in
//     length, so it is not even consistently "the first comment line".
//
//  2. A RANGE opens on the first doc-comment line and closes on the
//     declaration or somewhere in its body:
//
//     `cdkAddEventSourceRe` (internal/engine/cdk_edges.go:137-142)
//
//     The range form is deliberately kept — it is the useful and
//     consistently-applied "the comment through the declaration"
//     convention — so the checker requires the declaration to fall
//     WITHIN the range rather than at its start. The trade is stated
//     plainly: a range citation tolerates drift that stays inside the
//     range, where a single-line citation does not.
//
// # What is in the checked population, and what is not
//
// IN: every `path/to/file.go:N` or `path/to/file.go:N-M` token in any
// `notes` string, at ANY depth. The utoipa citations live at
// capabilities.Routing.route_extraction.notes rather than at
// capabilities.<cell>.notes, so a flat walk of `capabilities` sees 38
// of them and misses 15. This check hangs off validateCapabilityCell,
// which every tier (flat, grouped, framework_specific) routes through,
// so the recursion is structural rather than re-implemented here.
//
// Every such token MUST be symbol-anchored, and its path MUST be
// repo-relative. Bare basenames are rejected: `extractor.go` alone
// matches 50 files in this tree and is resolvable only from the
// surrounding prose.
//
// OUT: bare continuation refs — `,472-479`, `(:158-160)`, `(:587)` —
// which prose uses to add a second location for a file already named
// earlier in the sentence. These carry no file token, so deciding which
// file they belong to requires reading natural language; there is no
// mechanical resolution. They are left as prose. This is a stated
// limitation, not an oversight: the anchoring rule below still prevents
// a NEW `file.go:N` from entering the registry unanchored, which is the
// vector that produced the measured drift.
var (
	// citeLineRe defines the population: any file.go:N[-M] token.
	citeLineRe = regexp.MustCompile(`([A-Za-z0-9_./-]*[A-Za-z0-9_-]\.go):(\d+)(?:-(\d+))?`)

	// citeAnchorRe matches the symbol-anchored form. The symbol is
	// backticked and immediately precedes the citation, optionally
	// separated by a comma and/or an opening paren:
	//
	//	`Symbol` (path/to/file.go:N)
	//	(`Symbol`, path/to/file.go:N-M)
	citeAnchorRe = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_]*)`,?[ ]*\\(?([A-Za-z0-9_./-]*[A-Za-z0-9_-]\\.go):(\\d+)(?:-(\\d+))?")
)

// declIndex caches parsed declaration positions per file so a
// validation pass parses each cited source file at most once.
type declIndex struct {
	repoRoot string
	cache    map[string]map[string][]int
}

func newDeclIndex(repoRoot string) *declIndex {
	return &declIndex{repoRoot: repoRoot, cache: map[string]map[string][]int{}}
}

// declLinesForSymbol returns every line on which name is declared at
// package level in rel (a repo-relative .go path). Top-level funcs and
// methods, and every ValueSpec/TypeSpec inside a var/const/type block,
// are indexed — the registry anchors on all of these (regexp vars in a
// `var (…)` block are the single most-cited shape).
//
// ok is false when the file cannot be read or parsed; the caller
// reports that separately from "symbol not found" so a broken path is
// never silently reported as cite drift.
func (d *declIndex) declLinesForSymbol(rel, name string) (lines []int, ok bool) {
	byName, seen := d.cache[rel]
	if !seen {
		byName = d.indexFile(rel)
		d.cache[rel] = byName
	}
	if byName == nil {
		return nil, false
	}
	return byName[name], true
}

func (d *declIndex) indexFile(rel string) map[string][]int {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(d.repoRoot, rel), nil, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	out := map[string][]int{}
	add := func(name string, pos token.Pos) {
		if name == "" || name == "_" {
			return
		}
		out[name] = append(out[name], fset.Position(pos).Line)
	}
	for _, decl := range f.Decls {
		switch dd := decl.(type) {
		case *ast.FuncDecl:
			// The declaration line is the `func` keyword's line,
			// which is what the convention cites.
			add(dd.Name.Name, dd.Pos())
		case *ast.GenDecl:
			for _, spec := range dd.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					for _, n := range s.Names {
						add(n.Name, s.Pos())
					}
				case *ast.TypeSpec:
					add(s.Name.Name, s.Pos())
				}
			}
		}
	}
	return out
}

// validateCiteSymbols enforces the citation convention documented above
// against one capability cell's notes prose.
func validateCiteSymbols(res *ValidationResult, capPrefix string, cap Capability, idx *declIndex) {
	notes := cap.Notes
	if notes == "" {
		return
	}

	anchoredAt := map[int]struct{}{}
	for _, m := range citeAnchorRe.FindAllStringSubmatchIndex(notes, -1) {
		anchoredAt[m[4]] = struct{}{} // start offset of the path group
	}

	// Rule A: every citation in the population must be anchored.
	for _, m := range citeLineRe.FindAllStringSubmatchIndex(notes, -1) {
		if _, ok := anchoredAt[m[2]]; ok {
			continue
		}
		res.Errors = append(res.Errors, fmt.Sprintf(
			"%s: notes cite %q is not symbol-anchored — write it as `Symbol` (path/to/file.go:N) so the symbol, not the line number, is what is checked (#6673)",
			capPrefix, notes[m[0]:m[1]]))
	}

	// Rule B: every anchored citation must resolve to its symbol.
	for _, m := range citeAnchorRe.FindAllStringSubmatch(notes, -1) {
		symbol, rel, loStr, hiStr := m[1], m[2], m[3], m[4]
		cite := rel + ":" + loStr
		if hiStr != "" {
			cite += "-" + hiStr
		}
		if !strings.Contains(rel, "/") {
			res.Errors = append(res.Errors, fmt.Sprintf(
				"%s: notes cite %q uses a bare basename — cite the repo-relative path (#6673)", capPrefix, cite))
			continue
		}
		lo, _ := strconv.Atoi(loStr)
		hi := lo
		if hiStr != "" {
			hi, _ = strconv.Atoi(hiStr)
		}
		if hi < lo {
			res.Errors = append(res.Errors, fmt.Sprintf(
				"%s: notes cite %q has an inverted line range", capPrefix, cite))
			continue
		}
		if _, err := os.Stat(filepath.Join(idx.repoRoot, rel)); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf(
				"%s: notes cite %q refers to a file not found on disk", capPrefix, cite))
			continue
		}
		lines, ok := idx.declLinesForSymbol(rel, symbol)
		if !ok {
			res.Errors = append(res.Errors, fmt.Sprintf(
				"%s: notes cite %q — %s could not be parsed as Go", capPrefix, cite, rel))
			continue
		}
		if len(lines) == 0 {
			res.Errors = append(res.Errors, fmt.Sprintf(
				"%s: notes cite %q names symbol %q, which is not declared at package level in %s (#6673)",
				capPrefix, cite, symbol, rel))
			continue
		}
		hit := false
		for _, l := range lines {
			if l >= lo && l <= hi {
				hit = true
				break
			}
		}
		if !hit {
			sort.Ints(lines)
			got := make([]string, 0, len(lines))
			for _, l := range lines {
				got = append(got, strconv.Itoa(l))
			}
			res.Errors = append(res.Errors, fmt.Sprintf(
				"%s: notes cite %q is stale — %s is declared at %s:%s (#6673)",
				capPrefix, cite, symbol, rel, strings.Join(got, ",")))
		}
	}
}
