// dead_code.go — MCP handler for grafel_dead_code (#2766 Phase 1B).
//
// Returns entities marked unreachable by the reachability pass in
// internal/links/reachability.go. The pass writes a sidecar
// <group>-reachability.json beside the cross-repo links file; this
// handler reads that sidecar and projects it into the MCP wire shape.
//
// Optional filters:
//   - kind_filter: bare entity kind ("function"/"class"/"endpoint"/…).
//   - repo_filter: restrict to a subset of repo slugs.
//   - limit: cap the returned list (default 200).
//   - from: compute reachability from a SINGLE entry-point id instead
//     of the full union — re-runs BFS in-handler against the in-memory
//     graph so callers do not need to re-index.
//
// When the sidecar is missing (group never had link passes run, or
// grafel version is older than #2766), the handler falls back to a
// live-recompute from the in-memory graph using the same edge-kind set
// the pass uses on disk — so the answer is always correct, just
// slower.
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// reachabilitySidecarEntry mirrors the on-disk shape written by
// internal/links/reachability.go. Kept in this package (rather than
// imported) to avoid an mcp → links dependency, which would invert
// the existing layering.
type reachabilitySidecarEntry struct {
	Repo         string   `json:"repo"`
	EntityID     string   `json:"entity_id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	SourceFile   string   `json:"source_file,omitempty"`
	Reachable    bool     `json:"reachable"`
	ReachableVia []string `json:"reachable_via,omitempty"`
	EntrySource  string   `json:"entry_source,omitempty"`
}

type reachabilitySidecarDoc struct {
	Version       int                        `json:"version"`
	Group         string                     `json:"group"`
	WrittenAt     string                     `json:"written_at"`
	TotalEntities int                        `json:"total_entities"`
	Reachable     int                        `json:"reachable"`
	Unreachable   int                        `json:"unreachable"`
	EntryPoints   int                        `json:"entry_points"`
	Entries       []reachabilitySidecarEntry `json:"entries"`
}

// handleDeadCode is the MCP handler for grafel_dead_code.
func (s *Server) handleDeadCode(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	groupName, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	kindFilter := strings.ToLower(argString(req, "kind_filter", ""))
	repoFilter := map[string]bool{}
	for _, r := range argStringSlice(req, "repo_filter") {
		repoFilter[r] = true
	}
	limit := argInt(req, "limit", 200)
	from := strings.TrimSpace(argString(req, "from", ""))

	var dead []deadCodeItem
	var totalEntities, reachable, entryPoints int
	source := "sidecar"

	if from != "" {
		// Per-entry-point recompute against the in-memory graph.
		dead, totalEntities, reachable, entryPoints = computeDeadCodeFromEntry(lg, from, repoFilter, kindFilter)
		source = "from_entry"
	} else {
		// Prefer the on-disk sidecar (canonical, written by the link
		// pass). Fall back to live recompute when missing.
		path := reachabilitySidecarPath(groupName)
		doc, ok := loadReachabilitySidecar(path)
		if !ok {
			dead, totalEntities, reachable, entryPoints = computeDeadCodeLive(lg, repoFilter, kindFilter)
			source = "live_recompute"
		} else {
			totalEntities = doc.TotalEntities
			reachable = doc.Reachable
			entryPoints = doc.EntryPoints
			for _, e := range doc.Entries {
				if e.Reachable {
					continue
				}
				if len(repoFilter) > 0 && !repoFilter[e.Repo] {
					continue
				}
				if !matchesBareKind(e.Kind, kindFilter) {
					continue
				}
				dead = append(dead, deadCodeItem{
					EntityID:   prefixedID(e.Repo, e.EntityID),
					Name:       e.Name,
					Kind:       e.Kind,
					Repo:       e.Repo,
					SourceFile: e.SourceFile,
				})
			}
		}
	}

	sort.Slice(dead, func(i, j int) bool {
		if dead[i].Repo != dead[j].Repo {
			return dead[i].Repo < dead[j].Repo
		}
		return dead[i].Name < dead[j].Name
	})
	total := len(dead)
	if limit > 0 && len(dead) > limit {
		dead = dead[:limit]
	}

	return jsonResult(map[string]any{
		"dead_code":      dead,
		"count":          len(dead),
		"total":          total,
		"truncated":      total > len(dead),
		"total_entities": totalEntities,
		"reachable":      reachable,
		"unreachable":    total,
		"entry_points":   entryPoints,
		"source":         source,
		"note":           "Reachability-based dead code (#2766). Entities not transitively reached from any framework or sniffed entry-point. Dynamic dispatch / reflection / cross-repo callers from unindexed clients can produce false positives — verify before deletion.",
	}), nil
}

// reachabilitySidecarPath is the conventional path for the
// <group>-reachability.json sidecar written by
// internal/links/reachability.go.
func reachabilitySidecarPath(group string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grafel", "groups", group+"-links-reachability.json")
}

// loadReachabilitySidecar reads + parses the sidecar; ok=false on any
// failure (missing file is the common, non-error case).
func loadReachabilitySidecar(path string) (*reachabilitySidecarDoc, bool) {
	if path == "" {
		return nil, false
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var doc reachabilitySidecarDoc
	if err := json.Unmarshal(buf, &doc); err != nil {
		return nil, false
	}
	return &doc, true
}

// reachabilityEdgeKindsMCP mirrors the link-pass edge kind set. Kept
// in this file (not imported from links/) to avoid an mcp → links
// dependency.
var reachabilityEdgeKindsMCP = map[string]bool{
	"CALLS": true, "IMPORTS": true, "REFERENCES": true, "USES": true,
	"USES_HOOK": true, "HANDLES": true, "HANDLES_SIGNAL": true,
	"NAVIGATES_TO": true, "ROUTES_TO": true, "IMPLEMENTS": true,
	"EXTENDS": true, "RENDERS": true, "FETCHES": true, "TESTS": true,
	"REGISTERS": true, "RESOLVES_TO": true, "STEP_IN_PROCESS": true,
	"PRODUCES": true, "CONSUMES": true, "CONTAINS": true,
	"DEPENDS_ON": true, "ENTRY_POINT_OF": true,
	"DISCRIMINATES_ON": true, "UNRESOLVED_FETCH": true,
}

// frameworkEntryKindsMCP mirrors the link-pass framework seeds.
var frameworkEntryKindsMCP = map[string]bool{
	"http_endpoint_definition": true,
	"http_endpoint":            true,
	"SCOPE.Endpoint":           true,
	"SCOPE.Route":              true,
	"SCOPE.MessageTopic":       true,
	// #5782 (ADR-0025): a ChannelBinding is a config-side messaging
	// declaration with no callers by design — never report it as dead code.
	"SCOPE.ChannelBinding":     true,
	"SCOPE.GrpcMethod":         true,
	"SCOPE.ServerlessFunction": true,
	"SCOPE.EventBusEvent":      true,
}

// deadCodeItem is the per-entry wire shape returned by handleDeadCode.
type deadCodeItem struct {
	EntityID     string   `json:"entity_id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Repo         string   `json:"repo"`
	SourceFile   string   `json:"source_file,omitempty"`
	EntrySource  string   `json:"entry_source,omitempty"`
	ReachableVia []string `json:"reachable_via,omitempty"`
}

