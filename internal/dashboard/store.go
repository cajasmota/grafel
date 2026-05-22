package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// RegistryStore is the small surface the HTTP handlers need. Splitting it
// out lets tests inject an in-memory implementation without touching
// ~/.archigraph on disk.
type RegistryStore interface {
	ListGroups() ([]GroupSummary, error)
	GroupGraph(group string) ([]byte, error)
	RepoGraph(group, repo string) ([]byte, error)
	CreateGroup(name string) (GroupSummary, error)
	AddRepo(group string, repo registry.Repo) error
}

// GroupSummary is the registry list shape returned by GET /api/registry.
// entity_count, last_indexed are aggregated from per-repo graph-stats.json
// sidecars (written by `archigraph index`). The aggregation is cached in
// registryStatsCache and refreshed at most once every 30 s.
type GroupSummary struct {
	Name        string   `json:"name"`
	ConfigPath  string   `json:"config_path"`
	Repos       []string `json:"repos"`
	EntityCount int      `json:"entity_count"`
	LastIndexed string   `json:"last_indexed,omitempty"` // RFC3339, most-recent across repos
	Frameworks  []string `json:"frameworks,omitempty"`  // top-8 frameworks by frequency, desc
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
			// Sidecar not yet written — fall back to graph.fb/graph.json mtime.
			if info, e2 := os.Stat(filepath.Join(stateDir, "graph.fb")); e2 == nil {
				if info.ModTime().After(latest) {
					latest = info.ModTime()
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
// registry under ~/.archigraph and from each repo's .archigraph/graph.json.
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
// to loading graph.fb and re-marshaling to JSON when only the binary
// graph exists.
func repoGraphBytes(repoPath string) ([]byte, error) {
	jsonPath := daemon.GraphPathForRepo(repoPath)
	if b, err := os.ReadFile(jsonPath); err == nil {
		return b, nil
	}
	// graph.json not found — try to load from graph.fb and re-marshal.
	stateDir := daemon.StateDirForRepo(repoPath)
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

func (liveStore) CreateGroup(name string) (GroupSummary, error) {
	if name == "" {
		return GroupSummary{}, errors.New("group name required")
	}
	configPath, err := registry.ConfigPathFor(name)
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
