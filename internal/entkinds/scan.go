// Package entkinds reads a grafel source tree and reports every ENTITY kind it
// declares — the entity vocabulary the graph will actually carry, as opposed to
// the vocabulary types.AllEntityKinds() says it carries.
//
// It is the entity-kind twin of internal/relkinds (#6757), and it exists for
// the same structural reason, discovered separately as #6744.
//
// # Why a Go-literal scan is not enough (#6744)
//
// internal/types/producer_kinds_test.go enumerates producers by parsing Go
// source for `Kind: "..."` literals on Entity / EntityRecord composite
// literals. That scan is structurally blind to a second producer family:
//
//	internal/engine/rules/**/*.yaml declares entity kinds as `entity_type:`
//	(and, on unbound keys, `entity_mapping:`). internal/engine/schema.go binds
//	source_patterns[].entity_type and file_conventions[].entity_type, and
//	internal/engine/detector.go:411 writes the value STRAIGHT THROUGH into
//	types.EntityRecord.Kind with no validation at all.
//
// So the rule layer can mint any kind string it likes — valid, retired, or
// misspelled — and no guard in the repository sees it. #6451 retired
// SCOPE.ExternalAPI from the enum precisely so a stale producer would fail
// IsValidEntityKind; the YAML producers sit outside that net, which is how
// three rule sites kept emitting `ExternalAPI` through the rename.
//
// # Bound vs unbound declarations
//
// A rule file may name an entity kind under a key the engine schema does not
// read — kubernetes/extras.yaml's `entity_extraction.mappings[].entity_type`
// and `k8s_resource_types.*[].entity_mapping` are the live examples. Those
// declarations reach no code today, so Site.Bound is false for them. They are
// still scanned, and deliberately so: they are the rule tree's own statement of
// what kind a resource maps to, they are what a future binding would activate,
// and leaving them unscanned is how the #6451 collision survived in the rule
// layer in the first place. A caller that cares can filter on Bound.
//
// The Bound decision is made from the YAML PATH, not from a schema decode, so
// that a declaration on an unknown key is reported rather than dropped. Keep
// boundPaths in step with internal/engine/schema.go.
//
// # What it deliberately does not see
//
// The Go scan resolves only source constants (a string literal, or an
// identifier / `string(ident)` naming a package-level string constant). A kind
// computed at run time is reported as unresolved rather than guessed at. The
// YAML scan reads scalars only; a sequence or an empty value is unresolved.
//
// # Scoping, and the two ways a bare-name resolver fabricates a kind (#6776)
//
// Both of these were live here and both produced a CONFIDENT WRONG ANSWER —
// the opposite failure direction from the "unresolved" one above, and the more
// dangerous one, because a fabricated kind is indistinguishable from a real
// declaration in the output.
//
//   - A SELECTOR is resolved only when its qualifier names a package this file
//     imports. `types.EntityKindRoute` resolves; `e.Kind` — a field read on a
//     value that merely happens to be spelled like a constant in scope — does
//     not, and is reported unresolved. Resolving by the selector's final
//     identifier alone reported a run-time field read as a source constant.
//   - A BARE IDENTIFIER is resolved only against package-level constants in
//     its OWN directory, which is what Go scoping actually permits: a bare name
//     cannot refer to another package's constant. A tree-wide fallback let a
//     FUNCTION-LOCAL `const localName = ...` resolve to an unrelated package's
//     `localName`, silently reporting a kind that appears nowhere at that site.
//
// The residual imprecision is stated rather than hidden: a selector still
// resolves against package-level constants tree-wide rather than against the
// specific package the qualifier names, so an import alias pointing at a
// different package than its path's last element could in principle mis-bind.
// `ambiguous` blunts it — a name declared with two DIFFERENT values anywhere in
// the tree resolves to neither — and function-local constants are not collected
// at all, so they are reported unresolved rather than guessed at.
//
// A MULTI-DOCUMENT YAML file is read only as far as its first document.
// yaml.Unmarshal decodes one document, so a kind declared after a `---`
// separator is invisible to this scan. That is not a hole the rule tree can
// currently fall through: internal/engine/loader.go:142 has the same
// limit, so a second document declares nothing that reaches the graph either.
// It becomes a hole the moment the loader learns to read them, and it is
// recorded here rather than discovered then.
//
// _test.go files and testdata/ are out of reach on purpose: fixtures
// legitimately emit invented kinds, and folding them in would make the
// population meaningless.
package entkinds

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Origin names the declaration mechanism a Site came from.
const (
	// OriginGo is a Go composite-literal declaration.
	OriginGo = "go"
	// OriginRuleYAML is an `entity_type:` / `entity_mapping:` key in a rule
	// YAML file.
	OriginRuleYAML = "rule_yaml"
)

