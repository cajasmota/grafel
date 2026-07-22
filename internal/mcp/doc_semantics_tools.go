package mcp

// grafel_apply_doc_semantics — MCP submit/apply tool for Layer-2 (agent-
// driven semantic) doc ingestion (#4309, epic #4294).
//
// This is the apply-path companion to the emit step (internal/ingest:
// EmitSemanticBundles / WriteBundle). The agent-driven loop is:
//
//  1. (emit)  the indexer/CLI writes per-document SemanticBundle artifacts under
//             <stateDir>/doc-semantics/<documentID>.bundle.json.
//  2. (fill)  the EXTERNAL calling agent reads each bundle, runs its OWN LLM to
//             classify sections, and writes <documentID>.result.json back beside
//             the bundle. grafel makes NO LLM call.
//  3. (apply) THIS tool reads each (bundle, result) pair, validates + applies via
//             ingest.ApplySemanticResult, and writes the produced
//             SCOPE.DesignDecision nodes + CONTAINS/RATIONALE_FOR edges to
//             <stateDir>/doc-semantics/applied-nodes.json (the splice sidecar,
//             mirroring enrichment-resolutions.json). dry_run validates without
//             writing.
//
// Mirrors grafel_apply_docgen_repairs (docgen_repair_tools.go): per-repo
// summary, repo_filter + dry_run params, deterministic ordering, honest partial.
//
// IDEMPOTENCY: ApplySemanticResult derives node/edge IDs deterministically, and
// the sidecar is rewritten (not appended) deduped-by-ID on every apply, so
// re-running over the same artifacts never duplicates.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/ingest"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// docSemanticsSubdir is the per-repo state-dir subdirectory holding the L2
// emit/apply artifacts.
const docSemanticsSubdir = "doc-semantics"

// docSemanticsAppliedFile is the splice sidecar written by a successful apply.
const docSemanticsAppliedFile = "applied-nodes.json"

// docSemanticsSidecar is the on-disk shape of the applied-nodes sidecar: the
// validated semantic nodes/edges ready to splice into the graph at load time.
type docSemanticsSidecar struct {
	Entities      []graph.Entity       `json:"entities"`
	Relationships []graph.Relationship `json:"relationships"`
}

