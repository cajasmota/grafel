// Package links implements the cross-repo link passes (P1/P2/P3) that run
// after per-repo indexing has produced <repo>/.archigraph/graph.json for
// every repo in a group. The output is a shared
// ~/.archigraph/groups/<group>-links.json document plus a
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
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
}

// PathsFor returns the canonical paths under archigraphHome ("" → ~/.archigraph)
// for the given group.
func PathsFor(archigraphHome, group string) (Paths, error) {
	if group == "" {
		return Paths{}, errors.New("group name required")
	}
	if archigraphHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		archigraphHome = filepath.Join(home, ".archigraph")
	}
	groupsDir := filepath.Join(archigraphHome, "groups")
	cacheDir := filepath.Join(archigraphHome+"-cache", group, "string-scan")
	if !strings.HasSuffix(archigraphHome, ".archigraph") {
		// Use sibling cache dir under archigraphHome for tests.
		cacheDir = filepath.Join(archigraphHome, "cache", group, "string-scan")
	}
	return Paths{
		Links:      filepath.Join(groupsDir, group+"-links.json"),
		Candidates: filepath.Join(groupsDir, group+"-link-candidates.json"),
		Rejections: filepath.Join(groupsDir, group+"-link-rejections.json"),
		ScanCache:  cacheDir,
	}, nil
}

// RunAllPasses runs P1, P2, then P3 against the per-repo graph documents
// found under graphsDir. graphsDir is expected to contain one
// <slug>/graph.json per repo. archigraphHome (use "" for default) is
// where the output JSON files land.
func RunAllPasses(group, graphsDir, archigraphHome string) (*RunResult, error) {
	paths, err := PathsFor(archigraphHome, group)
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

	for _, r := range res.Results {
		res.TotalLinks += r.LinksAdded
		res.TotalCandid += r.Candidates
	}
	return res, nil
}

// repoGraph is a small projection of graph.Document used by passes.
type repoGraph struct {
	Repo     string
	Path     string
	Entities []entityNode
	Edges    []edgeRef
	// fileRoot is the absolute directory of the repo (parent of .archigraph).
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
			if base == "graph.json" || base == "graph.fb" {
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
		// fleet config uses dashes (e.g. "upvate_core" vs "upvate-core").
		// Using the directory name here means the emitter writes the correct
		// fleet slug into Link.Source / Link.Target, so downstream readers
		// (MCP find_paths, dashboard graph merge) never need to alias-map
		// the underscore form. Fixes #1701.
		//
		// Special case: when the graph file lives in a hidden subdirectory
		// (e.g. <repo>/.archigraph/graph.json in test fixtures and legacy
		// on-disk layouts), the immediate basename is ".archigraph" — not
		// a useful slug. In that case fall up one level to the repo directory
		// name, which agrees with doc.Repo. If that is also unhelpful, fall
		// back to doc.Repo.
		dirBase := filepath.Base(dir)
		if strings.HasPrefix(dirBase, ".") {
			// Hidden sub-dir (e.g. .archigraph): use parent directory name.
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
				Properties: e.Properties,
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
