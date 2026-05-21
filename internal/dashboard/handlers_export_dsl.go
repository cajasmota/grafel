package dashboard

// handlers_export_dsl.go — DSL export endpoint (#1318)
//
//	GET /api/export/{group}/{entity_id}/{format}?depth=N&limit=M
//
// Exports a subgraph (one entity + N-hop BFS neighbors) as paste-ready
// diagram source in one of four formats:
//
//	mermaid   — Mermaid flowchart LR
//	graphviz  — DOT language (digraph)
//	plantuml  — PlantUML component diagram
//	d2        — D2 language
//
// The subgraph is limited to `limit` nodes (default 15, max 50) for
// inline-doc usability. Nodes are color-coded by kind using tokens that
// match the graph canvas palette.
//
// Response: Content-Type text/plain; the DSL text is returned as-is so
// callers can copy it into a GH issue, Notion block, or README code fence.

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// defaultExportDepth is the default BFS depth when ?depth= is omitted.
const defaultExportDepth = 2

// defaultExportLimit is the default node cap for export subgraphs.
const defaultExportLimit = 15

// maxExportLimit caps how many nodes a single export request can return.
const maxExportLimit = 50

// handleExportDSL — GET /api/export/{group}/{entity_id}/{format}
func (s *Server) handleExportDSL(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	entityID := r.PathValue("entity_id")
	format := strings.ToLower(r.PathValue("format"))

	if group == "" || entityID == "" || format == "" {
		writeErr(w, http.StatusBadRequest, "group, entity_id, and format are required")
		return
	}

	switch format {
	case "mermaid", "graphviz", "plantuml", "d2":
	default:
		writeErr(w, http.StatusBadRequest, "format must be one of: mermaid, graphviz, plantuml, d2")
		return
	}

	depth := defaultExportDepth
	if d := r.URL.Query().Get("depth"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n >= 0 {
			depth = n
			if depth > 5 {
				depth = 5 // cap to prevent runaway traversal
			}
		}
	}

	limit := defaultExportLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
			if limit > maxExportLimit {
				limit = maxExportLimit
			}
		}
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	repo, root := findEntity(grp, entityID)
	if root == nil {
		writeErr(w, http.StatusNotFound, "entity not found: "+entityID)
		return
	}

	nodes, edges := bfsSubgraph(repo, root, depth, limit)

	var dsl string
	switch format {
	case "mermaid":
		dsl = renderMermaid(nodes, edges)
	case "graphviz":
		dsl = renderGraphviz(nodes, edges)
	case "plantuml":
		dsl = renderPlantUML(nodes, edges)
	case "d2":
		dsl = renderD2(nodes, edges)
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Export-Format", format)
	w.Header().Set("X-Export-NodeCount", strconv.Itoa(len(nodes)))
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, dsl)
}

// ── Subgraph BFS ──────────────────────────────────────────────────────────────

// exportNode is the flattened representation used by all renderers.
type exportNode struct {
	ID    string // prefixed "repo::local"
	Label string
	Kind  string
	Repo  string
}

// exportEdge connects two exportNode.ID values.
type exportEdge struct {
	FromID string
	ToID   string
	Kind   string
}