// Site is one place an entity kind is declared.
type Site struct {
	// File is the path relative to the scanned root, slash-separated.
	File string
	// Line is the 1-based line of the declaration.
	Line int
	// Kind is the entity-kind string as it will reach the graph.
	Kind string
	// Origin is OriginGo or OriginRuleYAML.
	Origin string
	// Path is the YAML key path of the declaration, with `[]` for sequence
	// steps (e.g. "source_patterns[].entity_type"). Empty for OriginGo.
	Path string
	// Bound reports whether internal/engine/schema.go actually decodes this
	// path, i.e. whether the declaration reaches EntityRecord.Kind today.
	// Always false for OriginGo sites, which carry Detail instead.
	Bound bool
	// Detail describes how the value was read — the Go type and field, or the
	// YAML path — so a failure message can point at the mechanism.
	Detail string
}

// String renders a site for a failure message.
func (s Site) String() string {
	return fmt.Sprintf("%s (%s) at %s:%d [%s]", s.Kind, s.Origin, s.File, s.Line, s.Detail)
}

// Result is a scan's output plus the evidence that the scan actually read
// something. FilesParsed is load-bearing: a walk that reaches no files reports
// no sites and looks identical to a clean tree.
type Result struct {
	Sites []Site
	// FilesParsed is GoFilesParsed + YAMLFilesParsed for Scan, and the single
	// mechanism's count for ScanGo / ScanRuleYAML.
	FilesParsed int
	// GoFilesParsed / YAMLFilesParsed are the per-mechanism counts, so a caller
	// can tell "the YAML half read nothing" from "the tree has no YAML".
	GoFilesParsed   int
	YAMLFilesParsed int
	// UnresolvedSites are entity-kind declarations whose value this package
	// cannot read. They are reported, not dropped, so the blind spot is visible
	// instead of silent. Kind is empty on these; Detail names the mechanism.
	UnresolvedSites []Site
}

// Unresolved is the number of entity-kind declarations the scan could not read.
func (r Result) Unresolved() int { return len(r.UnresolvedSites) }

// Kinds returns the distinct kinds in the result, sorted.
func (r Result) Kinds() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range r.Sites {
		if !seen[s.Kind] {
			seen[s.Kind] = true
			out = append(out, s.Kind)
		}
	}
	sort.Strings(out)
	return out
}

