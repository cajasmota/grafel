package export

import (
	"bufio"
	"fmt"
	"io"
	"sort"

	"github.com/cajasmota/archigraph/internal/graph"
)

// DefaultTopN is the default node cap applied by the SVG and HTML writers when
// no explicit limit is supplied. Large graphs (tens of thousands of nodes)
// produce SVG/HTML that no browser can render usefully, so the writers keep the
// top-N highest-degree nodes (ties broken by stable id ordering) and drop the
// rest along with any edge that loses an endpoint. A non-positive TopN means
// "no cap".
const DefaultTopN = 500

// svgLayout holds the computed deterministic placement for a (possibly capped)
// graph. Nodes are placed on a fixed grid in stable id order so the same input
// always yields byte-identical output — no force simulation, no randomness.
type svgLayout struct {
	nodes   []layoutNode
	edges   []layoutEdge
	width   int
	height  int
	dropped int // number of nodes omitted by the top-N cap
}

type layoutNode struct {
	id    string
	label string
	kind  string
	x, y  int
}

type layoutEdge struct {
	from, to string
	kind     string
	x1, y1   int
	x2, y2   int
}

// layout constants — fixed so output is deterministic and dependency-free.
const (
	svgCellW  = 220
	svgCellH  = 90
	svgNodeW  = 160
	svgNodeH  = 44
	svgMargin = 40
	svgCols   = 6 // grid columns; rows grow as needed
	svgPadX   = (svgCellW - svgNodeW) / 2
	svgPadY   = (svgCellH - svgNodeH) / 2
)

// computeLayout selects up to topN nodes (highest CALLS/edge degree first,
// then stable id order), assigns each a grid cell, and resolves edge endpoints
// to node centers. Edges with an endpoint outside the kept set are dropped.
//
// topN <= 0 disables the cap. The selection and placement are fully
// deterministic for a given Document.
func computeLayout(doc *graph.Document, topN int) svgLayout {
	// Degree map for ranking when capping.
	degree := make(map[string]int, len(doc.Entities))
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		degree[r.FromID]++
		degree[r.ToID]++
	}

	// Stable-sorted copy of entity ids.
	type rankedEntity struct {
		idx int
		id  string
		deg int
	}
	ranked := make([]rankedEntity, len(doc.Entities))
	for i := range doc.Entities {
		ranked[i] = rankedEntity{idx: i, id: doc.Entities[i].ID, deg: degree[doc.Entities[i].ID]}
	}
	// Primary: id ascending (stable, reproducible base order).
	sort.SliceStable(ranked, func(a, b int) bool { return ranked[a].id < ranked[b].id })

	kept := ranked
	dropped := 0
	if topN > 0 && len(ranked) > topN {
		// Re-rank by degree desc, ties by id asc, then take the first topN.
		byDeg := make([]rankedEntity, len(ranked))
		copy(byDeg, ranked)
		sort.SliceStable(byDeg, func(a, b int) bool {
			if byDeg[a].deg != byDeg[b].deg {
				return byDeg[a].deg > byDeg[b].deg
			}
			return byDeg[a].id < byDeg[b].id
		})
		byDeg = byDeg[:topN]
		// Re-sort the kept slice back into stable id order for placement so
		// the grid is reproducible regardless of degree distribution.
		sort.SliceStable(byDeg, func(a, b int) bool { return byDeg[a].id < byDeg[b].id })
		kept = byDeg
		dropped = len(ranked) - topN
	}

	keptSet := make(map[string]int, len(kept)) // id -> grid index
	lay := svgLayout{dropped: dropped}
	for gi, re := range kept {
		e := &doc.Entities[re.idx]
		col := gi % svgCols
		row := gi / svgCols
		x := svgMargin + col*svgCellW + svgPadX
		y := svgMargin + row*svgCellH + svgPadY
		keptSet[e.ID] = gi
		lay.nodes = append(lay.nodes, layoutNode{
			id:    e.ID,
			label: e.Name,
			kind:  e.Kind,
			x:     x,
			y:     y,
		})
	}

	// Canvas size from grid extent.
	rows := (len(kept) + svgCols - 1) / svgCols
	if rows < 1 {
		rows = 1
	}
	cols := svgCols
	if len(kept) < svgCols {
		cols = len(kept)
		if cols < 1 {
			cols = 1
		}
	}
	lay.width = svgMargin*2 + cols*svgCellW
	lay.height = svgMargin*2 + rows*svgCellH

	// Resolve edges (only those fully inside the kept set), in stable order.
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		fi, okF := keptSet[r.FromID]
		ti, okT := keptSet[r.ToID]
		if !okF || !okT {
			continue
		}
		fn := lay.nodes[fi]
		tn := lay.nodes[ti]
		lay.edges = append(lay.edges, layoutEdge{
			from: r.FromID,
			to:   r.ToID,
			kind: r.Kind,
			x1:   fn.x + svgNodeW/2,
			y1:   fn.y + svgNodeH/2,
			x2:   tn.x + svgNodeW/2,
			y2:   tn.y + svgNodeH/2,
		})
	}
	// Edges already follow Relationships order, which the export CLI sorts for
	// determinism; sort defensively here too so the writer is order-independent.
	sort.SliceStable(lay.edges, func(a, b int) bool {
		if lay.edges[a].from != lay.edges[b].from {
			return lay.edges[a].from < lay.edges[b].from
		}
		if lay.edges[a].to != lay.edges[b].to {
			return lay.edges[a].to < lay.edges[b].to
		}
		return lay.edges[a].kind < lay.edges[b].kind
	})

	return lay
}

