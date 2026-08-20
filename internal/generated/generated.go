// Package generated detects machine-generated source files (#6329).
//
// # Why this is not the thing #6330 deleted
//
// The 31 internal/engine/rules/*/skip_patterns.yaml files removed in #6330
// were a configuration file with no reader: classifier.New only populated its
// glob list when handed a non-empty yamlDataDir and all four production call
// sites passed "". They therefore never executed and were never validated —
// 63% of their patterns could not match under filepath.Match, and switching
// the rest on would have dropped every *_test.go in the repository.
//
// This package is deliberately the opposite shape:
//
//   - It is code, not config. There is no YAML, no data directory, no loader.
//   - Every rule in pathRules carries its own positive AND negative cases, and
//     a test fails the build if either is missing.
//   - The matcher is stated and tested per pattern class rather than inherited
//     from filepath.Match (hazard 5 in docs/generated-source-patterns.md).
//   - It ships with a consumer in the same change (ranking, #6314).
//
// # Detection order and why the marker leads
//
// Two signals, combined by OR:
//
//	path    — a filename pattern from a generator whose naming is unambiguous
//	marker  — an anchored comment header emitted by the generator itself
//
// The marker does almost all of the work. Re-measured on this repository with
// the shipped package AFTER the first-comment-block narrowing (see
// FromContent): 7,730 files scanned, 13 flagged — 12 by the marker alone and 1
// by the filename table, and that one carries a marker as well. So the
// filename table has yet to flag a single file the marker would have missed
// here. That matches the argument in #6321 — a content marker catches
// generators nobody enumerated, and it is checkable evidence about a file
// rather than a naming convention.
//
// EVERY NUMBER IN THIS COMMENT IS A MEASUREMENT, NOT AN ESTIMATE, and it was
// wrong twice before: the package doc claimed 14 and the PR body claimed 12
// while the truth was 13. Re-run internal/generated's sweep before editing it.
//
// # Fail-safe direction
//
// Detection can only ever mark a file GENERATED. It never marks a file
// skipped, and nothing in this package removes a file from the index or from
// the graph. A missed detection is the status quo: the file is treated as
// hand-written.
//
// A WRONG DETECTION COSTS RANKING POSITIONS. This comment used to say it costs
// "some ranking position, not its entities", and in the default MCP output
// path that was FALSE. internal/mcp made the demotion an absolute partition,
// and the two default views keep only the first few rows — per-repo top 3 in a
// group, the first 10 in single-repo compact mode — so three weak authored
// matches were enough to remove a generated hit from the default view
// entirely, silently. A wrongly flagged hand-written file did not slip down
// the list, it VANISHED.
//
// Two changes in internal/mcp make the sentence true rather than aspirational,
// and neither lives here, so this comment is a pointer to them rather than a
// promise on their behalf:
//
//   - rerankScored exempts each repo's top-RANKED hit when that hit is
//     generated, so the demotion can never bury the strongest match for a
//     query. The rule is positional, not a score comparison: hit.Score is an
//     RRF reciprocal wherever a repo has an embeddings sidecar, and a score
//     rule was therefore green without embeddings and inert with them.
//   - both truncating default views now emit a truncation_note, so a row that
//     was dropped is distinguishable from a row that does not exist.
//
// The entities themselves are never touched either way. That asymmetry is why
// the marker alone is sufficient to flip a file (#6329, second comment) — and
// why a filename rule that only fires WITH a marker would be dead weight,
// since the marker already flipped the file. PR1's table therefore contains
// unambiguous rules only.
package generated

import (
	"bytes"
	"path"
	"regexp"
	"strings"
)

// Detection is the outcome of inspecting one file.
type Detection struct {
	// Generated is true when the file is machine-generated.
	Generated bool

	// Rule is the provenance of the decision: "path:<pattern>" or
	// "marker:<name>". Empty when Generated is false.
	//
	// This is stamped onto every entity alongside the flag itself. A wrong
	// flag with no provenance is undiagnosable in the field, and given how
	// close the JPA @GeneratedValue near-miss was (101 hand-written classes on
	// the corpus under a case-insensitive scan) we should assume there will
	// eventually be a wrong flag.
	Rule string
}

