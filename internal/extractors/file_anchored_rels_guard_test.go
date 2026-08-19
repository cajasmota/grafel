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
//   - some entity in the same extraction carries that exact path as its Name /
//     QualifiedName (an extractor.FileEntity does, and so does any hand-rolled
//     synthetic container named after the path), and the rewrite lands on THAT
//     node. The edge leaves the file rather than the type that declares it, and
//     several types in one file merge their edges onto that one node. This is
//     Solidity's and verilog's failure. MISANCHORED.
//   - nothing carries that id, so the raw path reaches the graph verbatim. This
//     is astro's and svelte's failure. DANGLING — worse, because there is no
//     node at the other end at all.
//
// THE RESOLUTION RULE, PRECISELY. internal/resolve/refs.go has no path→entity
// index. A path-valued FromID resolves if and only if some emitted node carries
// that exact string as Name / QualifiedName. looksLikeSourceFilePath
// (refs.go:2729) does NOT resolve anything — it routes the reference to
// DispositionDynamic, which suppresses the bug-extractor metric while leaving
// the edge dangling. Anything whose extension is missing from
// sourceFileExtensions (refs.go:2752-2767 — `.bicep` and `Dockerfile` are)
// misses even that and counts as DispositionBugExtractor. "The owning record is
// conceptually file-scoped" is therefore NOT the criterion; "a node carrying
// that exact path string exists" is. The first round of this allow-list used the
// conceptual criterion and, on the strength of it, blessed six entries across
// five languages that are real defects — they are re-labelled KNOWN OFFENDER
// below rather than fixed, because each needs its own measurement.
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
// ── WHAT IT CAN AND CANNOT SEE ──────────────────────────────────────────────
//
// A statement of the gap, not a disclaimer. Every line below was established by
// running a probe package against this scan and reading the output, not by
// reasoning about the code. Probe results, #6365 review round 2:
//
// THE ONE THING THAT MATTERS FIRST: this scan bottoms out on a SPELLING LIST at
// the leaves. Every structural form below is recognised only when the value it
// ultimately reads is a bare identifier in filePathIdents or a selector whose
// FIELD name is in filePathFields. A probe with FromID: fp — a plain parameter
// named `fp` holding the file path — was NOT seen. Structure is handled; naming
// is not, and cannot be without type information this scan does not build.
//
// SEEN (probe made the test fail, at the exact line):
//
//	A. post-construction assignment — `rel.FromID = file.Path`, record appended
//	   elsewhere. LIVE IN-TREE at internal/extractors/yaml/helm.go:875, whose
//	   Kind is set in a different literal (helm.go:918 INCLUDES, helm.go:931
//	   BINDS), so it lands with kind "?". This form existed before this guard was
//	   written, which is why "composite literals only" was never a safe
//	   assumption. Intra-function only.
//	B. intra-function alias — `sourceOfTruth := filePath; …FromID: sourceOfTruth`.
//	   ONE hop, no fixed-point iteration, no crossing of function boundaries or
//	   struct fields, and only from a PURE path RHS: `x := "scope:…:" + path` is
//	   a structural ref, not a path, and is deliberately not aliased.
//	C. any struct field whose FIELD NAME is a path spelling — `c.SourceFile`,
//	   `file.Path`, `whatever.RelPath`. Matched on the field name alone, so a new
//	   receiver name needs no edit here.
//	E. `filepath.Join(dir, filePath)` — seen when at least one ARGUMENT is itself
//	   a recognised path spelling. `filepath.Join(dir, base)` is not seen.
//	F. concatenation, BOUNDED — `filePath + ""`, `filePath + ".x"`: the path must
//	   come FIRST and every appended operand must be a string literal. A literal
//	   PREFIX (`"scope:ormmodel:" + filePath + "#" + name` — ormlink, haskell,
//	   abibridge, swift) builds a STRUCTURAL REF, which resolves byQualifiedName
//	   and is a different deliberate mechanism, not this bug. Matching those would
//	   bury the signal under five allow-list entries that are not instances of the
//	   defect. The cost of the bound: a genuine anchor spelled `prefix + filePath`
//	   would be missed.
//	G. positional composite literals of a relationship-shaped type, including the
//	   elided-type form inside `[]types.RelationshipRecord{{…}}`. No field names
//	   exist, so ANY path-valued element reports the site and the kind stays "?" —
//	   deliberately over-eager.
//	I. split literals — FromID in one composite literal, Kind assigned in another.
//	   Kind is no longer required; when absent the site is recorded as "?" rather
//	   than skipped.
//
// NOT SEEN (probe ran and the test stayed GREEN):
//
//	   THE LEAF SPELLING. `FromID: fp` where fp is the file path. No cheap fix.
//	D. a helper's return value — `FromID: pathOf(filePath)`. Only filepath.Join is
//	   special-cased; every other call is opaque. proto.go's fileContainsRel is
//	   this shape at its three call sites, and is caught only because the
//	   composite literal INSIDE the helper is itself visible — had the helper
//	   built the string another way, all three sites would be invisible.
//	   Also not seen, same root cause: an alias assigned in one function and read
//	   in another; a path stored on a struct field with an unlisted name and read
//	   back; form A performed across a function boundary.
//	   A computed Kind (`Kind: k`, k a parameter) reduces to "?" and so cannot be
//	   IMPORTS-filtered; such a site must be allow-listed by hand.
//
// This is a guard against the careless repetition of a known pattern, not a
// proof. It is honest about being one.
package extractors

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// filePathIdents are bare identifier spellings this repo's extractors use for
// "the path of the file being extracted". Lower-cased before comparison.
// Extended at scan time by the intra-function alias pass (form B).
var filePathIdents = map[string]bool{
	"filepath": true, "path": true, "frompath": true, "srcpath": true,
	"relpath": true, "sourcefile": true, "srcfile": true, "filename": true,
}

