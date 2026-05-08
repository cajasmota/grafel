package enrichers

// LibBoundaryEnricher classifies DEPENDS_ON edge targets as first_party or third_party.
// Port of Python lib_boundary_enricher.py (MX-690).

import "strings"

// DependsOnEdge is a DEPENDS_ON edge from dependency_extractor / import_extractor.
type DependsOnEdge struct {
	EdgeID            string
	SourceEntityID    string
	TargetPackageName string
	TargetVersion     string
}

// BoundaryAnnotation is the classification result for a single DEPENDS_ON edge.
type BoundaryAnnotation struct {
	EdgeID        string
	PackageName   string
	Boundary      string
	MatchedPrefix string
}

// AnnotateLibBoundaries classifies edge targets using org-configured namespace prefixes.
func AnnotateLibBoundaries(edges []DependsOnEdge, firstPartyNamespaces []string) []BoundaryAnnotation {
	var annotations []BoundaryAnnotation
	for _, edge := range edges {
		pkg := edge.TargetPackageName
		if pkg == "" {
			continue
		}
		matched := ""
		for _, prefix := range firstPartyNamespaces {
			if strings.HasPrefix(pkg, prefix) {
				matched = prefix
				break
			}
		}
		boundary := "third_party"
		if matched != "" {
			boundary = "first_party"
		}
		annotations = append(annotations, BoundaryAnnotation{
			EdgeID:        edge.EdgeID,
			PackageName:   pkg,
			Boundary:      boundary,
			MatchedPrefix: matched,
		})
	}
	return annotations
}
