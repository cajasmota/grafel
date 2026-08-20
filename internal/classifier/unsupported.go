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
// INVARIANT: no key here may be an extension whose language has a REGISTERED
// EXTRACTOR. Enforced by TestUnsupportedLanguageRegistryHasNoRegisteredExtractor
// (internal/cli/unsupported_realrepo_6338_test.go), which lives in internal/cli
// because that is the lowest package that can import both internal/classifier
// and internal/extractors. When an extractor lands for one of these (VB.NET,
// #6327 S4) that test fails until the entry is removed, which is what
// guarantees the row stops being printed.
//
// It says EXTRACTOR and not "supported extension" on purpose. Keyed on
// SupportedExtension it could only fire on routing, so it went green for
// `.proto`, `.prisma` and `.toml` — routed, extractor-less, and silently
// dropped from the report — and it could never have fired for `.vb` at all
// (#6327 S2 review).
var unsupportedLanguageNames = map[string]string{
	// Visual Basic family — the report that prompted #6338. `.vb` itself is
	// GONE from this table: #6327 S5 registered internal/extractors/vbnet, and
	// the invariant above forbids an entry whose language has an extractor.
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

	// Oracle PL/SQL package sources (#6344). These are the entries in this
	// batch that matter most, because `.sql` IS routed and extracted: an
	// Oracle shop currently gets SILENTLY PARTIAL coverage, which is worse
	// than none — the graph looks populated, so nothing prompts the reader to
	// ask what is missing, and the package specs and bodies where the actual
	// procedural logic lives are exactly the part that vanished. `.cls` is
	// still deliberately absent (Apex / VBA / LaTeX), but `.trigger` below has
	// exactly one owner and is safe to name.
	".pks": "PL/SQL", // package specification
	".pkb": "PL/SQL", // package body
	".plb": "PL/SQL", // wrapped package body

	// Salesforce.
	".trigger": "Apex",

	// XSLT. `.xml` is (correctly) junk, but a stylesheet is a program.
	".xsl":  "XSLT",
	".xslt": "XSLT",

	// Sketch-style C++ dialects. Split rather than merged: `.ino` is Arduino's
	// own extension and `.pde` is Processing's, and naming each for its own
	// tool is more useful than a joint label on both.
	".ino": "Arduino",
	".pde": "Processing",

	// SQL Server Integration Services packages.
	".dtsx": "SSIS package",

	// Everything else with exactly one plausible owner.
	".4gl":         "Informix 4GL",
	".nut":         "Squirrel",
	".moon":        "MoonScript",
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

// languagesAwaitingExtractor names languages the extension table in
// classifier.go recognises but which NO extractor is registered for yet.
//
// This exists because #6327 S2 opens a gap between two facts that used to be
// the same fact: "grafel knows what language this file is" and "grafel can
// extract it". Adding `.vb` → "vbnet" to the extension map settles the first
// and changes nothing about the second — after S2 a `.vb` file still yields
// ZERO entities; S3–S5 are what change that.
//
// Without this set, closing the first gap would silently REOPEN the second,
// one layer deeper than #6338 found it:
//
//   - Classify would return Skip=false, so UnsupportedTally.Observe (which
//     only counts skips) would stop counting `.vb`, and the 672-file row #6338
//     exists to print would vanish from `doctor`/`status`.
//   - SupportedExtension asks detectLanguage, so it would start answering true
//     for `.vb`, and LanguageDisplayName would blank the row a second time
//     even for counts already on disk.
//   - The files would then reach internal/daemon/extract/subproc.go:336, where
//     a missing extractor increments stats.Skipped and emits nothing at all.
//
// So a language listed here is classified — ClassifyResult.Language carries
// its slug — and simultaneously skipped as SkipReasonUnsupportedLanguage,
// which is not a contradiction but the accurate description of the state: we
// know the language, we have no extractor. The report keeps printing the row,
// and the row keeps pointing at the tracking issue, until the extractor lands.
//
// REMOVAL PROTOCOL: delete the entry in the same change that registers the
// extractor, and delete the matching key from unsupportedLanguageNames /
// unsupportedTrackingIssues. Two tests in internal/cli enforce this, and they
// enforce different halves — neither one alone is sufficient:
//
//   - TestLanguagesAwaitingExtractorHaveNoRegisteredExtractor fires on THIS
//     map. Without it a registered vbnet extractor would receive zero files,
//     because a language listed here is skipped before extraction. That is
//     #6321 all over again, with the extractor already built.
//   - TestUnsupportedLanguageRegistryHasNoRegisteredExtractor fires on
//     unsupportedLanguageNames, and stops the stale "not supported" row.
//
// Both are keyed on extractors.Get, so both fire on the S4 commit itself
// rather than after the entry a developer was supposed to remember to delete
// has been deleted (#6327 S2 review).
// It is EMPTY today, and that is the correct state rather than a reason to
// delete the mechanism: `vbnet` was its only entry, removed by #6327 S5 when
// internal/extractors/vbnet landed. The next language to be classified ahead
// of its extractor goes here, and the removal protocol above is what unwinds
// it again.
var languagesAwaitingExtractor = map[string]bool{}

// awaitingExtractorResult returns the ClassifyResult for a language in
// languagesAwaitingExtractor, and ok=false when the language is not in it.
//
// The Language field is populated even though Skip is true: callers that want
// to know what the file is (and tests pinning that `.vb` is recognised as
// VB.NET) get the answer, while the extraction pipeline still never receives a
// file it has no extractor for.
func awaitingExtractorResult(lang string) (ClassifyResult, bool) {
	if !languagesAwaitingExtractor[lang] {
		return ClassifyResult{}, false
	}
	return ClassifyResult{
		Language:   lang,
		Skip:       true,
		SkipReason: SkipReasonUnsupportedLanguage,
	}, true
}

// unsupportedTrackingIssues names the grafel issue tracking support for an
// extension, so the report can point at it instead of leaving the reader to
// search. Only populated where an issue actually exists.
// Empty since #6327 S5: `.vb` was its only entry and VB.NET now has an
// extractor, so there is nothing left to point a reader at.
var unsupportedTrackingIssues = map[string]string{}

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

// LanguageForExtension returns the language slug the ROUTER produces for ext
// (which must include the leading dot, e.g. ".go"), or "" when no rule claims
// it. Case-insensitive.
//
// It asks detectLanguage rather than consulting extensionLanguageMap directly.
// That map is only one of detectLanguage's routes — compound suffixes
// (.scala.html, .tofu.json, .schema.json) and exact basenames route too — so a
// map-only check would answer "unknown" for an extension the router actually
// claims.
//
// IT ANSWERS FOR THE ROUTER, NOT FOR THE EXTRACTORS. A slug coming back here
// means grafel has a NAME for the file; it does not mean any extractor will
// produce an entity from it. Eight of the classifier's routed languages have no
// registered extractor at all. This package cannot tell them apart, and must
// not pretend to: internal/extractors imports internal/classifier
// (extractors/incremental.go), so the dependency edge only runs one way.
//
// The caller that needs "will this produce entities" asks extractors.Get and
// derives it — internal/cli/unsupported_report.go does exactly that, which is
// what drops a report row the moment an extractor lands, however that extractor
// was wired up. Deriving it there also means the answer is right for all eight
// languages instead of for one hand-listed carve-out (#6327 S2 review).
func LanguageForExtension(ext string) string {
	norm := strings.ToLower(ext)
	if norm == "" || !strings.HasPrefix(norm, ".") {
		return ""
	}
	return detectLanguage(supportedProbeStem + norm)
}

// SupportedExtension reports whether the router names a language for ext.
// Shorthand for LanguageForExtension(ext) != "" — read that function's comment
// for what this does and does not promise.
func SupportedExtension(ext string) bool {
	return LanguageForExtension(ext) != ""
}

// LanguageDisplayName returns the human name registered for ext, or "" when ext
// names no language this registry knows of — in which case it is not reported.
//
// It used to also return "" for any extension SupportedExtension claimed, which
// was the extractor-exists filter wearing a router-shaped disguise: those are
// different facts, and once `.vb` was routed the disguise blanked a row for a
// language nothing could extract. The filter now lives where it can be DERIVED,
// in internal/cli/unsupported_report.go, keyed on extractors.Get. This function
// is a lookup and nothing more (#6327 S2 review).
func LanguageDisplayName(ext string) string {
	return unsupportedLanguageNames[strings.ToLower(ext)]
}

// TrackingIssue returns the grafel issue reference tracking support for ext, or
// "" when there is none. A lookup, for the same reason as LanguageDisplayName.
func TrackingIssue(ext string) string {
	return unsupportedTrackingIssues[strings.ToLower(ext)]
}

// The *ForTest helpers expose package internals to the report-layer tests in
// internal/cli, which is where the F1/F2/F5 regressions are asserted end-to-end
// and — since internal/classifier cannot import internal/extractors — the only
// place the classifier↔extractor invariants can be checked at all.
func ReportableExtensionForTest(filePath string) string { return reportableExtension(filePath) }

// SeedUnsupportedLanguageNameForTest temporarily gives ext a display name and
// a tracking issue, and returns the restore func.
//
// It exists because #6327 S5 emptied the only pairing the derived-filter test
// in internal/cli could use. That test — TestUnsupportedRows_DropsRowOnceAnExtractorIsRegistered
// — proves the row disappears the moment an extractor registers for the
// extension's language, which needs an extension that is BOTH routed and
// named. `.vb` was the only one, and the INVARIANT above forbids it staying
// named now that vbnet is extractable. Without a seed the test degenerates to
// asserting an absence that two independent filters already guarantee, i.e.
// nothing.
//
// It mutates package state, so it is not safe for parallel tests. That is the
// same contract the rest of the *ForTest helpers carry.
func SeedUnsupportedLanguageNameForTest(ext, name, issue string) func() {
	ext = strings.ToLower(ext)
	unsupportedLanguageNames[ext] = name
	if issue != "" {
		unsupportedTrackingIssues[ext] = issue
	}
	return func() {
		delete(unsupportedLanguageNames, ext)
		delete(unsupportedTrackingIssues, ext)
	}
}

// LanguagesAwaitingExtractorForTest returns the sorted keys of
// languagesAwaitingExtractor so internal/cli can assert none of them is
// extractable. See that map's REMOVAL PROTOCOL.
func LanguagesAwaitingExtractorForTest() []string {
	out := make([]string, 0, len(languagesAwaitingExtractor))
	for lang := range languagesAwaitingExtractor {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out
}

// RoutedLanguagesForTest returns every distinct language slug the router can
// produce, sorted. Tests derive "routed but not extractable" from this rather
// than maintaining a fourth hand-written list of the same fact.
func RoutedLanguagesForTest() []string {
	seen := make(map[string]bool, len(extensionLanguageMap)+len(basenameLanguageMap))
	for _, lang := range extensionLanguageMap {
		seen[lang] = true
	}
	for _, lang := range basenameLanguageMap {
		seen[lang] = true
	}
	out := make([]string, 0, len(seen))
	for lang := range seen {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out
}

func UnsupportedLanguageExtensionsForTest() []string {
	out := make([]string, 0, len(unsupportedLanguageNames))
	for ext := range unsupportedLanguageNames {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}
