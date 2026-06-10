// archigraph_literal_parity MCP tool (#4421, epic #4419 P0).
//
// Diffs a SCOPE.Enum / ConstantSet value-set in an ORACLE group against its
// mirror in a V3-rewrite group, answering the rewrite-parity question for ANY
// value-set in ANY language: does the v3 rewrite reproduce the oracle's literal
// value-set key-for-key and value-for-value?
//
// It is a GENERIC differ keyed off the structured members_json property emitted
// by the shared enum/value-set extractor (internal/extractor/enum_valueset.go),
// NOT a hack for one named constant collection. The diff core lives in
// internal/literalparity and is unit-tested independently of MCP.
//
// Signature:
//
//	literal_parity(
//	  group_oracle:  "<oracle group>",   (required)
//	  group_v3:      "<v3 group>",        (required)
//	  set:           "page_slugs" | "action_codenames" | "status_strings"
//	                 | "enum:<Name>",     (required — alias or enum:<Name>)
//	  oracle_source: "<entity_id>",       (optional — pin the oracle value-set)
//	  v3_source:     "<entity_id>",       (optional — pin the v3 value-set)
//	)
//
// Result:
//
//	{
//	  "set": "page_slugs",
//	  "oracle_source": "<resolved entity id>",
//	  "v3_source":     "<resolved entity id>",
//	  "only_in_oracle": ["..."],
//	  "only_in_v3":     ["..."],
//	  "value_mismatches": [{"key","oracle","v3"}],
//	  "intra_v3_inconsistencies": [{"convention","outliers","detail"}],
//	  "verdict": "equivalent" | "drift"
//	}
//
// Auto-locate (when *_source not given): the `set` alias maps to a list of
// conventional value-set names (e.g. page_slugs → PERMISSION_PAGES /
// PermissionPage / PAGE_SLUGS / PageSlug). Each group's SCOPE.Enum entities are
// scanned and the best name match (exact-normalised, then substring) carrying a
// non-empty members_json is selected. `enum:<Name>` matches the bare enum name
// directly. Auto-locate is intentionally tolerant of cross-stack naming drift
// (the whole point: the oracle and v3 name the same set differently).
package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/literalparity"
	"github.com/cajasmota/archigraph/internal/types"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// setAliasCandidates maps a human `set` alias to the conventional value-set
// entity names a rewrite tends to use. Order is preference; matching is done on
// the NormalizeKey form so case/separator differences fold automatically.
var setAliasCandidates = map[string][]string{
	"page_slugs":       {"PERMISSION_PAGES", "PermissionPage", "PAGE_SLUGS", "PageSlug", "Pages", "PageSlugs"},
	"action_codenames": {"ACTION_CODENAMES", "ActionCodename", "ACTIONS", "Action", "Codenames", "ActionCodenames"},
	"status_strings":   {"STATUS_STRINGS", "STATUSES", "Status", "StatusString", "StatusStrings"},
}

// handleLiteralParity implements archigraph_literal_parity.
func (s *Server) handleLiteralParity(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	oracleGroup := argString(req, "group_oracle", "")
	v3Group := argString(req, "group_v3", "")
	set := strings.TrimSpace(argString(req, "set", ""))
	if oracleGroup == "" || v3Group == "" {
		return mcpapi.NewToolResultError("group_oracle and group_v3 are both required"), nil
	}
	if set == "" {
		return mcpapi.NewToolResultError("set is required (alias e.g. \"page_slugs\" or \"enum:<Name>\")"), nil
	}

	lgOracle := s.State.Group(oracleGroup)
	if lgOracle == nil {
		return mcpapi.NewToolResultError("group_oracle " + oracleGroup + " not loaded"), nil
	}
	lgV3 := s.State.Group(v3Group)
	if lgV3 == nil {
		return mcpapi.NewToolResultError("group_v3 " + v3Group + " not loaded"), nil
	}

	oracleID := argString(req, "oracle_source", "")
	v3ID := argString(req, "v3_source", "")

	oracleEnt, oErr := locateValueSet(lgOracle, set, oracleID)
	if oErr != "" {
		return mcpapi.NewToolResultError("oracle: " + oErr), nil
	}
	v3Ent, vErr := locateValueSet(lgV3, set, v3ID)
	if vErr != "" {
		return mcpapi.NewToolResultError("v3: " + vErr), nil
	}

	oracleMembers, err := parseMembersJSON(oracleEnt)
	if err != nil {
		return mcpapi.NewToolResultError("oracle members_json: " + err.Error()), nil
	}
	v3Members, err := parseMembersJSON(v3Ent)
	if err != nil {
		return mcpapi.NewToolResultError("v3 members_json: " + err.Error()), nil
	}

	res := literalparity.Diff(set, oracleMembers, v3Members)

	return jsonResult(map[string]any{
		"set":                      res.Set,
		"oracle_source":            oracleEnt.ID,
		"v3_source":                v3Ent.ID,
		"oracle_set_name":          oracleEnt.Name,
		"v3_set_name":              v3Ent.Name,
		"only_in_oracle":           res.OnlyInOracle,
		"only_in_v3":               res.OnlyInV3,
		"value_mismatches":         res.ValueMismatches,
		"intra_v3_inconsistencies": res.IntraV3Inconsistencies,
		"verdict":                  res.Verdict,
	}), nil
}

