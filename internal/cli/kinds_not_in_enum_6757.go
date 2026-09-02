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

	shown := list
	if len(shown) > DoctorKindsNotInEnumNames {
		shown = shown[:DoctorKindsNotInEnumNames]
	}
	names := make([]string, 0, len(shown))
	for _, k := range shown {
		names = append(names, fmt.Sprintf("%s (%s)", k.name, fmtInt(k.edges)))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "ℹ %s edges carry %s relationship kind(s) absent from the enum",
		fmtInt(edges), fmtInt(distinct))
	if len(names) > 0 {
		fmt.Fprintf(&b, ": %s", strings.Join(names, ", "))
	}
	if rest := distinct - len(shown); rest > 0 {
		fmt.Fprintf(&b, ", +%s more", fmtInt(rest))
	}
	return b.String()
}
