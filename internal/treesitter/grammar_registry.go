package treesitter

import "github.com/cajasmota/grafel/internal/treesitter/ts"

// GrammarLanguages returns a copy of the language-key → grammar map the parser
// factory dispatches on (migratedLanguages).
//
// It exists so that offline tooling — specifically tools/node-type-gate, the
// #7065 gate — can resolve node-type string literals against exactly the
// grammar objects the daemon parses with, rather than against a second,
// hand-maintained list that would drift. Returning a copy keeps the internal
// map immutable from the caller's side.
//
// The keys are registry keys, not display names: aliases ("shell" → bash,
// "terraform" → hcl, "protobuf" → proto, "tsx" → the TSX grammar) are present
// as distinct keys pointing at shared grammar handles.
func GrammarLanguages() map[string]ts.Language {
	out := make(map[string]ts.Language, len(migratedLanguages))
	for k, v := range migratedLanguages {
		out[k] = v
	}
	return out
}
