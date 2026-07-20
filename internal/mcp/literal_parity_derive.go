// Derivation resolvers for literal_parity (#4665 part b).
//
// Some value-sets the rewrite-parity audit cares about are IMPLICIT on one side
// — they are not declared as a constant/enum the value-set extractor can pick
// up, but are derivable from other graph structure. The canonical example is DRF
// "action codenames": Django REST Framework exposes a ViewSet's custom actions
// via `@action`-decorated methods, and the codename of each action is implicitly
// the lowercased method name (or its explicit url_path). There is no declared
// enum on the oracle side, so locateValueSet returns "unresolved" — but the set
// is real and DERIVABLE.
//
// A derivation resolver synthesises a literalparity-compatible value-set
// (a *graph.Entity carrying members_json) from such structure, so the very same
// Diff core can compare it to the other side. The resolver is opt-in per side via
// the `oracle_derive` / `v3_derive` params, so it never fires silently — the
// caller explicitly asks for a derived set instead of an explicit *_source pin.
//
// Currently one derivation is supported:
//
//	drf_action_codenames — collect @action-decorated methods (Properties
//	  ["drf_action"]=="true") across the group, codename = url_path when set,
//	  else the lowercased bare method name. An optional `viewset` param scopes
//	  the collection to actions CONTAINS-owned by the named ViewSet (canonical
//	  name match), so two unrelated ViewSets don't get merged into one set.
package mcp

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/literalparity"
	"github.com/cajasmota/grafel/internal/types"
)

// knownDerivations is the registry of supported `*_derive` values.
var knownDerivations = map[string]struct{}{
	"drf_action_codenames": {},
}

// deriveValueSet builds a synthetic SCOPE.Enum value-set entity for a derivation
// kind within a group, scoped by an optional viewset name. It returns the
// synthetic entity (with members_json populated) and an empty error string on
// success, or a nil entity and a non-empty error string when the derivation is
// unknown or yields nothing.
func deriveValueSet(lg *LoadedGroup, kind, viewset string) (*graph.Entity, string) {
	if _, ok := knownDerivations[kind]; !ok {
		return nil, "unknown derivation " + kind + " (supported: drf_action_codenames)"
	}
	switch kind {
	case "drf_action_codenames":
		return deriveDRFActionCodenames(lg, viewset)
	default:
		return nil, "unknown derivation " + kind
	}
}

// deriveDRFActionCodenames collects the implicit action-codename value-set from a
// group's @action-decorated methods. The codename is url_path when present, else
// the lowercased bare method name (DRF's default url_path is the method name).
// When viewset is non-empty only actions CONTAINS-owned by a ViewSet whose name
// canonicalises equal to it are included.
func deriveDRFActionCodenames(lg *LoadedGroup, viewset string) (*graph.Entity, string) {
	wantVS := canonicalSetName(viewset)

	// dedup by codename (an action may be emitted once; defensive against dups).
	seen := map[string]bool{}
	var members []literalparity.Member

	for _, r := range lg.Repos {
		if r == nil || r.Doc == nil {
			continue
		}
		// Owner lookup: method entity ID -> canonical ViewSet name, via CONTAINS.
		ownerVS := map[string]string{}
		if wantVS != "" {
			byID := r.getByID()
			for i := range r.Doc.Relationships {
				rel := &r.Doc.Relationships[i]
				if rel.Kind != "CONTAINS" {
					continue
				}
				parent, ok := byID[bareID(rel.FromID)]
				if !ok {
					parent, ok = byID[rel.FromID]
				}
				if !ok || !isViewSetLike(parent) {
					continue
				}
				ownerVS[bareID(rel.ToID)] = canonicalSetName(bareEntityName(parent))
				ownerVS[rel.ToID] = canonicalSetName(bareEntityName(parent))
			}
		}

		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isDRFAction(e) {
				continue
			}
			if wantVS != "" {
				vs, ok := ownerVS[e.ID]
				if !ok {
					vs = ownerVS[bareID(e.ID)]
				}
				if vs != wantVS {
					continue
				}
			}
			code := drfActionCodename(e)
			if code == "" || seen[code] {
				continue
			}
			seen[code] = true
			members = append(members, literalparity.Member{Key: code, Value: code, Line: e.StartLine})
		}
	}

	if len(members) == 0 {
		detail := ""
		if viewset != "" {
			detail = " under viewset " + viewset
		}
		return nil, "no @action-decorated methods found" + detail +
			" to derive drf_action_codenames (need Properties[\"drf_action\"]==\"true\")"
	}

	sort.Slice(members, func(i, j int) bool { return members[i].Key < members[j].Key })
	mj, err := json.Marshal(members)
	if err != nil {
		return nil, "encode derived members: " + err.Error()
	}

	name := "DerivedActionCodenames"
	if viewset != "" {
		name = viewset + ".DerivedActionCodenames"
	}
	return graph.EntityPtr(graph.Entity{
		ID:   "derived::drf_action_codenames::" + name,
		Name: name,
		Kind: string(types.EntityKindEnum),
	}.WithProperties(map[string]string{
		"enum_name":    name,
		"members_json": string(mj),
		"derived":      "drf_action_codenames",
	},
	)), ""
}

// isDRFAction reports whether an entity is a DRF @action-decorated method.
func isDRFAction(e *graph.Entity) bool {
	if e == nil || e.PropLen() == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(e.PropGet("drf_action")), "true")
}

// drfActionCodename returns the implicit codename of a DRF action: the explicit
// url_path when present, else the lowercased bare method name (the part after the
// final '.'), which is DRF's default url_path.
func drfActionCodename(e *graph.Entity) string {
	if e.PropLen() > 0 {
		if up := strings.TrimSpace(e.PropGet("url_path")); up != "" {
			return strings.ToLower(up)
		}
	}
	name := e.Name
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// isViewSetLike reports whether an entity looks like a DRF ViewSet / controller
// class (a class-ish component whose name ends in ViewSet/View/Controller, or any
// class that CONTAINS @action methods — the latter is handled by the caller).
func isViewSetLike(e *graph.Entity) bool {
	if e == nil {
		return false
	}
	n := strings.ToLower(e.Name)
	return strings.HasSuffix(n, "viewset") ||
		strings.HasSuffix(n, "view") ||
		strings.HasSuffix(n, "controller") ||
		e.Subtype == "class"
}

// bareEntityName returns the bare class name (after the final '.') for matching.
func bareEntityName(e *graph.Entity) string {
	n := e.Name
	if i := strings.LastIndex(n, "."); i >= 0 {
		n = n[i+1:]
	}
	return n
}

// bareID strips a "<repo>::" prefix from an entity id, if present.
func bareID(id string) string {
	if i := strings.Index(id, "::"); i >= 0 {
		return id[i+2:]
	}
	return id
}
