// Package relkinds reads a grafel source tree and reports every relationship
// kind it declares — the vocabulary the graph will actually carry, as opposed
// to the vocabulary types.AllRelationshipKinds() says it carries.
//
// # Why this exists (#6757)
//
// types.IsValidRelationshipKind has no non-test caller, so the relationship
// vocabulary is enforced by nothing and any string can reach the graph. Before
// the validator can be wired anywhere, the population of strings that would be
// rejected has to be known. That population is NOT discoverable by reading
// kinds.go — the kinds are declared in three different places:
//
//  1. Go string literals in relationship composite literals, e.g.
//     internal/extractors/sql/sql.go's `Kind: "INDEXES"`, or the custom-Java
//     `Relationship{RelationshipType: "OWNS"}` shape whose RelationshipType is
//     copied verbatim into graph.Relationship.Kind by
//     internal/custom/java/patterns_dispatch.go.
//  2. Package-local Go constants that were never registered in internal/types,
//     e.g. internal/engine/process_flow_kinds.go's STEP_IN_PROCESS, reaching the
//     literal through a `string(...)` conversion.
//  3. `relationship_rules:` entries in the rule YAML under
//     internal/engine/rules. internal/engine/schema.go binds them and
//     detector.go compiles rr.Relationship UNVALIDATED, then writes it straight
//     into types.RelationshipRecord.Kind.
//
// Mechanism 3 is why this package exists rather than a grep. A Go-literal scan
// alone sees roughly half the population and reports success — the exact trap
// #6757 was filed to avoid.
//
// # What it deliberately does not see
//
// The Go scan resolves only values that are constant in the source: string
// literals, and identifiers or `string(ident)` conversions naming a
// package-level string constant. A kind computed at runtime — the `relType`
// variable at internal/custom/java/caching.go:243 is the live example — is not
// a declaration this package can read, and is reported as unresolved rather
// than guessed at. The same goes for a kind assembled by concatenation.
//
// _test.go files are out of reach on purpose: test fixtures legitimately emit
// invented kinds, and folding them in would make the population meaningless.
package relkinds

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
	// OriginGo is a Go source declaration (mechanisms 1 and 2).
	OriginGo = "go"
	// OriginRuleYAML is a `relationship_rules:` entry in a rule YAML file.
	OriginRuleYAML = "rule_yaml"
)

// Site is one place a relationship kind is declared.
type Site struct {
	// File is the path relative to the scanned root, slash-separated.
	File string
	// Line is the 1-based line of the declaration.
	Line int
	// Kind is the relationship-kind string as it will reach the graph.
	Kind string
	// Origin is OriginGo or OriginRuleYAML.
	Origin string
	// Detail describes how the value was read, e.g. the field name or the
	// constant it resolved through. It exists so a failure message can point
	// at the mechanism, not just the string.
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
	// FilesParsed counts the files this scan parsed. For Scan it is the sum of
	// GoFilesParsed and YAMLFilesParsed.
	FilesParsed int
	// GoFilesParsed / YAMLFilesParsed are the per-mechanism counts, so a caller
	// can tell "the YAML half read nothing" from "the tree has no YAML".
	GoFilesParsed   int
	YAMLFilesParsed int
	// UnresolvedSites are relationship-kind fields whose value is not a source
	// constant (a variable, a call, a concatenation) — declarations this
	// package cannot read. They are reported, not dropped, so the blind spot
	// is visible instead of silent. Kind is empty on these; Detail names the
	// field.
	UnresolvedSites []Site
}

// Unresolved is the number of relationship-kind fields the scan could not read.
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

// relationshipTypeNames are the composite-literal type names whose keyed fields
// carry a relationship kind. Matching is on the type's final identifier, so
// `types.RelationshipRecord`, `graph.Relationship` and a package-local
// `Relationship` all match.
var relationshipTypeNames = map[string]bool{
	"RelationshipRecord": true,
	"Relationship":       true,
}

// relationshipKindFields are the field names that hold the kind.
//
// `Kind` covers types.RelationshipRecord and graph.Relationship;
// `RelationshipType` covers the custom-Java internal Relationship, whose value
// patterns_dispatch.go copies verbatim into graph.Relationship.Kind.
//
// The pair (type name, field name) is what makes this safe: graph.Entity also
// has a `Kind` field, carrying an entity type rather than a relationship kind,
// and it is excluded by the type half.
var relationshipKindFields = map[string]bool{
	"Kind":             true,
	"RelationshipType": true,
}

// ScanGo walks root for non-test .go files and reports every relationship kind
// declared by a composite literal.
func ScanGo(root string) (Result, error) {
	// Package-level string constants, collected first so a declaration in one
	// file can be resolved from another file of the same directory (the
	// engine's process_flow_kinds.go / process_flow.go split), and, failing
	// that, from anywhere in the tree (a cross-package const reference).
	perDir := map[string]map[string]string{}
	global := map[string]string{}
	ambiguous := map[string]bool{}

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

	res := Result{FilesParsed: len(asts)}
	for _, p := range asts {
		resolve := func(name string) (string, bool) {
			if v, ok := perDir[p.dir][name]; ok {
				return v, true
			}
			if ambiguous[name] {
				return "", false
			}
			v, ok := global[name]
			return v, ok
		}
		sites, unresolved := goSitesIn(p.fset, p.file, p.rel, resolve)
		res.Sites = append(res.Sites, sites...)
		res.UnresolvedSites = append(res.UnresolvedSites, unresolved...)
	}
	return res, nil
}

