package classifier

import (
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// The two dispositions a file with no detected language can have (#6338).
//
// Both mean "no extractor claimed this extension", and before #6338 they were
// one reason string. That conflation is why the first cut of this report was
// unusable: driven over a real 31k-file monorepo it produced sixty rows —
// `.json 1938`, `.xml 1531`, `.txt 149`, `.log 67`, `.ds_store 11` — under a
// header reading "Unsupported languages". None of those are languages, and the
// one row that mattered (`.vb 672`) arrived buried in the middle of them.
//
// They are split HERE, at the point the decision is made, rather than filtered
// at the renderer, because they are genuinely different facts about the file:
//
//   - SkipReasonUnsupportedLanguage: the extension names a programming language
//     that grafel has no extractor for. Something a user can act on, and the
//     only disposition this report surfaces.
//
//   - SkipReasonUnsupportedExtension: no extractor, and no reason to expect
//     one. Config, data, docs, lockfiles, logs, build artifacts, and anything
//     else unrecognised. classifier.go says this outright where it declines to
//     route generic .json ("indexing every .json file would balloon scope"),
//     and the walker's extension filter is a DENY-list of binary/media types,
//     so every config and data file in the tree reaches this branch. Reporting
//     them would recreate the noise problem #6338 exists to avoid.
const (
	SkipReasonUnsupportedLanguage  = "unsupported_language"
	SkipReasonUnsupportedExtension = "unsupported_extension"
)

// unsupportedLanguageNames is the policy surface for the split above: an
// extension is reported as a silently-skipped LANGUAGE if and only if it
// appears here, and the value is the name the report prints.
//
// This is deliberately an allow-list of named languages rather than a
// deny-list of junk extensions, and the tradeoff is explicit:
//
//   - Every row the report can possibly emit carries a language name, so every
//     row is actionable. "VB.NET — not supported" tells a reader what happened;
//     ".ktr 1107 files" does not. A deny-list cannot promise this, because the
//     long tail of unknown extensions (.1, .835, .native, .jrxml) is unbounded
//     and grows with every repo grafel is pointed at.
//
//   - The cost is that a language absent from this table is silent — the very
//     blind spot #6338 is about, for that language. That is accepted because
//     closing it is a one-line addition here, and because a report a user
//     scrolls past protects nobody. If a language is missing, add it.
//
// Entries with more than one plausible owner are deliberately omitted and
// render nothing: .m (Objective-C / MATLAB / Mercury — and grafel routes it to
// objective_c anyway), .pp (Pascal / Puppet), .cls (Apex / VBA / LaTeX),
// .d (D / DTrace / make depfile), .st (Smalltalk / Stata). A confidently wrong
// language name is worse than no row.
//
// INVARIANT: no key here may be an extension grafel supports. Enforced by
// TestUnsupportedLanguageRegistryHasNoSupportedExtension — when an extractor
// lands for one of these (VB.NET, #6327), that test fails until the entry is
// removed, which is what guarantees the row stops being printed.
var unsupportedLanguageNames = map[string]string{
	// Visual Basic family — the report that prompted #6338.
	".vb":  "VB.NET",
	".vbs": "VBScript",
	".vba": "VBA",
	".bas": "BASIC",
	".frm": "Visual Basic form",

	// Pascal / Delphi.
	".pas": "Pascal",
	".dpr": "Delphi",
	".dfm": "Delphi form",

	// Fortran.
	".f": "Fortran", ".f77": "Fortran", ".f90": "Fortran",
	".f95": "Fortran", ".f03": "Fortran", ".f08": "Fortran",
	".for": "Fortran", ".ftn": "Fortran",

	// Ada.
	".ada": "Ada", ".adb": "Ada", ".ads": "Ada",

	// Windows scripting — both showed up on grafel's own tree.
	".ps1": "PowerShell", ".psm1": "PowerShell", ".psd1": "PowerShell",
	".bat": "Batch", ".cmd": "Batch",

	// Enterprise / mainframe.
	".abap":  "ABAP",
	".rpg":   "RPG",
	".rpgle": "RPG",
	// .sqlrpgle is the more common modern IBM i form; having .rpgle without it
	// is the near-miss the allow-list tradeoff produces.
	".sqlrpgle": "RPG",
	".pli":      "PL/I",
	".pl1":      "PL/I",
	".rexx":     "REXX",
	".rex":      "REXX",
	".sas":      "SAS",

	// Web templating with no extractor.
	".asp":  "Classic ASP",
	".aspx": "ASP.NET Web Forms",
	".jsp":  "JSP",
	".jspx": "JSP",
	// .razor IS supported; .cshtml/.vbhtml are not, and they are the majority
	// form in real ASP.NET codebases — measured at 473 and 639 files across two
	// public aspnetcore corpora, a silent gap the size of the one that prompted
	// #6338.
	".cshtml": "Razor view",
	".vbhtml": "Razor view",
	".cfm":    "ColdFusion",
	".cfc":    "ColdFusion",

	// Everything else with exactly one plausible owner.
	".jl":          "Julia",
	".tcl":         "Tcl",
	".hx":          "Haxe",
	".coffee":      "CoffeeScript",
	".applescript": "AppleScript",
	".awk":         "AWK",
	".gd":          "GDScript",
	".purs":        "PureScript",
	".raku":        "Raku",
	".p6":          "Raku",
	".ahk":         "AutoHotkey",
	".au3":         "AutoIt",
}

// unsupportedTrackingIssues names the grafel issue tracking support for an
// extension, so the report can point at it instead of leaving the reader to
// search. Only populated where an issue actually exists.
var unsupportedTrackingIssues = map[string]string{
	".vb": "#6327",
}

// UnsupportedTally aggregates, by lowercased file extension, the files the
// classifier dropped for SkipReasonUnsupportedLanguage.
//
// Aggregation happens here — at the point the decision is made — rather than
// by collecting a file list and grouping later: the originating report was 672
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
// not an unsupported-LANGUAGE skip is ignored: indexed files, vendored files,
// binaries, oversized files, and the unrecognised-but-not-a-language files that
// carry SkipReasonUnsupportedExtension.
func (t *UnsupportedTally) Observe(filePath string, res ClassifyResult) {
	if t == nil {
		return
	}
	if !res.Skip || res.SkipReason != SkipReasonUnsupportedLanguage {
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
// extension or one that names no language.
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
		if LanguageDisplayName(norm) == "" {
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
// (.gitignore, .DS_Store) have no extension to aggregate on and are excluded.
func reportableExtension(filePath string) string {
	if filePath == "" {
		return ""
	}
	base := strings.ToLower(path.Base(filepath.ToSlash(filePath)))
	ext := filepath.Ext(base)
	if ext == "" || ext == base {
		// ext == base catches dotfiles: filepath.Ext(".gitignore") is
		// ".gitignore", not "". BOTH sides are lowercased — comparing a
		// lowercased ext against a raw base let ".DS_Store" and ".Rprofile"
		// through while ".gitignore" was correctly excluded.
		return ""
	}
	return ext
}

// unsupportedSkipReason splits "no extractor claimed this extension" into the
// two dispositions above. Called only when detectLanguage returned "".
func unsupportedSkipReason(filePath string) string {
	if _, ok := unsupportedLanguageNames[reportableExtension(filePath)]; ok {
		return SkipReasonUnsupportedLanguage
	}
	return SkipReasonUnsupportedExtension
}

// supportedProbeStem is a filename stem chosen so a probe path exercises
// detectLanguage's extension and compound-suffix rules without colliding with
// its exact-basename rules (Dockerfile, Package.swift, ocelot.json, ...).
const supportedProbeStem = "grafelextprobe"

// SupportedExtension reports whether some extractor claims ext (which must
// include the leading dot, e.g. ".go"). Case-insensitive.
//
// It asks detectLanguage rather than consulting extensionLanguageMap directly.
// That map is only one of detectLanguage's routes — compound suffixes
// (.scala.html, .tofu.json, .schema.json) and exact basenames route too — so a
// map-only check would answer "unsupported" for an extension the router
// actually claims. That matters most for the guarantee this function exists to
// provide: the report drops a row the moment an extractor lands for it, and it
// must do so however that extractor was wired up.
//
// This is consulted again when the report is RENDERED, not only when the counts
// are collected, because the counts live in an on-disk sidecar that can be
// months old.
func SupportedExtension(ext string) bool {
	norm := strings.ToLower(ext)
	if norm == "" || !strings.HasPrefix(norm, ".") {
		return false
	}
	return detectLanguage(supportedProbeStem+norm) != ""
}

// LanguageDisplayName returns the human name of the language ext belongs to,
// or "" when ext names no language we know of — in which case it is not
// reported at all.
//
// A SUPPORTED extension always returns "": this registry describes languages we
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

// ReportableExtensionForTest and UnsupportedLanguageExtensionsForTest expose
// package internals to the report-layer tests in internal/cli, which is where
// the F1/F2/F5 regressions are asserted end-to-end.
func ReportableExtensionForTest(filePath string) string { return reportableExtension(filePath) }

func UnsupportedLanguageExtensionsForTest() []string {
	out := make([]string, 0, len(unsupportedLanguageNames))
	for ext := range unsupportedLanguageNames {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}
