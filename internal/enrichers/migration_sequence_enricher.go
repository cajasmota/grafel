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
	// discriminator. The revision group is 12+ HEX characters: that is the
	// shape of Alembic's DEFAULT generator, `uuid.uuid4().hex[-12:]`. It is a
	// default, not a constraint — `alembic revision --rev-id=<anything>` is
	// unvalidated, and `file_template` is configurable (the alembic.ini that
	// `alembic init` ships carries a commented date-prefixed example, under
	// which the rev id is not the first basename segment at all and this regex
	// misses entirely). So this matches the common shape, deliberately, rather
	// than everything Alembic can emit.
	//
	// The leading `^` is part of that shape and is deliberate: the revision is
	// the FIRST underscore-delimited segment, not a hex run anywhere in the
	// basename. Unanchored, `helpers_abc123def456_util.py` parses as revision
	// abc123def456. If `file_template` support ever lands, the anchor is the
	// thing someone will reach for, and changing it should be a visible test
	// change — pinned by the last case in
	// TestAnnotateMigrationSequences_OrdinaryPythonModulesUnstamped.
	//
	// Both bounds carry weight. The former `[A-Za-z0-9]{12,}` matched any
	// Python module whose first underscore-delimited segment was long enough —
	// `notification_stream.py` and `authentication_service.py` both parsed as
	// migrations (#6557). And the `{12,}` minimum is what keeps ordinary words
	// spelled in hex letters (`added_field.py`, `deface_x.py`) out, including
	// inside a real versions/ directory. Both are pinned by
	// TestAnnotateMigrationSequences_OrdinaryPythonModulesUnstamped, and the
	// charset policy separately by
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
// The comparison is EqualFold on purpose, so a case-insensitive filesystem
// reporting `VERSIONS/` still resolves. Pinned by
// TestAnnotateMigrationSequences_VersionsComponentIsCaseInsensitive.
//
// filepath.ToSlash does NOT buy backslash handling off Windows: it is a no-op
// there, and filepath.Dir on a `app\alembic\versions\x.py` string then returns
// ".", so such a path is REJECTED on darwin/linux. The behaviour is therefore
// GOOS-dependent and untested — SourceFile arrives slash-normalised upstream,
// and this mirrors the house idiom at internal/graph/coverage.go:561.
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
