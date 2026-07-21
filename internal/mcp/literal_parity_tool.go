// grafel_literal_parity MCP tool (#4421, epic #4419 P0).
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
//	  oracle_derive: "drf_action_codenames", (optional — derive an IMPLICIT
//	                 oracle set from graph structure instead of a declared enum)
//	  v3_derive:     "drf_action_codenames", (optional — same, v3 side)
//	  viewset:       "<ViewSet name>",     (optional — scope a derivation to one
//	                 ViewSet's @action methods)
//	)
//
// Resolution precedence per side: *_derive (derivation resolver, opt-in) >
// *_source (hard pin) > auto-locate by alias. A side that can be auto-located to
// no SEMANTIC counterpart returns verdict:"unresolved" rather than silently
// comparing a wrong set; for sets that are IMPLICIT on one side (DRF action
// codenames have no declared enum on the oracle) use *_derive or an explicit
// *_source.
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

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/literalparity"
	"github.com/cajasmota/grafel/internal/types"

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

// handleLiteralParity implements grafel_literal_parity.
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
	oracleDerive := strings.TrimSpace(argString(req, "oracle_derive", ""))
	v3Derive := strings.TrimSpace(argString(req, "v3_derive", ""))
	viewset := strings.TrimSpace(argString(req, "viewset", ""))

	// Resolution precedence per side:
	//  1. *_derive — a derivation resolver synthesises an IMPLICIT value-set from
	//     graph structure (e.g. DRF action codenames = @action method names). This
	//     is opt-in so it never fires silently; a miss is a hard error (the caller
	//     explicitly asked to derive). It outranks auto-locate so an implicit side
	//     can be paired with a declared set on the other side. (#4665 part b)
	//  2. *_source — a hard pin to a specific declared value-set entity; a miss is
	//     a real error (the caller asked for a specific entity).
	//  3. auto-locate by alias / enum:<Name>. This may fail to find a SEMANTIC
	//     counterpart on one side; we then return verdict:"unresolved" with that
	//     side's *_source null and a note (NEVER a silent wrong-set comparison).
	oracleEnt, oErr := resolveSide(lgOracle, set, oracleID, oracleDerive, viewset)
	if oErr != "" && (oracleID != "" || oracleDerive != "") {
		return mcpapi.NewToolResultError("oracle: " + oErr), nil
	}
	v3Ent, vErr := resolveSide(lgV3, set, v3ID, v3Derive, viewset)
	if vErr != "" && (v3ID != "" || v3Derive != "") {
		return mcpapi.NewToolResultError("v3: " + vErr), nil
	}
	if oErr != "" || vErr != "" {
		return unresolvedResult(set, oracleEnt, v3Ent, oErr, vErr), nil
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

// unresolvedResult builds a verdict:"unresolved" payload when a real counterpart
// value-set could not be auto-located on one (or both) sides. The located side's
// source id is reported; the unresolved side's is null. A note explains what to
// do (supply an explicit oracle_source/v3_source). This is deliberately a
// NO-RESULT, not a fabricated comparison against the wrong set.
func unresolvedResult(set string, oracleEnt, v3Ent *graph.Entity, oErr, vErr string) *mcpapi.CallToolResult {
	out := map[string]any{
		"set":                      set,
		"verdict":                  literalparity.VerdictUnresolved,
		"oracle_source":            nil,
		"v3_source":                nil,
		"oracle_set_name":          nil,
		"v3_set_name":              nil,
		"only_in_oracle":           []string{},
		"only_in_v3":               []string{},
		"value_mismatches":         []literalparity.ValueMismatch{},
		"intra_v3_inconsistencies": []literalparity.IntraInconsistency{},
	}
	notes := []string{}
	if oErr != "" {
		notes = append(notes, "oracle: "+oErr)
	} else if oracleEnt != nil {
		out["oracle_source"] = oracleEnt.ID
		out["oracle_set_name"] = oracleEnt.Name
	}
	if vErr != "" {
		notes = append(notes, "v3: "+vErr)
	} else if v3Ent != nil {
		out["v3_source"] = v3Ent.ID
		out["v3_set_name"] = v3Ent.Name
	}
	notes = append(notes, "no semantic counterpart auto-located on one side; "+
		"this set may be IMPLICIT on that side (e.g. DRF action codenames = "+
		"lowercased @action method names, no declared enum) — supply an explicit "+
		"oracle_source/v3_source to compare. Refusing to compare a mismatched set.")
	out["note"] = strings.Join(notes, " | ")
	return jsonResult(out)
}

// resolveSide resolves one side's value-set with the precedence derive > pin >
// auto-locate. derive (when non-empty) synthesises an implicit set from graph
// structure scoped by viewset; sourceID (when non-empty) hard-pins a declared
// entity; otherwise the set alias / enum:<Name> drives auto-locate. Returns a
// non-empty error string on failure.
func resolveSide(lg *LoadedGroup, set, sourceID, derive, viewset string) (*graph.Entity, string) {
	if derive != "" {
		return deriveValueSet(lg, derive, viewset)
	}
	return locateValueSet(lg, set, sourceID)
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
		for _, cand := range []string{id, bare} {
			if e, ok := r.getByIDOne(cand); ok && isValueSet(e) {
				return e
			}
		}
	}
	return nil
}

