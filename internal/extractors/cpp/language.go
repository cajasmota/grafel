package cpp

import (
	sitter "github.com/smacker/go-tree-sitter"
	tsc "github.com/smacker/go-tree-sitter/c"
	tscpp "github.com/smacker/go-tree-sitter/cpp"
)

// cGrammar returns the tree-sitter grammar for C.
func cGrammar() *sitter.Language {
	return tsc.GetLanguage()
}

// cppGrammar returns the tree-sitter grammar for C++.
func cppGrammar() *sitter.Language {
	return tscpp.GetLanguage()
}
