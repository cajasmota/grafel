// Internal-package half of the #6969 grading: the cap constant the external
// fixtures hard-code, and the report line that stops the degradation being
// silent.
package gitmeta

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSparseCapConstantMatchesTheTestsLiteral ties the independent literal in
// sparse_truncation_6969_test.go to the production constant.
//
// The external fixtures cannot import maxSparsePatternBytes and must not: a
// fixture that reads the constant under test builds its file relative to
// whatever the constant says, so a cap change can never fail it. This is the
// one place the two are compared, and it fails loudly rather than letting the
// byte-exact geometries over there quietly stop measuring the boundary.
func TestSparseCapConstantMatchesTheTestsLiteral(t *testing.T) {
	const literalUsedByTheExternalFixtures = 8 << 20
	if maxSparsePatternBytes != literalUsedByTheExternalFixtures {
		t.Fatalf("maxSparsePatternBytes = %d but sparse_truncation_6969_test.go builds its fixtures against %d; update sparseCapBytes6969 there",
			maxSparsePatternBytes, literalUsedByTheExternalFixtures)
	}
}

// TestParseSparsePatternFile_TruncatedIsReportedAndRejected covers the unit
// directly, without a git repo, on the two facts ProbeRepo's callers cannot
// see: the verdict is "no file" (so the repo indexes in full) and the reason
// is printed.
//
// A truncated read that is merely handled is still a behaviour change with no
// stated cause — #6338's shape. The line has to name the file and say which
// way the decision fell, because "not sparse" on a repo the user made sparse
// is otherwise indistinguishable from grafel ignoring their sparse-checkout.
func TestParseSparsePatternFile_TruncatedIsReportedAndRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sparse-checkout")

	body := bytes.Repeat([]byte("/src/\n"), (maxSparsePatternBytes/6)+64)
	if int64(len(body)) <= maxSparsePatternBytes {
		t.Fatalf("fixture premise broken: %d bytes does not exceed the %d cap", len(body), maxSparsePatternBytes)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	restore := setGitMetaSkipOutput(&buf)
	defer restore()

	patterns, haveFile := parseSparsePatternFile(path)
	if haveFile {
		t.Fatalf("a truncated pattern file must read as NO file (so the repo indexes in full); got haveFile=true with %d patterns", len(patterns))
	}
	if patterns != nil {
		t.Fatalf("a rejected pattern file must yield no patterns, got %v", patterns[:min(3, len(patterns))])
	}

	out := buf.String()
	if !strings.Contains(out, path) {
		t.Fatalf("the truncation report names no file, so a user cannot act on it: %q", out)
	}
	if !strings.Contains(out, "NOT sparse") {
		t.Fatalf("the truncation report does not say which way the decision fell: %q", out)
	}
	if !strings.Contains(out, "#6969") {
		t.Fatalf("the truncation report carries no issue reference: %q", out)
	}
}

// TestParseSparsePatternFile_CompleteFileIsNotReported is the other direction:
// an ordinary pattern file must produce patterns and print NOTHING. A reporter
// that fires on every healthy repo buries the signal it exists to carry, and
// this package is entered on every whoami/ResolveCWD.
func TestParseSparsePatternFile_CompleteFileIsNotReported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sparse-checkout")
	if err := os.WriteFile(path, []byte("/*\n!/*/\n/src/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	restore := setGitMetaSkipOutput(&buf)
	defer restore()

	patterns, haveFile := parseSparsePatternFile(path)
	if !haveFile {
		t.Fatal("a complete pattern file read as no file")
	}
	if len(patterns) != 3 {
		t.Fatalf("patterns = %v, want the 3 lines written", patterns)
	}
	if buf.Len() != 0 {
		t.Fatalf("a complete pattern file printed a report: %q", buf.String())
	}
}
