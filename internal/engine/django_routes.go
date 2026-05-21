// AST-driven Django REST Framework route composition.
//
// The YAML rule engine treats `path("api/", include(router.urls))` and
// `router.register(r"users", UserViewSet)` as independent regex matches and
// emits two orphan Route entities — `Route:api/` and `Route:users`. The real
// HTTP route is `/api/users`: the parent `path()` prefix composes with each
// `<router>.register(...)` registration. Regex-only YAML rules can't do that
// composition because they don't see the router-variable binding.
//
// This pass walks the tree-sitter Python CST, finds every `path(prefix,
// include(<router>.urls))` call to learn the prefix bound to each router
// variable, then emits composed `Route:<prefix><name>` entities (and matching
// `ROUTES_TO` edges) for every `<router>.register("<name>", <ViewSet>)` call
// in the same file. The pass also reports the bare router-name and bare
// include-prefix paths it "claimed" so the surrounding engine can suppress
// the duplicate flat Routes the YAML rules would otherwise emit.
//
// Cross-file suppression (#1278): when router.register() calls live in one
// file and path("api/v1/", include(router.urls)) lives in another file, the
// per-file claimedRegisterNames set is empty during the register-file pass and
// cannot suppress the bare YAML Route entities. A global pre-pass registry
// (drfGlobalRegisterNames) is populated by ScanDRFRegisterNames before
// per-file extraction begins. The suppression gate in applyDjangoRouteComposition
// now also consults this global set so cross-file bare Routes are correctly dropped.
//
// Refs #64, #1278.
package engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/treesitter"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Cross-file DRF register-name registry (#1278)
// ---------------------------------------------------------------------------

// drfRegisterScanRe is a lightweight line-based regex that captures every
// router.register() basename from a Python file without an AST parse. Used
// in the cross-file pre-pass (ScanDRFRegisterNames) to build a global set
// of claimed register names before per-file extraction runs.
//
// Accepts both raw-string (r"name") and plain-string ("name" / 'name') forms.
// Anchors the receiver to router-like identifiers (same pattern as the YAML
// rule) to avoid false positives from unrelated .register() calls.
var drfRegisterScanRe = regexp.MustCompile(
	`(?:[\w]*[Rr]outer|api_router|v\d+_router|router_v\d+)\.register\s*\(\s*r?["']([^"']+)["']`,
)

// drfGlobalMu guards drfGlobalRegisterNames for concurrent pre-pass callers.
var drfGlobalMu sync.RWMutex

// drfGlobalRegisterNames is the cross-repo set of all router.register()
// basenames found during the ScanDRFRegisterNames pre-pass. It is consulted
// by applyDjangoRouteComposition when the per-file claimedRegisterNames set
// is empty (cross-file scenario). Reset by ClearDRFRegisterNames at the
// start of each index run.
var drfGlobalRegisterNames = map[string]bool{}

// ScanDRFRegisterNames scans a single Python file's content and merges every
// router.register() basename into the global cross-file registry. Safe for
// concurrent calls from parallel file-walkers.
//
// Call this for every .py file BEFORE per-file extraction begins (same
// lifecycle as pyextr.ScanPythonClassRegistry). The global set is then
// available to applyDjangoRouteComposition when suppressing cross-file ghosts.
func ScanDRFRegisterNames(content []byte) {
	if len(content) == 0 {
		return
	}
	matches := drfRegisterScanRe.FindAllSubmatch(content, -1)
	if len(matches) == 0 {
		return
	}
	drfGlobalMu.Lock()
	defer drfGlobalMu.Unlock()
	for _, m := range matches {
		if len(m) >= 2 && len(m[1]) > 0 {
			drfGlobalRegisterNames[string(m[1])] = true
		}
	}
}

// ClearDRFRegisterNames resets the global DRF register-name registry.
// Call at the start of each index run and in test teardowns.
func ClearDRFRegisterNames() {
	drfGlobalMu.Lock()
	defer drfGlobalMu.Unlock()
	drfGlobalRegisterNames = map[string]bool{}
}

// isDRFGlobalRegisterName reports whether name appears in the cross-file
// DRF register-name registry populated by the ScanDRFRegisterNames pre-pass.
func isDRFGlobalRegisterName(name string) bool {
	drfGlobalMu.RLock()
	defer drfGlobalMu.RUnlock()
	return drfGlobalRegisterNames[name]
}

