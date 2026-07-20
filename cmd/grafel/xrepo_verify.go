package main

// xrepo_verify.go — isolated verification harness for issue #1409.
//
// Indexes a set of repos with THIS binary into a temp graphs dir, runs the
// cross-repo link passes, and reports HTTP route↔fetch link counts plus a
// framework endpoint/call extraction coverage table. Never touches the live
// daemon or ~/.grafel state (uses an isolated temp GRAFEL_HOME).
//
// Not part of the public command surface; invoked only during verification:
//
//	grafel xrepo-verify <group> <slug>=<path> [<slug>=<path> ...]
//
// Extended in #1445 to also accept --diagnose which dumps sample orphan
// consumer call → nearest producer-endpoint pairs for gap analysis.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/links"
)

// pathParamDiagRe normalises path-param placeholders to {*} for diagnostic
// output only — mirrors the normalisation done inside http_pass.go.
var pathParamDiagRe = regexp.MustCompile(`\{[^}]+\}|:[a-zA-Z][a-zA-Z0-9_]*|<[^>]+>`)

func runXRepoVerify(args []string) int {
	// Check for --diagnose flag.
	diagnose := false
	filtered := args[:0]
	for _, a := range args {
		if a == "--diagnose" {
			diagnose = true
		} else {
			filtered = append(filtered, a)
		}
	}
	args = filtered

	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: grafel xrepo-verify [--diagnose] <group> <slug>=<path> [<slug>=<path> ...]")
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

	if diagnose {
		diagnoseOrphans(tmpGraphs, linkedSources)
	}

	// Service-level SCC detection over the produced links (#1502). Aggregates
	// directed cross-repo links into a service graph and reports cycles.
	reportServiceCycles(linksPath)

	return 0
}

