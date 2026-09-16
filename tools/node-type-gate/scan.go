package main

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Form names the syntactic shape a literal was collected from. Forms are
// reported so a reviewer can tell at a glance whether a hit is one grep would
// also have seen ("cmp") or one it is structurally blind to.
const (
	FormCmp       = "cmp"       // n.Type() == "x" / != "x"
	FormSwitch    = "switch"    // case "x": of switch n.Type()
	FormHelper    = "helper"    // arg i of a func whose i-th param reaches a node-type position
	FormMapLookup = "maplookup" // key of a table indexed by n.Type(), or a slices.Contains element
)

// Site is one occurrence of a string literal in a node-type position.
type Site struct {
	Pkg  string // import path of the package the site lives in
	Dir  string // module-relative directory, e.g. "internal/extractors/scala"
	File string // module-relative file path
	Line int
	Lit  string
	Form string
	// Alias is true when the literal was reached through a value that HOLDS a
	// node type rather than through a syntactic `x.Type()` call — a local
	// (`t := n.Type(); t == "…"`) or a parameter fed one. See aliasNote.
	Alias bool
	// Const distinguishes a folded constant (Const true, Lit its value —
	// including the empty string) from a position the scanner could not fold
	// (Const false). Without it a `""` sentinel and a dynamic position would be
	// indistinguishable.
	Const bool
}

// Scan is the derived surface: every constant literal site, plus the positions
// where the compared value is not a constant (reported, never silently dropped).
type Scan struct {
	Sites   []Site
	Dynamic []Site // same positions, Lit == "" — a variable or call reaches the sink
	// Sinks and Sources are the discovered helper surface, "pkg.Func#i" per
	// parameter position: Sinks are the positions whose ARGUMENT is a
	// node-type literal, Sources the positions that RECEIVE a node-type value.
	// Both are printed by -report under "helper surface", which is what makes
	// "reported" true of them — a field nobody prints is exactly as invisible
	// as a surface nobody scans, and "the fixpoint found nothing" must not look
	// like "the fixpoint found the wrong thing".
	Sinks   []string
	Sources []string
}

// nodeTypeIfacePath is the package that declares the CST node interface every
// extractor traverses. A ".Type()" call is only a node-type position when its
// receiver implements this interface.
const nodeTypeIfacePath = "github.com/cajasmota/grafel/internal/treesitter/ts"

type paramKey struct {
	fn  *types.Func
	idx int
}

type scanner struct {
	fset      *token.FileSet
	modRoot   string
	nodeIface *types.Interface

	// sinks is the fixpoint set of (func, param index) pairs whose argument is
	// a node type. Discovered, never hand-listed. This is the ARGUMENT
	// direction: the caller's literal flows INTO a node-type position.
	sinks map[paramKey]bool

	// sources is the opposite direction, and it is what closes the #7076-r2
	// blind spot: (func, param index) pairs that RECEIVE a node-type value, so
	// literals compared against that parameter INSIDE the callee are node
	// types. `isKotlinDeclType(nx.Type())` is the real instance.
	//
	// A position qualifies only when it has at least one VISIBLE call site and
	// every visible call site passes a node-type value. A helper called once
	// with n.Type() and once with an ordinary string is polymorphic, and
	// treating its literals as node types would invent failures.
	//
	// "VISIBLE" is doing real work in that sentence and is the fixpoint's one
	// SOUNDNESS limit — every other limit in this tool is completeness-only.
	// A call site is visible when calleeFunc resolves it, i.e. it is a direct
	// call through an identifier or a selector, in a package the loader read.
	// Two shapes are therefore NOT counted:
	//
	//	call through a func value   g := f; g(someString)
	//	call from a _test.go        the loader runs with Tests: false
	//
	// The second is the likelier one in practice. If a helper's only
	// ordinary-string caller lives in its own test file, the rule sees a
	// unanimous node-type population that is not unanimous, resolves a plain
	// string as a node type, and can report a dead literal that is not one —
	// telling an author to file an issue for a non-defect.
	//
	// Latent, not live: no such shape exists in the tree today, which is what
	// makes the narrow reading affordable. Widening it means loading tests
	// (Tests: true roughly doubles the load) and modelling func values; if this
	// ever fires wrongly, that is the fix, not an exception list.
	sources map[paramKey]bool
	// argSites / argNTSites count, per parameter position, how many call sites
	// there are and how many of them pass a node-type value.
	argSites   map[paramKey]int
	argNTSites map[paramKey]int

	// keyTables is the set of variables indexed by a node-type call
	// (`set[n.Type()]`) or searched by one (`slices.Contains(s, n.Type())`).
	// Their keys/elements are node-type positions.
	keyTables map[types.Object]bool

	// composites records the composite literals a variable was initialised
	// from, so a package-level table resolves even though its literal is not
	// inside any function body.
	composites map[types.Object][]*ast.CompositeLit
	// appends records values appended to a variable.
	appends map[types.Object][]ast.Expr

	pkgs []*packages.Package
}

