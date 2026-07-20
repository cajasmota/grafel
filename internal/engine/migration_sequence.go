package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cajasmota/grafel/internal/enrichers"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// MigrationSequence is the engine pass that restores the previously-orphaned
// migration_sequence enricher (issue #3639, epic #3625): a parser existed at
// internal/enrichers/migration_sequence_enricher.go but was imported by zero
// production code, so no migration entity ever carried a sequence_number /
// migration_name property and no migration-ordering edge was ever emitted.
//
// WHAT IT DOES. For every graph entity anchored to a recognised migration file
// — Rails (db/migrate/YYYYMMDDHHMMSS_name.rb), Django (NNNN_name.py),
// Flyway (V{n}__desc.sql), golang-migrate (NNNNNN_name.up|down.sql), and
// Alembic (versions/{rev}_name.py) — the pass parses the FILENAME and stamps:
//
//	sequence_number   the migration's ordinal (int for Rails/Django/golang-migrate
//	                  & Alembic-fallback; dotted string for Flyway "1.2"; for
//	                  Alembic the 12+-char revision hash from the filename).
//	migration_name    the human-readable description (underscores → spaces).
//	migration_pattern which convention matched (rails|django|flyway|
//	                  golang_migrate|alembic).
//
// This is the DB-migration ordering signal: it lets find/neighbors/agents read
// "which migration is this, and where does it sit in the sequence" directly off
// the entity instead of re-deriving it from the path.
//
// ALEMBIC ORDERING EDGE. Alembic stores its apply-order DAG inside the file
// body (`revision = "..."`, `down_revision = "..."`), not in the filename. When
// a source reader is supplied, the pass parses those assignments and emits a
// PRECEDES edge down_revision → revision for every migration whose parent is
// present in the graph. PRECEDES means "the source migration must be applied
// BEFORE the target". This makes the Alembic migration chain a traversable
// subgraph rather than opaque filename ordering.
//
// HONEST / IDEMPOTENT. Only files whose basename matches a known migration
// convention are touched (unknown-prefix schema files are left untouched). No
// new migration entities are created — the pass annotates entities the
// language extractors already emitted. A second call recomputes identical
// values and (because edges are keyed on a stable RelationshipID) does not
// duplicate the PRECEDES edges.

// migrationPrecedesKind is the directed apply-order edge between migration
// entities: FromID (the earlier migration) PRECEDES ToID (the later one).
var migrationPrecedesKind = string(types.RelationshipKindPrecedes)

// pendingAlembic refers to an Alembic migration entity by its index in
// doc.Entities, deferred so the file body can be read once and parsed for the
// revision DAG.
type pendingAlembic struct {
	idx int
}

// MigrationSequenceStats summarises a MigrationSequence run.
type MigrationSequenceStats struct {
	// EntitiesAnnotated is the number of entities that received
	// sequence_number / migration_name / migration_pattern properties.
	EntitiesAnnotated int
	// FilesMatched is the number of distinct migration files whose basename
	// matched a known convention.
	FilesMatched int
	// PrecedesEdges is the number of Alembic down_revision → revision PRECEDES
	// edges emitted.
	PrecedesEdges int
	// Skipped is true when there were no entities to scan.
	Skipped bool
}

// MigrationSourceReader returns the full source text of a migration file given
// its (repo-relative) SourceFile path. The second return is false when the file
// could not be read; callers that have no disk access (e.g. unit tests) may
// pass nil, which disables Alembic PRECEDES-edge emission while leaving
// filename-derived annotation fully functional.
type MigrationSourceReader func(sourceFile string) (string, bool)

// DiskMigrationSourceReader builds a MigrationSourceReader that resolves each
// SourceFile against absRepo and reads it from disk. Used by the indexer.
func DiskMigrationSourceReader(absRepo string) MigrationSourceReader {
	return func(sourceFile string) (string, bool) {
		if sourceFile == "" {
			return "", false
		}
		path := sourceFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(absRepo, sourceFile)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", false
		}
		return string(data), true
	}
}

