// Package graph defines the public on-disk schema produced by `archigraph index`.
// The schema is stable and versioned; downstream tools (graph loaders,
// MCP servers, viewers) consume graph.json files written by this package.
package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SchemaVersion is the integer version of the on-disk graph.json schema.
// Bump when making a backwards-incompatible change.
const SchemaVersion = 1

// Document is the top-level structure written to <repo>/.archigraph/graph.json.
type Document struct {
	Version        int               `json:"version"`
	GeneratedAt    time.Time         `json:"generated_at"`
	Repo           string            `json:"repo"`
	IndexerVersion string            `json:"indexer_version"`
	Stats          Stats             `json:"stats"`
	Entities       []Entity          `json:"entities"`
	Relationships  []Relationship    `json:"relationships"`
	Communities    []CommunityResult `json:"communities,omitempty"`
	SurpriseEdges  []SurpriseEdge    `json:"surprise_edges,omitempty"`
	AlgorithmStats *AlgorithmStats   `json:"algorithm_stats,omitempty"`

	// Phase 0 git metadata (#2088). Populated at index time by
	// internal/gitmeta.Capture. Empty/false for non-git repos or when the
	// graph was loaded from an older graph.fb written before this field was
	// added (FlatBuffers defaults to "" / false for missing fields).
	IndexedRef string `json:"indexed_ref,omitempty"`
	IndexedSHA string `json:"indexed_sha,omitempty"`
	IsWorktree bool   `json:"is_worktree,omitempty"`

	// CoverageStatus indicates whether the indexed working tree is a full
	// or partial checkout (#2181 / M4 of monorepo epic #2175).
	//
	// Values (see internal/gitmeta constants):
	//   ""        — field absent / legacy graph (treated as "full" by readers).
	//   "full"    — normal full checkout; all tracked files are present.
	//   "partial" — git sparse-checkout is active; only a subset of paths
	//               was indexed. Readers should surface a badge in the UI.
	CoverageStatus string `json:"coverage_status,omitempty"`
}

// Stats summarises a Document.
type Stats struct {
	Files         int `json:"files"`
	Entities      int `json:"entities"`
	Relationships int `json:"relationships"`
}

// Entity is a single node in the graph.
type Entity struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	QualifiedName string                 `json:"qualified_name,omitempty"`
	Kind          string                 `json:"kind"`
	Subtype       string                 `json:"subtype,omitempty"`
	SourceFile    string                 `json:"source_file"`
	StartLine     int                    `json:"start_line"`
	EndLine       int                    `json:"end_line"`
	Language      string                 `json:"language"`
	Signature     string                 `json:"signature,omitempty"`
	Tags          []string               `json:"tags,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Properties    map[string]string      `json:"properties,omitempty"`

	// PH8 (#2100): content-hash pointer into the shared embedding cache.
	// When non-empty, readers load the vector from Cache instead of
	// computing it inline. Old graphs have this absent; omitempty preserves
	// byte-identical output for graphs written before PH8.
	EmbeddingRef string `json:"embedding_ref,omitempty"`

	// Pass 4 (graph algorithm) attributes. Pointers + omitempty so that
	// documents written with --skip-pass=graph-algo stay byte-identical to
	// the pre-PORT-4 schema.
	CommunityID        *int     `json:"community_id,omitempty"`
	Centrality         *float64 `json:"centrality,omitempty"`
	PageRank           *float64 `json:"pagerank,omitempty"`
	IsGodNode          bool     `json:"is_god_node,omitempty"`
	IsSurpriseEndpoint bool     `json:"is_surprise_endpoint,omitempty"`
	IsArticulationPt   bool     `json:"is_articulation_point,omitempty"`
}

// Relationship is a directed edge between entities.
type Relationship struct {
	ID         string            `json:"id"`
	FromID     string            `json:"from_id"`
	ToID       string            `json:"to_id"`
	Kind       string            `json:"kind"`
	Properties map[string]string `json:"properties,omitempty"`
}

// EntityID computes a stable 16-char hex id from a repo tag and an entity's
// identity fields (kind + name + source file).
func EntityID(repo, kind, name, sourceFile string) string {
	h := sha256.New()
	h.Write([]byte(repo))
	h.Write([]byte{0})
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(name))
	h.Write([]byte{0})
	h.Write([]byte(sourceFile))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// RelationshipID computes a stable 16-char hex id for an edge.
func RelationshipID(fromID, toID, kind string) string {
	h := sha256.New()
	h.Write([]byte(fromID))
	h.Write([]byte{0})
	h.Write([]byte(toID))
	h.Write([]byte{0})
	h.Write([]byte(kind))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// GraphStatsSidecar is the corpus-level summary written to
// <repo>/.archigraph/graph-stats.json. Consumed by `archigraph doctor` and the
// future MCP `graph_stats` tool.
type GraphStatsSidecar struct {
	Version            int       `json:"version"`
	ComputedAt         time.Time `json:"computed_at"`
	TotalFiles         int       `json:"total_files,omitempty"`
	TotalEntities      int       `json:"total_entities"`
	TotalRelationships int       `json:"total_relationships"`
	Communities        int       `json:"communities"`
	Modularity         float64   `json:"modularity"`
	GodNodes           int       `json:"god_nodes"`
	ArticulationPoints int       `json:"articulation_points"`
	RuntimeMS          int64     `json:"runtime_ms"`
}

// WriteSidecar emits the graph-stats.json sidecar next to the main document.
// outPath is the same path passed to WriteAtomic; the sidecar is written to
// the sibling file `graph-stats.json`. When pretty is true, the JSON is
// indented for human readability; otherwise it is minified (default).
func WriteSidecar(outPath string, side *GraphStatsSidecar, pretty bool) error {
	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("graph: mkdir %s: %w", dir, err)
	}
	target := filepath.Join(dir, "graph-stats.json")
	tmp := target + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("graph: create sidecar tmp: %w", err)
	}
	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(side); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("graph: encode sidecar: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, target)
}

// WriteAtomic marshals doc to JSON and writes it to outPath atomically by
// writing to a sibling .tmp file and renaming on success. When pretty is
// true, the JSON is indented for human readability; otherwise it is minified
// (the default — minified output is materially smaller on real repos).
func WriteAtomic(outPath string, doc *Document, pretty bool) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("graph: mkdir %s: %w", filepath.Dir(outPath), err)
	}
	tmp := outPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("graph: create tmp: %w", err)
	}
	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(doc); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("graph: encode: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("graph: close tmp: %w", err)
	}
	if err := os.Rename(tmp, outPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("graph: rename: %w", err)
	}
	return nil
}
