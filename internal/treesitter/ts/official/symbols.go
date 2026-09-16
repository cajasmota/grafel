package official

import (
	tsofficial "github.com/tree-sitter/go-tree-sitter"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// Unwrap exposes the underlying official grammar handle so that offline tooling
// can read the grammar's symbol table (NodeKindCount / NodeKindForId /
// FieldCount / FieldNameForId).
//
// It exists for tools/node-type-gate (#7065), which checks that every
// node-type string literal in the extractor layer names a symbol that the
// grammar actually has. Resolving against the handle the daemon parses with is
// the point: a second, hand-copied grammar list would drift, which is the same
// failure mode the gate exists to catch.
//
// Returns false for a ts.Language produced by any other adapter. Parsing must
// still go through Adapter.NewParser; this is a read-only escape hatch for the
// grammar metadata, not a general unwrapping facility.
func Unwrap(l ts.Language) (*tsofficial.Language, bool) {
	ol, ok := l.(Language)
	if !ok || ol.lang == nil {
		return nil, false
	}
	return ol.lang, true
}
