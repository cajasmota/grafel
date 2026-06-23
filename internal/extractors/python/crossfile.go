// crossfile.go — cross-file class-hierarchy resolution for the Python extractor.
//
// Issue #698: Python cross-file class-hierarchy resolution (extractor-level).
//
// The base extractor emits SCOPE.Component entities for declared classes but
// has never emitted EXTENDS edges — leaving class inheritance invisible to the
// graph. This file adds:
//
//  1. EXTENDS edge emission in extractBaseClasses: for every `class Foo(Bar):`
//     declaration, emit one EXTENDS edge per base class with a
//     scope:component:class:python:<file>:<BaseName> stub. The resolver's
//     lookupUniqueRealComponentByName path (refs.go line 2491) binds
//     same-package and project-global stubs to real class entities. The
//     external synthesiser (internal/external/synth.go) synthesises ext:
//     placeholders for module-qualified bases like "serializers.ModelSerializer".
//
//  2. PythonClassRegistry — a global pre-pass store mapping class simple-name
//     → ordered list of source files where the class is declared. Populated by
//     ScanPythonClassRegistry before per-file extraction runs (Option B, same
//     pattern as #845 JavaDIRegistry).
//
//  3. resolveBaseFile — given a base name seen in `class Foo(Base):`, checks the
//     global registry to see if the name maps to exactly one file in the same
//     project. When unambiguous, the EXTENDS stub uses THAT file's path instead
//     of the consumer's path, allowing the resolver to bind the edge without any
//     additional lookup.
//
// Lifecycle:
//   - ScanPythonClassRegistry(content) populates the registry from raw file text.
//   - extractBaseClasses(node, file, registry) returns EXTENDS edges for a class node.
//   - ClearPythonClassRegistry() resets the registry (test isolation / new run).
//   - The Indexer calls Clear then Scan for every .py file before pass 1 runs.
package python

import (
	"strings"
	"sync"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// PythonClassRegistry — global pre-pass registry (Option B)
// ---------------------------------------------------------------------------

// pythonClassRegistryEntry holds the set of files that declare a given class.
type pythonClassRegistryEntry struct {
	files []string // sorted in scan order; first-declaration-wins for single-file resolution
}

// PythonClassRegistry maps class simple-name → set of source files declaring it.
// Exported so cmd/grafel/index.go and the daemon subprocess path can populate it.
type PythonClassRegistry map[string]*pythonClassRegistryEntry

var (
	pyClassGlobal   PythonClassRegistry = PythonClassRegistry{}
	pyClassGlobalMu sync.RWMutex
)

// ScanPythonClassRegistry extracts all top-level class declarations from `filePath`
// and `content` (raw Python source text) and merges them into the global registry.
// Safe for concurrent calls from parallel file walkers.
//
// The scan is a lightweight line-based scan (no AST) for speed. We look for lines
// matching `class <Name>` at column 0 (no indentation), which reliably identifies
// top-level class declarations. Indented classes (nested) are intentionally skipped
// because nested class names are qualified at the extractor level and not looked up
// by bare simple-name in the cross-file pass.
func ScanPythonClassRegistry(filePath, content string) {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "class ") {
			continue
		}
		// Extract the name: "class Foo:" or "class Foo(Bar):" or "class Foo :"
		rest := line[len("class "):]
		end := strings.IndexAny(rest, "(: \t\r")
		if end <= 0 {
			continue
		}
		name := rest[:end]
		if name == "" {
			continue
		}
		pyClassGlobalMu.Lock()
		entry := pyClassGlobal[name]
		if entry == nil {
			entry = &pythonClassRegistryEntry{}
			pyClassGlobal[name] = entry
		}
		// Avoid duplicate file entries.
		found := false
		for _, f := range entry.files {
			if f == filePath {
				found = true
				break
			}
		}
		if !found {
			entry.files = append(entry.files, filePath)
		}
		pyClassGlobalMu.Unlock()
	}
}

// ClearPythonClassRegistry resets the global Python class registry.
// Call at the start of each index run and in test teardowns.
func ClearPythonClassRegistry() {
	pyClassGlobalMu.Lock()
	defer pyClassGlobalMu.Unlock()
	pyClassGlobal = PythonClassRegistry{}
}

// resolveBaseFile returns the single source file path where `baseName` is
// declared when the registry maps it to exactly one file AND that file is
// different from `consumerFile`. Returns "" when the registry has 0 or 2+
// declarations (ambiguous / unknown) or when the only declaration is in the
// consumer's own file (same-file base, the stub's consumer path is fine).
func resolveBaseFile(baseName, consumerFile string) string {
	pyClassGlobalMu.RLock()
	defer pyClassGlobalMu.RUnlock()
	entry, ok := pyClassGlobal[baseName]
	if !ok || entry == nil {
		return ""
	}
	// Filter out the consumer file itself.
	var others []string
	for _, f := range entry.files {
		if f != consumerFile {
			others = append(others, f)
		}
	}
	if len(others) == 1 {
		return others[0]
	}
	return "" // 0 = same-file or unknown; 2+ = ambiguous
}

