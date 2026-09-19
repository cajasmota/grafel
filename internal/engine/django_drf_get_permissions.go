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

// drfReturnPermsComprehensionRe matches the comprehension return that closes the
// assign-then-return idiom: `return [ <call>(...) for <var> in permission_classes ]`.
// We only need to recognise that the method returns the assigned `permission_classes`
// local so the per-branch assignments are the real result (no captured groups
// beyond detection).
var drfReturnPermsComprehensionRe = regexp.MustCompile(`return\s+[\[(].*\bfor\b.*\bin\b.*\bpermission_classes\b`)

// drfPermissionPagesKeyRe matches a `PERMISSION_PAGES["<KEY>"]` (or single-quoted)
// constant-dict subscript — the fine-grained page-key argument of a custom
// page/action permission guard. Group 1 is the constant KEY (e.g. JURISDICTIONS),
// which is the stable per-action authorisation identity surfaced as
// `auth_permissions` (#3972). A dynamic / computed subscript does not match —
// honest-partial.
var drfPermissionPagesKeyRe = regexp.MustCompile(`PERMISSION_PAGES\s*\[\s*["']([^"']+)["']\s*\]`)

// drfBalancedListBody returns the body of the FIRST `[...]` list literal in src,
// with the outer brackets stripped, tracking nested ()/[]/{} so a permission
// entry that is itself a call with a subscript argument — e.g.
// `CustomPagePermissionCheck(PERMISSION_PAGES["JURISDICTIONS"])` — is captured
// WHOLE rather than truncated at the first inner `]`/`)` (the limitation of the
// flat `[^\])]` regexes). Only a square-bracket list is matched: a `return
// super().get_permissions()` or a bare call must NOT be mistaken for a permission
// list (it would otherwise bind an empty default branch). Returns ("", false)
// when no `[...]` list literal is found.
func drfBalancedListBody(src string) (string, bool) {
	open := strings.IndexByte(src, '[')
	if open < 0 {
		return "", false
	}
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			depth--
			if depth == 0 {
				return src[open+1 : i], true
			}
		}
	}
	return "", false
}

// drfSplitTopLevel splits a list body on top-level commas only — commas inside
// nested ()/[]/{} (a permission entry's call arguments / subscript) are not
// split points. Each returned segment is one permission-list entry.
func drfSplitTopLevel(body string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, body[start:i])
				start = i + 1
			}
		}
	}
	if start <= len(body) {
		out = append(out, body[start:])
	}
	return out
}

// drfPermissionItemClass returns the permission-class symbol of one list entry,
// i.e. the leading dotted identifier BEFORE any `(` instantiation, dropping the
// call arguments. `IsAuthenticated` → "IsAuthenticated";
// `CustomPagePermissionCheck(PERMISSION_PAGES["X"])` → "CustomPagePermissionCheck";
// `permissions.IsAdminUser` → "permissions.IsAdminUser" (finalDottedSegments
// later reduces it). Returns "" for an empty / non-identifier entry.
func drfPermissionItemClass(entry string) string {
	entry = strings.TrimSpace(entry)
	if paren := strings.IndexByte(entry, '('); paren >= 0 {
		entry = entry[:paren]
	}
	entry = strings.TrimSpace(entry)
	m := drfLeadingIdentRe.FindString(entry)
	return m
}

// drfLeadingIdentRe matches a bare/dotted Python identifier at the START of a
// permission-list entry.
var drfLeadingIdentRe = regexp.MustCompile(`^[A-Za-z_][\w.]*`)

// drfPermissionClasses extracts the per-entry permission-class symbols (final
// dotted segment) from a list body, splitting on top-level commas so each
// entry's call arguments (e.g. a PERMISSION_PAGES[...] page-key subscript) do
// NOT leak in as spurious class names. This replaces the flat
// `finalDottedSegments(drfClassNames(...))` path inside get_permissions parsing,
// which over-captured `PERMISSION_PAGES` and the page-key string as classes.
func drfPermissionClasses(listBody string) []string {
	var out []string
	for _, entry := range drfSplitTopLevel(listBody) {
		if cls := drfPermissionItemClass(entry); cls != "" {
			out = append(out, finalDottedSegment(cls))
		}
	}
	return out
}

