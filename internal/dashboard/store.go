package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
type GroupSummary struct {
	Name       string   `json:"name"`
	ConfigPath string   `json:"config_path"`
	Repos      []string `json:"repos"`
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
		if cfg, err := registry.LoadGroupConfig(g.ConfigPath); err == nil {
			for _, r := range cfg.Repos {
				s.Repos = append(s.Repos, r.Slug)
			}
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
	// graph.json bytes. Communities, god-nodes and cross-repo links are
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
		b, err := os.ReadFile(filepath.Join(r.Path, ".archigraph", "graph.json"))
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
			return os.ReadFile(filepath.Join(r.Path, ".archigraph", "graph.json"))
		}
	}
	return nil, fmt.Errorf("repo %q not registered in group %q", repo, group)
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
