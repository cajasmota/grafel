package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/links"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// analysis_merges_test.go — dispatch tests for the ANALYSIS-cluster canonical
// tools (#5546/#5550). Each test asserts that a discriminator value on the new
// canonical handler produces the same output as the absorbed handler it routes
// to (order-insensitive via normalizeForCompare, defined in core_merges_test.go).
// Helpers coreTestServer / callBare / assertSameDispatch are shared from there.

// 1. grafel_debt kind= → dead_code/find_dead_code/cycles/import_cycles/stubs/impure/license.
func TestAnalysisDebtDispatch(t *testing.T) {
	srv := coreTestServer(t)
	g := map[string]any{"group": "g"}
	with := func(kind string) map[string]any {
		return map[string]any{"group": "g", "kind": kind}
	}
	stub := map[string]any{"group_v3": "g", "group_oracle": "g"}
	stubKind := map[string]any{"group_v3": "g", "group_oracle": "g", "kind": "stubs"}
	assertSameDispatch(t, "kind=dead_code", srv.handleAnalysisDebt, with("dead_code"), srv.handleDeadCode, g)
	assertSameDispatch(t, "kind=default", srv.handleAnalysisDebt, g, srv.handleDeadCode, g)
	assertSameDispatch(t, "kind=find_dead_code", srv.handleAnalysisDebt, with("find_dead_code"), srv.handleFindDeadCode, g)
	assertSameDispatch(t, "kind=cycles", srv.handleAnalysisDebt, with("cycles"), srv.handleQualityCycles, g)
	assertSameDispatch(t, "kind=import_cycles", srv.handleAnalysisDebt, with("import_cycles"), srv.handleModuleCyclesSidecar, g)
	assertSameDispatch(t, "kind=stubs", srv.handleAnalysisDebt, stubKind, srv.handleStubDetector, stub)
	assertSameDispatch(t, "kind=impure", srv.handleAnalysisDebt, with("impure"), srv.handlePureFunctions, g)
	assertSameDispatch(t, "kind=license", srv.handleAnalysisDebt, with("license"), srv.handleLicenseAudit, g)
}

// 2. grafel_security kind= → findings/secrets/auth_coverage.
func TestAnalysisSecurityDispatch(t *testing.T) {
	srv := coreTestServer(t)
	g := map[string]any{"group": "g"}
	with := func(kind string) map[string]any {
		return map[string]any{"group": "g", "kind": kind}
	}
	assertSameDispatch(t, "kind=findings", srv.handleAnalysisSecurity, with("findings"), srv.handleSecurityFindings, g)
	assertSameDispatch(t, "kind=default", srv.handleAnalysisSecurity, g, srv.handleSecurityFindings, g)
	assertSameDispatch(t, "kind=secrets", srv.handleAnalysisSecurity, with("secrets"), srv.handleSecrets, g)
	assertSameDispatch(t, "kind=auth_coverage", srv.handleAnalysisSecurity, with("auth_coverage"), srv.handleAuthCoverage, g)
}

// 3. grafel_test_analysis kind= → coverage/reachability/contract_effectiveness/coverage_effectiveness.
func TestAnalysisTestDispatch(t *testing.T) {
	srv := coreTestServer(t)
	g := map[string]any{"group": "g"}
	with := func(kind string) map[string]any {
		return map[string]any{"group": "g", "kind": kind}
	}
	assertSameDispatch(t, "kind=coverage", srv.handleAnalysisTest, with("coverage"), srv.handleTestCoverage, g)
	assertSameDispatch(t, "kind=default", srv.handleAnalysisTest, g, srv.handleTestCoverage, g)
	assertSameDispatch(t, "kind=reachability", srv.handleAnalysisTest, with("reachability"), srv.handleTestReachability, g)
	assertSameDispatch(t, "kind=contract_effectiveness", srv.handleAnalysisTest, with("contract_effectiveness"), srv.handleContractTestEffectiveness, g)
	assertSameDispatch(t, "kind=coverage_effectiveness", srv.handleAnalysisTest, with("coverage_effectiveness"), srv.handleCoverageEffectiveness, g)
}