// WriteSVG streams a self-contained, dependency-free SVG rendering of doc to w.
// Nodes are placed on a deterministic grid (no force layout, no randomness) and
// labeled with their name and kind; directed edges are drawn as lines with an
// arrowhead marker. For large graphs the top-N highest-degree nodes are kept
// (see DefaultTopN); pass topN <= 0 to disable the cap.
//
// The output is well-formed XML and byte-identical for a given (doc, topN).
func WriteSVG(w io.Writer, doc *graph.Document, topN int) error {
	if topN == 0 {
		topN = DefaultTopN
	}
	lay := computeLayout(doc, topN)

	bw := bufio.NewWriter(w)
	if _, err := fmt.Fprintf(bw,
		`<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="sans-serif">
`, lay.width, lay.height, lay.width, lay.height); err != nil {
		return err
	}

	// Arrowhead marker + minimal styles.
	if _, err := io.WriteString(bw, `  <defs>
    <marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="3" orient="auto" markerUnits="strokeWidth">
      <path d="M0,0 L7,3 L0,6 Z" fill="#888"/>
    </marker>
  </defs>
  <rect width="100%" height="100%" fill="#ffffff"/>
`); err != nil {
		return err
	}

	// Edges first (drawn under nodes).
	if _, err := io.WriteString(bw, "  <g stroke=\"#888\" stroke-width=\"1\">\n"); err != nil {
		return err
	}
	for i := range lay.edges {
		e := &lay.edges[i]
		if _, err := fmt.Fprintf(bw,
			"    <line x1=\"%d\" y1=\"%d\" x2=\"%d\" y2=\"%d\" marker-end=\"url(#arrow)\"><title>%s</title></line>\n",
			e.x1, e.y1, e.x2, e.y2, xmlEscape(e.kind)); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(bw, "  </g>\n"); err != nil {
		return err
	}

	// Nodes.
	if _, err := io.WriteString(bw, "  <g>\n"); err != nil {
		return err
	}
	for i := range lay.nodes {
		n := &lay.nodes[i]
		label := n.label
		if label == "" {
			label = n.id
		}
		if _, err := fmt.Fprintf(bw,
			"    <g><rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"6\" fill=\"#eef3fb\" stroke=\"#3b6ea5\"/>\n",
			n.x, n.y, svgNodeW, svgNodeH); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(bw,
			"      <text x=\"%d\" y=\"%d\" font-size=\"12\" text-anchor=\"middle\" fill=\"#16324f\">%s</text>\n",
			n.x+svgNodeW/2, n.y+18, xmlEscape(truncateLabel(label, 22))); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(bw,
			"      <text x=\"%d\" y=\"%d\" font-size=\"10\" text-anchor=\"middle\" fill=\"#6b7c93\">%s</text></g>\n",
			n.x+svgNodeW/2, n.y+34, xmlEscape(truncateLabel(n.kind, 26))); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(bw, "  </g>\n"); err != nil {
		return err
	}

	if lay.dropped > 0 {
		if _, err := fmt.Fprintf(bw,
			"  <text x=\"%d\" y=\"%d\" font-size=\"11\" fill=\"#a33\">%d of %d nodes shown (top-N cap); %d hidden</text>\n",
			svgMargin, lay.height-12, len(lay.nodes), len(lay.nodes)+lay.dropped, lay.dropped); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(bw, "</svg>\n"); err != nil {
		return err
	}
	return bw.Flush()
}

// truncateLabel shortens s to at most max runes, appending an ellipsis when
// truncated. Operates on runes so multi-byte text is not split.
func truncateLabel(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}