// drfPageKeysIn extracts the ordered, de-duplicated set of PERMISSION_PAGES["KEY"]
// constant keys referenced in a permission-list body (e.g. the `[...]` of a
// `permission_classes = [...]` assignment or a `return [...]`). Returns nil when
// no static page-key subscript is present (honest-partial).
func drfPageKeysIn(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range drfPermissionPagesKeyRe.FindAllStringSubmatch(body, -1) {
		key := m[1]
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

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
func parseDRFActionPermissions(classBody string) (perms map[string][]string, pages map[string][]string) {
	out := map[string][]string{}
	pageOut := map[string][]string{}

	// Idiom (2): permission_classes_by_action dict. Parsed first so an explicit
	// get_permissions self.action branch (idiom 1) can refine / override it.
	if m := drfPermissionsByActionDictRe.FindStringSubmatch(classBody); len(m) >= 2 {
		for _, e := range drfDictEntryRe.FindAllStringSubmatch(m[1], -1) {
			action := e[1]
			perms := drfPermissionClasses(e[2])
			key := action
			if action == "default" {
				key = ""
			}
			// Always record the key (even when perms is empty, e.g. `[]` = open)
			// so the route is recognised as explicitly resolved.
			out[key] = perms
			if pk := drfPageKeysIn(e[2]); len(pk) > 0 {
				pageOut[key] = pk
			}
		}
	}

	// Idiom (1): get_permissions(self) branching on self.action.
	mergeGetPermissionsBranches(classBody, out, pageOut)

	if len(out) == 0 {
		out = nil
	}
	if len(pageOut) == 0 {
		pageOut = nil
	}
	return out, pageOut
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
func mergeGetPermissionsBranches(classBody string, out map[string][]string, pageOut map[string][]string) {
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
	// pendingActions holds the action names that the *next* result line (a
	// `return [...]` literal OR a `permission_classes = [...]` assignment in the
	// assign-then-return-comprehension idiom) should bind to. An empty slice with
	// pendingResolvable=true means "no guard / default branch" — a result found in
	// that state is the method-level default.
	var pendingActions []string
	pendingResolvable := true // whether the current guard was statically resolvable

	// bindResult binds a resolved permission-class list (and any page-keys) to the
	// pending guard's action(s), or to the default key "" when unguarded. First
	// binding wins (matches the existing precedence: an earlier branch is not
	// overwritten by a later default).
	bindResult := func(perms, pages []string) {
		targets := pendingActions
		if len(targets) == 0 {
			// Unguarded / default branch — only bind when the (absent) guard was
			// resolvable (a dynamic guard sets pendingResolvable=false and is skipped).
			if !pendingResolvable {
				return
			}
			targets = []string{""}
		}
		for _, a := range targets {
			if _, exists := out[a]; !exists {
				out[a] = perms
			}
			if len(pages) > 0 {
				if _, exists := pageOut[a]; !exists {
					pageOut[a] = pages
				}
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
		case strings.HasPrefix(trimmed, "permission_classes"):
			// Assign-then-return-comprehension idiom: each branch assigns the local
			// `permission_classes = [...]`, and the method ends with
			// `return [p() for p in permission_classes]`. The assignment IS the
			// branch result (#3972). Bind it like a return literal.
			if eq := strings.IndexByte(line, '='); eq >= 0 {
				if listBody, ok := drfBalancedListBody(line[eq+1:]); ok {
					bindResult(drfPermissionClasses(listBody), drfPageKeysIn(listBody))
				}
			}
			// Reset the guard so a stray subsequent unguarded assignment is not
			// mis-bound; the closing comprehension return is ignored below.
			pendingActions = nil
			pendingResolvable = true
		case strings.HasPrefix(trimmed, "return "):
			// The closing comprehension `return [p() for p in permission_classes]`
			// of the assign-then-return idiom is NOT a per-branch literal — skip it
			// so it is not mis-bound as the default branch (its perms came from the
			// assignments above).
			if drfReturnPermsComprehensionRe.MatchString(trimmed) {
				pendingActions = nil
				pendingResolvable = true
				break
			}
			// Direct `return [<perms>]` literal (the #3933 idiom). Balanced
			// extraction so a permission entry instantiated with a nested
			// subscript/call argument is captured whole (#3972).
			if listBody, ok := drfBalancedListBody(trimmed); ok {
				bindResult(drfPermissionClasses(listBody), drfPageKeysIn(listBody))
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
	// #7200: no length guard needed — strings.Split with a non-empty separator
	// always returns >= 1 element, so the index below cannot be out of range.
	// Full derivation: extractIndentBody in internal/extractors/nim/nim.go.
	lines := strings.Split(src, "\n")
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
	// #3972 — resolve the per-action page-key identity independently of the
	// permission-class list. Both share the same action→default("") keying. An
	// action with an explicit per-action page-key entry uses it; otherwise it
	// inherits the default branch's page-keys (if any). Absent both, no page-key
	// is stamped (honest-partial). Resolved even when actionPermissions is nil
	// is impossible (they are produced together), but the lookups are guarded.
	if pages, ok := actionLookup(vc.actionPermissionPages, action); ok {
		pos.permissionPages = pages
	}
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

// actionLookup resolves a per-action value from a map keyed by action name with
// "" as the default branch: an explicit entry for action wins, else the default
// ("") entry, else (nil, false). Mirrors the precedence postureForAction applies
// to actionPermissions so the page-key map tracks the same per-action shape.
func actionLookup(m map[string][]string, action string) ([]string, bool) {
	if m == nil {
		return nil, false
	}
	if v, ok := m[action]; ok {
		return v, true
	}
	if v, ok := m[""]; ok {
		return v, true
	}
	return nil, false
}