// Scan window for the marker pass. Generator headers are the first thing in
// the file by construction — no generator buries its own banner. Bounding the
// scan keeps the cost independent of file size, which matters because the
// pathological inputs here are exactly the large ones (minified bundles, a
// 40k-line designer file).
const (
	headScanLines = 20

	// headerSkipBudget caps how many non-comment STATEMENT lines the header
	// block may step over before it closes. #6345 closed it on the first one,
	// which made every banner sitting behind a leading statement invisible —
	// `set -e` in godownloader's install.sh, `using System;`, `Imports System`,
	// `from __future__ import ...`. See #6348.
	//
	// It is a budget rather than a wider headerLinePrefixes on purpose: an
	// enumeration of per-language leading statements makes every form nobody
	// thought to list silently undetectable, which is the #6344 failure shape.
	// A budget names no language and so cannot be incomplete.
	//
	// WHAT THIS CONSTANT DOES NOT DO, because the obvious reading is wrong and
	// was measured wrong: it is not what holds the #6345 false positives out.
	// Raising it to 3, 4, 6 or 10 leaves every one of them dead, because
	// skippableStatement rejects their next line before the budget is reached.
	// The budget is the independent ceiling for the shapes NOBODY MEASURED — it
	// bounds how far a chain of statements that all look skippable can carry the
	// window, and that is the only claim it can support.
	//
	// Two is chosen from the other side, as the smallest value covering every
	// shape #6348 lists: the deepest is Java's package plus two imports, and
	// package is a declarationLine already. Widening it needs a corpus sweep,
	// not an argument.
	headerSkipBudget = 2
	headScanBytes    = 4096
)

// Detect combines both signals for one file. rel is the repository-relative
// path; content may be the whole file (only the head is read) or nil.
//
// The path signal is evaluated first so that provenance is deterministic when
// both fire. Provenance that varies with evaluation order is not diagnosable.
func Detect(rel string, content []byte) Detection {
	if d := FromPath(rel); d.Generated {
		return d
	}
	return FromContent(content)
}

// FromPath applies the filename rules. No IO.
func FromPath(rel string) Detection {
	norm := normalise(rel)
	if norm == "" {
		return Detection{}
	}
	for _, r := range pathRules {
		if matchPattern(r.Pattern, norm) {
			return Detection{Generated: true, Rule: "path:" + r.Pattern}
		}
	}
	return Detection{}
}

