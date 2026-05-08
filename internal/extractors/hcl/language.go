package hcl

import (
	sitter "github.com/smacker/go-tree-sitter"
	tshcl "github.com/smacker/go-tree-sitter/hcl"
)

// hclGrammar returns the tree-sitter grammar for HCL.
func hclGrammar() *sitter.Language {
	return tshcl.GetLanguage()
}
