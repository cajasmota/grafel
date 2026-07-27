package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// printAnnotationStatus renders the ANNOTATION (group-algo) line of
// `grafel status`.
//
// A rebuild reports completion once extraction + the cross-repo link pass have
// written graph.fb — the graph is queryable at that point and every structural
// MCP tool (find/expand/find_callers/traces/cross_links/get_source) works. The
// community / pagerank / centrality OVERLAY is produced afterwards, in the
// background, by the group-algo pass. On a large corpus that pass takes far
// longer than the rebuild it follows.
//
// That is the right trade — but it makes a specific user complaint possible:
// "the rebuild said it was done, so why does grafel_clusters return nothing?"
// Absent overlay is a silent state everywhere it is consumed (nil community
// pointers, empty cluster lists), so without this line the honest answer
// ("it has not been computed yet") is indistinguishable from a real fault.
// The line is silent when nothing is running or pending — the common case.
func printAnnotationStatus(w io.Writer, st proto.StatusReply) {
	running := st.GroupAlgoInFlight
	if running == 0 {
		running = len(st.GroupAlgoRunning)
	}
	if running == 0 && len(st.PendingAlgo) == 0 {
		return
	}
	fmt.Fprint(w, "  annotation:")
	switch {
	case len(st.GroupAlgoRunning) > 0:
		// Monolith mode knows WHICH groups.
		fmt.Fprintf(w, " running=%s", strings.Join(st.GroupAlgoRunning, ","))
	case running > 0:
		// Split mode publishes only the count.
		fmt.Fprintf(w, " running=%d", running)
	}
	if len(st.PendingAlgo) > 0 {
		fmt.Fprintf(w, " pending=%s", strings.Join(st.PendingAlgo, ","))
	}
	fmt.Fprint(w, "  (communities/pagerank/centrality overlay not yet current; the graph itself is queryable)\n")
}
