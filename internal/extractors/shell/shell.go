// Package shell implements the tree-sitter–based extractor for Shell/Bash source files.
//
// Extracted entities:
//   - function_definition           → Kind="SCOPE.Operation",  Subtype="function"
//   - script (file-level wrapper)   → Kind="SCOPE.Component",  Subtype="script"
//   - source/. import stubs         → Kind="SCOPE.Component" carrying IMPORTS edge
//
// Issue #380 (PORT-RELS-SHELL) — emits the same three relationship kinds the
// other ported extractors emit:
//
//   - IMPORTS: every `source <path>` and `. <path>` command produces a
//     SCOPE.Component import-stub entity carrying an IMPORTS edge from the
//     file → the sourced path. Properties carry source_module / local_name /
//     imported_name / import_kind="source" so the resolver's dynamic dispatch
//     (issue #90) can target the shell pattern catalog.
//
//   - CALLS: each function body is scanned for `command_name` nodes whose
//     identifier matches a function defined in the same file. External
//     program executions (docker, rm, echo, etc.) are dropped — bash has no
//     reliable way to distinguish a function call from an external invocation
//     without a shell-aware resolver, so we restrict to known same-file
//     callees per the issue spec. Self-recursion is dropped, callees deduped.
//
//   - CONTAINS: a single file-level SCOPE.Component (subtype="script") emits
//     one CONTAINS edge per declared function via BuildOperationStructuralRef
//     ("shell", file, name) — Format A structural-ref (issue #144).
//
// Uses the bash grammar from smacker/go-tree-sitter. Registers itself via
// init() and is imported by the generated registry_gen.go.
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

	root := file.Tree.RootNode()
	imports := collectSources(root, file.Content)

	// Pass 1: IMPORTS — emit one stub entity per `source`/`.` command.
	var entities []types.EntityRecord
	emitImportStubs(root, file, &entities)

	// Pass 2: collect the names of every function defined in this file. The
	// CALLS pass uses this set to filter out external program invocations
	// (the issue spec: "only emit for identifiers that match a previously-
	// defined function in the file").
	localFns := collectLocalFunctionNames(root, file.Content)

	// Pass 3: walk the CST emitting Operation entities, attaching CALLS edges.
	var fnEntities []types.EntityRecord
	walkShell(root, file, imports, localFns, &fnEntities)

	// Pass 4: emit a script-level SCOPE.Component carrying CONTAINS edges to
	// every function in fnEntities. Skipped when there are no functions.
	if len(fnEntities) > 0 {
		entities = append(entities, buildScriptComponent(file, fnEntities))
	}
	entities = append(entities, fnEntities...)

	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "shell")
	extractor.TagEntitiesLanguage(entities, "shell")
	return entities, nil
}

// walkShell performs a depth-first traversal collecting function_definition nodes.
func walkShell(node *sitter.Node, file extractor.FileInput, imports []string, localFns map[string]bool, out *[]types.EntityRecord) {
	if node == nil {
		return
	}
	if node.Type() == "function_definition" {
		if rec, ok := buildFunction(node, file, imports); ok {
			body := findCompoundStatement(node)
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(body, file.Content, rec.Name, localFns)...)
			*out = append(*out, rec)
		}
	}
	for i := range node.ChildCount() {
		walkShell(node.Child(int(i)), file, imports, localFns, out)
	}
}

// findCompoundStatement returns the `compound_statement` body child of a
// function_definition node, or nil.
func findCompoundStatement(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch != nil && ch.Type() == "compound_statement" {
			return ch
		}
	}
	return nil
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

// collectSources collects source/. commands as the legacy
// Properties["imports"] string list (preserved for backwards compatibility
// with the existing Operation-entity contract).
func collectSources(root *sitter.Node, src []byte) []string {
	var imports []string
	walkForSources(root, src, &imports)
	return imports
}

func walkForSources(node *sitter.Node, src []byte, out *[]string) {
	if node == nil {
		return
	}
	if path := sourceCommandArg(node, src); path != "" {
		*out = append(*out, path)
	}
	for i := range node.ChildCount() {
		walkForSources(node.Child(int(i)), src, out)
	}
}

// sourceCommandArg returns the argument of a `source <arg>` / `. <arg>`
// command node, or "" if the node isn't a source command.
//
// Bash CST shape (smacker/go-tree-sitter):
//
//	command
//	  command_name
//	    word "source"   (or word ".")
//	  word "<path>"
func sourceCommandArg(node *sitter.Node, src []byte) string {
	if node == nil || node.Type() != "command" || node.ChildCount() < 2 {
		return ""
	}
	head := node.Child(0)
	if head == nil {
		return ""
	}
	headText := strings.TrimSpace(string(src[head.StartByte():head.EndByte()]))
	if headText != "source" && headText != "." {
		return ""
	}
	// Find the first non-command_name child as the path argument.
	for i := 1; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil {
			continue
		}
		t := ch.Type()
		if t == "word" || t == "string" || t == "raw_string" || t == "concatenation" {
			raw := string(src[ch.StartByte():ch.EndByte()])
			return strings.Trim(raw, `'"`)
		}
	}
	return ""
}

