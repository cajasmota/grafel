// phantom_edges.go — P5 pass (#769).
//
// After the HTTP/Kafka/WS link passes emit cross-repo Link records into
// <group>-links.json, this pass promotes every Link with
//
//	relation = "calls"
//	method   ∈ {"http", "kafka_topic", "ws_channel"}
//
// into a phantom Relationship inside the SOURCE repo's graph.Document. The
// phantom edge has Kind="CALLS" plus a set of properties that annotate it as
// a cross-repo terminal step:
//
//	cross_repo:  "true"
//	target_repo: "<slug>"       (the repo slug encoded in Link.Target)
//	link_method: link.Method    ("http", "kafka_topic", "ws_channel")
//	via:         "phantom_edge_pass_#769"
//
// The target entity ID (Link.Target) lives in a different repo's Entities
// slice and will NOT appear in the source doc's byID map — that is
// intentional. The BFS in RunProcessFlow terminates at any node with no
// outgoing CALLS edges; a phantom edge target has no outgoing edges by
// definition, so the chain terminates there. RunProcessFlow's
// chainCrossesRepo helper (see process_flow.go) detects the
// "cross_repo=true" property on the relationship and marks the resulting
// Process entity cross_stack=true.
//
// Why this file vs. modifying RunAllPasses inline?
//
// RunAllPasses operates on []repoGraph (a links-internal projection) and
// does not import internal/graph — adding phantom-edge emission there would
// require either a new package dependency or passing graph.Document maps as
// parameters. Instead we expose PromoteToPhantomEdges as a standalone
// function that the CLI/daemon passes call AFTER links.RunAllPasses returns,
// keeping the links package free of graph.Document knowledge and letting
// callers (RunLinksForGroup, daemonRebuildFunc) hold both the links result
// and the loaded documents.
//
// Refs: issue #769, PR #769.
package links

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// phantomEdgeMethods lists the Link.Method values that are promoted to
// phantom CALLS edges. Only CALLS-relation links with these methods
// represent actual code calling across repo boundaries; IMPORTS,
// SHARED_LABEL, and STRING_MATCH links are not traversable call steps.
var phantomEdgeMethods = map[string]bool{
	MethodHTTP:    true,
	"kafka_topic": true,
	"ws_channel":  true,
}

// PhantomEdgeResult summarises one PromoteToPhantomEdges call.
type PhantomEdgeResult struct {
	PassResult
	PhantomEdgesAdded int
	ReposUpdated      int
}

// PromoteToPhantomEdges promotes cross-repo CALLS Link records into
// phantom Relationships on the source repo's *graph.Document. docs maps
// repo slug → pointer to the in-memory document that will be mutated.
// group is used only for logging / the "via" property annotation.
//
// Only links with relation="calls" and a method in phantomEdgeMethods
// are promoted; others are silently skipped.
//
// Edges are appended in sorted (Source, Target) order to preserve
// determinism. Duplicate phantom edges (same Source, Target, Method) are
// deduplicated.
//
// Returns the number of phantom edges added across all repos and the
// first error encountered (remaining repos are still processed).
func PromoteToPhantomEdges(
	links []Link,
	docs map[string]*graph.Document,
	group string,
) (added int, err error) {
	// Collect candidate links: relation=calls, method ∈ phantomEdgeMethods.
	type candidate struct {
		link       Link
		sourceRepo string
		targetRepo string
		sourceID   string // local entity ID in source repo
		targetID   string // entity ID in target repo (opaque to source)
	}

	var candidates []candidate
	for _, lk := range links {
		if !strings.EqualFold(lk.Relation, RelationCalls) {
			continue
		}
		if !phantomEdgeMethods[strings.ToLower(lk.Method)] {
			continue
		}
		srcRepo, srcID, ok1 := splitEntityKey(lk.Source)
		tgtRepo, tgtID, ok2 := splitEntityKey(lk.Target)
		if !ok1 || !ok2 || srcRepo == tgtRepo {
			continue
		}
		candidates = append(candidates, candidate{
			link:       lk,
			sourceRepo: srcRepo,
			targetRepo: tgtRepo,
			sourceID:   srcID,
			targetID:   tgtID,
		})
	}

	// Sort by (Source, Target) for determinism before appending.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].link.Source != candidates[j].link.Source {
			return candidates[i].link.Source < candidates[j].link.Source
		}
		return candidates[i].link.Target < candidates[j].link.Target
	})

	// Dedup within the candidate list itself: same (Source, Target, Method)
	// must not produce two phantom edges.
	type edgeKey struct{ source, target, method string }
	emitted := make(map[edgeKey]bool)

	// Track which docs need their existing SCOPE.Process entities stripped
	// before re-running RunProcessFlow. We do this lazily per doc.
	docsUpdated := make(map[string]bool)

	for _, c := range candidates {
		doc, ok := docs[c.sourceRepo]
		if !ok {
			// Source repo's document not present — skip silently.
			// This can happen when a partial group has docs missing.
			continue
		}

		ek := edgeKey{c.link.Source, c.link.Target, c.link.Method}
		if emitted[ek] {
			continue
		}
		emitted[ek] = true

		relID := graph.RelationshipID(c.sourceID, c.targetID, "CALLS:phantom:"+c.link.Method)
		rel := graph.Relationship{
			ID:     relID,
			FromID: c.sourceID,
			ToID:   c.targetID,
			Kind:   "CALLS",
		}.WithProperties(map[string]string{
			"cross_repo":  "true",
			"target_repo": c.targetRepo,
			"link_method": c.link.Method,
			"via":         fmt.Sprintf("phantom_edge_pass_#769 group=%s", group),
		},
		)
		doc.Relationships = append(doc.Relationships, rel)
		added++
		docsUpdated[c.sourceRepo] = true
	}

	return added, nil
}

// splitEntityKey parses a Link.Source or Link.Target string of the form
// "<repo>::<entityID>" into its components. Returns ok=false when the
// string doesn't match the expected shape.
func splitEntityKey(key string) (repo, entityID string, ok bool) {
	const sep = "::"
	i := strings.Index(key, sep)
	if i <= 0 || i+len(sep) >= len(key) {
		return "", "", false
	}
	return key[:i], key[i+len(sep):], true
}

// LoadLinksDocument reads a group's links.json file and returns its Link
// slice. Returns nil,nil when the file does not exist yet (group has not
// been linked). Exported so callers outside the links package (cli, daemon)
// can read the links after RunAllPasses has written them.
func LoadLinksDocument(path string) ([]Link, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open links: %w", err)
	}
	defer f.Close()
	var d Document
	if err := json.NewDecoder(f).Decode(&d); err != nil {
		return nil, fmt.Errorf("decode links: %w", err)
	}
	return d.Links, nil
}