// computeDeadCodeLive runs the same BFS the link pass does, against
// the in-memory graph. Used when the sidecar is missing.
func computeDeadCodeLive(lg *LoadedGroup, repoFilter map[string]bool, kindFilter string) (
	out []deadCodeItem,
	totalEntities, reachable, entryPoints int,
) {
	if lg == nil {
		return
	}
	for _, r := range lg.Repos {
		if r == nil || r.Doc == nil {
			continue
		}
		adj := map[string][]string{}
		r.forEachRelationship(func(rel *graph.Relationship) bool {
			if !reachabilityEdgeKindsMCP[rel.Kind] {
				return true
			}
			adj[rel.FromID] = append(adj[rel.FromID], rel.ToID)
			return true
		})
		seeds := map[string]bool{}
		r.forEachEntity(func(e *graph.Entity) bool {
			if frameworkEntryKindsMCP[e.Kind] || isFrameworkOrHandler(e) {
				seeds[e.ID] = true
			}
			return true
		})
		r.forEachRelationship(func(rel *graph.Relationship) bool {
			switch rel.Kind {
			case "HANDLES", "HANDLES_SIGNAL", "NAVIGATES_TO", "ROUTES_TO", "REGISTERS":
				seeds[rel.ToID] = true
			}
			return true
		})
		reached := bfsLive(adj, seeds)
		totalEntities += len(r.Doc.Entities)
		reachable += len(reached)
		entryPoints += len(seeds)
		if len(repoFilter) > 0 && !repoFilter[r.Repo] {
			continue
		}
		r.forEachEntity(func(e *graph.Entity) bool {
			if reached[e.ID] {
				return true
			}
			if isStdlibEntity(e) {
				return true
			}
			if !isLiveCodeKind(e.Kind) {
				return true
			}
			if !matchesBareKind(e.Kind, kindFilter) {
				return true
			}
			out = append(out, deadCodeItem{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				Kind:       stripScopePrefix(e.Kind),
				Repo:       r.Repo,
				SourceFile: e.SourceFile,
			})
			return true
		})
	}
	return
}

