package javascript_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/javascript"
	"github.com/cajasmota/grafel/internal/types"
)

// extractAtPath runs the JS/TS extractor for content at a specific repo-relative
// path (Issue #1616 — qualified_name derivation depends on the file path).
func extractAtPath(t *testing.T, content []byte, language, path string) []types.EntityRecord {
	t.Helper()
	var tree = parseJS(t, content)
	if language == "typescript" {
		tree = parseTS(t, content)
	}
	e := javascript.New()
	got, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Content:  content,
		Language: language,
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return got
}

// TestIssue1616_QualifiedNamePopulated verifies that function, arrow, class,
// interface, and type-alias entities carry a module-path-qualified
// QualifiedName ("<dotted-module>.<name>") instead of an empty string.
func TestIssue1616_QualifiedNamePopulated(t *testing.T) {
	src := []byte(`
export function createOrder(x) { return x; }
export const handleClick = () => 42;
export class Widget {}
`)
	ents := extractAtPath(t, src, "javascript", "src/orders/handlers.js")

	wantQN := map[string]string{
		"createOrder": "src.orders.handlers.createOrder",
		"handleClick": "src.orders.handlers.handleClick",
		"Widget":      "src.orders.handlers.Widget",
	}
	for name, qn := range wantQN {
		e := findByName(ents, name)
		if e == nil {
			t.Fatalf("entity %q not emitted", name)
		}
		if e.QualifiedName != qn {
			t.Errorf("%q QualifiedName = %q, want %q", name, e.QualifiedName, qn)
		}
	}
}

func TestIssue1616_QualifiedNameTypeScript(t *testing.T) {
	src := []byte(`
export interface User { id: number }
export type ID = string | number;
`)
	ents := extractAtPath(t, src, "typescript", "src/models/user.ts")
	for name, qn := range map[string]string{
		"User": "src.models.user.User",
		"ID":   "src.models.user.ID",
	} {
		e := findByName(ents, name)
		if e == nil {
			t.Fatalf("entity %q not emitted", name)
		}
		if e.QualifiedName != qn {
			t.Errorf("%q QualifiedName = %q, want %q", name, e.QualifiedName, qn)
		}
	}
}

// TestIssue1616_DestructureLineAttribution verifies that each binding in a
// multi-line object-destructure is attributed to ITS OWN declaration line,
// not the single line of the RHS expression (the authReducer.js line-420
// cluster bug).
func TestIssue1616_DestructureLineAttribution(t *testing.T) {
	src := []byte(`const slice = { actions: {} };
export const {
    setToken,
    getToken,
    destroyToken,
} = slice.actions;
`)
	ents := extractAtPath(t, src, "javascript", "src/stores/authReducer.js")

	want := map[string]int{
		"setToken":     3,
		"getToken":     4,
		"destroyToken": 5,
	}
	seenLines := map[int]bool{}
	for name, line := range want {
		e := findByName(ents, name)
		if e == nil {
			t.Fatalf("destructured binding %q not emitted", name)
		}
		if e.StartLine != line {
			t.Errorf("%q StartLine = %d, want %d", name, e.StartLine, line)
		}
		seenLines[e.StartLine] = true
	}
	if len(seenLines) < 3 {
		t.Errorf("expected 3 distinct start lines, got %d (bindings still pinned to one line)", len(seenLines))
	}
}

// TestIssue1616_NoBuiltinMethodCalls verifies that calls to JS built-in
// array/string prototype methods (.map/.filter/.trim/.join/.some/...) do NOT
// produce CALLS edges to a bare built-in name (which downstream synthesises
// spurious Process flow steps like `Login -> map`).
func TestIssue1616_NoBuiltinMethodCalls(t *testing.T) {
	src := []byte(`
export function Login(items) {
  const a = items.map(x => x);
  const b = a.filter(Boolean);
  const c = "  hi  ".trim();
  const d = [1, 2].join(",");
  const e = items.some(Boolean);
  doRealWork(a);
  return c + d + e + b;
}
`)
	ents := extractAtPath(t, src, "javascript", "src/auth/Login.jsx")
	login := findByName(ents, "Login")
	if login == nil {
		t.Fatal("Login entity not emitted")
	}
	builtins := map[string]bool{
		"map": true, "filter": true, "trim": true, "join": true, "some": true,
	}
	sawReal := false
	for _, r := range login.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		if builtins[r.ToID] {
			t.Errorf("CALLS edge to built-in method %q should be filtered", r.ToID)
		}
		if r.ToID == "doRealWork" {
			sawReal = true
		}
	}
	if !sawReal {
		t.Error("expected the user-defined call doRealWork to still produce a CALLS edge")
	}
}
