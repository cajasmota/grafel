// Phase 1B reachability + dead-code identification (#2766).
//
// Replaces the heuristic in archigraph_find_dead_code with a rigorous
// reachability pass: BFS from the per-repo entry-point set across
// reachability-bearing edges (CALLS, IMPORTS, REFERENCES, USES,
// HANDLES, HANDLES_SIGNAL, NAVIGATES_TO, ROUTES_TO, IMPLEMENTS,
// RENDERS, FETCHES, TESTS, REGISTERS, RESOLVES_TO). Every reached
// entity is marked `reachable: true` with a comma-separated
// `reachable_via` provenance list of the entry-point IDs that lit it
// up; everything left over is marked `reachable: false` and is a
// dead-code candidate.
//
// Entry-point classes (per #2766):
//
//  1. Graph-encoded entry-points — http_endpoint_definition entities,
//     entities with inbound HANDLES_SIGNAL / NAVIGATES_TO / ROUTES_TO
//     edges, framework-lifecycle handlers (init / setup / etc.).
//     These already encode their reachability via the existing edge
//     graph, so the pass simply seeds the BFS with them.
//
//  2. Per-language source-sniffed entry-points — CLI mains, library
//     re-exports, test entries. The internal/substrate/entry_points*
//     sniffers lift them from raw source content; the pass matches
//     each entry by (file, ident) against entity Names in the graph
//     and seeds the BFS with the matches.
//
// Storage model: in-memory mutation of entityNode.Properties (so
// downstream passes see the marking), plus a persistent
// <group>-reachability.json sidecar that MCP reads via
// archigraph_dead_code. The per-repo graph.fb files are NOT
// rewritten — this mirrors the Phase 0 RESOLVES_TO sidecar model.
package links

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/substrate"
)

// MethodReachability identifies entries produced by the Phase 1B
// reachability pass. Method-segregated so re-runs rewrite only this
// pass's output.
const MethodReachability = "reachability"

// reachabilityEdgeKinds is the canonical set of edge kinds that
// propagate reachability. A target entity is considered reachable if
// any source entity that is reachable has an outbound edge of one of
// these kinds to it.
//
// CONTAINS is included so that reaching a class also reaches its
// methods (Java/C# style class-method hierarchy). Without it, a
// reachable class with no per-method CALLS edges would leave every
// method falsely marked dead.
var reachabilityEdgeKinds = map[string]bool{
	"CALLS":            true,
	"IMPORTS":          true,
	"REFERENCES":       true,
	"USES":             true,
	"USES_HOOK":        true,
	"HANDLES":          true,
	"HANDLES_SIGNAL":   true,
	"NAVIGATES_TO":     true,
	"ROUTES_TO":        true,
	"IMPLEMENTS":       true,
	"EXTENDS":          true,
	"RENDERS":          true,
	"FETCHES":          true,
	"TESTS":            true,
	"REGISTERS":        true,
	"RESOLVES_TO":      true,
	"STEP_IN_PROCESS":  true,
	"PRODUCES":         true,
	"CONSUMES":         true,
	"CONTAINS":         true,
	"DEPENDS_ON":       true,
	"ENTRY_POINT_OF":   true,
	"DISCRIMINATES_ON": true,
	"UNRESOLVED_FETCH": true,
}

// frameworkEntryKinds are entity kinds whose presence implies a
// framework-managed entry-point. Used to seed the BFS without needing
// inbound edges.
var frameworkEntryKinds = map[string]bool{
	"http_endpoint_definition": true,
	"http_endpoint":            true,
	"SCOPE.Endpoint":           true,
	"SCOPE.Route":              true,
	"SCOPE.MessageTopic":       true,
	"SCOPE.GrpcMethod":         true,
	"SCOPE.ServerlessFunction": true,
	"SCOPE.EventBusEvent":      true,
}

