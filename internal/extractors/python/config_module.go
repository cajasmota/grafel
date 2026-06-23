// config_module.go — supplemental pass for Python configuration-module entities.
//
// Many Python projects carry files that have strong architectural signal but
// zero extractable entities from the normal walkNode pass because they consist
// entirely (or almost entirely) of module-level assignments:
//
//	settings.py   — Django / DRF / Flask application settings
//	urls.py       — Django URL dispatcher
//	routes.py     — Flask/FastAPI route registrations defined imperatively
//	wsgi.py       — WSGI application entry point
//	asgi.py       — ASGI application entry point
//	conftest.py   — pytest shared fixtures
//	setup.py      — legacy setuptools project manifest
//	manage.py     — Django management script (typically has a main())
//	celery.py     — Celery application and task-routing config
//
// Without these entities, bench Q1 ("Where is the Django settings class for
// upvate-core?") returns WRONG because upvate_core/settings.py contains only
// module-level assignments (no class, no def at all) and the extractor
// previously emitted nothing but the file entity for it.
//
// Issue #1775 — add a SUPPLEMENTAL fallback pass that runs AFTER the base
// walkNode walk. If the file satisfies either:
//
//  1. Filename match — basename is in the canonical config-filename set
//     (case-sensitive; Python convention).
//  2. Content heuristic — ≥80% of top-level non-blank non-comment statements
//     are assignment / augmented-assignment nodes.
//
// …and the walk produced zero semantic entities (excluding the file entity
// and import records), emit exactly one SCOPE.Config entity with
// subtype="config_module" and a set of Properties that describe the config
// type, the count of top-level assignments, and the collected symbol names.
//
// If the file already produced semantic entities (classes, functions, …) the
// config_module entity is STILL emitted (supplemental, not replacing).  Only
// pure-config files usually hit criterion 2; named-file files always hit
// criterion 1 regardless of their content (e.g. a manage.py with a main()).
//
// The entity is wired into extractor.go by calling emitConfigModuleEntity
// just before TagRelationshipsLanguage.

package python

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// configFilenames is the canonical set of Python module basenames that always
// deserve a config_module entity regardless of their content. Case-sensitive
// per Python import convention.
var configFilenames = map[string]string{
	// basename → config_type
	"settings.py": "django_settings",
	"urls.py":     "django_urls",
	"routes.py":   "generic_routes",
	"wsgi.py":     "wsgi",
	"asgi.py":     "asgi",
	"conftest.py": "pytest_conftest",
	"setup.py":    "setuptools",
	"manage.py":   "django_manage",
	"celery.py":   "celery",
}

// configAssignmentRatio is the fraction of top-level non-blank non-comment
// statements that must be assignments for the content heuristic to fire.
const configAssignmentRatio = 0.80