func newScanner(fset *token.FileSet, modRoot string, pkgs []*packages.Package) *scanner {
	return &scanner{
		fset:       fset,
		modRoot:    modRoot,
		sinks:      map[paramKey]bool{},
		sources:    map[paramKey]bool{},
		argSites:   map[paramKey]int{},
		argNTSites: map[paramKey]int{},
		keyTables:  map[types.Object]bool{},
		composites: map[types.Object][]*ast.CompositeLit{},
		appends:    map[types.Object][]ast.Expr{},
		pkgs:       pkgs,
	}
}

func (s *scanner) findNodeIface() bool {
	for _, p := range s.pkgs {
		if p.PkgPath != nodeTypeIfacePath || p.Types == nil {
			continue
		}
		obj := p.Types.Scope().Lookup("Node")
		if obj == nil {
			continue
		}
		if iface, ok := obj.Type().Underlying().(*types.Interface); ok {
			s.nodeIface = iface
			return true
		}
	}
	return false
}

// isNodeTypeCall reports whether e is `x.Type()` with x a CST node.
func (s *scanner) isNodeTypeCall(info *types.Info, e ast.Expr) bool {
	call, ok := astUnparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := astUnparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Type" {
		return false
	}
	if s.nodeIface == nil || info == nil {
		return false
	}
	t := info.TypeOf(sel.X)
	if t == nil {
		return false
	}
	return types.Implements(t, s.nodeIface) || types.Implements(types.NewPointer(t), s.nodeIface)
}

// isNodeTypeValue reports whether e evaluates to a node type: either the call
// `x.Type()` itself, or a value known to hold one (ntLocals). It is the
// generalisation of isNodeTypeCall that the round-2 review's blocker required —
// `t := n.Type(); t == "…"` is a node-type comparison that neither the old scan
// nor a `.Type() == "…"` grep can see.
func (s *scanner) isNodeTypeValue(info *types.Info, e ast.Expr, ntLocals map[types.Object]bool) bool {
	if s.isNodeTypeCall(info, e) {
		return true
	}
	if len(ntLocals) == 0 {
		return false
	}
	obj := identObj(info, e)
	return obj != nil && ntLocals[obj]
}

func astUnparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// collectTables finds every variable used as a node-type lookup table, and
// every composite literal / append that feeds one. It runs before the fixpoint
// so package-level tables are known when function bodies are analysed.
func (s *scanner) collectTables() {
	for _, p := range s.pkgs {
		info := p.TypesInfo
		if info == nil {
			continue
		}
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.IndexExpr:
					if s.isNodeTypeCall(info, x.Index) {
						if obj := identObj(info, x.X); obj != nil {
							s.keyTables[obj] = true
						}
					}
				case *ast.CallExpr:
					// slices.Contains(tbl, n.Type()) — membership, not indexing.
					if sel, ok := astUnparen(x.Fun).(*ast.SelectorExpr); ok &&
						sel.Sel.Name == "Contains" && len(x.Args) == 2 &&
						s.isNodeTypeCall(info, x.Args[1]) {
						if obj := identObj(info, x.Args[0]); obj != nil {
							s.keyTables[obj] = true
						}
					}
				case *ast.ValueSpec:
					for i, nm := range x.Names {
						if i < len(x.Values) {
							s.recordInit(info.ObjectOf(nm), x.Values[i])
						}
					}
				case *ast.AssignStmt:
					for i, lhs := range x.Lhs {
						if i >= len(x.Rhs) {
							break
						}
						s.recordInit(identObj(info, lhs), x.Rhs[i])
					}
				}
				return true
			})
		}
	}
	for _, p := range s.pkgs {
		info := p.TypesInfo
		if info == nil {
			continue
		}
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
					return true
				}
				call, ok := astUnparen(as.Rhs[0]).(*ast.CallExpr)
				if !ok {
					return true
				}
				id, ok := astUnparen(call.Fun).(*ast.Ident)
				if !ok || id.Name != "append" || len(call.Args) < 2 {
					return true
				}
				obj := identObj(info, as.Lhs[0])
				if obj == nil {
					return true
				}
				s.appends[obj] = append(s.appends[obj], call.Args[1:]...)
				return true
			})
		}
	}
}

