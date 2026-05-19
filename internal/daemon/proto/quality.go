// Package proto — quality audit wire types.
//
// QualityAuditRequest / QualityAuditReply carry the `archigraph quality
// audit-orphans` request over the daemon socket. The daemon handles the
// request by calling into internal/quality/audit so the CLI process has
// no dependency on the graph or audit packages.
package proto

// QualityAuditRequest asks the daemon to audit a single repo (or a
// corpus directory when Corpus=true).
type QualityAuditRequest struct {
	// RepoPath is the absolute path to audit. For a single repo it must
	// contain .archigraph/graph.json; set Corpus=true to treat it as a
	// flat directory of repos.
	RepoPath string `json:"repo_path"`

	// Kind selects the audit variant. The only defined value today is
	// "orphans". Room is left for "imports", "references", etc.
	Kind string `json:"kind"`

	// Corpus mirrors the --corpus flag: treat RepoPath as a directory
	// containing many indexed repos rather than a single repo root.
	Corpus bool `json:"corpus,omitempty"`

	// JSON requests the reply's Markdown field to be replaced with a
	// JSON-encoded report rather than the markdown one. When false the
	// daemon fills Markdown with the human-readable report; when true it
	// fills Markdown with the JSON encoding of audit.Report.
	JSON bool `json:"json,omitempty"`
}

// QualityAuditReply carries the audit summary back to the CLI.
type QualityAuditReply struct {
	// OrphansByKind maps each orphan cause label to its count across all
	// repos in the request. Populated even when JSON=true.
	OrphansByKind map[string]int `json:"orphans_by_kind"`

	// TotalEntities is the sum of entities across all audited repos.
	TotalEntities int `json:"total_entities"`

	// TotalOrphans is the sum of orphans across all audited repos.
	TotalOrphans int `json:"total_orphans"`

	// OrphanRatePercent = 100 * TotalOrphans / TotalEntities. Zero when
	// TotalEntities is zero.
	OrphanRatePercent float64 `json:"orphan_rate_percent"`

	// Markdown contains the formatted report. The format is markdown by
	// default or JSON when QualityAuditRequest.JSON=true. The CLI writes
	// this directly to stdout so it does not need to reconstruct the
	// report from the scalar fields above.
	Markdown string `json:"markdown"`
}
