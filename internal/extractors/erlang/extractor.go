// Package erlang implements a regex-based extractor for Erlang source files.
//
// Extracted entities:
//   - module attributes (-module(foo).)           → Kind="SCOPE.Component", Subtype="module"
//   - OTP modules (-behaviour(gen_server).)        → Subtype refined to gen_server_module / supervisor_module / application_module / otp_module; Properties["otp_behaviour"], Tags ["otp", "otp:gen_server", ...]
//   - function clauses (name(Args) -> body.)       → Kind="SCOPE.Operation", Subtype="function" / "exported_function"
//   - OTP callbacks (handle_call/3, init/1, ...)   → Subtype="otp_callback", Properties["otp_callback_of"]
//   - record attributes (-record(Foo, {...}).)     → Kind="SCOPE.Component", Subtype="record"
//   - include attributes (-include("foo.hrl").)   → IMPORTS relationships
//
// Relationships emitted:
//   - IMPORTS — every -include/-include_lib attribute
//   - CALLS   — Module:Function and bare Function invocations inside function bodies
//   - CONTAINS — module entity links to each exported function
//
// No tree-sitter grammar for Erlang is available in smacker/go-tree-sitter.
// This extractor uses line-oriented regex parsing, matching the Nim extractor
// precedent (internal/extractors/nim/nim.go).
//
// Registers itself via init() and is imported by registry_gen.go.
package erlang

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("erlang", &Extractor{})
}

// Extractor implements extractor.Extractor for Erlang.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "erlang" }

// ---------------------------------------------------------------------------
// Compiled regular expressions
// ---------------------------------------------------------------------------

var (
	// moduleRE matches -module(foo). attribute.
	// Group 1: module name (atom).
	moduleRE = regexp.MustCompile(
		`(?m)^-module\s*\(\s*([a-z][a-zA-Z0-9_@]*)\s*\)\s*\.`,
	)

	// exportRE matches -export([...]) attribute.
	// Group 1: export list content.
	exportRE = regexp.MustCompile(
		`(?m)^-export\s*\(\s*\[([^\]]*)\]\s*\)\s*\.`,
	)

	// recordRE matches -record(RecordName, {fields}).
	// Group 1: record name.
	recordRE = regexp.MustCompile(
		`(?m)^-record\s*\(\s*([a-z][a-zA-Z0-9_@]*)\s*,`,
	)

	// includeRE matches -include("file.hrl") and -include_lib("app/include/file.hrl").
	// Group 1: the file path string.
	includeRE = regexp.MustCompile(
		`(?m)^-include(?:_lib)?\s*\(\s*"([^"]+)"\s*\)\s*\.`,
	)

	// funcHeadRE matches a function clause head.
	// Erlang function heads: name(Args) -> or name(Args) when Guard ->
	// Group 1: function name; Group 2 (optional): arguments text.
	funcHeadRE = regexp.MustCompile(
		`(?m)^([a-z_][a-zA-Z0-9_@]*)\s*(\([^)]*\))\s*(?:when\s+[^->\n]+)?\s*->`,
	)

	// callQualifiedRE matches Module:Function( invocations.
	// Group 1: module; Group 2: function.
	callQualifiedRE = regexp.MustCompile(
		`\b([a-z_][a-zA-Z0-9_@]*)\s*:\s*([a-z_][a-zA-Z0-9_@!?]*)\s*\(`,
	)

	// callBareRE matches bare function calls: name( — not preceded by ':'.
	// Group 1: function name.
	callBareRE = regexp.MustCompile(
		`(?:^|[^:a-zA-Z0-9_@])([a-z_][a-zA-Z0-9_@!?]*)\s*\(`,
	)

	// exportItemRE matches a single Name/Arity entry in an export list.
	// Group 1: function name; Group 2: arity (ignored for entity matching).
	exportItemRE = regexp.MustCompile(
		`([a-z_][a-zA-Z0-9_@]*)\s*/\s*(\d+)`,
	)

	// behaviourRE matches -behaviour(gen_server). / -behavior(gen_server).
	// (both British and American spellings are valid Erlang). Group 1: the
	// behaviour atom (e.g. gen_server, supervisor, application, gen_statem,
	// gen_event, gen_fsm).
	behaviourRE = regexp.MustCompile(
		`(?m)^-behaviou?r\s*\(\s*([a-z][a-zA-Z0-9_@]*)\s*\)\s*\.`,
	)
)

