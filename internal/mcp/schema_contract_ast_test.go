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

	// archigraph_save_finding: optional args undeclared for token budget (#2426 / #1639 pattern).
	{"archigraph_save_finding", "type", "#2426 token ceiling pattern — optional note type"},
	{"archigraph_save_finding", "nodes", "#2426 token ceiling pattern — optional node list"},
	{"archigraph_save_finding", "repo_filter", "#2426 token ceiling pattern — optional repo filter"},

	// archigraph_list_findings: optional args undeclared for token budget (#2426 / #1639 pattern).
	{"archigraph_list_findings", "since", "#2426 token ceiling pattern — optional RFC3339 filter"},
	{"archigraph_list_findings", "entity_id", "#2426 token ceiling pattern — optional entity filter"},
	{"archigraph_list_findings", "limit", "#2426 token ceiling pattern — optional result limit"},
	{"archigraph_list_findings", "type", "#2810 token ceiling pattern — optional finding-type filter (e.g. security_finding)"},

	// archigraph_cross_links: per-action args undeclared for token budget (#2424 / #1639 pattern).
	{"archigraph_cross_links", "channel", "#2424 token ceiling pattern — list filter"},
	{"archigraph_cross_links", "method", "#2424 token ceiling pattern — list filter"},
	{"archigraph_cross_links", "limit", "#2424 token ceiling pattern — list limit"},
	{"archigraph_cross_links", "repo_filter", "#2424 token ceiling pattern — list filter"},
	{"archigraph_cross_links", "candidate_id", "#2424 token ceiling pattern — accept/reject arg"},
	{"archigraph_cross_links", "override_target", "#2424 token ceiling pattern — accept override"},
	{"archigraph_cross_links", "reason", "#2424 token ceiling pattern — reject reason"},

	// archigraph_license_audit: optional args undeclared for token budget (#2427 / #1639 pattern).
	{"archigraph_license_audit", "include_transitive", "#2427 token ceiling pattern — optional transitive flag"},
	{"archigraph_license_audit", "severity", "#2427 token ceiling pattern — optional severity filter"},
	{"archigraph_license_audit", "limit", "#2427 token ceiling pattern — optional result limit"},

	// archigraph_payload_drift: optional filter args undeclared for token budget (#2770 / #1639 pattern).
	{"archigraph_payload_drift", "severity", "#2770 token ceiling pattern — optional severity floor"},
	{"archigraph_payload_drift", "endpoint", "#2770 token ceiling pattern — optional endpoint substring filter"},
	{"archigraph_payload_drift", "repo", "#2770 token ceiling pattern — optional repo substring filter"},
	{"archigraph_payload_drift", "limit", "#2770 token ceiling pattern — optional result limit"},

	// archigraph_literal_parity: optional entity-pin args undeclared for token
	// budget (#4421 / #1639 pattern). The three required args (group_oracle,
	// group_v3, set) ARE declared; *_source pin the value-sets when auto-locate
	// is insufficient.
	{"archigraph_literal_parity", "oracle_source", "#4421 token ceiling pattern — optional oracle value-set entity pin"},
	{"archigraph_literal_parity", "v3_source", "#4421 token ceiling pattern — optional v3 value-set entity pin"},

	// archigraph_endpoint_posture: scan-mode pagination undeclared for token
	// budget (#1639 pattern). entity_id/facet/path_contains/method ARE declared.
	{"archigraph_endpoint_posture", "limit", "#1639 token ceiling pattern — scan-mode result limit"},
	{"archigraph_endpoint_posture", "offset", "#1639 token ceiling pattern — scan-mode pagination offset"},
}

// handlerToTool and dispatchTree have been REMOVED.
// Both are now auto-derived from server.go's registerTools() via buildFuncToToolFromAST
// (see schema_contract_autoderiv_test.go). This eliminates the maintenance hazard
// where a new wrap() registration without a table update silently skipped the handler.
// Issue #2404.

// orphanedHandlers is the allowlist of handler functions whose corresponding MCP tool
// was deliberately dropped (the wrap() call removed) but whose function body was not
// yet deleted. They read argXxx but have no registered tool, so the scanner would
// otherwise fail loudly — which is the correct behavior for NEW orphans.
//
// orphanedHandlers is the allowlist of handler functions that are known to
// exist without a wrap() registration. Each entry must have a comment explaining
// why it is exempt. An empty map means all handlers are properly registered.
// (Cleared in #2428: all 5 previously-deferred orphans have now been deleted.)
var orphanedHandlers = map[string]string{}

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
	"argMinConfidence":       true, // #2769 Phase 1C — shared min_confidence reader
	"includeWants":           true, // #4423 — shared opt-in facet reader (include declared on archigraph_effects)
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
	// Step 2: build the full func→tool mapping (direct + transitive sub-handlers)
	// by parsing server.go's registerTools() via go/ast. Issue #2404.
	// -------------------------------------------------------------------------
	funcToTool := buildFuncToToolFromAST(t, mcpDir)

	// -------------------------------------------------------------------------
	// Step 3: AST scan — find all argXxx call sites across all non-test files.
	// Both registered and unregistered functions are included; step 6 below
	// flags unregistered ones as errors (catch missing wrap() registrations).
	// -------------------------------------------------------------------------
	usages := scanArgUsages(t, mcpDir)

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
	//
	// Additionally, assert that every function that reads args via argXxx is either:
	//   (a) mapped to a registered tool via funcToTool, OR
	//   (b) in the sharedHelpers allowlist (covered transitively by its callers).
	// This catches the case where a new wrap() registration is added to server.go
	// but a later deletion of that wrap() call leaves the handler unregistered —
	// the handler's args would otherwise be silently skipped. (#2404)
	// -------------------------------------------------------------------------
	failures := 0
	// Track functions already reported as unregistered to avoid duplicate errors.
	reportedUnregistered := make(map[string]bool)

	for _, u := range usages {
		tool, ok := funcToTool[u.funcName]
		if !ok {
			// Function reads args but is not reachable from any registered tool.
			// If it is a known pre-existing orphan, skip silently (tech debt noted).
			if _, isOrphaned := orphanedHandlers[u.funcName]; isOrphaned {
				continue
			}
			// Otherwise it's a new unregistered handler — fail loudly so the developer
			// knows they need to add a wrap() registration.
			if !reportedUnregistered[u.funcName] {
				reportedUnregistered[u.funcName] = true
				t.Errorf("%s:%d: handler %q reads arg %q but has no registered tool — "+
					"add s.wrap(\"archigraph_xxx\", s.%s) in registerTools, or "+
					"add %q to sharedHelpers if it is a shared utility (not a direct handler), "+
					"or add %q to orphanedHandlers if its tool was deliberately dropped",
					u.file, u.line, u.funcName, u.argKey, u.funcName, u.funcName, u.funcName)
				failures++
			}
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

// scanArgUsages walks all *.go files in dir (non-test files only) and returns
// every argInt/argString/argBool/argFloat call site found inside any non-helper function.
// Both known handlers and unknown ones are returned — the caller differentiates them.
func scanArgUsages(t *testing.T, dir string) []handlerArgUsage {
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

		usages = append(usages, extractArgUsages(fset, f)...)
	}

	return usages
}

// extractArgUsages walks a single parsed file and returns all argXxx call sites
// found inside any non-shared-helper function. The caller is responsible for
// deciding whether the enclosing function is a registered handler.
// It builds a position→funcName map via an outer FuncDecl walk.
func extractArgUsages(fset *token.FileSet, f *ast.File) []handlerArgUsage {
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