// filePathFields are FIELD names that mean "the path of the file". Matched on
// the selector's field name alone (`x.Path`, `c.SourceFile`), so a receiver
// spelled a new way needs no edit here — form C.
var filePathFields = map[string]bool{
	"path": true, "filepath": true, "sourcefile": true, "srcpath": true,
	"relpath": true, "frompath": true, "srcfile": true, "filename": true,
}

// allowEntry records how many sites a key is expected to cover and why. The
// COUNT is the load-bearing half: keying by package+kind alone let a brand-new
// offender hide behind an existing key (proved by mutant — a class-anchored
// verilog USES edge collided with the allow-listed tool USES edge and the guard
// stayed green). Keys are package + enclosing function + kind, and the count
// pins the number of sites inside that one function.
type allowEntry struct {
	count  int
	reason string
}

// allowedFileAnchored maps "<extractor package>:<enclosing func>:<Kind>" to the
// number of sites expected there and the reason they are correct or knowingly
// unfixed. Keyed by package + function + Kind rather than by line so it does
// not rot when code moves, and counted so a NEW site in an already-listed
// function fails rather than inheriting the blessing.
//
// IMPORTS is not listed and is never reported: refs.go:3658-3669 documents
// file-anchored FromID as the deliberate cross-language convention for import
// edges (#120). An import statement belongs to a file; a type relationship
// belongs to a type.
//
// A kind of "?" means the scan saw a path-valued FromID but could not see the
// Kind (forms A, G and I above). Such a site cannot be IMPORTS-filtered and so
// must be listed here explicitly.
var allowedFileAnchored = map[string]allowEntry{
	// ── Correct AND functional: a node carrying that exact path exists ───────
	//
	// vhdl / verilog: the owning record is the TOOL component, not a file
	// record — buildToolEntities emits one SCOPE.Component per detected
	// toolchain and hangs the USES edge off it. The FromID resolves because the
	// package ALSO emits an extractor.FileEntity whose Name is that same path,
	// so the edge reads file→tool as intended. The first round stated this as
	// "the owning record IS file-scoped", which is factually wrong, and that
	// wrong reasoning is what produced the six bad entries re-labelled below.
	"vhdl:buildVHDLToolEntities:USES": {1,
		"file → toolchain component. The owner is the TOOL record; the path-valued " +
			"FromID resolves because the package emits an extractor.FileEntity carrying " +
			"that exact path. VERIFIED by reading the site + the FileEntity emission."},
	"verilog:buildToolEntities:USES": {1,
		"file → toolchain component. The owner is the TOOL record; the path-valued " +
			"FromID resolves because the package emits an extractor.FileEntity carrying " +
			"that exact path. VERIFIED by reading the site + the FileEntity emission."},

	// graphql CONTAINS: the owning record is an explicit synthetic file-level
	// container built with Name: filePath in the same function, so the FromID is
	// a self-reference onto a node that demonstrably exists in the emission.
	"graphql:extractGraphQL:CONTAINS": {1,
		"the synthetic file-level container is emitted with Name: filePath in this " +
			"same function, so the edge is a benign self-reference onto a node that " +
			"exists. VERIFIED by reading the container emission."},

	// yaml OVERRIDES / BINDS / INCLUDES and the form-A site: all resolve via the
	// SCOPE.Document anchor the yaml dispatcher prepends, which carries the
	// file path. Functional, not instances of this bug.
	"yaml:extractHelmValues:OVERRIDES": {1,
		"Helm values file → subchart values key; the values FILE is the overriding " +
			"thing and the package emits a SCOPE.Document anchor carrying that path. " +
			"VERIFIED by reading the anchor emission."},
	"yaml:extractHelmTemplate:BINDS": {1,
		"`fromRef := file.Path` (form B alias) → helm_values key; resolves via the " +
			"SCOPE.Document anchor. VERIFIED by reading the site; the site's own " +
			"comment names the anchor."},
	"yaml:extractHelmTemplate:INCLUDES": {1,
		"`fromRef := file.Path` (form B alias) → helm_template; resolves via the " +
			"SCOPE.Document anchor. VERIFIED by reading the site."},
	// helm.go:875 — form A, post-construction `rel.FromID = file.Path` inside
	// the attachFromDefiner closure. Its kinds (INCLUDES at helm.go:918, BINDS
	// at helm.go:931) live in different literals, so this lands as "?". Listed
	// because it is the in-tree proof that the composite-literal-only shape
	// assumption was violated before this guard was ever written.
	"yaml:extractHelmHelpers:?": {1,
		"post-construction `rel.FromID = file.Path` (form A) in the attachFromDefiner " +
			"fallback path; resolves via the SCOPE.Document anchor. VERIFIED by reading " +
			"the site. Kind is unknowable at this site by construction."},

	// markdown: `docQName := file.Path` (form B alias), and the Document entity
	// is emitted with QualifiedName: docQName in the same function, so the edge
	// is a self-reference onto a node that exists.
	"markdown:Extract:CONTAINS": {1,
		"`docQName := file.Path` (form B alias) → code block; the Document entity is " +
			"emitted with QualifiedName: docQName in this same function (markdown.go:142), " +
			"so the FromID resolves. VERIFIED by reading both sites."},

	// ── KNOWN OFFENDERS, deliberately NOT fixed in #6298 ─────────────────────
	//
	// EVIDENCE STATUS. svelte is MEASURED (one kind, end to end). Everything
	// below svelte is INFERRED from reading the owning record against the
	// resolution rule stated at the top of this file — no runtime measurement.
	// #6298's own lesson is that astro was ASSUMED to match verilog and turned
	// out to be a different, worse failure, so nothing here is claimed as a
	// finding. Follow-up issue covers all six languages, each with its own
	// measurement.
	//
	// svelte has the astro failure, not the verilog one: no extractor.FileEntity
	// anywhere in the package, and entities[0] is the component named from the
	// file's basename. MEASURED through ResolveImports → ReferencesEmbedded on a
	// two-file snippet (Card.svelte renders <Button />):
	//
	//	owner="Card" kind=RENDERS FROM=<UNRESOLVED:src/lib/Card.svelte> TO=Button[SCOPE.Component]
	//
	// The ToID bound; the FromID did not. Every other svelte site below is the
	// same construction on the same record — all attach points are
	// `entities[0].Relationships = append(…)` — but NOT separately measured.
	//
	// Left out of #6298's diff on purpose: a language with its own behavioural
	// tests, none of which this issue reviewed. Fixing it is its own change with
	// its own measurement.
	"svelte:extractChildComponents:RENDERS": {1,
		"KNOWN OFFENDER (#6298): dangling FromID, MEASURED end to end (see the block " +
			"comment above). Out of scope here; fix separately."},
	"svelte:Extract:CONTAINS": {2,
		"KNOWN OFFENDER (#6298): same construction on the same entities[0] record as " +
			"the measured RENDERS site. INFERRED, not separately measured."},
	"svelte:extractActions:USES": {1,
		"KNOWN OFFENDER (#6298): same construction as the measured RENDERS site. " +
			"INFERRED, not separately measured."},
	"svelte:extractBranchConditions:USES": {1,
		"KNOWN OFFENDER (#6298): same construction as the measured RENDERS site. " +
			"INFERRED, not separately measured."},
	"svelte:extractContext:USES": {1,
		"KNOWN OFFENDER (#6298): same construction as the measured RENDERS site. " +
			"INFERRED, not separately measured."},
	"svelte:extractReactiveStatements:USES": {2,
		"KNOWN OFFENDER (#6298): same construction as the measured RENDERS site. " +
			"INFERRED, not separately measured."},
	"svelte:extractScriptDataFlow:USES": {1,
		"KNOWN OFFENDER (#6298): same construction as the measured RENDERS site. " +
			"INFERRED, not separately measured."},
	"svelte:extractRouteDirectives:NAVIGATES_TO": {1,
		"KNOWN OFFENDER (#6298): same construction as the measured RENDERS site. " +
			"INFERRED, not separately measured."},
	"svelte:extractScriptNavigation:NAVIGATES_TO": {1,
		"KNOWN OFFENDER (#6298): same construction as the measured RENDERS site. " +
			"INFERRED, not separately measured."},

	// graphql FEDERATES — MISANCHORED, and the site's own comment says so:
	// graphql.go:366-368 states the edge comes "from the extending stub", but
	// FromID is filePath, so the rewrite lands on the synthetic file container
	// instead. Every extending stub in one file merges its federation edges onto
	// that one node. The first-round reason ("the subgraph IS the file")
	// contradicted the code it blessed.
	"graphql:extractGraphQL:FEDERATES": {1,
		"KNOWN OFFENDER (#6298): misanchored. The site's own comment says the edge is " +
			"'from the extending stub', but FromID is filePath, so it lands on the " +
			"synthetic file container and every stub in the file merges onto it. " +
			"INFERRED from the site, NOT measured."},

	// proto CONTAINS — DANGLING. fileContainsRel builds FromID: file.Path, but
	// the proto package emits no node named for the containing file; the only
	// path-named entity in a proto extraction is the IMPORTED path. Two lines
	// below one of its call sites, proto.go:260's sibling service→rpc CONTAINS
	// edge correctly leaves FromID empty — the two shapes sit side by side.
	"proto:fileContainsRel:CONTAINS": {1,
		"KNOWN OFFENDER (#6298): dangling. No proto node carries the CONTAINING " +
			"file's path (only the IMPORTED path is path-named), and the sibling edge " +
			"at proto.go:260 correctly leaves FromID empty. Helper is called from 3 " +
			"sites. INFERRED from the site, NOT measured."},

	// hcl — DANGLING for every non-root .tf: the HCL file component's Name is
	// the BASENAME, not the path, so a path-valued FromID matches it only by
	// accident when the file is a root `main.tf` whose path IS its basename.
	"hcl:emitFileLevelRelationships:CONTAINS": {2,
		"KNOWN OFFENDER (#6298): dangling. The HCL file component's Name is the " +
			"BASENAME, so a path-valued FromID resolves only by accident for a root " +
			"main.tf. INFERRED from the site, NOT measured."},
	"hcl:parseDependsOnTuple:DEPENDS_ON": {1,
		"KNOWN OFFENDER (#6298): dangling (basename-named file component) AND wrong " +
			"owner — the owning record is the resource/module, so even a resolving " +
			"FromID would move the edge off it. INFERRED from the site, NOT measured."},

	// bicep — DANGLING unconditionally and misowned. Worse than the hcl pair:
	// `.bicep` is absent from sourceFileExtensions (refs.go:2752-2767), so these
	// do not even reach DispositionDynamic; they count as
	// DispositionBugExtractor. The first-round allow-list was blessing a live
	// bug-extractor figure.
	"bicep:dependencyEdges:DEPENDS_ON": {1,
		"KNOWN OFFENDER (#6298): dangling unconditionally, wrong owner, and `.bicep` " +
			"is not in sourceFileExtensions, so it lands in DispositionBugExtractor " +
			"rather than DispositionDynamic. INFERRED from the site, NOT measured."},

	// dockerfile — DANGLING for any Dockerfile not at the repo root, and
	// `Dockerfile` is likewise absent from sourceFileExtensions, so these also
	// count as DispositionBugExtractor.
	"dockerfile:collectCopy:USES": {1,
		"KNOWN OFFENDER (#6298): dangling for any non-root Dockerfile, and " +
			"`Dockerfile` is not in sourceFileExtensions, so it lands in " +
			"DispositionBugExtractor. INFERRED from the site, NOT measured."},
}

