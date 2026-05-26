package mcp_test

// schema_contract_ast_test.go — AST-based exhaustive scan for handler args vs schema declarations.
//
// Issue #2366: PR #2364 (#2318) added a hand-maintained schema_contract_2318_test.go
// as a regression guard for 3 specific gap fixes + an informational inventory of 13
// intentional gaps. New accidental gaps won't be caught automatically. This file is
// the proper static-analysis successor.
//
// TestSchemaContract_AllHandlerArgsDeclared:
//  1. Parses all internal/mcp/*.go files with go/parser + go/ast.
//  2. Walks every CallExpr whose function identifier is one of argInt, argString,
//     argBool, or argFloat and records (enclosingFunc, argKey).
//  3. Uses a hardcoded handlerToTool mapping (extracted directly from the wrap()
//     calls in registerTools) to map handler method names → tool names.
//  4. Builds a dispatch table (dispatcher → []sub-handler) to propagate tool
//     assignments through action-dispatch bundles.
//  5. Uses the live registered Server schema (same as TestSchemaContract_2318_*)
//     as the source of truth for declared parameters.
//  6. Asserts: every (tool, argKey) read in a handler must be declared in the
//     tool's JSON-Schema Properties, UNLESS it is in the intentionalGaps
//     allowlist below.
//
// Allowlist entries carry a reason comment — each maps to the #1639 token-ceiling
// pattern or another documented decision. Add new entries ONLY when the omission
// is intentional, with a justification comment referencing the issue number.
//
// To verify the test catches a regression:
//   temporarily remove a WithNumber/WithString call from server.go, run
//   `go test ./internal/mcp/... -run TestSchemaContract_AllHandlerArgsDeclared -v`
//   and confirm the test fails with a clear message identifying the missing param.
//   Revert the change afterwards.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// intentionalGap documents a (tool, argKey) pair that is intentionally read
// by the handler but NOT declared in the tool's JSON-Schema Properties.
// The why field must reference the relevant issue or ADR.
type intentionalGap struct {
	tool string
	arg  string
	why  string
}

