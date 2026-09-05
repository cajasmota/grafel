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

	"github.com/cajasmota/grafel/internal/repowalk"
)

// Origin names the declaration mechanism a Site came from.
const (
	// OriginGo is a Go composite-literal declaration.
	OriginGo = "go"
	// OriginRuleYAML is an `entity_type:` / `entity_mapping:` key in a rule
	// YAML file.
	OriginRuleYAML = "rule_yaml"
	// OriginGoReference is a Go source MENTION of a kind string — producer or
	// consumer — as reported by ScanGoReferences. It is never produced by Scan,
	// ScanGo or ScanRuleYAML.
	OriginGoReference = "go_reference"
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
			if repowalk.SkippedDir(d.Name()) {
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

// parsedGoFile is one parsed non-test Go file of the scanned tree.
type parsedGoFile struct {
	rel  string
	fset *token.FileSet
	file *ast.File
	dir  string
}

// goTree is a parsed source tree plus the constant tables the two Go scans
// resolve names against. It exists so ScanGo and ScanGoReferences answer from
// the SAME parse and the SAME resolver — one resolver, one hole (#6776).
type goTree struct {
	files     []parsedGoFile
	perDir    map[string]map[string]string
	global    map[string]string
	ambiguous map[string]bool
}

// resolvers returns the (local, qualified) resolver pair for one file. See
// ScanGo's body and the package doc for why the two are not interchangeable.
func (t goTree) resolvers(p parsedGoFile) (local, qualified func(string) (string, bool)) {
	// A bare identifier can only name a constant of its own package, so it
	// resolves against its own directory and nowhere else. A tree-wide
	// fallback here is what let a function-local constant resolve to an
	// unrelated package's same-named one (#6776).
	local = func(name string) (string, bool) {
		v, ok := t.perDir[p.dir][name]
		return v, ok
	}
	// A qualified selector names another package's constant, which is not in
	// perDir. `ambiguous` drops any name two packages declare with different
	// values, so a collision resolves to neither rather than to whichever the
	// walk saw last.
	qualified = func(name string) (string, bool) {
		if t.ambiguous[name] {
			return "", false
		}
		v, ok := t.global[name]
		return v, ok
	}
	return local, qualified
}

// parseGoTree walks root for non-test .go files, parses each and collects the
// package-level string constants a later resolve may need.
func parseGoTree(root string) (goTree, error) {
	tree := goTree{
		perDir:    map[string]map[string]string{},
		global:    map[string]string{},
		ambiguous: map[string]bool{},
	}

	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if repowalk.SkippedDir(d.Name()) {
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
		return goTree{}, err
	}
	sort.Strings(files)

	for _, path := range files {
		src, rerr := os.ReadFile(path) // #nosec G304 -- scanning a source tree the caller named
		if rerr != nil {
			return goTree{}, rerr
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if perr != nil {
			return goTree{}, fmt.Errorf("parse %s: %w", path, perr)
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return goTree{}, rerr
		}
		dir := filepath.Dir(path)
		tree.files = append(tree.files, parsedGoFile{rel: filepath.ToSlash(rel), fset: fset, file: f, dir: dir})

		for name, val := range stringConsts(f) {
			if tree.perDir[dir] == nil {
				tree.perDir[dir] = map[string]string{}
			}
			tree.perDir[dir][name] = val
			if prev, ok := tree.global[name]; ok && prev != val {
				tree.ambiguous[name] = true
			}
			tree.global[name] = val
		}
	}
	return tree, nil
}

// ScanGo walks root for non-test .go files and reports every entity kind
// declared by a composite literal.
func ScanGo(root string) (Result, error) {
	tree, err := parseGoTree(root)
	if err != nil {
		return Result{}, err
	}
	asts := tree.files

	res := Result{FilesParsed: len(asts), GoFilesParsed: len(asts)}
	for _, p := range asts {
		resolveLocal, resolveQualified := tree.resolvers(p)
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

// ---------------------------------------------------------------------------
// Go references (#6818)
// ---------------------------------------------------------------------------

// ScanGoReferences walks root for non-test .go files and reports the places a
// string in `wanted` appears as a resolvable source value — a string literal,
// or an identifier / selector / one-argument conversion naming a package-level
// string constant, resolved by exactly the resolver ScanGo uses.
//
// "The places", not "every place": the claim is bounded by what is not walked
// (names in declaring position, a selector's Sel half) and what is not resolved
// (anything assembled at run time). An earlier version of this doc said "every
// place" while the selector case pruned whole subtrees — 40 chained-literal
// sites in 22 files of this repo — so the sentence is now the weaker true one,
// and the strong claim that matters, that this scan sees every (file, kind)
// pair ScanGo does, is MEASURED against the live tree by
// TestEnumEntityKinds6818_EveryScanGoFileKindPairIsAlsoAGoReference rather than
// promised here.
//
// # This asks a DIFFERENT question from ScanGo, deliberately (#6818)
//
// ScanGo asks "does a Go PRODUCER write this kind?" and answers it by reading
// the `Kind:` field of an Entity / EntityRecord composite literal. That is a
// sound answer in one direction only: a producer that assigns (`e.Kind = "X"`),
// that passes the kind as a FUNCTION ARGUMENT (`emitEntity(id, someKind, ...)`,
// the shape that hid a third SCOPE.ScheduledJob producer through #6776 arm B4),
// or that builds some other struct entirely is INVISIBLE to it. "ScanGo found
// no site" therefore means "no composite-literal producer", never "nothing
// emits this".
//
// This function asks the weaker, and here the answerable, question: does ANY
// non-test Go source outside the scanned exclusion MENTION this string —
// producer or consumer, write side or read side? It does not distinguish the
// two and does not pretend to. A caller that needs "producer" must use ScanGo
// and live with its blind spot; a caller that needs "is this kind connected to
// anything real at all" wants this.
//
// # Which way it errs
//
// It resolves SOURCE values only, the same as ScanGo: a kind assembled at run
// time is not seen. So a MISS is possible and a FABRICATION is not — the scan
// can report a live kind as unreferenced, never an unreferenced one as live.
// For a guard that treats "referenced" as the passing condition, that is the
// safe direction: the failure mode is a spurious red naming a real file, not a
// silent green.
//
// UnresolvedSites is always empty here, and that is not an oversight. ScanGo
// reports unresolved sites because it has found a producer whose kind it cannot
// read — a specific, nameable blind spot. This scan is a membership test over a
// caller-supplied set; an expression it cannot read is not a failed read of a
// reference, it is simply not evidence of one, and there is no site to name.
func ScanGoReferences(root string, wanted []string) (Result, error) {
	want := make(map[string]bool, len(wanted))
	for _, w := range wanted {
		want[w] = true
	}
	tree, err := parseGoTree(root)
	if err != nil {
		return Result{}, err
	}
	res := Result{FilesParsed: len(tree.files), GoFilesParsed: len(tree.files)}
	for _, p := range tree.files {
		resolveLocal, resolveQualified := tree.resolvers(p)
		imports := importNames(p.file)
		record := func(pos token.Pos, val, detail string) {
			if !want[val] {
				return
			}
			res.Sites = append(res.Sites, Site{
				File:   p.rel,
				Line:   p.fset.Position(pos).Line,
				Kind:   val,
				Origin: OriginGoReference,
				Detail: detail,
			})
		}
		var walk func(ast.Node) bool
		walk = func(n ast.Node) bool {
			switch e := n.(type) {
			// The cases below skip the positions where a bare identifier
			// CANNOT be a reference to a kind string and can only COLLIDE with
			// one. Two positions qualify:
			//
			//   - a DECLARED NAME. `const widgetKind = "SCOPE.Widget"` mentions
			//     the kind once, not twice.
			//   - a TYPE EXPRESSION. A type name is not a value, so an
			//     identifier there never denotes a kind string — but it can be
			//     spelled like a same-package constant, and then resolveLocal
			//     answers with that constant's value. Package scope cannot
			//     produce such a collision (a package cannot declare a const
			//     and a type of one name), so the shapes that can are exactly
			//     the local ones: a struct FIELD name, a TYPE PARAMETER, and a
			//     function-local type.
			//
			// Both are the FABRICATION direction, which this scan must never
			// take. What each case gives up instead is stated on it; the losses
			// are misses, and the subset test over the live tree is what says
			// whether a miss has started to matter.
			case *ast.ValueSpec:
				// Names declared; Type is a type expression.
				for _, v := range e.Values {
					ast.Inspect(v, walk)
				}
				return false
			case *ast.Field:
				// Struct fields, parameters, results, interface methods and
				// type parameters: Names are declared and Type is a type
				// expression. The TAG is walked — it is a real string literal
				// of the source, and dropping it would silently narrow what a
				// struct can say about a kind.
				if e.Tag != nil {
					ast.Inspect(e.Tag, walk)
				}
				return false
			case *ast.TypeSpec:
				// Name and TypeParams are declared; Type is walked because a
				// struct's field TAGS live inside it.
				if e.Type != nil {
					ast.Inspect(e.Type, walk)
				}
				return false
			case *ast.FuncDecl:
				// Name is declared. Type is a *ast.FuncType and is dropped by
				// the type-expression case below; Recv reaches Fields, which
				// drop their own types.
				if e.Recv != nil {
					ast.Inspect(e.Recv, walk)
				}
				if e.Body != nil {
					ast.Inspect(e.Body, walk)
				}
				return false
			case *ast.CompositeLit:
				// Type is a type expression. A KEY spelled as a bare identifier
				// is a struct FIELD NAME — `box{Widget: 1}` — and the AST
				// cannot tell it from a map key without types, so it is not
				// walked. That gives up a map literal keyed by a kind CONSTANT
				// (`map[string]bool{widgetKind: true}`), which becomes a miss;
				// a map keyed by a kind LITERAL, the shape this repo actually
				// uses, is unaffected because its key is not an identifier.
				for _, el := range e.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						ast.Inspect(el, walk)
						continue
					}
					if _, isIdent := kv.Key.(*ast.Ident); !isIdent {
						ast.Inspect(kv.Key, walk)
					}
					ast.Inspect(kv.Value, walk)
				}
				return false
			case *ast.TypeAssertExpr:
				// X is a value, Type is a type expression.
				ast.Inspect(e.X, walk)
				return false
			case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType:
				// Pure type expressions wherever they appear, including as an
				// argument to make/new. An array LENGTH is given up with them;
				// a string constant cannot be one.
				return false
			case *ast.BasicLit:
				if v, ok := stringLit(e); ok {
					record(e.Pos(), v, "string literal")
				}
			case *ast.SelectorExpr:
				// The Sel identifier is NEVER walked: on its own it is a field
				// or method name as often as a constant name, and resolving it
				// as a bare identifier is the fabrication #6776 removed from
				// ScanGo. What happens to X depends on what X is.
				if pkg, ok := e.X.(*ast.Ident); ok {
					// A bare qualifier: either an imported package (a real
					// qualified constant) or a value whose field is being read
					// (`e.Kind`). Neither has a subtree worth walking.
					if imports[pkg.Name] {
						if v, ok := resolveQualified(e.Sel.Name); ok {
							record(e.Pos(), v, "selector "+pkg.Name+"."+e.Sel.Name)
						}
					}
					return false
				}
				// X is an EXPRESSION, not a qualifier — `graph.Entity{Kind:
				// "SCOPE.X"}.WithProperties(p)` is the live shape, 40 sites in
				// 22 files. Pruning here dropped the whole literal, which is
				// how three sites ScanGo resolves were invisible to this scan.
				ast.Inspect(e.X, walk)
				return false
			case *ast.Ident:
				if v, ok := resolveLocal(e.Name); ok {
					record(e.Pos(), v, "identifier "+e.Name)
				}
			}
			return true
		}
		ast.Inspect(p.file, walk)
	}
	return res, nil
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