// locateValueSet resolves the SCOPE.Enum value-set entity for a `set` within a
// group. If sourceID is non-empty it is looked up directly (prefixed or bare
// ID). Otherwise the set alias / enum:<Name> drives an auto-locate scan over
// the group's SCOPE.Enum entities. Returns a non-empty error string on failure.
func locateValueSet(lg *LoadedGroup, set, sourceID string) (*graph.Entity, string) {
	if sourceID != "" {
		if e := findEnumByID(lg, sourceID); e != nil {
			return e, ""
		}
		return nil, "source entity " + sourceID + " not found (must be a SCOPE.Enum value-set)"
	}

	// enum:<Name> — match the bare enum name directly.
	if strings.HasPrefix(set, "enum:") {
		name := strings.TrimSpace(strings.TrimPrefix(set, "enum:"))
		if name == "" {
			return nil, "set \"enum:\" requires a name (enum:<Name>)"
		}
		if e := matchEnumByNames(lg, []string{name}); e != nil {
			return e, ""
		}
		return nil, "no SCOPE.Enum value-set named " + name + " with members_json found"
	}

	cands, ok := setAliasCandidates[set]
	if !ok {
		return nil, "unknown set alias " + set + " (use a known alias or enum:<Name>)"
	}
	if e := matchEnumByNames(lg, cands); e != nil {
		return e, ""
	}
	return nil, "no SCOPE.Enum value-set matching alias " + set +
		" (tried: " + strings.Join(cands, ", ") + ")"
}

// findEnumByID returns the SCOPE.Enum entity with the given id (accepts both a
// bare entity id and a "<repo>::<id>" prefixed id) carrying members_json.
func findEnumByID(lg *LoadedGroup, id string) *graph.Entity {
	bare := id
	if i := strings.Index(id, "::"); i >= 0 {
		bare = id[i+2:]
	}
	for _, r := range lg.Repos {
		if r == nil || r.Doc == nil {
			continue
		}
		byID := r.getByID()
		for _, cand := range []string{id, bare} {
			if e, ok := byID[cand]; ok && isValueSet(e) {
				return e
			}
		}
	}
	return nil
}

// matchEnumByNames scans a group's SCOPE.Enum value-sets and returns the best
// match against the candidate names. Matching tiers (best first):
//  1. exact normalised name match;
//  2. one side is a normalised substring of the other.
// Within a tier, candidate order wins; then deterministic by entity name.
func matchEnumByNames(lg *LoadedGroup, candidates []string) *graph.Entity {
	type hit struct {
		ent  *graph.Entity
		tier int // 0 exact, 1 substring
		rank int // candidate preference rank
	}
	var hits []hit

	normCands := make([]string, len(candidates))
	for i, c := range candidates {
		normCands[i] = literalparity.NormalizeKey(c)
	}

	for _, r := range lg.Repos {
		if r == nil || r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isValueSet(e) {
				continue
			}
			en := literalparity.NormalizeKey(enumDisplayName(e))
			for rank, nc := range normCands {
				switch {
				case en == nc:
					hits = append(hits, hit{ent: e, tier: 0, rank: rank})
				case strings.Contains(en, nc) || strings.Contains(nc, en):
					hits = append(hits, hit{ent: e, tier: 1, rank: rank})
				}
			}
		}
	}
	if len(hits) == 0 {
		return nil
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].tier != hits[j].tier {
			return hits[i].tier < hits[j].tier
		}
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		return hits[i].ent.Name < hits[j].ent.Name
	})
	return hits[0].ent
}

// enumDisplayName returns the enum's logical name: the enum_name property when
// present (the bare type name), else the entity Name.
func enumDisplayName(e *graph.Entity) string {
	if e.Properties != nil {
		if n := strings.TrimSpace(e.Properties["enum_name"]); n != "" {
			return n
		}
	}
	return e.Name
}

// isValueSet reports whether an entity is a SCOPE.Enum value-set carrying a
// non-empty members_json — the only entities literal_parity can diff.
func isValueSet(e *graph.Entity) bool {
	if e == nil || e.Kind != string(types.EntityKindEnum) {
		return false
	}
	if e.Properties == nil {
		return false
	}
	return strings.TrimSpace(e.Properties["members_json"]) != ""
}

// parseMembersJSON decodes the structured members_json property into the
// literalparity.Member slice.
func parseMembersJSON(e *graph.Entity) ([]literalparity.Member, error) {
	raw := ""
	if e.Properties != nil {
		raw = e.Properties["members_json"]
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var members []literalparity.Member
	if err := json.Unmarshal([]byte(raw), &members); err != nil {
		return nil, err
	}
	return members, nil
}