// intentionalGaps is the allowlist of known intentional omissions.
// Each entry must have a non-empty why. Entries from the seed test
// (TestSchemaContract_2318_intentionally_undeclared) are preserved here.
//
// Adding a new entry means the omission is intentional and documented.
// Removing an entry means the param has been added to the schema — also fine.
var intentionalGaps = []intentionalGap{
	// archigraph_find: verbose, min_score, max_results — read from the request
	// map but not declared to stay under the token-ceiling (#1639 / #1921 / #1807).
	{"archigraph_find", "verbose", "#1639 token ceiling pattern (#1921/#1807)"},
	{"archigraph_find", "min_score", "#1639 token ceiling pattern (#1921/#1807)"},
	{"archigraph_find", "max_results", "#1639 token ceiling pattern (#1921/#1807)"},
	// archigraph_find: legacy param alias — accepted but deprecated, intentionally invisible.
	{"archigraph_find", "question", "#2318 deprecated alias for query, intentionally undeclared"},

	// archigraph_inspect: verbose — #1639 token ceiling pattern.
	{"archigraph_inspect", "verbose", "#1639 token ceiling pattern"},
	// archigraph_inspect: legacy alias, intentionally invisible.
	{"archigraph_inspect", "label_or_id", "#2318 deprecated alias for entity_id, intentionally undeclared"},

	// archigraph_expand: legacy param alias — deprecated alias for entity_id (#1916).
	{"archigraph_expand", "node", "deprecated alias accepted but intentionally undeclared (#1916)"},

	// archigraph_traces: min_steps, cross_stack_only, verbose — #1639 pattern.
	{"archigraph_traces", "min_steps", "#1639 token ceiling pattern"},
	{"archigraph_traces", "cross_stack_only", "#1639 token ceiling pattern"},
	{"archigraph_traces", "verbose", "#1639 token ceiling pattern"},

	// archigraph_find_callers / archigraph_find_callees: verbose — #1639 pattern.
	{"archigraph_find_callers", "verbose", "#1639 token ceiling pattern"},
	{"archigraph_find_callees", "verbose", "#1639 token ceiling pattern"},

	// archigraph_neighbors: verbose — shared with find_callers/find_callees
	// structured helper; verbose is intentionally undeclared for token ceiling (#1639).
	{"archigraph_neighbors", "verbose", "#1639 token ceiling pattern (shared via findCallersStructured)"},

	// archigraph_module_analysis: top_n, limit, min_size — #1639 pattern.
	{"archigraph_module_analysis", "top_n", "#1639 token ceiling pattern"},
	{"archigraph_module_analysis", "limit", "#1639 token ceiling pattern"},
	{"archigraph_module_analysis", "min_size", "#1639 token ceiling pattern"},

	// archigraph_repairs: submit-only args read from the bundle but undeclared
	// to keep the handshake token budget under its ceiling (#1756 / #1639 pattern).
	{"archigraph_repairs", "residual_id", "#1756 token ceiling pattern — submit-only arg"},
	{"archigraph_repairs", "resolution", "#1756 token ceiling pattern — submit-only arg"},
	{"archigraph_repairs", "confidence", "#1756 token ceiling pattern — submit-only arg"},
	{"archigraph_repairs", "reasoning", "#1756 token ceiling pattern — submit-only arg"},
	{"archigraph_repairs", "target_entity_id", "#1756 token ceiling pattern — submit-only arg"},
	{"archigraph_repairs", "module", "#1756 token ceiling pattern — submit-only arg"},
	{"archigraph_repairs", "new_target", "#1756 token ceiling pattern — submit-only arg"},
	{"archigraph_repairs", "dynamic_reason", "#1756 token ceiling pattern — submit-only arg"},
	{"archigraph_repairs", "abandon_reason", "#1756 token ceiling pattern — submit-only arg"},
	{"archigraph_repairs", "source", "#1756 token ceiling pattern — submit-only arg"},
	{"archigraph_repairs", "repo", "#1756 token ceiling pattern — submit-only arg (override when residual_id is ambiguous)"},

	// archigraph_topology: verbose read in handleTopologyTopicDetail for token-ceiling
	// suppression but not declared in schema (#1639 pattern).
	{"archigraph_topology", "verbose", "#1639 token ceiling pattern"},

	// archigraph_get_source: legacy alias node_id — deprecated, intentionally undeclared.
	{"archigraph_get_source", "node_id", "deprecated alias for entity_id, intentionally undeclared"},

	// archigraph_patterns: action-specific args for sub-actions (query, record, get,
	// reject, promote) that are undeclared in the schema to stay under the token-ceiling
	// (#1639 pattern). The top-level schema only declares the shared args (action, text,
	// category, limit, steps, exemplars, group, cwd).
	{"archigraph_patterns", "include_candidates", "#1639 token ceiling pattern — query-only arg"},
	{"archigraph_patterns", "include_private", "#1639 token ceiling pattern — query/get-only arg"},
	{"archigraph_patterns", "as_candidate", "#1639 token ceiling pattern — record-only arg"},
	{"archigraph_patterns", "proposer_subagent", "#1639 token ceiling pattern — record-only arg"},
	{"archigraph_patterns", "documentation_url", "#1639 token ceiling pattern — record-only arg"},
	{"archigraph_patterns", "set_to_zero", "#1639 token ceiling pattern — reject-only arg"},
	{"archigraph_patterns", "approval_note", "#1639 token ceiling pattern — promote-only arg"},

	// archigraph_enrichments: link-candidate sub-action args (channel, method, override_target)
	// undeclared in the schema to keep the handshake token budget under the ceiling (#1639).
	{"archigraph_enrichments", "channel", "#1639 token ceiling pattern — list link candidates filter"},
	{"archigraph_enrichments", "method", "#1639 token ceiling pattern — list link candidates filter"},
	{"archigraph_enrichments", "override_target", "#1639 token ceiling pattern — resolve link candidate arg"},

	// archigraph_repairs: include_stale — list-only filter arg, undeclared for token budget (#1639).
	{"archigraph_repairs", "include_stale", "#1639 token ceiling pattern — list-stale filter arg"},

	// archigraph_traces: branching_factor — follow-action-only arg, undeclared for token budget (#1639).
	{"archigraph_traces", "branching_factor", "#1639 token ceiling pattern — follow-only arg"},
}

