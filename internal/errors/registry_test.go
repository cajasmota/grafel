package errors_test

import (
	"fmt"
	"strings"
	"testing"

	idxerr "github.com/cajasmota/archigraph/internal/errors"
)

func TestAllCodesHaveHints(t *testing.T) {
	allCodes := []idxerr.Code{
		idxerr.CodePermissionDenied,
		idxerr.CodeFileTooLarge,
		idxerr.CodeUnsupportedEncoding,
		idxerr.CodeParserTimeout,
		idxerr.CodeOutOfMemory,
		idxerr.CodeSymlinkLoop,
		idxerr.CodeMissingManifest,
		idxerr.CodeASTExtractionFailed,
		idxerr.CodeCrossRepoUnresolved,
	}
	for _, code := range allCodes {
		err := idxerr.New(code, "test message", "", 0, nil)
		if err.Hint == "" {
			t.Errorf("code %s has no hint", code)
		}
		if err.DocsURL == "" {
			t.Errorf("code %s has no docs URL", code)
		}
		if !strings.HasPrefix(err.DocsURL, "https://") {
			t.Errorf("code %s docs URL should be https:// got %s", code, err.DocsURL)
		}
	}
}

func TestErrorString(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		line     int
		wantSubs []string
	}{
		{"no file", "", 0, []string{"IDX-001", "permission denied"}},
		{"with file", "/repo/foo.go", 0, []string{"IDX-001", "/repo/foo.go"}},
		{"with file+line", "/repo/foo.go", 42, []string{"IDX-001", "/repo/foo.go:42"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := idxerr.New(idxerr.CodePermissionDenied, "permission denied", tt.file, tt.line, nil)
			s := err.Error()
			for _, sub := range tt.wantSubs {
				if !strings.Contains(s, sub) {
					t.Errorf("Error() = %q; want substring %q", s, sub)
				}
			}
		})
	}
}

func TestWrap(t *testing.T) {
	cause := fmt.Errorf("underlying os error")
	err := idxerr.Wrap(idxerr.CodePermissionDenied, "/some/path", 0, cause)
	if err.Cause != cause {
		t.Errorf("Cause mismatch")
	}
	if err.Message != cause.Error() {
		t.Errorf("Message should equal cause.Error(); got %q", err.Message)
	}
	if !strings.Contains(err.Unwrap().Error(), "underlying os error") {
		t.Errorf("Unwrap should return cause")
	}
}

func TestIsCode(t *testing.T) {
	err := idxerr.New(idxerr.CodeFileTooLarge, "too big", "/large.bin", 0, nil)
	if !idxerr.IsCode(err, idxerr.CodeFileTooLarge) {
		t.Error("IsCode should return true for matching code")
	}
	if idxerr.IsCode(err, idxerr.CodePermissionDenied) {
		t.Error("IsCode should return false for non-matching code")
	}
	if idxerr.IsCode(fmt.Errorf("plain error"), idxerr.CodeFileTooLarge) {
		t.Error("IsCode should return false for non-IndexerError")
	}
}

func TestAs(t *testing.T) {
	original := idxerr.New(idxerr.CodeParserTimeout, "timed out", "/slow.rs", 0, nil)
	var target *idxerr.IndexerError
	if !idxerr.As(original, &target) {
		t.Fatal("As should succeed")
	}
	if target.Code != idxerr.CodeParserTimeout {
		t.Errorf("Code = %s; want IDX-004", target.Code)
	}
}