// bfsSubgraph performs a BFS from root up to depth hops, collecting at most
// limit nodes. Cross-repo links from grp.Links are not followed (too noisy
// for inline doc snippets); only same-repo relationships are traversed.
func bfsSubgraph(repo *DashRepo, root *graph.Entity, depth, limit int) ([]exportNode, []exportEdge) {
	if repo.Doc == nil {
		return nil, nil
	}

	// Build an adjacency index: localID → []Relationship
	adj := make(map[string][]graph.Relationship, len(repo.Doc.Relationships))
	for _, rel := range repo.Doc.Relationships {
		adj[rel.FromID] = append(adj[rel.FromID], rel)
		// Add reverse too so BFS walks in both directions.
		adj[rel.ToID] = append(adj[rel.ToID], graph.Relationship{
			FromID: rel.ToID,
			ToID:   rel.FromID,
			Kind:   rel.Kind,
		})
	}

	// Entity index for O(1) lookup.
	entityIdx := make(map[string]*graph.Entity, len(repo.Doc.Entities))
	for i := range repo.Doc.Entities {
		entityIdx[repo.Doc.Entities[i].ID] = &repo.Doc.Entities[i]
	}

	type frontier struct {
		id    string
		depth int
	}

	visited := map[string]bool{root.ID: true}
	queue := []frontier{{id: root.ID, depth: 0}}
	var nodeOrder []string
	nodeOrder = append(nodeOrder, root.ID)

	// Track which directed edges we've seen (original direction, not BFS reverse).
	edgeSet := map[string]exportEdge{}

	for len(queue) > 0 && len(nodeOrder) < limit {
		cur := queue[0]
		queue = queue[1:]

		if cur.depth >= depth {
			continue
		}

		for _, rel := range adj[cur.id] {
			neighbor := rel.ToID
			// Record the original-direction edge (not the reverse we added).
			ekey := rel.FromID + "->" + rel.ToID + ":" + rel.Kind
			if _, seen := edgeSet[ekey]; !seen {
				edgeSet[ekey] = exportEdge{
					FromID: dashPrefixedID(repo.Slug, rel.FromID),
					ToID:   dashPrefixedID(repo.Slug, rel.ToID),
					Kind:   rel.Kind,
				}
			}

			if !visited[neighbor] && len(nodeOrder) < limit {
				visited[neighbor] = true
				nodeOrder = append(nodeOrder, neighbor)
				queue = append(queue, frontier{id: neighbor, depth: cur.depth + 1})
			}
		}
	}

	// Build nodes slice (root first, then BFS order).
	nodes := make([]exportNode, 0, len(nodeOrder))
	for _, id := range nodeOrder {
		e := entityIdx[id]
		if e == nil {
			continue
		}
		nodes = append(nodes, exportNode{
			ID:    dashPrefixedID(repo.Slug, e.ID),
			Label: e.Name,
			Kind:  dashStripScopePrefix(e.Kind),
			Repo:  repo.Slug,
		})
	}

	// Collect only edges whose both endpoints are in the visited set.
	edges := make([]exportEdge, 0, len(edgeSet))
	for _, e := range edgeSet {
		fromLocal := strings.TrimPrefix(e.FromID, repo.Slug+"::")
		toLocal := strings.TrimPrefix(e.ToID, repo.Slug+"::")
		if visited[fromLocal] && visited[toLocal] {
			edges = append(edges, e)
		}
	}

	// Stable sort for deterministic output.
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].FromID != edges[j].FromID {
			return edges[i].FromID < edges[j].FromID
		}
		return edges[i].ToID < edges[j].ToID
	})

	return nodes, edges
}

// ── Color tokens per kind ─────────────────────────────────────────────────────
// These match the graph canvas palette used in the React SPA.

type kindStyle struct {
	mermaidFill    string // hex background
	mermaidStroke  string // hex border
	dotFillcolor   string // for graphviz
	dotFontcolor   string
	pumlStereo     string // PlantUML stereotype color
	d2StyleFill    string
	d2StyleStroke  string
}