// computeDeadCodeFromEntry restricts the BFS seed set to a single
// entity ID. Used by the optional `from` parameter so callers can ask
// "what is reachable starting at endpoint X, and what is dead given
// only X?".
func computeDeadCodeFromEntry(lg *LoadedGroup, fromID string, repoFilter map[string]bool, kindFilter string) (
	out []deadCodeItem,
	totalEntities, reachable, entryPoints int,
) {
	if lg == nil {
		return
	}
	_, local := splitPrefixed(fromID)
	if local == "" {
		local = fromID
	}
	for _, r := range lg.Repos {
		if r == nil || r.Doc == nil {
			continue
		}
		if _, ok := r.getByIDOne(local); !ok {
			// Entry not in this repo; skip without contributing.
			totalEntities += len(r.Doc.Entities)
			continue
		}
		adj := map[string][]string{}
		r.forEachRelationship(func(rel *graph.Relationship) bool {
			if !reachabilityEdgeKindsMCP[rel.Kind] {
				return true
			}
			adj[rel.FromID] = append(adj[rel.FromID], rel.ToID)
			return true
		})
		seeds := map[string]bool{local: true}
		reached := bfsLive(adj, seeds)
		totalEntities += len(r.Doc.Entities)
		reachable += len(reached)
		entryPoints += len(seeds)
		if len(repoFilter) > 0 && !repoFilter[r.Repo] {
			continue
		}
		r.forEachEntity(func(e *graph.Entity) bool {
			if reached[e.ID] {
				return true
			}
			if isStdlibEntity(e) {
				return true
			}
			if !isLiveCodeKind(e.Kind) {
				return true
			}
			if !matchesBareKind(e.Kind, kindFilter) {
				return true
			}
			out = append(out, deadCodeItem{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				Kind:       stripScopePrefix(e.Kind),
				Repo:       r.Repo,
				SourceFile: e.SourceFile,
			})
			return true
		})
	}
	return
}

// bfsLive runs a BFS over adj starting from seeds. Returns the
// reached-set including the seeds.
func bfsLive(adj map[string][]string, seeds map[string]bool) map[string]bool {
	reached := map[string]bool{}
	queue := make([]string, 0, len(seeds))
	for id := range seeds {
		reached[id] = true
		queue = append(queue, id)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nxt := range adj[cur] {
			if reached[nxt] {
				continue
			}
			reached[nxt] = true
			queue = append(queue, nxt)
		}
	}
	return reached
}

// isLiveCodeKind reports whether kind names a code-bearing entity
// (mirrors isCodeBearing in internal/links/reachability.go).
func isLiveCodeKind(kind string) bool {
	low := strings.ToLower(kind)
	low = strings.TrimPrefix(low, "scope.")
	switch low {
	case "file", "module", "package", "namespace", "directory", "folder",
		"document", "heading", "scopeunknown", "external", "project",
		"infraresource", "codeblock", "pattern", "evolution",
		"migration", "stylesheet", "schema", "dataaccess", "config",
		"constraint", "scheduledjob", "test", "queue", "event",
		"datastore", "messagetopic", "externalapi":
		return false
	}
	return true
}

// isTestFileMCP reports whether a source-file path matches a recognised
// test-file convention. It is a local copy of internal/graph/coverage.go's
// (unexported) isTestFile, kept in this package to avoid widening the graph
// package's exported surface and to keep the mcp → graph layering one-way and
// data-only — the same convention the rest of this file follows for the
// link-pass edge-kind / framework-seed sets.
//
// Recognised conventions (mirror coverage.go exactly):
//   - any /test/, /tests/, /__tests__/, /spec/ path segment
//   - Go:        *_test.go
//   - Python:    test_*.py, *_test.py            (plus conftest.py below)
//   - JS/TS:     *.test.{js,ts,jsx,tsx}, *.spec.*
//   - Ruby:      *_spec.rb
//   - Java/Kt/C#: *Test, *Tests, *Spec stems
//
// conftest.py is added on top of coverage.go's set because pytest fixtures
// defined there are test-only callers for dead-code purposes.
func isTestFileMCP(path string) bool {
	if path == "" {
		return false
	}
	lowerPath := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	slashed := "/" + lowerPath
	for _, seg := range []string{"/test/", "/tests/", "/__tests__/", "/spec/"} {
		if strings.Contains(slashed, seg) {
			return true
		}
	}

	base := lowerPath
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	if base == "conftest.py" {
		return true
	}
	ext := ""
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		ext = base[i:]
	}
	stem := strings.TrimSuffix(base, ext)

	switch ext {
	case ".go":
		return strings.HasSuffix(stem, "_test")
	case ".py":
		return strings.HasPrefix(stem, "test_") || strings.HasSuffix(stem, "_test")
	case ".ts", ".tsx", ".js", ".jsx":
		return strings.HasSuffix(stem, ".test") ||
			strings.HasSuffix(stem, ".spec") ||
			strings.Contains(base, ".test.") ||
			strings.Contains(base, ".spec.")
	case ".rb":
		return strings.HasSuffix(stem, "_spec")
	case ".java", ".kt", ".cs":
		return strings.HasSuffix(stem, "test") ||
			strings.HasSuffix(stem, "tests") ||
			strings.HasSuffix(stem, "spec")
	}
	return false
}

// matchesBareKind reports whether entity kind matches a user-supplied
// bare-kind filter ("function", "class", …). Empty filter passes.
func matchesBareKind(kind, filter string) bool {
	if filter == "" {
		return true
	}
	low := strings.ToLower(stripScopePrefix(kind))
	return low == filter
}

// Compile-time guard: handleDeadCode is used by server.go.
var _ = (*Server)(nil).handleDeadCode

// graph import retained for matchesKindFilter type alignment with the
// existing dead-code handler; the live-recompute path uses
// *graph.Entity via isStdlibEntity/isFrameworkOrHandler.
var _ = (*graph.Entity)(nil)
