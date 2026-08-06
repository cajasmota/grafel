package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// RegistryStore is the small surface the HTTP handlers need. Splitting it
// out lets tests inject an in-memory implementation without touching
// ~/.grafel on disk.
type RegistryStore interface {
	ListGroups() ([]GroupSummary, error)
	GroupGraph(group string) ([]byte, error)
	RepoGraph(group, repo string) ([]byte, error)
	CreateGroup(name string) (GroupSummary, error)
	AddRepo(group string, repo registry.Repo) error
}

// GroupSummary is the registry list shape returned by GET /api/registry.
// entity_count, last_indexed are aggregated from per-repo graph-stats.json
// sidecars (written by `grafel index`). The aggregation is cached in
// registryStatsCache and refreshed at most once every 30 s.
type GroupSummary struct {
	Name        string   `json:"name"`
	ConfigPath  string   `json:"config_path"`
	Repos       []string `json:"repos"`
	EntityCount int      `json:"entity_count"`
	LastIndexed string   `json:"last_indexed,omitempty"` // RFC3339, most-recent across repos
	Frameworks  []string `json:"frameworks,omitempty"`   // top-8 frameworks by frequency, desc

	// Monorepos maps parent-repo slug → list of registered module sub-paths.
	// Only populated for repos that have at least one Module declared (#2180).
	// Key is the repo slug; value is the Module slice from the fleet config.
	Monorepos map[string][]string `json:"monorepos,omitempty"`

	// RepoPaths are the absolute on-disk paths of each repo in the group.
	// Not serialised to JSON; used internally to compute tier state (S1 #2151).
	RepoPaths []string `json:"-"`
}

// ---------------------------------------------------------------------------
// Per-group registry-stats cache
// ---------------------------------------------------------------------------

type registryStatsCacheEntry struct {
	entityCount int
	lastIndexed time.Time
	computedAt  time.Time
}

var (
	registryStatsMu    sync.Mutex
	registryStatsCache = map[string]registryStatsCacheEntry{}
	registryStatsTTL   = 30 * time.Second
)

// aggregateGroupStats reads graph-stats.json for each repo in the group and
// returns (entity_count_sum, most_recent_computed_at). Results are cached for
// registryStatsTTL to keep /api/registry latency well under 100 ms on warm
// paths.
func aggregateGroupStats(groupName string, repos []registry.Repo) (entityCount int, lastIndexed time.Time) {
	registryStatsMu.Lock()
	if e, ok := registryStatsCache[groupName]; ok && time.Since(e.computedAt) < registryStatsTTL {
		registryStatsMu.Unlock()
		return e.entityCount, e.lastIndexed
	}
	registryStatsMu.Unlock()

	// Compute fresh — no lock held during I/O.
	var totalEntities int
	var latest time.Time
	for _, r := range repos {
		stateDir := daemon.StateDirForRepo(r.Path)
		sidecarPath := filepath.Join(stateDir, "graph-stats.json")
		data, err := os.ReadFile(sidecarPath)
		if err != nil {
			// Sidecar not yet written (e.g. a graph produced by the daemon's
			// incremental reindex path, which writes graph.fb but no sidecar).
			// Read the real entity count + index timestamp cheaply from the
			// graph.fb header so a cold-but-indexed group reports its true size
			// instead of "0 entities / never indexed" (#5442).
			if ps, ok := graph.PersistedStatsFromDir(stateDir); ok {
				totalEntities += ps.Entities
				switch {
				case !ps.ComputedAt.IsZero():
					if ps.ComputedAt.After(latest) {
						latest = ps.ComputedAt
					}
				default:
					// No header timestamp — fall back to the active graph's
					// mtime. #5915 J2 slice-2: CurrentGraphMtime is segment-set
					// aware (manifest.json mtime), unlike
					// os.Stat(CurrentGraphPath(stateDir)) which only ever resolves
					// a flat .fb path.
					if mt, ok := graph.CurrentGraphMtime(stateDir); ok && mt.After(latest) {
						latest = mt
					}
				}
			} else if mt, ok := graph.CurrentGraphMtime(stateDir); ok {
				// graph unreadable but present — fall back to its mtime.
				if mt.After(latest) {
					latest = mt
				}
			}
			continue
		}
		var side graph.GraphStatsSidecar
		if json.Unmarshal(data, &side) != nil {
			continue
		}
		totalEntities += side.TotalEntities
		if side.ComputedAt.After(latest) {
			latest = side.ComputedAt
		}
	}

	registryStatsMu.Lock()
	registryStatsCache[groupName] = registryStatsCacheEntry{
		entityCount: totalEntities,
		lastIndexed: latest,
		computedAt:  time.Now(),
	}
	registryStatsMu.Unlock()

	return totalEntities, latest
}