type fileAnchoredSite struct {
	pkg, fn, kind, key, file string
	line                     int
	fromExpr                 string
	form                     string
}

// scanFileAnchoredRels walks root for non-test Go sources and returns every
// site that binds a relationship's FromID to a file-path expression under a
// kind that is not IMPORTS. See the "WHAT IT CANNOT SEE" block above for the
// forms it covers and the ones it does not.
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

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			aliases := collectPathAliases(fn.Body)
			isPath := func(e ast.Expr) bool { return isFilePathExpr(e, aliases) }
			// Elements of `[]types.RelationshipRecord{{…}}` elide their type, so
			// the inner literal has cl.Type == nil. ast.Inspect is top-down, so
			// the outer literal is always seen first and can mark its children.
			elided := map[*ast.CompositeLit]bool{}

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.AssignStmt:
					// Form A: post-construction `rel.FromID = <path>`.
					for i, lhs := range v.Lhs {
						sel, ok := lhs.(*ast.SelectorExpr)
						if !ok || sel.Sel.Name != "FromID" || i >= len(v.Rhs) {
							continue
						}
						if !isPath(v.Rhs[i]) {
							continue
						}
						out = append(out, mkSite(fset, pkg, fn.Name.Name, "?", rel,
							v.Pos(), exprString(v.Rhs[i]), "A:assignment"))
					}
				case *ast.CompositeLit:
					if eltShaped(v.Type) {
						for _, el := range v.Elts {
							if inner, ok := el.(*ast.CompositeLit); ok && inner.Type == nil {
								elided[inner] = true
							}
						}
					}
					var from, kind ast.Expr
					keyed := false
					for _, el := range v.Elts {
						kv, ok := el.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						keyed = true
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
					if !keyed && len(v.Elts) > 0 {
						// Form G: positional literal of a relationship-shaped
						// type. No field names available, so any path-valued
						// element counts and the kind stays unknown.
						if !relationshipShaped(v.Type) && !elided[v] {
							return true
						}
						for _, el := range v.Elts {
							if isPath(el) {
								out = append(out, mkSite(fset, pkg, fn.Name.Name, "?", rel,
									v.Pos(), exprString(el), "G:positional"))
								break
							}
						}
						return true
					}
					if from == nil || !isPath(from) {
						return true
					}
					// Form I: Kind absent from this literal. Recorded as "?"
					// rather than skipped, so a split literal cannot hide.
					k := "?"
					if kind != nil {
						k = relKindString(kind)
					}
					if k == "IMPORTS" {
						return true
					}
					form := "keyed"
					if kind == nil {
						form = "I:split-literal"
					}
					out = append(out, mkSite(fset, pkg, fn.Name.Name, k, rel,
						v.Pos(), exprString(from), form))
				}
				return true
			})
		}
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

