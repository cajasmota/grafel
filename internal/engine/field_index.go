// Shared Django model field-index helper (issue #2295).
//
// BuildFieldIndex is extracted from the inline indexDjangoModelFields
// function that was defined solely in orm_field_edges.go. Any engine
// pass that needs a "<Model>.<field>" presence index for a Django source
// file should call BuildFieldIndex rather than re-implement the regex
// scan. The key format — `<Model>.<field>` — is byte-identical to the
// Name the Python extractor emits at python/extractor.go:1411-1412, so
// consumers that see both pipelines can match by name without further
// normalisation.
//
// Refs #2295.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// djangoClassDeclRe locates Django model class declarations:
//
//	class <Name>(... Model ...):
//
// The base class list is matched loosely (any parenthesised group that
// contains "Model") so subclasses of project-local abstract bases
// (e.g. `class User(TimestampedModel):` where TimestampedModel itself
// inherits from models.Model) are still recognised. False positives
// (a plain class whose parens contain the word "Model" but is not
// actually a Django model) are inert: their fields just won't be the
// target of any ORM filter_keys so no spurious edges are emitted.
var djangoClassDeclRe = regexp.MustCompile(
	`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\(([^)]*Model[^)]*)\)\s*:`,
)

// djangoFieldDeclRe locates field declarations inside a model body:
//
//	<name> = models.<SomethingField>(...)
//	<name> = <CustomField>(...)
//
// We accept either the `models.` namespace (stdlib Django fields) or a
// bare `<Capitalised>Field(` call (project-local custom field classes,
// django-money MoneyField, etc.). The leading indentation is required
// so top-level assignments at module scope (e.g. `User = get_user_model()`)
// are NOT treated as field declarations.
var djangoFieldDeclRe = regexp.MustCompile(
	`(?m)^[ \t]+([a-z_][a-zA-Z0-9_]*)\s*=\s*(?:models\.[A-Z]\w*|[A-Z]\w*?Field)\s*\(`,
)

// BuildFieldIndex scans src for Django model class bodies and returns
// the set of "<Model>.<field>" names discovered, mirroring the Name
// convention the Python extractor uses at python/extractor.go:1411.
//
// Strategy: locate each `class X(... Model ...):` header, then scan
// forward to the next class declaration (or EOF) and harvest every
// indented `<name> = models.…Field(…)` or `<name> = <Custom>Field(…)`
// assignment as a field of the enclosing class.
//
// The returned map is a presence set — values are always true. A nil /
// empty map is returned for source files that contain no recognisable
// Django model definitions.
func BuildFieldIndex(src string) map[string]bool {
	out := map[string]bool{}
	classMatches := djangoClassDeclRe.FindAllStringSubmatchIndex(src, -1)
	if len(classMatches) == 0 {
		return out
	}
	for i, m := range classMatches {
		className := src[m[2]:m[3]]
		// Body extends from the end of the matched header to the start
		// of the next class declaration (or EOF for the last class).
		bodyStart := m[1]
		bodyEnd := len(src)
		if i+1 < len(classMatches) {
			bodyEnd = classMatches[i+1][0]
		}
		body := src[bodyStart:bodyEnd]
		for _, fm := range djangoFieldDeclRe.FindAllStringSubmatch(body, -1) {
			if len(fm) < 2 {
				continue
			}
			fieldName := fm[1]
			if fieldName == "" {
				continue
			}
			out[className+"."+fieldName] = true
		}
	}
	return out
}

