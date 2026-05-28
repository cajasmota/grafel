package vue

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// Pinia dedicated-store entity model (issue #2890, deferred from #2876).
//
// Before #2890 a `defineStore('id', …)` call was modelled as a single thin
// SCOPE.Operation (subtype="state_store") with a CONTAINS edge from the
// component — it recorded only that a store existed (#2876 left pinia_store at
// `partial` for exactly this reason). Pinia stores are first-class state
// containers with a `state`, `getters` and `actions` surface; the honest
// `full` model entitizes the store itself plus each of its members so callers
// can navigate `store → state field / getter / action`.
//
// This file emits, for every `defineStore('id', { state, getters, actions })`
// (options syntax) or `defineStore('id', () => { … })` (setup syntax):
//
//	store          → SCOPE.Operation subtype="pinia_store"  name="store:<id>"
//	state field    → SCOPE.Operation subtype="pinia_state"  name="<id>.state.<f>"
//	getter         → SCOPE.Operation subtype="pinia_getter" name="<id>.getters.<g>"
//	action         → SCOPE.Operation subtype="pinia_action" name="<id>.actions.<a>"
//
// with a CONTAINS edge from the store entity to each member, and a CONTAINS
// edge from the enclosing component to the store entity. No new entity Kind is
// introduced — members decorate the existing SCOPE.Operation kind, mirroring
// how the Zustand store idiom entitizes its actions
// (internal/extractors/javascript/zustand_store.go).
//
// Setup-syntax stores expose their surface via `return { … }`; the returned
// names are recorded as members with subtype derived from their initializer
// (ref/reactive → state, computed → getter, function → action).

var (
	// `state: () => ({ … })` or `state: () => { return { … } }` — capture the
	// keyword position; the object body is located by brace matching.
	rePiniaState = regexp.MustCompile(`(?m)\bstate\s*:\s*\(\s*\)\s*=>`)
	// `getters: { … }` and `actions: { … }` — keyword position only.
	rePiniaGetters = regexp.MustCompile(`(?m)\bgetters\s*:\s*\{`)
	rePiniaActions = regexp.MustCompile(`(?m)\bactions\s*:\s*\{`)

	// A method/arrow member inside getters/actions: `name(…)` or `name: (…)=>`
	// or `name: function`.
	rePiniaMember = regexp.MustCompile(`(?m)^\s*(?:async\s+)?([A-Za-z_$][A-Za-z0-9_$]*)\s*(?:\([^)]*\)\s*\{|:\s*(?:async\s+)?(?:function|\([^)]*\)\s*=>|[A-Za-z_$][A-Za-z0-9_$]*\s*=>))`)

	// Setup-syntax member declarations inside the store factory body:
	// `const x = ref(…)` / `reactive(…)` → state; `computed(…)` → getter;
	// `function f(…)` or `const f = (…) =>` → action.
	rePiniaSetupState       = regexp.MustCompile(`(?m)\b(?:const|let)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*(?:ref|reactive|shallowRef|shallowReactive)\s*\(`)
	rePiniaSetupGetter      = regexp.MustCompile(`(?m)\b(?:const|let)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*computed\s*\(`)
	rePiniaSetupActionFn    = regexp.MustCompile(`(?m)\bfunction\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)
	rePiniaSetupActionArrow = regexp.MustCompile(`(?m)\b(?:const|let)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*(?:async\s+)?\([^)]*\)\s*=>`)
)

// matchBrace returns the index just past the brace that closes the brace at
// src[open]. It returns -1 if src[open] is not '{' or the brace is unbalanced.
func matchBrace(src string, open int) int {
	if open < 0 || open >= len(src) || src[open] != '{' {
		return -1
	}
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

type objectKey struct {
	name   string
	offset int
}

// topLevelKeys scans an object literal `{ … }` (src[0]=='{') and returns the
// `identifier:` keys declared at depth 1 — the immediate members of the object.
// It tracks brace/bracket/paren nesting and skips string/template literals so
// nested object values and array contents don't leak spurious keys. Offsets are
// relative to src. Handles both single-line (`{ a: 1, b: 2 }`) and multi-line
// object literals (Pinia state can be written either way).
func topLevelKeys(src string) []objectKey {
	var keys []objectKey
	depth := 0
	// pending holds the most recent depth-1 identifier run that has not yet been
	// confirmed (by ':') or discarded (by any other significant token).
	pendStart, pendEnd := -1, -1
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '"' || c == '\'' || c == '`':
			i++
			for i < len(src) {
				if src[i] == '\\' {
					i++
				} else if src[i] == c {
					break
				}
				i++
			}
			pendStart, pendEnd = -1, -1
		case c == '{' || c == '[' || c == '(':
			depth++
			pendStart, pendEnd = -1, -1
		case c == '}' || c == ']' || c == ')':
			depth--
			pendStart, pendEnd = -1, -1
		case c == ':':
			if depth == 1 && pendStart >= 0 {
				keys = append(keys, objectKey{name: src[pendStart:pendEnd], offset: pendStart})
			}
			pendStart, pendEnd = -1, -1
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			// whitespace separates tokens but does not discard a pending key.
		case depth == 1 && isIdentByte(c, pendEnd < 0):
			if pendEnd < 0 {
				pendStart = i
			} else if pendEnd != i {
				// non-contiguous identifier bytes (e.g. `a b:`) — restart.
				pendStart = i
			}
			pendEnd = i + 1
		default:
			pendStart, pendEnd = -1, -1
		}
	}
	return keys
}

