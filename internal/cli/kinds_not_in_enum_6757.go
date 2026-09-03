package cli

import (
	"fmt"
	"sort"
	"strings"
)

// DoctorKindsNotInEnumNames is how many distinct kind names the doctor line
// spells out before it summarises the remainder as "+N more". The COUNTS are
// never abbreviated — only the name list is, exactly as the writer caps the
// sidecar's map while leaving its totals exact (#6757 arm C).
const DoctorKindsNotInEnumNames = 5

// KindsNotInEnumLine renders one informational `doctor` line for the
// relationship kinds an index wrote that are absent from the relationship-kind
// enum, or "" when there are none.
//
// It is informational and never a status change: every one of these edges IS
// in the graph — arm C counts, it never drops — so nothing is broken. What is
// invisible without this line is that consumers traversing by the enum's
// vocabulary (mcp, links, the dashboard) do not recognise these kinds, and the
// graph gives no other signal that they exist.
//
// edges and distinct are the UNCAPPED totals; kinds is the writer's truncated
// map, so len(kinds) may be smaller than distinct. The line reports the true
// totals and says how many names it did not print.
func KindsNotInEnumLine(edges, distinct int, kinds map[string]int) string {
	if edges == 0 {
		return ""
	}
	names, rest := topKindNames(kinds, distinct)

	var b strings.Builder
	fmt.Fprintf(&b, "ℹ %s edges carry %s relationship kind(s) absent from the enum",
		fmtInt(edges), fmtInt(distinct))
	if len(names) > 0 {
		fmt.Fprintf(&b, ": %s", strings.Join(names, ", "))
	}
	if rest > 0 {
		fmt.Fprintf(&b, ", +%s more", fmtInt(rest))
	}
	return b.String()
}

// DerivedKindsLine renders one informational `doctor` line for the DERIVED
// (statistical) relationship kinds an index wrote, or "" when there are none
// (#6773).
//
// It exists because of what declaring COMMIT_COUPLED did to the line above.
// That kind was 27,407 of the 27,645 edges arm C reported — 99.1% — and
// declaring it in the derived vocabulary takes it out of that count. Without
// this second line, the visible effect of the decision would be a metric
// falling by two orders of magnitude and a population of 27k edges becoming
// invisible, which is indistinguishable from silencing the measurement. So
// the count moves; it does not disappear.
//
// Same contract as KindsNotInEnumLine: informational, never a status change —
// every one of these edges is in the graph and nothing failed — and edges and
// distinct are the UNCAPPED totals while kinds may be a truncated map.
func DerivedKindsLine(edges, distinct int, kinds map[string]int) string {
	if edges == 0 {
		return ""
	}
	names, rest := topKindNames(kinds, distinct)
	var b strings.Builder
	fmt.Fprintf(&b, "ℹ %s edges carry %s DERIVED relationship kind(s) — statistical signals, not structural facts",
		fmtInt(edges), fmtInt(distinct))
	if len(names) > 0 {
		fmt.Fprintf(&b, ": %s", strings.Join(names, ", "))
	}
	if rest > 0 {
		fmt.Fprintf(&b, ", +%s more", fmtInt(rest))
	}
	return b.String()
}

// topKindNames renders the busiest DoctorKindsNotInEnumNames entries of kinds
// as "NAME (count)" and returns how many of the distinct total were not
// named. distinct is the caller's UNCAPPED count, so the remainder accounts
// for kinds the writer's own truncation already dropped from the map.
func topKindNames(kinds map[string]int, distinct int) (names []string, rest int) {
	type kv struct {
		name  string
		edges int
	}
	list := make([]kv, 0, len(kinds))
	for k, n := range kinds {
		list = append(list, kv{k, n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].edges != list[j].edges {
			return list[i].edges > list[j].edges
		}
		return list[i].name < list[j].name
	})
	if len(list) > DoctorKindsNotInEnumNames {
		list = list[:DoctorKindsNotInEnumNames]
	}
	for _, k := range list {
		names = append(names, fmt.Sprintf("%s (%s)", k.name, fmtInt(k.edges)))
	}
	return names, distinct - len(list)
}