// SitesFor returns every site declaring kind, sorted by file and line.
func (r Result) SitesFor(kind string) []Site {
	var out []Site
	for _, s := range r.Sites {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// Scan runs both halves over root and returns their union.
func Scan(root string) (Result, error) {
	goRes, err := ScanGo(root)
	if err != nil {
		return Result{}, err
	}
	yamlRes, err := ScanRuleYAML(root)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Sites:           append(append([]Site{}, goRes.Sites...), yamlRes.Sites...),
		FilesParsed:     goRes.FilesParsed + yamlRes.FilesParsed,
		GoFilesParsed:   goRes.FilesParsed,
		YAMLFilesParsed: yamlRes.FilesParsed,
		UnresolvedSites: append(append([]Site{}, goRes.UnresolvedSites...), yamlRes.UnresolvedSites...),
	}, nil
}

// skippedDir reports directories the walks refuse to descend into.
//
// .claude is not an optimisation: it holds full worktree checkouts of this same
// repository, so walking it would scan (and report) other branches' rule files.
// testdata holds deliberate fixtures, including invalid ones.
func skippedDir(name string) bool {
	switch name {
	case ".git", ".claude", "node_modules", "vendor", "testdata", "dist", "build":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Rule YAML
// ---------------------------------------------------------------------------

// entityKeyNames are the YAML keys that name an entity kind. `entity_type` is
// the engine schema's spelling; `entity_mapping` is the spelling
// kubernetes/extras.yaml uses on its (currently unbound) resource tables.
var entityKeyNames = map[string]bool{
	"entity_type":    true,
	"entity_mapping": true,
}

// boundPaths are the YAML paths internal/engine/schema.go actually decodes into
// FrameworkRule, and therefore the paths whose value reaches
// types.EntityRecord.Kind via detector.go. Anything else is a declaration the
// engine does not read today.
//
// This must stay in step with internal/engine/schema.go's FileConvention.
// EntityType and SourcePattern.EntityType yaml tags; the guard's live scan of
// the real rule tree is what asserts it has not drifted.
var boundPaths = map[string]bool{
	"source_patterns[].entity_type":  true,
	"file_conventions[].entity_type": true,
}

// ScanRuleYAML walks root for YAML files and reports every entity kind declared
// by an `entity_type:` or `entity_mapping:` key, at any depth.
//
// The walk is over the whole YAML document rather than a decode of
// FrameworkRule on purpose. A schema decode sees only the keys the schema
// already binds, so a kind declared on an unbound key — which is exactly where
// the #6744 kubernetes/extras.yaml collision lived — would be reported as
// absent. See the package doc on Bound.
func ScanRuleYAML(root string) (Result, error) {
	var res Result
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skippedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		src, rerr := os.ReadFile(path) // #nosec G304 -- scanning a source tree the caller named
		if rerr != nil {
			return rerr
		}
		// Counted as read before the parse: the file WAS reached, and a caller
		// checking coverage is asking about the walk, not about YAML validity.
		// Folding a parse failure into the coverage number would report a
		// malformed file somewhere in the tree as "the walk is broken".
		res.YAMLFilesParsed++
		res.FilesParsed++
		var doc yaml.Node
		if uerr := yaml.Unmarshal(src, &doc); uerr != nil {
			// A YAML file the engine itself could not load declares nothing.
			return nil //nolint:nilerr // unloadable rule files declare no kinds
		}
		sites, unresolved := yamlSitesIn(&doc, filepath.ToSlash(rel))
		res.Sites = append(res.Sites, sites...)
		res.UnresolvedSites = append(res.UnresolvedSites, unresolved...)
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

// yamlSitesIn walks one document, carrying the dotted key path down.
func yamlSitesIn(doc *yaml.Node, rel string) (sites, unresolved []Site) {
	var visit func(n *yaml.Node, path string)
	visit = func(n *yaml.Node, path string) {
		switch n.Kind {
		case yaml.DocumentNode, yaml.AliasNode:
			for _, c := range n.Content {
				visit(c, path)
			}
		case yaml.SequenceNode:
			for _, c := range n.Content {
				visit(c, path+"[]")
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				child := k.Value
				if path != "" {
					child = path + "." + k.Value
				}
				if entityKeyNames[k.Value] {
					site := Site{
						File:   rel,
						Line:   k.Line,
						Origin: OriginRuleYAML,
						Path:   child,
						Bound:  boundPaths[child],
						Detail: child,
					}
					if v.Kind == yaml.ScalarNode && v.Value != "" {
						site.Kind = v.Value
						sites = append(sites, site)
					} else {
						site.Detail = child + " = <not a scalar>"
						unresolved = append(unresolved, site)
					}
				}
				visit(v, child)
			}
		}
	}
	visit(doc, "")
	return sites, unresolved
}

// ---------------------------------------------------------------------------
// Go source
// ---------------------------------------------------------------------------

// entityTypeNames are the composite-literal type names whose `Kind` field
// carries an entity kind. Matching is on the type's final identifier, so
// `types.EntityRecord`, `graph.Entity` and a package-local `Entity` all match.
//
// graph.Relationship also has a `Kind` field, carrying a RELATIONSHIP kind; it
// is excluded by this type half, and is internal/relkinds' business.
var entityTypeNames = map[string]bool{
	"Entity":       true,
	"EntityRecord": true,
}

// ScanGo walks root for non-test .go files and reports every entity kind
// declared by a composite literal.
func ScanGo(root string) (Result, error) {
	// Package-level string constants, collected first so a declaration in one
	// file can be resolved from another file of the same directory and, failing
	// that, from anywhere in the tree.
	perDir := map[string]map[string]string{}
	global := map[string]string{}
	ambiguous := map[string]bool{}

	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skippedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	sort.Strings(files)

	type parsed struct {
		rel  string
		fset *token.FileSet
		file *ast.File
		dir  string
	}
	var asts []parsed
	for _, path := range files {
		src, rerr := os.ReadFile(path) // #nosec G304 -- scanning a source tree the caller named
		if rerr != nil {
			return Result{}, rerr
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if perr != nil {
			return Result{}, fmt.Errorf("parse %s: %w", path, perr)
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return Result{}, rerr
		}
		dir := filepath.Dir(path)
		asts = append(asts, parsed{rel: filepath.ToSlash(rel), fset: fset, file: f, dir: dir})

		for name, val := range stringConsts(f) {
			if perDir[dir] == nil {
				perDir[dir] = map[string]string{}
			}
			perDir[dir][name] = val
			if prev, ok := global[name]; ok && prev != val {
				ambiguous[name] = true
			}
			global[name] = val
		}
	}

	res := Result{FilesParsed: len(asts), GoFilesParsed: len(asts)}
	for _, p := range asts {
		// A bare identifier can only name a constant of its own package, so it
		// resolves against its own directory and nowhere else. A tree-wide
		// fallback here is what let a function-local constant resolve to an
		// unrelated package's same-named one (#6776).
		resolveLocal := func(name string) (string, bool) {
			v, ok := perDir[p.dir][name]
			return v, ok
		}
		// A qualified selector names another package's constant, which is not
		// in perDir. `ambiguous` drops any name two packages declare with
		// different values, so a collision resolves to neither rather than to
		// whichever the walk saw last.
		resolveQualified := func(name string) (string, bool) {
			if ambiguous[name] {
				return "", false
			}
			v, ok := global[name]
			return v, ok
		}
		sites, unresolved := goSitesIn(p.fset, p.file, p.rel, resolveLocal, resolveQualified, importNames(p.file))
		res.Sites = append(res.Sites, sites...)
		res.UnresolvedSites = append(res.UnresolvedSites, unresolved...)
	}
	return res, nil
}

// stringConsts returns every package-level constant in f whose value is a
// string literal, keyed by name.
func stringConsts(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) || name.Name == "_" {
					continue
				}
				if v, ok := stringLit(vs.Values[i]); ok {
					out[name.Name] = v
				}
			}
		}
	}
	return out
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return v, true
}

// goSitesIn walks one file for entity composite literals. Untyped elements are
// handled: in `[]types.EntityRecord{{Kind: "X"}}` the inner literal has no Type
// of its own and inherits the slice's element type.
func goSitesIn(fset *token.FileSet, f *ast.File, rel string, resolveLocal, resolveQualified func(string) (string, bool), imports map[string]bool) (sites, unresolved []Site) {
	var visit func(n ast.Node, inherited string)
	visit = func(n ast.Node, inherited string) {
		ast.Inspect(n, func(node ast.Node) bool {
			cl, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			name := typeNameOf(cl.Type)
			if name == "" {
				name = inherited
			}
			elem := elemTypeNameOf(cl.Type)
			if elem == "" && cl.Type == nil {
				elem = inherited
			}
			if entityTypeNames[name] {
				for _, el := range cl.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || key.Name != "Kind" {
						continue
					}
					val, ok := constValueOf(kv.Value, resolveLocal, resolveQualified, imports)
					if !ok {
						unresolved = append(unresolved, Site{
							File:   rel,
							Line:   fset.Position(kv.Pos()).Line,
							Origin: OriginGo,
							Detail: name + ".Kind = <not a source constant>",
						})
						continue
					}
					sites = append(sites, Site{
						File:   rel,
						Line:   fset.Position(kv.Pos()).Line,
						Kind:   val,
						Origin: OriginGo,
						Detail: name + ".Kind",
					})
				}
			}
			for _, el := range cl.Elts {
				visit(el, elem)
			}
			return false
		})
	}
	for _, decl := range f.Decls {
		visit(decl, "")
	}
	return sites, unresolved
}

