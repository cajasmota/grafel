// Package php provides regex-based custom extractors for PHP source files.
// Each extractor targets a specific framework and registers via init().
package php

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

func makeEntity(name, kind, subtype, filePath, language string, lineNum int) types.EntityRecord {
	types.ValidateProducedEntityKind("internal/custom/php.makeEntity", kind)
	e := types.EntityRecord{
		Name:             name,
		Kind:             kind,
		Subtype:          subtype,
		SourceFile:       filePath,
		StartLine:        lineNum,
		EndLine:          lineNum,
		Language:         language,
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Properties: map[string]string{
			"kind":    kind,
			"subtype": subtype,
		},
	}
	e.ID = e.ComputeID()
	return e
}

func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}

// --- Endpoint deprecation contract (epic #3628) -----------------------------
//
// The flagship engine deprecation pass (internal/engine/http_endpoint_deprecation.go)
// stamps a flat property contract on synthesised http_endpoint_definition
// endpoints. Symfony `#[Route]` and API Platform `#[ApiResource]` endpoints are
// SCOPE.Operation entities emitted by these custom extractors instead, so the
// engine pass cannot reach them. To keep the PHP deprecation surface complete and
// consistent, the custom extractors stamp the IDENTICAL property contract at the
// source from their own framework idioms (Symfony `deprecated: true` /
// `@deprecated` PHPDoc, API Platform `deprecationReason`).
//
// Property contract (mirrors the flagship exactly):
//
//	deprecated             — "true" (present only when a marker was found)
//	deprecated_since       — version/date from the marker, when available
//	deprecated_replacement — the suggested replacement, when the marker names one
//	deprecation_source     — the signal that fired (evidence)

// depSinceRe extracts a "since X" / "as of X" version/date from a free-text
// deprecation message (mirrors the flagship depSinceRe).
var depSinceRe = regexp.MustCompile(`(?i)\b(?:since|as of)\s+([vV]?\d[\w.\-]*)`)

// depReplacementRe extracts a "use X instead" / "replaced by X" hint (mirrors the
// flagship depReplacementRe).
var depReplacementRe = regexp.MustCompile("(?i)\\b(?:use|replaced by|prefer|see)\\s+`?([A-Za-z0-9_./{}\\-]+)`?")

// parseDeprecationMessage pulls an optional since-version and replacement hint out
// of a free-text deprecation message. Both are honest-partial: an absent signal
// yields an empty string (never a fabricated value). Mirrors the flagship parser.
func parseDeprecationMessage(msg string) (since, replacement string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", ""
	}
	if m := depSinceRe.FindStringSubmatch(msg); m != nil {
		since = m[1]
	}
	if m := depReplacementRe.FindStringSubmatch(msg); m != nil {
		replacement = strings.TrimSuffix(m[1], ".")
	}
	return since, replacement
}

// stampDeprecation writes the flat deprecation contract onto an endpoint entity.
// since/replacement are honest-partial (omitted when empty). No-op when source is
// empty, so callers can guard-call without a found check.
func stampDeprecation(e *types.EntityRecord, source, since, replacement string) {
	if source == "" {
		return
	}
	setProps(e, "deprecated", "true", "deprecation_source", source)
	if since != "" {
		setProps(e, "deprecated_since", since)
	}
	if replacement != "" {
		setProps(e, "deprecated_replacement", replacement)
	}
}
