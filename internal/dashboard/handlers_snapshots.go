package dashboard

// handlers_snapshots.go — Graph Snapshot + Diff surface (issue #1353)
//
// Endpoints:
//
//	POST /api/snapshots/{group}                       — save named snapshot
//	GET  /api/snapshots/{group}                       — list snapshots
//	DELETE /api/snapshots/{group}/{id}                — delete snapshot
//	GET  /api/snapshots/{group}/{id}/diff             — diff snapshot vs current
//	                 ?against=current (default)
//	                 ?filter_kind=Function
//	                 ?filter_repo=myrepo
//
// On-disk layout:
//
//	~/.archigraph/snapshots/<group>/<id>/
//	  meta.json         — SnapshotMeta
//	  <repo-slug>.json  — raw graph.json bytes for that repo at snapshot time
//
// IDs are UTC timestamps in RFC3339-compact form (20060102T150405Z) so
// they sort chronologically without an extra field.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// SnapshotMeta is the metadata file stored alongside snapshot graph data.
type SnapshotMeta struct {
	ID          string             `json:"id"`
	Group       string             `json:"group"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	Repos       []string           `json:"repos"`                  // slugs captured
	Stats       map[string]int     `json:"stats"`                  // repo slug → entity count
	OrphanRates map[string]float64 `json:"orphan_rates,omitempty"` // repo → orphan %
}

// DiffSummary is the response body for the diff endpoint.
type DiffSummary struct {
	SnapshotID   string       `json:"snapshot_id"`
	Group        string       `json:"group"`
	Against      string       `json:"against"` // "current" or another snapshot ID
	AddedCount   int          `json:"added_count"`
	RemovedCount int          `json:"removed_count"`
	EdgeAdded    int          `json:"edge_added_count"`
	EdgeRemoved  int          `json:"edge_removed_count"`
	Added        []DiffEntity `json:"added"`
	Removed      []DiffEntity `json:"removed"`
	EdgeChanges  []DiffEdge   `json:"edge_changes,omitempty"`
}

// DiffEntity is a single entity change record.
type DiffEntity struct {
	Repo string `json:"repo"`
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"source_file"`
}

// DiffEdge is a single edge change record.
type DiffEdge struct {
	Change string `json:"change"` // "added" | "removed"
	Repo   string `json:"repo"`
	From   string `json:"from_id"`
	To     string `json:"to_id"`
	Kind   string `json:"kind"`
}

// ---------------------------------------------------------------------------
// Directory helpers
// ---------------------------------------------------------------------------

// snapshotRootDir returns ~/.archigraph/snapshots (or $ARCHIGRAPH_DAEMON_ROOT/snapshots).
func snapshotRootDir() (string, error) {
	h, err := registry.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "snapshots"), nil
}

// snapshotGroupDir returns the per-group snapshot directory.
func snapshotGroupDir(group string) (string, error) {
	root, err := snapshotRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, group), nil
}

// snapshotDir returns the directory for a specific snapshot.
func snapshotDir(group, id string) (string, error) {
	gdir, err := snapshotGroupDir(group)
	if err != nil {
		return "", err
	}
	return filepath.Join(gdir, id), nil
}

// snapshotID generates a sortable, filesystem-safe ID from the current time.
func snapshotID() string {
	return time.Now().UTC().Format("20060102T150405Z")
}

// ---------------------------------------------------------------------------
// POST /api/snapshots/{group}
// ---------------------------------------------------------------------------

// saveSnapshotReq is the request body for creating a snapshot.
type saveSnapshotReq struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// handleSaveSnapshot — POST /api/snapshots/{group}
//
// Body: {"name": "before-refactor", "description": "optional"}
//
// Reads graph.json for every repo in the group and writes it to
// ~/.archigraph/snapshots/<group>/<id>/<slug>.json alongside meta.json.
func (s *Server) handleSaveSnapshot(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	var req saveSnapshotReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}

	// Resolve group config to find repos.
	cfg, err := groupConfig(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, "group not found: "+err.Error())
		return
	}

	id := snapshotID()
	dir, err := snapshotDir(group, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "resolve snapshot dir: "+err.Error())
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, "create snapshot dir: "+err.Error())
		return
	}

	meta := SnapshotMeta{
		ID:          id,
		Group:       group,
		Name:        req.Name,
		Description: req.Description,
		CreatedAt:   time.Now().UTC(),
		Stats:       make(map[string]int),
		OrphanRates: make(map[string]float64),
	}

	for _, repo := range cfg.Repos {
		b, err := repoGraphBytes(repo.Path)
		if err != nil {
			// Repo not yet indexed — skip but record it.
			continue
		}
		// Write graph data.
		repoFile := filepath.Join(dir, repo.Slug+".json")
		if err := os.WriteFile(repoFile, b, 0o644); err != nil {
			writeErr(w, http.StatusInternalServerError, fmt.Sprintf("write %s: %v", repo.Slug, err))
			return
		}
		meta.Repos = append(meta.Repos, repo.Slug)

		// Extract stats for meta.
		var doc graph.Document
		if json.Unmarshal(b, &doc) == nil {
			meta.Stats[repo.Slug] = doc.Stats.Entities
			if doc.Stats.Entities > 0 {
				orphanCount := countOrphans(doc)
				meta.OrphanRates[repo.Slug] = float64(orphanCount) / float64(doc.Stats.Entities)
			}
		}
	}

	// Write meta.json.
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "marshal meta: "+err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), metaBytes, 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, "write meta: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"snapshot": meta,
	})
}

