package dashboard

// handlers_dataflow.go — Data-flow & Taint surface (#4265, epic #4249).
//
// Route:
//
//	GET /api/dataflow/{group}  — request-input → sink taint flows + ranked
//	                             security findings (source→sink paths).
//
// What the graph genuinely models (verified against the MCP tools this handler
// mirrors server-side — internal/mcp/phase3_tools.go handleDataFlows and
// internal/mcp/security_findings_tool.go handleSecurityFindings — and the passes
// that produce the on-disk sidecars: internal/links/dataflow_pass.go and
// internal/links/taint_flow.go):
//
//   - DATA_FLOWS_TO edges. The data-flow link pass walks request handlers and
//     records every request-input → sink edge it can soundly follow (intra-
//     function + bounded inter-procedural CALLS hops). Each edge carries the
//     tainted request `field`, the `sink_kind`, the resolved `sink`, and the
//     inter-procedural `hop_path`. The pass persists these to the data-flow
//     sidecar (~/.archigraph/groups/<group>-links-data-flow.json). Precision-
//     first: a flow the sniffer did not soundly follow is never fabricated.
//
//   - SecurityFinding records. The taint-flow pass (internal/links/taint_flow.go)
//     BFS-walks from each taint SOURCE across CALLS to each reachable SINK whose
//     category is active and unsanitised, emitting one ranked finding per
//     (source, sink, category) with an aggregated confidence in [0,1] (per-hop
//     decay × source/sink confidence). Persisted to the taint sidecar
//     (~/.archigraph/groups/<group>-links-taint.json). The pass drops findings
//     below TaintFindingFloor() (0.5); this handler surfaces every finding the
//     pass kept (the UI ranks + lets the user threshold), so the count is honest.
//
// Both sidecar endpoints are "<repo>::<localId>" keys whose repo PREFIX written
// by the link pass can diverge from dashboard repo slugs (underscore-vs-dash,
// short-name-vs-full — see normalizeLinkEndpoints). Rather than depend on that
// prefix, we index every entity-ID suffix → entity across all repos (the same
// resolution normalizeLinkEndpoints uses) and resolve source/sink names, files,
// and lines by suffix. Endpoints whose suffix is unknown or ambiguous keep their
// raw key as the label — never fabricated.
//
// Follows handlers_iac.go / handlers_security.go: prefer the cached group graph,
// fall back to a direct per-repo load; raw-JSON envelope. Sidecar paths mirror
// the MCP tools exactly (sidecarPath / LinksFile + "-taint.json").

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	fb "github.com/cajasmota/archigraph/internal/graph/fbgraph"
	"github.com/cajasmota/archigraph/internal/graph/fbreader"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes — mirror webui-v2/src/data/types.ts (Data-flow / Taint surface)
// ─────────────────────────────────────────────────────────────────────────────

