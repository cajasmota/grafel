package types

// BatchRecord holds a set of extracted entities for a single indexing batch.
// Written to S3 by IndexerTransform and read by IndexerLoad.
type BatchRecord struct {
	JobID       string         `json:"job_id"`
	OrgID       string         `json:"org_id"`
	ProjectID   string         `json:"project_id"`
	BatchID     string         `json:"batch_id"`
	Entities    []EntityRecord `json:"entities"`
}

// FileBatch is the SQS message payload dispatched by IndexerExtract.
// Each message covers up to 10 files for parallel IndexerTransform invocations.
type FileBatch struct {
	JobID       string      `json:"job_id"`
	OrgID       string      `json:"org_id"`
	ProjectID   string      `json:"project_id"`
	ProjectSlug string      `json:"project_slug"`
	BatchIndex  int         `json:"batch_index"`
	TotalFiles  int         `json:"total_files"`
	TotalBytes  int64       `json:"total_bytes"`
	Files       []FileEntry `json:"files"`
}

// FileEntry describes a single file within a FileBatch.
type FileEntry struct {
	Path      string `json:"path"`
	Language  string `json:"language"`
	SizeBytes int64  `json:"size_bytes"`
	S3Key     string `json:"s3_key"`
}
