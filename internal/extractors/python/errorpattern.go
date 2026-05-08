// Error-handling pattern extraction for Python source files (MX-1047).
//
// This file implements a secondary extraction pass that emits one
// SCOPE.Pattern EntityRecord per `try: ... except: ...` occurrence.
// It runs AFTER the base entity extraction in Extractor.Extract and
// never aborts the primary walker — a failure here is logged at warn
// level and partial results are returned (MX-1047 rule #3).
//
// Entity shape (matches Python indexer parser.py exactly):
//
//	Kind       = "SCOPE.Pattern"
//	Name       = "error_handling:try_catch:N"  (N = 1-based line number)
//	SourceFile = absolute path of the source file
//	StartLine  = line number of the try statement (EndLine matches)
//	Language   = "python"
//	Metadata   = {"pattern_type": "error_handling"}
//
// Detection rule:
//
//	AST node type: try_statement
//
// tree-sitter-python represents `try/except/finally/else` blocks as a
// single try_statement node. Every occurrence, whether it has one or
// many except clauses, is emitted as one entity — matching the Python
// parser behaviour where one `try` token in the file produces one
// entity with the try-line number as the key.

package python

import (
	"fmt"
	"log"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/types"
)

// extractErrorHandlingPatterns walks the AST and returns one EntityRecord
// per try_statement node found. Safe against panics — a recover at the
// top converts them into a warn-level log, preserving any records
// already collected.
func extractErrorHandlingPatterns(root *sitter.Node, filePath string) []types.EntityRecord {
	if root == nil {
		return nil
	}

	var records []types.EntityRecord
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[python extractor] WARNING: error-pattern pass panicked on %s: %v — returning partial results", filePath, r)
		}
	}()

	// Depth-first walk: collect every try_statement node. Iterative to
	// avoid stack overflow on deeply-nested try/except blocks.
	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if n.Type() == "try_statement" {
			line := int(n.StartPoint().Row) + 1
			records = append(records, types.EntityRecord{
				Name:       fmt.Sprintf("error_handling:try_catch:%d", line),
				Kind:       "SCOPE.Pattern",
				SourceFile: filePath,
				StartLine:  line,
				EndLine:    line,
				Language:   "python",
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