// matchEnumByNames scans a group's SCOPE.Enum value-sets and returns the best
// SEMANTIC match against the candidate names. Matching requires an EXACT
// canonical-name match (CanonicalKey folds case + separators + camelCase, and a
// trailing plural 's', so PERMISSION_PAGES / PermissionPage / PageSlugs all
// canonicalise comparably). Substring matching was REMOVED (#4532 Bug B): it let
// `action_codenames` grab unrelated "Action" enums (SyncActionStatus,
// OutboxAction) and silently compare the wrong sets. A no-result is safer than a
// wrong-set comparison — the caller gets verdict:"unresolved" and can pin an
// explicit source. Within the exact tier, candidate order wins; then by name.
func matchEnumByNames(lg *LoadedGroup, candidates []string) *graph.Entity {
	type hit struct {
		ent  *graph.Entity
		rank int // candidate preference rank
	}
	var hits []hit

	normCands := make([]string, len(candidates))
	for i, c := range candidates {
		normCands[i] = canonicalSetName(c)
	}

	for _, r := range lg.Repos {
		if r == nil || r.Doc == nil {
			continue
		}
		r.forEachEntity(func(e *graph.Entity) bool {
			if !isValueSet(e) {
				return true
			}
			en := canonicalSetName(enumDisplayName(e))
			for rank, nc := range normCands {
				if en == nc {
					hits = append(hits, hit{ent: e, rank: rank})
				}
			}
			return true
		})
	}
	if len(hits) == 0 {
		return nil
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		return hits[i].ent.Name < hits[j].ent.Name
	})
	return hits[0].ent
}

// canonicalSetName folds a value-set name to its semantic-match form:
// CanonicalKey (case + separator + camelCase fold) plus a trailing-plural fold
// so the singular/plural naming the oracle and v3 tend to differ on still align
// (PERMISSION_PAGES ↔ PermissionPage, PageSlug ↔ PageSlugs). Only the FINAL 's'
// of the whole canonical form is dropped (status → statu would be wrong, so we
// keep names ending in "ss" and very short stems intact).
func canonicalSetName(s string) string {
	c := literalparity.CanonicalKey(s)
	if len(c) > 3 && strings.HasSuffix(c, "s") && !strings.HasSuffix(c, "ss") {
		c = c[:len(c)-1]
	}
	return c
}

// enumDisplayName returns the enum's logical name: the enum_name property when
// present (the bare type name), else the entity Name.
func enumDisplayName(e *graph.Entity) string {
	if e.PropLen() > 0 {
		if n := strings.TrimSpace(e.PropGet("enum_name")); n != "" {
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
	if e.PropLen() == 0 {
		return false
	}
	return strings.TrimSpace(e.PropGet("members_json")) != ""
}

// parseMembersJSON decodes the structured members_json property into the
// literalparity.Member slice.
func parseMembersJSON(e *graph.Entity) ([]literalparity.Member, error) {
	raw := ""
	if e.PropLen() > 0 {
		raw = e.PropGet("members_json")
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
