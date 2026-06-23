// django_drf_permissions.go — class-level DRF authorisation surface (#2816).
//
// # Why this exists
//
// grafel_auth_coverage previously detected auth only via per-method /
// per-decorator signals (e.g. `@permission_classes(...)`, `@login_required`).
// Real DRF apps overwhelmingly declare authorisation at the *class* level:
//
//	class BuildingViewSet(viewsets.ModelViewSet):
//	    permission_classes = [IsAuthenticated]      # class attribute
//
//	    def get_permissions(self):                  # dynamic override
//	        return [IsAuthenticated(), ...]
//
// On a mature DRF codebase (upvate-core) this meant 0 % coverage / hundreds of
// false-positive "unprotected endpoint" findings — a security surface that
// cried wolf.  This pass stamps the class-level authorisation surface onto the
// ViewSet/APIView class entity so the auth_coverage detector can read it
// without re-parsing source:
//
//	permission_classes      → comma-joined identifier list of the class
//	                          attribute RHS (e.g. "IsAuthenticated" or
//	                          "AllowAny"). Empty list → "" (explicit
//	                          permission-less, i.e. open).
//	has_permission_classes  → "true" when the class attribute is present
//	                          (even if its value is `[]` / AllowAny) so the
//	                          detector can distinguish "explicitly open" from
//	                          "no signal at all".
//	has_get_permissions     → "true" when the class defines get_permissions().
//	get_permissions_classes → comma-joined identifier list of permission
//	                          classes referenced anywhere in the get_permissions
//	                          body (best-effort; covers the common pattern of
//	                          `permission_classes = [IsAuthenticated, ...]`).
//
// The detector treats a non-AllowAny class attribute, or a get_permissions()
// that references a non-AllowAny permission class, as auth coverage.  An
// explicit `permission_classes = [AllowAny]` (or `[]`) is recognised as
// genuinely public.
//
// This pass runs AFTER walkNode has emitted the class entity and its method /
// field children, mirroring applyFrameworkInnerClassProperties (#757).

package python

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// applyDRFPermissionProperties inspects classBody for a class-level
// `permission_classes = [...]` assignment and a `get_permissions()` method,
// and stamps the corresponding properties onto parent in-place.
//
// Safe to call on any class body — non-DRF classes simply have neither shape
// and the function is a no-op.
func applyDRFPermissionProperties(parent *types.EntityRecord, classBody ts.Node, src []byte) {
	if parent == nil || classBody == nil {
		return
	}

	var (
		setProp = func(k, v string) {
			if parent.Properties == nil {
				parent.Properties = make(map[string]string)
			}
			parent.Properties[k] = v
		}
	)

	for i := 0; i < int(classBody.ChildCount()); i++ {
		stmt := classBody.Child(i)
		if stmt == nil {
			continue
		}
		switch stmt.Type() {
		case "expression_statement":
			// Look for `permission_classes = [...]` at class-body scope.
			for j := 0; j < int(stmt.NamedChildCount()); j++ {
				expr := stmt.NamedChild(j)
				if expr == nil || expr.Type() != "assignment" {
					continue
				}
				lhs := expr.ChildByFieldName("left")
				if lhs == nil || lhs.Type() != "identifier" {
					continue
				}
				if nodeText(lhs, src) != "permission_classes" {
					continue
				}
				rhs := expr.ChildByFieldName("right")
				raw := parseListLiteralIdentifiers(rhs, src)
				perms := make([]string, 0, len(raw))
				for _, p := range raw {
					if leaf := permissionLeafName(p); leaf != "" {
						perms = append(perms, leaf)
					}
				}
				setProp("has_permission_classes", "true")
				setProp("permission_classes", strings.Join(perms, ","))
			}
		case "function_definition":
			if isGetPermissionsDef(stmt, src) {
				setProp("has_get_permissions", "true")
				if cls := getPermissionsReferencedClasses(stmt, src); len(cls) > 0 {
					setProp("get_permissions_classes", strings.Join(cls, ","))
				}
			}
		case "decorated_definition":
			// get_permissions is occasionally decorated (rare). Unwrap.
			if inner := stmt.ChildByFieldName("definition"); inner != nil &&
				inner.Type() == "function_definition" && isGetPermissionsDef(inner, src) {
				setProp("has_get_permissions", "true")
				if cls := getPermissionsReferencedClasses(inner, src); len(cls) > 0 {
					setProp("get_permissions_classes", strings.Join(cls, ","))
				}
			}
		}
	}
}