// handleApplyDocSemantics implements grafel_apply_doc_semantics.
//
// Parameters (all optional):
//
//	repo_filter []string — restrict to named repos; empty/["*"] = all
//	dry_run bool — validate + report, but do not write the sidecar
func (s *Server) handleApplyDocSemantics(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}

	dryRun := argBool(req, "dry_run", false)
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	sort.Slice(repos, func(i, j int) bool { return repos[i].Repo < repos[j].Repo })

	type repoResult struct {
		Repo               string   `json:"repo"`
		BundlesProcessed   int      `json:"bundles_processed"`
		DecisionsCreated   int      `json:"decisions_created"`
		RationaleEdges     int      `json:"rationale_edges"`
		AnchorEdges        int      `json:"anchor_edges"`
		SectionsClassified int      `json:"sections_classified"`
		SectionsNone       int      `json:"sections_none"`
		RejectedTargets    int      `json:"rejected_targets"`
		Errors             []string `json:"errors,omitempty"`
		SidecarPath        string   `json:"sidecar_path,omitempty"`
	}

	results := make([]repoResult, 0, len(repos))
	totalDecisions, totalEdges := 0, 0

	for _, r := range repos {
		res := repoResult{Repo: r.Repo}
		dir := filepath.Join(daemon.StateDirForRepo(r.Path), docSemanticsSubdir)

		pairs, listErr := listBundleResultPairs(dir)
		if listErr != nil {
			res.Errors = append(res.Errors, listErr.Error())
			results = append(results, res)
			continue
		}

		// Current code entities for target-existence validation.
		// #5870 PR7a: Reader-served materialize seam instead of raw r.Doc.Entities.
		var codeEntities []graph.Entity
		if r.Doc != nil {
			codeEntities = materializeAllEntities(r)
		}

		var sidecar docSemanticsSidecar
		entSeen := map[string]bool{}
		relSeen := map[string]bool{}

		for _, p := range pairs {
			bundle, bErr := ingest.ReadBundle(p.bundlePath)
			if bErr != nil {
				res.Errors = append(res.Errors, bErr.Error())
				continue
			}
			result, rErr := ingest.ReadResult(p.resultPath)
			if rErr != nil {
				res.Errors = append(res.Errors, rErr.Error())
				continue
			}
			applied, aErr := ingest.ApplySemanticResult(bundle, result, codeEntities)
			if aErr != nil {
				// Envelope-level rejection: report, skip this pair, no corruption.
				res.Errors = append(res.Errors, aErr.Error())
				continue
			}
			res.BundlesProcessed++
			res.DecisionsCreated += applied.Stats.DecisionsCreated
			res.RationaleEdges += applied.Stats.RationaleEdges
			res.AnchorEdges += applied.Stats.AnchorEdges
			res.SectionsClassified += applied.Stats.SectionsClassified
			res.SectionsNone += applied.Stats.SectionsNone
			res.RejectedTargets += applied.Stats.RejectedTargets

			// Idempotent merge: dedup by ID across all bundles in the repo.
			for _, e := range applied.Entities {
				if entSeen[e.ID] {
					continue
				}
				entSeen[e.ID] = true
				sidecar.Entities = append(sidecar.Entities, e)
			}
			for _, rel := range applied.Relationships {
				if relSeen[rel.ID] {
					continue
				}
				relSeen[rel.ID] = true
				sidecar.Relationships = append(sidecar.Relationships, rel)
			}
		}

		// Deterministic sidecar ordering.
		sort.SliceStable(sidecar.Entities, func(i, j int) bool { return sidecar.Entities[i].ID < sidecar.Entities[j].ID })
		sort.SliceStable(sidecar.Relationships, func(i, j int) bool { return sidecar.Relationships[i].ID < sidecar.Relationships[j].ID })

		if !dryRun && (len(sidecar.Entities) > 0 || len(sidecar.Relationships) > 0) {
			path, wErr := writeDocSemanticsSidecar(dir, sidecar)
			if wErr != nil {
				res.Errors = append(res.Errors, wErr.Error())
			} else {
				res.SidecarPath = path
			}
		}

		totalDecisions += res.DecisionsCreated
		totalEdges += res.RationaleEdges
		results = append(results, res)
	}

	return jsonResult(map[string]any{
		"dry_run":          dryRun,
		"repos":            results,
		"total_decisions":  totalDecisions,
		"total_rationale":  totalEdges,
		"artifacts_subdir": docSemanticsSubdir,
	}), nil
}

// bundleResultPair locates a bundle and its sibling result file.
type bundleResultPair struct {
	bundlePath string
	resultPath string
}

// listBundleResultPairs scans dir for *.bundle.json files that have a matching
// *.result.json sibling (same <documentID> stem). Bundles without a result are
// silently skipped (the agent has not filled them yet). Returns a deterministic,
// stem-sorted list. A missing dir is not an error (no artifacts yet).
func listBundleResultPairs(dir string) ([]bundleResultPair, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read doc-semantics dir %q: %w", dir, err)
	}
	have := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			have[e.Name()] = true
		}
	}
	var out []bundleResultPair
	for name := range have {
		if !strings.HasSuffix(name, ".bundle.json") {
			continue
		}
		stem := strings.TrimSuffix(name, ".bundle.json")
		resultName := stem + ".result.json"
		if !have[resultName] {
			continue
		}
		out = append(out, bundleResultPair{
			bundlePath: filepath.Join(dir, name),
			resultPath: filepath.Join(dir, resultName),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].bundlePath < out[j].bundlePath })
	return out, nil
}

// writeDocSemanticsSidecar writes the applied semantic nodes/edges sidecar as
// indented JSON. The whole file is rewritten (not appended) so a re-apply is
// idempotent. Returns the written path.
func writeDocSemanticsSidecar(dir string, sc docSemanticsSidecar) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create doc-semantics dir %q: %w", dir, err)
	}
	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal doc-semantics sidecar: %w", err)
	}
	path := filepath.Join(dir, docSemanticsAppliedFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write doc-semantics sidecar %q: %w", path, err)
	}
	return path, nil
}
