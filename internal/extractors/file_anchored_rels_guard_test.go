// Package extractors — file_anchored_rels_guard_test.go
//
// Issue #6298. A guard against the shape @arthurgeron found in Solidity
// (#6295 / #6297) and named in verilog, re-derived from source on every run so
// it catches the NEXT extractor, not just the ones already fixed.
//
// THE SHAPE. An extractor emits a relationship record with FromID set to the
// source file's path. That value is non-empty and non-hex, so
// ReferencesEmbeddedWithAllowlist (internal/resolve/refs.go) rewrites it, and
// both graph-assembly paths — cmd/grafel/index.go's record loop and
// relRecordToGraphRel in internal/extractors/incremental.go — substitute the
// owning record's own entity id ONLY when FromID is EMPTY. So a path-valued
// FromID on a record that is NOT file-scoped goes one of two ways:
//
//   - the package also emits an extractor.FileEntity, and the rewrite lands on
//     the FILE. The edge leaves the file rather than the type that declares it,
//     and several types in one file merge their edges onto that one node. This
//     is Solidity's and verilog's failure. MISANCHORED.
//   - the package emits no FileEntity, so nothing carries that id and the raw
//     path reaches the graph verbatim. This is astro's and svelte's failure.
//     DANGLING — worse, because there is no node at the other end at all.
//
// WHY A SOURCE SCAN AND NOT A RUNTIME ONE. The runtime form of this check —
// drive every registered extractor and inspect the emitted records — needs a
// syntactically valid sample per language, and there is no such corpus in the
// tree (testdata/fixtures covers 27 languages; astro, verilog and solidity are
// all absent, which is exactly why the benchmark never caught #6295). A table
// of hand-written snippets would be a curated list of languages wearing a
// derive-shaped coat: it would grow only when someone remembered to grow it.
// The source scan derives its candidate set from the extractor tree itself, so
// a new extractor is covered the day it lands, with no sample needed.
//
// WHAT IT CANNOT SEE. Only composite literals with a FromID key whose value is
// a syntactically recognisable file-path expression (see filePathExprs). A
// path reaching FromID through a variable of another name, a helper's return
// value, or a post-construction assignment is invisible to it. That is a real
// gap, accepted: this is a guard against the careless repetition of a known
// pattern, not a proof.
package extractors

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// filePathExprs are the expression spellings this repo's extractors use for
// "the path of the file being extracted". Lower-cased before comparison.
var filePathExprs = map[string]bool{
	"filepath": true, "path": true, "frompath": true, "srcpath": true, "relpath": true,
	"file.path": true, "f.path": true, "fi.path": true, "input.path": true, "src.path": true,
}

// allowedFileAnchored maps "<extractor package>:<Kind>" to the reason that site
// is correct or knowingly unfixed. Keyed by package + Kind rather than by line
// so it does not rot when code moves.
//
// IMPORTS is not listed and is never reported: refs.go:3658-3669 documents
// file-anchored FromID as the deliberate cross-language convention for import
// edges (#120). An import statement belongs to a file; a type relationship
// belongs to a type.
var allowedFileAnchored = map[string]string{
	// ── Correct: the owning record is genuinely file-scoped ──────────────────
	"vhdl:USES":    "file → toolchain component; the owning record is a file-scoped component (ruled out in #6298)",
	"verilog:USES": "same file→tool shape as vhdl's, on a file-scoped tool component (ruled out in #6298)",
	"graphql:CONTAINS": "the owning record is an explicit synthetic file-level container (Name: filePath), " +
		"so this is a benign self-reference (ruled out in #6298)",
	"graphql:FEDERATES": "file → federated type; the subgraph IS the file (subgraph_ref property is the same path)",
	"proto:CONTAINS":    "fileContainsRel by name and by doc comment: file.Path → top-level entity",
	"hcl:CONTAINS":      "file → locals block member; HCL entities are addressed per-file structural refs",
	"hcl:DEPENDS_ON":    "file → sibling resource, bound via byLocation on the same file (issue #44)",
	"bicep:DEPENDS_ON":  "file → locally-declared symbolic resource/module, same per-file structural-ref scheme",
	"dockerfile:USES":   "file → build stage / base image; a Dockerfile has no type layer to anchor on",
	"yaml:OVERRIDES":    "Helm values file → subchart values key; the values FILE is the overriding thing",

	// ── Known offenders, deliberately NOT fixed in #6298 ─────────────────────
	//
	// svelte has the astro failure, not the verilog one: no extractor.FileEntity
	// anywhere in the package, and entities[0] is the component named from the
	// file's basename. MEASURED through ResolveImports → ReferencesEmbedded on a
	// two-file snippet (Card.svelte renders <Button />):
	//
	//	owner="Card" kind=RENDERS FROM=<UNRESOLVED:src/lib/Card.svelte> TO=Button[SCOPE.Component]
	//
	// The ToID bound; the FromID did not. The other three kinds below are the
	// same construction on the same entities[0] record — NOT separately measured,
	// so treat that as inference, not a finding.
	//
	// Left out of #6298's diff on purpose: eleven sites across a language with
	// its own behavioural tests, none of which this issue reviewed. Fixing it is
	// its own change with its own measurement.
	"svelte:RENDERS":      "KNOWN OFFENDER (#6298): dangling FromID, measured. Out of scope; fix separately.",
	"svelte:USES":         "KNOWN OFFENDER (#6298): same construction as svelte:RENDERS, not separately measured.",
	"svelte:NAVIGATES_TO": "KNOWN OFFENDER (#6298): same construction as svelte:RENDERS, not separately measured.",
	"svelte:CONTAINS":     "KNOWN OFFENDER (#6298): same construction as svelte:RENDERS, not separately measured.",
}