// liveStore is the production RegistryStore: it reads from the on-disk
// registry under ~/.grafel and from each repo's .grafel/graph.json.
type liveStore struct{}

// NewLiveStore returns the production RegistryStore.
func NewLiveStore() RegistryStore { return liveStore{} }

func (liveStore) ListGroups() ([]GroupSummary, error) {
	groups, err := registry.Groups()
	if err != nil {
		return nil, err
	}
	out := make([]GroupSummary, 0, len(groups))
	for _, g := range groups {
		s := GroupSummary{Name: g.Name, ConfigPath: g.ConfigPath}
		var repos []registry.Repo
		if cfg, err := registry.LoadGroupConfig(g.ConfigPath); err == nil {
			repos = cfg.Repos
			for _, r := range cfg.Repos {
				s.Repos = append(s.Repos, r.Slug)
				// S1 (#2151): populate RepoPaths for tier-state reporting.
				s.RepoPaths = append(s.RepoPaths, r.Path)
				// M3 (#2180): populate Monorepos for repos with declared modules.
				if len(r.Modules) > 0 {
					if s.Monorepos == nil {
						s.Monorepos = make(map[string][]string)
					}
					s.Monorepos[r.Slug] = r.Modules
				}
			}
		}
		// Aggregate entity_count + last_indexed from per-repo graph-stats.json.
		entityCount, lastIndexed := aggregateGroupStats(g.Name, repos)
		s.EntityCount = entityCount
		if !lastIndexed.IsZero() {
			s.LastIndexed = lastIndexed.UTC().Format(time.RFC3339)
		}
		out = append(out, s)
	}
	return out, nil
}

