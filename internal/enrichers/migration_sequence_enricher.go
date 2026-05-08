package enrichers

// MigrationSequenceEnricher parses migration filenames to add sequence metadata.
// Port of Python migration_sequence_enricher.py (MX-701).

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
	railsMigrationRe   = regexp.MustCompile(`^(\d{14})_([^.]+)\.rb$`)
	djangoMigrationRe  = regexp.MustCompile(`^(\d{4})_([^.]+)\.py$`)
	flywayMigrationRe  = regexp.MustCompile(`^V(\d+\.\d+)__([^.]+)\.sql$`)
	golangMigrateRe    = regexp.MustCompile(`^(\d{1,13})_([^.]+)\.(up|down)\.sql$`)
	alembicMigrationRe = regexp.MustCompile(`^([A-Za-z0-9]{12})_([^.]+)\.py$`)
)

func parseMigrationFilename(basename string) (seq interface{}, name string, pattern MigrationPattern, ok bool) {
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
	if m := alembicMigrationRe.FindStringSubmatch(basename); m != nil {
		return m[1], strings.ReplaceAll(m[2], "_", " "), MigrationPatternAlembic, true
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
		basename := filepath.Base(entity.SourceFile)
		seq, name, pattern, ok := parseMigrationFilename(basename)
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
