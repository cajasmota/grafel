package enrichers

// CouplingScoreEnricher computes Ca, Ce, and instability per SCOPE.Component entity.
// Port of Python coupling_score_enricher.py (MX-691).

import (
	"fmt"
	"math"

	"github.com/cajasmota/archigraph/internal/types"
)

var couplingRelTypes = map[string]bool{
	"DEPENDS_ON": true,
	"CALLS":      true,
}

// EnrichCouplingScore computes Ca/Ce/instability for SCOPE.Component entities.
func EnrichCouplingScore(entities []types.EntityRecord) []types.EntityRecord {
	refToIdx := make(map[string]int)
	for i := range entities {
		if entities[i].Kind == "SCOPE.Component" {
			refToIdx[entities[i].ID] = i
		}
	}
	if len(refToIdx) == 0 {
		return entities
	}
	ca := make(map[int]int)
	ce := make(map[int]int)
	for i := range entities {
		for _, rel := range entities[i].Relationships {
			if !couplingRelTypes[rel.Kind] {
				continue
			}
			if srcIdx, ok := refToIdx[entities[i].ID]; ok {
				ce[srcIdx]++
			}
			if tgtIdx, ok := refToIdx[rel.ToID]; ok {
				ca[tgtIdx]++
			}
		}
	}
	for _, idx := range refToIdx {
		e := &entities[idx]
		caVal := ca[idx]
		ceVal := ce[idx]
		total := caVal + ceVal
		instability := 0.0
		if total > 0 {
			instability = math.Round(float64(ceVal)/float64(total)*100) / 100
		}
		if e.Properties == nil {
			e.Properties = make(map[string]string)
		}
		e.Properties["ca"] = fmt.Sprintf("%d", caVal)
		e.Properties["ce"] = fmt.Sprintf("%d", ceVal)
		e.Properties["instability"] = fmt.Sprintf("%.2f", instability)
		e.Properties["coupling_computed"] = "true"
	}
	return entities
}
