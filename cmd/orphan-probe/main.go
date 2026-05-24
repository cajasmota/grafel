// cmd/orphan-probe — cross-group orphan inventory probe.
//
// Loads every repo in each installed group (from registry.json), builds
// inbound + outbound degree per entity, identifies degree-0 orphans, and
// clusters them by (Kind, SourceFile-pattern). Output is a rich text report
// suitable for copy-paste into a GitHub issue.
//
// Usage:
//
//	go run ./cmd/orphan-probe [-groups upvate,polyglot-platform,...] [-top 20] [-json]
//
// Flags:
//
//	-groups  comma-separated list of group names to probe (default: all registered)
//	-top     top-N clusters to show per group (default: 20)
//	-json    emit machine-readable JSON instead of text report
//
// The probe is READ-ONLY and never mutates the graph.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// structuralRelKinds mirrors audit.go — containment edges are excluded from
// connectivity so a node linked only by CONTAINS/DECLARES stays an orphan.
var structuralRelKinds = map[string]bool{
	"CONTAINS": true,
	"DECLARES": true,
}

// Cluster groups orphan entities sharing the same (Kind, SourceFile pattern).
type Cluster struct {
	Kind            string   `json:"kind"`
	SrcPattern      string   `json:"src_pattern"`
	Count           int      `json:"count"`
	RepresentativeIDs []string `json:"representative_ids"`
}

// RepoSummary is per-repo aggregation inside a group.
type RepoSummary struct {
	Slug     string `json:"slug"`
	RepoPath string `json:"repo_path"`
	Entities int    `json:"entities"`
	Orphans  int    `json:"orphans"`
	Rate     float64 `json:"orphan_rate"`
}

// GroupResult holds the full output for one group.
type GroupResult struct {
	Group    string        `json:"group"`
	Entities int           `json:"entities"`
	Orphans  int           `json:"orphans"`
	Rate     float64       `json:"orphan_rate"`
	Repos    []RepoSummary `json:"repos"`
	Clusters []Cluster     `json:"top_clusters"`
	Errors   []string      `json:"errors,omitempty"`
}

// ProbeReport is the top-level output structure.
type ProbeReport struct {
	Groups          []GroupResult    `json:"groups"`
	CrossGroupPatterns []CrossGroupPattern `json:"cross_group_patterns"`
}

// CrossGroupPattern is a cluster pattern appearing in more than one group.
type CrossGroupPattern struct {
	Kind       string   `json:"kind"`
	SrcPattern string   `json:"src_pattern"`
	Groups     []string `json:"groups"`
	TotalCount int      `json:"total_count"`
}

func main() {
	groupsFlag := flag.String("groups", "", "comma-separated group names (default: all)")
	topN := flag.Int("top", 20, "top-N clusters per group")
	emitJSON := flag.Bool("json", false, "emit JSON instead of text report")
	flag.Parse()

	// Load registry.
	reg, err := registry.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load registry: %v\n", err)
		os.Exit(1)
	}

	// Filter groups.
	wantGroups := map[string]bool{}
	if *groupsFlag != "" {
		for _, g := range strings.Split(*groupsFlag, ",") {
			wantGroups[strings.TrimSpace(g)] = true
		}
	}

	var results []GroupResult
	for _, ref := range reg.Groups {
		if len(wantGroups) > 0 && !wantGroups[ref.Name] {
			continue
		}
		cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
		if err != nil {
			results = append(results, GroupResult{
				Group:  ref.Name,
				Errors: []string{fmt.Sprintf("load config: %v", err)},
			})
			continue
		}
		gr := probeGroup(cfg, *topN)
		gr.Group = ref.Name
		results = append(results, gr)
	}

	// Build cross-group patterns.
	cross := buildCrossGroupPatterns(results)

	report := ProbeReport{
		Groups:             results,
		CrossGroupPatterns: cross,
	}

	if *emitJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(report)
		return
	}

	printTextReport(report)
}

