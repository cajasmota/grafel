package mcp_test

// schema_contract_autoderiv_test.go — AST-based auto-derivation of handlerToTool
// and dispatchTree from server.go's registerTools function.
//
// Issue #2404: The hardcoded handlerToTool (38 entries) and dispatchTree tables in
// schema_contract_ast_test.go were a maintenance hazard — a new wrap() registration
// without a matching table update silently skipped the new handler's args.
//
// This file replaces both tables with buildFuncToToolFromAST, which:
//   1. Parses server.go via go/ast to find registerTools.
//   2. Extracts every s.wrap(<tool-name>, s.handleXxx) call — building the direct map.
//      String literals are extracted directly; identifier references (e.g. sentinelToolName)
//      are resolved from the package-level const declarations in the same file.
//   3. Parses all non-test *.go files in internal/mcp/ to find dispatcher functions
//      (functions in the direct map) that delegate to sub-handlers via
//      `return s.XXX(ctx, req)` call expressions — building the transitive map.
//   4. Propagates tool-name assignments transitively (one hop is sufficient for the
//      current architecture; a BFS handles deeper nesting if it ever arises).
//
// Registration patterns handled:
//   - s.wrap("grafel_xxx", s.handleXxx)          — string literal tool name  [line ~282]
//   - s.wrap(sentinelToolName, s.handleStatus)        — const-ident tool name     [line ~725]
//
// Heterogeneous patterns that would require a STOP:
//   - wrap() called in a loop (range over a slice of handlers)
//   - registrations split across multiple functions other than registerTools
//   - tool name computed at runtime
// If any of these are detected, buildFuncToToolFromAST calls t.Fatalf with a clear message.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildFuncToToolFromAST parses server.go to auto-derive the handler→tool mapping,
// then parses all handler files to derive the dispatch sub-tree, and returns the
// complete funcName→toolName map (direct + transitive).
//
// It is the replacement for the hardcoded handlerToTool + dispatchTree combo.
func buildFuncToToolFromAST(t *testing.T, mcpDir string) map[string]string {
	t.Helper()

	serverFile := filepath.Join(mcpDir, "server.go")

	// Step 1: parse server.go.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, serverFile, nil, 0)
	if err != nil {
		t.Fatalf("buildFuncToToolFromAST: parse server.go: %v", err)
	}

	// Step 2: collect package-level const declarations for identifier resolution.
	// e.g. sentinelToolName = "grafel_status"
	constValues := extractPackageConsts(f)

	// Step 3: extract direct handler→tool from registerTools wrap() calls.
	directMap, err := extractWrapCalls(fset, f, constValues)
	if err != nil {
		t.Fatalf("buildFuncToToolFromAST: %v", err)
	}
	t.Logf("auto-derived %d direct handler→tool entries from registerTools wrap() calls", len(directMap))

	// Step 4: build the dispatch sub-tree by scanning all handler files for
	// sub-handler delegations (return s.handleXxx(ctx, req) patterns).
	dispTree, err := extractDispatchTree(mcpDir, directMap)
	if err != nil {
		t.Fatalf("buildFuncToToolFromAST: build dispatch tree: %v", err)
	}

	// Step 5: propagate tool names transitively.
	out := make(map[string]string, len(directMap)+64)
	for fn, tool := range directMap {
		out[fn] = tool
	}
	// BFS propagation — handles arbitrary depth (currently one hop).
	changed := true
	for changed {
		changed = false
		for parent, children := range dispTree {
			tool, ok := out[parent]
			if !ok {
				continue
			}
			for _, child := range children {
				if _, already := out[child]; !already {
					out[child] = tool
					changed = true
				}
			}
		}
	}

	// Log the transitive sub-handler count for diagnostics.
	subCount := len(out) - len(directMap)
	t.Logf("auto-derived %d sub-handler entries from dispatch tree (total: %d)", subCount, len(out))

	return out
}

