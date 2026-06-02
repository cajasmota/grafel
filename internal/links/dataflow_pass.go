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
		// Cross-file resolver: maps (handler entity, callee name) → the
		// callee's defining entity in another file via the CALLS graph.
		xfile := newCrossFileResolver(g)

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
			sniffEx := substrate.DataFlowSnifferExFor(lang)
			if sniffEx == nil {
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
			sniffed := sniffEx(string(content))
			// In-file flows: sink lives in the handler's own file.
			var rflows []resolvedFlow
			for _, fl := range sniffed.Flows {
				rflows = append(rflows, resolvedFlow{DataFlow: fl, SinkFile: file})
			}
			// Resolve cross-file boundaries into concrete flows (multi-hop,
			// bounded). Each resolved flow's HopPath includes the cross-file
			// callee chain and its sink lives in the resolved file.
			rflows = append(rflows, resolveCrossFileFlows(g, file, srcRoot, lang, xfile, sniffed.Boundaries)...)
			if len(rflows) == 0 {
				continue
			}

			// Group flows by handler for the property summary.
			byHandler := map[string][]resolvedFlow{}
			for _, fl := range rflows {
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
				summaryFlows := make([]substrate.DataFlow, 0, len(hflows))
				for _, fl := range hflows {
					l, ok := buildDataFlowLink(g.Repo, fl.SinkFile, fromID, fl.DataFlow, nameIdx, rejects)
					if !ok {
						res.Skipped++
						continue
					}
					links = append(links, l)
					summaryFlows = append(summaryFlows, fl.DataFlow)
				}
				stampDataFlowSummary(g, fromID, summaryFlows)
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

// resolvedFlow pairs a substrate flow with the file its SINK lives in. For an
// in-file flow that is the handler's own file; for a cross-file flow it is the
// resolved callee's file, so the sink entity is looked up in the right place.
type resolvedFlow struct {
	substrate.DataFlow
	SinkFile string
}

// crossFileResolver maps (caller entity ID, callee bare name) → the callee's
// defining entity, using the repo's CALLS graph. A name binds only when the
// caller has exactly ONE cross-file callee with that name (function-like,
// defined in a different file) — an ambiguous name is dropped (precision).
type crossFileResolver struct {
	// byCaller[callerID][calleeName] = []*entityNode (cross-file callees)
	byCaller map[string]map[string][]*entityNode
}

func newCrossFileResolver(g *repoGraph) *crossFileResolver {
	byID := make(map[string]*entityNode, len(g.Entities))
	for i := range g.Entities {
		byID[g.Entities[i].ID] = &g.Entities[i]
	}
	r := &crossFileResolver{byCaller: map[string]map[string][]*entityNode{}}
	for _, e := range g.Edges {
		if !strings.EqualFold(e.Kind, "calls") {
			continue
		}
		caller := byID[e.FromID]
		callee := byID[e.ToID]
		if caller == nil || callee == nil {
			continue
		}
		if callee.Name == "" || callee.SourceFile == "" {
			continue
		}
		if !isFunctionLikeKind(callee.Kind) {
			continue
		}
		// Cross-file only: the callee must live in a different file than the
		// caller. Same-file calls are handled by the in-file hop walk.
		if caller.SourceFile == callee.SourceFile {
			continue
		}
		m := r.byCaller[e.FromID]
		if m == nil {
			m = map[string][]*entityNode{}
			r.byCaller[e.FromID] = m
		}
		m[callee.Name] = append(m[callee.Name], callee)
	}
	return r
}

// resolve returns the unique cross-file callee entity named `name` reachable
// from callerID via a CALLS edge, or nil when none or ambiguous (>1 distinct
// defining entity) — honest-partial precision guard.
func (r *crossFileResolver) resolve(callerID, name string) *entityNode {
	cands := r.byCaller[callerID][name]
	if len(cands) == 0 {
		return nil
	}
	// Distinct by entity ID.
	first := cands[0]
	for _, c := range cands[1:] {
		if c.ID != first.ID {
			return nil // ambiguous — drop
		}
	}
	return first
}

// resolveCrossFileFlows resolves each cross-file boundary by following the
// CALLS edge from the handler entity to the imported callee's defining file,
// then continuing the bounded hop walk there. Returns the concrete flows
// reached (sink in the resolved file). Honest-partial throughout: a boundary
// that cannot be uniquely resolved to a same-repo function entity is dropped,
// the hop bound (DataFlowMaxHops) is enforced, and entity cycles are stopped.
func resolveCrossFileFlows(g *repoGraph, handlerFile, srcRoot, lang string, xfile *crossFileResolver, boundaries []substrate.DataFlowBoundary) []resolvedFlow {
	cont := substrate.DataFlowContinueFor(lang)
	if cont == nil || len(boundaries) == 0 {
		return nil
	}
	binder := newDataFlowHandlerBinder(g)
	// File-content cache (avoid re-reading the same callee file).
	cache := map[string]string{}
	readFile := func(rel string) (string, bool) {
		if c, ok := cache[rel]; ok {
			return c, c != ""
		}
		buf, err := os.ReadFile(filepath.Join(srcRoot, rel))
		if err != nil {
			cache[rel] = ""
			return "", false
		}
		cache[rel] = string(buf)
		return string(buf), true
	}

	var out []resolvedFlow
	for _, b := range boundaries {
		// Resolve the handler entity that owns the boundary.
		callerID := binder.lookup(handlerFile, b.Function)
		if callerID == "" {
			continue
		}
		// Worklist of (callerEntityID, callee name, argIndex, hopPath, hopsUsed).
		type job struct {
			callerID string
			callee   string
			argIndex int
			hopPath  []string
			hopsUsed int
		}
		visited := map[string]bool{callerID: true}
		work := []job{{callerID: callerID, callee: b.Callee, argIndex: b.ArgIndex, hopPath: append([]string(nil), b.HopPath...), hopsUsed: len(b.HopPath)}}

		for len(work) > 0 {
			j := work[0]
			work = work[1:]
			if j.hopsUsed >= substrate.DataFlowMaxHops {
				continue // bound exhausted — drop
			}
			callee := xfile.resolve(j.callerID, j.callee)
			if callee == nil {
				continue // unresolved / external / ambiguous — drop
			}
			if visited[callee.ID] {
				continue // entity cycle — stop, drop
			}
			visited[callee.ID] = true // mark before descending (cycle guard)
			content, ok := readFile(callee.SourceFile)
			if !ok {
				continue
			}
			// Entering this cross-file callee consumes one hop. The
			// continuation walks further in-file hops on top of that, bounded
			// by DataFlowMaxHops via hopsUsed.
			hopsUsed := j.hopsUsed + 1
			r := cont(content, callee.Name, j.argIndex, b.SourceField, hopsUsed)
			// Assemble the full hop path: prior hops + this cross-file callee
			// + any in-file hops the continuation walked.
			base := append(append([]string(nil), j.hopPath...), callee.Name)
			for _, fl := range r.Flows {
				full := append(append([]string(nil), base...), fl.HopPath...)
				out = append(out, resolvedFlow{
					DataFlow: substrate.DataFlow{
						Function:    b.Function,
						SourceField: b.SourceField,
						SourceLine:  b.SourceLine,
						SinkKind:    fl.SinkKind,
						SinkName:    fl.SinkName,
						SinkLine:    fl.SinkLine,
						HopVia:      base[0],
						HopPath:     full,
					},
					SinkFile: callee.SourceFile,
				})
			}
			// Chase further cross-file hops from the resolved callee, if the
			// continuation surfaced boundaries and we still have budget. The
			// shared `visited` set (entity IDs) stops cross-file cycles.
			for _, nb := range r.Boundaries {
				nextHops := append(append([]string(nil), base...), nb.HopPath...)
				if len(nextHops) >= substrate.DataFlowMaxHops {
					continue
				}
				work = append(work, job{
					callerID: callee.ID,
					callee:   nb.Callee,
					argIndex: nb.ArgIndex,
					hopPath:  nextHops,
					hopsUsed: len(nextHops),
				})
			}
		}
	}
	return out
}

// dataFlowHandlerBinder resolves (file, function name) → entity ID, scoped to
// one repo graph. A thin wrapper over the same accumulation the effect binder
// uses, kept local so the cross-file resolver needn't reach across passes.
type dataFlowHandlerBinder struct {
	byFile map[string]map[string]string // file -> name -> entityID
}

func newDataFlowHandlerBinder(g *repoGraph) *dataFlowHandlerBinder {
	b := &dataFlowHandlerBinder{byFile: map[string]map[string]string{}}
	for i := range g.Entities {
		e := &g.Entities[i]
		if e.SourceFile == "" || e.Name == "" || !isFunctionLikeKind(e.Kind) {
			continue
		}
		m := b.byFile[e.SourceFile]
		if m == nil {
			m = map[string]string{}
			b.byFile[e.SourceFile] = m
		}
		if _, exists := m[e.Name]; !exists {
			m[e.Name] = e.ID // first wins, deterministic
		}
	}
	return b
}

func (b *dataFlowHandlerBinder) lookup(file, name string) string {
	return b.byFile[file][name]
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
	if len(fl.HopPath) > 0 {
		// Full inter-procedural chain (handler→A→B→sink => "A>B"); marks the
		// flow as cross-file when the path crosses ≥1 hop the resolver bound.
		props["hop_path"] = strings.Join(fl.HopPath, ">")
		props["hop_count"] = fmt.Sprintf("%d", len(fl.HopPath))
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

// dataFlowConfidence assigns a confidence: intra-fn flows with a known field
// are most certain; each inter-procedural hop lowers it, and whole-object
// pass-through (no field provenance) lowers it further. Confidence decays per
// hop so a 3-hop cross-file flow ranks below a direct one — precision-first.
func dataFlowConfidence(fl substrate.DataFlow) float64 {
	c := 0.85
	hops := len(fl.HopPath)
	if hops == 0 && fl.HopVia != "" {
		hops = 1 // defensive: HopVia set but HopPath unset
	}
	c -= 0.12 * float64(hops) // each hop is less certain
	if fl.SourceField == "" {
		c -= 0.10 // whole-object pass-through, no field provenance
	}
	if c < 0.3 {
		c = 0.3
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
		if len(f.HopPath) > 0 {
			s += " via " + strings.Join(f.HopPath, ">")
		} else if f.HopVia != "" {
			s += " via " + f.HopVia
		}
		parts[i] = s
	}
	return strings.Join(parts, ",")
}