func (liveStore) GroupGraph(group string) ([]byte, error) {
	cfg, err := groupConfig(group)
	if err != nil {
		return nil, err
	}
	// Compose a minimal envelope: one entry per repo with the embedded
	// graph JSON bytes. Communities, god-nodes and cross-repo links are
	// deferred per the issue body.
	type repoEntry struct {
		Slug  string          `json:"slug"`
		Path  string          `json:"path"`
		Graph json.RawMessage `json:"graph,omitempty"`
		Error string          `json:"error,omitempty"`
	}
	entries := make([]repoEntry, 0, len(cfg.Repos))
	for _, r := range cfg.Repos {
		e := repoEntry{Slug: r.Slug, Path: r.Path}
		b, err := repoGraphBytes(r.Path)
		if err != nil {
			e.Error = err.Error()
		} else {
			e.Graph = b
		}
		entries = append(entries, e)
	}
	return json.Marshal(map[string]any{
		"group":     group,
		"repos":     entries,
		"deferred":  []string{"communities", "god_nodes", "cross_repo_links"},
		"served_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (liveStore) RepoGraph(group, repo string) ([]byte, error) {
	cfg, err := groupConfig(group)
	if err != nil {
		return nil, err
	}
	for _, r := range cfg.Repos {
		if r.Slug == repo {
			return repoGraphBytes(r.Path)
		}
	}
	return nil, fmt.Errorf("repo %q not registered in group %q", repo, group)
}

// repoGraphBytes returns the graph as JSON bytes for a repo. ADR-0016
// flip-day (#808): tries graph.json first (fast raw-read), falls back
// to loading the active FB graph and re-marshaling to JSON.
//
// #6015: the graph.json read used to be UNCONDITIONAL, so a repo that had ever
// produced one — via `--export-json`, or by predating the ADR-0016 layout —
// served that snapshot from the dashboard's RepoGraph / GroupGraph endpoints
// forever, across every subsequent reindex, with no indication it was stale.
// The read is now gated on graphJSONUsable, which is the same stat-compare the
// sibling sidecar readers (graph/descriptions, graph/flows) already perform.
func repoGraphBytes(repoPath string) ([]byte, error) {
	stateDir := daemon.StateDirForRepo(repoPath)
	jsonPath := daemon.GraphPathForRepo(repoPath)
	if graphJSONUsable(stateDir, jsonPath) {
		if b, err := os.ReadFile(jsonPath); err == nil {
			return b, nil
		}
	}
	// No graph.json, or one superseded by a newer graph — load the active FB
	// graph and re-marshal. A load error is returned rather than silently
	// falling back to the stale JSON: a loud failure beats a snapshot the
	// caller would have no way to know is out of date.
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		return nil, graphUnreadableError(stateDir, err)
	}
	return json.Marshal(doc)
}

// graphUnreadableError turns a failed graph load into an error that says what
// to DO about it, rather than a bare loader message.
//
// This matters because of what #6015 changed. Before the staleness gate, a repo
// whose graph.fb had gone version-incompatible after an upgrade still rendered:
// it silently fell back to graph.json and showed an older-but-honest graph.
// That fallback is exactly the bug — a stale snapshot standing in for a current
// one, with nothing on the surface saying so — and it is gone. But
// format-version skew is TRANSIENT-BY-REINDEX, not corruption, and answering it
// with an opaque error behind an empty panel tells the user nothing about the
// one action that fixes it.
//
// So the no-fallback behaviour stays and the REASON is surfaced instead.
// graph.ReindexRequiredReason re-reads the on-disk header (segment-set aware)
// and renders the same "graph format vN incompatible — reindex required"
// wording every other consumer of FormatVersionError uses, so a user sees one
// consistent message whichever surface reported it first. Any other load
// failure is passed through unchanged.
func graphUnreadableError(stateDir string, err error) error {
	var fvErr *graph.FormatVersionError
	if required, reason := graph.ReindexRequiredReason(stateDir); required {
		return fmt.Errorf("graph unreadable — %s", reason)
	} else if errors.As(err, &fvErr) {
		// The header re-read disagreed (the graph changed under us, or only a
		// later segment is incompatible) but the load itself reported version
		// skew. Trust the load and render the same wording.
		return fmt.Errorf("graph unreadable — %s",
			graph.FormatVersionReason(fvErr.Found, fvErr.Required))
	}
	return err
}

// graphJSONUsable reports whether stateDir's graph.json may be served as-is,
// i.e. whether it is STRICTLY NEWER than the active graph on disk (#6015).
//
// The comparison is deliberately `After`, never `!Before` — and the reason is
// the opposite of the obvious one. It is not that ties are an unlucky accident
// of filesystem granularity: an equal mtime is DELIBERATELY ENGINEERED.
// cmd/grafel/index.go stamps both artifacts with one timestamp on every export
// (`now := time.Now(); os.Chtimes(fbPath, now, now); os.Chtimes(outPath, now,
// now)`, #1626) precisely so a drift check cannot fire on two encodings of the
// same pass.
//
// So a tie carries two readings that mtime alone cannot separate: "co-written
// by one index pass, and current", or "an older export that a later graph write
// happened to land on". The first is common, the second is vanishingly rare —
// but only the second is a CORRECTNESS question, and the cost of being wrong is
// asymmetric: a needless re-marshal is slow, a silently-stale graph is wrong.
// Ties therefore resolve to regenerate.
//
// CONSEQUENCE, stated plainly because it is a real cost and not a theoretical
// one: for a SINGLE-FILE repo that also exports graph.json, the Chtimes above
// makes the mtimes equal every time, so this returns false every time and the
// raw-read fast path is effectively DISABLED — every dashboard graph request
// re-loads and re-marshals, and GroupGraph does so once per repo in the group.
// SEGMENT-SET repos are unaffected: there fbPath is the gen DIRECTORY (see
// fbwriter.writeSegments' `return genDir, nil`), so Chtimes stamps the dir
// while this gate reads manifest.json — written before the pointer flip, hence
// strictly older — and the JSON stays newer and served. The behaviour is
// genuinely asymmetric between the two layouts.
//
// That asymmetry is accepted here only because the blast radius is narrow:
// FB-only is the default, so this touches repos that opted into `--export-json`
// (and the graph.json written by `grafel quality` / xrepo_verify), never the
// 427k-entity reference corpus. Removing it properly needs a generation
// identity stamped INTO the JSON — the shape descriptions.CurrentSourceKey
// already computes for its sidecars — rather than a timestamp comparison, which
// is a larger change than this fix should carry.
//
// Freshness is measured against graph.CurrentGraphDescriptor, not a hardcoded
// graph.fb path, so a SEGMENT-SET repo (graph.<gen>/ + manifest.json, no flat
// .fb) is compared against its manifest rather than being mistaken for "no
// graph exists" and pinned to the JSON — the same defect one layout over.
//
// It returns true when graph.json is the ONLY graph on disk (GraphAbsent): a
// legacy JSON-only repo has nothing newer for it to be stale against.
func graphJSONUsable(stateDir, jsonPath string) bool {
	ji, err := os.Stat(jsonPath)
	// The IsDir arm is belt-and-braces, not a load-bearing guard: a directory at
	// graph.json would fail os.ReadFile in repoGraphBytes and fall through to
	// the loader anyway. It is kept so this predicate answers honestly on its
	// own terms ("is there a graph.json FILE to serve?") rather than relying on
	// its caller's error handling, but no test binds it and none should imply
	// otherwise.
	if err != nil || ji.IsDir() {
		return false
	}
	desc, err := graph.CurrentGraphDescriptor(stateDir)
	if err != nil {
		// A corrupt/hostile segment-set manifest: a graph exists but cannot be
		// dated. Refuse to serve the JSON — the load below will surface the real
		// error instead of masking it with a stale snapshot.
		return false
	}
	if desc.Kind == graph.GraphAbsent {
		// graph.json is the ONLY graph on disk (a legacy JSON-only repo): there
		// is nothing for it to be stale against, and the raw read is the fast
		// path. LoadGraphFromDir would also fall back to this same file, but via
		// an unmarshal + re-marshal round trip on every request.
		return true
	}
	srcMod, ok := graphSourceModTime(desc)
	if !ok {
		// A graph exists but cannot be dated (it was replaced or removed between
		// the descriptor resolve and the stat). Undatable is treated exactly like
		// newer: refuse the JSON and let the load below produce either the real
		// graph or a real error. There is no reading of "I could not check" that
		// justifies serving a possibly-superseded snapshot.
		return false
	}
	return ji.ModTime().After(srcMod)
}

// graphSourceModTime returns the commit mtime of the active graph desc
// describes, and ok=false when desc names no graph (GraphAbsent) or the graph
// cannot be stat'd. Split out so repoGraphBytes' gate reads as one comparison
// and each layout's commit point is named explicitly.
func graphSourceModTime(desc graph.GraphDescriptor) (time.Time, bool) {
	switch desc.Kind {
	case graph.GraphSingleFile:
		fi, err := os.Stat(desc.Path)
		if err != nil {
			return time.Time{}, false
		}
		return fi.ModTime(), true
	case graph.GraphSegmentSet:
		// manifest.json is the segment set's atomic commit point; the gen dir is
		// the fallback when the manifest itself cannot be stat'd.
		fi, err := os.Stat(filepath.Join(desc.GenDir, graph.ManifestFileName))
		if err != nil {
			if fi, err = os.Stat(desc.GenDir); err != nil {
				return time.Time{}, false
			}
		}
		return fi.ModTime(), true
	default: // graph.GraphAbsent
		return time.Time{}, false
	}
}

func (liveStore) CreateGroup(name string) (GroupSummary, error) {
	// #6186 F6/R1: ConfigPathForNew validates before deriving the path, not
	// after — ConfigPathFor filepath.Joins the raw name, which collapses
	// "..", so a name like "../../pwned" resolves outside the config
	// directory; validating only at AddGroup (after SaveGroupConfig already
	// wrote the file) is too late.
	configPath, err := registry.ConfigPathForNew(name)
	if err != nil {
		return GroupSummary{}, err
	}
	if _, err := os.Stat(configPath); err == nil {
		return GroupSummary{}, fmt.Errorf("group %q already exists", name)
	}
	cfg := &registry.GroupConfig{Name: name}
	cfg.Features.Watchers = true // new groups default to watcher ON (debounced partial reindex)
	if err := registry.SaveGroupConfig(configPath, cfg); err != nil {
		return GroupSummary{}, err
	}
	if err := registry.AddGroup(name, configPath); err != nil {
		return GroupSummary{}, err
	}
	return GroupSummary{Name: name, ConfigPath: configPath}, nil
}

func (liveStore) AddRepo(group string, repo registry.Repo) error {
	if repo.Slug == "" {
		return errors.New("repo slug required")
	}
	if repo.Path == "" {
		return errors.New("repo path required")
	}
	groups, err := registry.Groups()
	if err != nil {
		return err
	}
	var configPath string
	for _, g := range groups {
		if g.Name == group {
			configPath = g.ConfigPath
			break
		}
	}
	if configPath == "" {
		return fmt.Errorf("group %q not registered", group)
	}
	cfg, err := registry.LoadGroupConfig(configPath)
	if err != nil {
		return err
	}
	for _, r := range cfg.Repos {
		if r.Slug == repo.Slug {
			return fmt.Errorf("repo %q already registered in group %q", repo.Slug, group)
		}
	}
	cfg.Repos = append(cfg.Repos, repo)
	return registry.SaveGroupConfig(configPath, cfg)
}

// groupConfig is a small helper used by the read-side handlers.
func groupConfig(group string) (*registry.GroupConfig, error) {
	groups, err := registry.Groups()
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		if g.Name == group {
			return registry.LoadGroupConfig(g.ConfigPath)
		}
	}
	return nil, fmt.Errorf("group %q not registered", group)
}