// handlerToTool maps every (*Server).handleXxx method name to its registered
// MCP tool name. Source: the wrap("tool_name", s.handleXxx) calls in registerTools.
// Sub-handlers reached via action-dispatch are listed separately in dispatchTree.
var handlerToTool = map[string]string{
	"handleWhoami":             "archigraph_whoami",
	"handleGetNodeSource":      "archigraph_get_source",
	"handleQueryGraph":         "archigraph_find",
	"handleGetNode":            "archigraph_inspect",
	"handleGetNeighbors":       "archigraph_expand",
	"handleShortestPath":       "archigraph_trace",
	"handleTraces":             "archigraph_traces",
	"handleListCommunities":    "archigraph_clusters",
	"handleGraphStats":         "archigraph_stats",
	"handleEnrichments":        "archigraph_enrichments",
	"handleRepairs":            "archigraph_repairs",
	"handleApplyDocgenRepairs": "archigraph_apply_docgen_repairs",
	"handlePatterns":           "archigraph_patterns",
	"handleTopology":           "archigraph_topology",
	"handleFlows":              "archigraph_flows",
	"handleGraphPatterns":      "archigraph_graph_patterns",
	"handleSearchEntities":     "archigraph_search_entities",
	"handleSubgraph":           "archigraph_subgraph",
	"handleFindPaths":          "archigraph_find_paths",
	"handleEndpoints":          "archigraph_endpoints",
	"handleNeighbors":          "archigraph_neighbors",
	"handleFindCallers":        "archigraph_find_callers",
	"handleFindCallees":        "archigraph_find_callees",
	"handleImpactRadius":       "archigraph_impact_radius",
	"handleFindDeadCode":       "archigraph_find_dead_code",
	"handleQualityCycles":      "archigraph_quality_cycles",
	"handleAuthCoverage":       "archigraph_auth_coverage",
	"handleTestCoverage":       "archigraph_test_coverage",
	"handleModuleAnalysis":     "archigraph_module_analysis",
	"handleSecrets":            "archigraph_secrets",
	"handleDiffRefs":           "archigraph_diff_refs",
	"handleDocgenStartRun":     "archigraph_docgen_start_run",
	"handleDocgenStatus":       "archigraph_docgen_status",
	"handleDocgenValidate":     "archigraph_docgen_validate",
	"handleDocgenPromote":      "archigraph_docgen_promote",
	"handleDocgenAbort":        "archigraph_docgen_abort",
	"handleDocgenList":         "archigraph_docgen_list",
	"handleStatus":             "archigraph_status",
}

// dispatchTree maps each top-level handler to the set of sub-handlers it can
// dispatch to (action-based bundles). Sub-handlers inherit the same tool name.
// Source: switch action { case ...: return s.handleXxx } blocks in handler files.
var dispatchTree = map[string][]string{
	// archigraph_topology
	"handleTopology": {
		"handleTopologyOrphanPublishers",
		"handleTopologyOrphanSubscribers",
		"handleTopologyTopicDetail",
	},
	// archigraph_flows
	"handleFlows": {
		"handleFlowDeadEnds",
		"handleFlowTruncated",
		"handleFlowDetail",
	},
	// archigraph_graph_patterns
	"handleGraphPatterns": {
		"handlePatternsListGraph",
		"handlePatternsGetGraph",
	},
	// archigraph_patterns
	"handlePatterns": {
		"handlePatternsQuery",
		"handlePatternsRecord",
		"handlePatternsRefine",
		"handlePatternsApply",
		"handlePatternsReject",
		"handlePatternsPromote",
		"handlePatternsGet",
	},
	// archigraph_traces
	"handleTraces": {
		"handleTracesList",
		"handleTracesGet",
		"handleTracesFollow",
	},
	// archigraph_enrichments
	"handleEnrichments": {
		"handleListEnrichmentCandidates",
		"handleSubmitEnrichment",
		"handleRejectEnrichment",
		"handleListLinkCandidates",
		"handleResolveLinkCandidateAction",
	},
	// archigraph_repairs
	"handleRepairs": {
		"handleListResiduals",
		"handleSubmitRepairFromBundle",
	},
	// archigraph_endpoints
	"handleEndpoints": {
		"handleEndpointDefinitions",
		"handleEndpointCalls",
		"handleEndpointStats",
	},
	// archigraph_module_analysis
	"handleModuleAnalysis": {
		"handleModuleCombined",
		"handleModuleCycles",
		"handleModuleCentrality",
	},
	// archigraph_subgraph — sub-methods are helpers, not dispatch-pattern handlers
	// but they do read args.
	"handleSubgraph": {
		"subgraphRaw",
		"subgraphMarkdown",
	},
	// archigraph_neighbors — uses structured helpers.
	"handleNeighbors": {
		"findCallersStructured",
		"findCalleesStructured",
	},
	// archigraph_find_callers — uses structured helper.
	"handleFindCallers": {
		"findCallersStructured",
	},
	// archigraph_find_callees — uses structured helper.
	"handleFindCallees": {
		"findCalleesStructured",
	},
}