// FromContent scans the header of a file for a generator marker.
//
// Three properties are load-bearing, not stylistic:
//
//   - The regexes are case-SENSITIVE. Case-insensitivity makes `@generated`
//     match JPA's `@GeneratedValue`, which flags 101 hand-written entity
//     classes on the measurement corpus.
//   - They are anchored to the start of a line behind a comment lead-in.
//   - The scan is restricted to the file's FIRST CONTIGUOUS COMMENT BLOCK
//     (headerBlock), not merely to its first few lines.
//
// THE THIRD ONE IS THE FALSE-POSITIVE FIX AND IT IS NOT OPTIONAL. This package
// originally argued that the `(?m)^` anchor alone kept prose about generated
// code from matching. That argument was WRONG and was measured wrong: `^` is
// start-of-LINE, not start-of-FILE, and prose inside source almost always
// starts a line. A sweep over ~207k files in the Go module cache flagged 8,236
// of them, in three systematic classes of hand-written source:
//
//   - generator SCRIPTS carrying the banner they emit, in a heredoc or a
//     quoted string (mksysnum_linux.pl, mkcgo.sh, mkstd.sh);
//   - DOCUMENTATION about the marker — a PR template quoting it in a fenced
//     code block, a README describing a sibling file;
//   - hand-written TEST DRIVERS whose mid-function comment describes the data
//     file being read ("This file was generated from monsterdata_test.json").
//
// Every one of those matched at lines 12-18; every real generator banner in
// the corpus sat at lines 1-5. The boundary drawn here is that fact stated as
// a rule rather than as a magic number: a generated-file banner is part of the
// file's HEADER, so the scan stops at the first line that is not a comment,
// blank, or a language preamble directive.
//
// TestFromContent_OnlyTheFirstCommentBlockIsScanned reproduces all three
// classes synthetically; TestFromContent_HandWrittenNearMisses keeps the
// original header-shaped near-misses pinned.
//
// MEASURED EFFECT, same method before and after (11,740 module-cache files
// pre-filtered with grep for any marker string, then run through this
// package): 8,544 flagged before, 8,506 after. 38 files removed, 0 added.
// The overwhelming majority of the 8,506 are genuinely generated (*.pb.go and
// friends) — the sweep was never a false-positive count, and the fix was never
// going to move it much. What matters is the CONTENT of the 38: every one is
// hand-written source, and they are exactly the three classes above, with one
// exception recorded below.
//
// ONE MEASURABLE TRUE POSITIVE WAS LOST BY #6345 — AND #6348 BOUGHT IT BACK.
// The file is tetratelabs/wazero's site/static/install.sh, a
// godownloader-generated script whose banner sits at line 17 BEHIND a bare
// `set -e`. It is detected again as of #6348.
//
// The reasoning that once justified leaving it lost is kept here because it is
// part of the record and because it is now DISPROVEN by the code below. #6345
// argued that buying it back "would mean letting an arbitrary shell statement
// extend the header window, which is precisely what re-admits mkcgo.sh and
// mksysnum_linux.pl". The skip #6348 adds is not arbitrary: headerSkipBudget
// bounds how many statements may be stepped over, and skippableStatement's
// three refusal clauses — unbalanced quote, opening brace, prose length — are
// what hold the #6345 false positives out. mkcgo.sh trips the quote clause on
// its `hdr='` line before any budget is consumed; see
// TestFromContent_SkippedStatementOpeningAStringEndsTheBlock and
// TestFromContent_ProseAndBodyLinesAreNotSkippable.
//
// "Measurable" was doing real work in #6345's sentence and still is: the
// corpus is ~all Go, and Go idiom puts the banner FIRST, so a Go-only sweep
// CANNOT SIZE THIS CLASS AT ALL. That caveat is still true, it is why this bug
// survived a measured narrowing, and re-sizing the class against a polyglot
// corpus is tracked in #6396.
//
// THE SHAPES #6345'S SWEEP WAS BLIND TO, probed directly against FromContent
// rather than inferred, and every one of them a MISS at that time. This list
// is now precisely the set #6348 FIXES — each shape has a case in the
// table-driven TestFromContent_BannerBehindLeadingStatements. It is kept
// verbatim as the historical record of what was broken and as the enumeration
// the fix is measured against:
//
//   - `sh` with `set -e` (or `set -euo pipefail`) before the banner — the
//     wazero class, i.e. all of godownloader/goreleaser, an ecosystem and not
//     one file;
//   - a Python module-docstring banner: `"""` is not in headerLinePrefixes at
//     all, so the block closes on line 1;
//   - Python `from __future__ import ...` before the banner;
//   - C# `using System;` before `<auto-generated />`;
//   - VB `Imports System` before `'<auto-generated>` — this one matters
//     because VB.NET designer files are the next cycle's target;
//   - Kotlin `@file:Suppress(...)`, PHP `declare(strict_types=1);`, Scala
//     imports, SQL after `USE db;`, Java package + imports;
//   - PHP `namespace Acme\Gen;` — declarationLine's identifier class has no
//     backslash, so only a single-segment PHP namespace keeps the block open.
//
// Controls that DO hit, so the window is not simply broken: a `//go:build`
// tag, a bare `package` line, an eslint-disable block, `# frozen_string_literal`,
// a Rust inner attribute, a `#include`, and a comment-only licence header of
// any length (comment lines keep the block open by construction).
//
// POLICY ON headerLinePrefixes, reconciled with what #6348 actually changed:
// broad widening remains deliberately NOT done, because an enumeration of
// per-language leading statements is the same knob that re-admits the false
// positives #6345 removed, and because anything nobody thought to list stays
// silently undetectable — the #6344 failure shape. That is why the fix is a
// bounded skip.
//
// #6348 does nonetheless make ONE addition to the slice: `"""`. Stating it
// plainly rather than leaving the paragraph above to contradict the diff. It
// is consistent with the policy rather than an exception to it, because the
// Python docstring is not a statement standing BEFORE the banner that the skip
// could step over — it is the banner's own delimiter, with the marker text
// living INSIDE the block it opens. No budget reaches it. It is therefore also
// listed in blockOpener's pairs, so the region it opens is terminated by the
// matching `"""` instead of running to the end of the head window: the window
// grows by a bounded, self-closing region, not by an open-ended prefix class.
// Widening the slice with any prefix that lacks a closer still needs its own
// measured sweep (#6396).
func FromContent(content []byte) Detection {
	head := headerBlock(headOf(content))
	if len(head) == 0 {
		return Detection{}
	}
	for _, m := range markers {
		if m.re.Match(head) {
			return Detection{Generated: true, Rule: "marker:" + m.Name}
		}
	}
	return Detection{}
}

