// Package links implements the cross-repo link passes (P1/P2/P3) that run
// after per-repo indexing has produced <repo>/.archigraph/graph.json for
// every repo in a group. The output is a shared
// ~/.archigraph/groups/<group>-links.json document plus a
// candidates.json sidecar for mid-confidence matches and a
// rejections.json file used to suppress already-rejected candidates.
//
// Three pass kinds:
//
//   - P1 (import_pass.go) — structural cross-repo imports/calls edges
//   - P2 (label_pass.go) — TF-IDF + kind-compat shared-label match
//   - P3 (string_pass.go) — string-pattern catalog (HTTP routes, ARNs, etc.)
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
)

// SchemaVersion is the integer version of the links file shape.
const SchemaVersion = 1

// Relation values used in the relation field.
const (
	RelationCalls       = "calls"
	RelationImports     = "imports"
	RelationSharedLabel = "shared_label"
	RelationStringMatch = "string_match"
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

	// Reason is populated only on candidate entries (not in links.json).
	Reason string `json:"reason,omitempty"`

	// Resolution is populated only on rejection entries.
	Resolution *Resolution `json:"resolution,omitempty"`
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
	ID         string            // local entity id
	Name       string            // raw name
	Kind       string            // class/function/...
	SourceFile string            // relative path
	Properties map[string]string // optional
}

type edgeRef struct {
	FromID string
	ToID   string
	Kind   string // imports, calls, ...
}

// onDiskGraph is the minimal subset of graph.Document we need.
type onDiskGraph struct {
	Repo     string `json:"repo"`
	Entities []struct {
		ID         string            `json:"id"`
		Name       string            `json:"name"`
		Kind       string            `json:"kind"`
		SourceFile string            `json:"source_file"`
		Properties map[string]string `json:"properties,omitempty"`
	} `json:"entities"`
	Relationships []struct {
		FromID string `json:"from_id"`
		ToID   string `json:"to_id"`
		Kind   string `json:"kind"`
	} `json:"relationships"`
}

// loadAllGraphs walks graphsDir and returns one repoGraph per
// <slug>/graph.json (or one if graphsDir is itself a graph.json).
func loadAllGraphs(graphsDir string) ([]repoGraph, error) {
	if graphsDir == "" {
		return nil, errors.New("graphs dir required")
	}
	fi, err := os.Stat(graphsDir)
	if err != nil {
		return nil, err
	}
	var paths []string
	if !fi.IsDir() {
		paths = []string{graphsDir}
	} else {
		err := filepath.WalkDir(graphsDir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Base(p) == "graph.json" {
				paths = append(paths, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(paths)
	graphs := make([]repoGraph, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var g onDiskGraph
		if err := json.Unmarshal(b, &g); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		if g.Repo == "" {
			// Fallback: derive from parent dir name.
			g.Repo = filepath.Base(filepath.Dir(filepath.Dir(p)))
		}
		if seen[g.Repo] {
			continue
		}
		seen[g.Repo] = true
		// Resolve the on-disk source-file root: if `p` is a symlink, follow
		// it so FileRoot points at the real repository (where the source
		// files live), not the staging directory we may have built.
		realPath := p
		if rp, err := filepath.EvalSymlinks(p); err == nil {
			realPath = rp
		}
		rg := repoGraph{
			Repo:     g.Repo,
			Path:     p,
			FileRoot: filepath.Dir(filepath.Dir(realPath)),
		}
		for _, e := range g.Entities {
			rg.Entities = append(rg.Entities, entityNode{
				ID:         e.ID,
				Name:       e.Name,
				Kind:       e.Kind,
				SourceFile: e.SourceFile,
				Properties: e.Properties,
			})
		}
		for _, r := range g.Relationships {
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
