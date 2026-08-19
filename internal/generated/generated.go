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
// FromContent): 7,728 files scanned, 13 flagged — 12 by the marker alone and 1
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
//   - rerankScored exempts the single generated hit that outscores every
//     authored hit in the result set, so the demotion can never bury the
//     strongest match for a query.
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
	headScanBytes = 4096
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
// ONE TRUE POSITIVE IS LOST: tetratelabs/wazero's site/static/install.sh, a
// godownloader-generated script whose banner sits at line 17 BEHIND a bare
// `set -e`. That is the fail-safe direction — the file is treated as
// hand-written, which is the status quo — and buying it back would mean
// letting an arbitrary shell statement extend the header window, which is
// precisely what re-admits mkcgo.sh and mksysnum_linux.pl.
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
	for pos := 0; pos < len(head); {
		nl := bytes.IndexByte(head[pos:], '\n')
		line := head[pos:]
		next := len(head)
		if nl >= 0 {
			line = head[pos : pos+nl]
			next = pos + nl + 1
		}
		if !headerLine(line) {
			break
		}
		end = next
		pos = next
	}
	return head[:end]
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

// ---------------------------------------------------------------------------
// Markers
// ---------------------------------------------------------------------------

// commentLead matches an optional comment introducer at the start of a line:
// //, #, --, ', *, /*, <!--. The leading (?m)^ makes every marker line-anchored.
const commentLead = `(?m)^[ \t]*(?://+|#+|--+|'+|\*+|/\*+|<!--)?[ \t]*`

// commentLeadRequired is commentLead with the trailing `?` removed, so the
// introducer is MANDATORY. It is used by the `tool-generated` marker alone.
//
// "This file was generated" is ordinary English; the other markers' text
// ("Code generated ... DO NOT EDIT.", "<auto-generated", "@generated") is not
// something a person writes by accident. Requiring the lead there costs
// nothing — every real jOOQ / openapi-typescript / Microsoft banner is inside
// a comment — and it removes the one marker that could fire on a bare prose
// line in a markdown or plain-text header block.
const commentLeadRequired = `(?m)^[ \t]*(?://+|#+|--+|'+|\*+|/\*+|<!--)[ \t]*`

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
		// The lead-in is MANDATORY here, unlike every other marker — see
		// commentLeadRequired for why this one phrase needs it.
		re: regexp.MustCompile(commentLeadRequired + `Th(?:is|e) (?:file|code|class|content) (?:was|is) (?:auto[- ])?generated`),
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
