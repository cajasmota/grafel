package python

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_mongodb", &MongoDBExtractor{})
}

// MongoDBExtractor extracts MongoDB usage patterns: driver connections,
// aggregation pipelines, change streams, transactions, indexes, schema
// validation, GridFS, and Atlas Search.
type MongoDBExtractor struct{}

func (e *MongoDBExtractor) Language() string { return "python_mongodb" }

var (
	mgPyDriverRe = regexp.MustCompile(
		`(?:pymongo\.MongoClient|motor\.motor_asyncio\.AsyncIOMotorClient|motor\.motor_tornado\.MotorClient|MongoClient)\s*\(`)
	mgAggregateRe     = regexp.MustCompile(`(?:(?:\.|->)aggregate(?:ToStream)?(?:\s*<[^>]*>)?\s*\(|\baggregate\s+["\w])`)
	mgPipelineStageRe = regexp.MustCompile(
		`\$(?P<stage>match|group|sort|project|limit|skip|unwind|lookup|addFields|replaceRoot|count|bucket|bucketAuto|facet|graphLookup|merge|out|sortByCount|sample|geoNear|indexStats|collStats|planCacheStats|redact|set|unset)\b`)
	mgWatchRe        = regexp.MustCompile(`(?:\.|->)watch\s*\(`)
	mgWithTxRe       = regexp.MustCompile(`\.withTransaction\s*[(\{]`)
	mgStartSessionRe = regexp.MustCompile(`\.startSession\s*\(`)
	mgCreateIndexRe  = regexp.MustCompile(`\.createIndex(?:es)?\s*\(`)
	mgIndexTypeRe    = regexp.MustCompile(`(?i)["']?\$?(?P<idx_type>text|2dsphere|2d|hashed|compound|sparse|unique|ttl)\b["']?`)
	mgJSONSchemaRe   = regexp.MustCompile(`\$jsonSchema\b`)
	mgValidatorRe    = regexp.MustCompile(`["']?validator["']?\s*[:=]\s*\{`)
	mgGridFSBucketRe = regexp.MustCompile(`\bGridFSBucket\b`)
	mgGridFSOpRe     = regexp.MustCompile(
		`\.(?:open_upload_stream|upload_from_stream|open_download_stream|download_to_stream|uploadFromStream|downloadToStream|openUploadStream|openDownloadStream)\s*\(`)
	mgPyGridFSRe    = regexp.MustCompile(`\bgridfs\.(?:GridIn|GridOut|GridFS|open_upload_stream|open_download_stream)\b`)
	mgAtlasSearchRe = regexp.MustCompile(`\$search\b`)
	mgSearchIndexRe = regexp.MustCompile(`\.(?:createSearchIndex|updateSearchIndex|listSearchIndexes|dropSearchIndex)\s*\(`)
)

