// Package mage implements a build-system extractor for Mage
// (github.com/magefile/mage) magefiles.
//
// Mage is a make-like build tool written in Go: build targets are exported
// Go functions declared in a file (or directory) guarded by the `mage` build
// tag. Unlike Make or Task there is no DSL — the magefile is ordinary Go
// source. A function qualifies as a runnable target when it is exported and
// has one of Mage's accepted signatures:
//
//	func Target()
//	func Target() error
//	func Target(ctx context.Context)
//	func Target(ctx context.Context) error
//
// Inter-target dependencies are expressed through the mg helper package:
//
//	mg.Deps(Build, Test)        // parallel dependencies
//	mg.SerialDeps(Clean, Build) // ordered dependencies
//	mg.CtxDeps(ctx, Build)      // ctx-aware parallel dependencies
//
// The first argument of mg.CtxDeps is the context and is not a target.
//
// This extractor parses each magefile with go/parser (the magefile is real
// Go, so an AST is exact — no regex heuristics needed) and emits:
//
//   - one SCOPE.Component (subtype="mage_magefile") per magefile, carrying a
//     CONTAINS edge to each target it declares (target_extraction);
//   - one SCOPE.Operation (subtype="mage_target") per exported target
//     function, carrying a MAGE_DEPENDS_ON edge to each target named in a
//     mg.Deps / mg.SerialDeps / mg.CtxDeps call within its body
//     (dependency_graph).
//
// Detection mirrors the build_tools.yaml signal set: a *.go file with the
// `//go:build mage` (or legacy `// +build mage`) constraint, or any *.go file
// inside a magefiles/ directory. Failure is per-file and non-fatal.
package mage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/types"
)

// maxMagefileBytes caps the bytes read from any single magefile so a
// pathologically large generated file cannot stall the indexer.
const maxMagefileBytes = 1 << 20 // 1 MiB

// RelationshipKindMageDependsOn is the edge kind emitted between Mage targets
// for a declared mg.Deps / mg.SerialDeps / mg.CtxDeps dependency. It aliases
// the canonical types.RelationshipKindMageDependsOn.
const RelationshipKindMageDependsOn = string(types.RelationshipKindMageDependsOn)

// magefileBasenames are the conventional single-file magefile names.
var magefileBasenames = map[string]bool{
	"magefile.go": true,
	"Magefile.go": true,
}

// IsMagefile reports whether relPath is a Mage build file this extractor
// should attempt to parse. It is true for a conventional magefile.go basename
// and for any *.go file inside a magefiles/ directory. The build-tag check is
// applied later (during parse) because the directory/basename signal alone is
// the cheap pre-filter; the tag is the definitive confirmation for bare
// magefile.go files.
func IsMagefile(relPath string) bool {
	base := filepath.Base(relPath)
	if !strings.HasSuffix(base, ".go") || strings.HasSuffix(base, "_test.go") {
		return false
	}
	if magefileBasenames[base] {
		return true
	}
	// magefiles/ directory layout: any *.go directly under a path component
	// named "magefiles".
	for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(relPath)), "/") {
		if part == "magefiles" {
			return true
		}
	}
	return false
}

