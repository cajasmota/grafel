package errors_test

import (
	"bytes"
	"strings"
	"testing"

	idxerr "github.com/cajasmota/archigraph/internal/errors"
)

func TestFormat_PlainOutput(t *testing.T) {
	var buf bytes.Buffer
	err := idxerr.New(idxerr.CodeFileTooLarge, "file is 42 MiB", "/repo/big.js", 0, nil)
	idxerr.Format(&buf, err)
	out := buf.String()

	requireContains(t, out, "IDX-002")
	requireContains(t, out, "file is 42 MiB")
	requireContains(t, out, "/repo/big.js")
	requireContains(t, out, ".archigraphignore")
	requireContains(t, out, "https://archigraph.dev/docs/errors/IDX-002")
}

func TestFormat_WithLine(t *testing.T) {
	var buf bytes.Buffer
	err := idxerr.New(idxerr.CodeParserTimeout, "timed out after 5s", "/src/app.py", 137, nil)
	idxerr.Format(&buf, err)
	out := buf.String()

	requireContains(t, out, "/src/app.py:137")
}

func TestFormat_Nil(t *testing.T) {
	var buf bytes.Buffer
	idxerr.Format(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil error")
	}
}

func TestFormatError_FallsBackToPlain(t *testing.T) {
	var buf bytes.Buffer
	plain := plainErr("something went wrong")
	idxerr.FormatError(&buf, plain)
	out := buf.String()

	requireContains(t, out, "error:")
	requireContains(t, out, "something went wrong")
}

func TestFormatError_RichForIndexerError(t *testing.T) {
	var buf bytes.Buffer
	err := idxerr.New(idxerr.CodePermissionDenied, "cannot read", "/protected", 0, nil)
	idxerr.FormatError(&buf, err)
	out := buf.String()

	requireContains(t, out, "IDX-001")
	requireContains(t, out, "chmod")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func requireContains(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("expected output to contain %q\ngot:\n%s", sub, s)
	}
}

type plainErr string

func (e plainErr) Error() string { return string(e) }
