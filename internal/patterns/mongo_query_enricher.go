package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// mongoQueryEnricher detects MongoDB query patterns.
// Matches Python mongo_query_enricher.py.
type mongoQueryEnricher struct{}

var (
	mqFindRE       = regexp.MustCompile(`\b(\w+)\.find(?:One)?\s*\(`)
	mqInsertRE     = regexp.MustCompile(`\b(\w+)\.insert(?:One|Many)?\s*\(`)
	mqUpdateRE     = regexp.MustCompile(`\b(\w+)\.update(?:One|Many)?\s*\(`)
	mqDeleteRE     = regexp.MustCompile(`\b(\w+)\.delete(?:One|Many)?\s*\(`)
	mqAggregateRE  = regexp.MustCompile(`\b(\w+)\.aggregate\s*\(`)
	mqCollectionRE = regexp.MustCompile(`(?:db|client)\s*\.\s*(?:collection|getCollection)\s*\(\s*["']([^"']+)["']`)
)

var mongoImportTokens = []string{
	"pymongo", "motor", "MongoClient", "mongoose",
	"mongo-go-driver", "go.mongodb.org/mongo-driver",
	"com.mongodb", "mongodb",
}

func (m *mongoQueryEnricher) Category() string { return "mongo_query" }

func (m *mongoQueryEnricher) AppliesTo(src string) bool {
	srcLower := strings.ToLower(src)
	for _, tok := range mongoImportTokens {
		if strings.Contains(srcLower, strings.ToLower(tok)) {
			return true
		}
	}
	return mqCollectionRE.MatchString(src)
}

func (m *mongoQueryEnricher) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	ops := []struct {
		re  *regexp.Regexp
		op  string
	}{
		{mqFindRE, "find"},
		{mqInsertRE, "insert"},
		{mqUpdateRE, "update"},
		{mqDeleteRE, "delete"},
		{mqAggregateRE, "aggregate"},
	}

	for _, op := range ops {
		for idx, match := range op.re.FindAllStringSubmatchIndex(src, -1) {
			collection := src[match[2]:match[3]]
			key := fmt.Sprintf("%s:%s:%d", op.op, collection, idx)
			if seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, makeEntity(filePath,
				fmt.Sprintf("mongo_%s_%s_%d", op.op, collection, idx),
				"SCOPE.Operation", "mongo_query", language,
				lineOf(src, match[0]),
				map[string]string{
					"kind":       "mongo_query",
					"operation":  op.op,
					"collection": collection,
				}))
		}
	}

	return results
}

func init() {
	Register(&mongoQueryEnricher{})
}