// DataflowEndpoint is one end (source or sink) of a flow/finding, resolved to a
// human-readable entity reference where possible.
type DataflowEndpoint struct {
	// EntityID is the resolved "<repo>/<id>" key when the endpoint resolved to a
	// known entity, else the raw sidecar key.
	EntityID string `json:"entity_id"`
	Repo     string `json:"repo,omitempty"`
	// Name is the entity name, or the raw key tail when unresolved.
	Name       string `json:"name"`
	Kind       string `json:"kind,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
	Line       int    `json:"line,omitempty"`
	// Primitive is the taint-rule label that fired (source_primitive /
	// sink_primitive) — only set on findings.
	Primitive string `json:"primitive,omitempty"`
}

// TaintFlow is one request-input → sink DATA_FLOWS_TO edge.
type TaintFlow struct {
	ID         string           `json:"id"`
	Source     DataflowEndpoint `json:"source"`
	Sink       DataflowEndpoint `json:"sink"`
	Relation   string           `json:"relation,omitempty"`
	Confidence float64          `json:"confidence"`
	// Field is the tainted request field that flows to the sink.
	Field string `json:"field,omitempty"`
	// SinkKind classifies the sink (sql / command / fs / http / template / …).
	SinkKind string `json:"sink_kind,omitempty"`
	// HopPath is the inter-procedural chain from source to sink (raw string from
	// the pass, "a -> b -> c" form when present).
	HopPath  string `json:"hop_path,omitempty"`
	HopCount int    `json:"hop_count,omitempty"`
}

// SecurityFindingView is one ranked taint source→sink finding.
type SecurityFindingView struct {
	Fingerprint string           `json:"fingerprint"`
	Category    string           `json:"category"`
	Confidence  float64          `json:"confidence"`
	Source      DataflowEndpoint `json:"source"`
	Sink        DataflowEndpoint `json:"sink"`
	// Path is the ordered entity-ID chain from source to sink (inclusive). Each
	// entry is resolved to a readable name where possible.
	Path []DataflowPathStep `json:"path"`
	// Hops is len(Path)-1 (number of call hops), clamped at 0.
	Hops        int    `json:"hops"`
	Explanation string `json:"explanation"`
}

// DataflowPathStep is one node on a finding's source→sink path.
type DataflowPathStep struct {
	EntityID string `json:"entity_id"`
	Name     string `json:"name"`
	Repo     string `json:"repo,omitempty"`
}

// DataflowReport is the wire shape for GET /api/dataflow/{group}.
type DataflowReport struct {
	Group string `json:"group"`

	// Findings — ranked source→sink security findings (confidence desc).
	Findings []SecurityFindingView `json:"findings"`
	// Flows — request-input → sink DATA_FLOWS_TO edges.
	Flows []TaintFlow `json:"flows"`

	// Totals + roll-ups.
	TotalFindings int `json:"total_findings"`
	TotalFlows    int `json:"total_flows"`
	// FindingsByCategory — vulnerability category → finding count.
	FindingsByCategory map[string]int `json:"findings_by_category"`
	// FlowsBySinkKind — sink_kind → flow count.
	FlowsBySinkKind map[string]int `json:"flows_by_sink_kind"`
	// ConfidenceFloor is the taint pass's drop threshold (provenance for the UI).
	ConfidenceFloor float64 `json:"confidence_floor"`
	// TaintMethod / FlowMethod identify the producing passes (provenance).
	TaintMethod string `json:"taint_method,omitempty"`
	FlowMethod  string `json:"flow_method,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Sidecar shapes (read-side mirrors of the producer documents)
// ─────────────────────────────────────────────────────────────────────────────

type dfLinkSidecar struct {
	ID         string            `json:"id"`
	Source     string            `json:"source"`
	Target     string            `json:"target"`
	Relation   string            `json:"relation"`
	Confidence float64           `json:"confidence"`
	Properties map[string]string `json:"properties,omitempty"`
}

type dfSidecarDoc struct {
	Version int             `json:"version"`
	Method  string          `json:"method"`
	Links   []dfLinkSidecar `json:"links"`
}

// taintFindingSidecar mirrors the subset of links.SecurityFinding the dashboard
// projects. Decoded structurally so the dashboard does not import internal/links
// (and the substrate.TaintCategory enum) just to read a string category.
type taintFindingSidecar struct {
	Fingerprint     string   `json:"fingerprint"`
	SourceID        string   `json:"source_id"`
	SourcePrimitive string   `json:"source_primitive"`
	SourceLine      int      `json:"source_line"`
	SinkID          string   `json:"sink_id"`
	SinkPrimitive   string   `json:"sink_primitive"`
	SinkLine        int      `json:"sink_line"`
	Category        string   `json:"category"`
	Path            []string `json:"path"`
	Confidence      float64  `json:"confidence"`
	Repo            string   `json:"repo"`
}

type taintSidecarDoc struct {
	Version  int                   `json:"version"`
	Method   string                `json:"method"`
	Findings []taintFindingSidecar `json:"findings"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Sidecar path helpers (mirror internal/mcp)
// ─────────────────────────────────────────────────────────────────────────────

// dataFlowSidecarPath mirrors mcp.sidecarPath(group, "data-flow").
func dataFlowSidecarPath(group string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".archigraph", "groups", group+"-links-data-flow.json")
}

// taintSidecarPath mirrors the MCP taint sidecar path: the group links file with
// ".json" replaced by "-taint.json".
func taintSidecarPath(group string) string {
	return strings.TrimSuffix(defaultLinksFile(group), ".json") + "-taint.json"
}

// loadJSONSidecar reads + decodes a JSON sidecar; ok=false on any I/O or decode
// error (missing file is the common, benign case).
func loadJSONSidecar(path string, v any) bool {
	if path == "" {
		return false
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(buf, v) == nil
}

// dfEndpointTail returns the last ':' or '/'-delimited segment of an endpoint
// key, used as a readable label when the entity does not resolve.
func dfEndpointTail(key string) string {
	// strip a "<repo>::" prefix first.
	if _, local := dashSplitPrefixed(key); local != "" {
		key = local
	}
	if i := strings.LastIndexAny(key, ":/"); i >= 0 && i+1 < len(key) {
		return key[i+1:]
	}
	return key
}

// ─────────────────────────────────────────────────────────────────────────────
// Entity resolution: suffix → entity, across all repos
// ─────────────────────────────────────────────────────────────────────────────

// dfResolvedEntity is the minimal projection the report needs per entity.
type dfResolvedEntity struct {
	id         string
	repo       string
	name       string
	kind       string
	sourceFile string
	line       int
}

// dfEntityIndex resolves a sidecar endpoint key to a dfResolvedEntity by entity-
// ID suffix. Suffixes seen in more than one repo are recorded ambiguous and
// never used (defensive — mirrors normalizeLinkEndpoints).
type dfEntityIndex struct {
	bySuffix  map[string]dfResolvedEntity
	ambiguous map[string]bool
}

func (ix *dfEntityIndex) add(repo string, e dfResolvedEntity) {
	if _, seen := ix.bySuffix[e.id]; seen {
		ix.ambiguous[e.id] = true
		return
	}
	e.repo = repo
	ix.bySuffix[e.id] = e
}

// resolve returns the DataflowEndpoint for a sidecar key. Unknown/ambiguous keys
// fall back to the raw tail label and keep the raw key as entity_id.
func (ix *dfEntityIndex) resolve(key, primitive string, fallbackLine int) DataflowEndpoint {
	_, local := dashSplitPrefixed(key)
	if local == "" {
		local = key
	}
	if !ix.ambiguous[local] {
		if e, ok := ix.bySuffix[local]; ok {
			line := e.line
			if line == 0 && fallbackLine > 0 {
				line = fallbackLine
			}
			return DataflowEndpoint{
				EntityID:   e.repo + "/" + e.id,
				Repo:       e.repo,
				Name:       e.name,
				Kind:       e.kind,
				SourceFile: e.sourceFile,
				Line:       line,
				Primitive:  primitive,
			}
		}
	}
	return DataflowEndpoint{
		EntityID:  key,
		Name:      dfEndpointTail(key),
		Line:      fallbackLine,
		Primitive: primitive,
	}
}

// resolveStep is the lighter resolver used for path nodes.
func (ix *dfEntityIndex) resolveStep(key string) DataflowPathStep {
	_, local := dashSplitPrefixed(key)
	if local == "" {
		local = key
	}
	if !ix.ambiguous[local] {
		if e, ok := ix.bySuffix[local]; ok {
			return DataflowPathStep{EntityID: e.repo + "/" + e.id, Name: e.name, Repo: e.repo}
		}
	}
	return DataflowPathStep{EntityID: key, Name: dfEndpointTail(key)}
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: GET /api/dataflow/{group}
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleDataflow(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	if groupName == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	repoPaths, err := repoPathsForGroup(groupName)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q: %v", groupName, err))
		return
	}
	if len(repoPaths) == 0 {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q has no repos", groupName))
		return
	}

	q := r.URL.Query()
	filterCategory := strings.ToLower(strings.TrimSpace(q.Get("category")))
	filterSinkKind := strings.ToLower(strings.TrimSpace(q.Get("sink_kind")))

	report := DataflowReport{
		Group:              groupName,
		Findings:           []SecurityFindingView{},
		Flows:              []TaintFlow{},
		FindingsByCategory: map[string]int{},
		FlowsBySinkKind:    map[string]int{},
	}

	// Build the entity index from every repo's graph (suffix → entity).
	ix := &dfEntityIndex{
		bySuffix:  map[string]dfResolvedEntity{},
		ambiguous: map[string]bool{},
	}
	cachedGrp, _ := s.graphs.GetGroupCached(groupName)
	for _, rp := range repoPaths {
		var doc *graph.Document
		var rdr *fbreader.Reader
		if cachedGrp != nil {
			if dr, ok := cachedGrp.Repos[rp.Slug]; ok && dr != nil {
				doc = dr.Doc
				rdr = dr.Reader
			}
		}
		if doc == nil && rdr == nil {
			stateDir := daemon.StateDirForRepo(rp.Path)
			var loadErr error
			doc, loadErr = graph.LoadGraphFromDir(stateDir)
			if loadErr != nil {
				continue
			}
		}
		if rdr != nil {
			rdr.IterateEntities(func(e *fb.Entity) bool {
				ix.add(rp.Slug, dfResolvedEntity{
					id:         string(e.Id()),
					name:       string(e.Name()),
					kind:       string(e.Kind()),
					sourceFile: string(e.SourceFile()),
					line:       int(e.SourceLine()),
				})
				return true
			})
			continue
		}
		for i := range doc.Entities {
			ent := &doc.Entities[i]
			ix.add(rp.Slug, dfResolvedEntity{
				id:         ent.ID,
				name:       ent.Name,
				kind:       ent.Kind,
				sourceFile: ent.SourceFile,
				line:       ent.StartLine,
			})
		}
	}

	// ── Findings (ranked taint source→sink) ──────────────────────────────────
	var tdoc taintSidecarDoc
	if loadJSONSidecar(taintSidecarPath(groupName), &tdoc) {
		report.TaintMethod = tdoc.Method
		for _, f := range tdoc.Findings {
			cat := strings.ToLower(strings.TrimSpace(f.Category))
			if filterCategory != "" && cat != filterCategory {
				continue
			}
			src := ix.resolve(f.SourceID, f.SourcePrimitive, f.SourceLine)
			sink := ix.resolve(f.SinkID, f.SinkPrimitive, f.SinkLine)
			steps := make([]DataflowPathStep, 0, len(f.Path))
			for _, p := range f.Path {
				steps = append(steps, ix.resolveStep(p))
			}
			hops := len(f.Path) - 1
			if hops < 0 {
				hops = 0
			}
			report.Findings = append(report.Findings, SecurityFindingView{
				Fingerprint: f.Fingerprint,
				Category:    f.Category,
				Confidence:  f.Confidence,
				Source:      src,
				Sink:        sink,
				Path:        steps,
				Hops:        hops,
				Explanation: dfFindingExplanation(f, src, sink, hops),
			})
			report.FindingsByCategory[f.Category]++
		}
	}
	// Rank findings by confidence desc, then category, then fingerprint (stable).
	sort.SliceStable(report.Findings, func(i, j int) bool {
		a, b := report.Findings[i], report.Findings[j]
		if a.Confidence != b.Confidence {
			return a.Confidence > b.Confidence
		}
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		return a.Fingerprint < b.Fingerprint
	})
	report.TotalFindings = len(report.Findings)

	// ── Flows (request-input → sink DATA_FLOWS_TO edges) ─────────────────────
	var fdoc dfSidecarDoc
	if loadJSONSidecar(dataFlowSidecarPath(groupName), &fdoc) {
		report.FlowMethod = fdoc.Method
		for _, l := range fdoc.Links {
			props := l.Properties
			if props == nil {
				props = map[string]string{}
			}
			sinkKind := strings.TrimSpace(props["sink_kind"])
			if filterSinkKind != "" && strings.ToLower(sinkKind) != filterSinkKind {
				continue
			}
			hopCount := 0
			if v := strings.TrimSpace(props["hop_count"]); v != "" {
				hopCount, _ = strconv.Atoi(v)
			}
			report.Flows = append(report.Flows, TaintFlow{
				ID:         l.ID,
				Source:     ix.resolve(l.Source, "", 0),
				Sink:       ix.resolve(l.Target, "", 0),
				Relation:   l.Relation,
				Confidence: l.Confidence,
				Field:      strings.TrimSpace(props["field"]),
				SinkKind:   sinkKind,
				HopPath:    strings.TrimSpace(props["hop_path"]),
				HopCount:   hopCount,
			})
			if sinkKind != "" {
				report.FlowsBySinkKind[sinkKind]++
			}
		}
	}
	// Stable flow order: by source name, then sink name, then id.
	sort.SliceStable(report.Flows, func(i, j int) bool {
		a, b := report.Flows[i], report.Flows[j]
		if a.Source.Name != b.Source.Name {
			return a.Source.Name < b.Source.Name
		}
		if a.Sink.Name != b.Sink.Name {
			return a.Sink.Name < b.Sink.Name
		}
		return a.ID < b.ID
	})
	report.TotalFlows = len(report.Flows)

	report.ConfidenceFloor = dataflowConfidenceFloor

	writeReportJSON(w, report)
}

// dataflowConfidenceFloor mirrors links.TaintFindingFloor() — the taint pass
// drops findings below this. Surfaced for UI provenance. Kept as a local const
// (rather than importing internal/links) so the dashboard reader stays decoupled
// from the producer's enum surface; the value is asserted in handlers_dataflow_test.go.
const dataflowConfidenceFloor = 0.5

// dfCategoryNarrative returns a short prose label for a finding category.
// Mirrors mcp.categoryNarrative.
func dfCategoryNarrative(cat string) string {
	switch cat {
	case "sql_injection":
		return "SQL injection"
	case "command_injection":
		return "Command / dynamic-code injection"
	case "path_traversal":
		return "Path traversal"
	case "xss":
		return "Cross-site scripting"
	case "redos":
		return "Regular-expression DoS"
	case "deserialization":
		return "Unsafe deserialization"
	case "ssrf":
		return "Server-side request forgery"
	}
	return "Security finding"
}

// dfFindingExplanation builds a human-readable lead for a finding. Mirrors
// mcp.buildFindingExplanation so the dashboard and MCP narrate identically.
func dfFindingExplanation(f taintFindingSidecar, src, sink DataflowEndpoint, hops int) string {
	cat := dfCategoryNarrative(f.Category)
	if hops == 0 {
		return fmt.Sprintf(
			"%s: tainted input from %s (%s, line %d) reaches the sink %s (%s, line %d) inside the same function with no sanitizer in between. Confidence %.2f.",
			cat, src.Name, f.SourcePrimitive, f.SourceLine, sink.Name, f.SinkPrimitive, f.SinkLine, f.Confidence,
		)
	}
	return fmt.Sprintf(
		"%s: tainted input from %s (%s, line %d) flows through %d call hop(s) to the sink %s (%s, line %d) without a sanitizer of category %s on the path. Confidence %.2f.",
		cat, src.Name, f.SourcePrimitive, f.SourceLine, hops, sink.Name, f.SinkPrimitive, f.SinkLine, f.Category, f.Confidence,
	)
}