// 4. grafel_patterns kind= → code (agent store) / graph / template.
func TestAnalysisPatternsDispatch(t *testing.T) {
	srv := coreTestServer(t)
	// code: handlePatterns reads its own action=; query is the read path.
	codeArgs := map[string]any{"group": "g", "action": "query", "text": "x"}
	assertSameDispatch(t, "kind=code", srv.handleAnalysisPatterns,
		map[string]any{"group": "g", "kind": "code", "action": "query", "text": "x"},
		srv.handlePatterns, codeArgs)
	assertSameDispatch(t, "kind=default", srv.handleAnalysisPatterns, codeArgs, srv.handlePatterns, codeArgs)
	// graph: dispatcher defaults action=list.
	assertSameDispatch(t, "kind=graph", srv.handleAnalysisPatterns,
		map[string]any{"group": "g", "kind": "graph"},
		srv.handleGraphPatterns, map[string]any{"group": "g", "action": "list"})
	// template.
	assertSameDispatch(t, "kind=template", srv.handleAnalysisPatterns,
		map[string]any{"group": "g", "kind": "template"},
		srv.handleTemplatePatterns, map[string]any{"group": "g"})
}

// 4b. Regression for #5784 bug 1: grafel_patterns kind=template must NOT
// clobber handleTemplatePatterns's own `kind` literal-type filter (values
// like log_format/sql). Before the fix, the umbrella discriminator value
// "template" is passed straight through as the inner filter, so it never
// matches any real entry and `patterns` comes back empty even though
// `by_kind` shows real data — the exact live symptom from the issue.
func TestAnalysisPatternsTemplateKindNotClobbered(t *testing.T) {
	srv := coreTestServer(t)
	writeTemplatePatternSidecar(t, "g", templatePatternSidecarDoc{
		Version: 1,
		Method:  "test",
		Total:   2,
		ByKind:  map[string]int{"log_format": 1, "sql": 1},
		Entries: []templatePatternSidecarEntry{
			{Repo: "r1", SourceFile: "a.go", Line: 10, Kind: "log_format", Tag: "info", Literal: "starting %s"},
			{Repo: "r1", SourceFile: "b.go", Line: 20, Kind: "sql", Tag: "select", Literal: "SELECT * FROM x"},
		},
	})
	out := callBare(t, srv.handleAnalysisPatterns, map[string]any{"group": "g", "kind": "template"})
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("result not JSON object: %v (%s)", err, out)
	}
	var patterns []any
	if err := json.Unmarshal(obj["patterns"], &patterns); err != nil {
		t.Fatalf("patterns not an array: %v (%s)", err, out)
	}
	if len(patterns) == 0 {
		t.Fatalf("kind=template returned empty patterns despite non-empty by_kind sidecar data: %s", out)
	}
}

// writeTemplatePatternSidecar writes a <group>-links-template-patterns.json
// sidecar under $HOME (via t.Setenv, matching sidecarPath's resolution) so
// handleTemplatePatterns finds real data instead of the "missing" fallback.
func writeTemplatePatternSidecar(t *testing.T, group string, doc templatePatternSidecarDoc) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".grafel", "groups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sidecar dir: %v", err)
	}
	buf, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	path := filepath.Join(dir, group+"-links-template-patterns.json")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

// 5. grafel_findings action= → list / save.
func TestAnalysisFindingsDispatch(t *testing.T) {
	srv := coreTestServer(t)
	g := map[string]any{"group": "g"}
	assertSameDispatch(t, "action=list", srv.handleAnalysisFindings,
		map[string]any{"group": "g", "action": "list"}, srv.handleListFindings, g)
	assertSameDispatch(t, "action=default", srv.handleAnalysisFindings, g, srv.handleListFindings, g)
	// save: handleSaveResult requires question + answer, and returns
	// {"path": "<memDir>/<ts>-<hash>.json"} where <ts> is second-resolution.
	//
	// #6073: rather than normalising <ts> away (which leaves the field checked
	// in NEITHER direction — it passes whether or not the two handlers agree,
	// and fails when they do), we pin the clock so BOTH dispatch paths observe
	// the same instant. The comparison is then exact and total: a genuine
	// divergence in how either handler stamps the timestamp now fails the test,
	// which the normalising version could not detect at all.
	setFixedNowForTest(t, time.Date(2031, 3, 4, 5, 6, 7, 0, time.UTC))
	save := map[string]any{"group": "g", "question": "q", "answer": "a"}
	assertSameSaveDispatch(t, "action=save", srv.handleAnalysisFindings,
		map[string]any{"group": "g", "action": "save", "question": "q", "answer": "a"},
		srv.handleSaveResult, save)
}

