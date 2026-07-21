// Package links implements the cross-repo link passes (P1/P2/P3) that run
// after per-repo indexing has produced <repo>/.grafel/graph.json for
// every repo in a group. The output is a shared
// ~/.grafel/groups/<group>-links.json document plus a
// candidates.json sidecar for mid-confidence matches and a
// rejections.json file used to suppress already-rejected candidates.
//
// Pass kinds:
//
//   - P1 (import_pass.go) — structural cross-repo imports/calls edges
//   - P2 (label_pass.go) — TF-IDF + kind-compat shared-label match
//   - P3 (string_pass.go) — string-pattern catalog (HTTP routes, ARNs, etc.)
//   - P4 (http_pass.go) — HTTP route ↔ fetch matcher
//   - P5 (openapi_pass.go) — OpenAPI spec → HTTP route linker
//   - P6 (grpc_pass.go) — gRPC client-stub → server-impl linker
//   - P7 (topic_pass.go) — message-topic publisher ↔ subscriber linker
//   - P8 (sameas_pass.go) — cross-language SAME_AS linker for shared-lib
//     domain models
//
// All passes are idempotent and method-segregated: re-running a pass only
// rewrites the entries whose `method` matches that pass; entries from
// other passes are preserved.
package links

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// SchemaVersion is the integer version of the links file shape.
const SchemaVersion = 1

// Relation values used in the relation field.
const (
	RelationCalls       = "calls"
	RelationImports     = "imports"
	RelationSharedLabel = "shared_label"
	RelationStringMatch = "string_match"
	// RelationSameAs marks an undirected cross-language identity edge
	// between two shared-library domain models that represent the same
	// concept (emitted by the P8 same-as pass — see sameas_pass.go).
	RelationSameAs = "same_as"
	// RelationRoutesTo marks an intra-repo HTTP self-call edge where the
	// caller and the server-side handler live in the same repository
	// (#2585). These are emitted with method = MethodHTTPSelf so they can
	// be queried and displayed independently of cross-repo CALLS links.
	RelationRoutesTo = "routes_to"
)

// Method values identify which pass produced an entry. Method-segregated
// overwrite uses these.
const (
	MethodImport             = "import"
	MethodLabelMatch         = "label_match"
	MethodLabelMatchResolved = "label_match+resolved"
	MethodString             = "string"
	MethodStringResolved     = "string+resolved"
)

// (Re-declared in http_pass.go; this comment is the public anchor.)
//
// MethodHTTP identifies cross-repo HTTP route ↔ fetch links emitted by
// runHTTPPass — see http_pass.go for the contract. Declared in the
// pass file so the constant lives next to its consumer.

// Link is one cross-repo edge.
type Link struct {
	ID              string     `json:"id"`
	Source          string     `json:"source"`
	Target          string     `json:"target"`
	Relation        string     `json:"relation"`
	Method          string     `json:"method"`
	Confidence      float64    `json:"confidence"`
	Channel         *string    `json:"channel"`
	Identifier      *string    `json:"identifier"`
	DiscoveredAt    string     `json:"discovered_at"`
	SourceLocations [][]string `json:"source_locations,omitempty"`

	// MatchQuality, when set, annotates how a method-specific matcher
	// (currently only the HTTP pass) selected this producer:
	//   "exact_verb"   — consumer's HTTP verb matched the producer's
	//   "any_fallback" — consumer had a specific verb, no specific-verb
	//                    producer existed; matched against an ANY-verb
	//                    producer (Django ViewSet without per-method
	//                    routing, etc.)
	//   "wildcard"     — both sides used ANY (no specific verb on either)
	// Empty for any non-HTTP method.
	MatchQuality string `json:"match_quality,omitempty"`

	// Reason is populated only on candidate entries (not in links.json).
	Reason string `json:"reason,omitempty"`

	// Resolution is populated only on rejection entries.
	Resolution *Resolution `json:"resolution,omitempty"`

	// Properties carries optional key/value annotations set by specific
	// matching passes. Currently used by the HTTP pass to record
	// prefix_normalized when a cross-repo consumer path was resolved by
	// prepending a well-known API/version prefix (e.g. "/api/v1") to the
	// consumer's raw path. Empty for all other passes.
	Properties map[string]string `json:"properties,omitempty"`
}

// Resolution annotates a rejected link.
type Resolution struct {
	At     string `json:"at"`
	Reason string `json:"reason"`
}

