package cli

import (
	"fmt"
	"os"

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
// static interchange file. Two formats are supported in this PR:
//
//	archigraph export graphml [--group g --ref r --out file.graphml]
//	archigraph export cypher  [--group g --ref r --out file.cypher]
//
// The self-contained HTML/SVG export described in #4291 is a deferred
// follow-up.
func newExportCmd() *cobra.Command {
	var group string
	var refFlag string
	var outPath string

	cmd := &cobra.Command{
		Use:   "export <graphml|cypher>",
		Short: "Export the group graph to a static file (GraphML or Cypher)",
		Long: `export serializes a group's indexed code graph to a static file for
offline analysis or visualization.

Formats:
  graphml   GraphML 1.0 XML (<graphml>/<graph>/<node>/<edge>) — opens in
            Gephi, yEd, Cytoscape, etc.
  cypher    Neo4j Cypher CREATE statements — load into a Neo4j database.

The graph is loaded from the indexed store for the resolved group and ref;
run 'archigraph index' first if no graph exists.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format := args[0]
			switch format {
			case "graphml", "cypher":
			default:
				return fmt.Errorf("export: unknown format %q (want: graphml | cypher)", format)
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

			// Resolve destination writer.
			out := cmd.OutOrStdout()
			var w *os.File
			if outPath != "" && outPath != "-" {
				w, err = os.Create(outPath)
				if err != nil {
					return fmt.Errorf("export: create %s: %w", outPath, err)
				}
				defer w.Close()
			}

			var werr error
			switch format {
			case "graphml":
				if w != nil {
					werr = export.WriteGraphML(w, doc)
				} else {
					werr = export.WriteGraphML(out, doc)
				}
			case "cypher":
				if w != nil {
					werr = export.WriteCypher(w, doc)
				} else {
					werr = export.WriteCypher(out, doc)
				}
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
	return cmd
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
