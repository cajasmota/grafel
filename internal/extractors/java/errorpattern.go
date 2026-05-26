// Error-handling pattern extraction for Java source files.
//
// HISTORY: this pass used to emit one SCOPE.Pattern EntityRecord per
// `try { ... } catch (...) { ... }` (and try-with-resources)
// occurrence, with Name "error_handling:try_catch:N". Issue #2282
// dropped the emit — see internal/extractors/python/errorpattern.go for
// the full rationale (per-line try nodes were ~5.5% of the UpVate
// graph and had no consumer).
//
// The function is preserved as a no-op so the calling extractor in
// java.go doesn't need to be rewired.

package java

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/types"
)

// extractErrorHandlingPatterns is intentionally a no-op since #2282.
func extractErrorHandlingPatterns(root *sitter.Node, filePath string) []types.EntityRecord {
	_ = root
	_ = filePath
	return nil
}