func (e *MongoDBExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_mongodb")
	_, span := tracer.Start(ctx, "custom.python_mongodb")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	var out []types.EntityRecord
	seenLines := make(map[string]map[int]bool)

	getSeen := func(category string) map[int]bool {
		if seenLines[category] == nil {
			seenLines[category] = make(map[int]bool)
		}
		return seenLines[category]
	}

	// 1. Driver connections
	for _, idx := range allMatchesIndex(mgPyDriverRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("driver")
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, entity(fmt.Sprintf("mongodb_driver:%s:%d", file.Path, line),
			"SCOPE.Service", "", file.Path, line,
			map[string]string{"framework": "mongodb", "pattern_type": "driver", "language": "python"}))
	}

	// 2. Aggregation pipelines
	for _, idx := range allMatchesIndex(mgAggregateRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("aggregation")
		if seen[line] {
			continue
		}
		seen[line] = true
		window := source[idx[0]:min(idx[0]+500, len(source))]
		stageMatches := mgPipelineStageRe.FindAllStringSubmatch(window, -1)
		var stages []string
		stageSeen := make(map[string]bool)
		for _, sm := range stageMatches {
			if !stageSeen[sm[1]] {
				stageSeen[sm[1]] = true
				stages = append(stages, sm[1])
			}
		}
		out = append(out, entity(fmt.Sprintf("mongodb_aggregation:%s:%d", file.Path, line),
			"SCOPE.Operation", "aggregation", file.Path, line,
			map[string]string{"framework": "mongodb", "pattern_type": "aggregation", "language": "python", "pipeline_stages": strings.Join(stages, ",")}))
	}

	// 3. Change streams
	for _, idx := range allMatchesIndex(mgWatchRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("change_stream")
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, entity(fmt.Sprintf("mongodb_change_stream:%s:%d", file.Path, line),
			"SCOPE.Operation", "change_stream", file.Path, line,
			map[string]string{"framework": "mongodb", "pattern_type": "change_stream", "language": "python"}))
	}

	// 4. Transactions
	txPatterns := []*regexp.Regexp{mgWithTxRe, mgStartSessionRe}
	for _, pat := range txPatterns {
		for _, idx := range allMatchesIndex(pat, source) {
			line := lineOf(source, idx[0])
			seen := getSeen("transaction")
			if seen[line] {
				continue
			}
			seen[line] = true
			out = append(out, entity(fmt.Sprintf("mongodb_transaction:%s:%d", file.Path, line),
				"SCOPE.Pattern", "", file.Path, line,
				map[string]string{"framework": "mongodb", "pattern_type": "transaction", "language": "python"}))
		}
	}

	// 5. Indexes
	for _, idx := range allMatchesIndex(mgCreateIndexRe, source) {
		line := lineOf(source, idx[0])
		seen := getSeen("index")
		if seen[line] {
			continue
		}
		seen[line] = true
		window := source[idx[0]:min(idx[0]+200, len(source))]
		idxType := "single"
		if itm := mgIndexTypeRe.FindStringSubmatch(window); itm != nil {
			idxType = strings.ToLower(itm[1])
		}
		out = append(out, entity(fmt.Sprintf("mongodb_index:%s:%d", file.Path, line),
			"SCOPE.Schema", "index", file.Path, line,
			map[string]string{"framework": "mongodb", "pattern_type": "index", "language": "python", "index_type": idxType}))
	}

	// 6. Schema validation
	valPatterns := []*regexp.Regexp{mgJSONSchemaRe, mgValidatorRe}
	for _, pat := range valPatterns {
		for _, idx := range allMatchesIndex(pat, source) {
			line := lineOf(source, idx[0])
			seen := getSeen("validation")
			if seen[line] {
				continue
			}
			seen[line] = true
			out = append(out, entity(fmt.Sprintf("mongodb_validation:%s:%d", file.Path, line),
				"SCOPE.Schema", "validation", file.Path, line,
				map[string]string{"framework": "mongodb", "pattern_type": "validation", "language": "python"}))
		}
	}

	// 7. GridFS
	gridfsPatterns := []*regexp.Regexp{mgGridFSBucketRe, mgGridFSOpRe, mgPyGridFSRe}
	for _, pat := range gridfsPatterns {
		for _, idx := range allMatchesIndex(pat, source) {
			line := lineOf(source, idx[0])
			seen := getSeen("gridfs")
			if seen[line] {
				continue
			}
			seen[line] = true
			out = append(out, entity(fmt.Sprintf("mongodb_gridfs:%s:%d", file.Path, line),
				"SCOPE.Operation", "gridfs", file.Path, line,
				map[string]string{"framework": "mongodb", "pattern_type": "gridfs", "language": "python"}))
		}
	}

	// 8. Atlas Search
	searchPatterns := []*regexp.Regexp{mgAtlasSearchRe, mgSearchIndexRe}
	for _, pat := range searchPatterns {
		for _, idx := range allMatchesIndex(pat, source) {
			line := lineOf(source, idx[0])
			seen := getSeen("atlas_search")
			if seen[line] {
				continue
			}
			seen[line] = true
			out = append(out, entity(fmt.Sprintf("mongodb_atlas_search:%s:%d", file.Path, line),
				"SCOPE.Operation", "atlas_search", file.Path, line,
				map[string]string{"framework": "mongodb", "pattern_type": "atlas_search", "language": "python"}))
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
