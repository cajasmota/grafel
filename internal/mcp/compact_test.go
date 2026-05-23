// compact_test.go — #1663 compact serializer tests; #1672 TOON wire helpers; #1737 find TOON.
package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestCompactJSON_Minified verifies the output has no indentation whitespace
// and round-trips back to the same shape.
func TestCompactJSON_Minified(t *testing.T) {
	v := map[string]any{
		"id":          "abc",
		"source_file": "foo/bar.go",
		"start_line":  42,
		"nested":      map[string]any{"k": "v"},
	}
	got := compactJSON(v)
	if strings.Contains(got, "  ") || strings.Contains(got, "\n") {
		t.Errorf("compactJSON should not contain pretty whitespace: %q", got)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("round-trip failed: %v", err)
	}
	if back["id"] != "abc" || back["source_file"] != "foo/bar.go" {
		t.Errorf("schema lost on round-trip: %v", back)
	}
}

// TestJSONResult_NoIndent verifies the public helper now emits minified JSON.
func TestJSONResult_NoIndent(t *testing.T) {
	res := jsonResult(map[string]any{"foo": "bar", "baz": []int{1, 2, 3}})
	if res == nil || len(res.Content) == 0 {
		t.Fatal("nil result")
	}
	// Inspect the text content.
	type texter interface{ GetText() string }
	var text string
	for _, c := range res.Content {
		// mcpapi.TextContent has a Text field; rely on JSON-marshalling the
		// content to read it portably here.
		data, err := json.Marshal(c)
		if err != nil {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(data, &obj); err == nil {
			if t, ok := obj["text"].(string); ok {
				text = t
				break
			}
		}
	}
	if text == "" {
		t.Fatal("could not read text content")
	}
	if strings.Contains(text, "\n  ") {
		t.Errorf("jsonResult emitted indented JSON: %q", text)
	}
}

// TestTabularEncode_Shape verifies the schema/row format and escaping.
func TestTabularEncode_Shape(t *testing.T) {
	got := tabularEncode(
		[]string{"id", "kind", "file", "line"},
		[][]any{
			{"abc", "View", "routers.py", 12},
			{"def", "Method", "view.py", 40},
			{"weird,name", "Class", "a\\b.py", 1},
		},
	)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 lines (1 schema + 3 rows), got %d: %q", len(lines), got)
	}
	if lines[0] != "[!schema {id,kind,file,line}]" {
		t.Errorf("schema line wrong: %q", lines[0])
	}
	if lines[1] != "{abc,View,routers.py,12}" {
		t.Errorf("row 1 wrong: %q", lines[1])
	}
	// Escaped comma and backslash.
	if !strings.Contains(lines[3], `weird\,name`) {
		t.Errorf("comma should be escaped: %q", lines[3])
	}
	if !strings.Contains(lines[3], `a\\b.py`) {
		t.Errorf("backslash should be escaped: %q", lines[3])
	}
}

// TestTabularEncode_SavesTokensVsJSON spot-checks that for list-of-record
// payloads, the tabular form is meaningfully shorter than the equivalent
// minified JSON array.
func TestTabularEncode_SavesTokensVsJSON(t *testing.T) {
	rows := make([][]any, 50)
	for i := range rows {
		rows[i] = []any{"id_" + strings.Repeat("x", 4), "Method", "path/to/file.go", 100 + i}
	}
	tab := tabularEncode([]string{"id", "kind", "file", "line"}, rows)

	// Equivalent JSON: array of objects.
	arr := make([]map[string]any, len(rows))
	for i, r := range rows {
		arr[i] = map[string]any{"id": r[0], "kind": r[1], "file": r[2], "line": r[3]}
	}
	data, _ := json.Marshal(arr)
	if len(tab) >= len(data) {
		t.Errorf("tabular (%d) should be shorter than JSON array (%d) for list-of-record",
			len(tab), len(data))
	}
}

// ---------------------------------------------------------------------------
// #1672 — recordsToTOON helper tests
// ---------------------------------------------------------------------------

// TestRecordsToTOON_HomogeneousConverts verifies homogeneous record arrays are
// converted to TOON with a sorted schema line and one row per record.
func TestRecordsToTOON_HomogeneousConverts(t *testing.T) {
	// Simulate what json.Unmarshal produces for []any of map[string]any.
	input := []any{
		map[string]any{"id": "e1", "name": "OrderService", "repo": "svc"},
		map[string]any{"id": "e2", "name": "UserService", "repo": "svc"},
	}
	got, ok := recordsToTOON(input)
	if !ok {
		t.Fatal("expected recordsToTOON to return ok=true for homogeneous input")
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	// 1 schema line + 2 rows
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), got)
	}
	// Keys sorted: id, name, repo
	if lines[0] != "[!schema {id,name,repo}]" {
		t.Errorf("schema line wrong: %q", lines[0])
	}
	// Both rows present (exact order is deterministic via sorted keys).
	if !strings.Contains(got, "e1") || !strings.Contains(got, "OrderService") {
		t.Errorf("missing row 1 data: %q", got)
	}
	if !strings.Contains(got, "e2") || !strings.Contains(got, "UserService") {
		t.Errorf("missing row 2 data: %q", got)
	}
}

