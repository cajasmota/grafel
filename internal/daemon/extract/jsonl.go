// Package extract implements the daemon-side subprocess-extraction
// architecture (Phase F). The package owns the JSONL protocol that
// short-lived extractor subprocesses use to stream entity and
// relationship records back to the daemon coordinator.
//
// Phase F architecture:
//
//	[daemon] -- fork-exec self with `grafel extract --batch=...`
//	   |          (subprocess only loads grammars / extractors it needs)
//	   |
//	   <-- JSONL stdout: {"type":"entity",...} / {"type":"relationship",...}
//	   |
//	[daemon] accumulates → runs resolution + classification + algorithms
//	         on the merged record set, then writes graph.fb / graph.json.
//
// Memory model: each subprocess parses only the files in its batch
// (~50-100). Its tree-sitter trees + emitted records are freed on exit.
// The daemon coordinator never holds AST trees; it holds only the final
// record stream (which is what the original in-process pipeline also
// holds after Pass 3, so resolution-stage RSS is unchanged).
package extract

import (
	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/types"
)

// EnvelopeKind tags each JSONL line so the decoder can route it.
type EnvelopeKind string

const (
	// KindEntity wraps a types.EntityRecord (Pass 1 / Pass 2.5 / Pass 3
	// entities produced by per-file extractors).
	KindEntity EnvelopeKind = "entity"

	// KindRelationship wraps a types.RelationshipRecord (Pass 2.5
	// framework-rule relationships that are not embedded under an
	// entity).
	KindRelationship EnvelopeKind = "relationship"

	// KindStats is emitted exactly once at the end of a subprocess's
	// output. It carries per-pass counters so the coordinator can
	// fold them into the daemon-side stats summary without re-counting.
	KindStats EnvelopeKind = "stats"

	// KindError is emitted by the subprocess when a non-fatal error
	// occurs (e.g., a single extractor panic). The coordinator logs it
	// but does not fail the batch.
	KindError EnvelopeKind = "error"
)

// Envelope is the on-wire JSONL line. Exactly one of Entity / Rel /
// Stats / Err is populated depending on Type. Keeping a single struct
// makes streaming-decode straightforward (one json.Decoder, one type).
type Envelope struct {
	Type EnvelopeKind `json:"type"`

	Entity *types.EntityRecord       `json:"entity,omitempty"`
	Rel    *types.RelationshipRecord `json:"rel,omitempty"`
	Stats  *BatchStats               `json:"stats,omitempty"`
	Err    string                    `json:"err,omitempty"`
}

// BatchStats is the per-subprocess counter set. The coordinator sums
// these across all batches into the IndexerStats it would otherwise
// have computed in-process.
type BatchStats struct {
	BatchID    string         `json:"batch_id"`
	Files      int            `json:"files"`
	Processed  int            `json:"processed"`
	Extracted  int            `json:"extracted"`
	Skipped    int            `json:"skipped"`
	Failed     int            `json:"failed"`
	Pass1Rels  int            `json:"pass1_rels"`
	Pass2Rels  int            `json:"pass2_rels"`
	Pass25Rels int            `json:"pass2_5_rels"`
	Pass3Rels  int            `json:"pass3_rels"`
	ByLang     map[string]int `json:"by_lang,omitempty"`
	ByCrossExt map[string]int `json:"by_cross_ext,omitempty"`
	RSSBytes   uint64         `json:"rss_bytes"`

	// ParseErrors is the per-language ERROR-node accumulator for the A4
	// parse-error canary (#5414). Each subprocess reports its node-weighted
	// per-language error stats; the coordinator merges them across batches.
	ParseErrors map[string]treesitter.LangErrorStats `json:"parse_errors,omitempty"`

	// Pass1Plumbed counters (issue #2447): track how many files had
	// FileInput.Pass1Entities non-empty (True) vs empty (False) when
	// Detector.Detect was called for Pass 2.5.
	//
	// Heterogeneous-repo semantics (issue #2464): runPass25FrameworkRules
	// runs Pass 2.5 against ALL classified files regardless of language.
	// Non-Django files (Go, JS, TypeScript, etc.) never produce
	// SCOPE.Schema(subtype=field) entities in Pass 1, so they legitimately
	// contribute to FalseCount. A non-zero FalseCount is therefore EXPECTED
	// on any multi-language repository and does NOT indicate a plumbing bug.
	//
	// Correct production diagnostic:
	//   ratio = TrueCount / (TrueCount + FalseCount)
	// For Django-only repos this ratio should be near 1.0. For heterogeneous
	// repos it will be proportional to the fraction of files that are Django
	// models. A ratio near 0.0 on a known-Django repo is the signal worth
	// alerting on.
	//
	// The naive check "FalseCount == 0" is only valid on Django-only test
	// fixtures (e.g. the unit-test fixture from PR #2463). Do NOT use it
	// as a production health gate.
	//
	// Pre-Pass-2.5 language filtering (run Detect only on Python files) is
	// the deeper fix that would eliminate FalseCount noise entirely; tracked
	// as a follow-up to issue #2464.
	Pass1PlumbedTrueCount  int `json:"pass1_plumbed_true"`
	Pass1PlumbedFalseCount int `json:"pass1_plumbed_false"`
}
