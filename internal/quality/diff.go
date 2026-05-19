package quality

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// EntityResult records the outcome of evaluating one ExpectedEntity.
type EntityResult struct {
	Expected ExpectedEntity
	Found    bool
	// MatchedID is the Entity.ID we bound the expectation to (empty when
	// no match). Useful for debugging fixture authors.
	MatchedID string
}

// RelationshipResult records the outcome of evaluating one
// ExpectedRelationship.
//
// FromResolved / ToResolved report whether the expectation's endpoints
// could even be resolved to extracted entities — if not, the recall miss
// is most likely a missing ENTITY rather than a missing edge. The reporter
// surfaces this so fixture authors can fix the root cause.
type RelationshipResult struct {
	Expected     ExpectedRelationship
	Found        bool
	FromResolved bool
	ToResolved   bool
	// MatchedRelID is the Relationship.ID of the edge we matched, when one
	// was found. Empty otherwise.
	MatchedRelID string
}

// Report is the full diff between a Fixture and an extracted graph.Document.
type Report struct {
	FixtureName string

	// Entity scoring.
	EntityResults     []EntityResult
	EntityExpected    int // total must_exist
	EntityFound       int // must_exist AND found
	EntityExtractedN  int // |doc.Entities| — extra context, not in recall

	// Relationship scoring.
	RelResults    []RelationshipResult
	RelExpected   int
	RelFound      int
	RelExtractedN int // |doc.Relationships| — extra context

	// Forbidden-relationship hits — false positives. Each entry is an
	// extracted relationship that matches a `forbidden_relationships`
	// entry. A non-zero count is a hard quality regression.
	ForbiddenHits []RelationshipResult

	// Nice-to-have stats — surfaced separately so authors see what they
	// could add without being penalised on must-have recall.
	NiceEntityFound int
	NiceEntityTotal int
	NiceRelFound    int
	NiceRelTotal    int
}

// EntityRecall returns the recall ratio over MUST_EXIST entities. Returns 0
// when nothing is expected (the harness reports that as N/A).
func (r *Report) EntityRecall() float64 {
	if r.EntityExpected == 0 {
		return 0
	}
	return float64(r.EntityFound) / float64(r.EntityExpected)
}

// RelationshipRecall returns the recall ratio over MUST_EXIST relationships.
func (r *Report) RelationshipRecall() float64 {
	if r.RelExpected == 0 {
		return 0
	}
	return float64(r.RelFound) / float64(r.RelExpected)
}

