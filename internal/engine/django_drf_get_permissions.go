// django_drf_get_permissions.go — per-action DRF permission resolution (#3933).
//
// # Why this exists
//
// DRF ViewSets routinely vary their authorisation surface PER ACTION rather
// than declaring a single class-level `permission_classes`. The two canonical
// idioms are:
//
//	# (1) get_permissions() branching on self.action
//	class OrderViewSet(ModelViewSet):
//	    def get_permissions(self):
//	        if self.action == 'create':
//	            return [IsAdminUser()]
//	        elif self.action in ['list', 'retrieve']:
//	            return [AllowAny()]
//	        return [IsAuthenticated()]          # default
//
//	# (2) permission_classes_by_action dict
//	class OrderViewSet(ModelViewSet):
//	    permission_classes_by_action = {
//	        'create': [IsAdminUser],
//	        'list': [AllowAny],
//	        'default': [IsAuthenticated],
//	    }
//	    def get_permissions(self):
//	        try:
//	            return [p() for p in self.permission_classes_by_action[self.action]]
//	        except KeyError:
//	            return [p() for p in self.permission_classes_by_action['default']]
//
// Before #3933 the DRF expansion pass stamped only the flat class-level
// `permission_classes` union (via parseDRFPosture). A get_permissions override
// that branched per action collapsed to whatever flat `permission_classes`
// attribute happened to exist (often `IsAuthenticated`), so POST /orders
// (create → IsAdminUser) and GET /orders (list → AllowAny) both showed the same
// union — wrong on both the over- and under-protective sides.
//
// This pass parses the two idioms into a per-action permission map. The DRF
// expansion pass (emitOneCRUDFamily / emitActionRoutes) then overrides the flat
// posture's permission_classes with the per-action list for the matching CRUD
// or @action route, attaching the right permission to the right route.
//
// Honest-partial: a branch whose condition is not a statically resolvable
// `self.action == '<literal>'` / `self.action in [<literals>]` (e.g.
// `if self.request.user.is_staff:` or a computed action set) is skipped — the
// affected routes fall back to the flat-union posture (the pre-#3933 behaviour).

package engine

import (
	"regexp"
	"strings"
)

// drfGetPermissionsDefRe locates the `def get_permissions(self...):` declaration
// in a ViewSet class body. Group boundaries are unused — the match index marks
// where the method body parsing begins.
var drfGetPermissionsDefRe = regexp.MustCompile(`(?m)^[ \t]*def[ \t]+get_permissions[ \t]*\(`)

// drfSelfActionEqRe matches a `self.action == '<literal>'` (or `!=` is NOT
// matched — only equality narrows to a single action) condition. Group 1 is the
// quoted action name.
var drfSelfActionEqRe = regexp.MustCompile(`self\.action\s*==\s*["']([^"']+)["']`)

// drfSelfActionInRe matches a `self.action in [ '<a>', '<b>', ... ]` (or tuple /
// set) condition. Group 1 is the raw bracketed body of action-name literals.
var drfSelfActionInRe = regexp.MustCompile(`self\.action\s+in\s+[\[(\{]([^\])\}]*)[\])\}]`)

// drfReturnPermsRe matches a `return [ ... ]` (list/tuple) statement and
// captures the bracketed body so the permission class symbols can be pulled out
// with drfClassNames. The leading `return` keeps it from matching arbitrary
// list literals.
var drfReturnPermsRe = regexp.MustCompile(`return\s+[\[(]([^\])]*)[\])]`)

// drfPermissionsByActionDictRe locates a `permission_classes_by_action = { ... }`
// (or `_perms_by_action` style) dict assignment and captures the brace body.
// (?s) lets the dict span multiple lines. The first `}` closes it — DRF maps are
// flat string→list dicts, so nested braces do not occur in practice.
var drfPermissionsByActionDictRe = regexp.MustCompile(`(?s)permission_classes_by_action\s*=\s*\{([^}]*)\}`)

// drfDictEntryRe matches a `'<action>': [ <perms> ]` entry inside a
// permission_classes_by_action dict body. Group 1 is the action key, group 2 is
// the bracketed permission list body. Entries whose value is not a list literal
// are skipped (honest-partial).
var drfDictEntryRe = regexp.MustCompile(`["']([^"']+)["']\s*:\s*[\[(]([^\])]*)[\])]`)

// drfStringLiteralRe pulls the individual quoted string literals out of a
// `self.action in [...]` body.
var drfStringLiteralRe = regexp.MustCompile(`["']([^"']+)["']`)