type fileAnchoredSite struct {
	pkg, key, file string
	line           int
	fromExpr       string
}

// scanFileAnchoredRels walks root for non-test Go sources and returns every
// composite literal that sets FromID to a file-path expression alongside a
// Kind that is not IMPORTS.
func scanFileAnchoredRels(t *testing.T, root string) []fileAnchoredSite {
	t.Helper()
	fset := token.NewFileSet()
	var out []fileAnchoredSite

	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			t.Errorf("parse %s: %v", p, perr)
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		pkg := filepath.Dir(rel)
		// Nested extractor groups (cross/httpclient, …) keep their full path so
		// the allow-list key stays unambiguous.
		if pkg == "." {
			pkg = "extractors"
		}
		ast.Inspect(f, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			var from, kind ast.Expr
			for _, el := range cl.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				id, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch id.Name {
				case "FromID":
					from = kv.Value
				case "Kind":
					kind = kv.Value
				}
			}
			if from == nil || kind == nil {
				return true
			}
			if !filePathExprs[strings.ToLower(exprString(from))] {
				return true
			}
			k := relKindString(kind)
			if k == "IMPORTS" {
				return true
			}
			pos := fset.Position(cl.Pos())
			out = append(out, fileAnchoredSite{
				pkg:      pkg,
				key:      pkg + ":" + k,
				file:     rel,
				line:     pos.Line,
				fromExpr: exprString(from),
			})
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	slices.SortFunc(out, func(a, b fileAnchoredSite) int {
		if a.file != b.file {
			return strings.Compare(a.file, b.file)
		}
		return a.line - b.line
	})
	return out
}

// relKindString renders a Kind expression as the edge kind. A plain string
// literal is unquoted; `string(types.RelationshipKindOverrides)` and the bare
// constant both reduce to OVERRIDES. Anything else is returned verbatim so an
// unrecognised spelling shows up as an unknown key rather than passing quietly.
func relKindString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		return strings.Trim(v.Value, `"`)
	case *ast.CallExpr:
		if len(v.Args) == 1 {
			if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "string" {
				return relKindString(v.Args[0])
			}
		}
	case *ast.SelectorExpr:
		return strings.ToUpper(strings.TrimPrefix(v.Sel.Name, "RelationshipKind"))
	case *ast.Ident:
		return strings.ToUpper(strings.TrimPrefix(v.Name, "RelationshipKind"))
	}
	return exprString(e)
}

func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	case *ast.BasicLit:
		return v.Value
	case *ast.CallExpr:
		return exprString(v.Fun) + "(…)"
	case *ast.BinaryExpr:
		return exprString(v.X) + v.Op.String() + exprString(v.Y)
	}
	return "<expr>"
}

// TestNoNewFileAnchoredTypeRelationships fails when an extractor emits a
// non-IMPORTS relationship anchored on the source file path from a site the
// allow-list above does not account for.
func TestNoNewFileAnchoredTypeRelationships(t *testing.T) {
	sites := scanFileAnchoredRels(t, ".")
	if len(sites) == 0 {
		t.Fatal("scanner found no file-anchored relationship literals at all — " +
			"the walk or the AST match has broken, and a guard that matches " +
			"nothing passes for free")
	}

	var unaccounted []string
	for _, s := range sites {
		if _, ok := allowedFileAnchored[s.key]; !ok {
			unaccounted = append(unaccounted, fmt.Sprintf("%s:%d  FromID: %s  (key %q)",
				s.file, s.line, s.fromExpr, s.key))
		}
	}
	if len(unaccounted) > 0 {
		t.Errorf("non-IMPORTS relationship(s) anchored on the source file path (#6298):\n  %s\n\n"+
			"Leave FromID EMPTY so graph assembly stamps the owning record's own entity id\n"+
			"(cmd/grafel/index.go and relRecordToGraphRel in internal/extractors/incremental.go).\n"+
			"If the owning record really IS file-scoped, add the key to allowedFileAnchored\n"+
			"in this file with the reason.",
			strings.Join(unaccounted, "\n  "))
	}

	// A stale allow-list entry is its own defect: it hides the next offender
	// under a key nobody is watching any more.
	present := make(map[string]bool, len(sites))
	for _, s := range sites {
		present[s.key] = true
	}
	for key := range allowedFileAnchored {
		if !present[key] {
			t.Errorf("allowedFileAnchored[%q] matches no site any more — delete it", key)
		}
	}
}
