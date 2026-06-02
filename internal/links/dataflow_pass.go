// SCOPED request-input → sink dataflow pass (#3628 roadmap area #22).
//
// Consumes the per-language substrate.DataFlowSnifferFor sniffers, which
// lift request-input → sink flows (intra-function, plus exactly one local
// call hop) per the honest-partial contract in
// internal/substrate/dataflow.go. For every flow this pass emits a
// DATA_FLOWS_TO link:
//
//	FROM  the request-handler entity that reads the untrusted input
//	TO    the sink entity (resolved to a local entity when the sink callee's
//	      last identifier names one) or a synthetic `sink:` residue otherwise
//
// Properties carried: field, sink_kind, sink, hop_via (when one-hop).
//
// Links are persisted to <group>-links-data-flow.json (sidecar) so the
// method-segregated rewrite logic in RunAllPasses is undisturbed, exactly
// as the constant-propagation resolves-to pass does. A compact summary is
// also stamped onto the handler entity:
//
//	data_flows        "<field>-><sink>(<kind>)[ via <hop>],..." (bounded)
//	data_flows_count  "<n>"
//
// Precision over recall: the pass NEVER fabricates a flow the sniffer did
// not soundly follow.
package links

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/substrate"
	"github.com/cajasmota/archigraph/internal/types"
)

// MethodDataFlow identifies sidecar artefacts from this pass.
const MethodDataFlow = "data_flow"

// DataFlowProperty* are the stable property keys stamped on the handler.
const (
	DataFlowPropertyKeyFlows = "data_flows"
	DataFlowPropertyKeyCount = "data_flows_count"
)

// maxDataFlowSummary bounds the compact per-entity property summary.
const maxDataFlowSummary = 24

// dataFlowDocument is the on-disk sidecar shape.
type dataFlowDocument struct {
	Version int    `json:"version"`
	Method  string `json:"method"`
	Total   int    `json:"total_flows"`
	Links   []Link `json:"links"`
}

// runDataFlowPass walks every source file with a registered dataflow
// sniffer, binds each flow's originating handler to its graph entity, and
// emits DATA_FLOWS_TO links.
func runDataFlowPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "data_flow"}
	binder := newEffectBinder(graphs)

	var links []Link
	scanned := 0

	for ri := range graphs {
		g := &graphs[ri]

		// Per-(repo) index of function-like entity names → entity ID, scoped
		// by source file, used to resolve a sink callee to a real entity.
		nameIdx := buildDataFlowNameIndex(g)

		fileSet := map[string]bool{}
		for _, e := range g.Entities {
			if e.SourceFile != "" {
				fileSet[e.SourceFile] = true
			}
		}
		// Deterministic file iteration.
		files := make([]string, 0, len(fileSet))
		for f := range fileSet {
			files = append(files, f)
		}
		sort.Strings(files)

		for _, file := range files {
			lang := substrate.LanguageForPath(file)
			if lang == "" {
				continue
			}
			sniff := substrate.DataFlowSnifferFor(lang)
			if sniff == nil {
				continue
			}
			srcRoot := repoSourcePathFor(g.Repo)
			if srcRoot == "" {
				srcRoot = g.FileRoot
			}
			abs := filepath.Join(srcRoot, file)
			content, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			scanned++
			flows := sniff(string(content))
			if len(flows) == 0 {
				continue
			}

			// Group flows by handler for the property summary.
			byHandler := map[string][]substrate.DataFlow{}
			for _, fl := range flows {
				if fl.Function == "" {
					continue
				}
				byHandler[fl.Function] = append(byHandler[fl.Function], fl)
			}

			for handler, hflows := range byHandler {
				fromID := binder.lookup(g.Repo, file, handler)
				if fromID == "" {
					continue // can't bind the handler entity — drop (honest)
				}
				sort.Slice(hflows, func(i, j int) bool {
					if hflows[i].SinkLine != hflows[j].SinkLine {
						return hflows[i].SinkLine < hflows[j].SinkLine
					}
					return hflows[i].SinkName < hflows[j].SinkName
				})
				for _, fl := range hflows {
					l, ok := buildDataFlowLink(g.Repo, file, fromID, fl, nameIdx, rejects)
					if !ok {
						res.Skipped++
						continue
					}
					links = append(links, l)
				}
				stampDataFlowSummary(g, fromID, hflows)
			}
		}
	}

	res.LinksAdded = len(links)
	res.Candidates = scanned

	if paths.Links == "" {
		return res, nil
	}
	sort.Slice(links, func(i, j int) bool { return links[i].ID < links[j].ID })
	sidecar := strings.TrimSuffix(paths.Links, ".json") + "-data-flow.json"
	doc := dataFlowDocument{Version: 1, Method: MethodDataFlow, Total: len(links), Links: links}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return res, err
	}
	if err := os.MkdirAll(filepath.Dir(sidecar), 0o755); err != nil {
		return res, err
	}
	if err := os.WriteFile(sidecar, buf, 0o644); err != nil {
		return res, fmt.Errorf("write data-flow doc: %w", err)
	}
	return res, nil
}