// composedDjangoRoutes holds the output of the Django AST pass.
type composedDjangoRoutes struct {
	// entities are the composed Route entity records (one per router.register).
	entities []types.EntityRecord
	// relationships are the composed ROUTES_TO records pointing at the ViewSet.
	relationships []types.RelationshipRecord
	// claimedRegisterNames is the set of bare register names this pass
	// consumed (e.g. "users", "orders"). Used to drop the duplicate orphan
	// Route entities the YAML rules emitted from the same `router.register`
	// calls (and the matching ROUTES_TO edges they emitted).
	claimedRegisterNames map[string]bool
	// claimedIncludePrefixes is the set of bare path() prefixes this pass
	// consumed because they were bound to a router via include(<router>.urls)
	// (e.g. "api/"). Used to drop the orphan Route entity the YAML rules
	// emit from the same `path()` call.
	claimedIncludePrefixes map[string]bool
}

// applyDjangoRouteComposition runs the Django AST pass on a Python file and
// merges its output with the YAML rules' raw entities/relationships,
// dropping the now-redundant flat Routes and the orphan parent-path Route.
//
// `lang` lets the engine no-op cleanly for non-Python files.
//
// #1278 — two-phase suppression:
//
//  1. Global cross-file suppression: always applied for any Python file. Drops
//     YAML Route entities and ROUTES_TO edges whose bare name appears in the
//     cross-file DRF register-name registry (built by the ScanDRFRegisterNames
//     pre-pass). This handles the case where router.register() calls live in
//     one file but the include(router.urls) call lives in another file.
//
//  2. Per-file composed suppression: applied only when the file contains both
//     include() and .register() calls (same-file composition). Replaces the
//     bare Route entities with the AST-composed prefixed routes.
func applyDjangoRouteComposition(
	ctx context.Context,
	lang string,
	path string,
	content []byte,
	rawEntities []types.EntityRecord,
	rawRels []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if lang != "python" || len(content) == 0 {
		return rawEntities, rawRels
	}

	// Phase 1 — global cross-file suppression (#1278): drop bare YAML Route
	// entities (and their ROUTES_TO edges) whose name is in the global
	// register-name set, regardless of whether this file contains an include()
	// binding. This handles register-only files (no include() in this file)
	// where the per-file claimedRegisterNames set would be empty.
	//
	// We apply this even for files that will also go through Phase 2 (the
	// same-file composition path) to keep the logic uniform; Phase 2's
	// claimedRegisterNames set is a strict subset of the global set.
	entities := suppressGlobalDRFOrphans(path, rawEntities, rawRels)
	rawEntities = entities.ents
	rawRels = entities.rels

	// Phase 2 — per-file AST-driven composed suppression (original #64 logic):
	// only fires when the file contains both include() and .register() calls.
	// Cheap pre-filter: a DRF-composing urls.py has at minimum a `path(`
	// and an `include(` somewhere, plus a `.register(` for the router.
	if !bytesContainsAll(content, "path(", "include(", ".register(") {
		return rawEntities, rawRels
	}

	composed, ok := extractDjangoComposedRoutes(ctx, path, content)
	if !ok || len(composed.entities) == 0 {
		return rawEntities, rawRels
	}

	// Drop YAML Route entities whose Name matches a claimed register name
	// (we replaced them with the composed version) or matches a claimed
	// include prefix (orphan from the bare `path()` call).
	filteredEntities := rawEntities[:0:0]
	for _, e := range rawEntities {
		if e.Kind == "Route" && e.SourceFile == path {
			if composed.claimedRegisterNames[e.Name] || composed.claimedIncludePrefixes[e.Name] {
				continue
			}
		}
		filteredEntities = append(filteredEntities, e)
	}
	filteredEntities = append(filteredEntities, composed.entities...)

	// Drop YAML ROUTES_TO edges whose source Route is one of the bare
	// register names we just replaced. The YAML edge is
	// `Route:<bare> -> View:<ViewSet>`; we emit the composed equivalent.
	filteredRels := rawRels[:0:0]
	for _, r := range rawRels {
		if r.Kind == "ROUTES_TO" && strings.HasPrefix(r.FromID, "Route:") {
			bare := strings.TrimPrefix(r.FromID, "Route:")
			if composed.claimedRegisterNames[bare] {
				continue
			}
		}
		filteredRels = append(filteredRels, r)
	}
	filteredRels = append(filteredRels, composed.relationships...)

	return filteredEntities, filteredRels
}

