// Package coverage ingests coverage reports (SonarQube model: archigraph does
// NOT execute tests — it parses the report CI already emits) and attaches real
// line-coverage to structural graph entities by source-span overlap.
//
// #5036 ships the LCOV, Cobertura and JaCoCo parsers + entity attribution as
// pure, table-tested transformations behind a shared ingestion entry point
// (ParseReport, parse.go). The dashboard overlay and the archigraph_coverage
// MCP query are deferred to follow-ups.
package coverage

import (
	"bufio"
	"io"
	"sort"
	"strconv"
	"strings"
)

// FuncCoverage is per-function coverage as reported by LCOV FN/FNDA records.
type FuncCoverage struct {
	Name string
	// Line is the declaration line of the function (FN:<line>,<name>).
	Line int
	// Hits is the execution count from FNDA:<hits>,<name>. -1 means the
	// function was declared (FN) but no FNDA record was seen.
	Hits int
}

// FileCoverage is the parsed coverage for a single source file (one SF: block).
type FileCoverage struct {
	// Path is the path exactly as it appeared in the SF: record (not yet
	// normalized). Use Normalize to turn it into a repo-relative path.
	Path string
	// LineHits maps a 1-based source line number to its execution count
	// (from DA:<line>,<hits>). A line absent from the map was not
	// instrumented and does not count toward totals.
	LineHits map[int]int
	// CoveredLines / TotalLines are the LH/LF summary values when present;
	// otherwise they are derived from LineHits.
	CoveredLines int
	TotalLines   int
	// Funcs is the per-function coverage, keyed implicitly by declaration order.
	Funcs []FuncCoverage
}

// Pct returns the line-coverage percentage in [0,100]. Zero instrumented lines
// yields 0.
func (f FileCoverage) Pct() float64 {
	if f.TotalLines == 0 {
		return 0
	}
	return 100.0 * float64(f.CoveredLines) / float64(f.TotalLines)
}

// Report is a parsed coverage report: a set of per-file coverage blocks.
//
// Source records which parser produced the report (SourceLCOV / SourceCobertura
// / SourceJaCoCo) so attribution stamps the correct coverage_source on entities.
// ParseLCOV sets it to SourceLCOV; the XML parsers set their own.
type Report struct {
	Files  []FileCoverage
	Source string
}

// ByPath returns the FileCoverage for the (raw, un-normalized) path, or nil.
func (r *Report) ByPath(path string) *FileCoverage {
	for i := range r.Files {
		if r.Files[i].Path == path {
			return &r.Files[i]
		}
	}
	return nil
}

// ParseLCOV parses a standard LCOV ".info" report.
//
// Recognized records (one per line, terminated by an "end_of_record" line):
//
//	SF:<path>            start of a file block
//	DA:<line>,<hits>     per-line execution count
//	FN:<line>,<name>     function declaration line
//	FNDA:<hits>,<name>   function execution count
//	LF:<n> / LH:<n>      line totals (found / hit)
//	BRDA / BRF / BRH     branch data (parsed-tolerant, currently ignored)
//	end_of_record        end of the file block
//
// Unknown records are ignored so the parser is tolerant of tool extensions
// (Istanbul, c8, nyc, genhtml, Go's gcov2lcov all emit supersets). When LF/LH
// are absent the totals are derived from the DA lines so a minimal report still
// yields correct percentages.
func ParseLCOV(r io.Reader) (*Report, error) {
	rep := &Report{Source: SourceLCOV}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var cur *FileCoverage
	// funcByName lets a later FNDA find the FN it belongs to.
	var funcByName map[string]int
	var haveLF, haveLH bool

	flush := func() {
		if cur == nil {
			return
		}
		if !haveLF {
			cur.TotalLines = len(cur.LineHits)
		}
		if !haveLH {
			covered := 0
			for _, h := range cur.LineHits {
				if h > 0 {
					covered++
				}
			}
			cur.CoveredLines = covered
		}
		rep.Files = append(rep.Files, *cur)
		cur = nil
		funcByName = nil
		haveLF, haveLH = false, false
	}

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case line == "end_of_record":
			flush()
		case strings.HasPrefix(line, "SF:"):
			flush()
			cur = &FileCoverage{
				Path:     strings.TrimPrefix(line, "SF:"),
				LineHits: map[int]int{},
			}
			funcByName = map[string]int{}
		case cur == nil:
			// Record outside any SF block — ignore.
			continue
		case strings.HasPrefix(line, "DA:"):
			ln, hits, ok := parseTwoInts(strings.TrimPrefix(line, "DA:"))
			if !ok {
				continue
			}
			// LCOV may emit the same DA line twice; keep the max hit count.
			if prev, exists := cur.LineHits[ln]; !exists || hits > prev {
				cur.LineHits[ln] = hits
			}
		case strings.HasPrefix(line, "FN:"):
			// FN:<line>,<name>  (name may itself contain commas)
			rest := strings.TrimPrefix(line, "FN:")
			comma := strings.IndexByte(rest, ',')
			if comma < 0 {
				continue
			}
			ln, err := strconv.Atoi(strings.TrimSpace(rest[:comma]))
			if err != nil {
				continue
			}
			name := rest[comma+1:]
			funcByName[name] = len(cur.Funcs)
			cur.Funcs = append(cur.Funcs, FuncCoverage{Name: name, Line: ln, Hits: -1})
		case strings.HasPrefix(line, "FNDA:"):
			// FNDA:<hits>,<name>
			rest := strings.TrimPrefix(line, "FNDA:")
			comma := strings.IndexByte(rest, ',')
			if comma < 0 {
				continue
			}
			hits, err := strconv.Atoi(strings.TrimSpace(rest[:comma]))
			if err != nil {
				continue
			}
			name := rest[comma+1:]
			if idx, ok := funcByName[name]; ok {
				cur.Funcs[idx].Hits = hits
			} else {
				cur.Funcs = append(cur.Funcs, FuncCoverage{Name: name, Line: 0, Hits: hits})
			}
		case strings.HasPrefix(line, "LF:"):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "LF:"))); err == nil {
				cur.TotalLines = n
				haveLF = true
			}
		case strings.HasPrefix(line, "LH:"):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "LH:"))); err == nil {
				cur.CoveredLines = n
				haveLH = true
			}
			// BRDA/BRF/BRH and other records are intentionally ignored in v1.
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	flush()

	// Stable ordering for deterministic downstream consumption.
	sort.Slice(rep.Files, func(i, j int) bool { return rep.Files[i].Path < rep.Files[j].Path })
	return rep, nil
}

// parseTwoInts parses "<a>,<b>" returning both ints. LCOV sometimes appends a
// third comma-separated checksum field on DA lines; it is ignored.
func parseTwoInts(s string) (int, int, bool) {
	parts := strings.SplitN(s, ",", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	a, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	b, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return a, b, true
}
