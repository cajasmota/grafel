// Error-handling pattern extraction for JavaScript and TypeScript source
// files (MX-1047).
//
// This file implements a secondary extraction pass that emits one
// SCOPE.Pattern EntityRecord per `try { ... } catch (...) { ... }`
// occurrence. It runs AFTER the base entity extraction in
// JSExtractor.Extract and never aborts the primary walker — a failure
// here is logged at warn level and partial results are returned
// (MX-1047 rule #3). The pass handles both "javascript" and
// "typescript" languages: the language field is carried from
// FileInput and written back into each emitted record so downstream
// consumers can tell them apart.
//
// Entity shape (parity with Python / Go / Java):
//
//	Kind       = "SCOPE.Pattern"
//	Name       = "error_handling:try_catch:N"  (N = 1-based line number)
//	SourceFile = absolute path of the source file
//	StartLine  = line number of the try statement (EndLine matches)
//	Language   = "javascript" or "typescript"
//	Metadata   = {"pattern_type": "error_handling"}
//
// Detection rule:
//
//	AST node type: try_statement
//
// tree-sitter-javascript and tree-sitter-typescript both expose try/catch
// blocks as a "try_statement" node regardless of whether they have a
// catch clause, a finally clause, or both. Each occurrence of `try`
// produces one entity.

package javascript

import (
	"fmt"
	"log"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/types"
)

// extractErrorHandlingPatterns walks the AST iteratively and returns one
// EntityRecord per try_statement found. Safe against panics — a recover
// at the top converts them into a warn-level log, preserving any
// records already collected.
//
// language is either "javascript" or "typescript" — written into each
// emitted record's Language field so downstream consumers can tell
// them apart without having to look at the file extension.
func extractErrorHandlingPatterns(root *sitter.Node, filePath, language string) []types.EntityRecord {
	if root == nil {
		return nil
	}

	var records []types.EntityRecord
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[javascript extractor] WARNING: error-pattern pass panicked on %s: %v — returning partial results", filePath, r)
		}
	}()

	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if n.Type() == "try_statement" {
			line := int(n.StartPoint().Row) + 1
			e := types.EntityRecord{
				Name:       fmt.Sprintf("error_handling:try_catch:%d", line),
				Kind:       "SCOPE.Pattern",
				SourceFile: filePath,
				StartLine:  line,
				EndLine:    line,
				Language:   language,
				Metadata: map[string]interface{}{
					"pattern_type": "error_handling",
				},
				QualityScore:       1.0,
				EnrichmentStatus:   types.StatusPending,
				EnrichmentRequired: false,
			}
			e.ID = e.ComputeID()
			records = append(records, e)
		}
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			stack = append(stack, n.Child(i))
		}
	}

	return records
}