func (s *scanner) recordInit(obj types.Object, rhs ast.Expr) {
	if obj == nil {
		return
	}
	if cl, ok := astUnparen(rhs).(*ast.CompositeLit); ok {
		s.composites[obj] = append(s.composites[obj], cl)
	}
}

func identObj(info *types.Info, e ast.Expr) types.Object {
	id, ok := astUnparen(e).(*ast.Ident)
	if !ok {
		return nil
	}
	return info.ObjectOf(id)
}

// position is one node-type position discovered inside a function body.
type position struct {
	expr  ast.Expr
	form  string
	alias bool
}

// analyzeFunc collects the node-type positions of one function body under the
// current sink set, and reports which of the function's own parameters are
// thereby node-type parameters.
// argFeed records one call-site argument, and whether it carried a node type.
type argFeed struct {
	key paramKey
	nt  bool
}

func (s *scanner) analyzeFunc(info *types.Info, fn *types.Func, body *ast.BlockStmt, ftype *ast.FuncType) ([]position, map[int]bool, []argFeed) {
	var out []position
	var feeds []argFeed

	// alias maps a local (a range variable, or a plain copy) back to the value
	// it came from, so `for _, k := range kinds` still attributes a hit on k to
	// the parameter kinds.
	alias := map[types.Object]types.Object{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.RangeStmt:
			if src := identObj(info, x.X); src != nil && x.Value != nil {
				if v := identObj(info, x.Value); v != nil {
					alias[v] = src
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				if i >= len(x.Rhs) {
					break
				}
				l, r := identObj(info, lhs), identObj(info, x.Rhs[i])
				if l != nil && r != nil {
					alias[l] = r
				}
			}
		}
		return true
	})

	// ntLocals: values in this body that HOLD a node type. Seeded from the
	// function's own parameters that every caller feeds a node type into, then
	// grown through plain assignment (`t := n.Type()`, `u := t`). Iterated
	// because an assignment can precede or follow the one it depends on.
	ntLocals := map[types.Object]bool{}
	for i, obj := range paramObjects(info, ftype) {
		if s.sources[paramKey{fn, i}] {
			ntLocals[obj] = true
		}
	}
	for pass := 0; pass < 8; pass++ {
		grew := false
		ast.Inspect(body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, lhs := range as.Lhs {
				if i >= len(as.Rhs) {
					break
				}
				l := identObj(info, lhs)
				if l == nil || ntLocals[l] {
					continue
				}
				if s.isNodeTypeValue(info, as.Rhs[i], ntLocals) {
					ntLocals[l] = true
					grew = true
				}
			}
			return true
		})
		if !grew {
			break
		}
	}

	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if x.Op != token.EQL && x.Op != token.NEQ {
				return true
			}
			if s.isNodeTypeCall(info, x.X) {
				out = append(out, position{x.Y, FormCmp, false})
			} else if s.isNodeTypeCall(info, x.Y) {
				out = append(out, position{x.X, FormCmp, false})
			} else if s.isNodeTypeValue(info, x.X, ntLocals) {
				out = append(out, position{x.Y, FormCmp, true})
			} else if s.isNodeTypeValue(info, x.Y, ntLocals) {
				out = append(out, position{x.X, FormCmp, true})
			}
		case *ast.SwitchStmt:
			if x.Tag == nil || !s.isNodeTypeValue(info, x.Tag, ntLocals) {
				return true
			}
			viaAlias := !s.isNodeTypeCall(info, x.Tag)
			for _, cl := range x.Body.List {
				cc, ok := cl.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, e := range cc.List {
					out = append(out, position{e, FormSwitch, viaAlias})
				}
			}
		case *ast.IndexExpr:
			// set[k] = true, where set is indexed elsewhere by n.Type().
			if s.isNodeTypeCall(info, x.Index) {
				return true
			}
			if obj := identObj(info, x.X); obj != nil && s.keyTables[obj] {
				out = append(out, position{x.Index, FormMapLookup, false})
			}
		case *ast.CallExpr:
			callee := calleeFunc(info, x)
			if callee == nil {
				return true
			}
			sig, _ := callee.Type().(*types.Signature)
			for i, arg := range x.Args {
				idx := i
				if sig != nil && sig.Variadic() && sig.Params().Len() > 0 && idx >= sig.Params().Len()-1 {
					idx = sig.Params().Len() - 1
				}
				k := paramKey{callee, idx}
				feeds = append(feeds, argFeed{key: k, nt: s.isNodeTypeValue(info, arg, ntLocals)})
				if s.sinks[k] {
					out = append(out, position{arg, FormHelper, false})
				}
			}
		}
		return true
	})

	// Which of this function's parameters are node-type parameters?
	params := map[types.Object]int{}
	if ftype != nil && ftype.Params != nil {
		idx := 0
		for _, fld := range ftype.Params.List {
			if len(fld.Names) == 0 {
				idx++
				continue
			}
			for _, nm := range fld.Names {
				if o := info.ObjectOf(nm); o != nil {
					params[o] = idx
				}
				idx++
			}
		}
	}
	hit := map[int]bool{}
	for _, p := range out {
		obj := identObj(info, p.expr)
		for hops := 0; obj != nil && hops < 16; hops++ {
			if i, ok := params[obj]; ok {
				hit[i] = true
				break
			}
			obj = alias[obj]
		}
	}
	return out, hit, feeds
}

