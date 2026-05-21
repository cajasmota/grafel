package main

// xrepo_verify.go — isolated verification harness for issue #1409.
//
// Indexes a set of repos with THIS binary into a temp graphs dir, runs the
// cross-repo link passes, and reports HTTP route↔fetch link counts plus a
// framework endpoint/call extraction coverage table. Never touches the live
// daemon or ~/.archigraph state (uses an isolated temp ARCHIGRAPH_HOME).
//
// Not part of the public command surface; invoked only during verification:
//
//	archigraph xrepo-verify <group> <slug>=<path> [<slug>=<path> ...]

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/links"
)

func runXRepoVerify(args []string) int {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: archigraph xrepo-verify <group> <slug>=<path> [<slug>=<path> ...]")
		return 2
	}
	group := args[0]
	repos := args[1:]

	tmpGraphs, err := os.MkdirTemp("", "ag-xrepo-graphs-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mktemp:", err)
		return 1
	}
	tmpHome, err := os.MkdirTemp("", "ag-xrepo-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mktemp:", err)
		return 1
	}
	fmt.Printf("graphs dir: %s\nhome dir:   %s\n", tmpGraphs, tmpHome)

	for _, spec := range repos {
		i := strings.IndexByte(spec, '=')
		if i < 0 {
			fmt.Fprintln(os.Stderr, "bad spec (need slug=path):", spec)
			return 2
		}
		slug := spec[:i]
		path := spec[i+1:]
		out := filepath.Join(tmpGraphs, slug, "graph.json")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "mkdir:", err)
			return 1
		}
		fmt.Printf("indexing %-22s %s\n", slug, path)
		if err := Index(path, out, slug, nil, false, false, WithExportJSON(true)); err != nil {
			fmt.Fprintf(os.Stderr, "index %s: %v\n", slug, err)
			return 1
		}
	}

	res, err := links.RunAllPasses(group, tmpGraphs, tmpHome)
	if err != nil {
		fmt.Fprintln(os.Stderr, "link passes:", err)
		return 1
	}

	// Read back links.json and count HTTP route↔fetch cross-repo links.
	linksPath := res.OutLinks
	httpLinks, repoPairs := countHTTPLinks(linksPath)
	fmt.Println("\n=== CROSS-REPO HTTP ROUTE↔FETCH LINKS ===")
	fmt.Printf("total http-method links: %d\n", httpLinks)
	pairs := make([]string, 0, len(repoPairs))
	for p, n := range repoPairs {
		pairs = append(pairs, fmt.Sprintf("  %s : %d", p, n))
	}
	sort.Strings(pairs)
	for _, p := range pairs {
		fmt.Println(p)
	}

	// Collect the set of link Source endpoints ("repo::entityID") so that
	// orphans = consumer synthetics whose caller/stamped entity never
	// appears as the source of an emitted cross-repo HTTP link.
	linkedSources := linkSourceSet(linksPath)

	// Framework coverage diagnosis + orphan count.
	coverage, prodTotal, consTotal, orphanConsumers := analyzeCoverage(tmpGraphs, linkedSources)
	fmt.Println("\n=== ENDPOINT/CALL EXTRACTION COVERAGE (by framework) ===")
	fmt.Printf("%-22s %12s %12s\n", "framework", "endpoints", "calls")
	fwks := make([]string, 0, len(coverage))
	for f := range coverage {
		fwks = append(fwks, f)
	}
	sort.Strings(fwks)
	for _, f := range fwks {
		c := coverage[f]
		fmt.Printf("%-22s %12d %12d\n", f, c.endpoints, c.calls)
	}
	fmt.Printf("\nproducer-side endpoint synthetics (total): %d\n", prodTotal)
	fmt.Printf("consumer-side call synthetics (total):     %d\n", consTotal)
	fmt.Printf("orphan consumer calls (no cross-repo link): %d\n", orphanConsumers)

	return 0
}

type fwkCov struct {
	endpoints int
	calls     int
}

func countHTTPLinks(linksPath string) (int, map[string]int) {
	pairs := map[string]int{}
	b, err := os.ReadFile(linksPath)
	if err != nil {
		return 0, pairs
	}
	var doc struct {
		Links []struct {
			Source string `json:"source"`
			Target string `json:"target"`
			Method string `json:"method"`
		} `json:"links"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return 0, pairs
	}
	n := 0
	for _, l := range doc.Links {
		if l.Method != "http" {
			continue
		}
		n++
		sr := repoOf(l.Source)
		tr := repoOf(l.Target)
		pairs[sr+"→"+tr]++
	}
	return n, pairs
}

// linkSourceSet returns the set of "repo::entityID" source endpoints across
// all method=http links.
func linkSourceSet(linksPath string) map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(linksPath)
	if err != nil {
		return out
	}
	var doc struct {
		Links []struct {
			Source string `json:"source"`
			Method string `json:"method"`
		} `json:"links"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return out
	}
	for _, l := range doc.Links {
		if l.Method == "http" {
			out[l.Source] = true
		}
	}
	return out
}

func repoOf(endpoint string) string {
	if i := strings.Index(endpoint, "::"); i >= 0 {
		return endpoint[:i]
	}
	return endpoint
}

// analyzeCoverage walks each repo's graph.json and counts http_endpoint
// synthetics by framework + side, plus a rough orphan-consumer estimate.
func analyzeCoverage(graphsDir string, linkedSources map[string]bool) (map[string]fwkCov, int, int, int) {
	cov := map[string]fwkCov{}
	prodTotal, consTotal := 0, 0
	orphans := 0

	_ = filepath.WalkDir(graphsDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) != "graph.json" {
			return nil
		}
		doc, err := graph.LoadGraphFromDir(filepath.Dir(p))
		if err != nil {
			return nil
		}
		repo := doc.Repo
		if repo == "" {
			repo = filepath.Base(filepath.Dir(p))
		}
		// Index entities by (kind,name,file) so source_caller refs resolve.
		type entKey struct{ kind, name, file string }
		entIDByKey := map[entKey]string{}
		for _, e := range doc.Entities {
			if isHTTPEndpointKind(e.Kind) {
				continue
			}
			k := entKey{e.Kind, e.Name, e.SourceFile}
			if _, ok := entIDByKey[k]; !ok {
				entIDByKey[k] = e.ID
			}
		}
		for _, e := range doc.Entities {
			if !isHTTPEndpointKind(e.Kind) {
				continue
			}
			fw := e.Properties["framework"]
			if fw == "" {
				fw = "(unknown)"
			}
			pt := e.Properties["pattern_type"]
			c := cov[fw]
			switch pt {
			case "http_endpoint_client_synthesis":
				c.calls++
				consTotal++
				// Resolve caller entity ID (source_caller → same-file entity),
				// else fall back to the synthetic's own stamped ID — matching
				// http_pass.go srcID resolution.
				callerID := ""
				if ref := e.Properties["source_caller"]; ref != "" {
					if i := strings.IndexByte(ref, ':'); i > 0 {
						kind, name := ref[:i], ref[i+1:]
						callerID = entIDByKey[entKey{kind, name, e.SourceFile}]
					}
				}
				if callerID == "" {
					callerID = e.ID
				}
				if !linkedSources[repo+"::"+callerID] {
					orphans++
				}
			default:
				// http_endpoint_synthesis or missing → producer side.
				c.endpoints++
				prodTotal++
			}
			cov[fw] = c
		}
		return nil
	})
	return cov, prodTotal, consTotal, orphans
}

func isHTTPEndpointKind(kind string) bool {
	k := strings.ToLower(kind)
	return strings.Contains(k, "http_endpoint")
}