// suppressedEntities bundles filtered entity + relationship slices.
type suppressedEntities struct {
	ents []types.EntityRecord
	rels []types.RelationshipRecord
}

// suppressGlobalDRFOrphans drops bare YAML Route entities and their ROUTES_TO
// edges from rawEntities/rawRels when the entity name appears in the global
// cross-file DRF register-name registry. This is Phase 1 of the #1278 fix.
//
// Returns a new suppressedEntities with the filtered results. When the global
// registry is empty (no router.register() found anywhere in the repo, or the
// pre-pass hasn't run yet), the inputs are returned unchanged.
func suppressGlobalDRFOrphans(
	filePath string,
	rawEntities []types.EntityRecord,
	rawRels []types.RelationshipRecord,
) suppressedEntities {
	// Fast path: if the global set is empty nothing to do.
	drfGlobalMu.RLock()
	globalEmpty := len(drfGlobalRegisterNames) == 0
	drfGlobalMu.RUnlock()
	if globalEmpty {
		return suppressedEntities{ents: rawEntities, rels: rawRels}
	}

	// Check whether any entity in this file is a candidate for suppression
	// before allocating new slices. This keeps the hot path allocation-free
	// for files with no YAML Route entities.
	hasCandidates := false
	for _, e := range rawEntities {
		if e.Kind == "Route" && e.SourceFile == filePath && isDRFGlobalRegisterName(e.Name) {
			hasCandidates = true
			break
		}
	}
	if !hasCandidates {
		// Also check rels in case an orphan edge slipped through without a
		// corresponding entity (unusual but possible with out-of-order processing).
		for _, r := range rawRels {
			if r.Kind == "ROUTES_TO" && strings.HasPrefix(r.FromID, "Route:") {
				bare := strings.TrimPrefix(r.FromID, "Route:")
				if isDRFGlobalRegisterName(bare) {
					hasCandidates = true
					break
				}
			}
		}
	}
	if !hasCandidates {
		return suppressedEntities{ents: rawEntities, rels: rawRels}
	}

	filteredEnts := rawEntities[:0:0]
	for _, e := range rawEntities {
		if e.Kind == "Route" && e.SourceFile == filePath && isDRFGlobalRegisterName(e.Name) {
			continue // suppress cross-file ghost
		}
		filteredEnts = append(filteredEnts, e)
	}

	filteredRels := rawRels[:0:0]
	for _, r := range rawRels {
		if r.Kind == "ROUTES_TO" && strings.HasPrefix(r.FromID, "Route:") {
			bare := strings.TrimPrefix(r.FromID, "Route:")
			if isDRFGlobalRegisterName(bare) {
				continue // suppress orphan ROUTES_TO edge
			}
		}
		filteredRels = append(filteredRels, r)
	}

	return suppressedEntities{ents: filteredEnts, rels: filteredRels}
}

// bytesContainsAll returns true iff every needle is present in content.
func bytesContainsAll(content []byte, needles ...string) bool {
	s := string(content)
	for _, n := range needles {
		if !strings.Contains(s, n) {
			return false
		}
	}
	return true
}

// extractDjangoComposedRoutes parses the Python source, walks the CST, and
// returns composed DRF routes for every `<router>.register(...)` call whose
// router variable is bound to a prefix via `path("<prefix>", include(
// <router>.urls))`.
func extractDjangoComposedRoutes(ctx context.Context, path string, content []byte) (composedDjangoRoutes, bool) {
	out := composedDjangoRoutes{
		claimedRegisterNames:   map[string]bool{},
		claimedIncludePrefixes: map[string]bool{},
	}

	factory := treesitter.NewParserFactory(nil)
	pr, err := factory.Parse(ctx, content, "python")
	if err != nil || pr == nil || pr.Tree == nil {
		return out, false
	}

	root := pr.Tree.RootNode()

	// Pass 1: collect all `path("<prefix>", include(<router>.urls))` bindings.
	// router var name -> prefix string (raw, as written in source, without
	// any quote stripping beyond the literal value).
	routerPrefixes := map[string]string{}
	collectIncludePrefixes(root, content, routerPrefixes, out.claimedIncludePrefixes)

	// Pass 2: for every `<router>.register("<name>", <ViewSet>)` whose
	// receiver is in routerPrefixes, emit a composed Route + ROUTES_TO.
	collectRegisterCalls(root, content, path, routerPrefixes, &out)

	return out, true
}

