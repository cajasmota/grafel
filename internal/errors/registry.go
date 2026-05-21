// Package errors defines the typed error registry for all indexer error classes.
//
// Each error has:
//   - A stable string Code (e.g. "IDX-001")
//   - A plain-language Message
//   - Optional FilePath + Line when the error is file-scoped
//   - A human-readable Hint (suggested fix)
//   - A DocsURL pointing to the relevant documentation section
//
// Callers wrap sentinel errors with New / Wrap; Format renders them for the CLI.
// The dashboard /api/indexer-errors surface consumes the structured JSON form.
package errors

import (
	"errors"
	"fmt"
)

// Code is a stable identifier for one error class.
type Code string

const (
	// CodePermissionDenied — the indexer cannot read the repo path.
	CodePermissionDenied Code = "IDX-001"

	// CodeFileTooLarge — a single file exceeds the 10 MiB limit.
	CodeFileTooLarge Code = "IDX-002"

	// CodeUnsupportedEncoding — a file is not valid UTF-8.
	CodeUnsupportedEncoding Code = "IDX-003"

	// CodeParserTimeout — the tree-sitter parser timed out on a specific file.
	CodeParserTimeout Code = "IDX-004"

	// CodeOutOfMemory — the process exceeded its RSS budget.
	CodeOutOfMemory Code = "IDX-005"

	// CodeSymlinkLoop — the file walker detected a symlink loop.
	CodeSymlinkLoop Code = "IDX-006"

	// CodeMissingManifest — no recognised package manifest found in the repo.
	CodeMissingManifest Code = "IDX-007"

	// CodeASTExtractionFailed — the AST extractor returned an unrecoverable error.
	CodeASTExtractionFailed Code = "IDX-008"

	// CodeCrossRepoUnresolved — a cross-repo reference could not be resolved.
	CodeCrossRepoUnresolved Code = "IDX-009"
)

// hint returns the canonical remediation text for each code.
func hint(c Code) string {
	switch c {
	case CodePermissionDenied:
		return "Check that the current user can read the repository path: `ls -la <repo>`. " +
			"If the directory is owned by another user, adjust permissions with `chmod`/`chown` " +
			"or re-run archigraph as the owner."
	case CodeFileTooLarge:
		return "This file exceeds the 10 MiB single-file limit. Add it to .archigraphignore " +
			"to skip it, or split the file into smaller modules."
	case CodeUnsupportedEncoding:
		return "The file is not valid UTF-8. Convert it with `iconv -f <encoding> -t UTF-8` " +
			"or add it to .archigraphignore."
	case CodeParserTimeout:
		return "The parser timed out — the file may be minified or machine-generated. " +
			"Add it to .archigraphignore to exclude it from indexing."
	case CodeOutOfMemory:
		return "The indexer exceeded its memory budget. Try indexing fewer repos at once, " +
			"or add large generated directories (vendor/, node_modules/) to .archigraphignore."
	case CodeSymlinkLoop:
		return "A symlink loop was detected. Break the cycle or add the looping path to .archigraphignore."
	case CodeMissingManifest:
		return "No package manifest (pyproject.toml, package.json, go.mod, …) was found. " +
			"Ensure the repo root contains a recognised manifest, or use `archigraph index --skip-pass manifest`."
	case CodeASTExtractionFailed:
		return "AST extraction failed for this file. If the file uses experimental syntax, " +
			"add it to .archigraphignore. File a bug report with the file path if this is unexpected."
	case CodeCrossRepoUnresolved:
		return "A cross-repo reference could not be resolved. Ensure all referenced repos are " +
			"registered (`archigraph list`) and have been indexed (`archigraph index`)."
	default:
		return ""
	}
}

// docsURL returns the canonical documentation URL for each code.
func docsURL(c Code) string {
	return "https://archigraph.dev/docs/errors/" + string(c)
}

// IndexerError is a structured indexer error with a stable code, plain-language
// message, optional file path + line, remediation hint, and docs link.
type IndexerError struct {
	// Code is the stable error identifier.
	Code Code `json:"code"`
	// Message is a plain-language description.
	Message string `json:"message"`
	// FilePath is the file that caused the error, if applicable.
	FilePath string `json:"file_path,omitempty"`
	// Line is the approximate line number, if known.
	Line int `json:"line,omitempty"`
	// Hint is the suggested remediation.
	Hint string `json:"hint"`
	// DocsURL links to further documentation.
	DocsURL string `json:"docs_url"`
	// Cause is the underlying error, not serialised to JSON.
	Cause error `json:"-"`
}

// Error implements the error interface.
func (e *IndexerError) Error() string {
	if e.FilePath != "" {
		if e.Line > 0 {
			return fmt.Sprintf("[%s] %s (%s:%d)", e.Code, e.Message, e.FilePath, e.Line)
		}
		return fmt.Sprintf("[%s] %s (%s)", e.Code, e.Message, e.FilePath)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause so errors.Is / errors.As work across wrapping.
func (e *IndexerError) Unwrap() error { return e.Cause }

// New constructs an IndexerError for code c using the canonical hint and docs URL.
// The message is provided by the caller; it may include dynamic context (e.g. sizes,
// paths). Set filePath to "" and line to 0 when not applicable.
func New(c Code, message, filePath string, line int, cause error) *IndexerError {
	return &IndexerError{
		Code:     c,
		Message:  message,
		FilePath: filePath,
		Line:     line,
		Hint:     hint(c),
		DocsURL:  docsURL(c),
		Cause:    cause,
	}
}

// Wrap is a convenience helper that extracts the cause message automatically.
func Wrap(c Code, filePath string, line int, cause error) *IndexerError {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	return New(c, msg, filePath, line, cause)
}

// As reports whether target is an *IndexerError and populates it.
func As(err error, target **IndexerError) bool {
	return errors.As(err, target)
}

// IsCode reports whether err (or any error in its chain) is an IndexerError
// with the given code.
func IsCode(err error, c Code) bool {
	var ie *IndexerError
	if errors.As(err, &ie) {
		return ie.Code == c
	}
	return false
}