// countOrphans returns the number of entities in doc that have no inbound relationships.
func countOrphans(doc graph.Document) int {
	hasInbound := make(map[string]bool, len(doc.Entities))
	for _, r := range doc.Relationships {
		hasInbound[r.ToID] = true
	}
	count := 0
	for _, e := range doc.Entities {
		if !hasInbound[e.ID] {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// GET /api/snapshots/{group}
// ---------------------------------------------------------------------------

// handleListSnapshots — GET /api/snapshots/{group}
//
// Returns all snapshots for the group, newest first.
func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	snapshots, err := loadSnapshotList(group)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list snapshots: "+err.Error())
		return
	}
	if snapshots == nil {
		snapshots = []SnapshotMeta{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"group":     group,
		"snapshots": snapshots,
	})
}

// loadSnapshotList reads all meta.json files under the group snapshot dir,
// sorted newest-first.
func loadSnapshotList(group string) ([]SnapshotMeta, error) {
	gdir, err := snapshotGroupDir(group)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(gdir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []SnapshotMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(gdir, e.Name(), "meta.json")
		b, err := os.ReadFile(metaPath)
		if err != nil {
			continue // corrupt or incomplete snapshot — skip
		}
		var m SnapshotMeta
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		out = append(out, m)
	}

	// Sort newest-first (IDs are time-sortable strings).
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// ---------------------------------------------------------------------------
// DELETE /api/snapshots/{group}/{id}
// ---------------------------------------------------------------------------

// handleDeleteSnapshot — DELETE /api/snapshots/{group}/{id}
func (s *Server) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	id := r.PathValue("id")
	if group == "" || id == "" {
		writeErr(w, http.StatusBadRequest, "group and id required")
		return
	}

	// Sanitise: id must not contain path separators.
	if strings.ContainsAny(id, "/\\..") {
		writeErr(w, http.StatusBadRequest, "invalid snapshot id")
		return
	}

	dir, err := snapshotDir(group, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		writeErr(w, http.StatusInternalServerError, "delete snapshot: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// ---------------------------------------------------------------------------
// GET /api/snapshots/{group}/{id}/diff
// ---------------------------------------------------------------------------

// handleSnapshotDiff — GET /api/snapshots/{group}/{id}/diff
//
// Query params:
//
//	against=current (default) — compare snapshot against live graph
//	filter_kind=Function      — restrict diff to entities of this kind
//	filter_repo=myrepo        — restrict diff to a single repo slug
func (s *Server) handleSnapshotDiff(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	id := r.PathValue("id")
	if group == "" || id == "" {
		writeErr(w, http.StatusBadRequest, "group and id required")
		return
	}

	against := r.URL.Query().Get("against")
	if against == "" {
		against = "current"
	}
	filterKind := r.URL.Query().Get("filter_kind")
	filterRepo := r.URL.Query().Get("filter_repo")

	// Load snapshot meta.
	dir, err := snapshotDir(group, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	metaBytes, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "snapshot not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "read meta: "+err.Error())
		return
	}
	var meta SnapshotMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		writeErr(w, http.StatusInternalServerError, "parse meta: "+err.Error())
		return
	}

	// Determine the right-hand side graph source.
	var rhsGraphByRepo map[string][]byte
	switch against {
	case "current":
		rhsGraphByRepo, err = loadCurrentGraphByRepo(group, filterRepo)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "load current graph: "+err.Error())
			return
		}
	default:
		// against = another snapshot id
		if strings.ContainsAny(against, "/\\..") {
			writeErr(w, http.StatusBadRequest, "invalid against id")
			return
		}
		rhsGraphByRepo, err = loadSnapshotGraphByRepo(group, against, filterRepo)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "load comparison snapshot: "+err.Error())
			return
		}
	}

	// Load snapshot (left-hand side) graphs.
	lhsGraphByRepo, err := loadSnapshotGraphByRepo(group, id, filterRepo)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load snapshot graph: "+err.Error())
		return
	}

	// Compute diff.
	summary := computeDiff(id, group, against, lhsGraphByRepo, rhsGraphByRepo, filterKind)
	writeJSON(w, http.StatusOK, summary)
}