// ApplyMigrationSequence annotates migration entities in doc with ordering
// metadata and (for Alembic, when reader != nil) emits PRECEDES edges along the
// down_revision chain. It returns stats describing the run.
func ApplyMigrationSequence(doc *graph.Document, reader MigrationSourceReader) MigrationSequenceStats {
	if doc == nil || len(doc.Entities) == 0 {
		return MigrationSequenceStats{Skipped: true}
	}

	// Build the enricher input from every entity that carries a SourceFile.
	// AnnotateMigrationSequences discriminates by basename, so non-migration
	// files fall through to the unknown bucket and are never annotated.
	input := make([]enrichers.MigrationEntity, 0, len(doc.Entities))
	idByEntityID := make(map[string]int, len(doc.Entities))
	for i := range doc.Entities {
		idByEntityID[doc.Entities[i].ID] = i
		if doc.Entities[i].SourceFile == "" {
			continue
		}
		input = append(input, enrichers.MigrationEntity{
			EntityID:   doc.Entities[i].ID,
			SourceFile: doc.Entities[i].SourceFile,
		})
	}

	annotations, _ := enrichers.AnnotateMigrationSequences(input)
	if len(annotations) == 0 {
		return MigrationSequenceStats{Skipped: false}
	}

	matchedFiles := make(map[string]bool)
	annotated := 0
	// alembicEntities collects the entity that anchors each Alembic migration
	// file, so we read each file once and key the revision DAG by file.
	alembicEntities := make([]pendingAlembic, 0)

	for _, a := range annotations {
		idx, ok := idByEntityID[a.EntityID]
		if !ok {
			continue
		}
		e := &doc.Entities[idx]
		if e.PropLen() == 0 {
			e.PropsReplace(make(map[string]string))
		}
		e.PropSet("sequence_number", fmt.Sprintf("%v", a.SequenceNumber))
		e.PropSet("migration_name", a.MigrationName)
		e.PropSet("migration_pattern", string(a.PatternMatched))
		annotated++
		matchedFiles[e.SourceFile] = true

		if a.PatternMatched == enrichers.MigrationPatternAlembic {
			alembicEntities = append(alembicEntities, pendingAlembic{idx: idx})
		}
	}

	stats := MigrationSequenceStats{
		EntitiesAnnotated: annotated,
		FilesMatched:      len(matchedFiles),
		Skipped:           false,
	}

	if reader != nil && len(alembicEntities) > 0 {
		stats.PrecedesEdges = emitAlembicPrecedes(doc, alembicEntities, reader)
	}

	return stats
}

// emitAlembicPrecedes reads each Alembic migration file once, extracts its
// (revision, down_revision) pair, and emits a PRECEDES edge from the parent
// migration entity (down_revision) to the child (revision) when BOTH endpoints
// are present in the graph. Returns the number of edges added.
func emitAlembicPrecedes(doc *graph.Document, alembic []pendingAlembic, reader MigrationSourceReader) int {
	// Map revision-id → entity ID. One entity per distinct file is enough; if
	// several entities share a file (rare for Alembic .py) we key on the file
	// to read it once and attribute the revision to the first entity seen.
	type revInfo struct {
		entityID     string
		downRevision string
	}
	revToEntity := make(map[string]string)    // revision id → entity ID
	infos := make([]revInfo, 0, len(alembic)) // preserve order for determinism
	seenFile := make(map[string]bool)

	for _, pe := range alembic {
		e := &doc.Entities[pe.idx]
		if seenFile[e.SourceFile] {
			continue
		}
		seenFile[e.SourceFile] = true
		src, ok := reader(e.SourceFile)
		if !ok {
			continue
		}
		rev, down := enrichers.ParseAlembicRevisions(src)
		if rev == "" {
			continue
		}
		// Record the parsed revision id on the entity too — it is the canonical
		// Alembic identity (filename hash can be truncated/aliased).
		if e.PropLen() == 0 {
			e.PropsReplace(make(map[string]string))
		}
		e.PropSet("revision", rev)
		if down != "" {
			e.PropSet("down_revision", down)
		}
		revToEntity[rev] = e.ID
		infos = append(infos, revInfo{entityID: e.ID, downRevision: down})
	}

	// Emit one PRECEDES edge per child whose parent revision resolves to a
	// known entity: parent (down_revision) PRECEDES child (revision).
	existing := make(map[string]bool, len(doc.Relationships))
	for k := range doc.Relationships {
		existing[doc.Relationships[k].ID] = true
	}

	// Deterministic order.
	sort.Slice(infos, func(i, j int) bool { return infos[i].entityID < infos[j].entityID })

	added := 0
	for _, info := range infos {
		if info.downRevision == "" {
			continue // base migration: nothing precedes it
		}
		parentID, ok := revToEntity[info.downRevision]
		if !ok {
			continue // parent not in graph: don't fabricate an edge
		}
		if parentID == info.entityID {
			continue // defensive self-edge guard
		}
		relID := graph.RelationshipID(parentID, info.entityID, migrationPrecedesKind)
		if existing[relID] {
			continue
		}
		existing[relID] = true
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     relID,
			FromID: parentID,
			ToID:   info.entityID,
			Kind:   migrationPrecedesKind,
		}.WithProperties(map[string]string{
			"ordering": "alembic_down_revision",
			"pattern":  string(enrichers.MigrationPatternAlembic),
		},
		))
		added++
	}
	return added
}