func isIdentByte(c byte, first bool) bool {
	if c == '_' || c == '$' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
		return true
	}
	if !first && c >= '0' && c <= '9' {
		return true
	}
	return false
}

// matchParen returns the index just past the matching ')' for src[open]=='('.
func matchParen(src string, open int) int {
	if open < 0 || open >= len(src) || src[open] != '(' {
		return -1
	}
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// firstBraceFrom returns the index of the first '{' at or after pos, or -1.
func firstBraceFrom(src string, pos int) int {
	idx := strings.IndexByte(src[pos:], '{')
	if idx < 0 {
		return -1
	}
	return pos + idx
}

// extractPiniaStores parses every `defineStore('id', …)` call in scriptSrc and
// emits a dedicated store entity plus its state/getters/actions members. The
// returned entities carry a store→member CONTAINS edge; the caller is expected
// to add a component→store CONTAINS edge (returned separately as rels).
func extractPiniaStores(scriptSrc string, scriptOffset int, fullSrc, filePath, componentName string) (ents []types.EntityRecord, compRels []types.RelationshipRecord) {
	matches := rePiniaDefine.FindAllStringSubmatchIndex(scriptSrc, -1)
	for _, m := range matches {
		id := scriptSrc[m[2]:m[3]]

		// Locate the defineStore argument list so we can scope member parsing to
		// this store's body (and not bleed into sibling stores in the same file).
		parenOpen := strings.IndexByte(scriptSrc[m[0]:], '(')
		if parenOpen < 0 {
			continue
		}
		parenOpen += m[0]
		parenClose := matchParen(scriptSrc, parenOpen)
		if parenClose < 0 {
			parenClose = len(scriptSrc)
		}
		argRegion := scriptSrc[parenOpen:parenClose]
		argOffset := parenOpen

		storeName := "store:" + id
		storeLine := lineOf(fullSrc, scriptOffset+m[0])
		storeRef := storeName

		store := types.EntityRecord{
			Name:             storeName,
			QualifiedName:    fmt.Sprintf("%s.%s", componentName, storeName),
			Kind:             "SCOPE.Operation",
			Subtype:          "pinia_store",
			SourceFile:       filePath,
			Language:         "vue",
			StartLine:        storeLine,
			EndLine:          storeLine,
			Signature:        "defineStore('" + id + "')",
			QualityScore:     0.85,
			EnrichmentStatus: types.StatusPending,
			Properties: map[string]string{
				"state_lib": "pinia",
				"store_id":  id,
				"component": componentName,
				"framework": "vue",
			},
		}

		seen := map[string]bool{}
		addMember := func(section, subtype, member string, absOffset int) {
			member = strings.TrimSpace(member)
			if member == "" || isReservedWord(member) {
				return
			}
			key := section + ":" + member
			if seen[key] {
				return
			}
			seen[key] = true
			name := fmt.Sprintf("%s.%s.%s", id, section, member)
			ln := lineOf(fullSrc, absOffset)
			ents = append(ents, types.EntityRecord{
				Name:             name,
				QualifiedName:    fmt.Sprintf("%s.%s", componentName, name),
				Kind:             "SCOPE.Operation",
				Subtype:          subtype,
				SourceFile:       filePath,
				Language:         "vue",
				StartLine:        ln,
				EndLine:          ln,
				Signature:        section + " " + member,
				QualityScore:     0.8,
				EnrichmentStatus: types.StatusPending,
				Properties: map[string]string{
					"state_lib": "pinia",
					"store_id":  id,
					"member":    member,
					"section":   section,
					"component": componentName,
					"framework": "vue",
				},
			})
			// store → member CONTAINS edge.
			store.Relationships = append(store.Relationships, types.RelationshipRecord{
				ToID: name,
				Kind: "CONTAINS",
				Properties: map[string]string{
					"store":   id,
					"section": section,
					"subtype": subtype,
				},
			})
		}

		// ── options syntax: state / getters / actions sections ───────────────
		matchedOptions := false
		if loc := rePiniaState.FindStringIndex(argRegion); loc != nil {
			matchedOptions = true
			brace := firstBraceFrom(argRegion, loc[1])
			if brace >= 0 {
				end := matchBrace(argRegion, brace)
				if end < 0 {
					end = len(argRegion)
				}
				stateBody := argRegion[brace:end]
				for _, k := range topLevelKeys(stateBody) {
					addMember("state", "pinia_state", k.name, argOffset+scriptOffset+brace+k.offset)
				}
			}
		}
		matchedOptions = parseSection(argRegion, argOffset, scriptOffset, rePiniaGetters, "getters", "pinia_getter", addMember) || matchedOptions
		matchedOptions = parseSection(argRegion, argOffset, scriptOffset, rePiniaActions, "actions", "pinia_action", addMember) || matchedOptions

		// ── setup syntax: defineStore('id', () => { … return {…} }) ──────────
		// Only fall back to setup-member parsing when the options-object sections
		// were absent (a store is one syntax or the other).
		if !matchedOptions {
			for _, sm := range rePiniaSetupState.FindAllStringSubmatchIndex(argRegion, -1) {
				addMember("state", "pinia_state", argRegion[sm[2]:sm[3]], argOffset+scriptOffset+sm[0])
			}
			for _, sm := range rePiniaSetupGetter.FindAllStringSubmatchIndex(argRegion, -1) {
				addMember("getters", "pinia_getter", argRegion[sm[2]:sm[3]], argOffset+scriptOffset+sm[0])
			}
			for _, sm := range rePiniaSetupActionFn.FindAllStringSubmatchIndex(argRegion, -1) {
				addMember("actions", "pinia_action", argRegion[sm[2]:sm[3]], argOffset+scriptOffset+sm[0])
			}
			for _, sm := range rePiniaSetupActionArrow.FindAllStringSubmatchIndex(argRegion, -1) {
				addMember("actions", "pinia_action", argRegion[sm[2]:sm[3]], argOffset+scriptOffset+sm[0])
			}
		}

		ents = append([]types.EntityRecord{store}, ents...)
		// component → store CONTAINS edge.
		compRels = append(compRels, types.RelationshipRecord{
			ToID: storeRef,
			Kind: "CONTAINS",
			Properties: map[string]string{
				"component": componentName,
				"framework": "vue",
				"subtype":   "pinia_store",
			},
		})
	}
	return ents, compRels
}

// parseSection brace-matches a `getters: { … }` / `actions: { … }` block within
// argRegion and emits each method member found inside it. Returns true if the
// section keyword was present.
func parseSection(argRegion string, argOffset, scriptOffset int, kw *regexp.Regexp, section, subtype string, addMember func(section, subtype, member string, absOffset int)) bool {
	loc := kw.FindStringIndex(argRegion)
	if loc == nil {
		return false
	}
	// loc[1]-1 is the '{' that opened the section (the regex ends at `{`).
	brace := loc[1] - 1
	if brace < 0 || brace >= len(argRegion) || argRegion[brace] != '{' {
		brace = firstBraceFrom(argRegion, loc[1])
	}
	if brace < 0 {
		return true
	}
	end := matchBrace(argRegion, brace)
	if end < 0 {
		end = len(argRegion)
	}
	sectionBody := argRegion[brace:end]
	for _, mm := range rePiniaMember.FindAllStringSubmatchIndex(sectionBody, -1) {
		addMember(section, subtype, sectionBody[mm[2]:mm[3]], argOffset+scriptOffset+brace+mm[2])
	}
	return true
}

func isReservedWord(s string) bool {
	return jsKeywords[s]
}
