// Error-handling pattern extraction for Python source files.
//
// HISTORY: this pass used to emit one SCOPE.Pattern EntityRecord per
// `try: ... except: ...` occurrence, with Name
// "error_handling:try_catch:N". On the UpVate bench corpus that produced
// ~1,077 nodes (~5.5% of the entire graph) with zero query value: no
// caller asks "show me every try block at line N". Issue #2282 dropped
// the emit. Pattern-level discovery (clustering, frequency analysis)
// can be done over the existing kind=SCOPE.Component/SCOPE.Operation
// nodes when we want it.
//
// The function is preserved as a no-op so the per-language extractor.go
// call sites don't need to be rewired. If we ever want a different
// pattern shape here (per-function aggregate, per-module roll-up,
// per-file boolean), it slots in at this seam.

package python

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/types"
)

// extractErrorHandlingPatterns is intentionally a no-op since #2282.
// Returns nil so the calling extractor's "entities = append(entities,
// errorPatterns...)" continues to work without a special case.
func extractErrorHandlingPatterns(root *sitter.Node, filePath string) []types.EntityRecord {
	_ = root
	_ = filePath
	return nil
}