// collectIncludePrefixes walks the tree, identifies every
// `path("<prefix>", include(<router>.urls))` call, and records the
// router-variable -> prefix binding. The prefix is also added to
// claimedIncludePrefixes so the engine can drop the duplicate YAML Route.
func collectIncludePrefixes(
	node *sitter.Node,
	src []byte,
	routerPrefixes map[string]string,
	claimedPrefixes map[string]bool,
) {
	if node == nil {
		return
	}
	if node.Type() == "call" {
		if name := callFunctionName(node, src); name == "path" || name == "re_path" {
			prefix, routerVar, ok := pathIncludeBinding(node, src)
			if ok {
				routerPrefixes[routerVar] = prefix
				claimedPrefixes[prefix] = true
			}
		}
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		collectIncludePrefixes(node.Child(i), src, routerPrefixes, claimedPrefixes)
	}
}

// pathIncludeBinding inspects a `path(...)` call node. If its arguments are
// (string_prefix, include(<router>.urls)) it returns the prefix string,
// the router variable name, and true.
func pathIncludeBinding(call *sitter.Node, src []byte) (string, string, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return "", "", false
	}
	// Walk the argument list: first positional must be a string literal,
	// second positional must be `include(<router>.urls)`.
	var positional []*sitter.Node
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		t := ch.Type()
		if t == "(" || t == ")" || t == "," {
			continue
		}
		// Skip keyword arguments like `name="..."`.
		if t == "keyword_argument" {
			continue
		}
		positional = append(positional, ch)
	}
	if len(positional) < 2 {
		return "", "", false
	}
	prefix, ok := stringLiteralValue(positional[0], src)
	if !ok {
		return "", "", false
	}
	routerVar, ok := includeRouterUrls(positional[1], src)
	if !ok {
		return "", "", false
	}
	return prefix, routerVar, true
}

// includeRouterUrls inspects a node that should be `include(<router>.urls)`
// and returns the router variable name on success.
func includeRouterUrls(node *sitter.Node, src []byte) (string, bool) {
	if node == nil || node.Type() != "call" {
		return "", false
	}
	if callFunctionName(node, src) != "include" {
		return "", false
	}
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	// Look for an `attribute` child whose attribute is `urls`.
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch.Type() != "attribute" {
			continue
		}
		objNode := ch.ChildByFieldName("object")
		attrNode := ch.ChildByFieldName("attribute")
		if objNode == nil || attrNode == nil {
			continue
		}
		if nodeText(attrNode, src) != "urls" {
			continue
		}
		if objNode.Type() != "identifier" {
			continue
		}
		return nodeText(objNode, src), true
	}
	return "", false
}

// collectRegisterCalls walks the tree, finds every
// `<router>.register("<name>", <ViewSet>)` call whose receiver is in
// routerPrefixes, and emits composed Route + ROUTES_TO records.
func collectRegisterCalls(
	node *sitter.Node,
	src []byte,
	path string,
	routerPrefixes map[string]string,
	out *composedDjangoRoutes,
) {
	if node == nil {
		return
	}
	if node.Type() == "call" {
		if receiver, method := callReceiverAndMethod(node, src); method == "register" && receiver != "" {
			if prefix, ok := routerPrefixes[receiver]; ok {
				name, viewSet, parsed := registerArgs(node, src)
				if parsed {
					out.claimedRegisterNames[name] = true
					composedPath := joinDjangoRoutePaths(prefix, name)

					out.entities = append(out.entities, types.EntityRecord{
						Name:       composedPath,
						Kind:       "Route",
						SourceFile: path,
						Language:   "python",
						Properties: map[string]string{
							"framework":    "python",
							"pattern_type": "ast_driven",
						},
						EnrichmentRequired: false,
						EnrichmentStatus:   types.StatusPending,
						QualityScore:       0.7,
					})
					if viewSet != "" {
						out.relationships = append(out.relationships, types.RelationshipRecord{
							FromID: fmt.Sprintf("Route:%s", composedPath),
							ToID:   fmt.Sprintf("View:%s", viewSet),
							Kind:   "ROUTES_TO",
							Properties: map[string]string{
								"framework":    "python",
								"pattern_type": "ast_driven",
							},
						})
					}
				}
			}
		}
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		collectRegisterCalls(node.Child(i), src, path, routerPrefixes, out)
	}
}

