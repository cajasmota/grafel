package embed

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cajasmota/grafel/internal/graph"
)

// EmbeddingTextVersion is bumped whenever the embed-text construction below
// changes, so that content-hash invalidation re-embeds every entity even when
// the underlying source is unchanged. (#461 / ADR-0019.)
const EmbeddingTextVersion = "v1"

// Chunking parameters for the sliding-window fallback applied to oversized
// snippets (GitNexus precedent). We embed a single representative chunk per
// entity (the head window), which is sufficient for find-related-symbol
// recall while keeping one vector per entity.
const (
	chunkMaxChars = 1200
	chunkOverlap  = 120
)

// EmbedText builds the text that represents an entity for embedding:
// name + qualified_name + docstring + a bounded code snippet. The snippet is
// AST-bounded by the entity's StartLine/EndLine (statement/decl boundaries
// already captured by the extractors) and then clamped to a sliding window.
//
// snippetFor reads the source for an entity; it is injected so callers can
// cache file contents across the many entities that share a file.
func EmbedText(e *graph.Entity, snippet string) string {
	var b strings.Builder
	b.WriteString(e.Name)
	if e.QualifiedName != "" && e.QualifiedName != e.Name {
		b.WriteString("\n")
		b.WriteString(e.QualifiedName)
	}
	if e.Signature != "" {
		b.WriteString("\n")
		b.WriteString(e.Signature)
	}
	if e.PropLen() > 0 {
		if ds := strings.TrimSpace(e.PropGet("docstring")); ds != "" {
			b.WriteString("\n")
			b.WriteString(ds)
		} else if d := strings.TrimSpace(e.PropGet("description")); d != "" {
			b.WriteString("\n")
			b.WriteString(d)
		}
	}
	if snippet != "" {
		b.WriteString("\n")
		b.WriteString(headWindow(snippet))
	}
	return b.String()
}

// headWindow clamps text to chunkMaxChars on a rune boundary. (Overlap is
// reserved for a future multi-chunk strategy; with one vector per entity we
// take the head window only — see chunkOverlap doc.)
func headWindow(s string) string {
	_ = chunkOverlap
	if len(s) <= chunkMaxChars {
		return s
	}
	// Clamp on a UTF-8 boundary.
	cut := chunkMaxChars
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// ContentHash is the SHA1 over the embed text plus EmbeddingTextVersion. It is
// the invalidation key: an entity is re-embedded only when its hash changes.
func ContentHash(text string) string {
	h := sha1.New()
	h.Write([]byte(EmbeddingTextVersion))
	h.Write([]byte{0})
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

// snippetReader reads and caches source files so the per-entity snippet
// extraction does not re-read a file once per entity.
type snippetReader struct {
	repoRoot string
	mu       sync.Mutex
	cache    map[string][]string // sourceFile -> lines
}

func newSnippetReader(repoRoot string) *snippetReader {
	return &snippetReader{repoRoot: repoRoot, cache: map[string][]string{}}
}

// snippet returns the source lines [StartLine, EndLine] for an entity (1-based
// inclusive), or "" if the file is unreadable or lines are unset.
func (r *snippetReader) snippet(e *graph.Entity) string {
	if e.SourceFile == "" || e.StartLine <= 0 || e.EndLine < e.StartLine {
		return ""
	}
	lines := r.lines(e.SourceFile)
	if lines == nil {
		return ""
	}
	start := e.StartLine - 1
	end := e.EndLine
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start >= end {
		return ""
	}
	return strings.Join(lines[start:end], "\n")
}

func (r *snippetReader) lines(sourceFile string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.cache[sourceFile]; ok {
		return v
	}
	path := sourceFile
	if !filepath.IsAbs(path) && r.repoRoot != "" {
		path = filepath.Join(r.repoRoot, sourceFile)
	}
	data, err := os.ReadFile(path)
	var ls []string
	if err == nil {
		ls = strings.Split(string(data), "\n")
	}
	r.cache[sourceFile] = ls // cache nil too, to avoid retrying
	return ls
}
