package quality

import (
	"encoding/json"
	"fmt"
	"io"
)

// JSONReport is the machine-readable shape emitted by `archigraph quality
// --json`. It is intentionally flat so CI dashboards / regression diff
// scripts can aggregate without depending on the in-process Report type.
type JSONReport struct {
	Fixture                  string  `json:"fixture"`
	EntityExpected           int     `json:"entity_expected"`
	EntityFound              int     `json:"entity_found"`
	EntityRecall             float64 `json:"entity_recall"`
	EntityExtractedTotal     int     `json:"entity_extracted_total"`
	RelationshipExpected     int     `json:"relationship_expected"`
	RelationshipFound        int     `json:"relationship_found"`
	RelationshipRecall       float64 `json:"relationship_recall"`
	RelationshipExtractedTotal int   `json:"relationship_extracted_total"`
	ForbiddenHits            int     `json:"forbidden_hits"`
	NiceEntityFound          int     `json:"nice_entity_found"`
	NiceEntityTotal          int     `json:"nice_entity_total"`
	NiceRelFound             int     `json:"nice_relationship_found"`
	NiceRelTotal             int     `json:"nice_relationship_total"`

	// Per-item details so a human can see WHICH expectations missed.
	MissingEntities      []missingEntity      `json:"missing_entities,omitempty"`
	MissingRelationships []missingRelationship `json:"missing_relationships,omitempty"`
	Forbidden            []missingRelationship `json:"forbidden,omitempty"`
}