// paramObjects lists a function's parameters by declaration index.
func paramObjects(info *types.Info, ftype *ast.FuncType) map[int]types.Object {
	out := map[int]types.Object{}
	if ftype == nil || ftype.Params == nil {
		return out
	}
	idx := 0
	for _, fld := range ftype.Params.List {
		if len(fld.Names) == 0 {
			idx++
			continue
		}
		for _, nm := range fld.Names {
			if o := info.ObjectOf(nm); o != nil {
				out[idx] = o
			}
			idx++
		}
	}
	return out
}

// tablePositions yields the keys of every composite literal, and every appended
// element, bound to a variable that is used as a node-type lookup table. These
// live outside any function body when the table is package-level, so they are
// harvested separately from analyzeFunc.
func (s *scanner) tablePositions() map[*packages.Package][]position {
	out := map[*packages.Package][]position{}
	owner := map[types.Object]*packages.Package{}
	for _, p := range s.pkgs {
		if p.Types == nil {
			continue
		}
		for obj := range s.keyTables {
			if obj.Pkg() != nil && obj.Pkg().Path() == p.PkgPath {
				owner[obj] = p
			}
		}
	}
	for obj := range s.keyTables {
		p := owner[obj]
		if p == nil {
			continue
		}
		for _, cl := range s.composites[obj] {
			for _, elt := range cl.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					out[p] = append(out[p], position{kv.Key, FormMapLookup, false})
				} else {
					out[p] = append(out[p], position{elt, FormMapLookup, false})
				}
			}
		}
		for _, e := range s.appends[obj] {
			out[p] = append(out[p], position{e, FormMapLookup, false})
		}
	}
	return out
}

type funcUnit struct {
	pkg   *packages.Package
	body  *ast.BlockStmt
	ftype *ast.FuncType
	fn    *types.Func
}

func (s *scanner) units() []funcUnit {
	var out []funcUnit
	for _, p := range s.pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				fn, _ := p.TypesInfo.ObjectOf(fd.Name).(*types.Func)
				if fn == nil {
					continue
				}
				out = append(out, funcUnit{pkg: p, body: fd.Body, ftype: fd.Type, fn: fn})
			}
		}
	}
	return out
}