// emitConfigModuleEntity is the supplemental pass wired into Extract.
//
// It inspects the file and, when either the filename-match or content
// heuristic fires, appends a single SCOPE.Config/config_module entity to
// *out. The pass is unconditional — it runs even when walkNode already
// produced semantic entities (the entity is supplemental, not replacing).
//
// Parameters:
//
//	root — tree-sitter parse tree root (used for the content heuristic).
//	file — source file input (Path + Content are consulted).
//	out  — the entity accumulator; the file entity is always at index 0.
//	       emitConfigModuleEntity appends to it in-place.
//
// Returns true when a config_module entity was appended, false otherwise.
// Callers may use the boolean to increment an OTel attribute counter.
func emitConfigModuleEntity(root ts.Node, file extractor.FileInput, out *[]types.EntityRecord) bool {
	base := filepath.Base(filepath.FromSlash(file.Path))

	// --- criterion 1: filename match ---
	configType, byName := configFilenames[base]

	// --- criterion 2: content heuristic ---
	var byContent bool
	assignCount, totalCount := countTopLevelStatements(root, file.Content)
	if !byName && totalCount > 0 {
		ratio := float64(assignCount) / float64(totalCount)
		if ratio >= configAssignmentRatio {
			byContent = true
			configType = "generic_config"
		}
	}

	if !byName && !byContent {
		return false
	}

	// Collect top-level symbol names for the properties bag.
	symbolNames := collectTopLevelAssignmentNames(root, file.Content)

	// Derive the short name (strip .py suffix from base).
	shortName := strings.TrimSuffix(base, ".py")

	// Qualified name: <module>.<shortName>
	mod := filePathToModule(file.Path)
	qualName := shortName
	if mod != "" {
		qualName = mod + "." + shortName
	}

	props := map[string]string{
		"config_type":      configType,
		"assignment_count": fmt.Sprintf("%d", assignCount),
	}
	if len(symbolNames) > 0 {
		props["top_level_symbols"] = strings.Join(symbolNames, ",")
	}

	// Issue #2816 — capture the DRF global default permission policy so the
	// auth_coverage detector knows whether endpoints with no explicit perms
	// are protected by default. We stamp two properties:
	//
	//	drf_default_permission_classes — comma-joined leaf names from
	//	    REST_FRAMEWORK["DEFAULT_PERMISSION_CLASSES"] (empty when the key is
	//	    absent — DRF's built-in default is then AllowAny, i.e. open).
	//	drf_default_permission_present — "true" when the key is present at all
	//	    (so "" can be distinguished from "explicit empty tuple").
	if configType == "django_settings" || configType == "generic_config" {
		if perms, present := extractDRFDefaultPermissionClasses(root, file.Content); present {
			props["drf_default_permission_present"] = "true"
			props["drf_default_permission_classes"] = strings.Join(perms, ",")
		}
	}

	// Issue #1964 — emit the real file end line so the docgen
	// source_window helper can excerpt the entire settings/manage/celery
	// module body instead of clipping at line 1. The config_module entity
	// represents the whole file; its boundary is the file boundary.
	endLine := int(root.EndPoint().Row) + 1
	if endLine < 1 {
		endLine = 1
	}
	rec := types.EntityRecord{
		Name:          shortName,
		QualifiedName: qualName,
		Kind:          string(types.EntityKindConfig),
		Subtype:       "config_module",
		Language:      "python",
		SourceFile:    file.Path,
		StartLine:     1,
		EndLine:       endLine,
		Signature:     "# " + base,
		Properties:    props,
	}

	// Wire a CONTAINS edge from the file entity so the config_module is
	// reachable from the file in graph traversals.
	if len(*out) > 0 {
		(*out)[0].Relationships = append((*out)[0].Relationships, types.RelationshipRecord{
			ToID: "scope:config:config_module:python:" + file.Path + ":" + shortName,
			Kind: string(types.RelationshipKindContains),
		})
	}

	*out = append(*out, rec)
	return true
}

// countTopLevelStatements walks the immediate children of the module root node
// and returns (assignCount, totalCount) where:
//   - totalCount is the number of non-blank, non-comment top-level statements.
//   - assignCount is the subset of those that are expression_statement nodes
//     wrapping an assignment or augmented_assignment — i.e. module-level
//     variable / constant declarations.
//
// Only the direct children of the module root are examined (file-level
// scope). Nested statements inside functions, classes, or if-blocks are
// intentionally NOT counted — we want the file-level heuristic only.
func countTopLevelStatements(root ts.Node, src []byte) (assignCount, totalCount int) {
	if root == nil {
		return 0, 0
	}
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "comment", "newline", "":
			// skip blank lines and comments
			continue
		}
		totalCount++
		if child.Type() == "expression_statement" {
			for j := 0; j < int(child.NamedChildCount()); j++ {
				expr := child.NamedChild(j)
				if expr == nil {
					continue
				}
				if expr.Type() == "assignment" || expr.Type() == "augmented_assignment" {
					assignCount++
					break
				}
			}
		}
	}
	return assignCount, totalCount
}