// extractPackageConsts returns a map of const-name → string-value for all
// package-level const declarations in the given file that have string literal values.
// This handles the sentinelToolName = "grafel_status" pattern.
func extractPackageConsts(f *ast.File) map[string]string {
	out := make(map[string]string)
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i < len(vs.Values) {
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						out[name.Name] = strings.Trim(lit.Value, `"`)
					}
				}
			}
		}
	}
	return out
}

// extractWrapCalls finds the registerTools function in the parsed file and extracts
// every s.wrap(<toolName>, s.handleXxx) call, returning handler-name → tool-name.
//
// Supported tool-name forms:
//   - string literal:   s.wrap("grafel_xxx", s.handleXxx)
//   - const identifier: s.wrap(sentinelToolName, s.handleXxx)
//
// Returns an error if wrap() is called in a loop, or if the tool-name cannot be
// resolved to a string (both indicate a pattern that needs manual review).
func extractWrapCalls(fset *token.FileSet, f *ast.File, constValues map[string]string) (map[string]string, error) {
	// Find the registerTools function declaration.
	var registerToolsBody *ast.BlockStmt
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "registerTools" || fd.Body == nil {
			continue
		}
		registerToolsBody = fd.Body
		break
	}
	if registerToolsBody == nil {
		return nil, fmt.Errorf("registerTools function not found in server.go")
	}

	out := make(map[string]string)
	var walkErr error

	ast.Inspect(registerToolsBody, func(n ast.Node) bool {
		if walkErr != nil {
			return false
		}

		// Detect loop constructs — these indicate a heterogeneous registration pattern
		// that this scanner does not handle.
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			walkErr = fmt.Errorf("wrap() called inside a loop in registerTools — auto-derivation requires manual review; STOP")
			return false
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Match s.wrap(...) — a SelectorExpr where Sel.Name == "wrap".
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "wrap" {
			return true
		}

		// wrap() must have exactly 2 arguments.
		if len(call.Args) != 2 {
			pos := fset.Position(call.Pos())
			walkErr = fmt.Errorf("unexpected wrap() call with %d args at %s:%d — expected 2",
				len(call.Args), pos.Filename, pos.Line)
			return false
		}

		// Resolve the tool name (first arg).
		toolName, err := resolveStringArg(call.Args[0], constValues)
		if err != nil {
			pos := fset.Position(call.Args[0].Pos())
			walkErr = fmt.Errorf("cannot resolve wrap() tool-name arg at %s:%d: %w",
				pos.Filename, pos.Line, err)
			return false
		}

		// Resolve the handler name (second arg): must be s.handleXxx — a SelectorExpr.
		handlerSel, ok := call.Args[1].(*ast.SelectorExpr)
		if !ok {
			pos := fset.Position(call.Args[1].Pos())
			walkErr = fmt.Errorf("wrap() second arg is not a selector expression at %s:%d — expected s.handleXxx",
				pos.Filename, pos.Line)
			return false
		}
		handlerName := handlerSel.Sel.Name

		out[handlerName] = toolName
		return true
	})

	if walkErr != nil {
		return nil, walkErr
	}
	return out, nil
}

// resolveStringArg resolves an AST expression to its string value.
// Handles:
//   - *ast.BasicLit with token.STRING — direct string literal
//   - *ast.Ident — lookup in constValues map (package-level const)
func resolveStringArg(expr ast.Expr, constValues map[string]string) (string, error) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", fmt.Errorf("expected string literal, got %v", e.Kind)
		}
		return strings.Trim(e.Value, `"`), nil
	case *ast.Ident:
		val, ok := constValues[e.Name]
		if !ok {
			return "", fmt.Errorf("identifier %q not found in package-level const declarations", e.Name)
		}
		return val, nil
	default:
		return "", fmt.Errorf("unsupported expression type %T — only string literals and const identifiers are supported", expr)
	}
}

