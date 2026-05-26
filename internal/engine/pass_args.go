// Package engine — shared arg/result types for engine detector passes.
//
// Every pass called from detector.go's Detect method is refactored to accept
// DetectorPassArgs and return DetectorPassResult.  The structs carry the full
// set of inputs any pass may need; passes that don't use a field simply ignore
// it.  This eliminates the previous ad-hoc positional signatures and fixes the
// asymmetry introduced by PR #2444 (#2430), where applyORMFieldEdges was the
// only pass that returned bare relationships instead of (entities, relationships).
//
// Fields:
//
//   - Ctx           — context for passes that spawn spans or do I/O (Spring,
//     Django route composition).
//   - Lang          — file language tag ("python", "go", "java", …).
//   - Path          — repo-relative file path.
//   - RepoRoot      — absolute filesystem path of the repo root (Kafka edges).
//   - Content       — raw file bytes.
//   - Pass1Entities — side-channel with Pass 1 per-file entities (ORM field
//     edges, see #2352).
//   - Entities      — entities accumulated so far in the Detect pipeline.
//   - Relationships — relationships accumulated so far in the Detect pipeline.
//
// Refs #2446.
package engine

import (
	"context"

	"github.com/cajasmota/archigraph/internal/types"
)

// DetectorPassArgs is the single struct-of-args passed to every engine pass
// called from detector.go's Detect method.
type DetectorPassArgs struct {
	// Ctx is the context forwarded from Detect; used by passes that start
	// OpenTelemetry spans or perform I/O (e.g. Spring/Django route composition).
	Ctx context.Context

	// Lang is the file's language tag (e.g. "python", "go", "java", "kotlin").
	Lang string

	// Path is the repo-relative file path.
	Path string

	// RepoRoot is the absolute filesystem path of the repository root.
	// Only populated when the Kafka edge pass (or future passes with the same
	// need) requires it; zero value ("") is safe for all other passes.
	RepoRoot string

	// Content is the raw file bytes.
	Content []byte

	// Pass1Entities is the side-channel plumbed in from Pass 1 of the
	// indexing pipeline.  The ORM field-edges pass uses it to resolve
	// SCOPE.Schema(subtype=field) entities without a regex re-scan.
	// Other passes ignore it.
	Pass1Entities []types.EntityRecord

	// Entities holds the entity slice accumulated so far in the Detect
	// pipeline.  Passes that emit or modify entities receive this as input
	// and include their additions in DetectorPassResult.Entities.
	Entities []types.EntityRecord

	// Relationships holds the relationship slice accumulated so far in the
	// Detect pipeline.  All passes may append to this and return the grown
	// slice in DetectorPassResult.Relationships.
	Relationships []types.RelationshipRecord
}

// DetectorPassResult is the uniform return type of every engine pass called
// from detector.go's Detect method.
//
// Passes that don't modify the entity slice pass Entities through unchanged.
// Passes that previously returned only relationships (applyORMFieldEdges) now
// also return the Entities slice unmodified, restoring the symmetry broken by
// PR #2444 (#2430).
type DetectorPassResult struct {
	// Entities is the (possibly extended) entity slice after the pass ran.
	Entities []types.EntityRecord

	// Relationships is the (possibly extended) relationship slice after the
	// pass ran.
	Relationships []types.RelationshipRecord
}
