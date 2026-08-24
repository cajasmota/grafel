package enrichers

// MigrationSequenceEnricher parses migration filenames to add sequence metadata.
// Port of Python migration_sequence_enricher.py.

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// MigrationPattern identifies which filename convention was matched.
type MigrationPattern string

const (
	MigrationPatternRails         MigrationPattern = "rails"
	MigrationPatternDjango        MigrationPattern = "django"
	MigrationPatternFlyway        MigrationPattern = "flyway"
	MigrationPatternGolangMigrate MigrationPattern = "golang_migrate"
	MigrationPatternAlembic       MigrationPattern = "alembic"
	MigrationPatternUnknown       MigrationPattern = "unknown"
)

// MigrationAnnotation is parsed metadata for a single migration entity.
type MigrationAnnotation struct {
	EntityID       string
	SequenceNumber interface{}
	MigrationName  string
	PatternMatched MigrationPattern
}

// MigrationEntity is the input record for migration annotation.
type MigrationEntity struct {
	EntityID   string
	SourceFile string
}

var (
	railsMigrationRe  = regexp.MustCompile(`^(\d{14})_([^.]+)\.rb$`)
	djangoMigrationRe = regexp.MustCompile(`^(\d{4})_([^.]+)\.py$`)
	flywayMigrationRe = regexp.MustCompile(`^V(\d+(?:\.\d+)*)__([^.]+)\.sql$`)
	golangMigrateRe   = regexp.MustCompile(`^(\d{1,14})_([^.]+)\.(up|down)\.sql$`)
	// alembicMigrationRe matches the basename half of the Alembic
	// discriminator. The revision group is HEX only: Alembic generates rev_id
	// as `uuid.uuid4().hex[-12:]`, so every generated id is hex, while the
	// former `[A-Za-z0-9]{12,}` matched any Python module whose first
	// underscore-delimited segment was long enough — `notification_stream.py`
	// and `authentication_service.py` both parsed as migrations (#6557).
	// A hand-set `--rev-id` containing a non-hex letter is not recognised;
	// that recall cost is deliberate and is pinned by
	// TestAnnotateMigrationSequences_AlembicRequiresHexRevision.
	//
	// The basename is NOT sufficient on its own: it is conjoined with
	// hasAlembicVersionsAncestor below.
	alembicMigrationRe = regexp.MustCompile(`^([0-9a-fA-F]{12,})_([^.]+)\.py$`)
)

// hasAlembicVersionsAncestor reports whether sourceFile sits under a directory
// named `versions`, which is where Alembic keeps migration scripts and what the
// `lang.python.orm.alembic` coverage record already claims this pass matches
// ("each Alembic versions/*.py entity", docs/coverage/registry.json). It is the
// path half of the discriminator; the hex basename above is the other half.
// Neither half alone is enough — a hex-named module can live outside versions/,
// and an API-versioning `versions/` directory holds ordinary modules — so the
// two are required together. Pure: string inspection only, no I/O.
//
// The match is on a path COMPONENT, not a substring of the directory: an
// `oldversions/` or `versions_old/` directory must NOT qualify. Pinned by
// TestAnnotateMigrationSequences_VersionsMustBeAPathComponent.
//
// filepath.ToSlash is load-bearing only on Windows, where filepath.Dir emits
// backslashes; on unix it is a no-op, so no test in this package observes it.
func hasAlembicVersionsAncestor(sourceFile string) bool {
	dir := filepath.ToSlash(filepath.Dir(sourceFile))
	for _, seg := range strings.Split(dir, "/") {
		if strings.EqualFold(seg, "versions") {
			return true
		}
	}
	return false
}

// alembicRevisionRe and alembicDownRevisionRe extract the module-level
// `revision` and `down_revision` string assignments from an Alembic migration
// file body. Alembic stores the DAG ordering in these variables (not in the
// filename): `down_revision = None` for the root migration, otherwise the id of
// the parent revision. We match both single- and double-quoted string literals
// and the `None` sentinel. Anchored with (?m) so they match a top-level
// assignment on its own line, tolerating leading whitespace.
var (
	alembicRevisionRe     = regexp.MustCompile(`(?m)^\s*revision\s*(?::[^=]*)?=\s*['"]([A-Za-z0-9_]+)['"]`)
	alembicDownRevisionRe = regexp.MustCompile(`(?m)^\s*down_revision\s*(?::[^=]*)?=\s*(?:['"]([A-Za-z0-9_]+)['"]|None)`)
)

// ParseAlembicRevisions extracts the (revision, downRevision) pair from an
// Alembic migration file's source. revision is the id this migration defines;
// downRevision is the parent it must run AFTER (empty string when the source
// declares `down_revision = None`, i.e. this is the base migration). Either
// return value is empty when the corresponding assignment is absent or
// unparseable. Pure: no I/O, deterministic.
func ParseAlembicRevisions(source string) (revision, downRevision string) {
	if m := alembicRevisionRe.FindStringSubmatch(source); m != nil {
		revision = m[1]
	}
	if m := alembicDownRevisionRe.FindStringSubmatch(source); m != nil {
		// m[1] is empty when the literal was `None`.
		downRevision = m[1]
	}
	return revision, downRevision
}

// parseMigrationFilename classifies a source path against the migration naming
// conventions. It takes the FULL (repo-relative) path, not just the basename:
// the Alembic branch needs the directory context to discriminate, because an
// Alembic-shaped basename alone is not evidence of a migration (#6557).
func parseMigrationFilename(sourceFile string) (seq interface{}, name string, pattern MigrationPattern, ok bool) {
	basename := filepath.Base(sourceFile)
	if m := railsMigrationRe.FindStringSubmatch(basename); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, strings.ReplaceAll(m[2], "_", " "), MigrationPatternRails, true
	}
	if m := djangoMigrationRe.FindStringSubmatch(basename); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, strings.ReplaceAll(m[2], "_", " "), MigrationPatternDjango, true
	}
	if m := flywayMigrationRe.FindStringSubmatch(basename); m != nil {
		return m[1], strings.ReplaceAll(m[2], "_", " "), MigrationPatternFlyway, true
	}
	if m := golangMigrateRe.FindStringSubmatch(basename); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, strings.ReplaceAll(m[2], "_", " "), MigrationPatternGolangMigrate, true
	}
	if hasAlembicVersionsAncestor(sourceFile) {
		if m := alembicMigrationRe.FindStringSubmatch(basename); m != nil {
			return m[1], strings.ReplaceAll(m[2], "_", " "), MigrationPatternAlembic, true
		}
	}
	return nil, "", MigrationPatternUnknown, false
}

// AnnotateMigrationSequences parses migration filenames and returns annotations.
func AnnotateMigrationSequences(entities []MigrationEntity) ([]MigrationAnnotation, int) {
	var annotations []MigrationAnnotation
	unknownCount := 0
	for _, entity := range entities {
		if entity.SourceFile == "" {
			continue
		}
		seq, name, pattern, ok := parseMigrationFilename(entity.SourceFile)
		if !ok {
			unknownCount++
			continue
		}
		annotations = append(annotations, MigrationAnnotation{
			EntityID:       entity.EntityID,
			SequenceNumber: seq,
			MigrationName:  name,
			PatternMatched: pattern,
		})
	}
	return annotations, unknownCount
}