// Document is the on-disk shape of every links/candidates/rejections file.
type Document struct {
	Version int    `json:"version"`
	Links   []Link `json:"links"`
}

// PassResult is returned from each pass to give callers basic counters.
type PassResult struct {
	Pass       string
	LinksAdded int
	Candidates int
	Skipped    int // suppressed by rejection list

	// OrphanCalls is the number of consumer-side HTTP endpoint hits that
	// were not matched to any producer this pass. Reset to zero on every
	// invocation so successive index runs never accumulate.
	OrphanCalls int

	// CrossRepoResolved is the number of consumer-side HTTP endpoint hits
	// that were matched and had a cross-repo link emitted this pass.
	// This is the single source of truth; endpoint_tools derives its
	// cross_repo_resolved counter from this value via the links file.
	CrossRepoResolved int

	// CrossRepoResolveAttempts is the total number of (consumer, producer-repo)
	// match attempts the HTTP pass made — i.e. the size of the search space
	// for cross-repo resolution. CrossRepoResolveHitsByStrategy + the
	// no-match misses sum to this value. (#2669)
	CrossRepoResolveAttempts int

	// CrossRepoResolveHitsByStrategy reports how many consumer→producer
	// resolutions each strategy contributed. Keys (stable):
	//   - "exact"               byPath bucket hit with a same-name producer
	//   - "prefix_stripped"     prefix-injection retry (#2569)
	//   - "mount_prefix_added"  consumer-side mount-prefix retry (#2702)
	//   - "case_style_normalized" per-segment camelCase↔snake_case↔kebab-case
	//                           normalization (#2703, broadened in #3169)
	//   - "param_normalized"    path-param NAME bridge, e.g. {clientId}↔{pk}
	//                           with identical {*}-collapsed shape (#2808)
	//   - "literal_param_fill"  a CONCRETE caller segment fills a producer
	//                           param slot, e.g. /recents/buildings ↔
	//                           /api/v1/recents/{pk} (#2808)
	//   - "url_pattern"         normalizeURLPattern fallback (#2588)
	//   - "dynamic_suffix_match" dynamic-baseURL call (`${apiUrl}/x/y`) whose
	//                           static suffix uniquely + specifically matched a
	//                           backend endpoint after stripping the runtime
	//                           prefix (#2813)
	//   - "graphql_root"        consumer pointed at /graphql root, producer
	//                           per-field synthetic registered there (#1496)
	// (#2669)
	CrossRepoResolveHitsByStrategy map[string]int

	// CrossRepoResolveMissesByReason reports why consumer hits remained
	// orphaned. Keys (stable):
	//   - "no_endpoint_match"  byPath / prefix / url_pattern all missed —
	//                          no producer with a compatible (verb, path)
	//                          exists in any other repo
	//   - "dynamic_baseurl"    consumer path is a template-only string
	//                          (e.g. "/{apiUrl}/things") whose canonical
	//                          form collapses to a single param and cannot
	//                          be matched without runtime context
	// (#2669)
	CrossRepoResolveMissesByReason map[string]int

	// ResidualCandidates is the total number of ranked producer candidates the
	// #2813 dynamic-baseurl static-suffix matcher surfaced for consumers it
	// could NOT auto-link (ambiguous: multiple candidates, or a too-generic
	// suffix). These consumers stay orphaned (counted under the
	// "dynamic_baseurl" miss reason) and are exposed to grafel-resolve via
	// the per-repo dynamic_baseurl_endpoint enrichment candidate; this counter
	// reports how much candidate signal the suffix matcher found for them. (#2813)
	ResidualCandidates int
}

// RunResult is the aggregate of all three passes from RunAllPasses.
type RunResult struct {
	Group       string
	GraphsDir   string
	OutLinks    string
	OutCandid   string
	OutReject   string
	Results     []PassResult
	TotalLinks  int
	TotalCandid int
}

// MakeID computes the deterministic 8-char hex id for a link, per spec:
// sha8(source + "→" + target + ":" + method).
func MakeID(source, target, method string) string {
	h := sha256.New()
	h.Write([]byte(source))
	h.Write([]byte("→"))
	h.Write([]byte(target))
	h.Write([]byte(":"))
	h.Write([]byte(method))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)[:8]
}

// nowFn is the clock used for DiscoveredAt; overridable in tests.
var nowFn = func() time.Time { return time.Now().UTC() }