// reachabilityEntry is one persistent reachability fact for the sidecar.
type reachabilityEntry struct {
	Repo         string   `json:"repo"`
	EntityID     string   `json:"entity_id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	SourceFile   string   `json:"source_file,omitempty"`
	Reachable    bool     `json:"reachable"`
	ReachableVia []string `json:"reachable_via,omitempty"`
	EntrySource  string   `json:"entry_source,omitempty"`
}

// reachabilityDocument is the on-disk shape of
// <group>-reachability.json.
type reachabilityDocument struct {
	Version       int                 `json:"version"`
	Group         string              `json:"group"`
	WrittenAt     string              `json:"written_at"`
	TotalEntities int                 `json:"total_entities"`
	Reachable     int                 `json:"reachable"`
	Unreachable   int                 `json:"unreachable"`
	EntryPoints   int                 `json:"entry_points"`
	Entries       []reachabilityEntry `json:"entries"`
}

// runReachabilityPass computes reachability + dead-code marking across
// all repos in the group. Returns a PassResult so RunAllPasses can fold
// it into the link-pass-stats telemetry.
func runReachabilityPass(group string, graphs []repoGraph, paths Paths) (PassResult, error) {
	res := PassResult{Pass: "reachability"}

	totalEntities := 0
	totalReachable := 0
	totalEntries := 0
	allEntries := []reachabilityEntry{}

	for ri := range graphs {
		g := &graphs[ri]

		// Build outbound adjacency on reachability-bearing edges.
		adj := map[string][]string{}
		for _, e := range g.Edges {
			if !reachabilityEdgeKinds[e.Kind] {
				continue
			}
			adj[e.FromID] = append(adj[e.FromID], e.ToID)
		}

		// Index entities by ID (for the BFS) and build a (file, name)
		// lookup for matching sniffed entry-points.
		byID := make(map[string]*entityNode, len(g.Entities))
		// nameByFile[file][name] -> entity IDs (may be > 1 for
		// overloads; we seed all of them).
		nameByFile := map[string]map[string][]string{}
		for ei := range g.Entities {
			e := &g.Entities[ei]
			byID[e.ID] = e
			if e.SourceFile == "" || e.Name == "" {
				continue
			}
			leaf := e.Name
			if i := strings.LastIndexByte(leaf, '.'); i >= 0 {
				leaf = leaf[i+1:]
			}
			fm, ok := nameByFile[e.SourceFile]
			if !ok {
				fm = map[string][]string{}
				nameByFile[e.SourceFile] = fm
			}
			fm[e.Name] = append(fm[e.Name], e.ID)
			if leaf != e.Name {
				fm[leaf] = append(fm[leaf], e.ID)
			}
		}

		// Seed set: graph-encoded entry-points.
		seeds := map[string]string{}
		for ei := range g.Entities {
			e := &g.Entities[ei]
			if frameworkEntryKinds[e.Kind] {
				seeds[e.ID] = "graph:" + e.Kind
				continue
			}
			// Inbound edges from outside the graph: any entity that is
			// the target of HANDLES/HANDLES_SIGNAL/NAVIGATES_TO/
			// ROUTES_TO is invocable by the framework. We pick those
			// up below from the adjacency built in reverse.
		}
		// Pre-compute targets of framework-invocation edges as seeds.
		for _, e := range g.Edges {
			switch e.Kind {
			case "HANDLES", "HANDLES_SIGNAL", "NAVIGATES_TO", "ROUTES_TO",
				"REGISTERS", "ENTRY_POINT_OF":
				// For ENTRY_POINT_OF the "endpoint" side is the From
				// (e.g. <handler> ENTRY_POINT_OF <endpoint>), so both
				// ends are reachable entry-points.
				if _, ok := byID[e.ToID]; ok {
					if _, seeded := seeds[e.ToID]; !seeded {
						seeds[e.ToID] = "graph_edge:" + e.Kind
					}
				}
				if _, ok := byID[e.FromID]; ok {
					if _, seeded := seeds[e.FromID]; !seeded {
						seeds[e.FromID] = "graph_edge:" + e.Kind
					}
				}
			}
		}

		// Source-sniffed entry-points. Sniff each supported-language
		// file once.
		fileSet := map[string]bool{}
		for ei := range g.Entities {
			if f := g.Entities[ei].SourceFile; f != "" {
				fileSet[f] = true
			}
		}
		for file := range fileSet {
			lang := substrate.LanguageForPath(file)
			if lang == "" {
				continue
			}
			sniff := substrate.EntryPointSnifferFor(lang)
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
			eps := sniff(string(content))
			if len(eps) == 0 {
				continue
			}
			fileNames := nameByFile[file]
			for _, ep := range eps {
				// Match the sniffed ident against entities declared
				// in the same file. Three lookup keys:
				//   1. the ident as-is (covers function/class names)
				//   2. a qualified-name form: <fileBase>.<ident>
				//   3. wildcard "*" — for runner-style entries
				//      ("it"/"test"/"describe") we seed every operation
				//      defined in the same file.
				ids := fileNames[ep.Ident]
				if len(ids) == 0 && ep.Kind == EntryKindTestEntry &&
					(ep.Ident == "it" || ep.Ident == "test" || ep.Ident == "describe") {
					// All ops in this file get seeded.
					for nm, sl := range fileNames {
						_ = nm
						ids = append(ids, sl...)
					}
				}
				for _, id := range ids {
					if _, seeded := seeds[id]; seeded {
						continue
					}
					seeds[id] = "sniff:" + string(ep.Kind) + ":" + ep.Ident
				}
			}
		}

		// BFS.
		reachable := map[string]map[string]bool{}
		queue := make([]string, 0, len(seeds))
		for id, src := range seeds {
			reachable[id] = map[string]bool{src: true}
			queue = append(queue, id)
		}
		// Stable order so output is byte-identical across runs.
		sort.Strings(queue)
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			seedSrc := firstKey(reachable[cur])
			for _, nxt := range adj[cur] {
				m, ok := reachable[nxt]
				if !ok {
					m = map[string]bool{}
					reachable[nxt] = m
					queue = append(queue, nxt)
				}
				m[seedSrc] = true
			}
		}

		// Stamp + emit entries.
		repoReachable := 0
		for ei := range g.Entities {
			e := &g.Entities[ei]
			isReach := reachable[e.ID] != nil
			if e.Properties == nil {
				e.Properties = map[string]string{}
			}
			if isReach {
				e.Properties["reachable"] = "true"
				vias := keysOf(reachable[e.ID])
				sort.Strings(vias)
				if len(vias) > 8 {
					vias = vias[:8]
				}
				e.Properties["reachable_via"] = strings.Join(vias, ",")
				repoReachable++
			} else {
				e.Properties["reachable"] = "false"
			}
			// Only emit entries for code-bearing entities — keeps the
			// sidecar focused. Skip Module/File/Document/External
			// noise.
			if !isCodeBearing(e.Kind) {
				continue
			}
			entry := reachabilityEntry{
				Repo:       g.Repo,
				EntityID:   e.ID,
				Name:       e.Name,
				Kind:       e.Kind,
				SourceFile: e.SourceFile,
				Reachable:  isReach,
			}
			if isReach {
				vias := keysOf(reachable[e.ID])
				sort.Strings(vias)
				if len(vias) > 4 {
					vias = vias[:4]
				}
				entry.ReachableVia = vias
				if _, isSeed := seeds[e.ID]; isSeed {
					entry.EntrySource = seeds[e.ID]
				}
			}
			allEntries = append(allEntries, entry)
		}

		totalEntities += len(g.Entities)
		totalReachable += repoReachable
		totalEntries += len(seeds)
	}

	res.LinksAdded = totalReachable
	res.Candidates = totalEntities - totalReachable
	res.Skipped = totalEntries

	if paths.Links != "" {
		sidecar := strings.TrimSuffix(paths.Links, ".json") + "-reachability.json"
		doc := reachabilityDocument{
			Version:       1,
			Group:         group,
			WrittenAt:     discoveredAt(),
			TotalEntities: totalEntities,
			Reachable:     totalReachable,
			Unreachable:   totalEntities - totalReachable,
			EntryPoints:   totalEntries,
			Entries:       allEntries,
		}
		sort.Slice(doc.Entries, func(i, j int) bool {
			if doc.Entries[i].Repo != doc.Entries[j].Repo {
				return doc.Entries[i].Repo < doc.Entries[j].Repo
			}
			return doc.Entries[i].EntityID < doc.Entries[j].EntityID
		})
		if err := writeReachabilityDoc(sidecar, doc); err != nil {
			return res, fmt.Errorf("write reachability doc: %w", err)
		}
	}

	return res, nil
}

// firstKey returns one key from m in arbitrary order; "" if empty.
// Used to label BFS traversal entries with the seed identifier their
// first hop came from.
func firstKey(m map[string]bool) string {
	for k := range m {
		return k
	}
	return ""
}

// keysOf returns the sorted keys of m.
func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// isCodeBearing reports whether kind names a code-bearing entity that
// dead-code analysis is meaningful for. Skips container/file/document
// nodes that are reachable trivially and add only noise to the sidecar.
// Also skips entity kinds that are framework-managed by definition
// (migrations, schemas, SQL DataAccess artefacts, infra resources):
// flagging them as dead code is always a false positive because the
// runtime/framework invokes them out-of-band.
func isCodeBearing(kind string) bool {
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

// EntryKind aliases — re-export the substrate constants so callers in
// this package can refer to them without importing the substrate
// package directly. Keeps the substrate package free of cross-package
// import cycles.
const (
	EntryKindCLIMain            = substrate.EntryKindCLIMain
	EntryKindLibraryExport      = substrate.EntryKindLibraryExport
	EntryKindTestEntry          = substrate.EntryKindTestEntry
	EntryKindFrameworkLifecycle = substrate.EntryKindFrameworkLifecycle
)

func writeReachabilityDoc(path string, doc reachabilityDocument) error {
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}
