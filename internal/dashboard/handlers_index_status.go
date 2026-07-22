package dashboard

// handlers_index_status.go — GET /api/v2/groups/{group}/index-status (#47
// phase 1).
//
// The web dashboard wizard has no visibility into the `enhancing`
// (background enrichment) state or engine CPU/RSS, because internal/dashboard
// never read the status plane (internal/statusfile) — the TUI already reads
// it directly. This is a small, read-only poll endpoint the frontend can hit
// during and after an index job to drive a secondary "enhancing" progress bar
// + CPU/RSS badges, matching the TUI (see internal/statusfile.File.Enhancing
// and the RSSMB/CPUPct doc comment on the same type).
//
// Response shape (wrapped in the standard v2 envelope, see v2_helpers.go):
//
//	{
//	  "ok": true,
//	  "data": {
//	    "engine": { "cpu_pct": <float>, "rss_mb": <int64> },
//	    "repos": [
//	      { "repo_slug": "<slug>", "indexing": <bool>, "enhancing": <bool>,
//	        "entities": <int64>, "relationships": <int64>,
//	        "graph_fb_mtime": <int64-ns>,
//	        "reindex_required": <bool>, "reindex_reason": <string>,
//	        "status": "<idle|indexing|enhancing|reindex-required|reindexing-after-upgrade>" }
//	    ]
//	  }
//	}
//
// repo_slug matches registry.Repo.Slug — the SAME slug progress.Event.RepoSlug
// carries on the SSE /api/index-progress stream (see cmd/grafel/daemon.go's
// indexing job, which stamps RepoSlug: rw.r.Slug), so the frontend can join
// rows from this endpoint against SSE progress rows by slug.
//
// A repo with no status-plane sidecar yet (never indexed by this engine) is
// reported as a zero/false row, not an error — RepoStatusFile's ok=false is
// exactly that "unknown" case. Missing/stale engine-liveness similarly
// degrades to a zero engine block rather than failing the whole response.
// This endpoint is read-only: no writes, no index trigger.
//
// #5907 SURFACING slice: before this change, ReindexRequired/ReindexReason
// (internal/statusfile.File, set by the engine's FIX2 auto-reindex-on-upgrade
// arm — internal/daemon/stale_reindex.go) were dropped when building
// indexStatusRepo, so a repo whose on-disk graph.fb was stamped by an older
// grafel build read as idle to the web dashboard — a silent multi-minute
// stall from the frontend's point of view, even though the engine had
// already loop-guard-enqueued a reindex. Those fields are now surfaced
// verbatim, plus a derived `status` string (see deriveIndexStatus) so the
// frontend can render a "reindexing after upgrade" badge instead of nothing
// at all. This mirrors internal/cli/statusline.go's shell-side
// `⟲ reindex required` rendering of the same statusfile fields.

import (
	"net/http"

	"github.com/cajasmota/grafel/internal/daemon"
)

// indexStatusEngine is the engine-global CPU/RSS block.
type indexStatusEngine struct {
	CPUPct float64 `json:"cpu_pct"`
	RSSMB  int64   `json:"rss_mb"`
}

// indexStatusRepo is one repo's status-plane row.
type indexStatusRepo struct {
	RepoSlug      string `json:"repo_slug"`
	Indexing      bool   `json:"indexing"`
	Enhancing     bool   `json:"enhancing"`
	Entities      int64  `json:"entities"`
	Relationships int64  `json:"relationships"`
	GraphFBMtime  int64  `json:"graph_fb_mtime"`

	// ReindexRequired/ReindexReason mirror internal/statusfile.File's fields
	// of the same name (#5907 FIX1/FIX2): true when the on-disk graph.fb this
	// repo is CURRENTLY SERVING was written by an older grafel build than
	// this engine's fbversion.Version supports. The engine has already
	// loop-guard-enqueued a reindex by the time this is observed true — it is
	// never a "please click something" prompt, only a visibility signal.
	ReindexRequired bool   `json:"reindex_required"`
	ReindexReason   string `json:"reindex_reason,omitempty"`

	// Status is a derived, frontend-friendly single-word summary of the row
	// (see deriveIndexStatus), so the dashboard can render one badge instead
	// of re-deriving the same precedence from four booleans client-side.
	Status string `json:"status"`
}

// deriveIndexStatus collapses a repo's raw statusfile flags into a single
// human-facing status string, in priority order:
//
//  1. "reindexing-after-upgrade" — ReindexRequired AND the auto-enqueued
//     reindex is actively running (Indexing=true). This is the state #5907
//     exists to surface: without it, a post-upgrade reindex reads as a
//     generic "indexing" row indistinguishable from a normal reindex.
//  2. "reindex-required" — ReindexRequired but the auto-enqueued reindex
//     hasn't started running yet (still queued behind other work, or the
//     engine hasn't drained the request yet). Before this slice, THIS was
//     the silent-stall window: the repo looked idle from the dashboard.
//  3. "indexing" — the ordinary extraction-phase state.
//  4. "enhancing" — queryable, background enrichment tail still running.
//  5. "idle" — none of the above.
func deriveIndexStatus(reindexRequired, indexing, enhancing bool) string {
	switch {
	case reindexRequired && indexing:
		return "reindexing-after-upgrade"
	case reindexRequired:
		return "reindex-required"
	case indexing:
		return "indexing"
	case enhancing:
		return "enhancing"
	default:
		return "idle"
	}
}

// indexStatusReply is the payload for GET /api/v2/groups/{group}/index-status.
type indexStatusReply struct {
	Engine indexStatusEngine `json:"engine"`
	Repos  []indexStatusRepo `json:"repos"`
}

// handleV2IndexStatus — GET /api/v2/groups/{group}/index-status. Read-only
// poll surface exposing per-repo indexing/enhancing state (from the
// statusfile status plane) and engine CPU/RSS (from the engine-liveness
// sidecar) so the web wizard can drive a secondary "enhancing" bar and
// CPU/RAM badges, mirroring the TUI (#47 phase 1).
func (s *Server) handleV2IndexStatus(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}

	cfg, err := loadGroupConfigBySlug(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", "group not registered: "+group)
		return
	}

	reply := indexStatusReply{Repos: []indexStatusRepo{}}

	if layout, lerr := daemon.DefaultLayout(); lerr == nil {
		if ef, fresh := daemon.EngineLivenessStatus(layout.Root); fresh && ef != nil {
			reply.Engine = indexStatusEngine{CPUPct: ef.CPUPct, RSSMB: ef.RSSMB}
		}
	}

	for _, repo := range cfg.Repos {
		row := indexStatusRepo{RepoSlug: repo.Slug}
		if f, ok := daemon.RepoStatusFile(repo.Path); ok && f != nil {
			row.Indexing = f.Indexing
			row.Enhancing = f.Enhancing
			row.Entities = f.Entities
			row.Relationships = f.Relationships
			row.GraphFBMtime = f.GraphFBMtime
			row.ReindexRequired = f.ReindexRequired
			row.ReindexReason = f.ReindexReason
		}
		row.Status = deriveIndexStatus(row.ReindexRequired, row.Indexing, row.Enhancing)
		reply.Repos = append(reply.Repos, row)
	}

	writeV2JSON(w, http.StatusOK, v2OK(reply))
}