func discoveredAt() string {
	return nowFn().Format(time.RFC3339Nano)
}

// orderEndpoints returns (a, b) sorted lexicographically. Used so that
// undirected pair-based passes (P2/P3) emit a stable, deduplicable id.
func orderEndpoints(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

// strPtr is a tiny helper: pointer-to-string for omitempty-ish JSON.
func strPtr(s string) *string {
	v := s
	return &v
}

// Paths returns the canonical on-disk locations for a group's three files.
type Paths struct {
	Links      string
	Candidates string
	Rejections string
	ScanCache  string
	// LinkPassStats is the JSON file where RunAllPasses writes the
	// PassResult counter snapshot at the end of every run. Picked up by
	// grafel_stats so MCP callers can read the resolve-strategy
	// telemetry (#2669) without re-running the pass pipeline.
	LinkPassStats string
}

// PathsFor returns the canonical paths under grafelHome ("" → ~/.grafel)
// for the given group.
func PathsFor(grafelHome, group string) (Paths, error) {
	if group == "" {
		return Paths{}, errors.New("group name required")
	}
	if grafelHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		grafelHome = filepath.Join(home, ".grafel")
	}
	groupsDir := filepath.Join(grafelHome, "groups")
	cacheDir := filepath.Join(grafelHome+"-cache", group, "string-scan")
	if !strings.HasSuffix(grafelHome, ".grafel") {
		// Use sibling cache dir under grafelHome for tests.
		cacheDir = filepath.Join(grafelHome, "cache", group, "string-scan")
	}
	return Paths{
		Links:         filepath.Join(groupsDir, group+"-links.json"),
		Candidates:    filepath.Join(groupsDir, group+"-link-candidates.json"),
		Rejections:    filepath.Join(groupsDir, group+"-link-rejections.json"),
		LinkPassStats: filepath.Join(groupsDir, group+"-link-pass-stats.json"),
		ScanCache:     cacheDir,
	}, nil
}

