package export

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/cajasmota/archigraph/internal/graph"
)

// htmlNode / htmlEdge are the trimmed shapes embedded as JSON in the HTML
// export. Only the fields the standalone viewer needs are included so the file
// stays small; the embedding is deterministic (stable field order via struct
// definition, stable record order via the caller's sort + the layout pass).
type htmlNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}

type htmlEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// WriteHTML streams a single self-contained HTML document to w. The file opens
// standalone in any browser with no server and no external assets: it embeds
// the (capped) graph as JSON, a static SVG rendering, and a tiny inline script
// that powers a live filter box over the node table. Styles and script are
// inlined.
//
// The top-N cap mirrors WriteSVG (DefaultTopN when topN == 0; topN <= 0
// disables). Output is deterministic for a given (doc, topN).
func WriteHTML(w io.Writer, doc *graph.Document, topN int) error {
	if topN == 0 {
		topN = DefaultTopN
	}
	lay := computeLayout(doc, topN)

	// Build the embedded JSON payload from the SAME kept set the SVG used so
	// the table and the picture agree. computeLayout already sorted nodes by id.
	nodes := make([]htmlNode, 0, len(lay.nodes))
	idToEntity := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		idToEntity[doc.Entities[i].ID] = &doc.Entities[i]
	}
	for i := range lay.nodes {
		n := &lay.nodes[i]
		hn := htmlNode{ID: n.id, Name: n.label, Kind: n.kind}
		if e := idToEntity[n.id]; e != nil {
			hn.File = e.SourceFile
			hn.Line = e.StartLine
		}
		nodes = append(nodes, hn)
	}
	edges := make([]htmlEdge, 0, len(lay.edges))
	for i := range lay.edges {
		e := &lay.edges[i]
		edges = append(edges, htmlEdge{From: e.from, To: e.to, Kind: e.kind})
	}
	// Defensive deterministic ordering for the embedded payload.
	sort.SliceStable(nodes, func(a, b int) bool { return nodes[a].ID < nodes[b].ID })
	sort.SliceStable(edges, func(a, b int) bool {
		if edges[a].From != edges[b].From {
			return edges[a].From < edges[b].From
		}
		if edges[a].To != edges[b].To {
			return edges[a].To < edges[b].To
		}
		return edges[a].Kind < edges[b].Kind
	})

	payload := struct {
		Repo    string     `json:"repo"`
		Nodes   []htmlNode `json:"nodes"`
		Edges   []htmlEdge `json:"edges"`
		Dropped int        `json:"dropped"`
	}{Repo: doc.Repo, Nodes: nodes, Edges: edges, Dropped: lay.dropped}

	// Marshal with HTML escaping enabled (the default) so the JSON is safe to
	// embed inside a <script> block (no raw </script> can appear).
	var jsonBuf bytes.Buffer
	enc := json.NewEncoder(&jsonBuf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(payload); err != nil {
		return err
	}

	// Render the SVG once into a buffer so we can inline it in the body.
	var svgBuf bytes.Buffer
	if err := WriteSVG(&svgBuf, doc, topN); err != nil {
		return err
	}
	// Strip the XML prolog from the embedded SVG (inline SVG must not carry an
	// <?xml?> declaration inside an HTML document).
	svgInline := stripXMLProlog(svgBuf.Bytes())

	bw := bufio.NewWriter(w)
	repo := htmlEscape(doc.Repo)
	// htmlHead is split around the two dynamic slots (the <title>/<h1> repo name
	// and the meta counts) so the literal CSS — which contains '%' characters —
	// is written verbatim, never through a format verb.
	if _, err := io.WriteString(bw, htmlHead1); err != nil {
		return err
	}
	if _, err := io.WriteString(bw, repo); err != nil { // <title>
		return err
	}
	if _, err := io.WriteString(bw, htmlHead2); err != nil {
		return err
	}
	if _, err := io.WriteString(bw, repo); err != nil { // <h1>
		return err
	}
	if _, err := io.WriteString(bw, htmlHead3); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(bw, "%d nodes · %d edges · %d hidden by top-N cap", len(nodes), len(edges), lay.dropped); err != nil {
		return err
	}
	if _, err := io.WriteString(bw, htmlHead4); err != nil {
		return err
	}
	if _, err := bw.Write(svgInline); err != nil {
		return err
	}
	if _, err := io.WriteString(bw, htmlMid); err != nil {
		return err
	}
	// Embed the JSON payload (trailing newline from Encode is harmless).
	if _, err := io.WriteString(bw, `<script id="graph-data" type="application/json">`); err != nil {
		return err
	}
	if _, err := bw.Write(jsonBuf.Bytes()); err != nil {
		return err
	}
	if _, err := io.WriteString(bw, "</script>\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(bw, htmlScript); err != nil {
		return err
	}
	return bw.Flush()
}

