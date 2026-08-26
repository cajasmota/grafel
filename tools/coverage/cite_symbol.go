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
//     BOTH ends are bounded, and both bounds are load-bearing. The
//     declaration must fall within the range; the range must OPEN
//     exactly on the declaration's first doc-comment line (the
//     declaration line itself when it carries no doc comment); and it
//     must CLOSE no later than the last line of the declaration —
//     `ast.Node.End()`, i.e. the closing paren of a multi-line
//     `regexp.MustCompile(…)` or the closing brace of a func body.
//
//     The two bounds catch different defects and neither substitutes
//     for the other, which is worth stating precisely because it is
//     easy to credit the wrong one:
//
//     - The OPENING bound rejects `terraform_deep.go:1-900` — but
//     for opening at line 1, NOT for its width. With only the
//     opening bound in place, width remained completely
//     unlimited: `cdk_edges.go:137-900` opened on the correct
//     doc-comment line and was accepted, claiming a 764-line span
//     that ran 366 lines past the end of a 534-line file. So did
//     `terraform_deep.go:220-9999`.
//     - The CLOSING bound is what rejects those. It is the only
//     width limit in the rule.
//
//     Both gaps were measured on the shipped registry, not imagined.
//
//     Enforcing both cost six registry corrections in total (five at
//     the opening, one at the closing), every one of them rot of the
//     same class this issue is about. A citation whose hi exceeds the
//     file's line count is reported separately, because that is wrong
//     independently of the symbol it names.
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
// OUT, and both exclusions are deliberate rather than overlooked:
//
//   - Bare continuation refs — `,472-479`, `(:158-160)`, `(:587)` —
//     which prose uses to add a second location for a file already
//     named earlier in the sentence. These carry no file token, so
//     deciding which file they belong to requires reading natural
//     language; there is no mechanical resolution.
//
//   - Line refs into NON-Go files. The registry carries five
//     (`aws_cdk.yaml:54-58` and four `lang.rust.framework.*.md:21`).
//     They are the same hand-typed-number defect class, but the check
//     validates a citation by resolving a SYMBOL DECLARATION, and a
//     YAML key or a Markdown table row has no declaration to resolve —
//     they are unanchorable for the same reason as the 23 statement-
//     block numbers this PR stripped. Owning them would mean a second,
//     differently-shaped checker per file format; that is a separate
//     piece of work and is not smuggled in here.
//
// Both are left as prose. This is a stated limitation: the anchoring
// rule below still prevents a NEW `file.go:N` from entering the
// registry unanchored, which is the vector that produced the measured
// drift.
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

// declSite is one package-level declaration of a symbol: the line the
// declaration itself sits on, and the line its doc comment opens on
// (equal to Line when the declaration carries no doc comment).
//
// Both are needed because the citation convention is two rules and the
// second one constrains where a range may OPEN — see the package doc.
type declSite struct {
	Line     int
	DocStart int
	// End is the last line of the declaration itself — the closing
	// paren of a multi-line `regexp.MustCompile(…)`, the closing brace
	// of a func body. It is the upper bound of the range half of the
	// convention ("closes on the declaration or body").
	End int
}

// declIndex caches parsed declaration positions per file so that one
// validation pass parses each cited source file at most once. The index
// is created once per validateRegistry call and threaded down to every
// cell, so the ~20 cited source files are parsed ~20 times per run, not
// once per citing cell.
type declIndex struct {
	repoRoot string
	cache    map[string]*fileDecls
}

// fileDecls is one parsed source file: its package-level declarations
// by name, and its length. The length is carried because a citation
// running past EOF is its own defect and gets its own message — a
// range can otherwise claim a span the file does not have.
type fileDecls struct {
	byName map[string][]declSite
	lines  int
}

func newDeclIndex(repoRoot string) *declIndex {
	return &declIndex{repoRoot: repoRoot, cache: map[string]*fileDecls{}}
}

// fileLineCount returns the number of lines in rel. ok is false when
// the file could not be parsed.
func (d *declIndex) fileLineCount(rel string) (n int, ok bool) {
	fd := d.load(rel)
	if fd == nil {
		return 0, false
	}
	return fd.lines, true
}