// Discover walks files, parses every magefile, and returns target entities
// plus their MAGE_DEPENDS_ON edges. repoRoot is the absolute repo path; files
// are repo-relative. Per-file parse failures are non-fatal.
func Discover(ctx context.Context, repoRoot string, files []string) ([]types.EntityRecord, []types.RelationshipRecord, error) {
	tracer := otel.Tracer("extractor.mage")
	ctx, span := tracer.Start(ctx, "mage.Discover")
	defer span.End()
	_ = ctx

	var entities []types.EntityRecord
	var rels []types.RelationshipRecord

	// target name → entity ID, populated in the first pass so the second
	// pass can resolve forward references (a target may depend on one
	// declared later, or in a sibling magefile).
	targetToID := map[string]string{}
	type parsedTarget struct {
		name string
		deps []string
	}
	var allTargets []parsedTarget
	var magefileCount int

	for _, rel := range files {
		if !IsMagefile(rel) {
			continue
		}
		abs := filepath.Join(repoRoot, rel)
		content, err := readBounded(abs)
		if err != nil {
			continue
		}
		targets, ok := ParseMagefile(content)
		if !ok {
			// Not actually a mage-tagged file (bare *.go that happened to be
			// named magefile.go without the build tag) — skip silently.
			continue
		}
		magefileCount++

		fileEnt := magefileEntity(rel, len(targets))

		for _, t := range targets {
			ent := targetEntity(rel, t)
			targetToID[t.Name] = ent.ID
			entities = append(entities, ent)
			allTargets = append(allTargets, parsedTarget{name: t.Name, deps: t.Deps})

			// CONTAINS: magefile → target.
			fileEnt.Relationships = append(fileEnt.Relationships, types.RelationshipRecord{
				FromID: fileEnt.ID,
				ToID:   ent.ID,
				Kind:   "CONTAINS",
			})
		}
		entities = append(entities, fileEnt)
	}

	// Second pass: dependency edges (forward references now resolvable).
	for _, t := range allTargets {
		fromID := targetToID[t.name]
		if fromID == "" {
			continue
		}
		seen := map[string]bool{}
		for _, dep := range t.deps {
			if dep == "" || dep == t.name || seen[dep] {
				continue
			}
			seen[dep] = true
			toID, ok := targetToID[dep]
			if !ok {
				// Dependency on a function not recognised as a target (e.g.
				// an unexported helper or one in an unparsed file). Emit a
				// synthetic ID so the edge is still recorded.
				toID = entityID("mage_target_ext", dep)
			}
			rels = append(rels, types.RelationshipRecord{
				FromID: fromID,
				ToID:   toID,
				Kind:   RelationshipKindMageDependsOn,
				Properties: map[string]string{
					"dep_target":    dep,
					"source_target": t.name,
				},
			})
		}
	}

	sort.Slice(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })
	sort.Slice(rels, func(i, j int) bool {
		if rels[i].FromID != rels[j].FromID {
			return rels[i].FromID < rels[j].FromID
		}
		return rels[i].ToID < rels[j].ToID
	})

	span.SetAttributes(
		attribute.Int("mage_magefiles", magefileCount),
		attribute.Int("mage_entities", len(entities)),
		attribute.Int("mage_edges", len(rels)),
	)
	return entities, rels, nil
}

// Target is a parsed Mage build target.
type Target struct {
	Name      string
	Deps      []string // names referenced via mg.Deps/SerialDeps/CtxDeps
	StartLine int
}

// ParseMagefile parses content as Go source and, if it carries the `mage`
// build constraint, returns the exported runnable targets it declares. The
// second return is false when the file does not carry the mage build tag (so
// the caller can skip non-mage *.go files that merely match the basename).
func ParseMagefile(content []byte) ([]Target, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "magefile.go", content, parser.ParseComments)
	if err != nil || f == nil {
		return nil, false
	}
	if !hasMageBuildTag(f) {
		return nil, false
	}

	var targets []Target
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue // skip methods — Mage namespaces use a different model
		}
		if !isExported(fn.Name.Name) {
			continue
		}
		if !isTargetSignature(fn) {
			continue
		}
		t := Target{
			Name:      fn.Name.Name,
			Deps:      extractDeps(fn),
			StartLine: fset.Position(fn.Pos()).Line,
		}
		targets = append(targets, t)
	}
	return targets, true
}

// hasMageBuildTag reports whether the file carries `//go:build mage` or the
// legacy `// +build mage` constraint anywhere in its leading comment groups.
func hasMageBuildTag(f *ast.File) bool {
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			text := c.Text
			if strings.HasPrefix(text, "//go:build") && containsTagWord(text, "mage") {
				return true
			}
			if strings.HasPrefix(text, "// +build") && containsTagWord(text, "mage") {
				return true
			}
		}
	}
	return false
}

// containsTagWord reports whether the build-constraint line references the
// given tag word as a whole token (so "magento" wouldn't match "mage").
func containsTagWord(line, tag string) bool {
	for _, tok := range strings.FieldsFunc(line, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ',' || r == '(' || r == ')' || r == '!' || r == '|' || r == '&'
	}) {
		if tok == tag {
			return true
		}
	}
	return false
}

