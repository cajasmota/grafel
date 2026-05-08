package html

import (
	sitter "github.com/smacker/go-tree-sitter"
	tshtml "github.com/smacker/go-tree-sitter/html"
)

// htmlGrammar returns the tree-sitter grammar for HTML.
// Isolated here to keep the CGO import contained.
func htmlGrammar() *sitter.Language {
	return tshtml.GetLanguage()
}