// otpCallbacks maps each OTP behaviour to the canonical callback function
// names it requires. Functions whose name is a known callback of a behaviour
// the module implements are tagged so supervision-tree / message-dispatch
// analysis can find them. Source: OTP design principles (gen_server,
// gen_statem, gen_event, supervisor, application, gen_fsm).
var otpCallbacks = map[string]map[string]bool{
	"gen_server": {
		"init": true, "handle_call": true, "handle_cast": true,
		"handle_info": true, "terminate": true, "code_change": true,
		"format_status": true,
	},
	"gen_statem": {
		"init": true, "callback_mode": true, "handle_event": true,
		"terminate": true, "code_change": true, "format_status": true,
	},
	"gen_event": {
		"init": true, "handle_event": true, "handle_call": true,
		"handle_info": true, "terminate": true, "code_change": true,
		"format_status": true,
	},
	"gen_fsm": {
		"init": true, "handle_event": true, "handle_sync_event": true,
		"handle_info": true, "terminate": true, "code_change": true,
	},
	"supervisor": {
		"init": true,
	},
	"application": {
		"start": true, "stop": true, "prep_stop": true, "config_change": true,
	},
}

// erlangKeywords are tokens that match funcHead or call patterns but are NOT
// real function names or call targets.
var erlangKeywords = map[string]bool{
	"if": true, "case": true, "receive": true, "begin": true, "try": true,
	"catch": true, "when": true, "fun": true, "end": true, "of": true,
	"after": true, "throw": true, "exit": true, "error": true,
	"andalso": true, "orelse": true, "not": true, "and": true, "or": true,
	"xor": true, "band": true, "bor": true, "bxor": true, "bnot": true,
	"bsl": true, "bsr": true, "div": true, "rem": true,
	// Erlang BIFs that are effectively keywords.
	"is_atom": true, "is_binary": true, "is_boolean": true, "is_float": true,
	"is_function": true, "is_integer": true, "is_list": true, "is_map": true,
	"is_number": true, "is_pid": true, "is_port": true, "is_record": true,
	"is_reference": true, "is_tuple": true,
	// module/record/export attribute keywords.
	"module": true, "export": true, "import": true, "record": true,
	"define": true, "include": true, "include_lib": true, "behaviour": true,
	"behavior": true, "vsn": true, "compile": true, "on_load": true,
	"spec": true, "type": true, "opaque": true, "callback": true,
	"optional_callbacks": true, "export_type": true,
}

// Extract processes an Erlang source file and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractErlang(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "erlang")
	extractor.TagEntitiesLanguage(out, "erlang")
	return out, nil
}

// ---------------------------------------------------------------------------
// Core extraction
// ---------------------------------------------------------------------------

// funcInfo collects data about a single logical function (grouped by name across all clauses).
type funcInfo struct {
	name      string
	exported  bool
	startLine int
	endLine   int
	calls     []string // raw call targets extracted from all clauses
}

// clauseMatch holds the parsed data for a single function clause head match.
type clauseMatch struct {
	name      string
	line      int
	matchEnd  int // byte offset after the '->'
	matchByte int // byte offset of the match start
}