// headOf returns the first headScanLines lines, capped at headScanBytes.
// The byte cap is applied first so a single enormous line (a minified bundle
// is one line) cannot defeat the line budget.
func headOf(content []byte) []byte {
	if len(content) == 0 {
		return nil
	}
	if len(content) > headScanBytes {
		content = content[:headScanBytes]
	}
	for i, n := 0, 0; i < len(content); i++ {
		if content[i] == '\n' {
			n++
			if n == headScanLines {
				return content[:i]
			}
		}
	}
	return content
}

// utf8BOM is stripped before the header block is measured. A byte-order mark
// is invisible to a human reading line 1 but would otherwise sit in front of
// the comment lead-in and defeat the anchor.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// headerBlock narrows head to the file's first contiguous comment block: every
// line up to (but not including) the first line that is not a comment, not
// blank, and not a language preamble directive.
//
// This is the false-positive boundary described on FromContent. It is a
// statement about file structure, not a tuned constant — moving it would
// require an argument about where generator banners live, and the corpus says
// they live in the header.
func headerBlock(head []byte) []byte {
	head = bytes.TrimPrefix(head, utf8BOM)
	end := 0
	// closer is non-empty while we are inside a /* … */ or <!-- … --> block.
	// Inside one, EVERY line belongs to the header regardless of how it starts:
	// "the first contiguous comment block" has to mean the whole block, not
	// only the lines that happen to repeat a lead character. A jOOQ or licence
	// banner written without a leading `*` on every line is common, and a
	// prefix-only rule would stop reading at line 2 and miss it entirely.
	var closer []byte
	// skipped counts the non-comment STATEMENT lines stepped over so far; see
	// headerSkipBudget.
	skipped := 0
	for pos := 0; pos < len(head); {
		nl := bytes.IndexByte(head[pos:], '\n')
		line := head[pos:]
		next := len(head)
		if nl >= 0 {
			line = head[pos : pos+nl]
			next = pos + nl + 1
		}
		if len(closer) > 0 {
			if bytes.Contains(line, closer) {
				closer = nil
			}
		} else if headerLine(line) {
			closer = blockOpener(line)
		} else {
			if skipped == headerSkipBudget || !skippableStatement(line) {
				break
			}
			skipped++
		}
		end = next
		pos = next
	}
	return head[:end]
}

// blockOpener returns the closing delimiter a line leaves open, or nil.
//
// A block that opens AND closes on the same line leaves nothing open, so
// `/* one-liner */` does not swallow the rest of the file.
func blockOpener(line []byte) []byte {
	t := bytes.TrimLeft(line, " \t")
	for _, b := range [...]struct{ open, close string }{
		{"/*", "*/"},
		{"<!--", "-->"},
		// Python's triple-quoted module docstring is that language's header
		// comment: a generated .py routinely carries its banner inside one, or
		// below one. Neither the opener nor the interior starts with a comment
		// introducer, so without this the block closes on line 1.
		{`"""`, `"""`},
		{`'''`, `'''`},
	} {
		if !bytes.HasPrefix(t, []byte(b.open)) {
			continue
		}
		if bytes.Contains(t[len(b.open):], []byte(b.close)) {
			return nil
		}
		return []byte(b.close)
	}
	return nil
}