func mkSite(fset *token.FileSet, pkg, fn, kind, file string, pos token.Pos, from, form string) fileAnchoredSite {
	return fileAnchoredSite{
		pkg:      pkg,
		fn:       fn,
		kind:     kind,
		key:      pkg + ":" + fn + ":" + kind,
		file:     file,
		line:     fset.Position(pos).Line,
		fromExpr: from,
		form:     form,
	}
}

// collectPathAliases does one hop of intra-function alias tracking (form B):
// `sourceFile := filePath` makes `sourceFile` a path spelling for the rest of
// that function. It does NOT iterate to a fixed point and does NOT cross
// function boundaries.
func collectPathAliases(body *ast.BlockStmt) map[string]bool {
	aliases := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || i >= len(as.Rhs) {
				continue
			}
			// Only a PURE path RHS makes an alias. `x := "scope:…:" + path`
			// is a structural ref, not a path (see isFilePathExpr's
			// BinaryExpr case), and aliasing it would flag every structural-ref
			// scheme in the tree.
			switch as.Rhs[i].(type) {
			case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr:
				if isFilePathExpr(as.Rhs[i], nil) {
					aliases[strings.ToLower(id.Name)] = true
				}
			}
		}
		return true
	})
	return aliases
}

// isFilePathExpr reports whether e is a syntactically recognisable "path of the
// file being extracted". Covers bare idents and aliases (B), any selector whose
// FIELD name is a path spelling (C), filepath.Join (E) and concatenation (F).
// A call to anything else is opaque (D).
func isFilePathExpr(e ast.Expr, aliases map[string]bool) bool {
	switch v := e.(type) {
	case *ast.Ident:
		n := strings.ToLower(v.Name)
		return filePathIdents[n] || aliases[n]
	case *ast.SelectorExpr:
		return filePathFields[strings.ToLower(v.Sel.Name)]
	case *ast.BinaryExpr:
		// Form F, bounded. A concatenation is still path-SHAPED only when the
		// path comes FIRST and everything appended to it is a string literal
		// (`filePath + ""`, `filePath + ".x"`). A literal PREFIX — the
		// `"scope:ormmodel:" + filePath + "#" + name` spelling used by ormlink,
		// haskell, abibridge and swift — builds a STRUCTURAL REF, which is a
		// different, deliberate resolution scheme (byQualifiedName), not a file
		// anchor. Flagging those would bury the real signal under five
		// allow-list entries that are not instances of this bug at all.
		ops := flattenConcat(v)
		if len(ops) == 0 || !isFilePathExpr(ops[0], aliases) {
			return false
		}
		for _, o := range ops[1:] {
			lit, ok := o.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return false
			}
		}
		return true
	case *ast.CallExpr:
		sel, ok := v.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, isIdent := sel.X.(*ast.Ident)
		if !isIdent || pkg.Name != "filepath" || sel.Sel.Name != "Join" {
			return false
		}
		for _, a := range v.Args {
			if isFilePathExpr(a, aliases) {
				return true
			}
		}
		return false
	case *ast.ParenExpr:
		return isFilePathExpr(v.X, aliases)
	}
	return false
}

