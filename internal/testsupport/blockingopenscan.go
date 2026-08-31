package testsupport

// blockingopenscan.go — the #6416/#6478 "this read can block forever on a
// non-regular file" boundary detector, made reusable so a single repo-wide
// guard can run it.
//
// # What it looks for, and why that shape
//
// os.ReadFile / os.Open / os.OpenFile block in open(2) until a writer appears
// when the path is a FIFO, and never reach EOF when it is a character device.
// internal/safeio exists to bound that. The population that matters is not
// "every open" — it is every open whose PATH WAS NAMED rather than discovered:
// a filename an attacker can choose to plant a FIFO under. docs/blocking-open-audit.md
// calls that bucket name-chosen, and its own recipe for finding it is "a grep
// for a literal filename argument", which is exactly what this automates:
//
//	an os.ReadFile/os.Open/os.OpenFile whose path expression, resolved up to
//	two assignments back inside the enclosing function (package-level consts
//	and vars from ANYWHERE in the package included), contains a filename-shaped
//	string literal.
//
// The audit was a hand-maintained snapshot and #6478 records the consequence:
// four consecutive rounds each fixed the sites the previous review named and
// swept no further, because nothing observed the boundary. This detector is
// the thing that observes it.
//
// # The honest limits, stated up front rather than discovered later
//
//   - It cannot decide PROVENANCE. "Is this directory attacker-writable, or is
//     it ~/.grafel?" is a judgement no AST pass makes. So it reports a
//     population, and the guard that consumes it subtracts declared ledgers.
//     That is a deliberate division of labour, not a gap: the ledgers are
//     ratcheted, so the population can only shrink.
//   - It resolves IDENTIFIERS, not calls. A path that arrives as a bare
//     function parameter is invisible, because answering it needs the CALLERS
//     of that function rather than its body. Pinned as a MISS row in
//     blockingopenscan_test.go. Cross-FILE consts are NOT such a case — they
//     were a hole, were found by review with a positive control, and are now
//     resolved; see CollectPackageValues.
//   - It is keyed on a literal. A filename that arrives entirely through a
//     parameter or a package-level slice of names is invisible — which is why
//     internal/install/hooks's HookNames loop needed the audit to find it and
//     needs safeio to stay fixed, not this scan.
//   - Writes are excluded by flag inspection, not by intent: an os.OpenFile
//     carrying O_CREATE/O_WRONLY/O_RDWR/O_APPEND/O_TRUNC is a write and a write
//     to a FIFO does not have the unbounded-read shape.

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// NameChosenOpen is one reported call site.
type NameChosenOpen struct {
	File    string // slash-separated path, relative to the scan root
	Line    int
	Fn      string // enclosing function name, or "" at package level
	Call    string // "os.ReadFile", "os.Open" or "os.OpenFile"
	Literal string // the filename-shaped literal that triggered the report
}

// Key is the ledger key: file + enclosing function. Deliberately NOT
// line-based — a line number churns on every edit above it, and a ledger that
// churns is a ledger nobody re-reads. Two reports in the same function collapse
// to one entry, which is the right granularity for "this function was judged".
func (o NameChosenOpen) Key() string { return o.File + ":" + o.Fn }

func (o NameChosenOpen) String() string {
	return fmt.Sprintf("%s:%d %s (%s, literal %q)", o.File, o.Line, o.Fn, o.Call, o.Literal)
}

// blockingOpens are the three standard-library entry points that can park in
// open(2). safeio.Open / safeio.ReadFile wrap them and are therefore not
// matched — routing a site through safeio is what makes it disappear from this
// scan, which is the whole feedback loop.
var blockingOpens = map[string]bool{"ReadFile": true, "Open": true, "OpenFile": true}

// writeFlags mark an os.OpenFile as a write rather than a read.
var writeFlags = map[string]bool{
	"O_CREATE": true, "O_WRONLY": true, "O_RDWR": true,
	"O_APPEND": true, "O_TRUNC": true, "O_EXCL": true,
}

