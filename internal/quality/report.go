package quality

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// JSONReport is the machine-readable shape emitted by `grafel quality
// --json`. It is intentionally flat so CI dashboards / regression diff
// scripts can aggregate without depending on the in-process Report type.
type JSONReport struct {
	Fixture                    string  `json:"fixture"`
	EntityExpected             int     `json:"entity_expected"`
	EntityFound                int     `json:"entity_found"`
	EntityRecall               float64 `json:"entity_recall"`
	EntityExtractedTotal       int     `json:"entity_extracted_total"`
	RelationshipExpected       int     `json:"relationship_expected"`
	RelationshipFound          int     `json:"relationship_found"`
	RelationshipRecall         float64 `json:"relationship_recall"`
	RelationshipExtractedTotal int     `json:"relationship_extracted_total"`
	ForbiddenHits              int     `json:"forbidden_hits"`
	// ForbiddenEntityHits is the entity analogue (#6488 arm B), counted under
	// its own key rather than folded into forbidden_hits: the existing key
	// means "false-positive EDGES" to every baseline and dashboard that reads
	// it, and widening its meaning in place would move a number nobody asked
	// to move. Not omitempty — a consumer must be able to tell "zero hits"
	// from "this report predates the field".
	ForbiddenEntityHits int `json:"forbidden_entity_hits"`
	NiceEntityFound     int `json:"nice_entity_found"`
	NiceEntityTotal     int `json:"nice_entity_total"`
	NiceRelFound        int `json:"nice_relationship_found"`
	NiceRelTotal        int `json:"nice_relationship_total"`

	// Per-item details so a human can see WHICH expectations missed.
	MissingEntities      []missingEntity       `json:"missing_entities,omitempty"`
	MissingRelationships []missingRelationship `json:"missing_relationships,omitempty"`
	Forbidden            []missingRelationship `json:"forbidden,omitempty"`
	ForbiddenEntities    []forbiddenEntity     `json:"forbidden_entities,omitempty"`
}

// forbiddenEntity is the serialised shape of a ForbiddenEntityHit. It reports
// the row's axes AND the offending entity's own name/file/subtype, because a
// row may narrow on fewer axes than the entity carries — "a forbidden entity
// was found" is not a diagnosis anyone can act on.
type forbiddenEntity struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	File       string `json:"source_file,omitempty"`
	Subtype    string `json:"subtype,omitempty"`
	GotName    string `json:"got_name,omitempty"`
	GotFile    string `json:"got_source_file,omitempty"`
	GotSubtype string `json:"got_subtype,omitempty"`
}