// reportServiceCycles reads the produced links.json, aggregates directed
// cross-service links (calls / publishes_to) into a service graph, runs Tarjan
// SCC, and prints every SCC of size >= 2. Exercises the exact #1502 code path
// on a real fixture without touching the live daemon.
func reportServiceCycles(linksPath string) {
	b, err := os.ReadFile(linksPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read links for SCC:", err)
		return
	}
	var doc struct {
		Links []struct {
			Source   string `json:"source"`
			Target   string `json:"target"`
			Relation string `json:"relation"`
		} `json:"links"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		fmt.Fprintln(os.Stderr, "parse links for SCC:", err)
		return
	}
	sl := make([]graph.ServiceLink, 0, len(doc.Links))
	for _, l := range doc.Links {
		from := repoOf(l.Source)
		to := repoOf(l.Target)
		sl = append(sl, graph.ServiceLink{FromService: from, ToService: to, Relation: l.Relation})
	}
	cycles := graph.FindServiceCycles(sl)
	fmt.Println("\n=== SERVICE-LEVEL SCC / CYCLES (#1502) ===")
	fmt.Printf("service SCCs (size >= 2): %d\n", len(cycles))
	for i, c := range cycles {
		fmt.Printf("[%d] size=%d members=%v\n", i+1, c.Size, c.Members)
		for _, e := range c.Edges {
			fmt.Printf("     %s -> %s  (%v)\n", e.From, e.To, e.Relations)
		}
	}
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
			fw := e.PropGet("framework")
			if fw == "" {
				fw = "(unknown)"
			}
			pt := e.PropGet("pattern_type")
			c := cov[fw]
			switch pt {
			case "http_endpoint_client_synthesis":
				c.calls++
				consTotal++
				// Resolve caller entity ID (source_caller → same-file entity),
				// else fall back to the synthetic's own stamped ID — matching
				// http_pass.go srcID resolution.
				callerID := ""
				if ref := e.PropGet("source_caller"); ref != "" {
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

// normPathDiag mirrors the normalisation in http_pass.go for diagnostic purposes.
func normPathDiag(path string) string {
	path = pathParamDiagRe.ReplaceAllString(path, "{*}")
	path = strings.ToLower(path)
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}
	return path
}

// apiPrefixDiagRe matches the well-known /api[/vN] or /vN prefix (diagnostic).
var apiPrefixDiagRe = regexp.MustCompile(`^/(?:api(?:/v\d+)?|v\d+)(/|$)`)

// stripAPIPrefixDiag removes a leading API/version prefix (diagnostic mirror of http_pass).
func stripAPIPrefixDiag(p string) (string, bool) {
	m := apiPrefixDiagRe.FindStringSubmatchIndex(p)
	if m == nil {
		return "", false
	}
	s := p[m[2]:]
	if s == "" {
		s = "/"
	}
	return s, s != p
}

// orphanRecord holds information about an orphan consumer call.
type orphanRecord struct {
	repo         string
	verb         string
	path         string
	name         string
	framework    string
	urlPrefix    string
	callerEntity string
}

// diagnoseOrphans dumps orphan consumer calls with nearest producer candidates.
// It shows the normalised path of the consumer and the closest producer paths
// so we can see exactly what form of mismatch is causing the miss.
func diagnoseOrphans(graphsDir string, linkedSources map[string]bool) {
	// Collect all producer endpoints keyed by repo.
	type producerEp struct {
		repo      string
		verb      string
		path      string
		normPath  string
		name      string
		urlPrefix string
	}
	var producers []producerEp
	var orphans []orphanRecord

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
			pt := e.PropGet("pattern_type")
			verb := e.PropGet("verb")
			path := e.PropGet("path")
			urlPrefix := e.PropGet("url_prefix")
			framework := e.PropGet("framework")

			if pt == "http_endpoint_client_synthesis" {
				// Consumer side — check if orphan.
				callerID := ""
				callerName := ""
				if ref := e.PropGet("source_caller"); ref != "" {
					if i := strings.IndexByte(ref, ':'); i > 0 {
						kind, name := ref[:i], ref[i+1:]
						callerID = entIDByKey[entKey{kind, name, e.SourceFile}]
						callerName = name
					}
				}
				if callerID == "" {
					callerID = e.ID
					callerName = e.Name
				}
				if !linkedSources[repo+"::"+callerID] {
					orphans = append(orphans, orphanRecord{
						repo:         repo,
						verb:         verb,
						path:         path,
						name:         e.Name,
						framework:    framework,
						urlPrefix:    urlPrefix,
						callerEntity: callerName,
					})
				}
			} else {
				// Producer side.
				producers = append(producers, producerEp{
					repo:      repo,
					verb:      verb,
					path:      path,
					normPath:  normPathDiag(path),
					name:      e.Name,
					urlPrefix: urlPrefix,
				})
			}
		}
		return nil
	})

	// Build byNorm index for producers: normPath → []producerEp
	byNorm := map[string][]producerEp{}
	for _, p := range producers {
		byNorm[p.normPath] = append(byNorm[p.normPath], p)
		if s, ok := stripAPIPrefixDiag(p.normPath); ok {
			byNorm[s] = append(byNorm[s], p)
		}
		if p.urlPrefix != "" && strings.HasPrefix(p.path, p.urlPrefix) {
			stripped := p.path[len(p.urlPrefix):]
			if stripped == "" {
				stripped = "/"
			}
			sn := normPathDiag(stripped)
			if sn != p.normPath {
				byNorm[sn] = append(byNorm[sn], p)
			}
		}
	}

	// Categorise miss reasons.
	type missReason struct {
		reason      string
		consVerb    string
		consPath    string
		prodMatches []string
	}
	reasonCounts := map[string]int{}
	var samples []missReason
	const maxSamples = 40

	for _, o := range orphans {
		normCons := normPathDiag(o.path)
		normStripped, hasStripped := stripAPIPrefixDiag(normCons)

		var matches []producerEp
		seen := map[string]bool{}
		for _, n := range []string{normCons, normStripped} {
			if n == "" {
				continue
			}
			for _, p := range byNorm[n] {
				k := p.repo + "::" + p.name
				if !seen[k] {
					seen[k] = true
					matches = append(matches, p)
				}
			}
		}
		_ = hasStripped

		reason := ""
		var matchDescs []string
		if len(matches) == 0 {
			reason = "NO_PATH_MATCH"
		} else {
			// Path matches exist — check verb.
			consVerbUp := strings.ToUpper(o.verb)
			hasCompatVerb := false
			for _, m := range matches {
				mv := strings.ToUpper(m.verb)
				if mv == consVerbUp || mv == "ANY" || consVerbUp == "ANY" || consVerbUp == "" {
					hasCompatVerb = true
				}
				matchDescs = append(matchDescs, fmt.Sprintf("%s %s:%s", m.repo, m.verb, m.path))
			}
			if hasCompatVerb {
				reason = "PATH_AND_VERB_MATCH_BUT_UNLINKED"
			} else {
				reason = "VERB_MISMATCH"
			}
		}
		reasonCounts[reason]++
		if len(samples) < maxSamples && (reason != "PATH_AND_VERB_MATCH_BUT_UNLINKED" || len(samples) < 10) {
			samples = append(samples, missReason{
				reason:      reason,
				consVerb:    o.verb,
				consPath:    o.path,
				prodMatches: matchDescs,
			})
		}
	}

	fmt.Println("\n=== ORPHAN MISS REASON BREAKDOWN ===")
	reasons := make([]string, 0, len(reasonCounts))
	for r := range reasonCounts {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	for _, r := range reasons {
		fmt.Printf("  %-45s : %d\n", r, reasonCounts[r])
	}

	fmt.Printf("\n=== SAMPLE ORPHAN CALLS (first %d) ===\n", maxSamples)
	for i, s := range samples {
		fmt.Printf("[%d] CONSUMER %s %s (reason=%s)\n", i+1, s.consVerb, s.consPath, s.reason)
		if len(s.prodMatches) == 0 {
			fmt.Println("     -> no producer path matches found")
		} else {
			for _, m := range s.prodMatches {
				fmt.Printf("     -> PRODUCER %s\n", m)
			}
		}
	}

	// --- Phase 2: dump all PATH_AND_VERB_MATCH_BUT_UNLINKED with their
	// consumer properties to understand why the link pass misses them. ---
	fmt.Println("\n=== PATH_AND_VERB_MATCH_BUT_UNLINKED — full detail ===")
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
			pt := e.PropGet("pattern_type")
			if pt != "http_endpoint_client_synthesis" {
				continue
			}
			verb := e.PropGet("verb")
			path := e.PropGet("path")
			framework := e.PropGet("framework")
			urlPrefix := e.PropGet("url_prefix")
			sourceCaller := e.PropGet("source_caller")
			callerID := ""
			if ref := sourceCaller; ref != "" {
				if i := strings.IndexByte(ref, ':'); i > 0 {
					kind, name := ref[:i], ref[i+1:]
					callerID = entIDByKey[entKey{kind, name, e.SourceFile}]
				}
			}
			if callerID == "" {
				callerID = e.ID
			}
			if linkedSources[repo+"::"+callerID] {
				continue // already linked — skip
			}
			// Check if it's a path match but unlinked.
			normCons := normPathDiag(path)
			normStripped, _ := stripAPIPrefixDiag(normCons)
			var matches []producerEp
			seen2 := map[string]bool{}
			for _, n := range []string{normCons, normStripped} {
				if n == "" {
					continue
				}
				for _, prod := range byNorm[n] {
					k := prod.repo + "::" + prod.name
					if !seen2[k] {
						seen2[k] = true
						matches = append(matches, prod)
					}
				}
			}
			if len(matches) == 0 {
				continue
			}
			consVerbUp := strings.ToUpper(verb)
			hasCompatVerb := false
			for _, m := range matches {
				mv := strings.ToUpper(m.verb)
				if mv == consVerbUp || mv == "ANY" || consVerbUp == "ANY" || consVerbUp == "" {
					hasCompatVerb = true
				}
			}
			if !hasCompatVerb {
				continue
			}
			fmt.Printf("ORPHAN consumer: name=%q verb=%q path=%q framework=%q url_prefix=%q source_caller=%q\n",
				e.Name, verb, path, framework, urlPrefix, sourceCaller)
			for _, m := range matches {
				fmt.Printf("  MATCH  producer: name=%q verb=%q path=%q url_prefix=%q\n",
					m.name, m.verb, m.path, m.urlPrefix)
			}
		}
		return nil
	})
}
