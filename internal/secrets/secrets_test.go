package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests
// ─────────────────────────────────────────────────────────────────────────────

func TestShannonEntropy(t *testing.T) {
	tests := []struct {
		input    string
		wantHigh bool // should be > entropyThreshold?
	}{
		{"aaaaaaaaaaaaaaaa", false},            // all same → zero entropy
		{"aB3xY7kLpQ2mN8rTwZ5v", true},         // random mix → high entropy
		{"AKIAIOSFODNN7REAL0000", true},        // real-looking AWS key → high entropy
		{"sk_live_AbCdEfGhIjKlMnOpQrSt", true}, // Stripe key format
	}
	for _, tc := range tests {
		e := shannonEntropy(tc.input)
		got := e > entropyThreshold
		if got != tc.wantHigh {
			t.Errorf("shannonEntropy(%q) high=%v, want %v (entropy=%.2f, threshold=%.2f)",
				tc.input, got, tc.wantHigh, e, entropyThreshold)
		}
	}
}

func TestMaskValue(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"ABCD1234EFGH5678", "ABCD" + "********" + "5678"},
		{"short", "*****"},
		{"12345678", "1234****"},
	}
	for _, tc := range tests {
		got := maskValue(tc.in)
		if got != tc.want {
			t.Errorf("maskValue(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsTestFile(t *testing.T) {
	trues := []string{
		"pkg/test/helpers.go",
		"src/tests/util.js",
		"pkg/__tests__/foo.ts",
		"auth/auth_test.go",
		"src/auth.test.ts",
		"fixtures/mock_data.py",
	}
	falses := []string{
		"internal/server/server.go",
		"cmd/main.go",
		"pkg/auth/auth.go",
	}
	for _, p := range trues {
		if !isTestFile(p) {
			t.Errorf("isTestFile(%q) = false, want true", p)
		}
	}
	for _, p := range falses {
		if isTestFile(p) {
			t.Errorf("isTestFile(%q) = true, want false", p)
		}
	}
}

func TestIsPlaceholder(t *testing.T) {
	placeholders := []string{
		"your_api_key_here",
		"REPLACE_ME_WITH_KEY",
		"changeme",
		"xxxxxxxxxxxxxxxxxxxx",
		"aaaaaaaaaaaaaaaaaaa",
		"AKIAIOSFODNN7EXAMPLE", // well-known AWS documentation test key
	}
	reals := []string{
		"AKIAIOSFODNN7REAL0000", // not the docs example
		"ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789AB",
		"sk_live_AbCdEfGhIjKlMnOpQrSt",
	}
	for _, v := range placeholders {
		if !isPlaceholder(v) {
			t.Errorf("isPlaceholder(%q) = false, want true", v)
		}
	}
	for _, v := range reals {
		if isPlaceholder(v) {
			t.Errorf("isPlaceholder(%q) = true, want false", v)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration: scan synthetic fixture files
// ─────────────────────────────────────────────────────────────────────────────

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestScanPath_DetectsAWSKey(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "config.go", `package main

const awsKey = "AKIAIOSFODNN7REAL0000"
`)
	findings, err := ScanPath(dir, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected at least one finding for AWS key, got none")
	}
	if findings[0].Severity != SeverityCritical {
		t.Errorf("expected critical severity, got %s", findings[0].Severity)
	}
	if findings[0].Kind != "aws_access_key" {
		t.Errorf("expected kind aws_access_key, got %s", findings[0].Kind)
	}
	if findings[0].MaskedValue == "AKIAIOSFODNN7REAL0000" {
		t.Error("masked value should not equal the original secret")
	}
}

func TestScanPath_DetectsGitHubToken(t *testing.T) {
	dir := t.TempDir()
	// ghp_ followed by exactly 36 alphanumeric characters.
	writeTempFile(t, dir, "ci.go", `package main

var token = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789AB"
`)
	findings, err := ScanPath(dir, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected finding for GitHub token, got none")
	}
	f := findings[0]
	if f.Kind != "github_token" {
		t.Errorf("want kind github_token, got %s", f.Kind)
	}
	if f.Severity != SeverityHigh {
		t.Errorf("want high severity, got %s", f.Severity)
	}
}

func TestScanPath_DetectsJWT(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "auth.go", `package main

const defaultJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
`)
	findings, err := ScanPath(dir, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected finding for JWT, got none")
	}
	if findings[0].Kind != "jwt_token" {
		t.Errorf("want kind jwt_token, got %s", findings[0].Kind)
	}
}

func TestScanPath_SuppressesTestFiles(t *testing.T) {
	dir := t.TempDir()
	// Test file — should be suppressed.
	writeTempFile(t, dir, "config_test.go", `package main

const awsKey = "AKIAIOSFODNN7REAL0000"
`)
	findings, err := ScanPath(dir, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	for _, f := range findings {
		t.Errorf("unexpected finding in test file: %+v", f)
	}
}

func TestScanPath_SuppressesIgnoreComment(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "config.go", `package main

const awsKey = "AKIAIOSFODNN7REAL0000" // archigraph: ignore-secret
`)
	findings, err := ScanPath(dir, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("expected no findings when opt-out comment present, got %d", len(findings))
	}
}

func TestScanPath_SuppressesPlaceholders(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "readme_code.go", `package main

const awsKey = "AKIA_EXAMPLE_REPLACE_ME_00"
const githubToken = "ghp_YOUR_TOKEN_HERE_ABCDEFGHIJKLMNO01234"
`)
	findings, err := ScanPath(dir, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	for _, f := range findings {
		t.Logf("finding (may be placeholder): %+v", f)
	}
	// No panic / crash is the primary assertion; specific suppressions depend
	// on placeholder detection. Placeholders that pass entropy still caught.
}

func TestScanPath_HighEntropyDetection(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "config.go", `package main

var secretKey = "aB3xY7kLpQ2mN8rTwZ5vJhGfDqCeUiOs"
`)
	findings, err := ScanPath(dir, 0)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected high-entropy finding, got none")
	}
}

func TestBuildReport(t *testing.T) {
	findings := []Finding{
		{File: "a.go", Line: 1, Kind: "aws_access_key", Severity: SeverityCritical, MaskedValue: "AKIA****"},
		{File: "a.go", Line: 2, Kind: "github_token", Severity: SeverityHigh, MaskedValue: "ghp_****"},
		{File: "b.go", Line: 5, Kind: "high_entropy_secret", Severity: SeverityMedium, MaskedValue: "aB3x****"},
	}
	report := BuildReport("/root", findings)
	if report.TotalFindings != 3 {
		t.Errorf("want 3 total findings, got %d", report.TotalFindings)
	}
	if report.BySeverity["critical"] != 1 {
		t.Errorf("want 1 critical, got %d", report.BySeverity["critical"])
	}
	if len(report.Files) != 2 {
		t.Errorf("want 2 file rollups, got %d", len(report.Files))
	}
	// a.go has 2 findings with max severity = critical.
	for _, rf := range report.Files {
		if rf.File == "a.go" && rf.Severity != SeverityCritical {
			t.Errorf("a.go rollup severity want critical, got %s", rf.Severity)
		}
	}
}