// sharedHelpers are functions that call argXxx but are not handlers — their
// arg reads are covered by the tool schemas of the handlers that call them.
// We skip these during the AST scan.
var sharedHelpers = map[string]bool{
	"resolveAndGroup":        true,
	"resolveAndGroupWithRef": true,
	"refForRequest":          true,
	"fieldsArg":              true,
	"resolveStagingPath":     true,
	"parseScopeArg":          true,
	"inferCWD":               true,
	"FromRequest":            true, // PaginationOpts.FromRequest — all its keys are declared
	"emitActivity":           true,
}

// argFuncNames is the set of arg-reader function names to match in the AST.
var argFuncNames = map[string]bool{
	"argInt":    true,
	"argString": true,
	"argBool":   true,
	"argFloat":  true,
}

// handlerArgUsage records all (funcName, argKey) pairs found by the AST scan.
type handlerArgUsage struct {
	funcName string
	argKey   string
	file     string
	line     int
}

// TestSchemaContract_AllHandlerArgsDeclared is the exhaustive AST-based check.
//
// It fails if any handler reads an arg via argInt/argString/argBool/argFloat that
// is NOT declared in the tool's JSON-Schema Properties AND NOT in intentionalGaps.
//
// The test does NOT fail if a schema property exists that is never read by a handler
// (schema-only extras are fine — they may be declared for documentation purposes or
// future use).
func TestSchemaContract_AllHandlerArgsDeclared(t *testing.T) {
	// -------------------------------------------------------------------------
	// Step 1: locate the internal/mcp directory relative to this test file.
	// -------------------------------------------------------------------------
	mcpDir := findMCPDir(t)

	// -------------------------------------------------------------------------
	// Step 2: build the full func→tool mapping (direct + transitive sub-handlers).
	// -------------------------------------------------------------------------
	funcToTool := buildFuncToTool()

	// -------------------------------------------------------------------------
	// Step 3: AST scan — find all argXxx call sites in handler functions.
	// -------------------------------------------------------------------------
	usages := scanArgUsages(t, mcpDir, funcToTool)

	// -------------------------------------------------------------------------
	// Step 4: build the intentional-gap lookup set.
	// -------------------------------------------------------------------------
	allowlist := make(map[string]bool, len(intentionalGaps))
	for _, g := range intentionalGaps {
		allowlist[g.tool+"\x00"+g.arg] = true
	}

	// Log each intentional gap so test output is informative.
	t.Logf("intentional-gap allowlist has %d entries:", len(intentionalGaps))
	for _, g := range intentionalGaps {
		t.Logf("  tool=%-40s arg=%-30s reason=%s", g.tool, g.arg, g.why)
	}

	// -------------------------------------------------------------------------
	// Step 5: load the live schema from a minimal Server.
	// -------------------------------------------------------------------------
	srv := newMinimalServer(t)
	byName := srv.MCP.ListTools()

	// -------------------------------------------------------------------------
	// Step 6: cross-reference. For each (tool, arg) from handler code, assert it
	// is declared in the schema OR in the allowlist.
	// -------------------------------------------------------------------------
	failures := 0
	for _, u := range usages {
		tool, ok := funcToTool[u.funcName]
		if !ok {
			// Not a registered handler — skip.
			continue
		}

		// Check allowlist first.
		key := tool + "\x00" + u.argKey
		if allowlist[key] {
			continue
		}

		// Check schema.
		st, toolFound := byName[tool]
		if !toolFound {
			// Tool not registered — already caught by TestSchemaContract_2318_* tests.
			continue
		}
		props := st.Tool.InputSchema.Properties
		if props == nil {
			t.Errorf("%s:%d: tool %q has no schema properties; handler %q reads arg %q",
				u.file, u.line, tool, u.funcName, u.argKey)
			failures++
			continue
		}
		if _, declared := props[u.argKey]; !declared {
			t.Errorf("%s:%d: tool %q is missing schema declaration for arg %q (read in %s) — "+
				"add mcpapi.WithNumber/WithString/WithBoolean/WithArray(%q, ...) to registerTools, "+
				"or add an intentionalGaps entry if the omission is intentional",
				u.file, u.line, tool, u.argKey, u.funcName, u.argKey)
			failures++
		}
	}

	if failures > 0 {
		t.Logf("%d schema gap(s) found — see errors above", failures)
	} else {
		t.Logf("all %d handler-arg usages are declared in their tool schemas (or in the intentional-gap allowlist)", len(usages))
	}
}

