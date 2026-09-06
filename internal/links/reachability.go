// Phase 1B reachability + dead-code identification (#2766).
//
// Replaces the heuristic in grafel_find_dead_code with a rigorous
// reachability pass: BFS from the per-repo entry-point set across
// reachability-bearing edges (CALLS, IMPORTS, REFERENCES, USES,
// HANDLES, HANDLES_SIGNAL, NAVIGATES_TO, ROUTES_TO, IMPLEMENTS,
// RENDERS, FETCHES, TESTS, REGISTERS, RESOLVES_TO). Every reached
// entity is marked `reachable: true` with a comma-separated
// `reachable_via` provenance list of the entry-point IDs that lit it
// up; everything left over is marked `reachable: false` and is a
// dead-code candidate.
//
// #6839 — the one exception to "everything left over": if an
// entry-point-bearing source file in a repo could not be READ (a
// non-regular file, a would-block open, an I/O error — anything but
// fs.ErrNotExist, which is an absent file rather than a failed read),
// the seed set for that repo is incomplete, so nothing left over can be
// called dead. That repo's unreached entities are left UNSTAMPED and
// emit no sidecar row; the skipped files are counted in
// PassResult.UnreadableSourceFiles and named in the sidecar's
// degraded_repos. reachable="true" is still stamped, since a lost seed
// can only add reachability.
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
// grafel_dead_code. The per-repo graph.fb files are NOT
// rewritten — this mirrors the Phase 0 RESOLVES_TO sidecar model.
package links

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/substrate"

	"github.com/cajasmota/grafel/internal/types"
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
//
// #6902: BOTH Route spellings are seeds. "SCOPE.Route" comes from the Lua
// routing extractors, Vaadin @Route pages and the engine's gateway /
// frontend-route synthesisers; bare "Route" comes from
// internal/custom/java/{play,spring_webflux,akka_http,javalin,vertx,struts}_routes.go
// and internal/engine/{spring,django}_routes.go. They name the same concept —
// an HTTP route — and #6776 arm B7 added both kinds together because both are
// live. Seeding only the prefixed one persisted every Java/Spring/Django
// route into the sidecar as unreachable, along with everything only it reaches.
// internal/mcp/dead_code.go's frameworkEntryKindsMCP mirrors this map.
var frameworkEntryKinds = map[string]bool{
	"http_endpoint_definition": true,
	"http_endpoint":            true,
	"SCOPE.Endpoint":           true,
	"SCOPE.Route":              true,
	"Route":                    true, // #6902
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
	Version       int    `json:"version"`
	Group         string `json:"group"`
	WrittenAt     string `json:"written_at"`
	TotalEntities int    `json:"total_entities"`
	Reachable     int    `json:"reachable"`
	Unreachable   int    `json:"unreachable"`
	EntryPoints   int    `json:"entry_points"`

	// Unknown counts entities whose reachability the pass could NOT compute
	// because an entry-point-bearing source file in their repo was
	// unreadable (#6839). They are neither reachable nor unreachable: they
	// carry no `reachable` stamp and appear in no entry below.
	Unknown int `json:"unknown,omitempty"`

	// DegradedRepos names the repos that produced an Unknown set, and
	// UnreadableEntryPointFiles counts the files that could not be read.
	// grafel_dead_code decodes both and returns them alongside its list, so a
	// caller sees that a degraded repo's answer is partial rather than
	// complete (#6839). No other consumer reads this document.
	DegradedRepos             []string `json:"degraded_repos,omitempty"`
	UnreadableEntryPointFiles int      `json:"unreadable_entry_point_files,omitempty"`

	Entries []reachabilityEntry `json:"entries"`
}

// readSourceFileForReachability is this pass's source read, indirected
// through a package-level var for one reason: #6839's predicate turns on
// the ERROR CLASS a read fails with, and two of the classes it must treat
// as failures cannot be produced on a real filesystem without planting a
// FIFO or a socket. safeio.ErrWouldBlock in particular is reachable only
// from safeio's process-global slot saturation (64 abandoned opens), and
// fs.ErrPermission is not reachable at all when the tests run as root.
// Without a seam those branches would be asserted only by prose.
//
// Production never reassigns it. The tests that do restore it via
// t.Cleanup, and they still drive the whole pass and assert the stamped
// property — the seam replaces the filesystem, not the code under test.
var readSourceFileForReachability = readSourceFile

