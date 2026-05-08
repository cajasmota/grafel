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
	Version         int               `json:"version"`
	GeneratedAt     time.Time         `json:"generated_at"`
	Repo            string            `json:"repo"`
	IndexerVersion  string            `json:"indexer_version"`
	Stats           Stats             `json:"stats"`
	Entities        []Entity          `json:"entities"`
	Relationships   []Relationship    `json:"relationships"`
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

// WriteAtomic marshals doc to JSON and writes it to outPath atomically by
// writing to a sibling .tmp file and renaming on success.
func WriteAtomic(outPath string, doc *Document) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("graph: mkdir %s: %w", filepath.Dir(outPath), err)
	}
	tmp := outPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("graph: create tmp: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
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