// CollectPackageValues maps every package-level const/var name declared across
// files to its initialiser. Pass the result to FindNameChosenOpensInPackage.
//
// It exists because a per-FILE scope was a real hole, found by review and
// reproduced with a positive control: a `const rulesFile = ".grafel"` in
// consts.go and an `os.ReadFile(rulesFile)` in reader.go evaded the scan
// entirely, while the byte-identical read with the const moved into the same
// file was caught. A separate consts.go or paths.go holding path literals is
// ordinary idiomatic Go, so that gap was likelier to be hit by the next
// name-chosen read than either of the two limits this detector documents.
//
// Package scope is safe to flatten in a way FUNCTION scope is not: Go
// guarantees these names are unique across the package, so merging them cannot
// let one declaration explain an unrelated identifier. Function locals are
// still collected per function for exactly that reason.
func CollectPackageValues(files []*ast.File) map[string][]ast.Expr {
	out := map[string][]ast.Expr{}
	for _, f := range files {
		for k, v := range collectFileLevelValues(f) {
			out[k] = append(out[k], v...)
		}
	}
	return out
}

// FindNameChosenOpens reports every name-chosen blocking open in f, resolving
// identifiers against f alone. Callers that can see the whole package should
// use FindNameChosenOpensInPackage instead — see CollectPackageValues for why
// the difference is load-bearing rather than cosmetic.
func FindNameChosenOpens(fset *token.FileSet, f *ast.File, rel string) []NameChosenOpen {
	return FindNameChosenOpensInPackage(fset, f, rel, nil)
}

// FindNameChosenOpensInPackage reports every name-chosen blocking open in f,
// resolving identifiers against f's own declarations AND pkgScope (from
// CollectPackageValues). A nil pkgScope degrades to single-file resolution.
func FindNameChosenOpensInPackage(fset *token.FileSet, f *ast.File, rel string, pkgScope map[string][]ast.Expr) []NameChosenOpen {
	var out []NameChosenOpen
	seen := map[string]bool{}

	// Package-level const/var declarations in THIS file are in scope for every
	// function in it. They are collected separately from function bodies rather
	// than by walking the whole file, because a single flat map over every
	// function's locals would let one function's `path := "x.json"` explain
	// another function's unrelated `path`. pkgScope adds the same declarations
	// from the package's OTHER files, which Go's own scoping rules already put
	// in reach of every function here.
	fileScope := collectFileLevelValues(f)

	inspectFunc := func(fnName string, body *ast.BlockStmt, scope ast.Node) {
		assigns := collectAssignments(scope)
		for k, v := range fileScope {
			assigns[k] = append(assigns[k], v...)
		}
		for k, v := range pkgScope {
			assigns[k] = append(assigns[k], v...)
		}
		ast.Inspect(scope, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := osCallName(call)
			if !ok || len(call.Args) == 0 {
				return true
			}
			if name == "OpenFile" && isWriteOpen(call) {
				return true
			}
			lit, ok := nameChosenLiteral(call.Args[0], assigns)
			if !ok {
				return true
			}
			pos := fset.Position(call.Pos())
			o := NameChosenOpen{File: rel, Line: pos.Line, Fn: fnName, Call: "os." + name, Literal: lit}
			k := fmt.Sprintf("%s:%d", rel, pos.Line)
			if seen[k] {
				return true
			}
			seen[k] = true
			out = append(out, o)
			return true
		})
	}

	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		name := fd.Name.Name
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			name = receiverName(fd.Recv.List[0].Type) + "." + name
		}
		inspectFunc(name, fd.Body, fd.Body)
	}
	// Package-level var initialisers can hold an open too.
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		inspectFunc("", nil, gd)
	}
	return out
}

func receiverName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return receiverName(t.X)
	}
	return "?"
}

func osCallName(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "os" {
		return "", false
	}
	if !blockingOpens[sel.Sel.Name] {
		return "", false
	}
	return sel.Sel.Name, true
}

func isWriteOpen(call *ast.CallExpr) bool {
	if len(call.Args) < 2 {
		return false
	}
	write := false
	ast.Inspect(call.Args[1], func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "os" && writeFlags[sel.Sel.Name] {
			write = true
		}
		return true
	})
	return write
}