// isTargetSignature reports whether fn has one of Mage's accepted target
// signatures: no params or a single context.Context param, and either no
// results or a single error result.
func isTargetSignature(fn *ast.FuncDecl) bool {
	t := fn.Type
	// Results: zero, or exactly one `error`.
	if t.Results != nil && t.Results.NumFields() > 0 {
		if t.Results.NumFields() != 1 {
			return false
		}
		if id, ok := t.Results.List[0].Type.(*ast.Ident); !ok || id.Name != "error" {
			return false
		}
	}
	// Params: zero, or a single context.Context.
	if t.Params != nil {
		n := t.Params.NumFields()
		if n > 1 {
			return false
		}
		if n == 1 {
			// Accept only ctx context.Context (a SelectorExpr ".Context").
			fld := t.Params.List[0]
			if !isContextParam(fld.Type) {
				return false
			}
		}
	}
	return true
}

// isContextParam reports whether expr names context.Context.
func isContextParam(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == "Context"
}

// extractDeps walks fn's body for mg.Deps / mg.SerialDeps / mg.CtxDeps calls
// and returns the referenced target names (the leading ctx arg of CtxDeps is
// dropped). Names are returned in source order; the caller de-duplicates.
func extractDeps(fn *ast.FuncDecl) []string {
	if fn.Body == nil {
		return nil
	}
	var deps []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok || pkgIdent.Name != "mg" {
			return true
		}
		method := sel.Sel.Name
		isCtx := method == "CtxDeps"
		if method != "Deps" && method != "SerialDeps" && !isCtx {
			return true
		}
		for i, arg := range call.Args {
			if isCtx && i == 0 {
				continue // first arg of CtxDeps is the context
			}
			if name := depName(arg); name != "" {
				deps = append(deps, name)
			}
		}
		return true
	})
	return deps
}

// depName extracts a target name from a dependency argument. Mage accepts a
// bare function value (Build), a qualified one (mytargets.Build, or a
// namespace method Foo.Bar), or mg.F(Build, args...). We resolve the simple
// and selector forms; mg.F wrappers are unwrapped to their first arg.
func depName(arg ast.Expr) string {
	switch a := arg.(type) {
	case *ast.Ident:
		return a.Name
	case *ast.SelectorExpr:
		// Namespace.Method or pkg.Func — use the trailing identifier as the
		// target name (Mage addresses namespaced targets by method name).
		return a.Sel.Name
	case *ast.CallExpr:
		// mg.F(Target, args...) — unwrap to the first argument.
		if sel, ok := a.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "mg" && sel.Sel.Name == "F" {
				if len(a.Args) > 0 {
					return depName(a.Args[0])
				}
			}
		}
	}
	return ""
}

// isExported reports whether name starts with an uppercase letter.
func isExported(name string) bool {
	if name == "" {
		return false
	}
	r := rune(name[0])
	return r >= 'A' && r <= 'Z'
}

// magefileEntity returns a SCOPE.Component entity for a magefile.
func magefileEntity(rel string, targetCount int) types.EntityRecord {
	return types.EntityRecord{
		ID:         entityID("mage_magefile", rel),
		Name:       rel,
		Kind:       string(types.EntityKindComponent),
		Subtype:    "mage_magefile",
		SourceFile: rel,
		Language:   "go",
		Properties: map[string]string{
			"build_system": "mage",
			"target_count": fmt.Sprintf("%d", targetCount),
		},
		QualityScore:     1.0,
		EnrichmentStatus: types.StatusPending,
	}
}

// targetEntity returns a SCOPE.Operation entity for a Mage target.
func targetEntity(rel string, t Target) types.EntityRecord {
	sig := "func " + t.Name + "()"
	deps := ""
	if len(t.Deps) > 0 {
		deps = strings.Join(t.Deps, ",")
	}
	props := map[string]string{
		"build_system": "mage",
		"target_name":  t.Name,
	}
	if deps != "" {
		props["dependencies"] = deps
	}
	return types.EntityRecord{
		ID:               entityID("mage_target", rel+"\x00"+t.Name),
		Name:             t.Name,
		Kind:             string(types.EntityKindOperation),
		Subtype:          "mage_target",
		SourceFile:       rel,
		StartLine:        t.StartLine,
		EndLine:          t.StartLine,
		Language:         "go",
		Signature:        sig,
		Properties:       props,
		QualityScore:     1.0,
		EnrichmentStatus: types.StatusPending,
	}
}

// entityID returns a deterministic 16-char hex ID from a namespace + key.
func entityID(ns, key string) string {
	h := sha256.New()
	h.Write([]byte("mage\x00" + ns + "\x00" + key))
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// readBounded reads at most maxMagefileBytes from path.
func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, maxMagefileBytes)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}
