package coverage

import (
	"encoding/xml"
	"io"
	"sort"
)

// Cobertura XML (coverage.py, generic) document model. Only the line-coverage
// fields archigraph attributes from are mapped; everything else (rates,
// conditions, methods, branch data) is parsed-tolerant and ignored.
//
// Shape:
//
//	<coverage>
//	  <packages>
//	    <package>
//	      <classes>
//	        <class filename="src/calc.py">
//	          <lines>
//	            <line number="1" hits="5"/>
//	          </lines>
//	        </class>
type coberturaDoc struct {
	XMLName  xml.Name           `xml:"coverage"`
	Packages []coberturaPackage `xml:"packages>package"`
}

type coberturaPackage struct {
	Classes []coberturaClass `xml:"classes>class"`
}

type coberturaClass struct {
	Filename string          `xml:"filename,attr"`
	Lines    []coberturaLine `xml:"lines>line"`
}

type coberturaLine struct {
	Number int `xml:"number,attr"`
	Hits   int `xml:"hits,attr"`
}

// ParseCobertura parses a Cobertura XML coverage report into the same per-file
// line-coverage Report that ParseLCOV produces.
//
// A <line number= hits=> contributes one instrumented line: it counts toward
// TotalLines, and toward CoveredLines when hits > 0. Multiple <class> blocks for
// the same filename (Cobertura emits one class per top-level type, so a file
// with several classes appears repeatedly) are merged into a single
// FileCoverage; duplicate line numbers keep the maximum hit count, matching the
// LCOV duplicate-DA rule.
//
// Unknown elements/attributes are ignored so the parser tolerates the many
// Cobertura dialects (coverage.py, gocover-cobertura, generic). The returned
// Report carries Source = SourceCobertura so attribution stamps coverage_source
// correctly.
func ParseCobertura(r io.Reader) (*Report, error) {
	var doc coberturaDoc
	dec := xml.NewDecoder(r)
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}

	// Merge classes by filename. Cobertura emits one <class> per top-level
	// type, so a source file with several classes appears as several blocks
	// that must roll up into a single FileCoverage.
	byPath := map[string]*FileCoverage{}

	merge := func(c *coberturaClass) {
		if c.Filename == "" {
			return
		}
		fc := byPath[c.Filename]
		if fc == nil {
			fc = &FileCoverage{Path: c.Filename, LineHits: map[int]int{}}
			byPath[c.Filename] = fc
		}
		for _, ln := range c.Lines {
			if ln.Number <= 0 {
				continue
			}
			if prev, ok := fc.LineHits[ln.Number]; !ok || ln.Hits > prev {
				fc.LineHits[ln.Number] = ln.Hits
			}
		}
	}

	for pi := range doc.Packages {
		for ci := range doc.Packages[pi].Classes {
			merge(&doc.Packages[pi].Classes[ci])
		}
	}

	rep := &Report{Source: SourceCobertura}
	for _, fc := range byPath {
		fc.TotalLines = len(fc.LineHits)
		covered := 0
		for _, h := range fc.LineHits {
			if h > 0 {
				covered++
			}
		}
		fc.CoveredLines = covered
		rep.Files = append(rep.Files, *fc)
	}
	sort.Slice(rep.Files, func(i, j int) bool { return rep.Files[i].Path < rep.Files[j].Path })
	return rep, nil
}
