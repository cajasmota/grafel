package enrichers

// ArchitectureClassifierEnricher classifies project architecture deterministically.
// Port of Python architecture_classifier_enricher.py (MX-689).
// LLM path not ported — deterministic fast-path only.

import "github.com/cajasmota/archigraph/internal/types"

// ArchClassificationInput holds signals for architecture classification.
type ArchClassificationInput struct {
	DockerComposeServiceCount int
	InterServiceCallCount     int
}

// ArchClassificationResult holds the deterministic classification output.
type ArchClassificationResult struct {
	ArchitectureType string
	IsFastPath       bool
}

const (
	ArchMonolith        = "monolith"
	ArchMicroservices   = "microservices"
	ArchModularMonolith = "modular-monolith"
	ArchUnknown         = "unknown"
)

const (
	DefaultMonolithComposeMax       = 1
	DefaultMonolithInterServiceMax  = 0
	DefaultMicroservicesComposeMin  = 5
	DefaultMicroservicesInterSvcMin = 3
)

// ClassifyArchitectureFastPath applies deterministic rules only.
func ClassifyArchitectureFastPath(input ArchClassificationInput) ArchClassificationResult {
	if input.DockerComposeServiceCount <= DefaultMonolithComposeMax &&
		input.InterServiceCallCount <= DefaultMonolithInterServiceMax {
		return ArchClassificationResult{ArchitectureType: ArchMonolith, IsFastPath: true}
	}
	if input.DockerComposeServiceCount >= DefaultMicroservicesComposeMin &&
		input.InterServiceCallCount >= DefaultMicroservicesInterSvcMin {
		return ArchClassificationResult{ArchitectureType: ArchMicroservices, IsFastPath: true}
	}
	return ArchClassificationResult{ArchitectureType: ArchUnknown, IsFastPath: false}
}

// EnrichArchitectureClassifier applies fast-path classification to SCOPE.Project entities.
func EnrichArchitectureClassifier(entities []types.EntityRecord) []types.EntityRecord {
	for i := range entities {
		e := &entities[i]
		if e.Kind != "SCOPE.Project" {
			continue
		}
		if e.Metadata == nil {
			continue
		}
		result := ClassifyArchitectureFastPath(ArchClassificationInput{
			DockerComposeServiceCount: metaInt(e.Metadata, "docker_compose_service_count"),
			InterServiceCallCount:     metaInt(e.Metadata, "inter_service_call_count"),
		})
		if result.IsFastPath {
			e.Metadata["architecture_type"] = result.ArchitectureType
		}
	}
	return entities
}

func metaInt(m map[string]interface{}, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	}
	return 0
}
