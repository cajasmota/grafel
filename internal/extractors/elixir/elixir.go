// Package elixir implements the tree-sitter–based extractor for Elixir source files.
//
// Extracted entities:
//   - call with def/defp   → Kind="SCOPE.Operation", Subtype="function"/"private_function"
//   - defmodule            → Kind="SCOPE.Component", Subtype="module"
//   - defprotocol          → Kind="SCOPE.Component", Subtype="protocol"
//   - alias/import/use     → IMPORTS relationships
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package elixir

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("elixir", &Extractor{})
}

// Extractor implements extractor.Extractor for Elixir.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "elixir" }

// Extract walks the tree-sitter CST and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if file.Tree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	var entities []types.EntityRecord
	walkNode(file.Tree.RootNode(), file, &entities)
	return entities, nil
}

// walkNode performs a depth-first traversal.
func walkNode(node *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	if node.Type() == "call" {
		if rec, ok := handleCall(node, file); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walkNode(node.Child(int(i)), file, out)
	}
}

// handleCall inspects a "call" node and produces an entity for def/defp/defmodule/etc.
func handleCall(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	if node.ChildCount() == 0 {
		return types.EntityRecord{}, false
	}

	target := node.Child(0)
	if target == nil {
		return types.EntityRecord{}, false
	}
	callName := string(file.Content[target.StartByte():target.EndByte()])

	switch callName {
	case "defmodule":
		return buildModule(node, file, "module")
	case "defprotocol":
		return buildModule(node, file, "protocol")
	case "def":
		return buildFunction(node, file, "function")
	case "defp":
		return buildFunction(node, file, "private_function")
	case "alias":
		return buildImportRecord(node, file, "alias")
	case "import":
		return buildImportRecord(node, file, "import")
	case "use":
		return buildImportRecord(node, file, "use")
	case "require":
		return buildImportRecord(node, file, "require")
	case "schema":
		return buildSchema(node, file)
	}
	return types.EntityRecord{}, false
}

// buildSchema creates a SCOPE.Schema entity for Ecto `schema "table_name" do` calls.
func buildSchema(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := extractFirstArg(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	// Remove surrounding quotes.
	name = strings.Trim(name, "\"'")
	if name == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Schema",
		Subtype:    "schema",
		SourceFile: file.Path,
		Language:   "elixir",
		StartLine:  int(node.StartPoint().Row) + 1,
		EndLine:    int(node.EndPoint().Row) + 1,
	}, true
}

// buildModule creates a SCOPE.Component entity for defmodule/defprotocol.
func buildModule(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := extractFirstArg(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "elixir",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "defmodule " + name,
		EnrichmentRequired: false,
	}, true
}

// buildFunction creates a SCOPE.Operation entity for def/defp calls.
func buildFunction(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := extractFunctionName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	sig := firstLine(file.Content, node)
	// Strip trailing " do" to match Python signature format
	sig = strings.TrimSuffix(sig, " do")
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "elixir",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
	}, true
}

// buildImportRecord creates a SCOPE.Component entity with an IMPORTS relationship.
func buildImportRecord(node *sitter.Node, file extractor.FileInput, kind string) (types.EntityRecord, bool) {
	raw := extractFirstArg(node, file.Content)
	if raw == "" {
		return types.EntityRecord{}, false
	}
	top := raw
	if idx := strings.Index(raw, "."); idx >= 0 {
		top = raw[:idx]
	}

	return types.EntityRecord{
		Name:       top,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   "elixir",
		Relationships: []types.RelationshipRecord{
			{
				FromID: file.Path,
				ToID:   raw,
				Kind:   "IMPORTS",
				Properties: map[string]string{
					"import_kind": kind,
				},
			},
		},
	}, true
}

// extractFirstArg returns the text of the first non-keyword argument of a call node.
func extractFirstArg(node *sitter.Node, src []byte) string {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if i == 0 {
			continue // skip the call name (def/defmodule/etc.)
		}
		t := ch.Type()
		if t == "arguments" {
			if ch.ChildCount() > 0 {
				arg := ch.Child(0)
				return strings.TrimSpace(string(src[arg.StartByte():arg.EndByte()]))
			}
			continue
		}
		if t == "alias" || t == "dot" || t == "atom" || t == "identifier" {
			return strings.TrimSpace(string(src[ch.StartByte():ch.EndByte()]))
		}
	}
	return ""
}

// extractFunctionName extracts the function name from a def/defp call node.
func extractFunctionName(node *sitter.Node, src []byte) string {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if i == 0 {
			continue // skip "def"/"defp"
		}
		t := ch.Type()
		if t == "identifier" {
			return string(src[ch.StartByte():ch.EndByte()])
		}
		if t == "arguments" {
			if ch.ChildCount() > 0 {
				first := ch.Child(0)
				if name := extractIdentifier(first, src); name != "" {
					return name
				}
			}
		}
		if t == "call" {
			if name := extractIdentifier(ch, src); name != "" {
				return name
			}
		}
	}
	return ""
}

// extractIdentifier finds the first identifier in a node subtree.
func extractIdentifier(node *sitter.Node, src []byte) string {
	if node.Type() == "identifier" {
		return string(src[node.StartByte():node.EndByte()])
	}
	for i := range node.ChildCount() {
		if r := extractIdentifier(node.Child(int(i)), src); r != "" {
			return r
		}
	}
	return ""
}

// firstLine returns the first line of the node's source text.
func firstLine(src []byte, node *sitter.Node) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return strings.TrimSpace(raw)
}
