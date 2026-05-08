package patterns

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// snapshotTestDetector detects snapshot testing patterns.
// Matches Python snapshot_test_detector.py.
type snapshotTestDetector struct{}

var (
	stJSSnapshotRE     = regexp.MustCompile(`(?:expect\s*\(.*\)\s*\.\s*toMatchSnapshot\s*\(|\.toMatchInlineSnapshot\s*\()`)
	stPyPytestSnapRE   = regexp.MustCompile(`(?:snapshot\.assert_match|assert_match_snapshot)`)
	stPySnapshotTestRE = regexp.MustCompile(`self\.assertMatchSnapshot\b`)
	stRubyApprovalsRE  = regexp.MustCompile(`(?:Approvals\.verify|ApprovalTests)`)
	stRustInstaRE      = regexp.MustCompile(`(?:insta::assert_snapshot|assert_snapshot!|assert_yaml_snapshot!)`)
	stGoCupaloyRE      = regexp.MustCompile(`cupaloy\.SnapshotT\s*\(`)
	stJavaApprovalsRE  = regexp.MustCompile(`Approvals\.verify\s*\(`)
	stSwiftSnapshotRE  = regexp.MustCompile(`assertSnapshot\s*\(`)
)

func (s *snapshotTestDetector) Category() string { return "snapshot_test" }

func (s *snapshotTestDetector) AppliesTo(src string) bool {
	return stJSSnapshotRE.MatchString(src) ||
		stPyPytestSnapRE.MatchString(src) ||
		stPySnapshotTestRE.MatchString(src) ||
		stRubyApprovalsRE.MatchString(src) ||
		stRustInstaRE.MatchString(src) ||
		stGoCupaloyRE.MatchString(src) ||
		stJavaApprovalsRE.MatchString(src) ||
		stSwiftSnapshotRE.MatchString(src)
}

func (s *snapshotTestDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, library string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"snapshot_test_"+library, "SCOPE.Pattern", "snapshot_test", language, line,
			map[string]string{"kind": "snapshot_test", "library": library}))
	}

	if m := stJSSnapshotRE.FindStringIndex(src); m != nil {
		emit("jest:snapshot", "jest", lineOf(src, m[0]))
	}
	if m := stPyPytestSnapRE.FindStringIndex(src); m != nil {
		emit("pytest:snapshot", "pytest-snapshot", lineOf(src, m[0]))
	}
	if m := stPySnapshotTestRE.FindStringIndex(src); m != nil {
		emit("py:snapshottest", "snapshottest", lineOf(src, m[0]))
	}
	if m := stRubyApprovalsRE.FindStringIndex(src); m != nil {
		emit("ruby:approvals", "approvals", lineOf(src, m[0]))
	}
	if m := stRustInstaRE.FindStringIndex(src); m != nil {
		emit("rust:insta", "insta", lineOf(src, m[0]))
	}
	if m := stGoCupaloyRE.FindStringIndex(src); m != nil {
		emit("go:cupaloy", "cupaloy", lineOf(src, m[0]))
	}
	if m := stJavaApprovalsRE.FindStringIndex(src); m != nil {
		emit("java:approvals", "approvals-java", lineOf(src, m[0]))
	}
	if m := stSwiftSnapshotRE.FindStringIndex(src); m != nil {
		emit("swift:snapshot-testing", "swift-snapshot-testing", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&snapshotTestDetector{})
}