// TestRecordsToTOON_HeterogeneousReturnsFalse verifies that arrays with
// mismatched key sets across elements return ok=false.
func TestRecordsToTOON_HeterogeneousReturnsFalse(t *testing.T) {
	input := []any{
		map[string]any{"id": "e1", "name": "fn1"},
		map[string]any{"id": "e2", "name": "fn2", "extra": "x"},
	}
	_, ok := recordsToTOON(input)
	if ok {
		t.Error("expected recordsToTOON to return ok=false for heterogeneous schema")
	}
}

// TestRecordsToTOON_EmptyReturnsFalse verifies empty slices return ok=false.
func TestRecordsToTOON_EmptyReturnsFalse(t *testing.T) {
	_, ok := recordsToTOON(nil)
	if ok {
		t.Error("expected ok=false for nil input")
	}
	_, ok2 := recordsToTOON([]any{})
	if ok2 {
		t.Error("expected ok=false for empty slice")
	}
}

// TestRecordsToTOON_NonObjectElementReturnsFalse verifies that a slice
// containing non-map elements is not TOON-encoded.
func TestRecordsToTOON_NonObjectElementReturnsFalse(t *testing.T) {
	input := []any{"just a string", "another"}
	_, ok := recordsToTOON(input)
	if ok {
		t.Error("expected ok=false when elements are not map[string]any")
	}
}

// TestRecordsToTOON_TokenSavings verifies that TOON text is shorter than the
// minified JSON for the same data — confirming actual token savings on the wire.
func TestRecordsToTOON_TokenSavings(t *testing.T) {
	const n = 40
	input := make([]any, n)
	for i := range input {
		input[i] = map[string]any{
			"entity_id":   "repo1::abcdef1234567890",
			"name":        "POST /api/v2/orders",
			"kind":        "http_endpoint_definition",
			"repo":        "orders-service",
			"source_file": "internal/handlers/orders.go",
			"start_line":  float64(42 + i),
			"method":      "POST",
			"path":        "/api/v2/orders",
		}
	}
	toonText, ok := recordsToTOON(input)
	if !ok {
		t.Fatal("expected homogeneous input to convert")
	}
	jsonData, _ := json.Marshal(input)

	toonTokens := estimateTokens(toonText)
	jsonTokens := estimateTokens(string(jsonData))
	savings := float64(jsonTokens-toonTokens) / float64(jsonTokens) * 100
	if toonTokens >= jsonTokens {
		t.Errorf("TOON (%d tokens) should be fewer than JSON (%d tokens)", toonTokens, jsonTokens)
	}
	// Expect at least 30% savings for this representative endpoint payload.
	if savings < 30 {
		t.Errorf("expected ≥30%% token savings, got %.1f%% (TOON=%d, JSON=%d)",
			savings, toonTokens, jsonTokens)
	}
	t.Logf("Token savings: %.1f%% (TOON=%d vs JSON=%d for %d endpoint records)",
		savings, toonTokens, jsonTokens, n)
}

// ---------------------------------------------------------------------------
// #1737 — hitsToTOON + renderCompact TOON path tests
// ---------------------------------------------------------------------------

// makeTestNode constructs a nodeWithRepo for testing without needing a loaded repo.
func makeTestNode(repo, name, kind, file string, line int, score float64) nodeWithRepo {
	return nodeWithRepo{
		Repo:  repo,
		Score: score,
		Entity: &graph.Entity{
			ID:         name,
			Name:       name,
			Kind:       kind,
			SourceFile: file,
			StartLine:  line,
		},
	}
}