// assertSameSaveDispatch is assertSameDispatch specialized for the findings
// save path. It compares the returned {"path": ...} payload BYTE-FOR-BYTE (no
// normalisation — the clock is pinned by the caller, see #6073) and, because
// the save handler's real output is a file on disk rather than the returned
// payload, additionally compares the bytes each call actually wrote. Both
// calls resolve to the same filename under a pinned clock, so the written
// content is read back after each call, before the next one overwrites it.
func assertSameSaveDispatch(t *testing.T, label string,
	canonical func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), canonArgs map[string]any,
	old func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), oldArgs map[string]any) {
	t.Helper()
	got, gotBody := callSaveAndReadBack(t, canonical, canonArgs)
	want, wantBody := callSaveAndReadBack(t, old, oldArgs)
	if got != want {
		t.Errorf("%s: canonical dispatch differs from absorbed handler\n got=%s\nwant=%s", label, got, want)
	}
	if gotBody != wantBody {
		t.Errorf("%s: canonical dispatch wrote different file content than absorbed handler\n got=%s\nwant=%s",
			label, gotBody, wantBody)
	}
}

// callSaveAndReadBack invokes a save handler and returns both its JSON result
// and the bytes it wrote at the reported path.
func callSaveAndReadBack(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error),
	args map[string]any) (result, body string) {
	t.Helper()
	result = callBare(t, fn, args)
	var obj struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(result), &obj); err != nil || obj.Path == "" {
		t.Fatalf("save result is not {\"path\": ...}: %s (err=%v)", result, err)
	}
	buf, err := os.ReadFile(obj.Path)
	if err != nil {
		t.Fatalf("reading back saved finding %s: %v", obj.Path, err)
	}
	return result, string(buf)
}

// TestSaveResultUsesInjectedClock pins the clock and asserts the saved
// filename and the saved_at payload field are BOTH derived from it, in the
// documented formats. This is the coverage the previous normalise-it-away
// fixture removed: without it, nothing checks that the timestamp segment is
// well-formed or that it corresponds to the instant the save happened.
func TestSaveResultUsesInjectedClock(t *testing.T) {
	srv := coreTestServer(t)
	at := time.Date(2031, 3, 4, 5, 6, 7, 0, time.UTC)
	setFixedNowForTest(t, at)

	out := callBare(t, srv.handleSaveResult, map[string]any{"group": "g", "question": "q", "answer": "a"})
	var obj struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("save result not JSON: %v (%s)", err, out)
	}
	// sha256("qa")[:8] — the deterministic content-hash segment.
	sum := sha256.Sum256([]byte("qa"))
	wantBase := "20310304T050607Z-" + hex.EncodeToString(sum[:])[:8] + ".json"
	if base := filepath.Base(obj.Path); base != wantBase {
		t.Errorf("saved filename = %q, want %q (timestamp must come from the injected clock)", base, wantBase)
	}
	buf, err := os.ReadFile(obj.Path)
	if err != nil {
		t.Fatalf("read saved finding: %v", err)
	}
	var payload struct {
		SavedAt string `json:"saved_at"`
	}
	if err := json.Unmarshal(buf, &payload); err != nil {
		t.Fatalf("saved finding not JSON: %v (%s)", err, buf)
	}
	if want := at.Format(time.RFC3339); payload.SavedAt != want {
		t.Errorf("saved_at = %q, want %q", payload.SavedAt, want)
	}
}

// TestSaveResultDispatchIsClockIndependent is the direct regression guard for
// #6073. It drives the two dispatch calls so they are GUARANTEED to straddle a
// wall-clock second boundary — the exact condition that reddened
// windows-latest under -race — and asserts the results are still identical.
// Before the clock seam this failed 100% of the time on every OS; the previous
// normalise-the-timestamp fallback masked it on POSIX only, because its regex
// was anchored on "/" and is a no-op on a Windows path separator.
func TestSaveResultDispatchIsClockIndependent(t *testing.T) {
	srv := coreTestServer(t)
	setFixedNowForTest(t, time.Date(2031, 3, 4, 5, 6, 7, 0, time.UTC))
	save := map[string]any{"group": "g", "question": "q", "answer": "a"}

	got := callBare(t, srv.handleAnalysisFindings,
		map[string]any{"group": "g", "action": "save", "question": "q", "answer": "a"})
	// Sleep past the next real second boundary: the injected clock must make
	// the crossing invisible to the handlers.
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second + 5*time.Millisecond)))
	want := callBare(t, srv.handleSaveResult, save)

	if got != want {
		t.Errorf("save dispatch differs across a real second boundary — the handlers are still reading the wall clock\n got=%s\nwant=%s", got, want)
	}
}

