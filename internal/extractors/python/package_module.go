// package_module.go — per-package Module entity emission for the Python extractor.
//
// # Problem (issue #1884 — Wave 1)
//
// Without this pass, the Python extractor emits only ONE Module entity per
// repo (the repo-root synthetic one from the module aggregation layer). Sub-package
// structure is entirely invisible to docgen:
//
//   - Per-package docgen pages cannot be seeded (no entity to seed from).
//   - Reference-dependency enumeration cannot name internal-package edges.
//   - Flow narratives cannot say "request enters via the core.views module".
//   - Louvain clusters are the only mid-level grouping — algorithmic, not semantic.
//
// # Solution
//
// For every Python source file whose base name is `__init__.py`, emit one
// Module entity representing the package boundary. Additionally, for every
// plain `.py` file that has siblings in its directory (i.e. lives in a
// directory that already has an `__init__.py` OR has peer `.py` files),
// emit a Module entity keyed on the directory's dotted name.
//
// This pass runs as a supplemental step at the end of Extract (after the
// primary walk, config-module pass, etc.). It never removes or modifies
// existing entities.
//
// # Emitted entity shape
//
//	Kind:          "Module"          (matches internal/module.KindModule for kind_filter=Module)
//	Subtype:       "package"
//	Name:          dotted package name (e.g. "core.views")
//	QualifiedName: "<repo-relative-prefix>.<dottedName>" — derived from file path
//	SourceFile:    path to __init__.py (or the .py file for lone-file modules)
//	StartLine:     1
//	EndLine:       actual line count from file content
//	Properties:
//	  is_package:     "true"  (for __init__.py anchors) / "false" (file module)
//	  parent_package: dotted path of the parent package ("" for repo root packages)
//
// # CONTAINS edges
//
// For __init__.py files the emitted Module entity carries CONTAINS stub edges
// pointing at all top-level children in the same entities slice (classes and
// functions that share the same SourceFile). Parent→child CONTAINS edges
// (parent Module → this Module) are NOT emitted here because the parent
// Module entity is emitted when the parent package's __init__.py is
// processed. The resolver stitches the hierarchy via the `parent_package`
// property.
//
// # Ignore rules
//
// The pass skips paths that the walker already ignores:
//   - site-packages / vendor / node_modules directories
//   - .archigraph/store paths
//   - Django migrations packages (isDjangoMigrationFile covers their __init__.py)
//
// Tests/ packages ARE emitted (they are real navigable package boundaries).
package python