// RunAllPasses runs P1, P2, then P3 against the per-repo graph documents
// found under graphsDir. graphsDir is expected to contain one
// <slug>/graph.json per repo. grafelHome (use "" for default) is
// where the output JSON files land.
func RunAllPasses(group, graphsDir, grafelHome string) (*RunResult, error) {
	paths, err := PathsFor(grafelHome, group)
	if err != nil {
		return nil, err
	}
	graphs, err := loadAllGraphs(graphsDir)
	if err != nil {
		return nil, err
	}
	rejects, err := loadRejections(paths.Rejections)
	if err != nil {
		return nil, err
	}

	res := &RunResult{
		Group:     group,
		GraphsDir: graphsDir,
		OutLinks:  paths.Links,
		OutCandid: paths.Candidates,
		OutReject: paths.Rejections,
	}

	p1, err := runImportPass(graphs, paths, rejects)
	if err != nil {
		return nil, fmt.Errorf("import pass: %w", err)
	}
	res.Results = append(res.Results, p1)

	// #2761 — Phase 0 substrate constant propagation. Runs after the
	// import pass so the in-memory IMPORTS-edge index reflects everything
	// the structural linker has seen. The returned Resolver is used to
	// canonicalise consumer-side dynamic_baseurl HTTP endpoint paths
	// before the HTTP pass below sees them, lifting those orphans into
	// the resolvable bucket.
	pCP, resolver, err := runConstantPropagationPass(graphs, paths, rejects)
	if err != nil {
		return nil, fmt.Errorf("constant propagation pass: %w", err)
	}
	// applyResolverToConsumerHTTP mutates the in-memory entityNode
	// Properties (path / url_kind / substrate_*) for consumer-side
	// http_endpoint_call entities whose url_kind was "dynamic_baseurl"
	// before substitution. Mutated entities feed directly into the
	// downstream HTTP pass below because Go slice-of-struct headers
	// share the underlying array. Folded the mutated count into the
	// pass's Candidates counter so it surfaces in PassResult telemetry
	// without conflating with the cross-file RESOLVES_TO LinksAdded
	// count emitted by runConstantPropagationPass.
	pCP.Candidates = applyResolverToConsumerHTTP(graphs, resolver)
	res.Results = append(res.Results, pCP)

	// #2764 — Phase 1A effect classification. Runs after constant
	// propagation so it sees any consumer-side path rewrites; runs
	// before the label / string / HTTP passes so downstream queries
	// reading entity.Properties see the effect annotation. Mutates
	// entity.Properties in-memory; emits a <group>-links-effects.json
	// sidecar for the MCP grafel_effects tool.
	pEP, err := runEffectPropagationPass(graphs, paths, rejects)
	if err != nil {
		return nil, fmt.Errorf("effect propagation pass: %w", err)
	}
	res.Results = append(res.Results, pEP)

	// #2772 — Phase 2B taint-flow analysis. Runs after effect
	// classification so taint roles share the same function-entity
	// binding shape; runs before the label / string / HTTP passes so
	// downstream queries reading entity.Properties see the taint_role
	// annotation. Mutates entity.Properties in-memory and emits the
	// <group>-links-taint.json sidecar consumed by the MCP
	// grafel_security_findings tool.
	pTF, err := runTaintFlowPass(graphs, paths, rejects)
	if err != nil {
		return nil, fmt.Errorf("taint flow pass: %w", err)
	}
	res.Results = append(res.Results, pTF)

	p2, err := runLabelPass(graphs, paths, rejects)
	if err != nil {
		return nil, fmt.Errorf("label pass: %w", err)
	}
	res.Results = append(res.Results, p2)

	p3, err := runStringPass(graphs, paths, rejects)
	if err != nil {
		return nil, fmt.Errorf("string pass: %w", err)
	}
	res.Results = append(res.Results, p3)

	// P4 — cross-repo HTTP route ↔ fetch matcher (this pass). Runs
	// after the structural / label / string passes so that its
	// method-segregated entries are rewritten cleanly without
	// disturbing earlier output.
	p4, err := runHTTPPass(graphs, paths, rejects)
	if err != nil {
		return nil, fmt.Errorf("http pass: %w", err)
	}
	res.Results = append(res.Results, p4)

	// P5 — OpenAPI spec → HTTP route cross-linker. Uses openapi_operation
	// entities emitted by the patterns extractor as the pivot to create
	// consumer-caller → producer-handler links with method=openapi-spec.
	p5, err := runOpenAPISpecPass(graphs, paths, rejects)
	if err != nil {
		return nil, fmt.Errorf("openapi-spec pass: %w", err)
	}
	res.Results = append(res.Results, p5)

	// P6 — cross-repo gRPC client-stub → server-impl linker. Uses
	// SCOPE.GrpcMethod entities with canonical name grpc:Service/Method
	// emitted by the gRPC engine pass (#725) as the join key.
	p6, err := runGRPCPass(graphs, paths, rejects)
	if err != nil {
		return nil, fmt.Errorf("grpc pass: %w", err)
	}
	res.Results = append(res.Results, p6)

	// P7 — cross-repo message-topic publisher↔subscriber linker. Uses
	// SCOPE.MessageTopic entities emitted by the Kafka/SNS/SQS/EventBridge
	// passes as the join key, matched by canonical topic Name.
	p7, err := runTopicPass(graphs, paths, rejects)
	if err != nil {
		return nil, fmt.Errorf("topic pass: %w", err)
	}
	res.Results = append(res.Results, p7)

	// P8 — cross-language SAME_AS linker for shared-library domain models.
	// Conservatively links same-named domain models (Component/Model/Schema
	// kinds) that live in shared-lib repos and share enough field names to
	// be the same concept across languages (e.g. py-shared Order ↔
	// js-shared Order). See sameas_pass.go.
	p8, err := runSameAsPass(graphs, paths, rejects)
	if err != nil {
		return nil, fmt.Errorf("same-as pass: %w", err)
	}
	res.Results = append(res.Results, p8)

	// #2766 — Phase 1B reachability + dead-code identification. Runs
	// last so it observes the in-memory entity/edge state after every
	// upstream pass has had a chance to mutate it (e.g. the constant
	// propagation pass rewriting consumer-side HTTP endpoint paths).
	pReach, err := runReachabilityPass(group, graphs, paths)
	if err != nil {
		return nil, fmt.Errorf("reachability pass: %w", err)
	}
	res.Results = append(res.Results, pReach)

	// #2770 — Phase 2A payload-shape drift detection. Runs after the
	// HTTP pass has emitted MethodHTTP cross-repo links (P4 above);
	// reads them back from disk and joins them to the per-language
	// payload-shape facts emitted by the substrate sniffers. Findings
	// land in a sidecar JSON document read by the
	// grafel_payload_drift MCP tool.
	pDrift, err := runPayloadDriftPass(group, graphs, paths)
	if err != nil {
		return nil, fmt.Errorf("payload drift pass: %w", err)
	}
	res.Results = append(res.Results, pDrift)

	// #2774 / #2775 — Phase 3A pure-function tagging. Derivative of
	// Phase 1A: every function-like entity without an effect set is
	// marked pure=true with a low confidence floor. Runs after the
	// effect propagation pass so it observes the in-memory effect
	// annotations stamped on entity Properties.
	pPure, err := runPureFunctionPass(graphs, paths)
	if err != nil {
		return nil, fmt.Errorf("pure-function pass: %w", err)
	}
	res.Results = append(res.Results, pPure)

	// #2774 / #2775 — Phase 3B module-cycle detection via Tarjan SCC
	// over IMPORTS edges. Language-agnostic; stamps `module_cycle_id`
	// on every cycle participant and emits a sidecar with full SCC
	// membership for the grafel_import_cycles MCP tool.
	pCycle, err := runModuleCyclePass(graphs, paths)
	if err != nil {
		return nil, fmt.Errorf("module-cycle pass: %w", err)
	}
	res.Results = append(res.Results, pCycle)

	// #2774 / #2775 — Phase 3C intra-procedural def-use chains. Per-
	// language sniffer lifts defs and uses, the pass composes reaching-
	// definitions (last-write-wins), stamps a compact summary on the
	// owning function entity, and persists the full chain set in a
	// sidecar for the grafel_def_use MCP tool.
	pDU, err := runDefUsePass(graphs, paths)
	if err != nil {
		return nil, fmt.Errorf("def-use pass: %w", err)
	}
	res.Results = append(res.Results, pDU)

	// #2774 / #2775 — Phase 3D template-pattern catalog (i18n / log-
	// format / SQL templates). Per-language sniffer lifts every recog-
	// nised template literal; the pass persists them in a sidecar for
	// the grafel_template_patterns MCP tool.
	pTP, err := runTemplatePatternPass(graphs, paths)
	if err != nil {
		return nil, fmt.Errorf("template-pattern pass: %w", err)
	}
	res.Results = append(res.Results, pTP)

	// #4831 (epic #4820) — universal cyclomatic-complexity enrichment. Walks
	// EVERY function-like entity with readable source-line info and stamps
	// cyclomatic_complexity / branch_count via the validated
	// substrate.ComputeFunctionComplexity. Runs BEFORE the data-flow pass so
	// this pass is the single source of truth: data-flow's per-handler stamp is
	// idempotent and finds the value already present (both call the same
	// ComputeFunctionComplexity, so they can never diverge). Generalises part
	// (a) (#4821), which only stamped data-flow-bound handlers.
	pCx, err := runComplexityPass(graphs, paths)
	if err != nil {
		return nil, fmt.Errorf("complexity pass: %w", err)
	}
	res.Results = append(res.Results, pCx)

	// #3628 area #22 — SCOPED request-input → sink dataflow. Emits
	// DATA_FLOWS_TO links (intra-function + one local-call hop) from the
	// per-language substrate dataflow sniffers. Honest-partial: see
	// internal/links/dataflow_pass.go.
	pDF, err := runDataFlowPass(graphs, paths, rejects)
	if err != nil {
		return nil, fmt.Errorf("data-flow pass: %w", err)
	}
	res.Results = append(res.Results, pDF)

	for _, r := range res.Results {
		res.TotalLinks += r.LinksAdded
		res.TotalCandid += r.Candidates
	}

	// #2669: persist the resolve-strategy telemetry to a small sidecar JSON.
	// MCP's grafel_stats reads this file so callers see the counters
	// without re-running the link pipeline. Write failure is non-fatal — the
	// link emission itself succeeded and the stats file is purely advisory.
	if err := writeLinkPassStats(paths.LinkPassStats, res); err != nil {
		// Surface as a warning but do not fail the whole run; downstream
		// consumers gracefully degrade to absent telemetry.
		fmt.Fprintf(os.Stderr, "grafel: warning: write link pass stats: %v\n", err)
	}
	return res, nil
}