// stringConsts returns every package-level constant in f whose value is a
// string literal, keyed by name. Const-block type/value elision is not
// resolved: an elided spec repeats an iota expression far more often than a
// string, and a missed constant is reported as unresolved rather than guessed.
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

// goSitesIn walks one file for relationship composite literals.
//
// Untyped elements are handled: in `[]types.RelationshipRecord{{Kind: "X"}}`
// the inner literal has no Type of its own and inherits the slice's element
// type. The walk therefore carries the enclosing literal's element type down
// into type-less children.
func goSitesIn(fset *token.FileSet, f *ast.File, rel string, resolve func(string) (string, bool)) (sites, unresolved []Site) {
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
				// A type-less literal nested inside another type-less literal
				// keeps whatever it inherited.
				elem = inherited
			}
			if relationshipTypeNames[name] {
				for _, el := range cl.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || !relationshipKindFields[key.Name] {
						continue
					}
					val, ok := constValueOf(kv.Value, resolve)
					if !ok {
						unresolved = append(unresolved, Site{
							File:   rel,
							Line:   fset.Position(kv.Pos()).Line,
							Origin: OriginGo,
							Detail: name + "." + key.Name + " = <not a source constant>",
						})
						continue
					}
					sites = append(sites, Site{
						File:   rel,
						Line:   fset.Position(kv.Pos()).Line,
						Kind:   val,
						Origin: OriginGo,
						Detail: name + "." + key.Name,
					})
				}
			}
			// Recurse manually so the inherited element type is carried.
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

// typeNameOf returns the final identifier of a composite-literal type:
// "RelationshipRecord" for `types.RelationshipRecord`, "" for a slice, map or
// absent type.
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

// constValueOf resolves an expression to a source-constant string. It accepts a
// string literal, an identifier or qualified identifier naming a string
// constant, and a one-argument conversion wrapping either — `string(X)`,
// `types.RelationshipKind(X)`. Anything else is not a declaration this package
// can read.
func constValueOf(e ast.Expr, resolve func(string) (string, bool)) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		return stringLit(v)
	case *ast.Ident:
		return resolve(v.Name)
	case *ast.SelectorExpr:
		return resolve(v.Sel.Name)
	case *ast.CallExpr:
		// A conversion has exactly one argument and a type for its callee.
		if len(v.Args) != 1 {
			return "", false
		}
		switch v.Fun.(type) {
		case *ast.Ident, *ast.SelectorExpr:
			return constValueOf(v.Args[0], resolve)
		}
	}
	return "", false
}

// ruleFile is the sliver of internal/engine's rule schema this scan needs. It
// is declared here rather than imported so that the guard does not depend on
// the engine package it is auditing; the yaml key must stay in step with
// internal/engine/schema.go's RelationshipRules / Relationship tags, which is
// asserted by the guard's live scan of the real rule tree.
type ruleFile struct {
	RelationshipRules []struct {
		Relationship string `yaml:"relationship"`
	} `yaml:"relationship_rules"`
}

// ScanRuleYAML walks root for rule YAML files and reports every kind declared
// by a `relationship_rules:` entry.
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
		res.FilesParsed++
		var doc yaml.Node
		if uerr := yaml.Unmarshal(src, &doc); uerr != nil {
			// A YAML file the engine itself could not load declares nothing.
			return nil //nolint:nilerr // unloadable rule files declare no kinds
		}
		var rf ruleFile
		if derr := doc.Decode(&rf); derr != nil {
			return nil //nolint:nilerr // a document with a different shape declares no rules
		}
		lines := relationshipRuleLines(&doc)
		for i, rr := range rf.RelationshipRules {
			if rr.Relationship == "" {
				res.UnresolvedSites = append(res.UnresolvedSites, Site{
					File:   filepath.ToSlash(rel),
					Origin: OriginRuleYAML,
					Detail: "relationship_rules[" + strconv.Itoa(i) + "] has no relationship",
				})
				continue
			}
			line := 0
			if i < len(lines) {
				line = lines[i]
			}
			res.Sites = append(res.Sites, Site{
				File:   filepath.ToSlash(rel),
				Line:   line,
				Kind:   rr.Relationship,
				Origin: OriginRuleYAML,
				Detail: "relationship_rules[" + strconv.Itoa(i) + "].relationship",
			})
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

// relationshipRuleLines returns the source line of each `relationship:` key
// under relationship_rules, in order, so a failure message can name the line.
// A missing entry yields line 0 rather than a wrong line.
func relationshipRuleLines(doc *yaml.Node) []int {
	var out []int
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return out
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "relationship_rules" {
			continue
		}
		seq := root.Content[i+1]
		if seq.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range seq.Content {
			line := item.Line
			if item.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(item.Content); j += 2 {
					if item.Content[j].Value == "relationship" {
						line = item.Content[j].Line
					}
				}
			}
			out = append(out, line)
		}
	}
	return out
}