// flattenConcat flattens a left-nested `a + b + c` string concatenation into
// its operands, in source order. Returns nil for any other operator.
func flattenConcat(e ast.Expr) []ast.Expr {
	be, ok := e.(*ast.BinaryExpr)
	if !ok {
		return []ast.Expr{e}
	}
	if be.Op != token.ADD {
		return nil
	}
	left := flattenConcat(be.X)
	right := flattenConcat(be.Y)
	if left == nil || right == nil {
		return nil
	}
	return append(left, right...)
}

// eltShaped reports whether t is a slice / array / map of a relationship-shaped
// type, i.e. whether its elements elide a relationship type name.
func eltShaped(t ast.Expr) bool {
	switch v := t.(type) {
	case *ast.ArrayType:
		return relationshipShaped(v.Elt)
	case *ast.MapType:
		return relationshipShaped(v.Value)
	}
	return false
}

// relationshipShaped reports whether a composite literal's type looks like a
// relationship record, used to bound the positional-literal scan (form G).
func relationshipShaped(t ast.Expr) bool {
	if t == nil {
		return false
	}
	return strings.Contains(strings.ToLower(exprString(t)), "relationship")
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
	case *ast.ParenExpr:
		return "(" + exprString(v.X) + ")"
	case *ast.StarExpr:
		return "*" + exprString(v.X)
	}
	return "<expr>"
}

