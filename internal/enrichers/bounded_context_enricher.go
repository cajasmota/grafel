package enrichers

// BoundedContextEnricher infers DDD bounded contexts from module structure.
// Port of Python bounded_context_enricher.py (MX-685).
// LLM path not ported — deterministic namespace grouping only.

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

const unambiguousMinEntities = 2

var topDomainSegments = map[string]bool{
	"com": true, "org": true, "io": true, "net": true, "uk": true,
	"de": true, "fr": true, "jp": true, "us": true, "co": true,
	"main": true, "java": true, "kotlin": true, "scala": true,
	"groovy": true, "python": true, "go": true,
}

var sourcePrefixes = []string{
	"src/main/java/", "src/main/kotlin/", "src/main/scala/", "src/main/groovy/",
	"src/", "lib/", "app/", "pkg/",
}

func extractTopLevelSegment(sourceFile, name string) string {
	if strings.Contains(name, ".") {
		parts := strings.Split(name, ".")
		if len(parts) >= 3 {
			return strings.ToLower(parts[2])
		}
		if len(parts) == 2 {
			return strings.ToLower(parts[0])
		}
	}
	if sourceFile == "" {
		return ""
	}
	norm := strings.ReplaceAll(sourceFile, "\\", "/")
	for _, prefix := range sourcePrefixes {
		if strings.HasPrefix(norm, prefix) {
			norm = norm[len(prefix):]
			break
		}
	}
	norm = strings.ReplaceAll(norm, "/", ".")
	parts := strings.Split(norm, ".")
	if len(parts) > 1 {
		last := parts[len(parts)-1]
		if len(last) <= 4 && isAlpha(last) {
			parts = parts[:len(parts)-1]
		}
	}
	i := 0
	for i < len(parts) && topDomainSegments[strings.ToLower(parts[i])] {
		i++
	}
	if i > 0 && i < len(parts)-1 {
		i++
	}
	for i < len(parts) {
		candidate := strings.ToLower(parts[i])
		if candidate != "" && !topDomainSegments[candidate] {
			return candidate
		}
		i++
	}
	return ""
}

func isAlpha(s string) bool {
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		default:
			return false
		}
	}
	return len(s) > 0
}

// EnrichBoundedContext applies deterministic namespace-based context grouping.
func EnrichBoundedContext(entities []types.EntityRecord) []types.EntityRecord {
	segments := make([]string, len(entities))
	for i := range entities {
		segments[i] = extractTopLevelSegment(entities[i].SourceFile, entities[i].Name)
	}
	segIndex := make(map[string][]int)
	for i, seg := range segments {
		if seg != "" {
			segIndex[seg] = append(segIndex[seg], i)
		}
	}
	contextAssigned := make(map[int]string)
	for seg, indices := range segIndex {
		if len(indices) >= unambiguousMinEntities {
			for _, idx := range indices {
				contextAssigned[idx] = seg
			}
		}
	}
	for i := range entities {
		e := &entities[i]
		if e.Metadata == nil {
			e.Metadata = make(map[string]interface{})
		}
		if ctx, ok := contextAssigned[i]; ok {
			e.Metadata["bounded_context"] = ctx
		} else {
			e.Metadata["bounded_context"] = "unknown"
		}
		if _, exists := e.Metadata["is_aggregate_root"]; !exists {
			e.Metadata["is_aggregate_root"] = false
		}
	}
	refToIdx := make(map[string]int)
	for i := range entities {
		refToIdx[entities[i].ID] = i
	}
	inbound := make(map[int]int)
	for i := range entities {
		for _, rel := range entities[i].Relationships {
			if rel.Kind != "CALLS" && rel.Kind != "DEPENDS_ON" {
				continue
			}
			targetIdx, ok := refToIdx[rel.ToID]
			if !ok || targetIdx == i {
				continue
			}
			srcCtx, _ := entities[i].Metadata["bounded_context"].(string)
			tgtCtx, _ := entities[targetIdx].Metadata["bounded_context"].(string)
			if srcCtx != tgtCtx {
				continue
			}
			inbound[targetIdx]++
		}
	}
	for i := range entities {
		e := &entities[i]
		if e.Kind != "class" && e.Kind != "interface" {
			continue
		}
		ctx, _ := e.Metadata["bounded_context"].(string)
		if ctx == "unknown" || ctx == "" {
			continue
		}
		if extractTopLevelSegment(e.SourceFile, e.Name) != ctx {
			continue
		}
		if inbound[i] >= 3 {
			e.Metadata["is_aggregate_root"] = true
		}
	}
	return entities
}

// GetBoundedContextSummary returns counts of distinct contexts and aggregate roots.
func GetBoundedContextSummary(entities []types.EntityRecord) (contextCount int, aggregateRootCount int) {
	contexts := make(map[string]bool)
	for _, e := range entities {
		if ctx, ok := e.Metadata["bounded_context"].(string); ok && ctx != "unknown" && ctx != "" {
			contexts[ctx] = true
		}
		if ar, ok := e.Metadata["is_aggregate_root"].(bool); ok && ar {
			aggregateRootCount++
		}
	}
	return len(contexts), aggregateRootCount
}

// ExtractTopLevelSegment is exported for testing.
func ExtractTopLevelSegment(sourceFile, name string) string {
	return extractTopLevelSegment(sourceFile, name)
}