// BuildCrossFileFieldLookup constructs a CrossFileFields closure
// (issue #2448 / Phase B) over the supplied Pass-1 entity slice. The
// returned function, given a model class name, returns every
// SCOPE.Schema(subtype=field) entity in the slice whose Name begins
// with `<model>.`. Pre-bucketing by model keeps per-call work O(fields
// in that model) rather than O(all fields in the indexed scope).
//
// The closure is built ONCE per indexing run by the coordinator (the
// in-process indexer in cmd/archigraph/index.go runPass25FrameworkRules
// and the subprocess in internal/daemon/extract/subproc.go) from the
// union of Pass-1 entities across every file in the indexed scope (or
// the batch, in the subprocess case), then attached to every per-file
// extractor.FileInput.CrossFileFields before Detect.
//
// Returns nil when the input slice contains no usable field entities —
// callers should treat a nil return value as "no cross-file resolution
// available" and fall through to the intra-file branch only.
func BuildCrossFileFieldLookup(pass1 []types.EntityRecord) func(modelName string) []types.EntityRecord {
	if len(pass1) == 0 {
		return nil
	}
	byModel := map[string][]types.EntityRecord{}
	for _, e := range pass1 {
		if e.Kind != "SCOPE.Schema" || e.Subtype != "field" {
			continue
		}
		dot := strings.Index(e.Name, ".")
		if dot <= 0 {
			continue
		}
		model := e.Name[:dot]
		byModel[model] = append(byModel[model], e)
	}
	if len(byModel) == 0 {
		return nil
	}
	return func(modelName string) []types.EntityRecord {
		return byModel[modelName]
	}
}

// buildPlumbedFieldIndex constructs a `<Class>.<member>` presence set
// sourced from Pass 1 entity records rather than re-parsing source
// (generalised from issue #2352).
//
// Parameters:
//   - path: when non-empty, only records whose SourceFile equals path are
//     considered (records with an empty SourceFile are always included as
//     a defensive allowance for in-memory test fixtures).
//   - pass1Entities: the flat slice of Pass-1 records for the scope.
//   - pred: a caller-supplied predicate that decides whether a given record
//     should be indexed. The predicate runs AFTER the path filter. Pass a
//     nil pred to accept every record that passes the path filter.
//
// The predicate pattern lets different consumers (Django ORM fields, future
// SQLAlchemy columns, non-ORM entity kinds, …) reuse the same index
// machinery without copy-pasting the path-filter loop. Example:
//
//	// Accept only Python ORM SCOPE.Schema(subtype=field) records:
//	buildPlumbedFieldIndex(path, records, isPythonORMField)
//
//	// Accept every record that passes the path filter (useful in tests):
//	buildPlumbedFieldIndex(path, records, nil)
//
// Entries with Name lacking the dot convention (`<Class>.<member>`) are
// skipped silently — preserving the byte-identical key shape the Python
// extractor emits at python/extractor.go:1411.
//
// Returns an empty map (never nil) when no records match; callers treat
// an empty result as "side-channel cold, use fallback".
func buildPlumbedFieldIndex(
	path string,
	pass1Entities []types.EntityRecord,
	pred func(types.EntityRecord) bool,
) map[string]bool {
	out := map[string]bool{}
	if len(pass1Entities) == 0 {
		return out
	}
	for _, e := range pass1Entities {
		if e.SourceFile != "" && path != "" && e.SourceFile != path {
			continue
		}
		if pred != nil && !pred(e) {
			continue
		}
		if !strings.Contains(e.Name, ".") {
			continue
		}
		out[e.Name] = true
	}
	return out
}

// isPythonORMField is the predicate that restricts buildPlumbedFieldIndex
// to Django / Python-ORM SCOPE.Schema(subtype=field) entities — the only
// record kind the original hardcoded filter accepted (issue #2431).
//
// It is exported as a package-level variable so tests and future wrappers
// can compose it without repeating the Kind/Subtype literals.
var isPythonORMField = func(e types.EntityRecord) bool {
	return e.Kind == "SCOPE.Schema" && e.Subtype == "field"
}

// buildPlumbedPythonORMFieldIndex is a thin wrapper around
// buildPlumbedFieldIndex that preserves the pre-#2431 call-site API used
// by orm_field_edges.go. It hard-wires the isPythonORMField predicate so
// existing callers need no changes.
//
// New consumers that need a different record type should call
// buildPlumbedFieldIndex directly with their own predicate.
func buildPlumbedPythonORMFieldIndex(path string, pass1Entities []types.EntityRecord) map[string]bool {
	return buildPlumbedFieldIndex(path, pass1Entities, isPythonORMField)
}