// collectFileLevelValues maps package-level const/var names declared in f to
// their initialisers.
func collectFileLevelValues(f *ast.File) map[string][]ast.Expr {
	out := map[string][]ast.Expr{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, s := range gd.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, nm := range vs.Names {
				if nm.Name != "_" {
					out[nm.Name] = append(out[nm.Name], vs.Values[i])
				}
			}
		}
	}
	return out
}

// collectAssignments maps identifier name -> every expression assigned to it
// inside scope. This is the "one assignment back" resolution: deliberately
// flow-insensitive, because a flow-sensitive pass would be a type checker and
// the extra precision buys nothing for a population that is then ledgered.
func collectAssignments(scope ast.Node) map[string][]ast.Expr {
	assigns := map[string][]ast.Expr{}
	if scope == nil {
		return assigns
	}
	record := func(lhs, rhs []ast.Expr) {
		if len(lhs) != len(rhs) {
			return
		}
		for i, l := range lhs {
			if id, ok := l.(*ast.Ident); ok && id.Name != "_" {
				assigns[id.Name] = append(assigns[id.Name], rhs[i])
			}
		}
	}
	ast.Inspect(scope, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			record(s.Lhs, s.Rhs)
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, 0, len(s.Names))
			for _, nm := range s.Names {
				lhs = append(lhs, nm)
			}
			record(lhs, s.Values)
		}
		return true
	})
	return assigns
}

// resolveDepth bounds how many identifier hops the scan follows. Two is
// measured, not arbitrary: one hop reaches `path := dir + "/" + name` and stops
// at `name`, which is where the real population lives —
// internal/quality/fitness's `path := stateDir + "/" + DefaultConfigName` and
// internal/coverage's `filepath.Join(repoRoot, DefaultReportPath)` are both
// invisible at depth one and both were live #6478 sites. Beyond two the returns
// vanish and the false-positive rate does not.
const resolveDepth = 2

// nameChosenLiteral returns the filename-shaped literal reached from expr,
// following identifiers up to resolveDepth assignments deep.
func nameChosenLiteral(expr ast.Expr, assigns map[string][]ast.Expr) (string, bool) {
	return literalIn(expr, assigns, resolveDepth, map[string]bool{})
}

// literalIn returns the first filename-shaped string literal reachable from e,
// descending through identifiers via assigns. seen breaks reference cycles
// (`x = x + "/y"` is ordinary Go and would otherwise recurse forever).
func literalIn(e ast.Expr, assigns map[string][]ast.Expr, depth int, seen map[string]bool) (string, bool) {
	var found string
	var pending []string
	ast.Inspect(e, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		switch t := n.(type) {
		case *ast.BasicLit:
			if t.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(t.Value)
			if err != nil {
				return true
			}
			if FilenameShaped(s) {
				found = s
				return false
			}
		case *ast.Ident:
			if !seen[t.Name] {
				pending = append(pending, t.Name)
			}
		}
		return true
	})
	if found != "" {
		return found, true
	}
	if depth <= 0 {
		return "", false
	}
	for _, name := range pending {
		if seen[name] {
			continue
		}
		seen[name] = true
		for _, rhs := range assigns[name] {
			if lit, ok := literalIn(rhs, assigns, depth-1, seen); ok {
				return lit, true
			}
		}
	}
	return "", false
}

// FilenameShaped reports whether s names a file rather than describing a
// directory, a URL or a fragment.
//
// The rule is the last path segment: it must be a plausible basename AND carry
// either an extension or a leading dot. "config" alone is not filename-shaped
// — too many directories are called that, and a bare word carries no signal
// that a file is being named. ".git", ".gitignore", "group.json" and
// "src/main/resources/application.properties" all are.
func FilenameShaped(s string) bool {
	if s == "" || len(s) > 128 || strings.Contains(s, "://") || strings.ContainsAny(s, " \t\n%*?") {
		return false
	}
	s = strings.TrimSuffix(s, "/")
	seg := s
	if i := strings.LastIndex(s, "/"); i >= 0 {
		seg = s[i+1:]
	}
	if seg == "" || seg == "." || seg == ".." {
		return false
	}
	dot := strings.Contains(strings.TrimPrefix(seg, "."), ".")
	if !strings.HasPrefix(seg, ".") && !dot {
		return false
	}
	for _, r := range seg {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-', r == '+':
		default:
			return false
		}
	}
	return true
}
