package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// validationConfidenceEnricher enriches entities with validation confidence scores.
// Matches Python validation_confidence_enricher.py.
type validationConfidenceEnricher struct{}

var vcSchemaFileTokens = []string{
	"schema", "model", "entity", "dto", "request", "response",
	"payload", "validator", "validation",
}

var (
	vcStripExtRE      = regexp.MustCompile(`\.[a-z]{1,5}$`)
	vcSchemaCommentRE = regexp.MustCompile(`(?i)@schema|@model|@entity|Schema\s*:`)
	vcValidateCallRE  = regexp.MustCompile(`(?:validate|is_valid|isValid|checkValidity)\s*\(`)
	vcReturnBoolRE    = regexp.MustCompile(`return\s+(?:True|False|true|false)`)
)

func isSchemaFile(filePath string) bool {
	base := vcStripExtRE.ReplaceAllString(filePath, "")
	base = strings.ToLower(base)
	for _, tok := range vcSchemaFileTokens {
		if strings.Contains(base, tok) {
			return true
		}
	}
	return false
}

func computeConfidence(filePath, src string) string {
	score := 0
	if isSchemaFile(filePath) {
		score += 2
	}
	if vcSchemaCommentRE.MatchString(src) {
		score++
	}
	if vcValidateCallRE.MatchString(src) {
		score += 2
	}
	if vcReturnBoolRE.MatchString(src) {
		score++
	}
	switch {
	case score >= 4:
		return "high"
	case score >= 2:
		return "medium"
	default:
		return "low"
	}
}

func (v *validationConfidenceEnricher) Category() string { return "validation_confidence" }

func (v *validationConfidenceEnricher) AppliesTo(src string) bool {
	return isSchemaFile("") || vcValidateCallRE.MatchString(src) || vcSchemaCommentRE.MatchString(src)
}

func (v *validationConfidenceEnricher) Detect(filePath, language, src string) []types.EntityRecord {
	confidence := computeConfidence(filePath, src)
	if confidence == "low" && !isSchemaFile(filePath) {
		return nil
	}

	return []types.EntityRecord{
		makeEntity(filePath,
			"validation_confidence_enrichment",
			"SCOPE.Pattern", "validation_confidence", language, 1,
			map[string]string{
				"kind":       "validation_confidence",
				"confidence": confidence,
			}),
	}
}

func init() {
	Register(&validationConfidenceEnricher{})
}
