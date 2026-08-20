package feedback

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Longitudinal participation history (#6377).
//
// `kind-carries-semantic-edges[kind]` cannot tell a terminal-by-design kind
// (markdown code fences, CSS selectors, HTML input fields, requirements.txt
// entries) from a dead resolver, because both look identical in the graph:
// zero participation. Measured per group across 12 corpus repos it fired 14
// times over 10 distinct kinds with roughly 2 true positives — a ~14%
// true-positive rate, which trains the reader to skip the section.
//
// The evidence that DOES separate the two is not in the graph, it is on disk:
// `grafel feedback` has always written every report to
// ~/.grafel/feedback/<group>-<timestamp>.md (internal/cli/feedback.go). This
// file reads that history back so the gate can ask the answerable question —
// "did this kind LOSE edges it used to have?" — instead of the unanswerable
// one.
//
// Two properties of the stored format are load-bearing and are pinned by
// tests, not assumed:
//
//   - The group name appears only in the FILENAME. The report body carries a
//     "Group profile:" line with language counts, never the group name (see
//     Render). So history is scoped by filename prefix.
//
//   - Kind names survive anonymisation VERBATIM. NameHash uses the kind only
//     to select a short prefix and never emits it; PathScrub only ever sees
//     file paths. Kind is the join key of this whole comparison, so
//     TestKindNamesSurviveAnonymisation asserts the round-trip through Render
//     rather than trusting that to stay true.

// section2Heading and the row pattern below are matched against the rendered
// report, which is the only durable artifact — there is no machine-readable
// sidecar. Parsing is deliberately confined to Section 2 so rows from the
// kind×language table in Section 1 or the disposition table in Section 3
// cannot be mistaken for kinds.
const section2Heading = "## 2. Orphan Rate"

// kindRowRe matches a 4-column Section-2 table row whose last cell is a
// percentage: "| <kind> | <total> | <orphans> | <pct>% |". Both the defect
// table and the Expected/terminal table share this shape.
var kindRowRe = regexp.MustCompile(`^\|\s*([^|]+?)\s*\|\s*(\d+)\s*\|\s*(\d+)\s*\|\s*([0-9.]+)%\s*\|$`)

// reportFileRe matches the stored report filename produced by
// `grafel feedback`: "<group>-<UTC timestamp>.md".
var reportFileRe = regexp.MustCompile(`^(.*)-\d{8}T\d{6}\.md$`)

// parseKindParticipation extracts per-kind semantic participation from one
// rendered report.
//
// The returned map distinguishes three states, which is what the gate needs:
//
//	value true   — the kind was OBSERVED and DID participate
//	value false  — the kind was OBSERVED and did NOT participate
//	key absent   — the kind was not in this report at all (no history for it)
//
// A kind participates when it appears in Section 2 with an orphan rate
// strictly below 100%. That single rule covers both tables and both report
// vintages: today report.go routes zero-participation kinds into the separate
// Expected/terminal table, while reports written before #6313 left them in the
// main table at exactly 100.0%. Reading the percentage rather than the table
// header means old reports on disk are usable history, not dead weight.
func parseKindParticipation(md string) map[string]bool {
	out := make(map[string]bool)

	inSection2 := false
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		if strings.HasPrefix(trimmed, "## ") {
			inSection2 = strings.HasPrefix(trimmed, section2Heading)
			continue
		}
		if !inSection2 {
			continue
		}
		m := kindRowRe.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		kind := m[1]
		if kind == "Kind" || strings.HasPrefix(kind, "-") {
			// Header row, or the |---|---| separator.
			continue
		}
		pct, err := strconv.ParseFloat(m[4], 64)
		if err != nil {
			continue
		}
		participated := pct < 100.0
		// OR within a single report too: a kind cannot legitimately appear in
		// both tables, but if it ever did, participation is the stronger fact.
		out[kind] = out[kind] || participated
	}
	return out
}

// loadKindParticipation reads every stored report for the given group from dir
// and folds them into a single participation map with the same three states as
// parseKindParticipation.
//
// Participation is OR-ed across reports: having participated in ANY prior run
// is what makes the current zero-participation reading a regression. The most
// recent report alone is not enough — if a kind broke two runs ago, the
// previous report already shows it terminal, and comparing only against that
// would silently accept the breakage as the new normal.
//
// A missing directory is the first-run case and returns an empty map with no
// error. Unreadable individual files are skipped rather than failing the whole
// report: history is an enhancement to the gate, never a precondition for
// producing a report.
func loadKindParticipation(dir, group string) (map[string]bool, error) {
	out := make(map[string]bool)
	if dir == "" || group == "" {
		return out, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := reportFileRe.FindStringSubmatch(e.Name())
		if m == nil || m[1] != group {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for kind, participated := range parseKindParticipation(string(data)) {
			prev, seen := out[kind]
			out[kind] = (seen && prev) || participated
		}
	}
	return out, nil
}
