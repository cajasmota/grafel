package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/archigraph/internal/classifier"
	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/treesitter"
	"github.com/cajasmota/archigraph/internal/types"
	"github.com/cajasmota/archigraph/internal/version"
)

// Index walks repoPath, runs the per-language extractors, and writes the
// resulting entity/relationship graph to outPath (or the default
// <repo>/.archigraph/graph.json). repoTag is stored on every entity; an
// empty value falls back to filepath.Base(repoPath).
func Index(repoPath, outPath, repoTag string) error {
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}
	info, err := os.Stat(absRepo)
	if err != nil {
		return fmt.Errorf("stat repo: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("repo path %q is not a directory", absRepo)
	}

	if repoTag == "" {
		repoTag = filepath.Base(absRepo)
	}
	if outPath == "" {
		outPath = filepath.Join(absRepo, ".archigraph", "graph.json")
	}

	start := time.Now()

	cls, err := classifier.New("", nil)
	if err != nil {
		return fmt.Errorf("init classifier: %w", err)
	}
	parser := treesitter.NewParserFactory(nil)

	files, err := walkRepo(absRepo)
	if err != nil {
		return fmt.Errorf("walk repo: %w", err)
	}
	fmt.Fprintf(os.Stderr, "archigraph: discovered %d candidate files in %s\n", len(files), absRepo)

	ctx := context.Background()

	type fileTask struct {
		relPath string
		absPath string
	}
	type fileResult struct {
		entities []types.EntityRecord
		err      error
	}

	tasks := make(chan fileTask, len(files))
	for _, rel := range files {
		tasks <- fileTask{relPath: rel, absPath: filepath.Join(absRepo, rel)}
	}
	close(tasks)

	var (
		mu         sync.Mutex
		allRecords []types.EntityRecord
		processed  int
		extracted  int
		skipped    int
		failed     int
	)

	workers := 8
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tasks {
				size := int64(-1)
				if st, err := os.Stat(t.absPath); err == nil {
					size = st.Size()
				}
				cr := cls.ClassifyWithSize(ctx, t.relPath, size)
				if cr.Skip || cr.Language == "" {
					mu.Lock()
					skipped++
					mu.Unlock()
					continue
				}

				content, err := os.ReadFile(t.absPath)
				if err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					continue
				}

				file := extractor.FileInput{
					Path:     t.relPath,
					Content:  content,
					Language: cr.Language,
				}

				if pr, perr := parser.Parse(ctx, content, cr.Language); perr == nil && pr != nil {
					file.Tree = pr.Tree
				}

				ents, err := extractors.Extract(ctx, file)
				mu.Lock()
				processed++
				if err != nil {
					failed++
				} else {
					extracted++
					allRecords = append(allRecords, ents...)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	doc := buildDocument(repoTag, allRecords, processed)
	if err := graph.WriteAtomic(outPath, doc); err != nil {
		return err
	}

	dur := time.Since(start)
	fmt.Fprintf(os.Stderr, "archigraph: processed=%d extracted=%d skipped=%d failed=%d entities=%d relationships=%d duration=%s\n",
		processed, extracted, skipped, failed, doc.Stats.Entities, doc.Stats.Relationships, dur.Round(time.Millisecond))
	fmt.Fprintf(os.Stderr, "archigraph: wrote %s\n", outPath)
	return nil
}

// buildDocument converts internal EntityRecords into the public on-disk schema.
func buildDocument(repoTag string, records []types.EntityRecord, fileCount int) *graph.Document {
	entities := make([]graph.Entity, 0, len(records))
	relationships := make([]graph.Relationship, 0)

	// Two-pass: first compute stable IDs for every entity by (kind, name, source_file).
	idIndex := make(map[string]string, len(records))
	for i := range records {
		r := &records[i]
		key := r.Kind + "\x00" + r.Name + "\x00" + r.SourceFile
		id := graph.EntityID(repoTag, r.Kind, r.Name, r.SourceFile)
		idIndex[key] = id
	}

	for i := range records {
		r := &records[i]
		key := r.Kind + "\x00" + r.Name + "\x00" + r.SourceFile
		id := idIndex[key]

		entities = append(entities, graph.Entity{
			ID:            id,
			Name:          r.Name,
			QualifiedName: r.QualifiedName,
			Kind:          r.Kind,
			Subtype:       r.Subtype,
			SourceFile:    r.SourceFile,
			StartLine:     r.StartLine,
			EndLine:       r.EndLine,
			Language:      r.Language,
			Signature:     r.Signature,
			Tags:          r.Tags,
			Metadata:      r.Metadata,
			Properties:    r.Properties,
		})

		for j := range r.Relationships {
			rel := &r.Relationships[j]
			fromID := rel.FromID
			toID := rel.ToID
			if fromID == "" {
				fromID = id
			}
			// ToID often references a bare name; pass through unchanged.
			relID := graph.RelationshipID(fromID, toID, rel.Kind)
			relationships = append(relationships, graph.Relationship{
				ID:         relID,
				FromID:     fromID,
				ToID:       toID,
				Kind:       rel.Kind,
				Properties: rel.Properties,
			})
		}
	}

	return &graph.Document{
		Version:        graph.SchemaVersion,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoTag,
		IndexerVersion: version.String(),
		Stats: graph.Stats{
			Files:         fileCount,
			Entities:      len(entities),
			Relationships: len(relationships),
		},
		Entities:      entities,
		Relationships: relationships,
	}
}

// walkRepo returns repo-relative file paths, skipping common directories
// that should never be indexed (.git, node_modules, vendor, etc.).
func walkRepo(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		base := d.Name()
		if d.IsDir() {
			if isSkippedDir(base) {
				return filepath.SkipDir
			}
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

func isSkippedDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn",
		"node_modules", "vendor", "__pycache__",
		".archigraph", ".venv", "venv",
		".idea", ".vscode",
		"dist", "build", "target", ".next", ".nuxt",
		"coverage", ".pytest_cache", ".mypy_cache":
		return true
	}
	if strings.HasPrefix(name, ".") && len(name) > 1 {
		// hidden dirs: skip by default (.terraform, .gradle, .m2, etc.)
		return true
	}
	return false
}
