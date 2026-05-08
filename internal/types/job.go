package types

// IndexJob represents a full indexing job checkpoint stored in Supabase index_jobs.
// Updated at each pipeline stage transition.
type IndexJob struct {
	JobID           string `json:"job_id"`
	OrgID           string `json:"org_id"`
	ProjectID       string `json:"project_id"`
	RepoURL         string `json:"repo_url"`
	Branch          string `json:"branch"`
	Status          string `json:"status"`           // pending/running/completed/failed
	Stage           string `json:"stage"`            // extract/transform/load/synthesize/qa
	FilesDiscovered int    `json:"files_discovered"`
	FilesQueued     int    `json:"files_queued"`
	FilesProcessed  int    `json:"files_processed"`
}