// registerArgs parses the arguments of a `<router>.register(...)` call. It
// returns (name, viewSet, true) on success. `name` is the bare URL
// fragment (e.g. "users"); `viewSet` is the identifier passed as the second
// positional argument (e.g. "UserViewSet").
func registerArgs(call *sitter.Node, src []byte) (string, string, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return "", "", false
	}
	var positional []*sitter.Node
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		t := ch.Type()
		if t == "(" || t == ")" || t == "," {
			continue
		}
		if t == "keyword_argument" {
			continue
		}
		positional = append(positional, ch)
	}
	if len(positional) < 1 {
		return "", "", false
	}
	name, ok := stringLiteralValue(positional[0], src)
	if !ok {
		return "", "", false
	}
	viewSet := ""
	if len(positional) >= 2 && positional[1].Type() == "identifier" {
		viewSet = nodeText(positional[1], src)
	}
	return name, viewSet, true
}

// callFunctionName returns the bare function name of a `call` node when the
// callee is a simple identifier. Returns "" for attribute calls.
func callFunctionName(call *sitter.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	if fn.Type() == "identifier" {
		return nodeText(fn, src)
	}
	return ""
}

// callReceiverAndMethod returns (receiver, method) when the call is
// `<receiver>.<method>(...)` with a simple identifier receiver. Otherwise
// returns ("", "").
func callReceiverAndMethod(call *sitter.Node, src []byte) (string, string) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "attribute" {
		return "", ""
	}
	obj := fn.ChildByFieldName("object")
	attr := fn.ChildByFieldName("attribute")
	if obj == nil || attr == nil {
		return "", ""
	}
	if obj.Type() != "identifier" {
		return "", ""
	}
	return nodeText(obj, src), nodeText(attr, src)
}

// stringLiteralValue extracts the literal text of a Python string node,
// supporting the raw-string form (`r"..."` / `r'...'`) used by Django URL
// patterns. It returns the inner text (without the surrounding quotes) and
// true on success.
func stringLiteralValue(node *sitter.Node, src []byte) (string, bool) {
	if node == nil || node.Type() != "string" {
		return "", false
	}
	// A tree-sitter Python `string` node typically has `string_start`,
	// (`string_content` | `escape_sequence`)*, `string_end` children. The
	// raw-string prefix is part of `string_start` (e.g. `r"`).
	var inner strings.Builder
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		switch ch.Type() {
		case "string_content":
			inner.WriteString(nodeText(ch, src))
		case "escape_sequence":
			inner.WriteString(nodeText(ch, src))
		}
	}
	if inner.Len() > 0 {
		return inner.String(), true
	}
	// Fallback: strip the outer quotes from the raw text. Handles older
	// grammars that don't expose `string_content`.
	raw := nodeText(node, src)
	// Trim a leading raw-string prefix (`r`, `R`, `rb`, `br`, etc.).
	for len(raw) > 0 && (raw[0] == 'r' || raw[0] == 'R' || raw[0] == 'b' || raw[0] == 'B' || raw[0] == 'u' || raw[0] == 'U' || raw[0] == 'f' || raw[0] == 'F') {
		raw = raw[1:]
	}
	if len(raw) >= 2 {
		first, last := raw[0], raw[len(raw)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return raw[1 : len(raw)-1], true
		}
	}
	return "", false
}

// joinDjangoRoutePaths concatenates a parent `path()` prefix with a bare
// `register()` name, normalising the slash boundary so we don't produce
// `api//users` or `apiusers`. The output is always rooted with a leading
// `/` so the composed Route name reads like an HTTP route.
func joinDjangoRoutePaths(prefix, name string) string {
	// Strip leading slashes from each side; we re-add a single leading
	// slash to the composed result.
	p := strings.TrimPrefix(prefix, "/")
	n := strings.TrimPrefix(name, "/")
	switch {
	case p == "" && n == "":
		return "/"
	case p == "":
		return "/" + n
	case n == "":
		return "/" + strings.TrimSuffix(p, "/")
	}
	if strings.HasSuffix(p, "/") {
		return "/" + p + n
	}
	return "/" + p + "/" + n
}