// emitImportStubs walks the CST and emits one SCOPE.Component entity per
// source/. command, each carrying a single IMPORTS edge file→path. Mirrors
// the lua/dart/elixir contract: per-import entity records so the resolver
// can dispatch on Properties["language"] (#90).
func emitImportStubs(root *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	walkForImportStubs(root, file, out)
}

func walkForImportStubs(node *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if node == nil {
		return
	}
	if path := sourceCommandArg(node, file.Content); path != "" {
		*out = append(*out, makeImportStub(file, path))
	}
	for i := range node.ChildCount() {
		walkForImportStubs(node.Child(int(i)), file, out)
	}
}

// makeImportStub builds a SCOPE.Component import-stub carrying a single
// IMPORTS edge for a `source <path>` / `. <path>` command.
//
//	Properties["local_name"]    — trailing path segment (basename).
//	Properties["source_module"] — full sourced path (matches ToID).
//	Properties["imported_name"] — equal to local_name.
//	Properties["import_kind"]   — always "source" for shell.
func makeImportStub(file extractor.FileInput, path string) types.EntityRecord {
	leaf := path
	if slash := strings.LastIndexByte(path, '/'); slash >= 0 {
		leaf = path[slash+1:]
	}
	return types.EntityRecord{
		Name:       path,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   "shell",
		Relationships: []types.RelationshipRecord{
			{
				FromID: file.Path,
				ToID:   path,
				Kind:   "IMPORTS",
				Properties: map[string]string{
					"local_name":    leaf,
					"source_module": path,
					"imported_name": leaf,
					"import_kind":   "source",
				},
			},
		},
	}
}

// collectLocalFunctionNames walks the CST and returns the set of names of
// every `function_definition` node. Used by extractCallRelationships to
// restrict CALLS edges to identifiers known to be functions defined in this
// file (the issue spec — bash can't reliably distinguish function calls from
// external program invocations otherwise).
func collectLocalFunctionNames(root *sitter.Node, src []byte) map[string]bool {
	out := make(map[string]bool)
	walkForFnNames(root, src, out)
	return out
}

func walkForFnNames(node *sitter.Node, src []byte, out map[string]bool) {
	if node == nil {
		return
	}
	if node.Type() == "function_definition" {
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			for i := range node.ChildCount() {
				ch := node.Child(int(i))
				if ch != nil && ch.Type() == "word" {
					nameNode = ch
					break
				}
			}
		}
		if nameNode != nil {
			name := string(src[nameNode.StartByte():nameNode.EndByte()])
			if name != "" {
				out[name] = true
			}
		}
	}
	for i := range node.ChildCount() {
		walkForFnNames(node.Child(int(i)), src, out)
	}
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// command head inside body whose identifier matches a known local function
// (localFns). External program invocations are dropped. Self-recursion is
// dropped, results are deduped.
func extractCallRelationships(body *sitter.Node, src []byte, callerName string, localFns map[string]bool) []types.RelationshipRecord {
	if body == nil || callerName == "" || len(localFns) == 0 {
		return nil
	}
	commands := findAllNodes(body, "command")
	if len(commands) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(commands))
	rels := make([]types.RelationshipRecord, 0, len(commands))
	for _, cmd := range commands {
		head := commandHeadName(cmd, src)
		if head == "" || head == callerName {
			continue
		}
		if !localFns[head] {
			continue
		}
		if seen[head] {
			continue
		}
		seen[head] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: head,
			Kind: "CALLS",
		})
	}
	return rels
}

// commandHeadName returns the identifier text of a command node's
// command_name child, or "" if not a simple identifier.
//
//	command
//	  command_name
//	    word "<head>"
func commandHeadName(cmd *sitter.Node, src []byte) string {
	if cmd == nil || cmd.ChildCount() == 0 {
		return ""
	}
	head := cmd.Child(0)
	if head == nil || head.Type() != "command_name" {
		return ""
	}
	// command_name typically has a single word child.
	for i := 0; i < int(head.ChildCount()); i++ {
		ch := head.Child(i)
		if ch != nil && ch.Type() == "word" {
			return string(src[ch.StartByte():ch.EndByte()])
		}
	}
	// Fall back to the whole command_name text.
	return strings.TrimSpace(string(src[head.StartByte():head.EndByte()]))
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

// buildScriptComponent emits a single file-level SCOPE.Component carrying
// one CONTAINS edge per function entity. The structural-ref shape matches
// every other Format A class→method emitter (#144).
func buildScriptComponent(file extractor.FileInput, fns []types.EntityRecord) types.EntityRecord {
	name := file.Path
	if slash := strings.LastIndexByte(file.Path, '/'); slash >= 0 {
		name = file.Path[slash+1:]
	}
	rels := make([]types.RelationshipRecord, 0, len(fns))
	for _, fn := range fns {
		toID := extractor.BuildOperationStructuralRef("shell", file.Path, fn.Name)
		rels = append(rels, types.RelationshipRecord{
			ToID: toID,
			Kind: "CONTAINS",
		})
	}
	return types.EntityRecord{
		Name:          name,
		Kind:          "SCOPE.Component",
		Subtype:       "script",
		SourceFile:    file.Path,
		Language:      "shell",
		Relationships: rels,
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
