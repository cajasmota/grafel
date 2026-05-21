// Package progress defines the event types and interfaces for real-time indexer
// progress tracking. This file is a placeholder stub matching the shape that
// sub-issue A (instrumentation) will define. Sub-A owns the canonical definition;
// once that PR lands this file should be removed and the import updated.
package progress

// Event represents a single indexer progress tick emitted during a rebuild.
// Fields mirror the ProgressEvent shape agreed in the epic (#1118, sub-A #1119).
type Event struct {
	GroupSlug     string `json:"group_slug"`
	RepoSlug      string `json:"repo_slug"`
	Phase         string `json:"phase"` // scanning|extracting_ast|resolving_refs|running_algorithms|materializing|done|error
	FilesDone     int    `json:"files_done"`
	FilesTotal    int    `json:"files_total"`
	EntitiesSoFar int    `json:"entities_so_far"`
	ETAms         int    `json:"eta_ms,omitempty"`
	Error         string `json:"error,omitempty"`
	TS            int64  `json:"ts"`
}

// Publisher is the write side of the progress pipeline. The indexer calls
// Publish after every instrumentation tick. The broker implements this interface.
type Publisher interface {
	Publish(e Event)
}
