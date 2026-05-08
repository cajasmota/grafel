package enrichers

// LayerClassifierEnricher identifies architectural layers for entities.
// Port of Python layer_classifier_enricher.py (MX-684).
// Heuristic path-based classification only — LLM path not ported.

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// LayerClassifyResult holds the layer classification output.
type LayerClassifyResult struct {
	Layer      string
	Confidence string
}

var layerHeuristicRules = []struct {
	Layer    string
	Keywords []string
}{
	{"controller", []string{"controller", "handler", "rest", "api", "resource"}},
	{"service", []string{"service", "business", "logic", "usecase", "application"}},
	{"repository", []string{"repository", "repo", "dao", "store", "persistence"}},
	{"domain", []string{"domain", "model", "entity", "aggregate", "valueobject"}},
	{"infrastructure", []string{"infra", "infrastructure", "adapter", "config", "configuration"}},
}

// ClassifyLayer applies path heuristics to determine the architectural layer.
func ClassifyLayer(filePath string) LayerClassifyResult {
	lower := strings.ToLower(filePath)
	for _, rule := range layerHeuristicRules {
		for _, kw := range rule.Keywords {
			if strings.Contains(lower, kw) {
				return LayerClassifyResult{Layer: rule.Layer, Confidence: "high"}
			}
		}
	}
	return LayerClassifyResult{Layer: "unknown", Confidence: "unknown"}
}

// EnrichLayerClassifier applies layer classification to all entities.
func EnrichLayerClassifier(entities []types.EntityRecord) []types.EntityRecord {
	for i := range entities {
		e := &entities[i]
		result := ClassifyLayer(e.SourceFile)
		if e.Metadata == nil {
			e.Metadata = make(map[string]interface{})
		}
		e.Metadata["layer"] = result.Layer
		e.Metadata["layer_confidence"] = result.Confidence
	}
	return entities
}
