package yaml

import (
	sitter "github.com/smacker/go-tree-sitter"
	tsyaml "github.com/smacker/go-tree-sitter/yaml"
)

// yamlGrammar returns the tree-sitter grammar for YAML.
// Isolated here to keep the CGO import contained.
func yamlGrammar() *sitter.Language {
	return tsyaml.GetLanguage()
}
