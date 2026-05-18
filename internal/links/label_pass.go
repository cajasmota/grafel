package links

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// lineNumberSuffix matches labels ending in `:<digits>` (e.g.
// `error_handling:try_catch:110`). Line numbers are pure positional
// coincidence across repos and produce noise-only matches in the
// cross-repo label channel — see issue #511.
var lineNumberSuffix = regexp.MustCompile(`:\d+$`)

// labelStopList is the set of generic names that should never produce
// a shared-label match — they are too common across codebases to carry
// any signal.
var labelStopList = map[string]bool{
	"get": true, "set": true, "list": true, "create": true, "update": true, "delete": true,
	"index": true, "view": true, "show": true, "init": true, "main": true, "run": true,
	"process": true, "handle": true, "handler": true, "helper": true, "util": true, "utils": true,
	"config": true, "settings": true, "factory": true, "manager": true, "service": true,
	"module": true, "app": true, "client": true, "server": true, "request": true, "response": true,
	"error": true, "exception": true, "result": true, "data": true, "value": true, "item": true,
	"entry": true, "node": true, "field": true, "model": true, "schema": true, "base": true,
}

// suffixStrip is the ordered list of suffix tokens removed before
// normalisation. Order matters — longer entries first to avoid partial
// matches. Both snake-case (`_viewset`) and CamelCase (`Service`)
// variants are included.
var suffixStrip = []string{
	"_viewset", "_serializer", "_service", "_queries", "_dto", "_interface",
	"viewset", "serializer", "service", "queries", "dto", "interface",
	"Stub", "Service", "Client", "Manager", "Handler",
}

// thresholds for P2.
const (
	labelLinkThreshold      = 0.5
	labelCandidateThreshold = 0.2
	labelEmissionCap        = 6
)

// normalizeLabel returns the canonical lower-cased identifier used for
// cross-repo matching. Empty string means the label was filtered out
// (stop-listed or stripped to nothing).
func normalizeLabel(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return ""
	}
	// Drop line-number-keyed labels (see #511). Any label whose final
	// segment is a bare integer (`foo:bar:110`, `route:42`) is a
	// positional artefact, not a structural identifier, and cross-repo
	// matches on it are pure coincidence.
	if lineNumberSuffix.MatchString(s) {
		return ""
	}
	// Strip suffixes (case-sensitive for the CamelCase variants;
	// lowercase variants apply after we lowercase below). We do two
	// passes: one CamelCase, one lowercase.
	for _, suf := range suffixStrip {
		if isUpperSuffix(suf) {
			if strings.HasSuffix(s, suf) && len(s) > len(suf) {
				s = s[:len(s)-len(suf)]
			}
		}
	}
	s = strings.ToLower(s)
	for _, suf := range suffixStrip {
		ls := strings.ToLower(suf)
		if strings.HasSuffix(s, ls) && len(s) > len(ls) {
			s = s[:len(s)-len(ls)]
		}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if labelStopList[s] {
		return ""
	}
	return s
}

