package mcp

// id_interning_test.go — #1740 Token sprint Tier-2.1: entity-ID interning tests.

import (
	"encoding/json"
	"strings"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// makeTextResult wraps a string in a CallToolResult with one TextContent.
func makeTextResult(text string) *mcpapi.CallToolResult {
	return &mcpapi.CallToolResult{
		Content: []mcpapi.Content{mcpapi.NewTextContent(text)},
	}
}

// ---------------------------------------------------------------------------
// countIDOccurrences
// ---------------------------------------------------------------------------

func TestCountIDOccurrences_PrefixedOnly(t *testing.T) {
	// Only the prefixed form <repo>::<hex> is interned; bare hex IDs are NOT.
	text := `{"id":"upvate-core::c84f9b9c0c3a7b18","callers":[{"id":"upvate-core::c84f9b9c0c3a7b18"},{"id":"1234567890abcdef"},{"id":"1234567890abcdef"}]}`
	counts := countIDOccurrences(text)

	if counts["upvate-core::c84f9b9c0c3a7b18"] != 2 {
		t.Errorf("expected 2 for prefixed id, got %d", counts["upvate-core::c84f9b9c0c3a7b18"])
	}
	// Bare hex IDs are intentionally NOT matched to avoid round-trip breakage.
	if counts["1234567890abcdef"] != 0 {
		t.Errorf("bare hex IDs should not be counted (only prefixed form is interned), got %d", counts["1234567890abcdef"])
	}
}

func TestCountIDOccurrences_SingleOccurrence(t *testing.T) {
	text := `{"id":"repo::aabbccddeeff0011"}`
	counts := countIDOccurrences(text)
	if counts["repo::aabbccddeeff0011"] != 1 {
		t.Errorf("expected 1 occurrence, got %d", counts["repo::aabbccddeeff0011"])
	}
}

// ---------------------------------------------------------------------------
// substituteIDs
// ---------------------------------------------------------------------------

func TestSubstituteIDs_ReplacesQualifyingIDs(t *testing.T) {
	table := map[string]string{
		"repo-a::c84f9b9c0c3a7b18": "@1",
		"repo-b::deadbeef01234567": "@2",
	}
	text := `{"from":"repo-a::c84f9b9c0c3a7b18","to":"repo-b::deadbeef01234567","also":"repo-a::c84f9b9c0c3a7b18"}`
	got := substituteIDs(text, table)
	if !strings.Contains(got, `"@1"`) {
		t.Errorf("expected @1 in substituted text: %s", got)
	}
	if !strings.Contains(got, `"@2"`) {
		t.Errorf("expected @2 in substituted text: %s", got)
	}
	if strings.Contains(got, "c84f9b9c0c3a7b18") {
		t.Errorf("original ID should be replaced: %s", got)
	}
}

func TestSubstituteIDs_LeavesNonQualifyingIDsAlone(t *testing.T) {
	// singletonID appears only once so it is not in the table.
	table := map[string]string{
		"repo::c84f9b9c0c3a7b18": "@1",
	}
	text := `{"a":"repo::c84f9b9c0c3a7b18","b":"repo::aabbccddeeff0011"}`
	got := substituteIDs(text, table)
	if !strings.Contains(got, `"@1"`) {
		t.Errorf("expected @1: %s", got)
	}
	if !strings.Contains(got, "aabbccddeeff0011") {
		t.Errorf("singleton ID should remain unchanged: %s", got)
	}
}

// ---------------------------------------------------------------------------
// applyIDInterning — opt-out
// ---------------------------------------------------------------------------

func TestApplyIDInterning_OptOut(t *testing.T) {
	t.Setenv("MCP_NO_ID_INTERNING", "1")
	payload := `{"id":"repo::c84f9b9c0c3a7b18","callers":[{"id":"repo::c84f9b9c0c3a7b18"},{"id":"repo::c84f9b9c0c3a7b18"}]}`
	res := makeTextResult(payload)
	out := applyIDInterning(res)
	got := resultText(out)
	if strings.Contains(got, "@1") {
		t.Errorf("interning should be skipped when MCP_NO_ID_INTERNING=1, got: %s", got)
	}
	if !strings.Contains(got, "c84f9b9c0c3a7b18") {
		t.Errorf("original IDs should be preserved on opt-out: %s", got)
	}
}

// ---------------------------------------------------------------------------
// applyIDInterning — single-occurrence pass-through
// ---------------------------------------------------------------------------

func TestApplyIDInterning_SingleOccurrence_NoTable(t *testing.T) {
	t.Setenv("MCP_NO_ID_INTERNING", "")
	// Each ID appears exactly once — no interning should happen.
	payload := `{"a":"repo::c84f9b9c0c3a7b18","b":"repo::deadbeef01234567"}`
	res := makeTextResult(payload)
	out := applyIDInterning(res)
	got := resultText(out)
	if strings.Contains(got, "_id_table") {
		t.Errorf("no _id_table expected when no ID repeats: %s", got)
	}
	if strings.Contains(got, "@1") {
		t.Errorf("no handles expected when no ID repeats: %s", got)
	}
}

// ---------------------------------------------------------------------------
// applyIDInterning — core: intern + inject table + reversible
// ---------------------------------------------------------------------------

func TestApplyIDInterning_InternAndTableInjected(t *testing.T) {
	t.Setenv("MCP_NO_ID_INTERNING", "")
	id1 := "upvate-core::c84f9b9c0c3a7b18"
	id2 := "upvate-core::deadbeef01234567"
	payload := map[string]any{
		"root": id1,
		"callers": []any{
			map[string]any{"id": id1, "name": "funcA"},
			map[string]any{"id": id2, "name": "funcB"},
			map[string]any{"id": id1, "name": "funcC"},
			map[string]any{"id": id2, "name": "funcD"},
		},
		"elapsed_ms": 12,
	}
	data, _ := json.Marshal(payload)
	res := makeTextResult(string(data))
	out := applyIDInterning(res)
	got := resultText(out)

	// _id_table must be present.
	if !strings.Contains(got, "_id_table") {
		t.Fatalf("_id_table missing from interned response: %s", got)
	}

	// Full IDs must only appear inside _id_table, not in the body fields.
	// Parse the object and check that body fields use handles, not raw IDs.
	var checkObj map[string]any
	if err := json.Unmarshal([]byte(got), &checkObj); err != nil {
		t.Fatalf("unmarshal for body-field check: %v", err)
	}
	// "root" field should be a handle, not the raw id.
	if checkObj["root"] == id1 {
		t.Errorf("root field should be a handle, not raw id1: %s", got)
	}
	// callers items should use handles.
	if callers, ok := checkObj["callers"].([]any); ok {
		for _, c := range callers {
			if obj, ok := c.(map[string]any); ok {
				if obj["id"] == id1 || obj["id"] == id2 {
					t.Errorf("caller id field should be a handle, not raw id: %v", obj)
				}
			}
		}
	}

	// Handles @1 and @2 must appear in the output.
	if !strings.Contains(got, `"@1"`) && !strings.Contains(got, `@1`) {
		t.Errorf("handle @1 missing: %s", got)
	}
	if !strings.Contains(got, `"@2"`) && !strings.Contains(got, `@2`) {
		t.Errorf("handle @2 missing: %s", got)
	}

	// Reversibility: parse the _id_table and resolve both handles back to full IDs.
	var obj map[string]any
	if err := json.Unmarshal([]byte(got), &obj); err != nil {
		t.Fatalf("unmarshal interned response: %v", err)
	}
	rawTable, ok := obj["_id_table"]
	if !ok {
		t.Fatal("_id_table key absent in parsed object")
	}
	tableMap, ok := rawTable.(map[string]any)
	if !ok {
		t.Fatalf("_id_table is not a map: %T", rawTable)
	}

	// Find which handle maps to id1 and id2.
	var h1, h2 string
	for handle, full := range tableMap {
		switch full {
		case id1:
			h1 = handle
		case id2:
			h2 = handle
		}
	}
	if h1 == "" {
		t.Errorf("id1 not found in _id_table: %v", tableMap)
	}
	if h2 == "" {
		t.Errorf("id2 not found in _id_table: %v", tableMap)
	}

	// Both handles must be resolvable (reversibility assertion).
	if tableMap[h1] != id1 {
		t.Errorf("handle %s does not resolve to %s: got %v", h1, id1, tableMap[h1])
	}
	if tableMap[h2] != id2 {
		t.Errorf("handle %s does not resolve to %s: got %v", h2, id2, tableMap[h2])
	}
}

// ---------------------------------------------------------------------------
// applyIDInterning — handles are assigned in first-appearance order
// ---------------------------------------------------------------------------

func TestApplyIDInterning_FirstAppearanceOrder(t *testing.T) {
	t.Setenv("MCP_NO_ID_INTERNING", "")
	// id1 appears first, id2 second.
	id1 := "svc-a::aaaa1111bbbb2222"
	id2 := "svc-b::cccc3333dddd4444"
	payload := `{"first":"` + id1 + `","second":"` + id2 + `","repeat_first":"` + id1 + `","repeat_second":"` + id2 + `"}`
	res := makeTextResult(payload)
	out := applyIDInterning(res)
	got := resultText(out)

	var obj map[string]any
	if err := json.Unmarshal([]byte(got), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	table, _ := obj["_id_table"].(map[string]any)
	if table == nil {
		t.Fatal("_id_table missing")
	}
	// @1 must resolve to id1 (first appearance), @2 to id2.
	if table["@1"] != id1 {
		t.Errorf("@1 should be %s, got %v", id1, table["@1"])
	}
	if table["@2"] != id2 {
		t.Errorf("@2 should be %s, got %v", id2, table["@2"])
	}
}

// ---------------------------------------------------------------------------
// applyIDInterning — elapsed_ms field is preserved after interning
// ---------------------------------------------------------------------------

func TestApplyIDInterning_ElapsedMSPreserved(t *testing.T) {
	t.Setenv("MCP_NO_ID_INTERNING", "")
	id1 := "repo::ffeeddccbbaa9988"
	payload := map[string]any{
		"id":         id1,
		"callers":    []any{map[string]any{"id": id1}},
		"elapsed_ms": int64(42),
	}
	data, _ := json.Marshal(payload)
	res := makeTextResult(string(data))
	out := applyIDInterning(res)
	got := resultText(out)

	var obj map[string]any
	if err := json.Unmarshal([]byte(got), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["elapsed_ms"] == nil {
		t.Errorf("elapsed_ms should be preserved after interning: %s", got)
	}
}

// ---------------------------------------------------------------------------
// applyIDInterning — nil / empty result safety
// ---------------------------------------------------------------------------

func TestApplyIDInterning_NilResult(t *testing.T) {
	if applyIDInterning(nil) != nil {
		t.Error("nil in, nil out")
	}
}

func TestApplyIDInterning_EmptyContent(t *testing.T) {
	res := &mcpapi.CallToolResult{}
	out := applyIDInterning(res)
	if out != res {
		t.Error("empty content: should return same pointer")
	}
}

// ---------------------------------------------------------------------------
// applyIDInterning — wire-bytes reduction smoke test
// ---------------------------------------------------------------------------

// TestApplyIDInterning_ByteReduction verifies that for a graph-heavy expand
// response (many repeated IDs) the interned output is meaningfully shorter
// than the original.
func TestApplyIDInterning_ByteReduction(t *testing.T) {
	t.Setenv("MCP_NO_ID_INTERNING", "")
	const baseID = "upvate-core::c84f9b9c0c3a7b18"
	const callerID = "upvate-core::deadbeef01234567"
	const calleeID = "upvate-core::aabbccdd11223344"

	// Simulate an expand depth=2 response: root node appears everywhere.
	nodes := make([]any, 50)
	for i := range nodes {
		var callee string
		if i%2 == 0 {
			callee = callerID
		} else {
			callee = calleeID
		}
		nodes[i] = map[string]any{
			"id":     baseID,
			"caller": callerID,
			"callee": callee,
			"name":   "SomeFunction",
		}
	}
	payload := map[string]any{
		"root":       baseID,
		"nodes":      nodes,
		"elapsed_ms": 33,
	}
	data, _ := json.Marshal(payload)
	before := len(data)

	res := makeTextResult(string(data))
	out := applyIDInterning(res)
	after := len(resultText(out))

	savingPct := float64(before-after) / float64(before) * 100
	t.Logf("ID interning: before=%d bytes, after=%d bytes, saving=%.1f%%", before, after, savingPct)

	if after >= before {
		t.Errorf("expected interned response to be smaller: before=%d after=%d", before, after)
	}
	// For this graph-heavy fixture we expect ≥15% savings.
	if savingPct < 15 {
		t.Errorf("expected ≥15%% savings, got %.1f%%", savingPct)
	}
}

// ---------------------------------------------------------------------------
// idPattern — regex correctness checks
// ---------------------------------------------------------------------------

func TestIDPattern_MatchesPrefixedForm(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{`"upvate-core::c84f9b9c0c3a7b18"`, true},
		{`"svc.name::aabbccddeeff0011"`, true},
		{`"my-repo_v2::1234567890abcdef"`, true},
		{`"notanid::ZZZZZZZZZZZZZZZZ"`, false}, // uppercase hex is not matched
		{`"short::abc"`, false},                // too short
	}
	for _, c := range cases {
		m := idPattern.FindString(c.input)
		got := m != ""
		if got != c.want {
			t.Errorf("idPattern.Match(%q) = %v, want %v (matched: %q)", c.input, got, c.want, m)
		}
	}
}

// TestIDPattern_BareHexNotMatched verifies that bare 16-hex strings (without
// the <repo>:: prefix) are NOT matched by idPattern. This is intentional:
// bare hex IDs are local entity IDs used as primary keys in responses (e.g.
// pattern IDs, edge IDs) and must not be substituted to preserve round-trips.
func TestIDPattern_BareHexNotMatched(t *testing.T) {
	cases := []string{
		`"c84f9b9c0c3a7b18"`,
		`"1234567890abcdef"`,
		`"c84f9b9c0c3a7b1"`,
		`"c84f9b9c0c3a7b189"`,
	}
	for _, input := range cases {
		matches := idPattern.FindAllString(input, -1)
		// None of the bare-hex strings should be matched.
		for _, m := range matches {
			if !strings.Contains(m, "::") {
				t.Errorf("bare hex should NOT be matched: input=%q matched=%q", input, m)
			}
		}
	}
}