// dataFlowNameIndex maps (file, lastIdent) → entityID for function-like
// entities, used to resolve a sink callee to a concrete entity.
type dataFlowNameIndex map[string]string

func dataFlowNameKey(file, name string) string { return file + "::" + name }

func buildDataFlowNameIndex(g *repoGraph) dataFlowNameIndex {
	idx := dataFlowNameIndex{}
	for _, e := range g.Entities {
		if e.Name == "" || e.SourceFile == "" {
			continue
		}
		k := dataFlowNameKey(e.SourceFile, e.Name)
		// First write wins for determinism; entities are graph-ordered.
		if _, exists := idx[k]; !exists {
			idx[k] = e.ID
		}
	}
	return idx
}

// buildDataFlowLink constructs one DATA_FLOWS_TO link. The TO endpoint is a
// concrete entity when the sink callee's last identifier names a same-file
// entity; otherwise a synthetic `sink:` residue (resolvable by the
// dashboard/MCP as a substrate residue, mirroring constant-propagation's
// `binding:` form).
func buildDataFlowLink(repo, file, fromID string, fl substrate.DataFlow, nameIdx dataFlowNameIndex, rejects map[string]bool) (Link, bool) {
	last := lastIdent(fl.SinkName)
	var toID string
	if id, ok := nameIdx[dataFlowNameKey(file, last)]; ok && id != fromID {
		toID = id
	} else {
		toID = fmt.Sprintf("sink:%s::%s@%d", file, fl.SinkName, fl.SinkLine)
	}

	source := entityKey(repo, fromID)
	target := entityKey(repo, toID)
	linkID := MakeID(source, target, MethodDataFlow)
	if rejects != nil && rejects[linkID] {
		return Link{}, false
	}
	props := map[string]string{
		"sink_kind": string(fl.SinkKind),
		"sink":      fl.SinkName,
	}
	if fl.SourceField != "" {
		props["field"] = fl.SourceField
	}
	if fl.HopVia != "" {
		props["hop_via"] = fl.HopVia
	}
	return Link{
		ID:           linkID,
		Source:       source,
		Target:       target,
		Relation:     string(types.RelationshipKindDataFlowsTo),
		Method:       MethodDataFlow,
		Confidence:   dataFlowConfidence(fl),
		DiscoveredAt: discoveredAt(),
		Properties:   props,
	}, true
}

// dataFlowConfidence assigns a confidence: intra-fn flows with a known
// field are most certain; one-hop and whole-object pass-through are lower.
func dataFlowConfidence(fl substrate.DataFlow) float64 {
	c := 0.85
	if fl.HopVia != "" {
		c -= 0.15 // inter-procedural hop is less certain
	}
	if fl.SourceField == "" {
		c -= 0.10 // whole-object pass-through, no field provenance
	}
	return c
}

// lastIdent returns the final identifier of a dotted callee (e.g.
// "User.objects.create" → "create", "repo.insert" → "insert").
func lastIdent(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 && i+1 < len(s) {
		return s[i+1:]
	}
	return s
}

// stampDataFlowSummary writes the compact per-entity property summary.
func stampDataFlowSummary(g *repoGraph, entityID string, flows []substrate.DataFlow) {
	for ei := range g.Entities {
		if g.Entities[ei].ID != entityID {
			continue
		}
		e := &g.Entities[ei]
		if e.Properties == nil {
			e.Properties = map[string]string{}
		}
		e.Properties[DataFlowPropertyKeyFlows] = formatDataFlowSummary(flows, maxDataFlowSummary)
		e.Properties[DataFlowPropertyKeyCount] = fmt.Sprintf("%d", len(flows))
		return
	}
}

// formatDataFlowSummary renders up to max flows as
// "<field>-><sink>(<kind>)[ via <hop>]" comma-joined, stably sorted.
func formatDataFlowSummary(flows []substrate.DataFlow, max int) string {
	cp := append([]substrate.DataFlow(nil), flows...)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].SinkLine != cp[j].SinkLine {
			return cp[i].SinkLine < cp[j].SinkLine
		}
		return cp[i].SinkName < cp[j].SinkName
	})
	if len(cp) > max {
		cp = cp[:max]
	}
	parts := make([]string, len(cp))
	for i, f := range cp {
		field := f.SourceField
		if field == "" {
			field = "*"
		}
		s := fmt.Sprintf("%s->%s(%s)", field, f.SinkName, f.SinkKind)
		if f.HopVia != "" {
			s += " via " + f.HopVia
		}
		parts[i] = s
	}
	return strings.Join(parts, ",")
}
