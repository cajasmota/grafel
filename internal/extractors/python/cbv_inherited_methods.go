// cbv_inherited_methods.go — Generic CBV inherited-method annotation.
//
// Issue #2011 — Django generic class-based views and DRF generic
// viewsets/views inherit a fixed contract of HTTP method handlers from
// their base classes. A `class PermitViewSet(ModelViewSet)` exposes
// `list / retrieve / create / update / partial_update / destroy`
// without declaring them. Earlier waves attempted to model this by
// synthesizing fake SCOPE.Operation entities for the inherited methods,
// which produced spurious "method exists but has no source" findings
// in audits AND inflated entity counts on every DRF project.
//
// Option A (this implementation): annotate the SCOPE.Component/class
// entity with an `inherited_methods` property listing every inherited
// method name from each recognised base class, comma-joined and
// deduplicated. Downstream tools that need to enumerate the class's
// API surface read the property directly without us having to
// fabricate operation entities or invent a synthetic provenance.
//
// We only handle the well-known generic CBV / DRF base classes
// enumerated in `cbvBaseInheritedMethods` below. Anything else gets no
// annotation — a deliberate trade-off: it's better to under-annotate
// than to invent inheritance from arbitrary user-defined bases whose
// method sets we don't know.

package python

import (
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/frameworks/baseknowledge"
	"github.com/cajasmota/grafel/internal/types"
)

// The recognised Django / DRF generic base classes and the HTTP-handler
// method names they contribute to a subclass's surface now live in the
// shared, typed catalog `internal/frameworks/baseknowledge` (DRF pack) —
// the single source of truth that MRO resolution (#3833) and
// effective-contract synthesis (#3835) also consume.
//
// `cbvInheritedMembers` resolves a base-class leaf name to the inherited
// method names that base contributes, via the catalog. It returns the
// method names (the property this annotation stamps is name-only by
// design — Option A, #2011) and whether the base is recognised at all
// (so empty-but-known bases like GenericViewSet still register as
// `cbv_bases` without adding methods, exactly as the old map did).
func cbvInheritedMembers(baseLeaf string) (methods []string, known bool) {
	c, ok := baseknowledge.Default().Lookup(baseLeaf)
	if !ok {
		return nil, false
	}
	return c.MemberNames(), true
}

// emitCBVInheritedMethodAnnotations walks every class entity in the
// file's slice. For each class whose EXTENDS edges include one or more
// recognised CBV / DRF base classes, it stamps an `inherited_methods`
// property holding the comma-joined union of method names contributed
// by those bases.
//
// We read EXTENDS edges (emitted by extractBaseClasses, #698) rather
// than re-parse the class header, so the rule fires consistently
// regardless of whether the base is `View`, `views.View`, or
// `django.views.generic.View` — extractBaseClasses normalises the leaf.
//
// Mutates *entities in place. Safe with nil/empty inputs.
func emitCBVInheritedMethodAnnotations(_ ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if entities == nil || len(*entities) == 0 {
		return
	}
	for i := range *entities {
		e := &(*entities)[i]
		if e.SourceFile != file.Path {
			continue
		}
		if e.Kind != "SCOPE.Component" || e.Subtype != "class" {
			continue
		}
		seen := make(map[string]struct{})
		hadBase := false
		for _, r := range e.Relationships {
			if r.Kind != "EXTENDS" {
				continue
			}
			baseLeaf := r.Properties["base_name"]
			if baseLeaf == "" {
				baseLeaf = extractLeafFromExtendsToID(r.ToID)
			}
			if dot := strings.LastIndexByte(baseLeaf, '.'); dot >= 0 {
				baseLeaf = baseLeaf[dot+1:]
			}
			methods, ok := cbvInheritedMembers(baseLeaf)
			if !ok {
				continue
			}
			hadBase = true
			for _, m := range methods {
				seen[m] = struct{}{}
			}
		}
		if !hadBase {
			continue
		}
		// Sort for deterministic property values across runs.
		ordered := make([]string, 0, len(seen))
		for m := range seen {
			ordered = append(ordered, m)
		}
		sort.Strings(ordered)
		if e.Properties == nil {
			e.Properties = make(map[string]string)
		}
		e.Properties["inherited_methods"] = strings.Join(ordered, ",")
		// Bases tag is useful for downstream audit; comma-joined leaves.
		bases := make([]string, 0)
		for _, r := range e.Relationships {
			if r.Kind != "EXTENDS" {
				continue
			}
			baseLeaf := r.Properties["base_name"]
			if baseLeaf == "" {
				baseLeaf = extractLeafFromExtendsToID(r.ToID)
			}
			if dot := strings.LastIndexByte(baseLeaf, '.'); dot >= 0 {
				baseLeaf = baseLeaf[dot+1:]
			}
			if _, ok := cbvInheritedMembers(baseLeaf); ok {
				bases = append(bases, baseLeaf)
			}
		}
		if len(bases) > 0 {
			e.Properties["cbv_bases"] = strings.Join(bases, ",")
		}
	}
}

// extractLeafFromExtendsToID returns the trailing colon-separated
// segment of an EXTENDS edge ToID. The structural-ref shape used by
// extractBaseClasses ends with the bare class name. Empty input yields
// "".
func extractLeafFromExtendsToID(toID string) string {
	if toID == "" {
		return ""
	}
	if idx := strings.LastIndexByte(toID, ':'); idx >= 0 {
		return toID[idx+1:]
	}
	return toID
}