// typeNameOf returns the final identifier of a composite-literal type.
func typeNameOf(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.StarExpr:
		return typeNameOf(t.X)
	}
	return ""
}

// elemTypeNameOf returns the element type name of a slice, array or map type,
// which is the type its type-less elements take.
func elemTypeNameOf(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.ArrayType:
		return typeNameOf(t.Elt)
	case *ast.MapType:
		return typeNameOf(t.Value)
	}
	return ""
}

// constValueOf resolves an expression to a source-constant string.
//
// The two resolvers are deliberately NOT interchangeable — see the package doc.
// A bare identifier goes to resolveLocal (its own package only); a selector
// goes to resolveQualified, and only after its qualifier is confirmed to name
// an imported package, so a field read like `e.Kind` is reported unresolved
// instead of being answered with whatever constant shares the field's name.
func constValueOf(e ast.Expr, resolveLocal, resolveQualified func(string) (string, bool), imports map[string]bool) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		return stringLit(v)
	case *ast.Ident:
		return resolveLocal(v.Name)
	case *ast.SelectorExpr:
		pkg, ok := v.X.(*ast.Ident)
		if !ok || !imports[pkg.Name] {
			return "", false
		}
		return resolveQualified(v.Sel.Name)
	case *ast.CallExpr:
		if len(v.Args) != 1 {
			return "", false
		}
		switch v.Fun.(type) {
		case *ast.Ident, *ast.SelectorExpr:
			return constValueOf(v.Args[0], resolveLocal, resolveQualified, imports)
		}
	}
	return "", false
}

// importNames returns the identifiers by which f can refer to an imported
// package: the explicit alias where there is one, otherwise the import path's
// last element. It is the qualifier whitelist for the selector case.
func importNames(f *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, imp := range f.Imports {
		if imp.Name != nil {
			if imp.Name.Name != "_" && imp.Name.Name != "." {
				out[imp.Name.Name] = true
			}
			continue
		}
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if i := strings.LastIndex(path, "/"); i >= 0 {
			path = path[i+1:]
		}
		if path != "" {
			out[path] = true
		}
	}
	return out
}

// BoundPaths returns, sorted, the YAML paths this package treats as actually
// decoded by internal/engine/schema.go. It is exported so a guard can assert
// the mirror has not drifted from the schema it mirrors.
func BoundPaths() []string {
	out := make([]string, 0, len(boundPaths))
	for p := range boundPaths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
