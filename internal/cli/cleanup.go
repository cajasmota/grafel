package cli

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/embed"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

func newCleanupCmd() *cobra.Command {
	var dryRun bool
	var ttlDays int
	cmd := &cobra.Command{
		Use:   "cleanup [--dry-run]",
		Short: "Clean up orphaned registry entries and stale embedding cache entries",
		Long: `Scan registry.json and remove entries for groups whose fleet config
files no longer exist at the target path.

Also sweeps the cross-ref embedding cache (~/.grafel/embeddings/) to
remove .vec files that are no longer referenced by any active graph and whose
mtime is older than --ttl-days (default: 30, or GRAFEL_EMBEDDING_TTL_DAYS).

Use --dry-run to list orphaned entries without removing them.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCleanup(cmd.OutOrStdout(), dryRun, ttlDays)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", true,
		"list orphaned entries without removing them (default: true)")
	cmd.Flags().IntVar(&ttlDays, "ttl-days", 0,
		"embedding cache TTL in days (0 = use GRAFEL_EMBEDDING_TTL_DAYS or 30)")
	return cmd
}

func runCleanup(w io.Writer, dryRun bool, ttlDays int) error {
	reg, err := registry.Load()
	if err != nil {
		return err
	}

	// --- Part 1: orphaned registry entries ----------------------------------

	var orphaned []registry.GroupRef
	for _, g := range reg.Groups {
		_, err := os.Stat(g.ConfigPath)
		if err != nil && os.IsNotExist(err) {
			orphaned = append(orphaned, g)
		}
	}

	if len(orphaned) == 0 {
		fmt.Fprintln(w, "✓ No orphaned registry entries found")
	} else {
		fmt.Fprintf(w, "Found %d orphaned entries:\n", len(orphaned))
		for _, g := range orphaned {
			fmt.Fprintf(w, "  - %s (config: %s)\n", g.Name, g.ConfigPath)
		}
		if dryRun {
			fmt.Fprintln(w, "\nRun 'grafel cleanup' (without --dry-run) to remove these entries")
		} else {
			var cleaned []registry.GroupRef
			for _, g := range reg.Groups {
				_, err := os.Stat(g.ConfigPath)
				if err == nil || !os.IsNotExist(err) {
					cleaned = append(cleaned, g)
				}
			}
			reg.Groups = cleaned
			if err := registry.Save(reg); err != nil {
				return err
			}
			fmt.Fprintf(w, "\n✓ Removed %d orphaned entries\n", len(orphaned))
		}
	}

	// --- Part 2: embedding cache sweep (PH8 / #2100) -----------------------

	cache, cacheErr := embed.DefaultCache()
	if cacheErr != nil {
		fmt.Fprintf(w, "\n⚠ Embedding cache unavailable: %v (skipping sweep)\n", cacheErr)
		return nil
	}

	activeHashes, collectErr := collectActiveEmbeddingHashes()
	if collectErr != nil {
		fmt.Fprintf(w, "\n⚠ Could not collect active embedding hashes: %v (skipping sweep)\n", collectErr)
		return nil
	}

	fmt.Fprintf(w, "\nEmbedding cache: found %d active hashes across all graphs\n", len(activeHashes))

	if dryRun {
		fmt.Fprintln(w, "  (dry-run: not removing any cache entries)")
		return nil
	}

	removed, sweepErr := cache.Sweep(activeHashes, ttlDays)
	if sweepErr != nil {
		fmt.Fprintf(w, "  ⚠ Sweep encountered errors: %v\n", sweepErr)
	}
	fmt.Fprintf(w, "✓ Embedding cache sweep: removed %d stale entries\n", removed)
	return nil
}

// collectActiveEmbeddingHashes walks all graph.fb files under the grafel
// store directory and collects the set of embedding_ref values referenced by
// any entity in any active graph.
//
// The store layout is:
//
//	~/.grafel/store/<slug>-<hash>/refs/<ref-safe>/graph.fb
//
// plus (legacy) <repo>/.grafel/graph.fb for repos indexed before PH1a.
// We walk the daemon store directory; .grafel sidecar graphs are not
// swept (they are repo-local, managed by the repo owner).
func collectActiveEmbeddingHashes() (map[string]bool, error) {
	h, err := registry.HomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home: %w", err)
	}
	storeDir := filepath.Join(h, "store")

	active := map[string]bool{}

	walkErr := filepath.WalkDir(storeDir, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return nil // skip unreadable dirs
		}
		if d.IsDir() {
			return nil
		}
		// #5891: recognise generation files (graph.<gen>.fb), and process each
		// state dir exactly once — only when this path is the ACTIVE generation
		// resolved by the pointer (or the legacy flat graph.fb) — so a dir with
		// current+previous gens isn't walked twice.
		if !graph.IsGraphFileName(filepath.Base(path)) {
			return nil
		}
		if path != graph.CurrentGraphPath(filepath.Dir(path)) {
			return nil
		}
		doc, loadErr := graph.LoadGraphFromDir(filepath.Dir(path))
		if loadErr != nil {
			return nil // corrupt/missing — skip
		}
		for i := range doc.Entities {
			if ref := doc.Entities[i].EmbeddingRef; ref != "" {
				active[ref] = true
			}
		}
		return nil
	})

	if walkErr != nil && !os.IsNotExist(walkErr) {
		return active, walkErr
	}
	return active, nil
}
