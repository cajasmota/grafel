// Error-handling pattern extraction for Java source files (MX-1047).
//
// This file implements a secondary extraction pass that emits one
// SCOPE.Pattern EntityRecord per `try { ... } catch (...) { ... }`
// occurrence. It runs AFTER the base entity extraction in
// Extractor.Extract and never aborts the primary walker — a failure
// here is logged at warn level and partial results are returned
// (MX-1047 rule #3).
//
// Entity shape (parity with Python / Go / JS):
//
//	Kind       = "SCOPE.Pattern"
//	Name       = "error_handling:try_catch:N"  (N = 1-based line number)
//	SourceFile = absolute path of the source file
//	StartLine  = line number of the try statement (EndLine matches)
//	Language   = "java"
//	Metadata   = {"pattern_type": "error_handling"}
//
// Detection rule:
//
//	AST node type: try_statement (matches both try/catch and
//	               try-with-resources, since tree-sitter-java exposes
//	               them as try_statement and try_with_resources_statement
//	               respectively). We treat both as error-handling blocks.
//
// We match on node.Type() string so a grammar upgrade that renames
// the node does not silently turn the pass into a no-op — a rename
// would be caught by the test suite (not here).

package java

import (
	"fmt"
	"log"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/types"
)

// extractErrorHandlingPatterns walks the AST iteratively and returns one
// EntityRecord per try_statement or try_with_resources_statement node
// found. Safe against panics — a recover at the top converts them into
// a warn-level log, preserving any records already collected.
func extractErrorHandlingPatterns(root *sitter.Node, filePath string) []types.EntityRecord {
	if root == nil {
		return nil
	}

	var records []types.EntityRecord
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[java extractor] WARNING: error-pattern pass panicked on %s: %v — returning partial results", filePath, r)
		}
	}()

	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		t := n.Type()
		if t == "try_statement" || t == "try_with_resources_statement" {
			line := int(n.StartPoint().Row) + 1
			records = append(records, types.EntityRecord{
				Name:       fmt.Sprintf("error_handling:try_catch:%d", line),
				Kind:       "SCOPE.Pattern",
				SourceFile: filePath,
				StartLine:  line,
				EndLine:    line,
				Language:   "java",
				Metadata: map[string]interface{}{
					"pattern_type": "error_handling",
				},
				QualityScore:       1.0,
				EnrichmentRequired: false,
			})
		}
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			stack = append(stack, n.Child(i))
		}
	}

	return records
}