// Evaluate diffs an extracted graph.Document against a Fixture and returns
// a Report. It does NOT mutate either argument.
//
// The matching strategy is deliberately forgiving: a single extracted edge
// satisfies any expected edge that resolves to the same (FromID, ToID,
// Kind) triple. This keeps fixtures small and authoring practical.
func Evaluate(fix *Fixture, doc *graph.Document) *Report {
	rep := &Report{
		FixtureName:      fix.Name,
		EntityExtractedN: len(doc.Entities),
		RelExtractedN:    len(doc.Relationships),
	}

	// Build entity lookup tables once. We index by:
	//   - (kind, name)              -> []*Entity (case-sensitive)
	//   - (kind, name, sourceFile)  -> *Entity (file-narrowed lookup)
	//   - qualified_name            -> *Entity
	//
	// Slice values rather than scalars because nothing in graph.Document
	// guarantees name+kind uniqueness for small fixtures (e.g. two `Meta`
	// inner classes).
	byKindName := make(map[string][]*graph.Entity)
	byKindNameFile := make(map[string]*graph.Entity)
	byQName := make(map[string]*graph.Entity)
	for k := range doc.Entities {
		e := &doc.Entities[k]
		kn := e.Kind + "\x00" + e.Name
		byKindName[kn] = append(byKindName[kn], e)
		byKindNameFile[kn+"\x00"+e.SourceFile] = e
		if e.QualifiedName != "" {
			byQName[e.QualifiedName] = e
		}
	}

	// Resolve each expected entity. We accept a hit when ANY extracted
	// entity matches — small fixtures with collisions can disambiguate
	// via the MatchBy / SourceFile fields.
	resolveEntity := func(ee ExpectedEntity) *graph.Entity {
		if ee.MatchBy == "qualified_name" && ee.QualifiedName != "" {
			return byQName[ee.QualifiedName]
		}
		if ee.SourceFile != "" {
			if e, ok := byKindNameFile[ee.Kind+"\x00"+ee.Name+"\x00"+ee.SourceFile]; ok {
				return e
			}
		}
		if es := byKindName[ee.Kind+"\x00"+ee.Name]; len(es) > 0 {
			return es[0]
		}
		return nil
	}

	for _, ee := range fix.ExpectedEntities {
		ent := resolveEntity(ee)
		res := EntityResult{Expected: ee, Found: ent != nil}
		if ent != nil {
			res.MatchedID = ent.ID
		}
		rep.EntityResults = append(rep.EntityResults, res)
		switch {
		case ee.NiceToHave:
			rep.NiceEntityTotal++
			if res.Found {
				rep.NiceEntityFound++
			}
		case ee.MustExist:
			rep.EntityExpected++
			if res.Found {
				rep.EntityFound++
			}
		}
	}

	// Build a relationship lookup keyed on (FromID, ToID, Kind). We also
	// keep a (Kind, ToID) bucket for the bare-name match path below.
	type relKey struct{ from, to, kind string }
	relByTriple := make(map[relKey]*graph.Relationship, len(doc.Relationships))
	relByKindTo := make(map[string][]*graph.Relationship)
	relByKindFrom := make(map[string][]*graph.Relationship)
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		relByTriple[relKey{r.FromID, r.ToID, r.Kind}] = r
		relByKindTo[r.Kind+"\x00"+r.ToID] = append(relByKindTo[r.Kind+"\x00"+r.ToID], r)
		relByKindFrom[r.Kind+"\x00"+r.FromID] = append(relByKindFrom[r.Kind+"\x00"+r.FromID], r)
	}

	// resolveExpectedEdge tries every combination of from/to candidates so
	// fixtures don't have to spell out the SourceFile when there is no
	// collision. Returns (matched Relationship or nil, fromResolved,
	// toResolved).
	resolveExpectedEdge := func(er ExpectedRelationship) (*graph.Relationship, bool, bool) {
		// Candidate "from" entities.
		var fromCands []*graph.Entity
		if er.FromFile != "" {
			if e, ok := byKindNameFile[er.FromKind+"\x00"+er.FromName+"\x00"+er.FromFile]; ok {
				fromCands = []*graph.Entity{e}
			}
		}
		if len(fromCands) == 0 {
			fromCands = byKindName[er.FromKind+"\x00"+er.FromName]
		}
		if len(fromCands) == 0 && er.FromKind == "" {
			// Best-effort: scan all kinds when fixture author left it blank.
			for k := range doc.Entities {
				e := &doc.Entities[k]
				if e.Name == er.FromName {
					fromCands = append(fromCands, e)
				}
			}
		}
		fromResolved := len(fromCands) > 0

		// Candidate "to" entities OR bare-name target.
		var toCands []*graph.Entity
		if er.ToFile != "" {
			if e, ok := byKindNameFile[er.ToKind+"\x00"+er.ToName+"\x00"+er.ToFile]; ok {
				toCands = []*graph.Entity{e}
			}
		}
		if len(toCands) == 0 && er.ToName != "" {
			toCands = byKindName[er.ToKind+"\x00"+er.ToName]
			if len(toCands) == 0 && er.ToKind == "" {
				for k := range doc.Entities {
					e := &doc.Entities[k]
					if e.Name == er.ToName {
						toCands = append(toCands, e)
					}
				}
			}
		}
		toResolved := len(toCands) > 0 || er.ToBareName != ""

		// First pass: try the strict (from, to, kind) triple lookup over
		// every candidate combination.
		for _, fc := range fromCands {
			for _, tc := range toCands {
				if r, ok := relByTriple[relKey{fc.ID, tc.ID, er.Kind}]; ok {
					return r, fromResolved, toResolved
				}
			}
			if er.ToBareName != "" {
				if r, ok := relByTriple[relKey{fc.ID, er.ToBareName, er.Kind}]; ok {
					return r, fromResolved, true
				}
				// Bare-name comparison is whitespace-insensitive; the
				// indexer may emit a slightly mangled stub.
				for _, r := range relByKindFrom[er.Kind+"\x00"+fc.ID] {
					if strings.EqualFold(strings.TrimSpace(r.ToID), strings.TrimSpace(er.ToBareName)) {
						return r, fromResolved, true
					}
				}
			}
		}
		return nil, fromResolved, toResolved
	}

	for _, er := range fix.ExpectedRelationships {
		match, fromOk, toOk := resolveExpectedEdge(er)
		res := RelationshipResult{
			Expected:     er,
			Found:        match != nil,
			FromResolved: fromOk,
			ToResolved:   toOk,
		}
		if match != nil {
			res.MatchedRelID = match.ID
		}
		rep.RelResults = append(rep.RelResults, res)
		switch {
		case er.NiceToHave:
			rep.NiceRelTotal++
			if res.Found {
				rep.NiceRelFound++
			}
		case er.MustExist:
			rep.RelExpected++
			if res.Found {
				rep.RelFound++
			}
		}
	}

	// Forbidden edges — count any extracted edge that satisfies one of
	// the fixture's forbidden patterns.
	for _, fb := range fix.ForbiddenRelationships {
		match, fromOk, toOk := resolveExpectedEdge(fb)
		if match != nil {
			rep.ForbiddenHits = append(rep.ForbiddenHits, RelationshipResult{
				Expected:     fb,
				Found:        true,
				FromResolved: fromOk,
				ToResolved:   toOk,
				MatchedRelID: match.ID,
			})
		}
	}

	return rep
}