// 6. grafel_diff aspect= → response_shape/payload/auth/literals/refs.
// The return is a discriminated union keyed by `aspect`: we compare the
// canonical result with the absorbed handler's result after STRIPPING the
// injected aspect key, then separately assert the aspect key is present + correct.
func TestAnalysisDiffDispatch(t *testing.T) {
	srv := coreTestServer(t)
	cross := func(aspect string) map[string]any {
		return map[string]any{"group_oracle": "g", "group_v3": "g", "aspect": aspect}
	}
	crossBare := map[string]any{"group_oracle": "g", "group_v3": "g"}

	type diffCase struct {
		aspect  string
		canon   map[string]any
		old     func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error)
		oldArgs map[string]any
	}
	cases := []diffCase{
		{"response_shape", cross("response_shape"), srv.handleResponseShapeDiff, crossBare},
		{"payload", map[string]any{"group": "g", "aspect": "payload"}, srv.handlePayloadDrift, map[string]any{"group": "g"}},
		{"auth", cross("auth"), srv.handleAuthPostureDiff, crossBare},
		{"literals", map[string]any{"group_oracle": "g", "group_v3": "g", "set": "page_slugs", "aspect": "literals"}, srv.handleLiteralParity, map[string]any{"group_oracle": "g", "group_v3": "g", "set": "page_slugs"}},
		{"refs", map[string]any{"group": "g", "repo": "r1", "ref_a": "main", "ref_b": "main", "aspect": "refs"}, srv.handleDiffRefs, map[string]any{"group": "g", "repo": "r1", "ref_a": "main", "ref_b": "main"}},
	}
	for _, c := range cases {
		got := callBare(t, srv.handleAnalysisDiff, c.canon)
		want := callBare(t, c.old, c.oldArgs)
		assertDiffAspect(t, c.aspect, got, want)
	}

	// default aspect=response_shape.
	gotDefault := callBare(t, srv.handleAnalysisDiff, crossBare)
	wantDefault := callBare(t, srv.handleResponseShapeDiff, crossBare)
	assertDiffAspect(t, "response_shape", gotDefault, wantDefault)
}

