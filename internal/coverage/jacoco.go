package coverage

import (
	"encoding/xml"
	"io"
	"path"
	"sort"
)

// JaCoCo XML (JVM) document model. JaCoCo reports per-line *instruction*
// coverage as ci (covered instructions) / mi (missed instructions) counts on
// each <line>; a line is "covered" when ci > 0. Branch counts (cb/mb) are
// parsed-tolerant and ignored.
//
// Shape:
//
//	<report>
//	  <package name="com/example">
//	    <sourcefile name="Calc.java">
//	      <line nr="1" mi="0" ci="3"/>
//	    </sourcefile>
//
// The graph stores source paths as repo-relative file paths, so the package
// name (a slash-separated directory) is joined with the sourcefile name to form
// the report path (e.g. "com/example/Calc.java"). Path normalization/matching
// against entity SourceFile happens downstream in Attribute via samePath, which
// already compares on basename suffixes.
type jacocoReport struct {
	XMLName  xml.Name        `xml:"report"`
	Packages []jacocoPackage `xml:"package"`
}

type jacocoPackage struct {
	Name        string             `xml:"name,attr"`
	SourceFiles []jacocoSourceFile `xml:"sourcefile"`
}

type jacocoSourceFile struct {
	Name  string       `xml:"name,attr"`
	Lines []jacocoLine `xml:"line"`
}

type jacocoLine struct {
	Nr int `xml:"nr,attr"`
	// Ci is covered instructions, Mi missed instructions. A line is covered
	// when Ci > 0.
	Ci int `xml:"ci,attr"`
	Mi int `xml:"mi,attr"`
}

// ParseJaCoCo parses a JaCoCo XML coverage report into the same per-file
// line-coverage Report that ParseLCOV produces.
//
// Each <line nr= ci= mi=> is one instrumented line: it counts toward TotalLines
// and, when ci > 0, toward CoveredLines. The report path is "<package
// name>/<sourcefile name>" so it carries enough directory context to match the
// graph's repo-relative SourceFile. Duplicate line numbers within a sourcefile
// keep the maximum covered-instruction count, mirroring the LCOV duplicate-DA
// rule.
//
// The DOCTYPE-declared external DTD JaCoCo emits is not fetched: the decoder is
// configured to leave entity resolution off (the default Strict decoder does
// not perform network access). Unknown elements/attributes are ignored. The
// returned Report carries Source = SourceJaCoCo.
func ParseJaCoCo(r io.Reader) (*Report, error) {
	var doc jacocoReport
	dec := xml.NewDecoder(r)
	// JaCoCo reports begin with a DOCTYPE referencing an external DTD. Make the
	// decoder tolerant of that without attempting any network fetch by leaving
	// Entity nil (default) — encoding/xml never resolves external DTDs.
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}

	byPath := map[string]*FileCoverage{}
	for pi := range doc.Packages {
		pkg := &doc.Packages[pi]
		for si := range pkg.SourceFiles {
			sf := &pkg.SourceFiles[si]
			if sf.Name == "" {
				continue
			}
			p := sf.Name
			if pkg.Name != "" {
				p = path.Join(pkg.Name, sf.Name)
			}
			fc := byPath[p]
			if fc == nil {
				fc = &FileCoverage{Path: p, LineHits: map[int]int{}}
				byPath[p] = fc
			}
			for _, ln := range sf.Lines {
				if ln.Nr <= 0 {
					continue
				}
				// Represent JaCoCo's instruction coverage as a hit count: ci
				// (covered instructions) so any value > 0 reads as "covered",
				// consistent with how LineHits is consumed (hits > 0 == covered).
				if prev, ok := fc.LineHits[ln.Nr]; !ok || ln.Ci > prev {
					fc.LineHits[ln.Nr] = ln.Ci
				}
			}
		}
	}

	rep := &Report{Source: SourceJaCoCo}
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