// headerLinePrefixes are the line starts that keep the header block open.
//
// The comment introducers mirror commentLead exactly — a line that cannot
// carry a marker must not be able to extend the window that looks for one.
// `*/` and `-->` are the closers of the block-comment forms already listed.
//
// The three PREAMBLE directives are the exception, and each is here because
// the language REQUIRES it to precede the banner: a shebang on a generated
// script, `<?php` on protoc's PHP output, `<?xml` on generated XAML/resx.
// Treating them as terminators would blind the detector to real generator
// output — see TestFromContent_LanguagePreamblesDoNotEndTheHeaderBlock.
var headerLinePrefixes = []string{
	// comment introducers (must stay in sync with commentLead)
	"//", "#", "--", "'", "*", "/*", "<!--",
	// Python triple-quoted docstring. The `'''` spelling is already
	// covered by the VB `'` entry; `"""` needs its own listing.
	`"""`,
	// block-comment closers
	"*/", "-->",
	// language preamble directives
	"#!", "<?php", "<?xml", "<?PHP", "<?XML", "<!DOCTYPE",
}

// declarationLine matches a package / module / namespace statement standing
// alone on its line.
//
// It keeps the header block open for exactly one reason, and it is a measured
// one: a first cut of this narrowing lost three genuinely generated files in
// the module cache (xo/terminfo capvals.go, gopher-lua state.go and vm.go)
// whose banner sits at line 3 BEHIND `package X`. Non-idiomatic for Go, which
// puts the banner first, but real — and in Java and C# the package/namespace
// statement is REQUIRED to precede everything else in the file, so the shape
// is not even unusual there.
//
// The pattern is deliberately narrow: a keyword, whitespace, one dotted
// identifier, an optional `;` or `{`, end of line. It does not match
// `module.exports = {` (no space after the keyword), and it does not match an
// import, a `use`, a `require`, or a shell assignment — all of which appear as
// the FIRST non-comment line of a false positive in the measured set and must
// therefore keep closing the block.
var declarationLine = regexp.MustCompile(`^(?i:package|module|namespace|unit)[ \t]+[\w.$/-]+[ \t]*[;{]?$`)

// headerLine reports whether line keeps the header block open. Blank lines do
// — a licence header separated from the banner by an empty line is the normal
// shape — but anything that is actual code closes it.
func headerLine(line []byte) bool {
	t := bytes.TrimLeft(line, " \t")
	t = bytes.TrimRight(t, " \t\r")
	if len(t) == 0 {
		return true
	}
	for _, p := range headerLinePrefixes {
		if bytes.HasPrefix(t, []byte(p)) {
			return true
		}
	}
	return declarationLine.Match(t)
}

// maxStatementWords is the longest a skipped line may be, in whitespace-
// separated fields, and still read as a STATEMENT rather than prose.
//
// It exists because the #6345 false-positive set is largely English: the
// aws-sdk PULL_REQUEST_TEMPLATE and the traceviewer README both quote a
// generator banner a few lines into ordinary paragraphs. Under a bare skip
// budget those paragraphs are just "lines", and the banner comes back. A
// leading statement is short by nature — `set -e`, `using System;`,
// `import java.util.List;`, `declare(strict_types=1);` — while a sentence is
// not, so length separates the two classes without naming a language.
//
// Six is comfortably above the longest statement in the #6348 set
// (`from __future__ import annotations`, four) and comfortably below the
// shortest measured prose line (seven).
//
// RESIDUAL FALSE NEGATIVES this bound leaves, probed against FromContent and
// recorded so the next widening has a starting list rather than an argument:
// `SET SESSION TRANSACTION ISOLATION LEVEL READ COMMITTED;` is seven fields
// and so is not skipped; nor are annotations carrying seven or more arguments.
// Two more miss on the sibling clauses rather than on length —
// `set -e # don't stop` leaves an apostrophe unbalanced, and Rust
// `use foo::Bar<'a>;` does the same. All four fail in the SAFE direction: the
// header block closes early and the banner is treated as absent, which is the
// pre-#6348 status quo for those files. Raising the bound to admit them costs
// prose lines, so it needs a sweep (#6396), not a nudge.
const maxStatementWords = 6