// buildFuncToTool builds the complete funcName→toolName map, including sub-handlers
// reached transitively via the dispatch tree.
func buildFuncToTool() map[string]string {
	out := make(map[string]string, 128)

	// Direct registrations from handlerToTool.
	for fn, tool := range handlerToTool {
		out[fn] = tool
	}

	// Transitively propagate via dispatchTree. One pass is sufficient because
	// all sub-handlers are one hop from a directly-registered handler.
	for parent, children := range dispatchTree {
		tool, ok := out[parent]
		if !ok {
			continue // parent not registered; children inherit nothing
		}
		for _, child := range children {
			if _, already := out[child]; !already {
				out[child] = tool
			}
		}
	}

	return out
}

// scanArgUsages walks all *.go files in dir (non-test files only) and returns
// every argInt/argString/argBool/argFloat call site found inside a function
// that appears in funcToTool (or a shared helper we need to skip).
func scanArgUsages(t *testing.T, dir string, funcToTool map[string]string) []handlerArgUsage {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	var usages []handlerArgUsage

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		fullPath := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, fullPath, nil, 0)
		if err != nil {
			t.Logf("warning: parse error in %s: %v (skipping)", name, err)
			continue
		}

		usages = append(usages, extractArgUsages(fset, f, funcToTool)...)
	}

	return usages
}

// extractArgUsages walks a single parsed file and returns all argXxx call sites
// found inside functions that are known handlers (or sub-handlers).
// It builds a position→funcName map via an outer FuncDecl walk.
func extractArgUsages(fset *token.FileSet, f *ast.File, funcToTool map[string]string) []handlerArgUsage {
	// Build an interval map: for each top-level FuncDecl, record (start, end, name).
	type funcInterval struct {
		start token.Pos
		end   token.Pos
		name  string
	}
	var funcs []funcInterval

	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		funcs = append(funcs, funcInterval{
			start: fd.Pos(),
			end:   fd.End(),
			name:  fd.Name.Name,
		})
	}

	// enclosingFunc returns the function name for a given position.
	enclosingFunc := func(pos token.Pos) string {
		for _, fi := range funcs {
			if pos >= fi.start && pos <= fi.end {
				return fi.name
			}
		}
		return ""
	}

	var out []handlerArgUsage

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Match argInt, argString, argBool, argFloat.
		var funcName string
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			funcName = fn.Name
		case *ast.SelectorExpr:
			// e.g. mcp.argString — unlikely in this package but handle it.
			funcName = fn.Sel.Name
		}
		if !argFuncNames[funcName] {
			return true
		}

		// The second argument must be a string literal (the arg key).
		if len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[1].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		argKey := strings.Trim(lit.Value, `"`)

		// Identify the enclosing function.
		enc := enclosingFunc(call.Pos())
		if enc == "" {
			return true
		}

		// Skip shared helpers (their arg reads are covered by all callers).
		if sharedHelpers[enc] {
			return true
		}

		// Skip if not a known handler/sub-handler — could be a utility function
		// not reachable from any tool (e.g. dropped handlers).
		if _, known := funcToTool[enc]; !known {
			return true
		}

		pos := fset.Position(call.Pos())
		out = append(out, handlerArgUsage{
			funcName: enc,
			argKey:   argKey,
			file:     filepath.Base(pos.Filename),
			line:     pos.Line,
		})
		return true
	})

	return out
}

// findMCPDir returns the absolute path to internal/mcp, located relative to
// this test file's directory (which is internal/mcp itself when tests run via
// `go test ./internal/mcp/...`).
func findMCPDir(t *testing.T) string {
	t.Helper()
	// os.Getwd() inside a `go test` run is the package directory.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	// Confirm we're in internal/mcp by checking for server.go.
	if _, err := os.Stat(filepath.Join(wd, "server.go")); err != nil {
		t.Fatalf("findMCPDir: expected to be in internal/mcp (server.go not found in %s): %v", wd, err)
	}
	return wd
}