// parseDRFActionPermissions resolves a per-action permission map from a ViewSet
// class body, handling both the get_permissions(self) self.action-branch idiom
// and the permission_classes_by_action dict idiom. Returns nil when neither is
// present or nothing statically resolvable was found (the routes then fall back
// to the flat-union posture — honest-partial).
//
// The returned map keys are DRF action names (CRUD verbs or @action method
// names); the value is the ordered list of permission-class leaf symbols. A key
// of "" carries the default branch that applies to any action not otherwise
// listed.
func parseDRFActionPermissions(classBody string) map[string][]string {
	out := map[string][]string{}

	// Idiom (2): permission_classes_by_action dict. Parsed first so an explicit
	// get_permissions self.action branch (idiom 1) can refine / override it.
	if m := drfPermissionsByActionDictRe.FindStringSubmatch(classBody); len(m) >= 2 {
		for _, e := range drfDictEntryRe.FindAllStringSubmatch(m[1], -1) {
			action := e[1]
			perms := finalDottedSegments(drfClassNames(e[2]))
			key := action
			if action == "default" {
				key = ""
			}
			// Always record the key (even when perms is empty, e.g. `[]` = open)
			// so the route is recognised as explicitly resolved.
			out[key] = perms
		}
	}

	// Idiom (1): get_permissions(self) branching on self.action.
	mergeGetPermissionsBranches(classBody, out)

	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeGetPermissionsBranches parses the body of `def get_permissions(self):`
// and merges its per-action permission resolutions into out. Each block (the
// suite under an `if/elif self.action ...:` header, or the method-level default
// `return [...]`) is matched to the `return [...]` it owns.
//
// The parser is line-oriented and indentation-aware: it splits the method body
// into header→return blocks. A header that is a resolvable self.action condition
// binds its return's permissions to the named action(s); a `return [...]` that
// is NOT guarded by a self.action condition (the method-level fallthrough or an
// `else:`) binds to the default key "". Non-resolvable conditions
// (`if self.request...`, computed sets) are skipped — honest-partial.
func mergeGetPermissionsBranches(classBody string, out map[string][]string) {
	loc := drfGetPermissionsDefRe.FindStringIndex(classBody)
	if loc == nil {
		return
	}
	// Body is everything after the def line until the next def/class at the
	// method's own (or lower) indentation. extractMethodBody handles dedent
	// boundary detection.
	body := extractDefBody(classBody[loc[0]:])
	if body == "" {
		return
	}

	lines := strings.Split(body, "\n")
	// pendingActions holds the action names that the *next* `return [...]` should
	// bind to. Empty slice + pendingDefault=false means "no guard seen yet" — a
	// return found in that state is the method-level default.
	var pendingActions []string
	pendingResolvable := true // whether the current guard was statically resolvable

	bindReturn := func(perms []string) {
		if len(pendingActions) == 0 {
			// Unguarded return → default branch.
			if pendingResolvable {
				if _, exists := out[""]; !exists {
					out[""] = perms
				}
			}
			return
		}
		for _, a := range pendingActions {
			if _, exists := out[a]; !exists {
				out[a] = perms
			}
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "if ") || strings.HasPrefix(trimmed, "elif "):
			actions, resolvable := resolveActionCondition(trimmed)
			pendingActions = actions
			pendingResolvable = resolvable
		case strings.HasPrefix(trimmed, "else:"):
			// else binds to the default key.
			pendingActions = nil
			pendingResolvable = true
		case strings.HasPrefix(trimmed, "return "):
			if rm := drfReturnPermsRe.FindStringSubmatch(trimmed); len(rm) >= 2 {
				perms := finalDottedSegments(drfClassNames(rm[1]))
				bindReturn(perms)
			}
			// A return that is not a list literal (e.g. a dict-lookup
			// comprehension) is left unresolved here — the dict idiom (parsed
			// separately) covers the common `permission_classes_by_action` case.
			// Reset the guard after every return so a subsequent top-level return
			// is treated as the default branch.
			pendingActions = nil
			pendingResolvable = true
		}
	}
}

// resolveActionCondition parses an `if`/`elif` header into the set of action
// names it narrows `self.action` to, and whether the condition was statically
// resolvable. `self.action == 'x'` → (["x"], true); `self.action in ['a','b']`
// → (["a","b"], true). Any other condition (computed, user-based, negated) →
// (nil, false) so the guarded return is skipped — honest-partial.
func resolveActionCondition(header string) (actions []string, resolvable bool) {
	if m := drfSelfActionEqRe.FindStringSubmatch(header); len(m) >= 2 {
		return []string{m[1]}, true
	}
	if m := drfSelfActionInRe.FindStringSubmatch(header); len(m) >= 2 {
		var names []string
		for _, sm := range drfStringLiteralRe.FindAllStringSubmatch(m[1], -1) {
			names = append(names, sm[1])
		}
		if len(names) > 0 {
			return names, true
		}
	}
	return nil, false
}

// extractDefBody returns the suite of a `def ...:` whose declaration starts at
// the head of src. It mirrors extractClassBody's boundary logic but is indent-
// relative: the body runs until the first non-blank line whose indentation is
// less than or equal to the `def` line's own indentation.
func extractDefBody(src string) string {
	lines := strings.Split(src, "\n")
	if len(lines) == 0 {
		return ""
	}
	defIndent := indentWidth(lines[0])
	var b strings.Builder
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		if indentWidth(line) <= defIndent {
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// indentWidth returns the leading-whitespace width of a line, counting a tab as
// one column (sufficient for boundary comparison — Python forbids mixing in a
// way that would defeat this for our purposes).
func indentWidth(line string) int {
	n := 0
	for _, r := range line {
		if r == ' ' || r == '\t' {
			n++
			continue
		}
		break
	}
	return n
}

// postureForAction returns the endpoint posture to stamp on the route backing
// the given DRF action. It starts from the ViewSet-level posture (#3864) and,
// when a per-action permission override exists for this action (#3933),
// replaces the posture's permissionClasses with the action-specific list. An
// action with no explicit entry inherits the default branch ("" key) when one
// was resolved; absent both, the flat-union ViewSet posture is returned
// unchanged (honest-partial).
func postureForAction(vc drfViewSetClass, action string) drfPosture {
	pos := vc.posture
	if vc.actionPermissions == nil {
		return pos
	}
	perms, ok := vc.actionPermissions[action]
	if !ok {
		// No explicit per-action entry — apply the resolved default branch if any.
		perms, ok = vc.actionPermissions[""]
		if !ok {
			return pos
		}
	}
	pos.permissionClasses = perms
	return pos
}