// skippableStatement reports whether a non-comment line may be stepped over
// while looking for the header banner. Three ways it can fail, each of which
// is a measured false positive from #6345 rather than a style preference:
//
//   - it leaves a QUOTE UNBALANCED, so the lines after it are string data and
//     not header. This is runtime/race/mkcgo.sh, the sharpest case #6345
//     removed: a hand-written generator script that assigns the banner it
//     EMITS to a shell variable, with `hdr='` alone on its own line. Skipping
//     that line would read "Code generated by mkcgo.sh. DO NOT EDIT." out of
//     the string and call the script generated.
//   - it OPENS A BRACE, so the file body has started and anything below is
//     code, not header. This is flatbuffers' JavaScriptTest.js, whose
//     mid-function comment describes the data file it reads.
//   - it is longer than maxStatementWords, i.e. it is PROSE.
//
// All three fail in the safe direction: a wrongly rejected line closes the
// header block early, which is exactly the pre-#6348 status quo for that file.
func skippableStatement(line []byte) bool {
	t := bytes.TrimRight(bytes.TrimLeft(line, " \t"), " \t\r")
	if bytes.HasSuffix(t, []byte("{")) {
		return false
	}
	if bytes.Count(t, []byte("'"))%2 == 1 || bytes.Count(t, []byte(`"`))%2 == 1 {
		return false
	}
	return len(bytes.Fields(t)) <= maxStatementWords
}

// ---------------------------------------------------------------------------
// Markers
// ---------------------------------------------------------------------------

// commentLead matches an optional comment introducer at the start of a line:
// //, #, --, ', *, /*, <!--. The leading (?m)^ makes every marker line-anchored.
const commentLead = `(?m)^[ \t]*(?://+|#+|--+|'+|\*+|/\*+|<!--)?[ \t]*`

// A MANDATORY comment lead-in for the `tool-generated` marker was considered
// and rejected. It would be inert: after the header narrowing the only
// non-comment lines that can appear inside a header block are blanks, language
// preambles and a lone package statement, none of which can carry the marker
// text. And once block-comment INTERIORS became part of the header (see
// headerBlock), it would be actively wrong — those interior lines are exactly
// the ones written without a lead character, which is how a jOOQ banner inside
// a bare `/* … */` is spelled. A rule that can never change an outcome is the
// #6330 pattern; one that changes it in the wrong direction is worse.

type marker struct {
	Name string
	re   *regexp.Regexp
}

// markers is ordered; the first match wins and names the provenance. Each
// entry's Name appears verbatim in TestFromContent_RealGeneratorHeaders
// against a header taken from a real generator.
var markers = []marker{
	{
		// Go's own convention, specified in the toolchain docs. Also emitted
		// by flatc, MockGen, stringer, sqlc, protoc-gen-go and controller-gen.
		// The `DO NOT EDIT.` clause is required — a bare "DO NOT EDIT" warning
		// in a hand-written file must not match.
		Name: "go-do-not-edit",
		re:   regexp.MustCompile(commentLead + `Code generated .{0,200}? DO NOT EDIT\.`),
	},
	{
		// Microsoft tooling across C#, VB.NET and XAML. This is the #6321
		// category: WSDL/XSD partial classes matching no filename convention.
		Name: "auto-generated-tag",
		re:   regexp.MustCompile(commentLead + `<auto-generated`),
	},
	{
		Name: "protoc",
		re:   regexp.MustCompile(commentLead + `Generated by the protocol buffer compiler`),
	},
	{
		// jOOQ ("This file is generated by jOOQ"), openapi-typescript
		// ("This file was auto-generated by openapi-typescript"), and the
		// second line of the Microsoft banner ("This code was generated by a
		// tool"). Both real hits on the measurement corpus came through here.
		Name: "tool-generated",
		re:   regexp.MustCompile(commentLead + `Th(?:is|e) (?:file|code|class|content) (?:was|is) (?:auto[- ])?generated`),
	},
	{
		// Meta's convention, also emitted by Relay and graphql-codegen. Must
		// be case-sensitive: `@Generated` is a Java annotation and
		// `@GeneratedValue` is JPA's primary-key strategy, neither of which
		// means the file is generated.
		Name: "at-generated",
		re:   regexp.MustCompile(commentLead + `@generated\b`),
	},
}