// runReachabilityPass computes reachability + dead-code marking across
// all repos in the group. Returns a PassResult so RunAllPasses can fold
// it into the link-pass-stats telemetry.
func runReachabilityPass(group string, graphs []repoGraph, paths Paths) (PassResult, error) {
	res := PassResult{Pass: "reachability"}

	totalEntities := 0
	totalReachable := 0
	totalUnknown := 0
	totalEntries := 0
	unreadableFiles := 0
	degradedRepos := []string{}
	allEntries := []reachabilityEntry{}

	for ri := range graphs {
		g := &graphs[ri]

		// #6839: source files whose entry-points this repo never got to see.
		// A read failure here is not "that file declares no entry-point" —
		// it is "we do not know what that file declares", and the seeds it
		// would have contributed can light up ANY entity in this repo
		// transitively. See the degraded-stamp block below.
		repoUnreadable := []string{}

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
		// #6839: resolve the source root ONCE and check it is a directory we
		// can see. Per-file fs.ErrNotExist below is read as "that file
		// genuinely is not there"; at the ROOT level the same inference is
		// wrong — a root that moved makes every file ErrNotExist while the
		// code all still exists, which is exactly #6839's harm. So a missing
		// root degrades the repo instead of being exempted file by file.
		srcRoot := repoSourcePathFor(g.Repo)
		if srcRoot == "" {
			srcRoot = g.FileRoot
		}
		srcRootOK := false
		if srcRoot != "" {
			if fi, err := os.Stat(srcRoot); err == nil && fi.IsDir() {
				srcRootOK = true
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
			if !srcRootOK {
				// The whole sniffed entry-point class is unavailable for this
				// repo. Record it once and stop — reading further files can
				// only produce the same answer.
				repoUnreadable = append(repoUnreadable, "<source root: "+srcRoot+">")
				fmt.Fprintf(os.Stderr,
					"grafel: warning: reachability: %s: source root %q is missing or not a "+
						"directory; entry-points cannot be sniffed and dead-code marking is "+
						"suppressed for this repo\n", g.Repo, srcRoot)
				break
			}
			abs := filepath.Join(srcRoot, file)
			content, err := readSourceFileForReachability(abs, maxSourceFileBytes)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					// The source root exists (checked above) but this file
					// under it does not. That is a KNOWN fact rather than a
					// failed read — nothing about the file was hidden from
					// the pass — so the pre-#6839 skip is right and dead-code
					// marking stays enabled. The dangerous shape of the same
					// error, a root that moved so EVERY file reports
					// ErrNotExist while the code exists, is caught by the
					// srcRootOK guard above and degrades the repo.
					continue
				}
				// #6839: skip the file (safeio's doc forbids the SILENCE, not
				// the non-abort), but record it — the skip has to be bounded
				// and REPORTED, and this repo's dead-code verdict is now
				// unsubstantiated.
				repoUnreadable = append(repoUnreadable, file)
				fmt.Fprintf(os.Stderr,
					"grafel: warning: reachability: %s: entry-point file %s unreadable (%v); "+
						"dead-code marking suppressed for this repo\n", g.Repo, file, err)
				continue
			}
			eps := sniff(string(content))
			if len(eps) == 0 {
				continue
			}
			fileNames := nameByFile[file]
			// #4466: library_export entries are only genuine entry-point
			// ROOTS when they form the package's public API surface — a
			// barrel / index / explicit package entry file. In an
			// application repo virtually every internal module re-exports
			// its symbols (services, controllers, DTOs, every type), so
			// honouring every export as a seed made ~65% of entities
			// "entry points" and falsely marked unconsumed exports
			// reachable. Internal-module exports that are actually used
			// are still reached transitively via the IMPORTS edge, so
			// dropping them as SEEDS does not lose live code — it only
			// stops masking genuinely dead exports.
			publicAPI := isPublicAPIFile(file)
			for _, ep := range eps {
				// Library exports from non-public-API files are not roots.
				if ep.Kind == EntryKindLibraryExport && !publicAPI {
					continue
				}
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
		//
		// #6839: when any entry-point file in this repo could not be read,
		// the seed set is incomplete, so "not reached by the BFS" no longer
		// means "not reachable". reachable="false" is the one stamp in this
		// package whose silence becomes a POSITIVE FALSE CLAIM — the
		// grafel_dead_code MCP tool reads it straight out of the sidecar and
		// reports live code as dead. So for a degraded repo the pass declines
		// to stamp and emits no unreachable row: an absent answer, not a
		// wrong one. reachable="true" is still stamped, because a lost seed
		// can only ADD reachability, never remove it.
		//
		// The scope is this repo and no wider: the BFS adjacency is built
		// from g.Edges, so a lost seed cannot reach into a sibling repo, and
		// suppressing group-wide would hide real dead code everywhere else.
		degraded := len(repoUnreadable) > 0
		repoReachable := 0
		for ei := range g.Entities {
			e := &g.Entities[ei]
			isReach := reachable[e.ID] != nil
			if e.Properties == nil {
				e.Properties = types.Props{}
			}
			if !isReach && degraded {
				// Undetermined: leave no stamp at all, and clear any stale
				// one carried in from an earlier run so no consumer reads a
				// claim this run cannot substantiate.
				e.Properties.Delete("reachable")
				e.Properties.Delete("reachable_via")
				totalUnknown++
				continue
			}
			if isReach {
				e.Properties.Set("reachable", "true")
				vias := keysOf(reachable[e.ID])
				sort.Strings(vias)
				if len(vias) > 8 {
					vias = vias[:8]
				}
				e.Properties.Set("reachable_via", strings.Join(vias, ","))
				repoReachable++
			} else {
				e.Properties.Set("reachable", "false")
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
		unreadableFiles += len(repoUnreadable)
		if degraded {
			degradedRepos = append(degradedRepos, g.Repo)
		}
	}

	res.LinksAdded = totalReachable
	res.Candidates = totalEntities - totalReachable - totalUnknown
	res.Skipped = totalEntries
	res.UnreadableSourceFiles = unreadableFiles

	if paths.Links != "" {
		sidecar := strings.TrimSuffix(paths.Links, ".json") + "-reachability.json"
		doc := reachabilityDocument{
			Version:       1,
			Group:         group,
			WrittenAt:     discoveredAt(),
			TotalEntities: totalEntities,
			Reachable:     totalReachable,
			Unreachable:   totalEntities - totalReachable - totalUnknown,
			EntryPoints:   totalEntries,

			Unknown:                   totalUnknown,
			DegradedRepos:             degradedRepos,
			UnreadableEntryPointFiles: unreadableFiles,

			Entries: allEntries,
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

// isPublicAPIFile reports whether a repo-relative source path is part of
// the package's public API surface — the set of files whose exports are
// genuine externally-invocable entry-point roots (#4466).
//
// Recognised as public API:
//   - barrel / package-entry files: index.{ts,tsx,js,jsx,mjs,cjs}
//   - explicit public-api / public_api files (Angular library convention)
//   - mod.ts (Deno) and lib.rs / mod.rs entry roots
//
// Everything else is an internal module: its exports are wiring consumed
// via IMPORTS edges, not external entry points. Internal exports that are
// actually used stay reachable transitively; unused ones correctly fall
// out as dead-code candidates rather than being masked as "entry points".
func isPublicAPIFile(file string) bool {
	base := strings.ToLower(file)
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	switch base {
	case "index.ts", "index.tsx", "index.js", "index.jsx",
		"index.mjs", "index.cjs",
		"public-api.ts", "public_api.ts", "public-api.js", "public_api.js",
		"mod.ts", "lib.rs", "mod.rs":
		return true
	}
	return false
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
