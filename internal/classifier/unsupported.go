package classifier

import (
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// SkipReasonUnsupportedExtension is the ClassifyResult.SkipReason the
// classifier stamps on a file that no extractor claims — the file was seen,
// was not vendored, ignored, binary or oversized, and was dropped purely
// because grafel has no extractor for its extension.
//
// It is exported because it is the ONE skip reason that means "grafel silently
// indexed nothing here" (#6338). Every other reason is a deliberate exclusion
// that is already reported (or deliberately not) elsewhere; conflating them
// would bury the signal under vendored-directory noise.
const SkipReasonUnsupportedExtension = "unsupported_extension"

// UnsupportedTally aggregates, by lowercased file extension, the files the
// classifier dropped for SkipReasonUnsupportedExtension.
//
// Aggregation happens here — at the point the decision is made — rather than
// by collecting a file list and grouping later: @dcastro-imp's report was 672
// `.vb` files, and a per-file list at that size is unusable. Only the counter
// is ever held in memory, so the tally costs O(distinct extensions) regardless
// of repo size.
//
// The zero value is not usable; call NewUnsupportedTally. All methods are safe
// for concurrent use — the index walk observes from a worker pool.
type UnsupportedTally struct {
	mu     sync.Mutex
	counts map[string]int
}

// NewUnsupportedTally returns an empty tally.
func NewUnsupportedTally() *UnsupportedTally {
	return &UnsupportedTally{counts: make(map[string]int)}
}

// Observe folds one classification decision into the tally. Everything that is
// not an unsupported-extension skip is ignored, including files that were
// indexed and files skipped for any other reason.
func (t *UnsupportedTally) Observe(filePath string, res ClassifyResult) {
	if t == nil {
		return
	}
	if !res.Skip || res.SkipReason != SkipReasonUnsupportedExtension {
		return
	}
	ext := reportableExtension(filePath)
	if ext == "" {
		return
	}
	t.mu.Lock()
	t.counts[ext]++
	t.mu.Unlock()
}

// Merge folds a counts map produced elsewhere (the subprocess-extract
// coordinator hands its tally back as a plain map) into this one. The same
// filters Observe applies are re-applied here: a map that arrived from another
// process, or from an older grafel, must not be able to introduce a supported
// extension or a bare/dotfile bucket.
func (t *UnsupportedTally) Merge(counts map[string]int) {
	if t == nil || len(counts) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for ext, n := range counts {
		if n <= 0 {
			continue
		}
		norm := strings.ToLower(ext)
		if !strings.HasPrefix(norm, ".") || strings.Count(norm, ".") != 1 {
			continue
		}
		if SupportedExtension(norm) {
			continue
		}
		t.counts[norm] += n
	}
}

// Counts returns a copy of the per-extension counts. Extensions with a zero
// count are never present: "report zero as absent, not as a zero row".
func (t *UnsupportedTally) Counts() map[string]int {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]int, len(t.counts))
	for k, v := range t.counts {
		if v > 0 {
			out[k] = v
		}
	}
	return out
}

// reportableExtension returns the lowercased extension to aggregate filePath
// under, or "" when the file has none worth reporting.
//
// Files with no extension (LICENSE, Makefile, bin/run) and dotfiles
// (.gitignore, .editorconfig) collapse into a single enormous bucket that
// names no language and suggests no action, so they are excluded. The report
// is per-extension by definition; a file with no extension has no row.
func reportableExtension(filePath string) string {
	if filePath == "" {
		return ""
	}
	base := path.Base(filepath.ToSlash(filePath))
	ext := strings.ToLower(filepath.Ext(base))
	if ext == "" || ext == base {
		// ext == base catches dotfiles: filepath.Ext(".gitignore") is
		// ".gitignore", not "".
		return ""
	}
	return ext
}

// SupportedExtension reports whether some extractor claims ext (which must
// include the leading dot, e.g. ".go"). Case-insensitive.
//
// This is the guard that makes the report self-correcting: it is consulted
// again when the report is RENDERED, not only when the counts are collected.
// The counts live in an on-disk sidecar that can be months old, so without the
// render-time check a repo indexed before VB.NET support landed would keep
// reporting ".vb — not supported" until someone reindexed it.
func SupportedExtension(ext string) bool {
	if ext == "" {
		return false
	}
	_, ok := extensionLanguageMap[strings.ToLower(ext)]
	return ok
}

// unsupportedLanguageNames maps an extension we have NO extractor for to the
// language a human would call it. ".vb — 672 files" makes a reader go looking;
// "VB.NET — not supported" tells them what happened.
//
// Deliberately conservative: an extension with more than one plausible owner
// (.m is Objective-C or MATLAB or Mercury; .pp is Pascal or Puppet; .cls is
// Apex or VBA or LaTeX) is left OUT and renders bare. A confidently wrong
// language name is worse than no name.
var unsupportedLanguageNames = map[string]string{
	".vb":          "VB.NET",
	".vbs":         "VBScript",
	".bas":         "BASIC",
	".frm":         "Visual Basic form",
	".pas":         "Pascal",
	".dpr":         "Delphi",
	".dfm":         "Delphi form",
	".f":           "Fortran",
	".f77":         "Fortran",
	".f90":         "Fortran",
	".f95":         "Fortran",
	".for":         "Fortran",
	".ada":         "Ada",
	".adb":         "Ada",
	".ads":         "Ada",
	".jl":          "Julia",
	".tcl":         "Tcl",
	".hx":          "Haxe",
	".coffee":      "CoffeeScript",
	".abap":        "ABAP",
	".applescript": "AppleScript",
	".sas":         "SAS",
	".rpg":         "RPG",
}

// unsupportedTrackingIssues names the grafel issue tracking support for an
// extension, so the report can point at it instead of leaving the reader to
// search. Only populated where an issue actually exists.
var unsupportedTrackingIssues = map[string]string{
	".vb": "#6327",
}

// LanguageDisplayName returns the human name of the language ext belongs to,
// or "" when we have no confident name for it (in which case the report shows
// the bare extension).
//
// A SUPPORTED extension always returns "" — this table describes languages we
// do not extract, and once an extractor claims an extension it has no entry to
// give even if the map still holds one.
func LanguageDisplayName(ext string) string {
	norm := strings.ToLower(ext)
	if SupportedExtension(norm) {
		return ""
	}
	return unsupportedLanguageNames[norm]
}

// TrackingIssue returns the grafel issue reference tracking support for ext,
// or "" when there is none (or the extension is already supported).
func TrackingIssue(ext string) string {
	norm := strings.ToLower(ext)
	if SupportedExtension(norm) {
		return ""
	}
	return unsupportedTrackingIssues[norm]
}