func extractErlang(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// ── 0. OTP behaviours ──────────────────────────────────────────────────
	// -behaviour(gen_server). attributes declare the module as an OTP process.
	// Collect them so the module entity (and its callbacks) can be stamped.
	var behaviours []string
	behaviourSet := make(map[string]bool)
	for _, m := range behaviourRE.FindAllStringSubmatch(src, -1) {
		b := m[1]
		if behaviourSet[b] {
			continue
		}
		behaviourSet[b] = true
		behaviours = append(behaviours, b)
	}
	sort.Strings(behaviours)

	// callbackOf maps a function name to the behaviour(s) it is a callback of
	// for the behaviours this module actually implements.
	callbackOf := make(map[string][]string)
	for _, b := range behaviours {
		for cb := range otpCallbacks[b] {
			callbackOf[cb] = append(callbackOf[cb], b)
		}
	}
	for cb := range callbackOf {
		sort.Strings(callbackOf[cb])
	}

	// ── 1. Module declaration ──────────────────────────────────────────────
	moduleName := ""
	moduleIdx := -1
	if m := moduleRE.FindStringSubmatchIndex(src); m != nil {
		moduleName = src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		moduleIdx = len(entities)
		modRec := types.EntityRecord{
			Name:               moduleName,
			Kind:               "SCOPE.Component",
			Subtype:            "module",
			SourceFile:         filePath,
			Language:           "erlang",
			StartLine:          startLine,
			EndLine:            strings.Count(src, "\n") + 1,
			Signature:          "-module(" + moduleName + ").",
			EnrichmentRequired: false,
		}
		if len(behaviours) > 0 {
			// Stamp the OTP behaviour(s) so supervision-tree / dispatch
			// analysis can identify gen_server/supervisor/application modules
			// without re-parsing. The module subtype is refined to the most
			// specific OTP role (preferring a single behaviour) and the full
			// list is preserved in Properties + Tags.
			modRec.Subtype = otpModuleSubtype(behaviours)
			if modRec.Properties == nil {
				modRec.Properties = map[string]string{}
			}
			modRec.Properties["otp_behaviour"] = strings.Join(behaviours, ",")
			modRec.Tags = append(modRec.Tags, "otp")
			for _, b := range behaviours {
				modRec.Tags = append(modRec.Tags, "otp:"+b)
			}
		}
		entities = append(entities, modRec)
	}

	// ── 2. Record declarations ─────────────────────────────────────────────
	for _, m := range recordRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Component",
			Subtype:            "record",
			SourceFile:         filePath,
			Language:           "erlang",
			StartLine:          startLine,
			EndLine:            startLine,
			Signature:          "-record(" + name + ", ...).",
			EnrichmentRequired: false,
		})
	}

	// ── 3. Include / imports ────────────────────────────────────────────────
	for _, m := range includeRE.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		if path == "" {
			continue
		}
		leaf := path
		if slash := strings.LastIndexByte(path, '/'); slash >= 0 {
			leaf = path[slash+1:]
		}
		entities = append(entities, types.EntityRecord{
			Name:       leaf,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   "erlang",
			Relationships: []types.RelationshipRecord{
				{
					FromID: filePath,
					ToID:   path,
					Kind:   "IMPORTS",
					Properties: map[string]string{
						"local_name":    leaf,
						"source_module": path,
						"imported_name": leaf,
						"import_kind":   "include",
					},
				},
			},
		})
	}

	// ── 4. Parse exported function names ──────────────────────────────────
	exported := make(map[string]bool)
	for _, m := range exportRE.FindAllStringSubmatch(src, -1) {
		list := m[1]
		for _, em := range exportItemRE.FindAllStringSubmatch(list, -1) {
			exported[em[1]] = true
		}
	}

	// ── 5. Function clauses — group by name ───────────────────────────────
	// We collect all clause matches, then group by function name.
	var clauses []clauseMatch
	for _, m := range funcHeadRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if erlangKeywords[name] {
			continue
		}
		// Skip attribute lines that accidentally look like function heads.
		// Erlang attributes start with '-' on the same line — check if this
		// head is inside an attribute by looking at the previous non-space line.
		matchStart := m[0]
		if isInsideAttribute(src, matchStart) {
			continue
		}
		line := strings.Count(src[:m[0]], "\n") + 1
		clauses = append(clauses, clauseMatch{
			name:      name,
			line:      line,
			matchEnd:  m[1],
			matchByte: m[0],
		})
	}

	// Group consecutive clauses by name.
	funcs := groupClauses(src, clauses)

	// ── 6. Emit function entities ──────────────────────────────────────────
	for _, fi := range funcs {
		subtype := "function"
		if exported[fi.name] {
			subtype = "exported_function"
		}

		// Collect CALLS.
		callRels := collectCallsFromText(fi.calls, fi.name)

		rec := types.EntityRecord{
			Name:               fi.name,
			Kind:               "SCOPE.Operation",
			Subtype:            subtype,
			SourceFile:         filePath,
			Language:           "erlang",
			StartLine:          fi.startLine,
			EndLine:            fi.endLine,
			Signature:          fi.name + "/...",
			EnrichmentRequired: false,
			Relationships:      callRels,
		}

		// Tag OTP callback functions (handle_call/2-3, init/1, ...) so message
		// dispatch (call/cast/info) and supervision callbacks are discoverable.
		if bs := callbackOf[fi.name]; len(bs) > 0 {
			rec.Subtype = "otp_callback"
			if rec.Properties == nil {
				rec.Properties = map[string]string{}
			}
			rec.Properties["otp_callback_of"] = strings.Join(bs, ",")
			rec.Tags = append(rec.Tags, "otp_callback")
		}
		opIdx := len(entities)
		entities = append(entities, rec)

		// Attach CONTAINS from the module entity.
		if moduleIdx >= 0 && exported[fi.name] {
			toID := extractor.BuildOperationStructuralRef("erlang", filePath, fi.name)
			entities[moduleIdx].Relationships = append(entities[moduleIdx].Relationships,
				types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
		}
		_ = opIdx
	}

	return entities
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// otpModuleSubtype refines a module's subtype based on the OTP behaviour(s)
// it implements. A module with a single behaviour gets the canonical role
// subtype (e.g. gen_server → "gen_server_module"); a module implementing
// multiple behaviours is tagged generically as "otp_module" (the precise
// list is preserved in Properties["otp_behaviour"]).
func otpModuleSubtype(behaviours []string) string {
	if len(behaviours) == 1 {
		switch behaviours[0] {
		case "gen_server", "gen_statem", "gen_event", "gen_fsm":
			return behaviours[0] + "_module"
		case "supervisor":
			return "supervisor_module"
		case "application":
			return "application_module"
		}
	}
	return "otp_module"
}

// isInsideAttribute returns true when the match at matchStart is preceded
// (on the same logical source context) by a '-' attribute marker. Since
// Erlang attributes can't span multiple logical lines, we check if the
// line at matchStart starts with '-'.
func isInsideAttribute(src string, matchStart int) bool {
	// Find start of the line.
	lineStart := strings.LastIndex(src[:matchStart], "\n")
	lineStart++ // 0 if no newline found
	line := src[lineStart:matchStart]
	return strings.HasPrefix(strings.TrimSpace(line), "-")
}

// groupClauses groups clause matches by function name, computing start/end lines
// and accumulating call text from each clause body.
func groupClauses(src string, clauses []clauseMatch) []funcInfo {
	if len(clauses) == 0 {
		return nil
	}

	// Build a map from name to the accumulated info.
	// We need to preserve order of first occurrence.
	type accumulator struct {
		fi       funcInfo
		firstIdx int
	}
	order := make([]string, 0)
	accMap := make(map[string]*accumulator)

	for i, c := range clauses {
		// Extract body: from clause end up to next clause's start (or EOF).
		bodyEnd := len(src)
		if i+1 < len(clauses) {
			bodyEnd = clauses[i+1].matchByte
		}
		body := src[c.matchEnd:bodyEnd]
		endLine := c.line + strings.Count(body, "\n")

		if acc, exists := accMap[c.name]; exists {
			acc.fi.endLine = endLine
			acc.fi.calls = append(acc.fi.calls, body)
		} else {
			accMap[c.name] = &accumulator{
				fi: funcInfo{
					name:      c.name,
					startLine: c.line,
					endLine:   endLine,
					calls:     []string{body},
				},
				firstIdx: i,
			}
			order = append(order, c.name)
		}
	}

	// Return in order of first appearance.
	result := make([]funcInfo, 0, len(order))
	for _, name := range order {
		result = append(result, accMap[name].fi)
	}
	return result
}

// collectCallsFromText scans body texts and returns one CALLS edge per unique callee.
// Erlang calls can be:
//   - Qualified: module:function(...)  → ToID = "module:function"
//   - Bare: function(...)              → ToID = "function"
func collectCallsFromText(bodies []string, callerName string) []types.RelationshipRecord {
	seen := make(map[string]bool)
	var rels []types.RelationshipRecord

	addCall := func(target string, lineNum int) {
		if target == "" || target == callerName || seen[target] {
			return
		}
		seen[target] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
			Properties: map[string]string{
				"line": strconv.Itoa(lineNum),
			},
		})
	}

	for _, body := range bodies {
		scrubbed := stripCommentsAndStrings(body)

		// Qualified calls Module:Function( — emit "module:function" form.
		for _, m := range callQualifiedRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 6 {
				continue
			}
			mod := scrubbed[m[2]:m[3]]
			fn := scrubbed[m[4]:m[5]]
			if erlangKeywords[fn] {
				continue
			}
			lineNum := 1 + strings.Count(scrubbed[:m[0]], "\n")
			addCall(mod+":"+fn, lineNum)
		}

		// Bare calls name( — only lowercase-starting names.
		for _, m := range callBareRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 || m[2] < 0 || m[3] < 0 {
				continue
			}
			fn := scrubbed[m[2]:m[3]]
			if erlangKeywords[fn] {
				continue
			}
			lineNum := 1 + strings.Count(scrubbed[:m[0]], "\n")
			addCall(fn, lineNum)
		}
	}

	// Sort for determinism.
	sort.Slice(rels, func(i, j int) bool { return rels[i].ToID < rels[j].ToID })
	return rels
}