// probeGroup runs the orphan probe across all repos in a group config.
func probeGroup(cfg *registry.GroupConfig, topN int) GroupResult {
	gr := GroupResult{
		Clusters: []Cluster{},
	}

	// Aggregate orphan entity list across all repos for clustering.
	type orphanEntry struct {
		id      string
		kind    string
		srcFile string
		repoSlug string
	}
	var allOrphans []orphanEntry

	for _, repo := range cfg.Repos {
		stateDir := daemon.StateDirForRepo(repo.Path)
		doc, err := graph.LoadGraphFromDir(stateDir)
		if err != nil {
			gr.Errors = append(gr.Errors, fmt.Sprintf("%s: %v", repo.Slug, err))
			continue
		}

		nEnts := len(doc.Entities)
		gr.Entities += nEnts

		// Build degree sets: any non-structural edge touching an entity
		// makes it "connected".
		connected := make(map[string]struct{}, nEnts)
		for i := range doc.Relationships {
			r := &doc.Relationships[i]
			k := strings.ToUpper(r.Kind)
			if structuralRelKinds[k] {
				continue
			}
			connected[r.FromID] = struct{}{}
			connected[r.ToID] = struct{}{}
		}

		nOrphans := 0
		for i := range doc.Entities {
			e := &doc.Entities[i]
			if _, ok := connected[e.ID]; ok {
				continue
			}
			// Exclude synthetic empty-SourceFile Module entities from orphan count.
			// These are structural grouping nodes created by the module-grouping pass
			// and don't represent real code constructs (Issue #2064).
			if e.Kind == "Module" && e.SourceFile == "" {
				continue
			}
			nOrphans++
			allOrphans = append(allOrphans, orphanEntry{
				id:       e.ID,
				kind:     kindKey(e),
				srcFile:  srcPattern(e.SourceFile),
				repoSlug: repo.Slug,
			})
		}
		gr.Orphans += nOrphans

		rate := 0.0
		if nEnts > 0 {
			rate = float64(nOrphans) / float64(nEnts)
		}
		gr.Repos = append(gr.Repos, RepoSummary{
			Slug:     repo.Slug,
			RepoPath: repo.Path,
			Entities: nEnts,
			Orphans:  nOrphans,
			Rate:     rate,
		})
	}

	if gr.Entities > 0 {
		gr.Rate = float64(gr.Orphans) / float64(gr.Entities)
	}

	// Cluster orphans by (kind, srcPattern).
	type clusterKey struct {
		kind string
		src  string
	}
	clusterCounts := map[clusterKey]int{}
	clusterReps := map[clusterKey][]string{}
	for _, o := range allOrphans {
		k := clusterKey{o.kind, o.srcFile}
		clusterCounts[k]++
		if len(clusterReps[k]) < 5 {
			clusterReps[k] = append(clusterReps[k], o.id)
		}
	}

	// Sort by count desc.
	type clusterEntry struct {
		key   clusterKey
		count int
	}
	var entries []clusterEntry
	for k, c := range clusterCounts {
		entries = append(entries, clusterEntry{k, c})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		if entries[i].key.kind != entries[j].key.kind {
			return entries[i].key.kind < entries[j].key.kind
		}
		return entries[i].key.src < entries[j].key.src
	})

	n := topN
	if n > len(entries) {
		n = len(entries)
	}
	for _, e := range entries[:n] {
		gr.Clusters = append(gr.Clusters, Cluster{
			Kind:            e.key.kind,
			SrcPattern:      e.key.src,
			Count:           e.count,
			RepresentativeIDs: clusterReps[e.key],
		})
	}

	return gr
}

// kindKey returns "Kind/Subtype" (or just "Kind") for an entity.
func kindKey(e *graph.Entity) string {
	if e.Subtype != "" {
		return e.Kind + "/" + e.Subtype
	}
	return e.Kind
}

// srcPattern collapses a SourceFile path into a human-readable pattern
// to cluster entities from the same "type" of location.
//
// Rules (applied in order):
//  1. ext:<module> prefixes → kept as "ext:<module>.*"
//  2. synthetic:/ prefix → "synthetic:/*"
//  3. Specific well-known synthetic SourceFile labels → kept verbatim
//  4. Actual file paths → reduce to "<dir-stem>/<base>" with extension normalised
func srcPattern(src string) string {
	if src == "" {
		return "(empty)"
	}
	// ext: placeholder
	if strings.HasPrefix(src, "ext:") {
		parts := strings.SplitN(src, ":", 3)
		if len(parts) >= 2 {
			mod := parts[1]
			return "ext:" + mod + ":*"
		}
		return "ext:*"
	}
	// synthetic: or manifest: prefixes
	for _, p := range []string{"synthetic:", "manifest:", "hierarchy:", "openapi:", "framework:"} {
		if strings.HasPrefix(src, p) {
			parts := strings.SplitN(src, "/", 3)
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1] + "/..."
			}
			return src
		}
	}
	// Panache synthetic stub (post-#2059)
	if strings.HasPrefix(src, "panache-synthetic:") {
		return "panache-synthetic:*"
	}

	// Real file path: keep parent dir basename + file basename.
	// Drop line annotations like "file.py:42".
	clean := strings.SplitN(src, ":", 2)[0]
	dir := filepath.Dir(clean)
	base := filepath.Base(clean)

	// Normalise extension to language bucket.
	ext := strings.ToLower(filepath.Ext(base))
	langBucket := extToLang(ext)

	// Use only the last two path components for brevity.
	dirBase := filepath.Base(dir)
	if dirBase == "." || dirBase == "" {
		return langBucket + ":" + base
	}
	return langBucket + ":" + dirBase + "/" + base
}

var reDigits = regexp.MustCompile(`\d+`)