// stripXMLProlog removes a leading <?xml ... ?> declaration (and the following
// newline) from an SVG byte slice so it can be inlined in HTML.
func stripXMLProlog(b []byte) []byte {
	if bytes.HasPrefix(b, []byte("<?xml")) {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			return b[i+1:]
		}
	}
	return b
}

// htmlEscape escapes the characters that are unsafe in HTML text/attribute
// context. Reuses the XML entity set (a superset of what HTML text needs).
func htmlEscape(s string) string { return xmlEscape(s) }

// Templates. Kept as plain consts (no html/template) so the writer is
// allocation-light and the output is byte-stable. The %s/%d verbs are filled
// from already-escaped values.

const htmlHead1 = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>archigraph export — `

const htmlHead2 = `</title>
<style>
  :root { font-family: system-ui, sans-serif; }
  body { margin: 0; color: #16324f; background: #f7f9fc; }
  header { padding: 12px 20px; background: #16324f; color: #fff; }
  header h1 { margin: 0; font-size: 18px; }
  header .meta { font-size: 12px; opacity: .85; margin-top: 4px; }
  main { display: flex; flex-wrap: wrap; gap: 16px; padding: 16px 20px; align-items: flex-start; }
  .panel { background: #fff; border: 1px solid #dde3ec; border-radius: 8px; padding: 12px; }
  .graph { overflow: auto; max-width: 100%; }
  .graph svg { max-width: none; }
  .controls { margin-bottom: 8px; }
  .controls input { padding: 6px 8px; border: 1px solid #c3ccda; border-radius: 6px; width: 220px; }
  table { border-collapse: collapse; font-size: 12px; }
  th, td { text-align: left; padding: 4px 8px; border-bottom: 1px solid #eef1f6; white-space: nowrap; }
  th { position: sticky; top: 0; background: #eef3fb; }
  tbody tr.hidden { display: none; }
  .kind { color: #6b7c93; }
  .tablewrap { max-height: 70vh; overflow: auto; }
  footer { padding: 8px 20px; font-size: 11px; color: #6b7c93; }
</style>
</head>
<body>
<header>
  <h1>archigraph export — `

const htmlHead3 = `</h1>
  <div class="meta">`

const htmlHead4 = `</div>
</header>
<main>
  <section class="panel graph">
`

const htmlMid = `  </section>
  <section class="panel">
    <div class="controls">
      <input id="filter" type="search" placeholder="Filter nodes by name or kind…" autocomplete="off">
    </div>
    <div class="tablewrap">
      <table>
        <thead><tr><th>Name</th><th>Kind</th><th>File</th><th>Line</th></tr></thead>
        <tbody id="rows"></tbody>
      </table>
    </div>
  </section>
</main>
<footer>Self-contained archigraph graph export. No network or server required.</footer>
`

const htmlScript = `<script>
(function () {
  var data = JSON.parse(document.getElementById('graph-data').textContent);
  var tbody = document.getElementById('rows');
  var rows = data.nodes.map(function (n) {
    var tr = document.createElement('tr');
    tr.dataset.search = ((n.name || '') + ' ' + (n.kind || '')).toLowerCase();
    var c1 = document.createElement('td'); c1.textContent = n.name || n.id;
    var c2 = document.createElement('td'); c2.className = 'kind'; c2.textContent = n.kind || '';
    var c3 = document.createElement('td'); c3.textContent = n.file || '';
    var c4 = document.createElement('td'); c4.textContent = n.line ? String(n.line) : '';
    tr.appendChild(c1); tr.appendChild(c2); tr.appendChild(c3); tr.appendChild(c4);
    tbody.appendChild(tr);
    return tr;
  });
  var filter = document.getElementById('filter');
  filter.addEventListener('input', function () {
    var q = filter.value.trim().toLowerCase();
    rows.forEach(function (tr) {
      tr.classList.toggle('hidden', q !== '' && tr.dataset.search.indexOf(q) === -1);
    });
  });
})();
</script>
</body>
</html>
`
