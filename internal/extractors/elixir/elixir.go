// Package elixir implements the tree-sitter–based extractor for Elixir source files.
//
// Extracted entities:
//   - call with def/defp   → Kind="SCOPE.Operation", Subtype="function"/"private_function"
//   - defmodule            → Kind="SCOPE.Component", Subtype="module"
//   - defprotocol          → Kind="SCOPE.Component", Subtype="protocol"
//   - alias/import/use/require → IMPORTS relationships
//
// Issue #370 (PORT-RELS-ELIXIR) — emits the same three relationship kinds
// the other ported extractors emit:
//
//   - IMPORTS: every `alias`, `import`, `use`, `require` carries
//     Properties{local_name, source_module, imported_name} matching the
//     Java contract (#120) and the Python schema (#93). The leaf segment
//     becomes local_name/imported_name and the prefix is source_module.
//     The original `import_kind` discriminator is preserved.
//   - CALLS: every `call` node inside a `def`/`defp` body emits one CALLS
//     edge per unique callee. Bare `helper()` → ToID="helper". Dotted
//     `Repo.all(User)` → ToID="all". Self-recursion is dropped, Elixir
//     control-flow keywords (`if`, `unless`, `case`, `cond`, `with`, `for`,
//     `try`, `receive`, `quote`, `unquote`, `do`, `fn`) and the def-defining
//     forms (`def`, `defp`, `defmodule`, `defprotocol`, `defimpl`,
//     `defmacro`, `defmacrop`, `defstruct`, `defguard`, `alias`, `import`,
//     `use`, `require`) are filtered.
//   - CONTAINS: defmodule/defprotocol declarations attach one CONTAINS edge
//     per def/defp declared in the body, with the canonical structural-ref
//     shape `scope:operation:method:elixir:<file>:<name>`
//     (BuildOperationStructuralRef, Format A, #144) so the resolver
//     disambiguates same-named functions declared in different files.
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
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "elixir")
	return entities, nil
}

// walkNode performs a depth-first traversal.
//
// Issue #370: defmodule/defprotocol declarations attach a CONTAINS edge per
// def/defp declared inside the body, every def body is scanned for `call`
// nodes that yield CALLS edges, and the four import forms (alias/import/
// use/require) emit IMPORTS entities with the property contract.
func walkNode(node *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	if node.Type() == "call" {
		callName := callHeadName(node, file.Content)
		switch callName {
		case "defmodule":
			handleModule(node, file, "module", out)
			return
		case "defprotocol":
			handleModule(node, file, "protocol", out)
			return
		case "def", "defp":
			subtype := "function"
			if callName == "defp" {
				subtype = "private_function"
			}
			if rec, ok := buildFunction(node, file, subtype); ok {
				rec.Relationships = append(rec.Relationships,
					extractCallRelationships(findDefBody(node), file.Content, rec.Name)...)
				*out = append(*out, rec)
			}
			return
		case "alias":
			if rec, ok := buildImportRecord(node, file, "alias"); ok {
				*out = append(*out, rec)
			}
			return
		case "import":
			if rec, ok := buildImportRecord(node, file, "import"); ok {
				*out = append(*out, rec)
			}
			return
		case "use":
			if rec, ok := buildImportRecord(node, file, "use"); ok {
				*out = append(*out, rec)
			}
			return
		case "require":
			if rec, ok := buildImportRecord(node, file, "require"); ok {
				*out = append(*out, rec)
			}
			return
		case "schema":
			if rec, ok := buildSchema(node, file); ok {
				*out = append(*out, rec)
			}
			// Schema bodies may contain field calls; not entities of interest here.
			return
		}
	}

	for i := range node.ChildCount() {
		walkNode(node.Child(int(i)), file, out)
	}
}

// handleModule emits a SCOPE.Component for defmodule/defprotocol and walks
// its body, then attaches a CONTAINS edge per def/defp found inside.
func handleModule(node *sitter.Node, file extractor.FileInput, subtype string, out *[]types.EntityRecord) {
	rec, ok := buildModule(node, file, subtype)
	if !ok {
		// Still descend so nested entities aren't lost.
		body := findDoBlock(node)
		if body != nil {
			for i := range body.ChildCount() {
				walkNode(body.Child(int(i)), file, out)
			}
		}
		return
	}
	idx := len(*out)
	*out = append(*out, rec)

	body := findDoBlock(node)
	if body == nil {
		return
	}
	before := len(*out)
	for i := range body.ChildCount() {
		walkNode(body.Child(int(i)), file, out)
	}
	after := len(*out)
	for k := before; k < after; k++ {
		child := &(*out)[k]
		if child.Kind != "SCOPE.Operation" {
			continue
		}
		toID := extractor.BuildOperationStructuralRef("elixir", file.Path, child.Name)
		(*out)[idx].Relationships = append((*out)[idx].Relationships,
			types.RelationshipRecord{
				ToID: toID,
				Kind: "CONTAINS",
			})
	}
}

// findDoBlock returns the `do_block` child of a call node, or nil.
func findDoBlock(node *sitter.Node) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() == "do_block" {
			return ch
		}
	}
	return nil
}

