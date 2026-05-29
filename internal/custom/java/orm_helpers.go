package java

import (
	"github.com/cajasmota/archigraph/internal/types"
)

// makeEntity builds a minimal EntityRecord with all required fields set.
// Mirrors the helper used by the registered JS/Kotlin custom extractors so the
// Java ORM extractors (Ebean / EclipseLink / MyBatis) emit registry-shaped
// records via the custom_java_* dispatch path.
func makeEntity(name, kind, subtype, filePath, language string, lineNum int) types.EntityRecord {
	e := types.EntityRecord{
		Name:             name,
		Kind:             kind,
		Subtype:          subtype,
		SourceFile:       filePath,
		StartLine:        lineNum,
		EndLine:          lineNum,
		Language:         language,
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Properties: map[string]string{
			"kind":    kind,
			"subtype": subtype,
		},
	}
	e.ID = e.ComputeID()
	return e
}

// setProps merges extra key-value pairs into entity.Properties.
func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}
