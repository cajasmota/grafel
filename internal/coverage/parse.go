package coverage

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// ParseReport is the single ingestion entry point shared by every coverage
// format. It dispatches to the LCOV / Cobertura / JaCoCo parser by `format`
// (one of the Format* constants); when `format` is empty it auto-detects from
// the report content (see detectFormat). The chosen parser sets Report.Source.
//
// This is the seam the indexer enrichment pass (enrich.go) drives so that adding
// a format never duplicates the attribution/enrich path — every parser produces
// the same per-file *Report.
func ParseReport(format string, r io.Reader) (*Report, error) {
	// Buffer the input so detection can sniff the head and the chosen parser can
	// still read the whole stream. Coverage reports are modest in size.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	f := strings.ToLower(strings.TrimSpace(format))
	if f == "" {
		f = detectFormat(data)
	}

	switch f {
	case FormatLCOV:
		return ParseLCOV(bytes.NewReader(data))
	case FormatCobertura:
		return ParseCobertura(bytes.NewReader(data))
	case FormatJaCoCo:
		return ParseJaCoCo(bytes.NewReader(data))
	default:
		return nil, fmt.Errorf("coverage: unrecognized report format %q", format)
	}
}

// detectFormat sniffs the report bytes to pick a parser when the group has not
// pinned Config.Format:
//
//   - XML reports are disambiguated by their root element: <coverage> →
//     Cobertura, <report> → JaCoCo (the JaCoCo root element).
//   - everything else falls back to LCOV, whose line-oriented "SF:/DA:" syntax
//     is not XML and is the historical default.
//
// Returns "" when nothing matches (ParseReport then errors).
func detectFormat(data []byte) string {
	head := bytes.TrimSpace(data)
	if len(head) == 0 {
		return ""
	}
	if head[0] == '<' {
		switch xmlRootElement(data) {
		case "coverage":
			return FormatCobertura
		case "report":
			return FormatJaCoCo
		default:
			return ""
		}
	}
	// Non-XML: LCOV is the only line-oriented format.
	return FormatLCOV
}

// xmlRootElement returns the local name of the first XML start element (the
// document root), skipping the prolog, comments, DOCTYPE and processing
// instructions. Returns "" if the stream has no start element.
func xmlRootElement(data []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local
		}
	}
}