// findDefBody returns the body of a def/defp call node. Two shapes:
//
//   - Block form `def foo do ... end` — body is the `do_block` child.
//   - Inline keyword form `defp foo, do: expr` — body is the `keywords`
//     subtree under `arguments` (we return the arguments node and let the
//     caller walk descendants).
func findDefBody(node *sitter.Node) *sitter.Node {
	if b := findDoBlock(node); b != nil {
		return b
	}
	// Inline form: scan arguments for keywords with a `do: expr` pair.
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() == "arguments" {
			return ch
		}
	}
	return nil
}

// callHeadName returns the head identifier of a call node — i.e. the first
// child when it is an `identifier`. Returns "" for dotted heads like
// `Repo.all` (those are method calls, not def-defining forms).
func callHeadName(node *sitter.Node, src []byte) string {
	if node.ChildCount() == 0 {
		return ""
	}
	first := node.Child(0)
	if first == nil || first.Type() != "identifier" {
		return ""
	}
	return string(src[first.StartByte():first.EndByte()])
}

// elixirCallStop lists Elixir keywords / def-defining macros that the
// parser surfaces as `call` heads but are NOT real call targets.
var elixirCallStop = map[string]bool{
	// Control flow / language constructs.
	"if": true, "unless": true, "case": true, "cond": true, "with": true,
	"for": true, "try": true, "receive": true, "quote": true, "unquote": true,
	"do": true, "fn": true, "raise": true, "throw": true,
	// Def-defining macros (callee names that introduce entities, not invocations).
	"def": true, "defp": true, "defmodule": true, "defprotocol": true,
	"defimpl": true, "defmacro": true, "defmacrop": true, "defstruct": true,
	"defguard": true, "defguardp": true, "defdelegate": true, "defexception": true,
	"defoverridable": true, "defcallback": true,
	// Imports.
	"alias": true, "import": true, "use": true, "require": true,
	// Test macros (Phoenix/ExUnit).
	"test": true, "describe": true, "setup": true, "setup_all": true,
	"assert": true, "refute": true, "assert_raise": true, "assert_receive": true,
	// Schema / field declarators handled elsewhere.
	"schema": true, "field": true, "has_many": true, "has_one": true,
	"belongs_to": true, "many_to_many": true, "embeds_one": true, "embeds_many": true,
	"timestamps": true,
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// `call` descendant of body. Targets:
//
//   - Bare `helper()`              — first child is `identifier` "helper"
//   - Dotted `Repo.all(args)`      — first child is `dot`; trailing
//     identifier of the dot is the callee name ("all")
//   - Atom-receiver `:foo.bar()`   — same dot shape; we still take the
//     trailing identifier
//
// Self-recursion is dropped. Keywords / def-defining forms are filtered.
func extractCallRelationships(body *sitter.Node, src []byte, callerName string) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAllNodes(body, "call")
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target := elixirCallTarget(call, src)
		if target == "" || target == callerName {
			continue
		}
		if elixirCallStop[target] {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
		})
	}
	return rels
}

// elixirCallTarget resolves the callee name from a `call` node.
func elixirCallTarget(call *sitter.Node, src []byte) string {
	if call.ChildCount() == 0 {
		return ""
	}
	head := call.Child(0)
	switch head.Type() {
	case "identifier":
		return string(src[head.StartByte():head.EndByte()])
	case "dot":
		// dot has children: alias/identifier/atom, ".", identifier
		// The trailing identifier is the method name.
		for i := int(head.ChildCount()) - 1; i >= 0; i-- {
			ch := head.Child(i)
			if ch.Type() == "identifier" {
				return string(src[ch.StartByte():ch.EndByte()])
			}
		}
	}
	return ""
}

// findAllNodes returns every descendant of root whose Type() is in kinds.
func findAllNodes(root *sitter.Node, kinds ...string) []*sitter.Node {
	if root == nil {
		return nil
	}
	set := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		set[k] = true
	}
	var out []*sitter.Node
	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if set[n.Type()] {
			out = append(out, n)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return out
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
//
// Issue #370 — the IMPORTS edge follows the same Properties contract Java
// emits (#120) and the Python schema (#93):
//
//	Properties["local_name"]    — the leaf identifier introduced by the
//	                              import. For `alias Foo` this is "Foo";
//	                              for `alias Foo.Bar.Baz` this is "Baz".
//	Properties["source_module"] — the dotted prefix. For `alias Foo` this
//	                              is "Foo"; for `alias Foo.Bar.Baz` this
//	                              is "Foo.Bar".
//	Properties["imported_name"] — equal to local_name.
//	Properties["import_kind"]   — preserved discriminator: "alias",
//	                              "import", "use", or "require".
func buildImportRecord(node *sitter.Node, file extractor.FileInput, kind string) (types.EntityRecord, bool) {
	raw := extractFirstArg(node, file.Content)
	if raw == "" {
		return types.EntityRecord{}, false
	}
	top := raw
	if idx := strings.Index(raw, "."); idx >= 0 {
		top = raw[:idx]
	}

	leaf := raw
	mod := raw
	if dot := strings.LastIndexByte(raw, '.'); dot > 0 {
		leaf = raw[dot+1:]
		mod = raw[:dot]
	}

	props := map[string]string{
		"local_name":    leaf,
		"source_module": mod,
		"imported_name": leaf,
		"import_kind":   kind,
	}

	return types.EntityRecord{
		Name:       top,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   "elixir",
		Relationships: []types.RelationshipRecord{
			{
				FromID:     file.Path,
				ToID:       raw,
				Kind:       "IMPORTS",
				Properties: props,
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