var kindStyles = map[string]kindStyle{
	"Function":     {mermaidFill: "#bfdbfe", mermaidStroke: "#3b82f6", dotFillcolor: "#bfdbfe", dotFontcolor: "#1e40af", pumlStereo: "#bfdbfe", d2StyleFill: "#bfdbfe", d2StyleStroke: "#3b82f6"},
	"Class":        {mermaidFill: "#a5f3fc", mermaidStroke: "#06b6d4", dotFillcolor: "#a5f3fc", dotFontcolor: "#155e75", pumlStereo: "#a5f3fc", d2StyleFill: "#a5f3fc", d2StyleStroke: "#06b6d4"},
	"Interface":    {mermaidFill: "#a5f3fc", mermaidStroke: "#06b6d4", dotFillcolor: "#a5f3fc", dotFontcolor: "#155e75", pumlStereo: "#a5f3fc", d2StyleFill: "#a5f3fc", d2StyleStroke: "#06b6d4"},
	"Endpoint":     {mermaidFill: "#bbf7d0", mermaidStroke: "#22c55e", dotFillcolor: "#bbf7d0", dotFontcolor: "#14532d", pumlStereo: "#bbf7d0", d2StyleFill: "#bbf7d0", d2StyleStroke: "#22c55e"},
	"Route":        {mermaidFill: "#bbf7d0", mermaidStroke: "#22c55e", dotFillcolor: "#bbf7d0", dotFontcolor: "#14532d", pumlStereo: "#bbf7d0", d2StyleFill: "#bbf7d0", d2StyleStroke: "#22c55e"},
	"MessageTopic": {mermaidFill: "#e9d5ff", mermaidStroke: "#a855f7", dotFillcolor: "#e9d5ff", dotFontcolor: "#581c87", pumlStereo: "#e9d5ff", d2StyleFill: "#e9d5ff", d2StyleStroke: "#a855f7"},
	"Queue":        {mermaidFill: "#e9d5ff", mermaidStroke: "#a855f7", dotFillcolor: "#e9d5ff", dotFontcolor: "#581c87", pumlStereo: "#e9d5ff", d2StyleFill: "#e9d5ff", d2StyleStroke: "#a855f7"},
	"Process":      {mermaidFill: "#fed7aa", mermaidStroke: "#f97316", dotFillcolor: "#fed7aa", dotFontcolor: "#7c2d12", pumlStereo: "#fed7aa", d2StyleFill: "#fed7aa", d2StyleStroke: "#f97316"},
	"Component":    {mermaidFill: "#99f6e4", mermaidStroke: "#14b8a6", dotFillcolor: "#99f6e4", dotFontcolor: "#134e4a", pumlStereo: "#99f6e4", d2StyleFill: "#99f6e4", d2StyleStroke: "#14b8a6"},
	"Service":      {mermaidFill: "#99f6e4", mermaidStroke: "#14b8a6", dotFillcolor: "#99f6e4", dotFontcolor: "#134e4a", pumlStereo: "#99f6e4", d2StyleFill: "#99f6e4", d2StyleStroke: "#14b8a6"},
	"Variable":     {mermaidFill: "#fef08a", mermaidStroke: "#ca8a04", dotFillcolor: "#fef08a", dotFontcolor: "#713f12", pumlStereo: "#fef08a", d2StyleFill: "#fef08a", d2StyleStroke: "#ca8a04"},
	"Struct":       {mermaidFill: "#fce7f3", mermaidStroke: "#ec4899", dotFillcolor: "#fce7f3", dotFontcolor: "#831843", pumlStereo: "#fce7f3", d2StyleFill: "#fce7f3", d2StyleStroke: "#ec4899"},
}

var defaultStyle = kindStyle{
	mermaidFill: "#f1f5f9", mermaidStroke: "#94a3b8",
	dotFillcolor: "#f1f5f9", dotFontcolor: "#334155",
	pumlStereo: "#f1f5f9",
	d2StyleFill: "#f1f5f9", d2StyleStroke: "#94a3b8",
}

func styleFor(kind string) kindStyle {
	if s, ok := kindStyles[kind]; ok {
		return s
	}
	return defaultStyle
}

// ── Mermaid renderer ──────────────────────────────────────────────────────────

// mermaidID returns a safe Mermaid node identifier. Mermaid does not allow
// "::" in node IDs, so we hash the prefixed ID.
func mermaidID(prefixedID string) string {
	return "n" + hashStr(prefixedID)
}

// mermaidLabel truncates a label to 40 chars for readability.
func mermaidLabel(label, kind string) string {
	if len(label) > 40 {
		label = label[:37] + "…"
	}
	return label + "\\n[" + kind + "]"
}

func renderMermaid(nodes []exportNode, edges []exportEdge) string {
	var sb strings.Builder
	sb.WriteString("flowchart LR\n")

	// Collect unique kinds for style declarations.
	kindNodes := map[string][]string{} // kind → []mermaidID
	for _, n := range nodes {
		id := mermaidID(n.ID)
		label := mermaidLabel(n.Label, n.Kind)
		sb.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", id, label))
		kindNodes[n.Kind] = append(kindNodes[n.Kind], id)
	}

	sb.WriteString("\n")

	for _, e := range edges {
		fromID := mermaidID(e.FromID)
		toID := mermaidID(e.ToID)
		label := e.Kind
		sb.WriteString(fmt.Sprintf("  %s -->|%s| %s\n", fromID, label, toID))
	}

	sb.WriteString("\n")

	// Emit style per kind.
	for kind, ids := range kindNodes {
		s := styleFor(kind)
		for _, id := range ids {
			sb.WriteString(fmt.Sprintf("  style %s fill:%s,stroke:%s\n", id, s.mermaidFill, s.mermaidStroke))
		}
	}

	return sb.String()
}