// drfPublicPermissionClasses are DRF permission classes that grant anonymous
// access; an endpoint whose only permission class is one of these is open.
var drfPublicPermissionClasses = map[string]bool{
	"AllowAny": true,
}

// stampDRFActionAuth normalises a per-action `permission_classes` property
// (set by the @action kwarg parser) into the cross-framework auth contract
// (auth_required / auth_method / auth_confidence / auth_guard), mirroring the
// Spring (java_auth_policy.go), FastAPI and Express resolvers. It is a no-op
// when no permission_classes property is present (the action inherits the
// class-level posture stamped by applyDRFPermissionProperties). An explicit
// `[AllowAny]` marks the endpoint public.
func stampDRFActionAuth(props map[string]string) {
	if props == nil {
		return
	}
	raw, ok := props["permission_classes"]
	if !ok || strings.TrimSpace(raw) == "" {
		return
	}
	var nonPublic []string
	for _, p := range strings.Split(raw, ",") {
		leaf := permissionLeafName(p)
		if leaf == "" {
			continue
		}
		if !drfPublicPermissionClasses[leaf] {
			nonPublic = append(nonPublic, leaf)
		}
	}
	props["auth_method"] = "permission_classes"
	props["auth_confidence"] = "high"
	if len(nonPublic) == 0 {
		// Only AllowAny → explicitly public.
		props["auth_required"] = "false"
		return
	}
	props["auth_required"] = "true"
	props["auth_guard"] = nonPublic[0]
}

// isGetPermissionsDef reports whether fnDef is `def get_permissions(self):`.
func isGetPermissionsDef(fnDef ts.Node, src []byte) bool {
	if fnDef == nil {
		return false
	}
	nameNode := fnDef.ChildByFieldName("name")
	return nameNode != nil && nodeText(nameNode, src) == "get_permissions"
}

// getPermissionsReferencedClasses collects identifier names referenced inside a
// get_permissions() body that look like DRF permission classes.  It walks the
// body for any `permission_classes = [...]` assignments (the canonical pattern)
// and, failing that, falls back to identifiers in the returned expression.
//
// The result is a best-effort, deduplicated list of the bare identifier names
// (e.g. "IsAuthenticated", "AllowAny", "CustomActionPermissionCheck"). The
// auth_coverage detector decides which of those constitute real protection.
func getPermissionsReferencedClasses(fnDef ts.Node, src []byte) []string {
	body := fnDef.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	collectPermissionAssignments(body, src, add)
	return out
}

// collectPermissionAssignments recursively walks a statement subtree and, for
// every `permission_classes = [...]` assignment (regardless of nesting depth —
// these typically live inside if/elif branches), feeds the list-literal
// identifiers to add. Falls through to `return [...]` expressions too.
func collectPermissionAssignments(n ts.Node, src []byte, add func(string)) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "assignment":
		lhs := n.ChildByFieldName("left")
		if lhs != nil && lhs.Type() == "identifier" && nodeText(lhs, src) == "permission_classes" {
			rhs := n.ChildByFieldName("right")
			for _, id := range parseListLiteralIdentifiers(rhs, src) {
				add(permissionLeafName(id))
			}
		}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		collectPermissionAssignments(n.Child(i), src, add)
	}
}

// permissionLeafName reduces a possibly-dotted or called permission reference
// to its leaf class name so the detector matches against a stable token:
//
//	IsAuthenticated                     → IsAuthenticated
//	permissions.IsAdminUser             → IsAdminUser
//	CustomPagePermissionCheck(PAGES[X]) → CustomPagePermissionCheck
func permissionLeafName(ref string) string {
	ref = strings.TrimSpace(ref)
	if paren := strings.IndexByte(ref, '('); paren >= 0 {
		ref = ref[:paren]
	}
	if dot := strings.LastIndexByte(ref, '.'); dot >= 0 {
		ref = ref[dot+1:]
	}
	return strings.TrimSpace(ref)
}
