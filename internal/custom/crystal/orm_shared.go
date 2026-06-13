// orm_shared.go — shared helper for the Crystal ORM extractors (#4936).
//
// newCrystalSchema builds a SCOPE.Schema entity stamped with the given Crystal
// ORM framework + provenance, mirroring newGraniteSchema's shape so the four
// additional ORMs (Jennifer/Clear/Avram/Crecto) emit a uniform model/table/
// column/association entity shape.
package crystal

import "github.com/cajasmota/archigraph/internal/types"

// newCrystalSchema builds a SCOPE.Schema entity with the given framework +
// provenance stamp. The grain (subtype) is model/table/column/association.
func newCrystalSchema(name, subtype, framework, path string, line int, provenance string) types.EntityRecord {
	return types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Schema",
		Subtype:    subtype,
		SourceFile: path,
		Language:   "crystal",
		StartLine:  line,
		EndLine:    line,
		Properties: map[string]string{
			"framework":  framework,
			"provenance": provenance,
		},
	}
}