// stripCommentsAndStrings replaces Erlang %-line-comments and string/atom
// literals with spaces so the call scanner doesn't pick up tokens inside them.
func stripCommentsAndStrings(src string) string {
	out := make([]byte, len(src))
	i := 0
	for i < len(src) {
		ch := src[i]
		switch {
		case ch == '%':
			// Erlang comment: % to end of line.
			for i < len(src) && src[i] != '\n' {
				out[i] = ' '
				i++
			}
		case ch == '"':
			// Double-quoted string — scan to closing ".
			out[i] = ' '
			i++
			for i < len(src) && src[i] != '"' {
				if src[i] == '\\' && i+1 < len(src) {
					out[i] = ' '
					out[i+1] = ' '
					i += 2
					continue
				}
				out[i] = ' '
				i++
			}
			if i < len(src) {
				out[i] = ' ' // closing "
				i++
			}
		case ch == '\'':
			// Single-quoted atom — scan to closing '.
			out[i] = ' '
			i++
			for i < len(src) && src[i] != '\'' {
				if src[i] == '\\' && i+1 < len(src) {
					out[i] = ' '
					out[i+1] = ' '
					i += 2
					continue
				}
				out[i] = ' '
				i++
			}
			if i < len(src) {
				out[i] = ' ' // closing '
				i++
			}
		default:
			out[i] = ch
			i++
		}
	}
	return string(out)
}