// ---------------------------------------------------------------------------
// EXTENDS edge extraction
// ---------------------------------------------------------------------------

// extractBaseClasses returns one EXTENDS RelationshipRecord per base class
// listed in `class Foo(Base1, Base2, ...)`. The function is called once per
// class_definition node AFTER the class entity has been appended to *out, so
// the caller (walkNode) can attach the returned edges to the class entity.
//
// The ToID follows the scope:component:class:python:<file>:<Name> Format A
// structural-ref shape the resolver's lookupStructural path already consumes:
//
//   - For a base whose bare name is found UNAMBIGUOUSLY in the global registry
//     in a different file, the stub uses THAT file's path. The resolver can
//     then bind the stub directly via byLocation[file][Name] without needing
//     the global byName fallback.
//
//   - For a base whose name is NOT in the registry (external or stdlib) or is
//     ambiguous, the stub uses the CONSUMER's file path. The resolver's
//     lookupUniqueRealComponentByName fallback at refs.go line 2491 handles
//     the in-project ambiguous case; the external synthesiser handles the
//     module-qualified shape (e.g. "serializers.ModelSerializer").
//
// Module-qualified bases (e.g. `models.Model`, `serializers.ModelSerializer`)
// carry the dotted form as the trailing segment so the external synthesiser's
// dotted-tail detection fires and generates an `ext:<module>` placeholder.
//
// Bases that are the enclosing class itself (erroneous code) are dropped.
//
// Returns nil when the class has no base-class list or the list is empty.
func extractBaseClasses(
	classNode ts.Node,
	filePath string,
	className string,
	src []byte,
) []types.RelationshipRecord {
	if classNode == nil {
		return nil
	}
	// tree-sitter Python grammar: class_definition children include an
	// optional "argument_list" named child containing the base-class list.
	argList := classNode.ChildByFieldName("superclasses")
	if argList == nil {
		// Fallback: iterate named children looking for argument_list type.
		// Some grammar versions name the field differently.
		for i := 0; i < int(classNode.ChildCount()); i++ {
			ch := classNode.Child(i)
			if ch != nil && ch.Type() == "argument_list" {
				argList = ch
				break
			}
		}
	}
	if argList == nil {
		return nil
	}

	var rels []types.RelationshipRecord
	seen := make(map[string]bool)

	for i := 0; i < int(argList.ChildCount()); i++ {
		child := argList.Child(i)
		if child == nil {
			continue
		}
		baseName := extractBaseNameFromNode(child, src)
		if baseName == "" {
			continue
		}
		// Drop self-references (rare erroneous code).
		if baseName == className {
			continue
		}
		if seen[baseName] {
			continue
		}
		seen[baseName] = true

		// Determine the stub file path.
		// For module-qualified bases (e.g. "models.Model"), the leaf is the
		// trailing component. We look up only the bare part after the last ".".
		bareLeaf := baseName
		if dot := strings.LastIndexByte(baseName, '.'); dot >= 0 {
			bareLeaf = baseName[dot+1:]
		}

		targetFile := filePath // default: consumer's file path
		if bareLeaf == baseName {
			// Simple name (no module prefix) — try the cross-file registry.
			if resolved := resolveBaseFile(baseName, filePath); resolved != "" {
				targetFile = resolved
			}
		}

		// Emit EXTENDS edge with structural-ref ToID.
		// scope:component:class:python:<targetFile>:<baseName>
		toID := "scope:component:class:python:" + targetFile + ":" + baseName
		rels = append(rels, types.RelationshipRecord{
			ToID: toID,
			Kind: "EXTENDS",
		})
	}
	return rels
}

// extractBaseNameFromNode returns the base-class name string from a single
// argument_list child node. Recognised shapes:
//
//	identifier           → "Foo"
//	attribute            → "module.Foo"  (e.g. models.Model)
//	subscript            → "List[str]" → "List"  (generic base, return inner name)
//	keyword_argument     → skip (e.g. metaclass=ABCMeta)
func extractBaseNameFromNode(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "identifier":
		return nodeText(n, src)
	case "attribute":
		// e.g. models.Model — return "models.Model"
		return nodeText(n, src)
	case "subscript":
		// e.g. Generic[T], List[str] — return outer name only (the value field)
		val := n.ChildByFieldName("value")
		if val != nil {
			return extractBaseNameFromNode(val, src)
		}
		return ""
	case "keyword_argument":
		// metaclass=ABCMeta — skip
		return ""
	case ",", "(", ")", "comment":
		return ""
	default:
		// For any other named node, try to get text and use as identifier if
		// it looks like one — e.g. dotted_name in some grammar versions.
		t := strings.TrimSpace(nodeText(n, src))
		if t == "" || strings.ContainsAny(t, " \t\n=()[]{}<>") {
			return ""
		}
		return t
	}
}
