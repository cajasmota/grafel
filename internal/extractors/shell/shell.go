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
	"strconv"
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
	seen := make(map[string]bool)
	var rels []types.RelationshipRecord
	// binder tracks `cmd=do_work` literal bindings within this function body so a
	// later indirect `$cmd` command head can be resolved to the real function
	// name (#5158, reusing the cross-language literal-binding resolver).
	// Shell variable names are case-sensitive ⇒ identity keyFn. Bash has no
	// nested-function lexical scope here; the function body IS the scope, so no
	// Reset is needed mid-walk.
	binder := extractor.NewLiteralBindingResolver(nil)

	// Walk the body in document order so a binding established earlier is visible
	// to a later command head (last-write-wins / taint semantics).
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "variable_assignment":
			recordShellAssignment(n, src, binder)
		case "command":
			if head, dynVar := commandHeadName(n, src, binder); head != "" &&
				head != callerName && localFns[head] && !seen[head] {
				seen[head] = true
				props := map[string]string{
					"line": strconv.Itoa(int(n.StartPoint().Row) + 1),
				}
				if dynVar != "" {
					// Recovered through a literal binding on `$dynVar` (#5158).
					props["resolved_via"] = extractor.ResolvedViaLiteralBinding
					props["dynamic_target"] = dynVar
				}
				rels = append(rels, types.RelationshipRecord{
					ToID:       head,
					Kind:       "CALLS",
					Properties: props,
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(body)
	return rels
}

// recordShellAssignment feeds a `variable_assignment` node into the binder:
// a bare-word / string-literal RHS Binds the variable to its command-name
// literal; any other RHS (command substitution, expansion, arithmetic) Taints
// the binding so a stale literal is never resolved.
//
//	cmd=do_work        → Bind("cmd", "do_work")
//	other="run_it"     → Bind("other", "run_it")
//	bad=$(date)        → Taint("bad")
func recordShellAssignment(n *sitter.Node, src []byte, binder *extractor.LiteralBindingResolver) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil || nameNode.Type() != "variable_name" {
		return
	}
	name := string(src[nameNode.StartByte():nameNode.EndByte()])
	// The value is the first non-(variable_name,"=") child.
	var val *sitter.Node
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "variable_name", "=":
			continue
		default:
			val = ch
		}
		if val != nil {
			break
		}
	}
	if lit, ok := shellStringLiteral(val, src); ok {
		binder.Bind(name, lit)
		return
	}
	// Non-literal RHS (or empty assignment) — taint.
	binder.Taint(name)
}

// shellStringLiteral returns the static command-name literal carried by an
// assignment RHS, or ("", false) when the RHS is not a plain literal. Handles
// a bare `word` (cmd=do_work) and a double/single-quoted `string` whose only
// content is a single string_content child (other="run_it"). A quoted string
// containing an expansion or anything other than literal text is rejected.
func shellStringLiteral(val *sitter.Node, src []byte) (string, bool) {
	if val == nil {
		return "", false
	}
	switch val.Type() {
	case "word":
		return string(src[val.StartByte():val.EndByte()]), true
	case "raw_string":
		// single-quoted 'literal' — strip the surrounding quotes.
		s := string(src[val.StartByte():val.EndByte()])
		if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
			return s[1 : len(s)-1], true
		}
		return "", false
	case "string":
		// Accept only "literal" — exactly one string_content child between the
		// quotes. Anything with an expansion/substitution child is non-static.
		var content *sitter.Node
		for i := 0; i < int(val.ChildCount()); i++ {
			ch := val.Child(i)
			if ch == nil {
				continue
			}
			switch ch.Type() {
			case "\"":
				continue
			case "string_content":
				if content != nil {
					return "", false
				}
				content = ch
			default:
				return "", false
			}
		}
		if content != nil {
			return string(src[content.StartByte():content.EndByte()]), true
		}
		return "", false
	}
	return "", false
}

// commandHeadName returns the resolved head identifier of a command node plus,
// when the head was an indirect `$var` expansion resolved through a literal
// binding, the original variable name (dynVar); dynVar is "" for a direct word
// head. Returns ("", "") when the head is neither a simple word nor a
// resolvable single-variable expansion.
//
//	command                      → direct:   word "<head>"
//	  command_name
//	    word "<head>"
//
//	command "$cmd"               → indirect: simple_expansion → variable_name
//	  command_name                            (or string wrapping it: "$cmd")
//	    simple_expansion
//	      $ ; variable_name "<var>"
func commandHeadName(cmd *sitter.Node, src []byte, binder *extractor.LiteralBindingResolver) (head, dynVar string) {
	if cmd == nil || cmd.ChildCount() == 0 {
		return "", ""
	}
	nameNode := cmd.Child(0)
	if nameNode == nil || nameNode.Type() != "command_name" {
		return "", ""
	}
	// Direct: a plain word child.
	for i := 0; i < int(nameNode.ChildCount()); i++ {
		ch := nameNode.Child(i)
		if ch != nil && ch.Type() == "word" {
			return string(src[ch.StartByte():ch.EndByte()]), ""
		}
	}
	// Indirect: a lone simple_expansion (`$cmd`) or a string wrapping exactly one
	// (`"$cmd"`). Resolve the variable through the literal binder.
	if v := loneExpansionVar(nameNode, src); v != "" {
		if lit, ok := binder.Resolve(v); ok {
			return lit, v
		}
		// Unresolved indirect head — not a recoverable call target.
		return "", ""
	}
	return "", ""
}

// loneExpansionVar returns the variable name of a command_name node that is
// exactly one `$var` simple_expansion, optionally wrapped in a double-quoted
// string ("$var"); "" when the head is anything else (literal text, multiple
// expansions, concatenations, ${var}-with-ops, etc.).
func loneExpansionVar(nameNode *sitter.Node, src []byte) string {
	// Unwrap a single double-quoted string child.
	node := nameNode
	if c := singleSignificantChild(node); c != nil && c.Type() == "string" {
		node = c
	}
	exp := singleSignificantChild(node)
	if exp == nil || exp.Type() != "simple_expansion" {
		return ""
	}
	for i := 0; i < int(exp.ChildCount()); i++ {
		ch := exp.Child(i)
		if ch != nil && ch.Type() == "variable_name" {
			return string(src[ch.StartByte():ch.EndByte()])
		}
	}
	return ""
}

// singleSignificantChild returns the unique non-quote child of node, or nil when
// node has zero or more than one significant child. Quote (`"`) tokens are
// ignored so a `"$var"` string is treated as wrapping a single expansion.
func singleSignificantChild(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	var only *sitter.Node
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil || ch.Type() == "\"" {
			continue
		}
		if only != nil {
			return nil
		}
		only = ch
	}
	return only
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