// Run derives the surface: table collection, the sink fixpoint, then the final
// literal harvest.
func (s *scanner) Run() Scan {
	s.collectTables()
	us := s.units()

	// Two mutually-dependent fixpoints, run together:
	//   sinks   — a parameter whose ARGUMENT is a node-type literal.
	//   sources — a parameter that RECEIVES a node-type value, which makes
	//             literals compared against it inside the callee node types.
	// sources feeds ntLocals, ntLocals decides which arguments count as
	// node-type values, and that decides sources. Iterate until neither grows.
	for {
		grew := false
		argSites := map[paramKey]int{}
		argNT := map[paramKey]int{}
		for _, u := range us {
			_, hits, feeds := s.analyzeFunc(u.pkg.TypesInfo, u.fn, u.body, u.ftype)
			for i := range hits {
				k := paramKey{u.fn, i}
				if !s.sinks[k] {
					s.sinks[k] = true
					grew = true
				}
			}
			for _, f := range feeds {
				argSites[f.key]++
				if f.nt {
					argNT[f.key]++
				}
			}
		}
		s.argSites, s.argNTSites = argSites, argNT
		// Every VISIBLE call site must carry a node type — visible meaning
		// resolved by calleeFunc, in a package the loader read (see the
		// soundness note on the sources field; test files are not loaded).
		// A helper called once with n.Type() and once with an ordinary string
		// is polymorphic, and resolving its literals as node types would
		// invent failures.
		for k, n := range argSites {
			if n > 0 && argNT[k] == n && !s.sources[k] {
				s.sources[k] = true
				grew = true
			}
		}
		if !grew {
			break
		}
	}

	var res Scan
	seen := map[Site]bool{}
	add := func(p *packages.Package, pos position) {
		site := s.siteFor(p, pos)
		if seen[site] {
			return
		}
		seen[site] = true
		if !site.Const {
			res.Dynamic = append(res.Dynamic, site)
		} else {
			res.Sites = append(res.Sites, site)
		}
	}
	for _, u := range us {
		poss, _, _ := s.analyzeFunc(u.pkg.TypesInfo, u.fn, u.body, u.ftype)
		for _, p := range poss {
			add(u.pkg, p)
		}
	}
	for p, poss := range s.tablePositions() {
		for _, pos := range poss {
			add(p, pos)
		}
	}
	for k := range s.sinks {
		res.Sinks = append(res.Sinks, fmt.Sprintf("%s.%s#%d", k.fn.Pkg().Path(), k.fn.Name(), k.idx))
	}
	for k := range s.sources {
		res.Sources = append(res.Sources, fmt.Sprintf("%s.%s#%d", k.fn.Pkg().Path(), k.fn.Name(), k.idx))
	}
	sort.Strings(res.Sinks)
	sort.Strings(res.Sources)
	sort.Slice(res.Sites, func(i, j int) bool { return siteLess(res.Sites[i], res.Sites[j]) })
	sort.Slice(res.Dynamic, func(i, j int) bool { return siteLess(res.Dynamic[i], res.Dynamic[j]) })
	return res
}

func siteLess(a, b Site) bool {
	if a.File != b.File {
		return a.File < b.File
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	if a.Lit != b.Lit {
		return a.Lit < b.Lit
	}
	return a.Form < b.Form
}

func (s *scanner) siteFor(p *packages.Package, pos position) Site {
	tp := s.fset.Position(pos.expr.Pos())
	file := tp.Filename
	if rel, err := filepath.Rel(s.modRoot, file); err == nil && !strings.HasPrefix(rel, "..") {
		file = filepath.ToSlash(rel)
	}
	site := Site{
		Alias: pos.alias,
		Pkg:   p.PkgPath,
		Dir:   filepath.ToSlash(filepath.Dir(file)),
		File:  file,
		Line:  tp.Line,
		Form:  pos.form,
	}
	// Constant folding: a `const K = string(types.X)` still yields a string
	// constant here, so hiding a literal behind a converted constant cannot
	// silently zero the site count (the #6983 trap).
	if tv, ok := p.TypesInfo.Types[pos.expr]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		site.Lit = constant.StringVal(tv.Value)
		site.Const = true
	}
	return site
}

func calleeFunc(info *types.Info, call *ast.CallExpr) *types.Func {
	switch fn := astUnparen(call.Fun).(type) {
	case *ast.Ident:
		f, _ := info.ObjectOf(fn).(*types.Func)
		return f
	case *ast.SelectorExpr:
		f, _ := info.ObjectOf(fn.Sel).(*types.Func)
		return f
	}
	return nil
}