type missingEntity struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"source_file,omitempty"`
	// Subtype is the row's asserted subtype (#6488), and GotSubtype the
	// subtype of the entity that matched on every other axis and was rejected
	// on this one. Both omitempty, so every pre-#6488 report stays
	// byte-identical: no row in the golden set states a subtype.
	Subtype    string `json:"subtype,omitempty"`
	GotSubtype string `json:"got_subtype,omitempty"`
}

type missingRelationship struct {
	From         string `json:"from"`
	FromKind     string `json:"from_kind,omitempty"`
	Kind         string `json:"kind"`
	To           string `json:"to"`
	ToKind       string `json:"to_kind,omitempty"`
	FromResolved bool   `json:"from_resolved"`
	ToResolved   bool   `json:"to_resolved"`
	// ToBareNameIsEntity mirrors RelationshipResult.ToBareNameIsEntity
	// (#6476). It is serialised because to_resolved is the ONE JSON key this
	// change moves, and without this key a consumer reading to_resolved:true
	// still concludes "both endpoints resolved, so the extractor dropped the
	// edge" for exactly the rows proven unsatisfiable. omitempty keeps every
	// pre-existing report byte-identical: the key appears only on flagged rows.
	ToBareNameIsEntity bool `json:"to_bare_name_is_entity,omitempty"`
	// FromFileMatchedNothing / ToFileMatchedNothing mirror the same-named
	// RelationshipResult fields. Serialised for the same reason
	// to_bare_name_is_entity is: without them a consumer reading
	// from_resolved:false / to_resolved:false concludes the extractor dropped
	// an entity it in fact extracted. omitempty keeps every pre-existing
	// report byte-identical — the keys appear only on flagged rows, and the
	// golden set has none.
	FromFileMatchedNothing bool `json:"from_file_matched_nothing,omitempty"`
	ToFileMatchedNothing   bool `json:"to_file_matched_nothing,omitempty"`
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
		ForbiddenEntityHits:        len(r.ForbiddenEntityHits),
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
			Name:       er.Expected.Name,
			Kind:       er.Expected.Kind,
			File:       er.Expected.SourceFile,
			Subtype:    er.Expected.Subtype,
			GotSubtype: er.GotSubtype,
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
			From:                   rr.Expected.FromName,
			FromKind:               rr.Expected.FromKind,
			Kind:                   rr.Expected.Kind,
			To:                     to,
			ToKind:                 rr.Expected.ToKind,
			FromResolved:           rr.FromResolved,
			ToResolved:             rr.ToResolved,
			ToBareNameIsEntity:     rr.ToBareNameIsEntity,
			FromFileMatchedNothing: rr.FromFileMatchedNothing,
			ToFileMatchedNothing:   rr.ToFileMatchedNothing,
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
	for _, fh := range r.ForbiddenEntityHits {
		jr.ForbiddenEntities = append(jr.ForbiddenEntities, forbiddenEntity{
			Name:       fh.Expected.Name,
			Kind:       fh.Expected.Kind,
			File:       fh.Expected.SourceFile,
			Subtype:    fh.Expected.Subtype,
			GotName:    fh.MatchedName,
			GotFile:    fh.MatchedSourceFile,
			GotSubtype: fh.MatchedSubtype,
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
	fmt.Fprintf(w, "  forbidden entity hits: %d  (false-positive entities; target=0)\n",
		len(r.ForbiddenEntityHits))
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
			// A subtype miss reads differently from an absence: the entity IS
			// in the graph, wearing the wrong subtype. Say so rather than let
			// the line be read as "the extractor produced nothing" (#6488).
			why := ""
			if er.SubtypeMismatch {
				why = fmt.Sprintf(" — extracted with subtype %q, expected %q",
					er.GotSubtype, er.Expected.Subtype)
			} else if er.Expected.Subtype != "" {
				why = fmt.Sprintf(" — expected subtype %q", er.Expected.Subtype)
			}
			fmt.Fprintf(w, "    - %s [%s]%s%s\n", er.Expected.Name, er.Expected.Kind, loc, why)
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
			var badPaths []string
			if rr.FromFileMatchedNothing {
				badPaths = append(badPaths, fmt.Sprintf("from_file %q", rr.Expected.FromFile))
			}
			if rr.ToFileMatchedNothing {
				badPaths = append(badPaths, fmt.Sprintf("to_file %q", rr.Expected.ToFile))
			}
			diag := ""
			switch {
			// FIRST, ahead of every missing-endpoint arm (#6464 follow-up).
			// A wrong path makes its endpoint unresolved, so this row also
			// satisfies `!FromResolved` / `!ToResolved`; those arms say "…
			// -entity not extracted", which is FALSE here — the entity was
			// extracted, under a different path. Placing this arm behind them
			// is not a stylistic choice: it silently restores the misdiagnosis
			// the arm exists to remove, and it does so on the single most
			// likely authoring mistake (a mistyped path). Pinned by
			// TestFileNarrowingArmMustOutrankTheNotExtractedArms_6464.
			case len(badPaths) > 0:
				diag = fmt.Sprintf("  (root cause: FIXTURE ROW — %s names a path no such entity"+
					" is under; the entity IS extracted, in another file, so this row can never"+
					" match; fix the path)", strings.Join(badPaths, " and "))
			case !rr.FromResolved && !rr.ToResolved:
				diag = "  (root cause: NEITHER endpoint extracted)"
			case !rr.FromResolved:
				diag = "  (root cause: from-entity not extracted)"
			case !rr.ToResolved:
				diag = "  (root cause: to-entity not extracted)"
			// LAST before the default, and deliberately BEHIND every
			// missing-endpoint arm (#6476 round 2). The advice this arm gives
			// — "use to_name + to_kind" — only repairs the TO side. On a row
			// whose FROM endpoint was never extracted, following it leaves the
			// row still missing, which is the same "points the reader at the
			// wrong thing" defect this arm exists to remove, relocated one step
			// over. When an endpoint is absent, that is the first thing to fix;
			// this arm refines the "both endpoints exist" default, so it sits
			// immediately in front of it and nowhere else.
			//
			// See TestBareNameRowWithUnresolvedFromReportsTheFromSide_6476.
			case rr.ToBareNameIsEntity:
				diag = fmt.Sprintf("  (root cause: FIXTURE ROW — to_bare_name %q is the NAME of an"+
					" extracted entity, whose real ID is a content hash; this row can only match a"+
					" stub edge emitted with that literal string as its ToID; use to_name + to_kind)",
					rr.Expected.ToBareName)
			default:
				diag = "  (both endpoints exist; edge not emitted)"
			}
			fmt.Fprintf(w, "    - %s --[%s]--> %s%s\n",
				rr.Expected.FromName, rr.Expected.Kind, to, diag)
		}
	}

	if len(r.ForbiddenEntityHits) > 0 {
		fmt.Fprintln(w, "  FORBIDDEN entities present (extractor false-positives):")
		for _, fh := range r.ForbiddenEntityHits {
			// The OFFENDING entity, not the row: a row narrowing on fewer axes
			// than the entity carries would otherwise leave the reader
			// guessing which of several candidates fired.
			sub := ""
			if fh.MatchedSubtype != "" {
				sub = " subtype=" + fh.MatchedSubtype
			}
			loc := ""
			if fh.MatchedSourceFile != "" {
				loc = " in " + fh.MatchedSourceFile
			}
			fmt.Fprintf(w, "    - %s [%s]%s%s (id=%s)\n",
				fh.MatchedName, fh.MatchedKind, loc, sub, fh.MatchedID)
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
