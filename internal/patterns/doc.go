// Package patterns implements language-agnostic pattern detectors for the
// archigraph pipeline.  Each detector is a pure regex-based scanner
// that runs on every file regardless of language (the extractor.Extractor
// interface covers language-specific AST extraction; this package covers
// cross-cutting semantic signals).
//
// There are 61 detectors matching Python parity.  Each file in this package
// defines exactly one detector and calls Register via init().
//
// Usage:
//
//	import _ "github.com/cajasmota/archigraph/internal/patterns"
//
//	detectors := patterns.All()
//	for _, d := range detectors {
//	    if d.AppliesTo(src) {
//	        entities := d.Detect(file, src)
//	        // process entities
//	    }
//	}
package patterns
