// trickle_collect.go — chunked, paced entity-candidate collection for the
// background enrichment worker (#5720). This is the trickle counterpart of
// CollectCandidates: instead of walking every entity and building one
// full-graph []Candidate slice, it walks doc.Entities in small batches,
// flushes each batch through a CandidateAppender, and pauses between
// batches so the worker is a slow, idle-preferring trickle rather than a
// burst — and so it can react to cancellation promptly (a new index of the
// same repo supersedes this run; see background.go).
package enrichment

import (
	"context"
	"runtime"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// DefaultTrickleChunkSize bounds how many entities are processed per batch.
// Combined with pacing between batches, this is what keeps peak heap
// attributable to enrichment a small constant: only one batch's worth of
// Candidate structs is ever alive at a time, independent of total entity
// count.
const DefaultTrickleChunkSize = 250

// DefaultTricklePace is the pause between batches. Small enough that even a
// huge graph finishes in a reasonable wall-clock time, large enough that the
// worker never saturates a core or contends with a foreground index/query.
const DefaultTricklePace = 15 * time.Millisecond

// TrickleOptions configures CollectAndAppendTrickle. Zero value uses the
// package defaults.
type TrickleOptions struct {
	ChunkSize int
	Pace      time.Duration
	// OnChunk, if non-nil, is called after each batch is appended with the
	// number of candidates written in that batch. Test/observability hook —
	// never required for correctness.
	OnChunk func(appended int)
}

func (o TrickleOptions) chunkSize() int {
	if o.ChunkSize > 0 {
		return o.ChunkSize
	}
	return DefaultTrickleChunkSize
}

func (o TrickleOptions) pace() time.Duration {
	if o.Pace > 0 {
		return o.Pace
	}
	return DefaultTricklePace
}

// CollectAndAppendTrickle runs emitters over doc.Entities in successive
// batches of opts.ChunkSize, appending each batch to appender and pausing
// opts.Pace between batches. It returns ctx.Err() (without appending further
// output) as soon as ctx is cancelled — the caller (background.go's
// Scheduler) is expected to Abort() the appender in that case rather than
// Close() it, so a superseded run never publishes a partial/stale file.
//
// rejected is the (subject_id|kind) rejection set, as produced by
// ReadRejections — passed in rather than re-read here so callers can load it
// once up front (it's small; no streaming concern).
func CollectAndAppendTrickle(ctx context.Context, doc *graph.Document, emitters []CandidateEmitter, rejected map[string]bool, appender *CandidateAppender, opts TrickleOptions) error {
	if doc == nil {
		return nil
	}
	chunkSize := opts.chunkSize()
	pace := opts.pace()
	seen := map[string]bool{}
	n := len(doc.Entities)

	for start := 0; start < n; start += chunkSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := start + chunkSize
		if end > n {
			end = n
		}
		var batch []Candidate
		for i := start; i < end; i++ {
			e := &doc.Entities[i]
			for _, em := range emitters {
				for _, c := range em.EmitFor(e, doc) {
					if c.ID == "" || seen[c.ID] {
						continue
					}
					key := c.SubjectID + "|" + c.Kind
					if rejected[key] {
						continue
					}
					seen[c.ID] = true
					batch = append(batch, c)
				}
			}
		}
		if len(batch) > 0 {
			if err := appender.AppendChunk(batch); err != nil {
				return err
			}
			if opts.OnChunk != nil {
				opts.OnChunk(len(batch))
			}
		}
		batch = nil // release this batch's memory before the next iteration

		if end >= n {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pace):
		}
		runtime.Gosched()
	}
	return nil
}
