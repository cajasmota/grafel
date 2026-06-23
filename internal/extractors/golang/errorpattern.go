// Error-handling pattern extraction for Go source files.
//
// This file implements a secondary extraction pass that emits one
// SCOPE.Pattern EntityRecord per `if err != nil { ... }` occurrence.
// It runs AFTER the base entity extraction in GoExtractor.Extract and
// never interferes with the primary walker — a failure here returns
// partial results and logs at warn level rather than aborting the
// whole file (see behaviour rule #3 in).
//
// Entity shape (matches Python indexer parser.py exactly):
//
//	Kind       = "SCOPE.Pattern"
//	Name       = "error_handling:go_error_return:N"  (N = 1-based line number)
//	SourceFile = absolute path of the source file
//	StartLine  = line number of the if statement (EndLine matches)
//	Language   = "go"
//	Metadata   = {"pattern_type": "error_handling"}
//
// Detection rule:
//
//	AST node type: if_statement
//	Cond          : binary_expression whose operator is "!=" and whose
//	                left  = identifier literal "err"   (or any identifier
//	                        whose name ends in "err" / "Err")
//	                right = nil literal
//
// Tree-sitter-go exposes the if statement as "if_statement" with a
// "condition" field pointing at a "binary_expression". We walk the
// whole tree (not just function bodies) so error patterns declared in
// init() / package-level var blocks with conditional returns are
// also captured — this mirrors Python parser.py behaviour.

package golang

import (
	"fmt"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// extractErrorHandlingPatterns walks the AST and returns one EntityRecord
// per `if err != nil { ... }` occurrence. Never returns a nil error —
// detection failures are logged at warn level and partial results are
// returned instead.
//
// The function is deliberately tolerant of malformed / unexpected AST
// shapes: a panic inside the walker is recovered and converted into a
// warn-level log so the primary extraction pipeline is never aborted
// by a secondary-pass bug.
func extractErrorHandlingPatterns(root ts.Node, src []byte, filePath string) []types.EntityRecord {
	if root == nil {
		return nil
	}

	var records []types.EntityRecord
	defer func() {
		if r := recover(); r != nil {
			logWarning("error-pattern pass panicked on %s: %v — returning partial results", filePath, r)
		}
	}()

	for _, n := range findAll(root, "if_statement") {
		if !isErrNotNilIf(n, src) {
			continue
		}
		// tree-sitter rows are 0-based, parity entity line numbers are 1-based.
		line := int(n.StartPoint().Row) + 1
		records = append(records, types.EntityRecord{
			Name:       fmt.Sprintf("error_handling:go_error_return:%d", line),
			Kind:       "SCOPE.Pattern",
			SourceFile: filePath,
			StartLine:  line,
			EndLine:    line,
			Language:   "go",
			Metadata: map[string]interface{}{
				"pattern_type": "error_handling",
			},
			QualityScore:       1.0,
			EnrichmentRequired: false,
		})
	}

	return records
}

// isErrNotNilIf reports whether an if_statement node is of the form
// `if err != nil { ... }`. The tree-sitter-go grammar pinned in go.mod
// exposes the if_statement's "condition" field pointing at a
// binary_expression whose fields are "left", "operator", and "right"
// (verified via FieldNameForChild in tree-sitter-go v0.x).
//
// For the init-statement form `if x, err := f(); err != nil { ... }`
// the same "condition" field returns only the boolean side of the
// expression — we do not need to walk around the initializer.
func isErrNotNilIf(ifNode ts.Node, src []byte) bool {
	if ifNode == nil {
		return false
	}

	cond := ifNode.ChildByFieldName("condition")
	if cond == nil || cond.Type() != "binary_expression" {
		return false
	}

	// Operator: exposed on the "operator" field of binary_expression
	// as a dedicated token whose Type() is the literal operator string.
	opNode := cond.ChildByFieldName("operator")
	if opNode == nil || opNode.Type() != "!=" {
		return false
	}

	left := cond.ChildByFieldName("left")
	right := cond.ChildByFieldName("right")
	if left == nil || right == nil {
		return false
	}

	// The right-hand side must be the nil literal. tree-sitter-go
	// emits nil as a dedicated "nil" node type — not as an
	// "identifier" whose text is "nil".
	if right.Type() != "nil" {
		return false
	}

	// The left-hand side must be an error-typed identifier. We use
	// the lexical convention (name == "err" or ends in "Err" / "err")
	// which is what the Python parser does — we don't have a type
	// checker.
	return isErrorIdent(left, src)
}

// isErrorIdent reports whether a node is an identifier representing an
// error value. The heuristic matches the Python parser:
//
//   - exact identifier "err"
//   - any identifier whose text ends in "Err" or "err"
//     (e.g. "parseErr", "requestErr", "ErrNoRows" is EXCLUDED because
//     uppercase-leading names are typically sentinel errors, not local
//     error variables — we only match a trailing "err"/"Err").
//
// This matches the Python grafel behaviour which walks the same
// set of local-error naming conventions.
func isErrorIdent(n ts.Node, src []byte) bool {
	if n == nil || n.Type() != "identifier" {
		return false
	}
	text := nodeText(n, src)
	if text == "" {
		return false
	}
	if text == "err" {
		return true
	}
	// Accept camelCase trailing "Err" (parseErr, fooErr) but not a
	// sentinel like "ErrNotFound". The rule: last 3 chars == "Err" or
	// "err" AND the first char is lowercase.
	if len(text) >= 4 {
		first := text[0]
		if first >= 'a' && first <= 'z' {
			tail := text[len(text)-3:]
			if tail == "Err" || tail == "err" {
				return true
			}
		}
	}
	return false
}