func isUpperSuffix(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// kindCompat returns the kind-compatibility multiplier per spec.
func kindCompat(a, b string) float64 {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la == lb {
		return 1.0
	}
	// Class ↔ interface bridge (cross-stack: Java interface ↔ Python class).
	classy := map[string]bool{"class": true, "struct": true, "type": true}
	ifaceLike := map[string]bool{"interface": true, "trait": true, "protocol": true}
	if (classy[la] && ifaceLike[lb]) || (classy[lb] && ifaceLike[la]) {
		return 0.85
	}
	return 0.5
}

// runLabelPass implements P2.
func runLabelPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "label"}
	if len(graphs) < 2 {
		// Still need to write empty output to keep idempotency clean.
		_, _, err := replaceByMethod(paths.Links, newMethodSet(MethodLabelMatch), nil, rejects)
		if err != nil {
			return res, err
		}
		_, _, err = replaceByMethod(paths.Candidates, newMethodSet(MethodLabelMatch), nil, rejects)
		return res, err
	}

	// Index: normalized → repo → []entityNode (with original index).
	type ent struct {
		repo string
		node entityNode
	}
	byLabel := map[string]map[string][]ent{}

	totalEntities := 0
	for _, g := range graphs {
		for _, e := range g.Entities {
			totalEntities++
			n := normalizeLabel(e.Name)
			if n == "" {
				continue
			}
			if _, ok := byLabel[n]; !ok {
				byLabel[n] = map[string][]ent{}
			}
			byLabel[n][g.Repo] = append(byLabel[n][g.Repo], ent{repo: g.Repo, node: e})
		}
	}
	corpusSize := totalEntities
	if corpusSize < 2 {
		corpusSize = 2
	}

	now := discoveredAt()
	var freshLinks, freshCands []Link

	// Stable iteration order over labels.
	labels := make([]string, 0, len(byLabel))
	for k := range byLabel {
		labels = append(labels, k)
	}
	sort.Strings(labels)

	// seenPair tracks (src,tgt) pairs already emitted by this pass run so
	// that a noisy label set cannot produce duplicate links for the same
	// repo pair. Keyed by ordered "src|tgt" — keeps the loop O(unique
	// pairs) instead of O(labels × repo_pairs).
	seenPair := map[string]bool{}

	for _, label := range labels {
		repos := byLabel[label]
		if len(repos) < 2 {
			continue
		}
		var totalOccur int
		for _, ents := range repos {
			totalOccur += len(ents)
		}
		idf := math.Log(float64(corpusSize+1)/float64(totalOccur+1)) / math.Log(float64(corpusSize+1))
		if idf < 0 {
			idf = 0
		}

		// Pairwise across repos. Stable order.
		repoNames := make([]string, 0, len(repos))
		for r := range repos {
			repoNames = append(repoNames, r)
		}
		sort.Strings(repoNames)

		emitted := 0
		for i := 0; i < len(repoNames) && emitted < labelEmissionCap; i++ {
			for j := i + 1; j < len(repoNames) && emitted < labelEmissionCap; j++ {
				ra, rb := repoNames[i], repoNames[j]
				if ra == rb {
					// Belt-and-suspenders self-pair guard.
					continue
				}
				// Pick best entity per repo: prefer non-stoplisted name length.
				ea := repos[ra][0].node
				eb := repos[rb][0].node
				kc := kindCompat(ea.Kind, eb.Kind)
				raw := idf * kc
				if raw < labelCandidateThreshold {
					continue
				}
				sa := entityKey(ra, ea.ID)
				sb := entityKey(rb, eb.ID)
				src, tgt := orderEndpoints(sa, sb)
				pairKey := src + "|" + tgt
				if seenPair[pairKey] {
					continue
				}
				seenPair[pairKey] = true
				conf := ScoreLabel(raw)
				link := Link{
					ID:           MakeID(src, tgt, MethodLabelMatch),
					Source:       src,
					Target:       tgt,
					Relation:     RelationSharedLabel,
					Method:       MethodLabelMatch,
					Confidence:   conf,
					Channel:      nil,
					Identifier:   strPtr(label),
					DiscoveredAt: now,
				}
				if raw >= labelLinkThreshold {
					freshLinks = append(freshLinks, link)
				} else {
					link.Reason = "label_match below threshold"
					freshCands = append(freshCands, link)
				}
				emitted++
			}
		}
	}

	added, skipped, err := replaceByMethod(paths.Links, newMethodSet(MethodLabelMatch), freshLinks, rejects)
	if err != nil {
		return res, err
	}
	cAdded, cSkipped, err := replaceByMethod(paths.Candidates, newMethodSet(MethodLabelMatch), freshCands, rejects)
	if err != nil {
		return res, err
	}
	res.LinksAdded = added
	res.Candidates = cAdded
	res.Skipped = skipped + cSkipped
	return res, nil
}