// TestNoNewFileAnchoredTypeRelationships fails when an extractor binds a
// non-IMPORTS relationship's FromID to the source file path from a site the
// allow-list above does not account for, INCLUDING a new site inside a function
// that already has an allow-listed site of the same kind.
func TestNoNewFileAnchoredTypeRelationships(t *testing.T) {
	sites := scanFileAnchoredRels(t, ".")

	// Vacuity guard 1: a matcher that matches nothing passes for free.
	if len(sites) == 0 {
		t.Fatal("scanner found no file-anchored relationship sites at all — " +
			"the walk or the AST match has broken, and a guard that matches " +
			"nothing passes for free")
	}

	observed := map[string][]fileAnchoredSite{}
	for _, s := range sites {
		observed[s.key] = append(observed[s.key], s)
	}

	fmtSites := func(ss []fileAnchoredSite) string {
		var b []string
		for _, s := range ss {
			b = append(b, fmt.Sprintf("%s:%d  FromID: %s  [%s]", s.file, s.line, s.fromExpr, s.form))
		}
		return strings.Join(b, "\n      ")
	}

	keys := make([]string, 0, len(observed))
	for k := range observed {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var unaccounted []string
	for _, k := range keys {
		ss := observed[k]
		entry, ok := allowedFileAnchored[k]
		if !ok {
			unaccounted = append(unaccounted, fmt.Sprintf("key %q (%d site(s)):\n      %s", k, len(ss), fmtSites(ss)))
			continue
		}
		// Vacuity guard 2: the count pins the number of sites, so a NEW site in
		// an already-blessed function+kind fails instead of inheriting the
		// blessing. This is the granularity mutant from the #6365 review.
		if len(ss) != entry.count {
			unaccounted = append(unaccounted, fmt.Sprintf(
				"key %q: allow-list expects %d site(s), found %d:\n      %s",
				k, entry.count, len(ss), fmtSites(ss)))
		}
	}
	if len(unaccounted) > 0 {
		t.Errorf("relationship FromID bound to the source file path at unaccounted site(s) (#6298):\n\n  %s\n\n"+
			"Leave FromID EMPTY so graph assembly stamps the owning record's own entity id\n"+
			"(cmd/grafel/index.go and relRecordToGraphRel in internal/extractors/incremental.go).\n"+
			"A path-valued FromID resolves ONLY if some emitted node carries that exact path\n"+
			"as its Name/QualifiedName — being 'conceptually file-scoped' is not enough.\n"+
			"If it really does resolve, add the key to allowedFileAnchored in this file with\n"+
			"the site count and the reason, and say what you MEASURED vs what you INFERRED.",
			strings.Join(unaccounted, "\n\n  "))
	}

	// Vacuity guard 3: a stale allow-list entry is its own defect — it hides the
	// next offender under a key nobody is watching any more, and it is also how
	// a NARROWING of the matcher announces itself (a narrowed matcher stops
	// producing sites, and every orphaned key fails here).
	for key := range allowedFileAnchored {
		if len(observed[key]) == 0 {
			t.Errorf("allowedFileAnchored[%q] matches no site any more — either the code "+
				"was fixed (delete the entry) or the scanner was narrowed (fix the scanner)", key)
		}
	}
}