// ---------------------------------------------------------------------------
// Filename rules
// ---------------------------------------------------------------------------

// Rule is one filename pattern plus the evidence that it is right.
//
// Positive and Negative are not documentation: TestRuleTable_CasesHold runs
// them through the public entry points, and
// TestRuleTable_EveryRuleCarriesPositiveAndNegativeCases fails if either is
// missing. That is the gate the deleted config never had.
type Rule struct {
	// Pattern is matched by matchPattern (see its doc for the semantics).
	Pattern string

	// Generator names the tool that emits these files.
	Generator string

	// Positive are paths this rule must match.
	Positive []string

	// Negative are paths that must NOT be flagged by ANY rule in the table.
	Negative []string
}

// pathRules holds only patterns whose naming is unambiguous — a file matching
// one of these is generated in every codebase, not merely in most.
//
// Deliberately ABSENT, each for a recorded reason:
//
//	*_test.go       — not generated. The deleted config would have dropped
//	                  every Go test file in the repository (hazard 1).
//	build.rs        — authored build metadata, not generator output (hazard 2).
//	*.g.cs          — collides with hand-written files (hazard 3). Roslyn's
//	                  real output carries <auto-generated>, so the marker
//	                  covers it without the false positives.
//	*.d.ts          — has authored exceptions; the same problem as *.g.cs.
//	mock_*.go       — collides with hand-written fakes. gomock stamps
//	*_mock.go         "Code generated by MockGen. DO NOT EDIT.", so the marker
//	                  covers the generated ones exactly.
//	*_string.go     — stringer output carries the Go marker; the suffix alone
//	                  matches hand-written helpers.
//	AssemblyInfo.cs — authored in most projects.
//	GlobalUsings.cs
//
// Sources: docs/generated-source-patterns.md, cross-checked against the
// originals recovered from 753771635 as that document instructs.
var pathRules = []Rule{
	{
		Pattern:   "*.pb.go",
		Generator: "protoc-gen-go",
		Positive:  []string{"api/v1/user.pb.go", "user.pb.go", "a[b.pb.go"},
		Negative:  []string{"internal/pb/loader.go", "api/v1/user_pb_test.go"},
	},
	{
		Pattern:   "*.pb.gw.go",
		Generator: "grpc-gateway",
		Positive:  []string{"api/v1/user.pb.gw.go"},
		Negative:  []string{"api/v1/gateway.go"},
	},
	{
		Pattern:   "wire_gen.go",
		Generator: "google/wire",
		Positive:  []string{"internal/di/wire_gen.go", "wire_gen.go"},
		Negative:  []string{"internal/di/wire.go", "internal/di/wire_gen_test.go"},
	},
	{
		Pattern:   "zz_generated*.go",
		Generator: "controller-gen (Kubernetes operators)",
		Positive:  []string{"api/v1/zz_generated.deepcopy.go", "api/v1/zz_generated_defaults.go"},
		Negative:  []string{"api/v1/generated.go", "api/v1/types.go"},
	},
	{
		Pattern:   "*_pb2.py",
		Generator: "protoc (python)",
		Positive:  []string{"proto/user_pb2.py"},
		Negative:  []string{"proto/user_pb.py", "proto/pb2_helpers.py"},
	},
	{
		Pattern:   "*_pb2_grpc.py",
		Generator: "protoc (python grpc plugin)",
		Positive:  []string{"proto/user_pb2_grpc.py"},
		Negative:  []string{"proto/grpc_client.py"},
	},
	{
		Pattern:   "*_pb2.pyi",
		Generator: "protoc (python stubs)",
		Positive:  []string{"proto/user_pb2.pyi"},
		Negative:  []string{"proto/user.pyi"},
	},
	{
		Pattern:   "*.Designer.cs",
		Generator: "WinForms designer",
		Positive:  []string{"Forms/MainForm.Designer.cs"},
		Negative:  []string{"Forms/MainForm.cs", "Forms/Designer.cs"},
	},
	{
		Pattern:   "*.Designer.vb",
		Generator: "WinForms designer (VB.NET)",
		Positive:  []string{"Forms/MainForm.Designer.vb"},
		Negative:  []string{"Forms/MainForm.vb"},
	},
	{
		Pattern:   "*.pb.swift",
		Generator: "SwiftProtobuf",
		Positive:  []string{"Sources/Proto/user.pb.swift"},
		Negative:  []string{"Sources/Proto/User.swift"},
	},
	{
		Pattern:   "*.grpc.swift",
		Generator: "grpc-swift",
		Positive:  []string{"Sources/Proto/user.grpc.swift"},
		Negative:  []string{"Sources/Net/GRPCClient.swift"},
	},
	{
		Pattern:   "*.g.dart",
		Generator: "build_runner (json_serializable, drift, floor, isar)",
		Positive:  []string{"lib/models/user.g.dart"},
		Negative:  []string{"lib/models/user.dart"},
	},
	{
		Pattern:   "*.freezed.dart",
		Generator: "freezed",
		Positive:  []string{"lib/models/user.freezed.dart"},
		Negative:  []string{"lib/models/frozen_user.dart"},
	},
	{
		Pattern:   "*.pb.cc",
		Generator: "protoc (c++)",
		Positive:  []string{"proto/user.pb.cc"},
		Negative:  []string{"src/user.cc"},
	},
	{
		Pattern:   "*_pb.rb",
		Generator: "protoc (ruby)",
		Positive:  []string{"lib/proto/user_pb.rb"},
		Negative:  []string{"lib/proto/user.rb"},
	},
	{
		// The one path-shaped rule in the table, kept deliberately: it is the
		// pattern class that filepath.Match could never express, and hazard 5
		// requires the matcher to be exercised per class rather than assumed.
		Pattern:   "**/__generated__/**",
		Generator: "Relay / graphql-codegen",
		Positive:  []string{"src/gql/__generated__/types.ts", "__generated__/types.ts"},
		Negative:  []string{"src/generated/types.ts", "src/gql/__generated__.ts"},
	},
}