type missingEntity struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"source_file,omitempty"`
}

type missingRelationship struct {
	From         string `json:"from"`
	FromKind     string `json:"from_kind,omitempty"`
	Kind         string `json:"kind"`
	To           string `json:"to"`
	ToKind       string `json:"to_kind,omitempty"`
	FromResolved bool   `json:"from_resolved"`
	ToResolved   bool   `json:"to_resolved"`
}

// ToJSON converts an in-memory Report to its persisted shape.
func (r *Report) ToJSON() *JSONReport {
	jr := &JSONReport{
		Fixture:                    r.FixtureName,
		EntityExpected:             r.EntityExpected,
		EntityFound:                r.EntityFound,
		EntityRecall:               r.EntityRecall(),
		EntityExtractedTotal:       r.EntityExtractedN,
		RelationshipExpected:       r.RelExpected,
		RelationshipFound:          r.RelFound,
		RelationshipRecall:         r.RelationshipRecall(),
		RelationshipExtractedTotal: r.RelExtractedN,
		ForbiddenHits:              len(r.ForbiddenHits),
		NiceEntityFound:            r.NiceEntityFound,
		NiceEntityTotal:            r.NiceEntityTotal,
		NiceRelFound:               r.NiceRelFound,
		NiceRelTotal:               r.NiceRelTotal,
	}
	for _, er := range r.EntityResults {
		if er.Found || er.Expected.NiceToHave || !er.Expected.MustExist {
			continue
		}
		jr.MissingEntities = append(jr.MissingEntities, missingEntity{
			Name: er.Expected.Name,
			Kind: er.Expected.Kind,
			File: er.Expected.SourceFile,
		})
	}
	for _, rr := range r.RelResults {
		if rr.Found || rr.Expected.NiceToHave || !rr.Expected.MustExist {
			continue
		}
		to := rr.Expected.ToName
		if to == "" {
			to = rr.Expected.ToBareName
		}
		jr.MissingRelationships = append(jr.MissingRelationships, missingRelationship{
			From:         rr.Expected.FromName,
			FromKind:     rr.Expected.FromKind,
			Kind:         rr.Expected.Kind,
			To:           to,
			ToKind:       rr.Expected.ToKind,
			FromResolved: rr.FromResolved,
			ToResolved:   rr.ToResolved,
		})
	}
	for _, fh := range r.ForbiddenHits {
		to := fh.Expected.ToName
		if to == "" {
			to = fh.Expected.ToBareName
		}
		jr.Forbidden = append(jr.Forbidden, missingRelationship{
			From:     fh.Expected.FromName,
			FromKind: fh.Expected.FromKind,
			Kind:     fh.Expected.Kind,
			To:       to,
			ToKind:   fh.Expected.ToKind,
		})
	}
	return jr
}

// WriteHuman emits a multi-line human-readable summary to w. Intended for
// terminal output; the JSON shape above is the source of truth for
// machine consumers.
func (r *Report) WriteHuman(w io.Writer) {
	fmt.Fprintf(w, "fixture: %s\n", r.FixtureName)
	fmt.Fprintf(w, "  entities:      %d / %d expected  (recall=%s)  [extracted total: %d]\n",
		r.EntityFound, r.EntityExpected, pct(r.EntityRecall()), r.EntityExtractedN)
	fmt.Fprintf(w, "  relationships: %d / %d expected  (recall=%s)  [extracted total: %d]\n",
		r.RelFound, r.RelExpected, pct(r.RelationshipRecall()), r.RelExtractedN)
	fmt.Fprintf(w, "  forbidden hits: %d  (false-positive edges; target=0)\n", len(r.ForbiddenHits))
	if r.NiceEntityTotal+r.NiceRelTotal > 0 {
		fmt.Fprintf(w, "  nice-to-have:  entities %d/%d, relationships %d/%d\n",
			r.NiceEntityFound, r.NiceEntityTotal, r.NiceRelFound, r.NiceRelTotal)
	}

	// Missing entities.
	missEnts := 0
	for _, er := range r.EntityResults {
		if !er.Found && er.Expected.MustExist && !er.Expected.NiceToHave {
			missEnts++
		}
	}
	if missEnts > 0 {
		fmt.Fprintln(w, "  missing entities:")
		for _, er := range r.EntityResults {
			if er.Found || !er.Expected.MustExist || er.Expected.NiceToHave {
				continue
			}
			loc := ""
			if er.Expected.SourceFile != "" {
				loc = " in " + er.Expected.SourceFile
			}
			fmt.Fprintf(w, "    - %s [%s]%s\n", er.Expected.Name, er.Expected.Kind, loc)
		}
	}

	// Missing relationships, annotated with WHY (endpoint resolution).
	missRels := 0
	for _, rr := range r.RelResults {
		if !rr.Found && rr.Expected.MustExist && !rr.Expected.NiceToHave {
			missRels++
		}
	}
	if missRels > 0 {
		fmt.Fprintln(w, "  missing relationships:")
		for _, rr := range r.RelResults {
			if rr.Found || !rr.Expected.MustExist || rr.Expected.NiceToHave {
				continue
			}
			to := rr.Expected.ToName
			if to == "" {
				to = rr.Expected.ToBareName
			}
			diag := ""
			switch {
			case !rr.FromResolved && !rr.ToResolved:
				diag = "  (root cause: NEITHER endpoint extracted)"
			case !rr.FromResolved:
				diag = "  (root cause: from-entity not extracted)"
			case !rr.ToResolved:
				diag = "  (root cause: to-entity not extracted)"
			default:
				diag = "  (both endpoints exist; edge not emitted)"
			}
			fmt.Fprintf(w, "    - %s --[%s]--> %s%s\n",
				rr.Expected.FromName, rr.Expected.Kind, to, diag)
		}
	}

	if len(r.ForbiddenHits) > 0 {
		fmt.Fprintln(w, "  FORBIDDEN edges present (extractor false-positives):")
		for _, fh := range r.ForbiddenHits {
			to := fh.Expected.ToName
			if to == "" {
				to = fh.Expected.ToBareName
			}
			fmt.Fprintf(w, "    - %s --[%s]--> %s\n",
				fh.Expected.FromName, fh.Expected.Kind, to)
		}
	}
}

// WriteJSON emits the JSONReport to w with 2-space indent (matches the
// indexer's --json-stats convention).
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r.ToJSON())
}

func pct(v float64) string {
	return fmt.Sprintf("%.1f%%", v*100)
}
