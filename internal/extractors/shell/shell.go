// Package shell implements the tree-sitter–based extractor for Shell/Bash source files.
//
// Extracted entities:
//   - function_definition → Kind="SCOPE.Operation", Subtype="function"
//
// Uses the bash grammar from smacker/go-tree-sitter.
// Registers itself via init() and is imported by registry_gen.go.
package shell

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("shell", &Extractor{})
}

// Extractor implements extractor.Extractor for Shell/Bash.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "shell" }

// Extract walks the tree-sitter CST and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}

	// Fall back to regex if no tree is available.
	if file.Tree == nil {
		return extractRegex(file), nil
	}

	var entities []types.EntityRecord
	imports := collectSources(file.Tree.RootNode(), file.Content)
	walkShell(file.Tree.RootNode(), file, imports, &entities)
	return entities, nil
}

// walkShell performs a depth-first traversal collecting function_definition nodes.
func walkShell(node *sitter.Node, file extractor.FileInput, imports []string, out *[]types.EntityRecord) {
	if node == nil {
		return
	}
	if node.Type() == "function_definition" {
		if rec, ok := buildFunction(node, file, imports); ok {
			*out = append(*out, rec)
		}
	}
	for i := range node.ChildCount() {
		walkShell(node.Child(int(i)), file, imports, out)
	}
}

// buildFunction creates a SCOPE.Operation entity for a function_definition node.
func buildFunction(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		// Try first word child.
		for i := range node.ChildCount() {
			ch := node.Child(int(i))
			if ch.Type() == "word" {
				nameNode = ch
				break
			}
		}
	}
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := string(file.Content[nameNode.StartByte():nameNode.EndByte()])
	if name == "" {
		return types.EntityRecord{}, false
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "function",
		SourceFile:         file.Path,
		Language:           "shell",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          name + "()",
		EnrichmentRequired: false,
		Properties: map[string]string{
			"imports": strings.Join(imports, ","),
		},
	}, true
}

// collectSources collects source/. commands as import equivalents.
func collectSources(root *sitter.Node, src []byte) []string {
	var imports []string
	walkForSources(root, src, &imports)
	return imports
}

func walkForSources(node *sitter.Node, src []byte, out *[]string) {
	if node == nil {
		return
	}
	if node.Type() == "command" && node.ChildCount() > 0 {
		first := node.Child(0)
		if first != nil {
			text := string(src[first.StartByte():first.EndByte()])
			if (text == "source" || text == ".") && int(node.ChildCount()) > 1 {
				arg := node.Child(1)
				if arg != nil {
					*out = append(*out, string(src[arg.StartByte():arg.EndByte()]))
				}
			}
		}
	}
	for i := range node.ChildCount() {
		walkForSources(node.Child(int(i)), src, out)
	}
}

// extractRegex is a fallback for when no tree-sitter parse result is available.
// Matches: name() { or function name {
func extractRegex(file extractor.FileInput) []types.EntityRecord {
	src := string(file.Content)
	lines := strings.Split(src, "\n")
	var entities []types.EntityRecord
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		var name string
		// Patterns: "function name {" or "name() {"
		if strings.HasPrefix(trimmed, "function ") {
			rest := strings.TrimPrefix(trimmed, "function ")
			rest = strings.TrimSpace(rest)
			// strip () and {
			if idx := strings.IndexAny(rest, "( {"); idx > 0 {
				name = rest[:idx]
			} else {
				name = rest
			}
		} else if idx := strings.Index(trimmed, "()"); idx > 0 {
			name = trimmed[:idx]
		}
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, " \t$#!") {
			continue
		}
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Operation",
			Subtype:            "function",
			SourceFile:         file.Path,
			Language:           "shell",
			StartLine:          i + 1,
			EndLine:            i + 1,
			Signature:          name + "()",
			EnrichmentRequired: false,
		})
	}
	return entities
}
