// migration_prune.go — central, belt-and-suspenders prune of Django
// migration entities at the indexer level (#2706).
//
// Background
// ----------
// Auto-generated Django migration files (`<app>/migrations/0NNN_*.py`) are
// pure ORM scaffolding with zero architectural signal. PRs #2551, #2602
// and #2616 layered per-extractor prunes (Python AST walk, cross-language
// hierarchy extractor) gated by the ARCHIGRAPH_EMIT_MIGRATION_ENTITIES
// env-var opt-in.
//
// Bench iter 8 (2026-05-27) found 43 SCOPE.Component entities for
// `core/migrations/*.py` had reappeared in the upvate graph (#2706).
// Root cause: `extractor.FileEntity` is called unconditionally at the top
// of every per-language extractor's Extract() and emits a
// SCOPE.Component(subtype="file") for every source file — including
// migration files. New extractor paths (file_conventions in #2382,
// new framework synthesisers in Wave 3-5: PRs #2680, #2696, #2698,
// #2700) can also emit SCOPE.Component-shaped entities anchored to
// migration files without knowing about the per-extractor prune.
//
// Fix
// ---
// One sweep at the indexer level that drops every entity whose
// SourceFile is a Django migration file AND whose Kind is one of the
// container/scope shapes we never want to surface for migrations:
// SCOPE.Component, SCOPE.Class, SCOPE.Operation, Class, Operation,
// Component. Entities of kind "Migration" (the lightweight file-tag
// emitted by the YAML file_convention rule + the opt-in
// extractMigrationEntity) are preserved — those are the *intended*
// migration representation.
//
// All relationships referencing a removed entity ID on either end are
// dropped. Opt-in via ARCHIGRAPH_EMIT_MIGRATION_ENTITIES=1|true bypasses
// the prune entirely so analysts who need full migration extraction can
// still get it.
//
// Both the full-rebuild path (cmd/archigraph/index.go::Indexer.Run, after
// buildDocument) and the incremental path (this package's
// TryIncremental, after the re-extraction merge) call PruneMigrationEntities
// so neither route can silently let migrations slip back into the graph.

package extractors

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// migrationEmitEnv mirrors the env-var name used by
// internal/extractors/python/extractor.go and
// internal/extractors/cross/hierarchy/extractor.go. Keep in sync.
const migrationEmitEnv = "ARCHIGRAPH_EMIT_MIGRATION_ENTITIES"

// prunedMigrationKinds is the set of entity kinds we drop when anchored
// to a Django migration file. "Migration" is intentionally absent: that
// kind is the lightweight file-tag we *want* to keep when emitted by the
// YAML file_convention rule or the opt-in extractMigrationEntity helper.
var prunedMigrationKinds = map[string]bool{
	"SCOPE.Component": true,
	"SCOPE.Class":     true,
	"SCOPE.Operation": true,
	"Class":           true,
	"Operation":       true,
	"Component":       true,
}

// IsDjangoMigrationFile mirrors the predicate in
// internal/extractors/python/extractor.go and
// internal/extractors/cross/hierarchy/extractor.go. Returns true for paths
// of the form `.../migrations/<anything>.py` (the parent directory is
// literally named "migrations").
func IsDjangoMigrationFile(path string) bool {
	if path == "" {
		return false
	}
	if !strings.HasSuffix(path, ".py") {
		return false
	}
	dir := filepath.Dir(filepath.FromSlash(path))
	return filepath.Base(dir) == "migrations"
}

// MigrationEmitEnabled returns true when the operator has opted into full
// migration extraction by setting ARCHIGRAPH_EMIT_MIGRATION_ENTITIES=1|true.
// Default is off — migrations are pruned.
func MigrationEmitEnabled() bool {
	v := os.Getenv(migrationEmitEnv)
	return v == "1" || v == "true"
}

// PruneMigrationEntities drops container/scope entities anchored to Django
// migration files plus every relationship that referenced them on either
// end. Returns the number of entities and relationships pruned so callers
// can log / surface the count. No-op when the opt-in env var is set.
//
// Idempotent — safe to call multiple times; the second pass finds nothing
// to remove.
func PruneMigrationEntities(doc *graph.Document) (entitiesPruned, relsPruned int) {
	if doc == nil || MigrationEmitEnabled() {
		return 0, 0
	}

	removedIDs := make(map[string]bool)
	keptEntities := make([]graph.Entity, 0, len(doc.Entities))
	for _, e := range doc.Entities {
		if IsDjangoMigrationFile(e.SourceFile) && prunedMigrationKinds[e.Kind] {
			if e.ID != "" {
				removedIDs[e.ID] = true
			}
			entitiesPruned++
			continue
		}
		keptEntities = append(keptEntities, e)
	}
	doc.Entities = keptEntities

	if len(removedIDs) == 0 {
		return entitiesPruned, 0
	}

	keptRels := make([]graph.Relationship, 0, len(doc.Relationships))
	for _, r := range doc.Relationships {
		if removedIDs[r.FromID] || removedIDs[r.ToID] {
			relsPruned++
			continue
		}
		keptRels = append(keptRels, r)
	}
	doc.Relationships = keptRels

	return entitiesPruned, relsPruned
}