// ── Graphviz renderer ─────────────────────────────────────────────────────────

// dotID returns a safe DOT identifier by replacing non-alphanumeric chars.
func dotID(prefixedID string) string {
	r := strings.NewReplacer("::", "_", "-", "_", "/", "_", ".", "_", " ", "_")
	return "n_" + r.Replace(prefixedID)
}

func renderGraphviz(nodes []exportNode, edges []exportEdge) string {
	var sb strings.Builder
	sb.WriteString("digraph subgraph {\n")
	sb.WriteString("  rankdir=LR;\n")
	sb.WriteString("  node [shape=box, style=filled, fontname=\"Helvetica\", fontsize=11];\n")
	sb.WriteString("  edge [fontname=\"Helvetica\", fontsize=9];\n\n")

	for _, n := range nodes {
		s := styleFor(n.Kind)
		label := n.Label
		if len(label) > 40 {
			label = label[:37] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %s [label=\"%s\\n[%s]\", fillcolor=\"%s\", fontcolor=\"%s\"];\n",
			dotID(n.ID), dotEscape(label), n.Kind, s.dotFillcolor, s.dotFontcolor))
	}

	sb.WriteString("\n")

	for _, e := range edges {
		sb.WriteString(fmt.Sprintf("  %s -> %s [label=\"%s\"];\n",
			dotID(e.FromID), dotID(e.ToID), e.Kind))
	}

	sb.WriteString("}\n")
	return sb.String()
}

func dotEscape(s string) string {
	return strings.NewReplacer(`"`, `\"`, `\`, `\\`).Replace(s)
}

// ── PlantUML renderer ─────────────────────────────────────────────────────────

func renderPlantUML(nodes []exportNode, edges []exportEdge) string {
	var sb strings.Builder
	sb.WriteString("@startuml\n")
	sb.WriteString("' Generated by archigraph export\n")
	sb.WriteString("left to right direction\n")
	sb.WriteString("skinparam componentStyle rectangle\n\n")

	// Build id → alias map.
	aliases := make(map[string]string, len(nodes))
	for _, n := range nodes {
		alias := "p" + hashStr(n.ID)
		aliases[n.ID] = alias
		s := styleFor(n.Kind)
		label := n.Label
		if len(label) > 40 {
			label = label[:37] + "..."
		}
		sb.WriteString(fmt.Sprintf("component \"%s\\n[%s]\" as %s #%s\n",
			pumlEscape(label), n.Kind, alias, strings.TrimPrefix(s.pumlStereo, "#")))
	}

	sb.WriteString("\n")

	for _, e := range edges {
		fromAlias := aliases[e.FromID]
		toAlias := aliases[e.ToID]
		if fromAlias == "" || toAlias == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("%s --> %s : %s\n", fromAlias, toAlias, e.Kind))
	}

	sb.WriteString("@enduml\n")
	return sb.String()
}

func pumlEscape(s string) string {
	return strings.NewReplacer(`"`, `'`, `\n`, ` `).Replace(s)
}

// ── D2 renderer ───────────────────────────────────────────────────────────────

// d2ID returns a safe D2 identifier.
func d2ID(prefixedID string) string {
	r := strings.NewReplacer("::", "_", "-", "_", "/", "_", ".", "_", " ", "_")
	return r.Replace(prefixedID)
}

func renderD2(nodes []exportNode, edges []exportEdge) string {
	var sb strings.Builder
	sb.WriteString("# Generated by archigraph export\n")
	sb.WriteString("direction: right\n\n")

	for _, n := range nodes {
		id := d2ID(n.ID)
		s := styleFor(n.Kind)
		label := n.Label
		if len(label) > 40 {
			label = label[:37] + "…"
		}
		sb.WriteString(fmt.Sprintf("%s: \"%s [%s]\" {\n  style.fill: \"%s\"\n  style.stroke: \"%s\"\n}\n",
			id, d2Escape(label), n.Kind, s.d2StyleFill, s.d2StyleStroke))
	}

	sb.WriteString("\n")

	for _, e := range edges {
		fromID := d2ID(e.FromID)
		toID := d2ID(e.ToID)
		sb.WriteString(fmt.Sprintf("%s -> %s: %s\n", fromID, toID, e.Kind))
	}

	return sb.String()
}

func d2Escape(s string) string {
	return strings.NewReplacer(`"`, `'`).Replace(s)
}