// LinkPassStatsDocument is the on-disk shape of <group>-link-pass-stats.json.
// Mirrors the per-pass counters that PassResult carries, plus a wall-clock
// timestamp so MCP responses can tell callers when the snapshot was taken.
// (#2669)
type LinkPassStatsDocument struct {
	Version     int                  `json:"version"`
	Group       string               `json:"group"`
	WrittenAt   string               `json:"written_at"`
	Passes      []LinkPassStatsEntry `json:"passes"`
	HTTPSummary *HTTPResolveStats    `json:"http_resolve,omitempty"`
}

// LinkPassStatsEntry is one per-pass counter snapshot.
type LinkPassStatsEntry struct {
	Pass              string `json:"pass"`
	LinksAdded        int    `json:"links_added"`
	Candidates        int    `json:"candidates"`
	Skipped           int    `json:"skipped"`
	OrphanCalls       int    `json:"orphan_calls,omitempty"`
	CrossRepoResolved int    `json:"cross_repo_resolved,omitempty"`
}

// HTTPResolveStats is the structured resolve-strategy telemetry surfaced
// at the document root so MCP can return it without descending into the
// per-pass list. (#2669)
type HTTPResolveStats struct {
	Attempts       int            `json:"cross_repo_resolve_attempts"`
	HitsByStrategy map[string]int `json:"cross_repo_resolve_hits_by_strategy,omitempty"`
	MissesByReason map[string]int `json:"cross_repo_resolve_misses_by_reason,omitempty"`
	// ResidualCandidates is the total ranked producer candidates the #2813
	// dynamic-baseurl static-suffix matcher surfaced for orphans it declined to
	// auto-link. Surfaced so the resolve surface can report candidate signal.
	ResidualCandidates int `json:"residual_candidates,omitempty"`
}