// ---------------------------------------------------------------------------
// Matcher
// ---------------------------------------------------------------------------

// matchPattern reports whether path (repository-relative, slash-separated,
// already normalised) matches pattern.
//
// Semantics are STATED here rather than inherited, per hazard 5 of
// docs/generated-source-patterns.md. filepath.Match has no ** and does not
// cross separators, which silently made 155 of the deleted config's 247
// parsed patterns permanently inert.
//
//	no "/" in pattern   matched against the BASENAME, at any depth
//	leading "/"         anchored at the repository root, matched against
//	                    the whole path
//	other patterns      matched against the whole path
//	*                   any run of characters, NOT crossing "/"
//	**                  any run of characters, INCLUDING "/"
//	**/                 as above, and also matches zero directories
//	?                   exactly one character, not "/"
//
// Everything else in the pattern is a literal, so metacharacters appearing in
// a real path (a "[" in a filename) are never interpreted.
func matchPattern(pattern, p string) bool {
	if pattern == "" || p == "" {
		return false
	}
	subject := p
	if !strings.Contains(pattern, "/") {
		subject = path.Base(p)
	} else if strings.HasPrefix(pattern, "/") {
		pattern = strings.TrimPrefix(pattern, "/")
	}
	re := compilePattern(pattern)
	return re != nil && re.MatchString(subject)
}

// patternCache memoises compiled patterns. The table is fixed at build time,
// so this fills once and is read-only thereafter; it is guarded because
// extraction is concurrent.
var patternCache = newRegexCache()

func compilePattern(pattern string) *regexp.Regexp {
	if re, ok := patternCache.get(pattern); ok {
		return re
	}
	re := regexp.MustCompile("^" + globToRegex(pattern) + "$")
	patternCache.put(pattern, re)
	return re
}

// globToRegex translates the stated glob syntax into a regular expression.
func globToRegex(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				// "**/" also matches zero directories, so that
				// "**/__generated__/**" matches a repo-root __generated__.
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					b.WriteString(`(?:.*/)?`)
					i += 2
					continue
				}
				b.WriteString(`.*`)
				i++
				continue
			}
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// normalise converts a path to the form the matcher expects: forward slashes,
// no leading "./", no leading "/".
func normalise(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	return p
}
