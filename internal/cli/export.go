package cli

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/export"
	"github.com/cajasmota/archigraph/internal/registry"
)

// newExportCmd returns the `archigraph export` subcommand (issue #4291).
//
// It loads a group's indexed graph (the same load path used by feedback,
// doctor and links: per-repo StateDir → graph.LoadGraphFromDir) and writes a
// static interchange file. Four formats are supported:
//
//	archigraph export graphml [--group g --ref r --out file.graphml]
//	archigraph export cypher  [--group g --ref r --out file.cypher]
//	archigraph export svg     [--group g --ref r --out file.svg  --top-N 500]
//	archigraph export html    [--group g --ref r --out file.html --top-N 500]
//
// The merged document is sorted into a canonical node/edge order before
// serialization so every format is reproducible (same input → identical bytes).
func newExportCmd() *cobra.Command {
	var group string
	var refFlag string
	var outPath string
	var topN int

	cmd := &cobra.Command{
		Use:   "export <graphml|cypher|svg|html>",
		Short: "Export the group graph to a static file (GraphML, Cypher, SVG, or HTML)",
		Long: `export serializes a group's indexed code graph to a static file for
offline analysis, visualization, or sharing.

Formats:
  graphml   GraphML 1.0 XML (<graphml>/<graph>/<node>/<edge>) — opens in
            Gephi, yEd, Cytoscape, etc.
  cypher    Neo4j Cypher CREATE statements — load into a Neo4j database.
  svg       Self-contained static SVG (deterministic grid layout).
  html      Self-contained single HTML file (inline SVG + filterable node
            table; opens in any browser with no server).

For svg/html, --top-N caps the rendering to the N highest-degree nodes
(default 500) so huge graphs stay legible; pass --top-N 0 to disable the cap.

The graph is loaded from the indexed store for the resolved group and ref;
run 'archigraph index' first if no graph exists.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format := args[0]
			switch format {
			case "graphml", "cypher", "svg", "html":
			default:
				return fmt.Errorf("export: unknown format %q (want: graphml | cypher | svg | html)", format)
			}

			// Resolve ref the same way as the other read-only commands.
			resolvedRef, _, err := resolveRef(refFlag, false /* @all not meaningful here */)
			if err != nil {
				return err
			}

			doc, err := loadGroupGraphForExport(cmd, group, resolvedRef)
			if err != nil {
				return err
			}
			// Canonicalize node/edge order so output is reproducible regardless
			// of per-repo load order or graph.fb iteration order.
			sortDocumentForExport(doc)

			// Resolve destination writer.
			out := cmd.OutOrStdout()
			w := out
			var f *os.File
			if outPath != "" && outPath != "-" {
				f, err = os.Create(outPath)
				if err != nil {
					return fmt.Errorf("export: create %s: %w", outPath, err)
				}
				defer f.Close()
				w = f
			}

			var werr error
			switch format {
			case "graphml":
				werr = export.WriteGraphML(w, doc)
			case "cypher":
				werr = export.WriteCypher(w, doc)
			case "svg":
				werr = export.WriteSVG(w, doc, topN)
			case "html":
				werr = export.WriteHTML(w, doc, topN)
			}
			if werr != nil {
				return fmt.Errorf("export: write %s: %w", format, werr)
			}

			if outPath != "" && outPath != "-" {
				fmt.Fprintf(out, "✓ exported %d entities, %d relationships to %s (%s)\n",
					len(doc.Entities), len(doc.Relationships), outPath, format)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&group, "group", "", "group to export (default: infer from current directory)")
	cmd.Flags().StringVar(&refFlag, "ref", "", refFlagUsage)
	cmd.Flags().StringVar(&outPath, "out", "", "output file (default: stdout; '-' is stdout)")
	cmd.Flags().IntVar(&topN, "top-N", export.DefaultTopN, "for svg/html: cap to the N highest-degree nodes (0 = no cap)")
	return cmd
}

// sortDocumentForExport sorts a merged document into a canonical, stable order
// so every export format produces byte-identical output for the same input.
// Entities are sorted by id; relationships by (from, to, kind). It mutates doc
// in place. This mirrors cmd/archigraph's sortDocumentForEmission but lives in
// the cli package to keep the export command self-contained.
func sortDocumentForExport(doc *graph.Document) {
	sort.SliceStable(doc.Entities, func(i, j int) bool {
		return doc.Entities[i].ID < doc.Entities[j].ID
	})
	sort.SliceStable(doc.Relationships, func(i, j int) bool {
		a, b := &doc.Relationships[i], &doc.Relationships[j]
		if a.FromID != b.FromID {
			return a.FromID < b.FromID
		}
		if a.ToID != b.ToID {
			return a.ToID < b.ToID
		}
		return a.Kind < b.Kind
	})
}

// loadGroupGraphForExport resolves the group, loads every repo's indexed graph
// for the given ref and merges them into a single graph.Document. It reuses the
// canonical load path (StateDir → graph.LoadGraphFromDir); repos with no graph
// on disk are skipped with a warning rather than failing the whole export.
//
// ref == "" means the current HEAD ref (StateDirForRepo); a named ref uses
// StateDirForRepoRef.
func loadGroupGraphForExport(cmd *cobra.Command, group, ref string) (*graph.Document, error) {
	w := cmd.OutOrStderr()

	if group == "" {
		resolved, err := inferGroupFromCWD()
		if err != nil {
			return nil, fmt.Errorf("export: could not infer group from current directory: %w\nUse --group <name> to specify a group explicitly", err)
		}
		group = resolved
	}

	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		return nil, fmt.Errorf("export: group %q not found: %w", group, err)
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("export: load group config: %w", err)
	}

	merged := &graph.Document{Repo: group}
	loadedRepos := 0
	for _, repo := range cfg.Repos {
		if repo.Path == "" {
			continue
		}
		var stateDir string
		if ref == "" {
			stateDir = daemon.StateDirForRepo(repo.Path)
		} else {
			stateDir = daemon.StateDirForRepoRef(repo.Path, ref)
		}
		doc, err := graph.LoadGraphFromDir(stateDir)
		if err != nil {
			fmt.Fprintf(w, "warning: skipping repo %s (graph not found: %v)\n", repo.Slug, err)
			continue
		}
		merged.Entities = append(merged.Entities, doc.Entities...)
		merged.Relationships = append(merged.Relationships, doc.Relationships...)
		loadedRepos++
	}

	if loadedRepos == 0 {
		return nil, fmt.Errorf("export: no indexed graphs found for group %q — run `archigraph index` first", group)
	}

	merged.Stats = graph.Stats{
		Entities:      len(merged.Entities),
		Relationships: len(merged.Relationships),
	}
	return merged, nil
}