import (
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// packageIgnoreDirs is the set of directory-name segments that should never
// receive a Module entity. Using a set (map) for O(1) lookup.
var packageIgnoreDirs = map[string]bool{
	"site-packages": true,
	"vendor":        true,
	"node_modules":  true,
	".archigraph":   true,
	// Python virtual environment directories — common names:
	".venv":  true,
	"venv":   true,
	"env":    true,
	".env":   true,
	"__pypackages__": true,
}

// isIgnoredPackagePath returns true when any path segment of filePath matches
// a directory in packageIgnoreDirs. This mirrors the walker's ignore logic
// without depending on its internals.
func isIgnoredPackagePath(filePath string) bool {
	// Use forward slashes for consistent splitting across platforms.
	parts := strings.Split(filepath.ToSlash(filePath), "/")
	for _, seg := range parts {
		if packageIgnoreDirs[seg] {
			return true
		}
	}
	return false
}

// isInitPy reports whether the given repo-relative file path is an __init__.py
// file (package anchor).
func isInitPy(filePath string) bool {
	return filepath.Base(filepath.FromSlash(filePath)) == "__init__.py"
}

// packageDottedName derives the dotted Python package name from the path of
// an __init__.py file. For a plain .py file it derives the module name (same
// as filePathToModule but without the __init__ rollup so callers can
// distinguish the two cases).
//
// Examples:
//
//	"core/__init__.py"           → "core"
//	"core/views/__init__.py"     → "core.views"
//	"src/app/models/__init__.py" → "app.models"   (src/ stripped)
//	"manage.py"                  → "manage"        (lone module file)
func packageDottedName(filePath string) string {
	// For __init__.py: strip the filename and use the directory.
	if isInitPy(filePath) {
		dir := filepath.Dir(filepath.FromSlash(filePath))
		if dir == "." {
			// __init__.py at repo root — unusual but valid.
			return ""
		}
		// Convert to forward slashes and strip known source-root prefixes.
		s := filepath.ToSlash(dir)
		for _, prefix := range []string{"src/", "lib/", "app/"} {
			if strings.HasPrefix(s, prefix) {
				s = strings.TrimPrefix(s, prefix)
				break
			}
		}
		return strings.ReplaceAll(s, "/", ".")
	}
	// For plain .py files: use filePathToModule (handles src/ prefix strip and
	// converts separators to dots).
	return filePathToModule(filePath)
}

// parentPackageName returns the dotted name of the parent package of the
// given dotted package name. Returns "" when the package is at the top level
// (i.e. has no dot separator — it is a direct child of the repo root).
func parentPackageName(dottedName string) string {
	if dot := strings.LastIndexByte(dottedName, '.'); dot > 0 {
		return dottedName[:dot]
	}
	return ""
}

// countFileLines returns the number of lines in src by counting newline
// characters. Returns 1 when src is empty (a valid empty file is still 1
// line). This matches the EndLine semantics used by buildClass/buildFunction
// (tree-sitter EndPoint().Row is 0-based; line count is node.EndPoint().Row+1).
func countFileLines(src []byte) int {
	if len(src) == 0 {
		return 1
	}
	n := 1
	for _, b := range src {
		if b == '\n' {
			n++
		}
	}
	return n
}

// emitPackageModuleEntity is the supplemental pass that emits a Module entity
// for Python package boundaries (issue #1884). It is called from Extract for
// every Python source file, but only emits an entity when the file is an
// __init__.py OR a plain .py file (treated as a single-file module).
//
// Parameters:
//
//	file — the source file input (Path + Content consulted).
//	out  — the entity accumulator; file entity is at index 0. Appended in-place.
//
// Returns the number of Module entities appended (0 or 1).
//
// For __init__.py files:
//   - Derives the package dotted name from the directory path.
//   - Emits a Module entity with is_package="true" and parent_package set.
//   - Appends CONTAINS edges from this Module to every top-level class and
//     function entity in *out that shares the same SourceFile (i.e. was emitted
//     by the main walk for this __init__.py). This covers the "children in the
//     same package" requirement for classes/functions defined in __init__.py.
//
// For plain .py files:
//   - Derives the module dotted name from filePathToModule.
//   - Emits a Module entity with is_package="false".
//   - No children CONTAINS edges — the file entity already CONTAINS the top-level
//     declarations (added by the main Extract loop over walkBeforeCount…len(entities)).
//
// Parent→child Module edges (parent Module → this Module) are the parent's
// responsibility: the resolver stitches the hierarchy after all files are
// processed using the parent_package property.
func emitPackageModuleEntity(file extractor.FileInput, out *[]types.EntityRecord) int {
	if isIgnoredPackagePath(file.Path) {
		return 0
	}
	// Skip Django migration __init__.py files (isDjangoMigrationFile covers them).
	if isDjangoMigrationFile(file.Path) {
		return 0
	}

	isInit := isInitPy(file.Path)

	dottedName := packageDottedName(file.Path)
	if dottedName == "" {
		// Repo-root __init__.py — unusual; skip (repo-root Module is synthetic).
		return 0
	}

	parent := parentPackageName(dottedName)
	isPackage := isInit

	endLine := countFileLines(file.Content)

	props := map[string]string{
		"is_package":     boolProp(isPackage),
		"parent_package": parent,
	}

	rec := types.EntityRecord{
		Name:          dottedName,
		QualifiedName: dottedName, // qualified by the dotted path itself; repo tag added at graph-build time
		Kind:          string(types.EntityKindModule),
		Subtype:       "package",
		Language:      "python",
		SourceFile:    file.Path,
		StartLine:     1,
		EndLine:       endLine,
		Signature:     "package " + dottedName,
		Properties:    props,
	}

	// For __init__.py files: wire CONTAINS edges from this Module to every
	// top-level class and function entity that was emitted from this same file
	// by the main walkNode pass. We look for entities in *out whose SourceFile
	// matches and whose Kind is SCOPE.Operation or SCOPE.Component/class.
	// The file entity itself (index 0) is excluded — file→declaration CONTAINS
	// is already emitted by the main Extract loop.
	if isInit {
		for i := range *out {
			child := &(*out)[i]
			if child.SourceFile != file.Path {
				continue
			}
			var toID string
			switch {
			case child.Kind == "SCOPE.Component" && child.Subtype == "class" &&
				!strings.ContainsRune(child.Name, '.'):
				// Top-level class.
				toID = "scope:component:class:python:" + filepath.ToSlash(file.Path) + ":" + child.Name
			case child.Kind == "SCOPE.Operation" && child.Subtype == "function" &&
				!strings.ContainsRune(child.Name, '.'):
				// Module-level function (not a method — no dot in name).
				toID = extractor.BuildOperationStructuralRef("python", file.Path, child.Name)
			}
			if toID != "" {
				rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
					ToID: toID,
					Kind: string(types.RelationshipKindContains),
				})
			}
		}
	}

	*out = append(*out, rec)
	return 1
}

// boolProp converts a bool to the canonical "true" / "false" property string.
func boolProp(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
