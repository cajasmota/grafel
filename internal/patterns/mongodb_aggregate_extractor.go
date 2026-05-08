package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// mongodbAggregateExtractor detects MongoDB aggregation pipeline stages.
// Matches Python mongodb_aggregate_extractor.py.
type mongodbAggregateExtractor struct{}

var (
	mbaGenericAggRE       = regexp.MustCompile(`\.aggregate\s*\(`)
	mbaStageKeyQuotedRE   = regexp.MustCompile(`["'](\$\w+)["']`)
	mbaStageKeyUnquotedRE = regexp.MustCompile(`\{?\s*(\$\w+)\s*:`)
)

func (m *mongodbAggregateExtractor) Category() string { return "mongodb_aggregate_pipeline" }

func (m *mongodbAggregateExtractor) AppliesTo(src string) bool {
	return mbaGenericAggRE.MatchString(src) && strings.Contains(src, "$")
}

func (m *mongodbAggregateExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	for idx, match := range mbaGenericAggRE.FindAllStringIndex(src, -1) {
		// Find pipeline stages after .aggregate(
		end := match[1]
		context := src[end:]
		if len(context) > 500 {
			context = context[:500]
		}

		stages := make([]string, 0)
		for _, sm := range mbaStageKeyQuotedRE.FindAllStringSubmatch(context, -1) {
			stages = append(stages, sm[1])
		}
		for _, sm := range mbaStageKeyUnquotedRE.FindAllStringSubmatch(context, -1) {
			stages = append(stages, sm[1])
		}

		// Dedup stages
		stageSet := map[string]bool{}
		uniqueStages := []string{}
		for _, s := range stages {
			if !stageSet[s] {
				stageSet[s] = true
				uniqueStages = append(uniqueStages, s)
			}
		}

		key := fmt.Sprintf("aggregate:%d", idx)
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("mongodb_aggregate_%d", idx),
			"SCOPE.Operation", "mongodb_aggregate_pipeline", language,
			lineOf(src, match[0]),
			map[string]string{
				"kind":   "mongodb_aggregate_pipeline",
				"stages": strings.Join(uniqueStages, ","),
			}))
	}

	return results
}

func init() {
	Register(&mongodbAggregateExtractor{})
}