// TestHitsToTOON_OneRepoSchema verifies the schema line and row format for
// single-repo mode (no repo column).
func TestHitsToTOON_OneRepoSchema(t *testing.T) {
	nodes := []nodeWithRepo{
		makeTestNode("auth-svc", "Login", "operation", "src/auth.py", 42, 8.32),
		makeTestNode("auth-svc", "LoginViewSet", "view", "core/views/auth.py", 22, 11.78),
	}
	got := hitsToTOON(nodes, true /* oneRepo */)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines (schema + 2 rows), got %d:\n%s", len(lines), got)
	}
	if lines[0] != "[!schema {name,kind,file,line,score}]" {
		t.Errorf("wrong schema line: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "{Login,") {
		t.Errorf("row 1 should start with {Login,: %q", lines[1])
	}
	if !strings.Contains(lines[1], "8.32") {
		t.Errorf("row 1 should contain score 8.32: %q", lines[1])
	}
	if !strings.Contains(lines[2], "LoginViewSet") {
		t.Errorf("row 2 should contain LoginViewSet: %q", lines[2])
	}
}

// TestHitsToTOON_MultiRepoIncludesRepoColumn verifies repo is prepended as the
// first column when oneRepo is false.
func TestHitsToTOON_MultiRepoIncludesRepoColumn(t *testing.T) {
	nodes := []nodeWithRepo{
		makeTestNode("repo-a", "OrderService", "class", "svc/orders.go", 10, 5.5),
	}
	got := hitsToTOON(nodes, false /* multi-repo */)
	if !strings.HasPrefix(got, "[!schema {repo,name,kind,file,line,score}]") {
		t.Errorf("expected repo as first schema column: %q", got)
	}
	if !strings.Contains(got, "repo-a") {
		t.Errorf("expected repo name in row: %q", got)
	}
}

// TestHitsToTOON_StripsScopePrefix verifies that SCOPE. prefixes are stripped
// from the kind column in TOON output.
func TestHitsToTOON_StripsScopePrefix(t *testing.T) {
	nodes := []nodeWithRepo{
		makeTestNode("r", "Dashboard", "SCOPE.Component", "src/Dashboard.tsx", 1, 1.0),
	}
	got := hitsToTOON(nodes, true)
	if strings.Contains(got, "SCOPE.") {
		t.Errorf("SCOPE. prefix should be stripped in TOON output: %q", got)
	}
	if !strings.Contains(got, "Component") {
		t.Errorf("stripped kind 'Component' should appear in output: %q", got)
	}
}

// TestHitsToTOON_Empty verifies the empty-input case returns an empty string.
func TestHitsToTOON_Empty(t *testing.T) {
	if got := hitsToTOON(nil, true); got != "" {
		t.Errorf("expected empty string for nil input, got %q", got)
	}
	if got := hitsToTOON([]nodeWithRepo{}, true); got != "" {
		t.Errorf("expected empty string for empty input, got %q", got)
	}
}

// TestRenderCompact_TOONPath verifies that renderCompact emits a TOON table for
// the nodes section when toonWireEnabled() is true (default env).
func TestRenderCompact_TOONPath(t *testing.T) {
	// Default env: MCP_WIRE_FORMAT unset, MCP_FIND_FORMAT unset → TOON active.
	t.Setenv("MCP_WIRE_FORMAT", "")
	t.Setenv("MCP_FIND_FORMAT", "")

	rr := renderResult{
		MatchedTotal: 2,
		OneRepo:      true,
		Nodes: []nodeWithRepo{
			makeTestNode("r1", "AuthMiddleware", "function", "src/middleware.py", 55, 9.1),
			makeTestNode("r1", "JWTValidator", "class", "src/jwt.py", 12, 7.3),
		},
	}
	got := renderCompact(rr, 0)

	// Prose header must be present.
	if !strings.HasPrefix(got, "# nodes (2 matched)") {
		t.Errorf("expected markdown header, got: %q", got)
	}
	// TOON schema line must be present.
	if !strings.Contains(got, "[!schema {name,kind,file,line,score}]") {
		t.Errorf("expected TOON schema line in output:\n%s", got)
	}
	// Both entity names must appear as rows.
	if !strings.Contains(got, "AuthMiddleware") {
		t.Errorf("AuthMiddleware missing from TOON output:\n%s", got)
	}
	if !strings.Contains(got, "JWTValidator") {
		t.Errorf("JWTValidator missing from TOON output:\n%s", got)
	}
	// Must NOT contain the old markdown line format "AuthMiddleware  src/…".
	if strings.Contains(got, "AuthMiddleware  src/") {
		t.Errorf("old markdown row format should not appear in TOON path:\n%s", got)
	}
}

// TestRenderCompact_MarkdownFallback verifies MCP_FIND_FORMAT=markdown restores
// the legacy text shape (no TOON schema line).
func TestRenderCompact_MarkdownFallback(t *testing.T) {
	t.Setenv("MCP_FIND_FORMAT", "markdown")

	rr := renderResult{
		MatchedTotal: 1,
		OneRepo:      true,
		Nodes: []nodeWithRepo{
			makeTestNode("r1", "AuthMiddleware", "function", "src/middleware.py", 55, 9.1),
		},
	}
	got := renderCompact(rr, 0)

	if strings.Contains(got, "[!schema") {
		t.Errorf("TOON schema should not appear when MCP_FIND_FORMAT=markdown:\n%s", got)
	}
	if !strings.Contains(got, "AuthMiddleware") {
		t.Errorf("entity name missing from markdown fallback output:\n%s", got)
	}
	// Legacy format: "Name  file:line"
	if !strings.Contains(got, "AuthMiddleware  src/middleware.py:55") {
		t.Errorf("expected legacy 'Name  file:line' format:\n%s", got)
	}
}

// TestRenderCompact_TOONTokenSavings verifies that for a representative find
// result the TOON-encoded hits section is shorter than the equivalent JSON-array
// encoding of the same {name,kind,file,line,score} fields — confirming the
// tabular format pays off once all five fields are present (#1737).
//
// The comparison is against JSON because the alternative to TOON+kind+score is
// not the stripped "Name  file:line" markdown (which omits kind/score entirely)
// but the richer JSON payload that a client would need to parse those fields.
func TestRenderCompact_TOONTokenSavings(t *testing.T) {
	t.Setenv("MCP_WIRE_FORMAT", "")
	t.Setenv("MCP_FIND_FORMAT", "")

	// Realistic varied names/paths that mirror actual archigraph_find output.
	fixtures := []struct {
		name, kind, file string
		line             int
		score            float64
	}{
		{"AuthMiddleware", "function", "src/middleware/auth.py", 42, 9.1},
		{"JWTValidator", "class", "src/auth/jwt_validator.py", 10, 7.3},
		{"LoginViewSet", "view", "core/views/auth.py", 22, 6.8},
		{"TokenRefreshView", "view", "core/views/token.py", 55, 6.1},
		{"PermissionCheck", "function", "src/auth/permissions.py", 88, 5.5},
		{"SessionManager", "class", "src/sessions/manager.py", 14, 5.0},
		{"OAuthCallbackHandler", "function", "src/oauth/callbacks.py", 71, 4.8},
		{"UserAuthSerializer", "class", "core/serializers/auth.py", 33, 4.6},
		{"AuthenticationBackend", "class", "src/backends/auth.py", 19, 4.4},
		{"verify_token", "function", "src/auth/tokens.py", 120, 4.0},
		{"decode_jwt_claims", "function", "src/auth/jwt.py", 204, 3.8},
		{"RequireAuthDecorator", "function", "src/decorators/auth.py", 6, 3.5},
		{"GroupPermissions", "class", "src/rbac/permissions.py", 44, 3.3},
		{"AccessControlList", "class", "src/rbac/acl.py", 88, 3.0},
		{"authenticate_user", "function", "core/auth/authenticate.py", 31, 2.8},
		{"TwoFactorAuth", "class", "src/auth/two_factor.py", 17, 2.6},
		{"OTPHandler", "function", "src/auth/otp.py", 93, 2.4},
		{"SSOMiddleware", "class", "src/sso/middleware.py", 5, 2.2},
		{"ldap_authenticate", "function", "src/ldap/auth.py", 66, 2.0},
		{"UserCredentials", "class", "src/models/credentials.py", 25, 1.9},
		{"reset_password_view", "function", "core/views/password.py", 49, 1.7},
		{"PasswordResetSerializer", "class", "core/serializers/password.py", 12, 1.5},
		{"check_login_rate_limit", "function", "src/ratelimit/login.py", 38, 1.3},
		{"ApiKeyMiddleware", "class", "src/middleware/apikey.py", 77, 1.1},
		{"auth_signal_handler", "function", "src/signals/auth.py", 102, 0.9},
	}

	nodes := make([]nodeWithRepo, len(fixtures))
	for i, f := range fixtures {
		nodes[i] = makeTestNode("auth-service", f.name, f.kind, f.file, f.line, f.score)
	}

	rr := renderResult{MatchedTotal: len(nodes), OneRepo: true, Nodes: nodes}
	toonOut := renderCompact(rr, 0)

	// Build the equivalent JSON array of the same 5 fields as the JSON baseline.
	type jsonHit struct {
		Name  string  `json:"name"`
		Kind  string  `json:"kind"`
		File  string  `json:"file"`
		Line  int     `json:"line"`
		Score float64 `json:"score"`
	}
	jsonArr := make([]jsonHit, len(fixtures))
	for i, f := range fixtures {
		jsonArr[i] = jsonHit{f.name, f.kind, f.file, f.line, f.score}
	}
	jsonBytes, _ := json.Marshal(jsonArr)

	toonTokens := estimateTokens(toonOut)
	jsonTokens := estimateTokens(string(jsonBytes))
	savings := float64(jsonTokens-toonTokens) / float64(jsonTokens) * 100

	t.Logf("TOON vs JSON: %d vs %d tokens (%.1f%% savings) for %d nodes", toonTokens, jsonTokens, savings, len(fixtures))
	if toonTokens >= jsonTokens {
		t.Errorf("TOON (%d tokens) should be fewer than JSON (%d tokens) for same-field payload", toonTokens, jsonTokens)
	}
	if savings < 20 {
		t.Errorf("expected ≥20%% token savings vs JSON, got %.1f%%", savings)
	}
}