// extToLang maps file extension to a language label for grouping.
func extToLang(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "ts"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "js"
	case ".py":
		return "py"
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".rb":
		return "ruby"
	case ".rs":
		return "rust"
	case ".cs":
		return "csharp"
	case ".cpp", ".cc", ".cxx", ".h", ".hpp":
		return "cpp"
	case ".json", ".yaml", ".yml", ".toml", ".xml":
		return "config"
	case ".tf":
		return "terraform"
	case ".graphql", ".gql":
		return "graphql"
	default:
		return "other"
	}
}

// buildCrossGroupPatterns identifies (Kind, SrcPattern) clusters appearing in
// more than one group.
func buildCrossGroupPatterns(groups []GroupResult) []CrossGroupPattern {
	type key struct{ kind, src string }
	groupsForPattern := map[key][]string{}
	countForPattern := map[key]int{}

	for _, gr := range groups {
		seen := map[key]bool{}
		for _, c := range gr.Clusters {
			k := key{c.Kind, c.SrcPattern}
			if !seen[k] {
				groupsForPattern[k] = append(groupsForPattern[k], gr.Group)
				seen[k] = true
			}
			countForPattern[k] += c.Count
		}
	}

	var out []CrossGroupPattern
	for k, gs := range groupsForPattern {
		if len(gs) < 2 {
			continue
		}
		out = append(out, CrossGroupPattern{
			Kind:       k.kind,
			SrcPattern: k.src,
			Groups:     gs,
			TotalCount: countForPattern[k],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalCount != out[j].TotalCount {
			return out[i].TotalCount > out[j].TotalCount
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// pct formats a float as a percentage string.
func pct(f float64) string {
	return fmt.Sprintf("%.1f%%", f*100)
}

// bar prints a simple ASCII bar proportional to rate (max 20 chars).
func bar(rate float64) string {
	n := int(math.Round(rate * 20))
	if n > 20 {
		n = 20
	}
	return strings.Repeat("█", n) + strings.Repeat("░", 20-n)
}

func printTextReport(report ProbeReport) {
	fmt.Println("# Orphan Inventory Probe Report")
	fmt.Println()
	fmt.Println("Orphan definition: entity with degree-0 after excluding CONTAINS/DECLARES edges.")
	fmt.Println()

	// Per-group sections.
	for _, gr := range report.Groups {
		fmt.Printf("## Group: %s\n\n", gr.Group)

		if len(gr.Errors) > 0 {
			fmt.Println("**Errors loading repos:**")
			for _, e := range gr.Errors {
				fmt.Printf("  - %s\n", e)
			}
			fmt.Println()
		}

		fmt.Printf("| Metric | Value |\n|--------|-------|\n")
		fmt.Printf("| Total entities | %d |\n", gr.Entities)
		fmt.Printf("| Total orphans  | %d |\n", gr.Orphans)
		fmt.Printf("| Orphan %%       | %s |\n", pct(gr.Rate))
		fmt.Println()

		// Per-repo table.
		if len(gr.Repos) > 0 {
			fmt.Println("### Per-repo breakdown\n")
			fmt.Printf("| Repo | Entities | Orphans | Rate | Bar |\n")
			fmt.Printf("|------|----------|---------|------|-----|\n")
			for _, r := range gr.Repos {
				slug := r.Slug
				if len(slug) > 40 {
					slug = slug[:37] + "..."
				}
				fmt.Printf("| %-40s | %8d | %7d | %5s | %s |\n",
					slug, r.Entities, r.Orphans, pct(r.Rate), bar(r.Rate))
			}
			fmt.Println()
		}

		// Top clusters.
		if len(gr.Clusters) > 0 {
			fmt.Println("### Top orphan clusters\n")
			fmt.Printf("| # | Kind | SourceFile pattern | Count | Rep IDs |\n")
			fmt.Printf("|---|------|--------------------|-------|---------|\n")
			for i, c := range gr.Clusters {
				repIDs := strings.Join(c.RepresentativeIDs, " ")
				if len(repIDs) > 60 {
					repIDs = repIDs[:57] + "..."
				}
				fmt.Printf("| %2d | %-35s | %-40s | %5d | %s |\n",
					i+1, c.Kind, c.SrcPattern, c.Count, repIDs)
			}
			fmt.Println()
		} else {
			fmt.Println("_No orphan clusters found._\n")
		}
	}

	// Cross-group patterns.
	fmt.Println("## Cross-group patterns\n")
	if len(report.CrossGroupPatterns) == 0 {
		fmt.Println("_No patterns appear in more than one group._\n")
	} else {
		fmt.Printf("| Kind | SourceFile pattern | Groups | Total count |\n")
		fmt.Printf("|------|--------------------|--------|-------------|\n")
		for _, p := range report.CrossGroupPatterns {
			groups := strings.Join(p.Groups, ", ")
			fmt.Printf("| %-35s | %-40s | %-30s | %5d |\n",
				p.Kind, p.SrcPattern, groups, p.TotalCount)
		}
		fmt.Println()
	}
}

// suppress unused warning for reDigits (used for possible future deduplication).
var _ = reDigits
