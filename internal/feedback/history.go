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
//     Render). So history is scoped by an EXACT match on the filename stem
//     before the timestamp — not a prefix: group "foo" does not read
//     "foo-bar-<ts>.md".
//
//   - Kind names survive anonymisation VERBATIM. NameHash uses the kind only
//     to select a short prefix and never emits it; PathScrub only ever sees
//     file paths. Kind is the join key of this whole comparison, so
//     TestKindNamesSurviveAnonymisation asserts the round-trip through Render
//     rather than trusting that to stay true.
//
// ONE-WAY DOOR, stated because nothing else documents it: participation is
// OR-ed across every stored report and never expires (loadKindParticipation).
// That is right for the common case — a kind that broke two runs ago must not
// launder its breakage into the new normal — but it means a kind that
// legitimately and PERMANENTLY stops participating (its repo left the group,
// its extractor was retired, the language was dropped) raises
// `kind-participation-not-regressed[kind]` on every run from then on, with no
// code change able to clear it. The only remedy is to delete this group's
// stored reports, `~/.grafel/feedback/<group>-*.md`, which resets the group to
// first-run behaviour. The check's own note says so, so the reader never has
// to find this comment.

// section2Heading and the row patterns below are matched against the rendered
// report, which is the only durable artifact — there is no machine-readable
// sidecar. Parsing is deliberately confined to Section 2 so rows from the
// kind×language table in Section 1 or the disposition table in Section 3
// cannot be mistaken for kinds.
const section2Heading = "## 2. Orphan Rate"

// directionAwareMarker is the Section-2 preamble Render has emitted since
// #6375 (c6e6e148c, 2026-08-20). It is the version gate for history.
//
// Before that commit an entity was orphan when it had no OUTGOING semantic
// edge; today it is orphan only when it has no semantic edge in EITHER
// direction. That is not a cosmetic rewording — it changes what the numbers
// in Section 2 MEAN. A pure sink (SCOPE.ExceptionType, SCOPE.Package,
// SCOPE.Plugin, SCOPE.Config: pointed at by THROWS/IMPORTS, sourcing nothing)
// reads 100.0% under the old metric while participating fully. Read as
// history, such a report records those kinds as "observed, never
// participated", which is precisely the state that SILENCES check 2b — so a
// dead THROWS or IMPORTS resolver would raise nothing at all. Both reports
// currently on disk are of that vintage and misclassify 8 kinds that way.
//
// A silent gate is worse than no history, so pre-#6375 files are ignored
// outright and the group falls back to first-run behaviour.
//
// The marker is the report's own definition sentence rather than the
// "grafel version:" line: the version line exists in both vintages but carries
// `git describe` strings (v0.1.8-4-g61c99101b, v0.1.7.4-29-gf1bdcce5a-dirty)
// that no total order can be recovered from, while the definition sentence is
// exactly what changed and is verifiable in the files on disk today.
const directionAwareMarker = "no semantic edge in EITHER direction"

// defectTableHeader and terminalTableHeader identify which of the two
// Section-2 tables the following rows belong to. Membership of the terminal
// table — not the printed percentage — is what says a kind has no semantic
// participation; see parseKindParticipation.
const (
	defectTableHeader   = "| Kind | Total | Orphan | Orphan % |"
	terminalTableHeader = "| Kind | Total | Terminal orphan | Terminal orphan % |"
)

// kindRowRe matches a 4-column Section-2 table row whose last cell is a
// percentage: "| <kind> | <total> | <orphans> | <pct>% |". Both the defect
// table and the Expected/terminal table share this shape.
var kindRowRe = regexp.MustCompile(`^\|\s*([^|]+?)\s*\|\s*(\d+)\s*\|\s*(\d+)\s*\|\s*([0-9.]+)%\s*\|$`)

// reportFileRe matches the stored report filename produced by
// `grafel feedback`: "<group>-<UTC timestamp>.md". The capture is the whole
// stem before the timestamp, and it is compared with ==, so matching is
// exact-stem, not prefix.
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
// A report that predates #6375 yields an EMPTY map: its orphan metric is not
// the current one (see directionAwareMarker), so it carries no comparable
// evidence in either direction.
//
// Participation is read from the two Section-2 tables structurally, not from
// the printed percentage:
//
//   - Terminal table ⇒ NOT participating. This is authoritative. Generate puts
//     every kind with Total >= 10 into OrphanByKind as well, so a terminal
//     kind ALSO appears in the defect table — with 0 orphans, because its
//     orphans are counted in the terminal map. Reading that 0.0% row as
//     participation would record every CSS stylesheet and markdown fence as
//     having had semantic edges and re-fire the regression gate on them
//     forever, which is the #6377 false positive rebuilt.
//
//   - Defect table ⇒ participating iff Orphan < Total, compared on the two
//     INTEGER columns. The percentage column is rendered at %.1f, so 9999 of
//     10000 orphans prints as "100.0%"; keying off it would file a kind that
//     did participate as never having done so and silently absorb a later real
//     regression. The integer columns are exact.
func parseKindParticipation(md string) map[string]bool {
	out := make(map[string]bool)
	if !strings.Contains(md, directionAwareMarker) {
		return out
	}

	const (
		tableNone = iota
		tableDefect
		tableTerminal
	)

	inSection2 := false
	table := tableNone
	terminal := make(map[string]bool)
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		if strings.HasPrefix(trimmed, "## ") {
			inSection2 = strings.HasPrefix(trimmed, section2Heading)
			table = tableNone
			continue
		}
		if !inSection2 {
			continue
		}
		switch trimmed {
		case defectTableHeader:
			table = tableDefect
			continue
		case terminalTableHeader:
			table = tableTerminal
			continue
		}
		if table == tableNone {
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
		if table == tableTerminal {
			terminal[kind] = true
			out[kind] = false
			continue
		}
		total, err1 := strconv.Atoi(m[2])
		orphans, err2 := strconv.Atoi(m[3])
		if err1 != nil || err2 != nil || total <= 0 {
			continue
		}
		// OR within a single report: participation is the stronger fact —
		// except against the terminal table, which always wins.
		out[kind] = out[kind] || orphans < total
	}
	for kind := range terminal {
		out[kind] = false
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
// Files are selected by an EXACT match of the filename stem before the
// timestamp against group, so group "foo" never reads "foo-bar-<ts>.md".
// Pre-#6375 reports contribute nothing (parseKindParticipation returns an
// empty map for them), so a group whose history is entirely of that vintage
// behaves as a first run.
//
// Participation never expires — see the ONE-WAY DOOR note at the top of this
// file for the consequence and the only remedy (deleting
// ~/.grafel/feedback/<group>-*.md).
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