// collectTopLevelAssignmentNames returns the left-hand-side identifier names
// of every simple module-level assignment (identifier = expr). At most 50
// names are returned to keep the Property value bounded. Dunder names are
// excluded.
func collectTopLevelAssignmentNames(root ts.Node, src []byte) []string {
	if root == nil {
		return nil
	}
	const maxSymbols = 50
	seen := make(map[string]bool)
	var names []string
	for i := 0; i < int(root.ChildCount()); i++ {
		if len(names) >= maxSymbols {
			break
		}
		child := root.Child(i)
		if child == nil || child.Type() != "expression_statement" {
			continue
		}
		for j := 0; j < int(child.NamedChildCount()); j++ {
			expr := child.NamedChild(j)
			if expr == nil {
				continue
			}
			var lhs ts.Node
			switch expr.Type() {
			case "assignment":
				lhs = expr.ChildByFieldName("left")
			case "augmented_assignment":
				lhs = expr.ChildByFieldName("left")
			default:
				continue
			}
			if lhs == nil || lhs.Type() != "identifier" {
				continue
			}
			name := nodeText(lhs, src)
			if name == "" || seen[name] {
				continue
			}
			// skip dunder names — they are implementation internals
			if strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__") {
				continue
			}
			seen[name] = true
			names = append(names, name)
			if len(names) >= maxSymbols {
				break
			}
		}
	}
	return names
}

// extractDRFDefaultPermissionClasses scans a Django settings module for
//
//	REST_FRAMEWORK = {
//	    "DEFAULT_PERMISSION_CLASSES": ("rest_framework.permissions.IsAuthenticated", ...),
//	}
//
// and returns (leafPermissionNames, present). `present` is true when the
// DEFAULT_PERMISSION_CLASSES key exists at all — a present-but-empty value
// means the project explicitly opted into an open default. When the key is
// absent the caller should treat the global default as AllowAny (DRF's
// built-in), i.e. NOT auth coverage.
//
// Values may be string literals ("rest_framework.permissions.IsAuthenticated")
// or bare identifiers (IsAuthenticated); both are reduced to their leaf class
// name (IsAuthenticated). Only the top-level REST_FRAMEWORK assignment is
// examined.
func extractDRFDefaultPermissionClasses(root ts.Node, src []byte) ([]string, bool) {
	if root == nil {
		return nil, false
	}
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child == nil || child.Type() != "expression_statement" {
			continue
		}
		for j := 0; j < int(child.NamedChildCount()); j++ {
			expr := child.NamedChild(j)
			if expr == nil || expr.Type() != "assignment" {
				continue
			}
			lhs := expr.ChildByFieldName("left")
			if lhs == nil || lhs.Type() != "identifier" || nodeText(lhs, src) != "REST_FRAMEWORK" {
				continue
			}
			rhs := expr.ChildByFieldName("right")
			if rhs == nil || rhs.Type() != "dictionary" {
				continue
			}
			return dictDefaultPermissionClasses(rhs, src)
		}
	}
	return nil, false
}

// dictDefaultPermissionClasses reads a `dictionary` node and, if it has a
// "DEFAULT_PERMISSION_CLASSES" key, returns the leaf names of its value list
// plus present=true.
func dictDefaultPermissionClasses(dict ts.Node, src []byte) ([]string, bool) {
	for i := 0; i < int(dict.NamedChildCount()); i++ {
		pair := dict.NamedChild(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		keyNode := pair.ChildByFieldName("key")
		valNode := pair.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			continue
		}
		if stripQuotes(strings.TrimSpace(nodeText(keyNode, src))) != "DEFAULT_PERMISSION_CLASSES" {
			continue
		}
		raw := parseListLiteralStringsOrIdents(valNode, src)
		out := make([]string, 0, len(raw))
		for _, r := range raw {
			if leaf := permissionLeafName(stripQuotes(r)); leaf != "" {
				out = append(out, leaf)
			}
		}
		return out, true
	}
	return nil, false
}

// parseListLiteralStringsOrIdents reads a list/tuple/set literal and returns
// the raw source text of each element (string literal or identifier/attribute),
// tolerating the mixed string-or-symbol forms common in DRF settings.
func parseListLiteralStringsOrIdents(n ts.Node, src []byte) []string {
	if n == nil {
		return nil
	}
	switch n.Type() {
	case "list", "tuple", "set":
		var out []string
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			if ch == nil {
				continue
			}
			switch ch.Type() {
			case "string", "identifier", "attribute", "dotted_name":
				out = append(out, strings.TrimSpace(nodeText(ch, src)))
			}
		}
		return out
	case "string", "identifier", "attribute", "dotted_name":
		return []string{strings.TrimSpace(nodeText(n, src))}
	}
	return nil
}