func (d *declIndex) load(rel string) *fileDecls {
	fd, seen := d.cache[rel]
	if !seen {
		fd = d.indexFile(rel)
		d.cache[rel] = fd
	}
	return fd
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
func (d *declIndex) declSitesForSymbol(rel, name string) (sites []declSite, ok bool) {
	fd := d.load(rel)
	if fd == nil {
		return nil, false
	}
	return fd.byName[name], true
}

func (d *declIndex) indexFile(rel string) *fileDecls {
	fset := token.NewFileSet()
	// ParseComments is required: the range half of the convention is
	// anchored on where the doc comment OPENS, so the comment positions
	// are load-bearing, not decoration.
	f, err := parser.ParseFile(fset, filepath.Join(d.repoRoot, rel), nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	out := map[string][]declSite{}
	add := func(name string, pos, end token.Pos, doc *ast.CommentGroup) {
		if name == "" || name == "_" {
			return
		}
		line := fset.Position(pos).Line
		docStart := line
		if doc != nil {
			docStart = fset.Position(doc.Pos()).Line
		}
		out[name] = append(out[name], declSite{Line: line, DocStart: docStart, End: fset.Position(end).Line})
	}
	for _, decl := range f.Decls {
		switch dd := decl.(type) {
		case *ast.FuncDecl:
			// The declaration line is the `func` keyword's line,
			// which is what the convention cites.
			add(dd.Name.Name, dd.Pos(), dd.End(), dd.Doc)
		case *ast.GenDecl:
			for _, spec := range dd.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					// A non-parenthesised `var X = …` carries its doc
					// on the GenDecl; a member of a `var (…)` block
					// carries its own.
					doc := s.Doc
					if doc == nil && dd.Lparen == token.NoPos {
						doc = dd.Doc
					}
					for _, n := range s.Names {
						add(n.Name, s.Pos(), s.End(), doc)
					}
				case *ast.TypeSpec:
					doc := s.Doc
					if doc == nil && dd.Lparen == token.NoPos {
						doc = dd.Doc
					}
					add(s.Name.Name, s.Pos(), s.End(), doc)
				}
			}
		}
	}
	return &fileDecls{byName: out, lines: fset.File(f.Pos()).LineCount()}
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
		sites, ok := idx.declSitesForSymbol(rel, symbol)
		if !ok {
			res.Errors = append(res.Errors, fmt.Sprintf(
				"%s: notes cite %q — %s could not be parsed as Go", capPrefix, cite, rel))
			continue
		}
		// A citation running past EOF is its own defect and gets its
		// own message: it is wrong independently of which symbol it
		// names, so reporting it as symbol drift would misdirect the
		// reader. Checked before the symbol for that reason.
		if n, okLines := idx.fileLineCount(rel); okLines && hi > n {
			res.Errors = append(res.Errors, fmt.Sprintf(
				"%s: notes cite %q runs past the end of %s, which has %d lines (#6673)",
				capPrefix, cite, rel, n))
			continue
		}
		if len(sites) == 0 {
			res.Errors = append(res.Errors, fmt.Sprintf(
				"%s: notes cite %q names symbol %q, which is not declared at package level in %s (#6673)",
				capPrefix, cite, symbol, rel))
			continue
		}
		// Rule 1 (single line) and rule 2 (range) are both applied
		// here, in three parts:
		//
		//   inRange     — shared: the declaration is covered by the
		//                 citation.
		//   opensRight  — range only: it OPENS on the declaration's
		//                 first doc-comment line. This rejects a range
		//                 that starts anywhere else, including one
		//                 starting on the declaration line itself and
		//                 skipping the doc comment — the exact shape of
		//                 two of the five corrections this rule forced.
		//   closesRight — range only: it CLOSES no later than the last
		//                 line of the declaration. This is what bounds
		//                 the WIDTH; opensRight does not. A range
		//                 opening correctly and closing anywhere was
		//                 accepted until this half existed.
		inRange, opensRight, closesRight := false, false, false
		wantOpen, wantClose := 0, 0
		for _, st := range sites {
			if st.Line < lo || st.Line > hi {
				continue
			}
			inRange = true
			if hiStr != "" && lo != st.DocStart {
				wantOpen = st.DocStart
				continue
			}
			opensRight = true
			if hiStr == "" || hi <= st.End {
				closesRight = true
				break
			}
			wantClose = st.End
		}
		switch {
		case !inRange:
			lines := make([]int, 0, len(sites))
			for _, st := range sites {
				lines = append(lines, st.Line)
			}
			sort.Ints(lines)
			got := make([]string, 0, len(lines))
			for _, l := range lines {
				got = append(got, strconv.Itoa(l))
			}
			res.Errors = append(res.Errors, fmt.Sprintf(
				"%s: notes cite %q is stale — %s is declared at %s:%s (#6673)",
				capPrefix, cite, symbol, rel, strings.Join(got, ",")))
		case !opensRight:
			res.Errors = append(res.Errors, fmt.Sprintf(
				"%s: notes cite %q opens at line %d, but the doc comment of %s opens at %s:%d — a range citation opens on the first doc-comment line (#6673)",
				capPrefix, cite, lo, symbol, rel, wantOpen))
		case !closesRight:
			res.Errors = append(res.Errors, fmt.Sprintf(
				"%s: notes cite %q closes at line %d, past the end of %s, whose declaration ends at %s:%d — a range citation closes on the declaration or in its body (#6673)",
				capPrefix, cite, hi, symbol, rel, wantClose))
		}
	}
}