// 6b. Regression for #5784 bug 3: stampAspect edits res.Content, but the
// payload/response_shape/auth/literals handlers return via jsonResult, which
// stashes the structured value on res.StructuredContent (the "deferred"
// path). wrap() (server.go) re-marshals the FINAL wire bytes from that
// deferred value, discarding the res.Content edit stampAspect made — so
// going through the real registered tool (callTool, which invokes wrap())
// the "aspect" key is silently dropped for every aspect except "refs" (which
// uses mcpapi.NewToolResultText directly, no deferred value). callBare above
// doesn't exercise this because it never calls wrap(). This test does.
func TestAnalysisDiffAspectStampSurvivesWrap(t *testing.T) {
	testsupport.IsolateHome(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGraph(t, repo, fixtureDoc("r1"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)

	// handleDiffRefs (aspect=refs) resolves the repo path via the
	// internal/registry package directly (diffToolRepoPath), independent of
	// the mcp.Registry loaded above — register "g" there too so the
	// same-ref fast path in handleDiffRefs can find repo "r1".
	cfgPath, err := registry.ConfigPathFor("g")
	if err != nil {
		t.Fatalf("registry.ConfigPathFor: %v", err)
	}
	cfg := &registry.GroupConfig{Name: "g", Repos: []registry.Repo{{Slug: "r1", Path: repo}}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("registry.SaveGroupConfig: %v", err)
	}
	if err := registry.AddGroup("g", cfgPath); err != nil {
		t.Fatalf("registry.AddGroup: %v", err)
	}

	writePayloadDriftSidecar(t, "g")

	assertWrappedAspect := func(aspect string, args map[string]any) {
		t.Helper()
		res := callTool(t, srv, "grafel_diff", args)
		text := resultText(res)
		// Non-deferred results (aspect=refs, built via mcpapi.NewToolResultText)
		// carry a trailing "\n# elapsed_ms=N\n" comment appended by
		// appendElapsedTrailer; strip it before parsing. Deferred results
		// (aspect=payload) fold elapsed_ms into the JSON object itself and have
		// no trailer.
		if i := strings.Index(text, "\n# elapsed_ms="); i >= 0 {
			text = text[:i]
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(text), &obj); err != nil {
			t.Fatalf("aspect=%s: result not a JSON object: %v (%s)", aspect, err, text)
		}
		a, ok := obj["aspect"]
		if !ok {
			t.Errorf("aspect=%s: wrapped result missing injected aspect key: %s", aspect, text)
			return
		}
		if string(a) != `"`+aspect+`"` {
			t.Errorf("aspect=%s: wrapped result aspect=%s, want %q", aspect, a, aspect)
		}
	}

	// payload rides the deferred (StructuredContent) path — this is the RED
	// case pre-fix.
	assertWrappedAspect("payload", map[string]any{"group": "g", "aspect": "payload"})
	// refs uses mcpapi.NewToolResultText directly (no deferred value) and must
	// keep working post-fix.
	assertWrappedAspect("refs", map[string]any{
		"group": "g", "repo": "r1", "ref_a": "main", "ref_b": "main", "aspect": "refs",
	})
}

// writePayloadDriftSidecar writes a minimal payload-drift findings sidecar
// under the caller's already-isolated $HOME so handlePayloadDrift returns a
// real JSON object instead of the "no sidecar" error result — the error path
// short-circuits stampAspect (res.IsError) and would mask the #5784 bug 3
// regression this test targets. Callers must isolate $HOME themselves (e.g.
// via testsupport.IsolateHome) before calling this.
func writePayloadDriftSidecar(t *testing.T, group string) {
	t.Helper()
	paths, err := links.PathsFor("", group)
	if err != nil {
		t.Fatalf("links.PathsFor: %v", err)
	}
	sidecarPath := links.DriftSidecarPath(paths)
	if err := os.MkdirAll(filepath.Dir(sidecarPath), 0o755); err != nil {
		t.Fatalf("mkdir sidecar dir: %v", err)
	}
	doc := links.DriftDocument{
		Version:     1,
		Method:      "test",
		Group:       group,
		Total:       1,
		SchemaCount: 1,
		Findings: []links.SchemaDrift{
			{
				EndpointID:   "r1::e1",
				EndpointName: "http:POST:/api/x",
				Direction:    "request",
				Severity:     "high",
				DriftClass:   "schema",
				Confidence:   0.9,
				Explanation:  "test finding",
			},
		},
	}
	buf, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	if err := os.WriteFile(sidecarPath, buf, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

// assertDiffAspect verifies the canonical grafel_diff result equals the absorbed
// handler's result once the injected aspect key is removed, and that the aspect
// key was present with the expected value. Non-JSON-object (error) results pass
// through unchanged, in which case got must equal want verbatim.
func assertDiffAspect(t *testing.T, aspect, got, want string) {
	t.Helper()
	var gotObj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(got), &gotObj); err != nil {
		// Not a JSON object (error/text result) — stampAspect is a no-op, so
		// the canonical result must be byte-identical to the absorbed handler.
		if got != want {
			t.Errorf("aspect=%s (non-object): canonical=%q want=%q", aspect, got, want)
		}
		return
	}
	a, ok := gotObj["aspect"]
	if !ok {
		t.Errorf("aspect=%s: result missing injected aspect key", aspect)
		return
	}
	if string(a) != `"`+aspect+`"` {
		t.Errorf("aspect=%s: injected aspect=%s, want %q", aspect, a, aspect)
	}
	delete(gotObj, "aspect")
	stripped, err := json.Marshal(gotObj)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	// want is the absorbed handler's native JSON object; re-marshal it through
	// the same map round-trip so key ordering matches.
	var wantObj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(want), &wantObj); err != nil {
		t.Fatalf("aspect=%s: absorbed handler result not a JSON object: %v", aspect, err)
	}
	wantNorm, _ := json.Marshal(wantObj)
	if string(stripped) != string(wantNorm) {
		t.Errorf("aspect=%s: stripped canonical differs from absorbed handler\n got=%s\nwant=%s", aspect, stripped, wantNorm)
	}
}

// 7. All six ANALYSIS canonical tools are registered (#5546/#5550).
func TestAnalysisCanonicalToolsRegistered(t *testing.T) {
	srv := coreTestServer(t)
	registered := map[string]bool{}
	for _, st := range srv.MCP.ListTools() {
		registered[st.Tool.Name] = true
	}
	for _, n := range []string{
		"grafel_debt", "grafel_security", "grafel_test_analysis",
		"grafel_patterns", "grafel_findings", "grafel_diff",
	} {
		if !registered[n] {
			t.Errorf("ANALYSIS canonical tool %q not registered", n)
		}
	}
}