// writeLinkPassStats serialises every PassResult counter on res to the
// given path. The function is idempotent — overwriting the previous run's
// stats — and atomic via os.WriteFile (rename-on-write under the hood).
func writeLinkPassStats(path string, res *RunResult) error {
	if path == "" {
		return nil
	}
	doc := LinkPassStatsDocument{
		Version:   1,
		Group:     res.Group,
		WrittenAt: discoveredAt(),
	}
	for _, r := range res.Results {
		doc.Passes = append(doc.Passes, LinkPassStatsEntry{
			Pass:              r.Pass,
			LinksAdded:        r.LinksAdded,
			Candidates:        r.Candidates,
			Skipped:           r.Skipped,
			OrphanCalls:       r.OrphanCalls,
			CrossRepoResolved: r.CrossRepoResolved,
		})
		if r.Pass == "http" {
			doc.HTTPSummary = &HTTPResolveStats{
				Attempts:           r.CrossRepoResolveAttempts,
				HitsByStrategy:     r.CrossRepoResolveHitsByStrategy,
				MissesByReason:     r.CrossRepoResolveMissesByReason,
				ResidualCandidates: r.ResidualCandidates,
			}
		}
	}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}

// ReadLinkPassStats loads the persisted resolve-strategy telemetry written
// by writeLinkPassStats. Returns (nil, nil) — i.e. silently absent — when
// the file does not exist, so callers can treat missing telemetry as
// "pass has not run yet for this group" without distinguishing it from a
// real I/O error. (#2669)
func ReadLinkPassStats(path string) (*LinkPassStatsDocument, error) {
	if path == "" {
		return nil, nil
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc LinkPassStatsDocument
	if err := json.Unmarshal(buf, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// repoGraph is a small projection of graph.Document used by passes.
type repoGraph struct {
	Repo     string
	Path     string
	Entities []entityNode
	Edges    []edgeRef
	// fileRoot is the absolute directory of the repo (parent of .grafel).
	FileRoot string
}

type entityNode struct {
	ID      string // local entity id
	Name    string // raw name
	Kind    string // class/function/...
	Subtype string // package/function/...; required to discriminate
	// real external packages (subtype=package) from bare-name built-in
	// placeholders (subtype=function). See issue #566 / import_pass.go.
	SourceFile string            // relative path
	StartLine  int               // 1-indexed body start (0 when unknown)
	EndLine    int               // 1-indexed body end (0 when unknown)
	Properties map[string]string // optional
}

type edgeRef struct {
	FromID string
	ToID   string
	Kind   string // imports, calls, ...
}

// loadAllGraphs walks graphsDir and returns one repoGraph per slug directory
// that contains graph.fb or graph.json (graph.fb preferred per ADR-0016).
//
// Previously this function only walked for graph.json, which silently skipped
// repos indexed with the default fb-only mode (ADR-0016 flip-day, issue #808).
// The fix: collect directories that own either graph file, then load each via
// graph.LoadGraphFromDir so the same fb-first / json-fallback logic applies
// throughout the link passes. Fixes #1374 item #4 (cross_repo_links = 0).
func loadAllGraphs(graphsDir string) ([]repoGraph, error) {
	if graphsDir == "" {
		return nil, errors.New("graphs dir required")
	}
	fi, err := os.Stat(graphsDir)
	if err != nil {
		return nil, err
	}

	// Collect the set of directories that contain at least one graph file.
	// Using a set avoids double-loading when both graph.fb and graph.json
	// are present in the same directory.
	dirSet := map[string]bool{}
	if !fi.IsDir() {
		// Caller passed a graph.json directly — treat its parent as the dir.
		dirSet[filepath.Dir(graphsDir)] = true
	} else {
		err := filepath.WalkDir(graphsDir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			base := filepath.Base(p)
			if base == "graph.json" || graph.IsGraphFileName(base) { // #5891 gen files
				dirSet[filepath.Dir(p)] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// Deterministic iteration order.
	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	graphs := make([]repoGraph, 0, len(dirs))
	seen := map[string]bool{}
	for _, dir := range dirs {
		// Resolve symlinks so FileRoot points at the real repository.
		realDir := dir
		if rp, err := filepath.EvalSymlinks(dir); err == nil {
			realDir = rp
		}

		doc, err := graph.LoadGraphFromDir(dir)
		if err != nil {
			return nil, fmt.Errorf("load graph in %s: %w", dir, err)
		}

		// Prefer the staged directory name as the canonical slug.
		// When called via stageGraphsDir, each graph file is at
		// <tmp>/<fleet-slug>/graph.{fb,json} so filepath.Base(dir) is the
		// exact fleet slug (dash form). doc.Repo, by contrast, is set from
		// the repoTag written at index time — historically derived from the
		// on-disk directory basename, which may use underscores where the
		// fleet config uses dashes (e.g. "acme_core" vs "acme-core").
		// Using the directory name here means the emitter writes the correct
		// fleet slug into Link.Source / Link.Target, so downstream readers
		// (MCP find_paths, dashboard graph merge) never need to alias-map
		// the underscore form. Fixes #1701.
		//
		// Special case: when the graph file lives in a hidden subdirectory
		// (e.g. <repo>/.grafel/graph.json in test fixtures and legacy
		// on-disk layouts), the immediate basename is ".grafel" — not
		// a useful slug. In that case fall up one level to the repo directory
		// name, which agrees with doc.Repo. If that is also unhelpful, fall
		// back to doc.Repo.
		dirBase := filepath.Base(dir)
		if strings.HasPrefix(dirBase, ".") {
			// Hidden sub-dir (e.g. .grafel): use parent directory name.
			dirBase = filepath.Base(filepath.Dir(dir))
		}
		repoName := dirBase
		if repoName == "" || repoName == "." {
			repoName = doc.Repo
		}
		if repoName == "" {
			// Final fallback: use the symlink-resolved directory.
			repoName = filepath.Base(realDir)
		}
		if seen[repoName] {
			continue
		}
		seen[repoName] = true
		// Also mark the graph-embedded repo name as seen so that if the same
		// repo is encountered a second time (e.g. both graph.fb and graph.json
		// present, or a symlink loop), the underscore variant won't be loaded
		// as a separate repo.
		if doc.Repo != "" && doc.Repo != repoName {
			seen[doc.Repo] = true
		}

		rg := repoGraph{
			Repo:     repoName,
			Path:     filepath.Join(dir, "graph.json"), // keep for logging/compat
			FileRoot: filepath.Dir(realDir),
		}
		for _, e := range doc.Entities {
			rg.Entities = append(rg.Entities, entityNode{
				ID:         e.ID,
				Name:       e.Name,
				Kind:       e.Kind,
				Subtype:    e.Subtype,
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
				EndLine:    e.EndLine,
				Properties: e.PropsSnapshot(),
			})
		}
		for _, r := range doc.Relationships {
			rg.Edges = append(rg.Edges, edgeRef{FromID: r.FromID, ToID: r.ToID, Kind: r.Kind})
		}
		graphs = append(graphs, rg)
	}
	return graphs, nil
}

// entityKey returns the wire-format endpoint string used in Link.Source/Target.
func entityKey(repo, entityID string) string {
	return repo + "::" + entityID
}
