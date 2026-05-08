// Package csharp provides regex-based custom extractors for C# source files.
// Each extractor targets a specific framework and registers via init().
package csharp

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

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

func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}

// csharpPrimitives are C# built-in types that should not be emitted as schema entities.
var csharpPrimitives = map[string]bool{
	"string": true, "int": true, "long": true, "double": true, "float": true,
	"bool": true, "char": true, "byte": true, "short": true, "void": true,
	"object": true, "decimal": true, "uint": true, "ulong": true,
	"String": true, "Int32": true, "Int64": true, "Boolean": true,
	"IActionResult": true, "ActionResult": true, "Task": true,
	"IEnumerable": true, "List": true, "IList": true, "Array": true,
	"Ok": true, "NotFound": true, "BadRequest": true, "Unauthorized": true,
}
