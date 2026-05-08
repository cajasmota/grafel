package golang

import (
	sitter "github.com/smacker/go-tree-sitter"
	tsgo "github.com/smacker/go-tree-sitter/golang"
)

// goGrammar returns the tree-sitter grammar for Go.
// Isolated here to keep the CGO import contained.
func goGrammar() *sitter.Language {
	return tsgo.GetLanguage()
}