// extractDispatchTree scans all non-test *.go files in mcpDir and, for each
// function in directMap (the top-level registered handlers), finds every
// `return s.Xxx(ctx, req)` call expression. The callee name is recorded as
// a sub-handler of that parent.
//
// This covers both standard dispatch (return s.handleFlowDeadEnds(ctx, req)) and
// structured helpers (return s.findCallersStructured(...), return s.subgraphRaw(...)).
//
// The scanner does NOT require the callee to start with "handle" — it collects
// any `s.Xxx(...)` return-call from within a registered handler body.
func extractDispatchTree(mcpDir string, directMap map[string]string) (map[string][]string, error) {
	entries, err := os.ReadDir(mcpDir)
	if err != nil {
		return nil, fmt.Errorf("ReadDir %s: %v", mcpDir, err)
	}

	// Build interval map: funcName → (startPos, endPos) across all files.
	// We need this to identify which top-level function contains a given call.
	type funcSpan struct {
		name  string
		start token.Pos
		end   token.Pos
	}

	fset := token.NewFileSet()
	var allFuncs []funcSpan // all parsed functions, across all files
	type fileAST struct {
		f    *ast.File
		name string
	}
	var parsedFiles []fileAST

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fullPath := filepath.Join(mcpDir, name)
		f, err := parser.ParseFile(fset, fullPath, nil, 0)
		if err != nil {
			continue // non-fatal for dispatch derivation
		}
		parsedFiles = append(parsedFiles, fileAST{f: f, name: name})
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			allFuncs = append(allFuncs, funcSpan{
				name:  fd.Name.Name,
				start: fd.Pos(),
				end:   fd.End(),
			})
		}
	}

	// enclosingFunc returns the top-level function name for a position.
	enclosingFunc := func(pos token.Pos) string {
		for _, fs := range allFuncs {
			if pos >= fs.start && pos <= fs.end {
				return fs.name
			}
		}
		return ""
	}

	// Scan for any s.Xxx(...) call expressions inside directly registered handlers.
	// This covers:
	//   - return s.handleXxx(ctx, req)       — standard dispatch
	//   - v, err := s.findCallersStructured(ctx, req) — assignment-form helpers
	//   - return s.handleResolveLinkCandidateAction(ctx, req, "accept") — extra args
	// We collect any callee called as s.Method(...) within a registered handler body
	// regardless of argument count or call form.
	dispTree := make(map[string][]string)
	seen := make(map[string]map[string]bool) // parent → set of children (dedup)

	for _, pf := range parsedFiles {
		ast.Inspect(pf.f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			callerSel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// Must be s.Xxx — receiver should be an Ident named "s".
			recvIdent, ok := callerSel.X.(*ast.Ident)
			if !ok || recvIdent.Name != "s" {
				return true
			}
			calleeName := callerSel.Sel.Name

			// Find the enclosing function.
			parent := enclosingFunc(call.Pos())
			if parent == "" {
				return true
			}

			// Record edges from ANY function, not just directly-registered
			// handlers. BFS propagation (in buildFuncToToolFromAST) only assigns
			// a tool to a child when the parent already carries one, so recording
			// every s.Xxx(...) edge is safe: edges off any never-reached function
			// simply never receive a tool. This is required for multi-level
			// dispatch chains introduced by the #5546/#5549 CORE-cluster canonical
			// tools, where a canonical handler (handleCoreEndpoints) delegates to
			// an absorbed handler (handleEndpoints) which in turn fans out to
			// sub-handlers (handleEndpointDefinitions, …). The absorbed handler is
			// no longer in directMap, so gating on directMap[parent] would sever
			// the chain and orphan the leaf sub-handlers.

			// Skip self-calls, wrap calls, and pure receiver ops (e.g. s.MCP.AddTool).
			if calleeName == parent || calleeName == "wrap" {
				return true
			}

			// Dedup.
			if seen[parent] == nil {
				seen[parent] = make(map[string]bool)
			}
			if !seen[parent][calleeName] {
				seen[parent][calleeName] = true
				dispTree[parent] = append(dispTree[parent], calleeName)
			}
			return true
		})
	}

	return dispTree, nil
}