// loadCurrentGraphByRepo reads live graph.json files for the group.
func loadCurrentGraphByRepo(group, filterRepo string) (map[string][]byte, error) {
	cfg, err := groupConfig(group)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte)
	for _, repo := range cfg.Repos {
		if filterRepo != "" && repo.Slug != filterRepo {
			continue
		}
		b, err := repoGraphBytes(repo.Path)
		if err != nil {
			continue // not indexed yet
		}
		out[repo.Slug] = b
	}
	return out, nil
}

// loadSnapshotGraphByRepo reads archived graph JSON files from a snapshot dir.
func loadSnapshotGraphByRepo(group, id, filterRepo string) (map[string][]byte, error) {
	dir, err := snapshotDir(group, id)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("snapshot %q not found", id)
		}
		return nil, err
	}
	out := make(map[string][]byte)
	for _, e := range entries {
		if e.IsDir() || e.Name() == "meta.json" {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".json")
		if filterRepo != "" && slug != filterRepo {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out[slug] = b
	}
	return out, nil
}

// computeDiff computes added/removed entities and edges between lhs and rhs.
//
// Semantics: lhs = snapshot, rhs = current (or another snapshot).
//
//   - Added: entity in rhs but not in lhs  (appeared since snapshot)
//   - Removed: entity in lhs but not in rhs (disappeared since snapshot)
func computeDiff(
	snapshotID, group, against string,
	lhs, rhs map[string][]byte,
	filterKind string,
) DiffSummary {
	// Build entity sets keyed by "<repo>::<entity-id>".
	type entityKey struct{ repo, id string }

	lhsEntities := make(map[entityKey]graph.Entity)
	rhsEntities := make(map[entityKey]graph.Entity)
	lhsEdges := make(map[string]struct{}) // edge ID → present
	rhsEdges := make(map[string]struct{})

	parseRepo := func(slug string, data []byte, entitySet map[entityKey]graph.Entity, edgeSet map[string]struct{}) {
		var doc graph.Document
		if json.Unmarshal(data, &doc) != nil {
			return
		}
		for _, e := range doc.Entities {
			if filterKind != "" && e.Kind != filterKind {
				continue
			}
			entitySet[entityKey{slug, e.ID}] = e
		}
		for _, rel := range doc.Relationships {
			edgeSet[slug+"::"+rel.ID] = struct{}{}
		}
	}

	for slug, b := range lhs {
		parseRepo(slug, b, lhsEntities, lhsEdges)
	}
	for slug, b := range rhs {
		parseRepo(slug, b, rhsEntities, rhsEdges)
	}

	// Entities added (in rhs, not in lhs).
	var added []DiffEntity
	for k, e := range rhsEntities {
		if _, ok := lhsEntities[k]; !ok {
			added = append(added, DiffEntity{
				Repo: k.repo,
				ID:   e.ID,
				Name: e.Name,
				Kind: e.Kind,
				File: e.SourceFile,
			})
		}
	}

	// Entities removed (in lhs, not in rhs).
	var removed []DiffEntity
	for k, e := range lhsEntities {
		if _, ok := rhsEntities[k]; !ok {
			removed = append(removed, DiffEntity{
				Repo: k.repo,
				ID:   e.ID,
				Name: e.Name,
				Kind: e.Kind,
				File: e.SourceFile,
			})
		}
	}

	// Edge changes.
	var edgeChanges []DiffEdge
	for key := range rhsEdges {
		if _, ok := lhsEdges[key]; !ok {
			parts := strings.SplitN(key, "::", 2)
			edgeChanges = append(edgeChanges, DiffEdge{Change: "added", Repo: parts[0], From: parts[1]})
		}
	}
	for key := range lhsEdges {
		if _, ok := rhsEdges[key]; !ok {
			parts := strings.SplitN(key, "::", 2)
			edgeChanges = append(edgeChanges, DiffEdge{Change: "removed", Repo: parts[0], From: parts[1]})
		}
	}

	// Sort for deterministic output.
	sort.Slice(added, func(i, j int) bool {
		if added[i].Repo != added[j].Repo {
			return added[i].Repo < added[j].Repo
		}
		return added[i].Name < added[j].Name
	})
	sort.Slice(removed, func(i, j int) bool {
		if removed[i].Repo != removed[j].Repo {
			return removed[i].Repo < removed[j].Repo
		}
		return removed[i].Name < removed[j].Name
	})

	var edgeAdded, edgeRemoved int
	for _, ec := range edgeChanges {
		if ec.Change == "added" {
			edgeAdded++
		} else {
			edgeRemoved++
		}
	}

	return DiffSummary{
		SnapshotID:   snapshotID,
		Group:        group,
		Against:      against,
		AddedCount:   len(added),
		RemovedCount: len(removed),
		EdgeAdded:    edgeAdded,
		EdgeRemoved:  edgeRemoved,
		Added:        added,
		Removed:      removed,
		EdgeChanges:  edgeChanges,
	}
}
