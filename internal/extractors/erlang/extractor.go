// Package erlang implements a regex-based extractor for Erlang source files.
//
// Extracted entities:
//   - module attributes (-module(foo).)           → Kind="SCOPE.Component", Subtype="module"
//   - OTP modules (-behaviour(gen_server).)        → Subtype refined to gen_server_module / supervisor_module / application_module / otp_module; Properties["otp_behaviour"], Tags ["otp", "otp:gen_server", ...]
//   - function clauses (name(Args) -> body.)       → Kind="SCOPE.Operation", Subtype="function" / "exported_function"; identity is name/arity (foo/1 and foo/2 are distinct entities; Signature="name/arity", Properties["arity"])
//   - OTP callbacks (handle_call/3, init/1, ...)   → Subtype="otp_callback", Properties["otp_callback_of"]
//   - gen_server dispatch callbacks                → Properties["otp_dispatch_tags"] + Tags ["otp_msg:<tag>", ...] (the recovered per-clause message tags)
//   - record attributes (-record(Foo, {...}).)     → Kind="SCOPE.Component", Subtype="record"
//   - include attributes (-include("foo.hrl").)   → IMPORTS relationships
//
// Relationships emitted:
//   - IMPORTS — every -include/-include_lib attribute
//   - CALLS   — Module:Function and bare Function invocations inside function bodies
//   - CONTAINS — module entity links to each exported function
//   - SUPERVISES — supervisor module → each child module named in its init/1 child spec list
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

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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

	// childSpecMapStartRE matches the `start => {Mod, Fun, Args}` MFA inside a
	// modern map-form child spec (#{id => ..., start => {Mod, fn, []}}).
	// Group 1: the child module atom (the M of the start MFA).
	childSpecMapStartRE = regexp.MustCompile(
		`start\s*=>\s*\{\s*([a-z][a-zA-Z0-9_@]*)\s*,`,
	)

	// childSpecMapIDRE captures the `id => atom` key of a map-form child spec
	// so the SUPERVISES edge can carry the child id. Group 1: the id atom.
	childSpecMapIDRE = regexp.MustCompile(
		`id\s*=>\s*([a-z][a-zA-Z0-9_@]*)`,
	)

	// childSpecTupleRE matches a legacy tuple child spec head:
	//   {Id, {M, F, A}, Restart, Shutdown, Type, Modules}
	// Group 1: the child id atom; Group 2: the child module atom (M of the MFA).
	// The id may be an atom; the inner {M, F, A} carries the module.
	childSpecTupleRE = regexp.MustCompile(
		`\{\s*([a-z][a-zA-Z0-9_@]*)\s*,\s*\{\s*([a-z][a-zA-Z0-9_@]*)\s*,`,
	)

	// typeRE matches a -type / -opaque type definition.
	//   -type my_type() :: ...      / -opaque handle() :: ...
	//   -type result(T) :: {ok, T} | {error, term()}.
	// Group 1: the attribute keyword (type|opaque); Group 2: the type name;
	// Group 3: the parenthesised type parameters "(...)".
	typeRE = regexp.MustCompile(
		`(?m)^-(type|opaque)\s+([a-z][a-zA-Z0-9_@]*)\s*(\([^)]*\))\s*::`,
	)

	// specRE matches a -spec function-signature attribute head.
	//   -spec foo(A) -> B.
	//   -spec mod:foo(A, B) -> C when A :: integer().
	// Group 1 (optional): a leading "module:" qualifier (rare, stripped);
	// Group 2: the function name; Group 3: the parenthesised argument list
	// "(...)" of the (first) signature clause, used to recover the arity so
	// the spec binds to the right name/arity SCOPE.Operation.
	specRE = regexp.MustCompile(
		`(?m)^-spec\s+(?:([a-z][a-zA-Z0-9_@]*)\s*:\s*)?([a-z][a-zA-Z0-9_@]*)\s*(\([^;]*?\))\s*->`,
	)

	// callbackRE matches a -callback behaviour-contract attribute head.
	//   -callback init(Args :: term()) -> {ok, State}.
	// Group 1: the callback function name; Group 2: the parenthesised
	// argument list "(...)" used to recover the callback's arity.
	callbackRE = regexp.MustCompile(
		`(?m)^-callback\s+([a-z][a-zA-Z0-9_@]*)\s*(\([^;]*?\))\s*->`,
	)

	// importRE matches -import(Mod, [f/1, g/2]).
	// Group 1: the source module atom; Group 2: the import-list content.
	importRE = regexp.MustCompile(
		`(?m)^-import\s*\(\s*([a-z][a-zA-Z0-9_@]*)\s*,\s*\[([^\]]*)\]\s*\)\s*\.`,
	)

	// defineSimpleRE matches a -define(NAME, Value). object macro (no args).
	// Group 1: the macro name; Group 2: the replacement text. The replacement
	// is captured up to the trailing `).` of the attribute.
	defineSimpleRE = regexp.MustCompile(
		`(?m)^-define\s*\(\s*([A-Za-z_][A-Za-z0-9_@]*)\s*,\s*(.*?)\s*\)\s*\.\s*$`,
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

// funcInfo collects data about a single logical function (grouped by name AND
// arity across all clauses). In Erlang a function's identity is name/arity:
// foo/1 and foo/2 are two distinct functions, so clauses are grouped on the
// (name, arity) pair, not name alone.
type funcInfo struct {
	name      string
	arity     int
	exported  bool
	startLine int
	endLine   int
	calls     []string // raw call targets extracted from all clauses
	argHeads  []string // per-clause argument text (the "(...)" of each clause head)
}

// clauseMatch holds the parsed data for a single function clause head match.
type clauseMatch struct {
	name      string
	arity     int    // number of arguments in this clause head (Erlang identity component)
	args      string // the parenthesised argument text of the clause head, e.g. "({get, Key}, _From, State)"
	line      int
	matchEnd  int // byte offset after the '->'
	matchByte int // byte offset of the match start
}

// countArity returns the number of top-level arguments in a clause head's
// parenthesised argument text "(...)". Nested tuples/lists/maps/binaries and
// strings are respected so that, e.g., ({get, Key}, _From, State) is arity 3,
// () is arity 0. Commas inside nested () {} [] <<>> "" ” do not split args.
func countArity(argText string) int {
	inner := strings.TrimSpace(argText)
	inner = strings.TrimPrefix(inner, "(")
	inner = strings.TrimSuffix(inner, ")")
	if strings.TrimSpace(inner) == "" {
		return 0
	}
	scrubbed := stripCommentsAndStrings(inner)
	count := 1
	depth := 0
	for i := 0; i < len(scrubbed); i++ {
		switch scrubbed[i] {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth > 0 {
				depth--
			}
		case '<':
			if i+1 < len(scrubbed) && scrubbed[i+1] == '<' {
				depth++
				i++
			}
		case '>':
			if i+1 < len(scrubbed) && scrubbed[i+1] == '>' {
				if depth > 0 {
					depth--
				}
				i++
			}
		case ',':
			if depth == 0 {
				count++
			}
		}
	}
	return count
}

func extractErlang(rawSrc, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// ── -1. Macro definitions + expansion ──────────────────────────────────
	// Object macros (-define(NAME, Val).) are recovered first, then every
	// `?NAME` use is expanded in a working copy so that macro-wrapped call
	// targets and atoms are recovered by the downstream scanners. The raw
	// source is preserved (rawSrc) for line/offset reporting of attributes that
	// are parsed verbatim, but functions/calls/specs scan the expanded copy.
	macros := parseObjectMacros(rawSrc)
	src := expandMacros(rawSrc, macros)

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

	// ── 3b. Function imports: -import(Mod, [f/1, g/2]). ─────────────────────
	// Each imported function is recorded as an IMPORTS edge (import_kind=
	// function) carrying the source module, and a lookup table maps the bare
	// imported name (and name/arity) → source module so that bare call sites
	// inside this module resolve to "mod:fn" instead of an unqualified name.
	importedFn := make(map[string]string)   // bare name → source module
	importedFnAr := make(map[string]string) // "name/arity" → source module
	for _, m := range importRE.FindAllStringSubmatchIndex(src, -1) {
		mod := src[m[2]:m[3]]
		list := src[m[4]:m[5]]
		for _, em := range exportItemRE.FindAllStringSubmatch(list, -1) {
			fn := em[1]
			ar := em[2]
			importedFn[fn] = mod
			importedFnAr[fn+"/"+ar] = mod
			entities = append(entities, types.EntityRecord{
				Name:       fn,
				Kind:       "SCOPE.Component",
				SourceFile: filePath,
				Language:   "erlang",
				Relationships: []types.RelationshipRecord{
					{
						FromID: filePath,
						ToID:   mod + ":" + fn,
						Kind:   "IMPORTS",
						Properties: map[string]string{
							"local_name":    fn,
							"source_module": mod,
							"imported_name": fn,
							"arity":         ar,
							"import_kind":   "function",
						},
					},
				},
			})
		}
	}

	// ── 4. Parse exported function name/arity pairs ───────────────────────
	// Erlang exports are Name/Arity pairs (foo/1, foo/2). Track the precise
	// pair so foo/1 can be exported while foo/2 is private; keep the bare-name
	// set as a fallback for arity-less callers/lookups.
	exported := make(map[string]bool)   // any arity of this name is exported
	exportedAr := make(map[string]bool) // "name/arity" exported
	for _, m := range exportRE.FindAllStringSubmatch(src, -1) {
		list := m[1]
		for _, em := range exportItemRE.FindAllStringSubmatch(list, -1) {
			exported[em[1]] = true
			exportedAr[em[1]+"/"+em[2]] = true
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
		args := src[m[4]:m[5]] // group 2: the "(...)" arg text
		clauses = append(clauses, clauseMatch{
			name:      name,
			arity:     countArity(args),
			args:      args,
			line:      line,
			matchEnd:  m[1],
			matchByte: m[0],
		})
	}

	// Group consecutive clauses by name.
	funcs := groupClauses(src, clauses)

	// ── 5b. -spec signatures, keyed by name/arity ──────────────────────────
	// A -spec attribute carries the type signature of a function; after #4930
	// functions are arity-keyed entities, so a spec recovered as name/arity
	// (from its argument-list arity) binds to the matching SCOPE.Operation.
	specByNameAr := make(map[string]string) // "name/arity" → full -spec text
	for _, m := range specRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[4]:m[5]]
		argText := src[m[6]:m[7]]
		// The spec attribute runs from '-spec' to the terminating '.'; capture
		// the whole signature for the Properties payload.
		full := specText(src, m[0])
		key := name + "/" + strconv.Itoa(countArity(argText))
		if _, exists := specByNameAr[key]; !exists {
			specByNameAr[key] = full
		}
	}

	// ── 5c. -type / -opaque type definitions → SCOPE.Component/type ─────────
	for _, m := range typeRE.FindAllStringSubmatchIndex(src, -1) {
		kw := src[m[2]:m[3]]   // "type" | "opaque"
		name := src[m[4]:m[5]] // type name
		params := src[m[6]:m[7]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		full := specText(src, m[0])
		endLine := startLine + strings.Count(full, "\n")
		subtype := "type"
		if kw == "opaque" {
			subtype = "opaque_type"
		}
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    subtype,
			SourceFile: filePath,
			Language:   "erlang",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "-" + kw + " " + name + params,
			Properties: map[string]string{
				"type_kind":  kw,
				"type_arity": strconv.Itoa(countArity(params)),
			},
		})
	}

	// ── 5d. -callback behaviour contracts → SCOPE.Operation/otp_callback_spec
	// A -callback attribute declares the contract a behaviour requires of its
	// implementing modules. It is emitted as a SCOPE.Operation marked as a
	// callback contract (this module DEFINES the behaviour), keyed by
	// name/arity so it does not collide with a same-named real function.
	for _, m := range callbackRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		argText := src[m[4]:m[5]]
		arity := countArity(argText)
		startLine := strings.Count(src[:m[0]], "\n") + 1
		full := specText(src, m[0])
		endLine := startLine + strings.Count(full, "\n")
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Operation",
			Subtype:    "callback_spec",
			SourceFile: filePath,
			Language:   "erlang",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  name + "/" + strconv.Itoa(arity),
			Properties: map[string]string{
				"arity":         strconv.Itoa(arity),
				"callback_spec": full,
			},
			Tags: []string{"otp_callback_contract"},
		})
	}

	// ── 6. Emit function entities ──────────────────────────────────────────
	for _, fi := range funcs {
		// Erlang identity is name/arity: a function is exported only if THIS
		// arity appears in an -export list (foo/1 exported, foo/2 private is a
		// real and common distinction). Fall back to the bare-name set only is
		// avoided — arity precision is the point of this pass.
		nameAr := fi.name + "/" + strconv.Itoa(fi.arity)
		subtype := "function"
		if exportedAr[nameAr] {
			subtype = "exported_function"
		}

		// Collect CALLS — bare calls to -import'ed functions are resolved to
		// "mod:fn" using the import tables so they bind to the source module.
		callRels := collectCallsFromText(fi.calls, fi.name, importedFn, importedFnAr)

		// #5363: ACCESSES_TABLE edges for mnesia/ets/dets table traffic in this
		// function's clause bodies (read/write/delete/… on an atom-named table).
		callRels = append(callRels, recoverTableAccessEdges(fi.calls)...)

		rec := types.EntityRecord{
			Name:               fi.name,
			Kind:               "SCOPE.Operation",
			Subtype:            subtype,
			SourceFile:         filePath,
			Language:           "erlang",
			StartLine:          fi.startLine,
			EndLine:            fi.endLine,
			Signature:          nameAr,
			EnrichmentRequired: false,
			Relationships:      callRels,
			Properties: map[string]string{
				"arity": strconv.Itoa(fi.arity),
			},
		}

		// Attach the -spec signature (arity-keyed) when one is declared for
		// this function, so the typed contract lives on the operation entity.
		if spec, ok := specByNameAr[nameAr]; ok {
			rec.Properties["spec"] = spec
			rec.Tags = append(rec.Tags, "has_spec")
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

			// ── message-tag dispatch ─────────────────────────────────────
			// For the request-dispatching callbacks (handle_call/cast/info),
			// recover the message TAG of each clause — the first pattern
			// element of the first argument, e.g. `{get, Key}` → "get",
			// bare `flush` → "flush". This makes per-message-tag handling
			// distinguishable on the callback entity (and lets a caller's
			// gen_server:call(?SERVER, {get, _}) be associated with the
			// clause that handles tag `get`).
			if isDispatchCallback(fi.name) {
				tags := dispatchTags(fi.argHeads)
				if len(tags) > 0 {
					rec.Properties["otp_dispatch_tags"] = strings.Join(tags, ",")
					for _, tg := range tags {
						rec.Tags = append(rec.Tags, "otp_msg:"+tg)
					}
				}
			}
		}
		// #5363: gen_server / gen_statem / gen_event CLIENT MESSAGE EDGES.
		// Recover client send primitives (gen_server:call/cast, gen_event:notify,
		// …) in this function's clause bodies and enrich the matching CALLS edge
		// with the message kind/server/tag so the protocol's client side is
		// traversable and pairs with the server-side otp_dispatch_tags.
		enrichOTPMessageEdges(&rec, recoverOTPMessages(fi.calls))

		opIdx := len(entities)
		entities = append(entities, rec)

		// Attach CONTAINS from the module entity. The structural-ref is keyed
		// by bare name (BuildOperationStructuralRef carries no arity), so the
		// bare-name export set is used here — a module CONTAINS each exported
		// function name (any arity of which is exported).
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

	// ── 7. Supervision-tree child_spec edges ───────────────────────────────
	// When this module is a `supervisor`, its `init/1` returns the child spec
	// list. Parse each child spec (modern map or legacy tuple form) and emit a
	// SUPERVISES edge from the supervisor module entity → each child module so
	// the supervision hierarchy is a traversable subgraph.
	if moduleIdx >= 0 && behaviourSet["supervisor"] {
		var initBody string
		for _, fi := range funcs {
			if fi.name == "init" {
				initBody = strings.Join(fi.calls, "\n")
				break
			}
		}
		if initBody != "" {
			// #5363: recover the restart strategy (SupFlags) and stamp it on the
			// supervisor module entity so the supervision tree carries its
			// restart semantics, not only its child topology.
			if sf, ok := recoverSupFlags(initBody); ok {
				stampSupFlags(&entities[moduleIdx], sf)
			}
			for _, c := range parseChildSpecs(initBody) {
				rel := types.RelationshipRecord{
					ToID: c.module,
					Kind: "SUPERVISES",
					Properties: map[string]string{
						"provenance": "otp_child_spec",
					},
				}
				if c.id != "" {
					rel.Properties["child_id"] = c.id
				}
				entities[moduleIdx].Relationships = append(
					entities[moduleIdx].Relationships, rel)
			}
		}
	}

	// ── 8. Mnesia / ETS / DETS table declarations → SCOPE.Datastore ─────────
	// mnesia:create_table(person, …) / ets:new(cache, …) primitives declare a
	// persistent/in-memory table; emit a datastore entity per (engine, table)
	// so table traffic (the ACCESSES_TABLE edges emitted per-function above)
	// converges on a node. Scans the macro-expanded source (`src`).
	entities = append(entities, buildTableEntities(recoverTableDecls(src), filePath)...)

	return entities
}

// childSpec is a single supervised child recovered from a supervisor's
// init/1 child-spec list.
type childSpec struct {
	id     string // the spec's id atom (empty if not recoverable)
	module string // the child module (the M of the start MFA)
}

// isDispatchCallback reports whether a callback name is one of the gen_server /
// gen_event request-dispatching callbacks whose first argument carries a
// message tag worth recovering.
func isDispatchCallback(name string) bool {
	switch name {
	case "handle_call", "handle_cast", "handle_info", "handle_event",
		"handle_sync_event":
		return true
	}
	return false
}

// dispatchTags recovers, in clause order and de-duplicated, the message tag of
// each dispatch-callback clause. The tag is the first pattern element of the
// first argument: `{get, Key}` → "get", a bare atom `flush` → "flush". Wildcard
// catch-all clauses (`_Request`, `_Msg`, `_`) and variable first args (which
// start uppercase or `_`) are skipped — they carry no concrete tag.
func dispatchTags(argHeads []string) []string {
	var tags []string
	seen := make(map[string]bool)
	for _, ah := range argHeads {
		tag := firstArgTag(ah)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags
}

// firstArgTag extracts the message tag from a clause's parenthesised argument
// text `(...)`. It isolates the first top-level argument, then:
//   - `{tag, ...}` tuple  → "tag" (first element, must be a lowercase atom)
//   - bare lowercase atom → the atom itself
//   - variable / wildcard → "" (no concrete tag)
func firstArgTag(argText string) string {
	inner := strings.TrimSpace(argText)
	inner = strings.TrimPrefix(inner, "(")
	inner = strings.TrimSuffix(inner, ")")
	first := splitTopLevelFirst(inner)
	first = strings.TrimSpace(first)
	if first == "" {
		return ""
	}
	if strings.HasPrefix(first, "{") {
		// Tuple pattern: take the first element.
		body := strings.TrimPrefix(first, "{")
		elem := splitTopLevelFirst(body)
		elem = strings.TrimSpace(elem)
		if isAtom(elem) {
			return elem
		}
		return ""
	}
	if isAtom(first) {
		return first
	}
	return ""
}

// splitTopLevelFirst returns the substring up to the first top-level comma,
// respecting nesting of (), {}, [], <<>>. The remainder (other arguments /
// tuple elements) is discarded.
func splitTopLevelFirst(s string) string {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				return s[:i]
			}
		}
	}
	return s
}

// isAtom reports whether s is a bare unquoted Erlang atom (lowercase start,
// alnum/_/@ tail) and not a known keyword.
func isAtom(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	if c < 'a' || c > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		ch := s[i]
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' ||
			ch >= '0' && ch <= '9' || ch == '_' || ch == '@') {
			return false
		}
	}
	return !erlangKeywords[s]
}

// parseChildSpecs scans a supervisor init/1 body for child specs and returns
// the recovered children, de-duplicated by module (keeping the first id seen).
// Both the modern map form (#{id => ..., start => {Mod, F, A}}) and the legacy
// tuple form ({Id, {M, F, A}, ...}) are recognised. Comments and string/atom
// literals are scrubbed first so MFAs inside them are not matched.
func parseChildSpecs(body string) []childSpec {
	scrubbed := stripCommentsAndStrings(body)
	var out []childSpec
	seen := make(map[string]bool)

	add := func(module, id string) {
		if module == "" || erlangKeywords[module] || seen[module] {
			return
		}
		seen[module] = true
		out = append(out, childSpec{id: id, module: module})
	}

	// Modern map form: a `#{... start => {Mod, ...}}` map. We pair each map's
	// `start => {Mod,` with the nearest preceding `id => Atom` (if any) within
	// the same map by scanning map starts.
	for _, seg := range mapSegments(scrubbed) {
		sm := childSpecMapStartRE.FindStringSubmatch(seg)
		if sm == nil {
			continue
		}
		id := ""
		if im := childSpecMapIDRE.FindStringSubmatch(seg); im != nil {
			id = im[1]
		}
		add(sm[1], id)
	}

	// Legacy tuple form: {Id, {M, F, A}, ...}.
	for _, tm := range childSpecTupleRE.FindAllStringSubmatch(scrubbed, -1) {
		add(tm[2], tm[1])
	}

	sort.Slice(out, func(i, j int) bool { return out[i].module < out[j].module })
	return out
}

// mapSegments splits a body into the text of each `#{ ... }` map literal,
// respecting nested braces, so each child-spec map is matched in isolation
// (a `start =>` is paired with the `id =>` of the same map, not a sibling).
func mapSegments(s string) []string {
	var segs []string
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && i+1 < len(s) && s[i+1] == '{' {
			depth := 0
			j := i + 1
			for ; j < len(s); j++ {
				switch s[j] {
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						break
					}
				}
				if depth == 0 {
					break
				}
			}
			if j < len(s) {
				segs = append(segs, s[i:j+1])
				i = j
			}
		}
	}
	return segs
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

// groupClauses groups clause matches by function IDENTITY (name AND arity),
// computing start/end lines and accumulating call text from each clause body.
// In Erlang foo/1 and foo/2 are distinct functions, so clauses are keyed on the
// (name, arity) pair — only consecutive clauses of the same name/arity (the
// `;`-separated clause group) collapse into one funcInfo entity.
func groupClauses(src string, clauses []clauseMatch) []funcInfo {
	if len(clauses) == 0 {
		return nil
	}

	// Build a map from "name/arity" to the accumulated info.
	// We preserve order of first occurrence of each distinct name/arity.
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

		key := c.name + "/" + strconv.Itoa(c.arity)
		if acc, exists := accMap[key]; exists {
			acc.fi.endLine = endLine
			acc.fi.calls = append(acc.fi.calls, body)
			acc.fi.argHeads = append(acc.fi.argHeads, c.args)
		} else {
			accMap[key] = &accumulator{
				fi: funcInfo{
					name:      c.name,
					arity:     c.arity,
					startLine: c.line,
					endLine:   endLine,
					calls:     []string{body},
					argHeads:  []string{c.args},
				},
				firstIdx: i,
			}
			order = append(order, key)
		}
	}

	// Return in order of first appearance.
	result := make([]funcInfo, 0, len(order))
	for _, key := range order {
		result = append(result, accMap[key].fi)
	}
	return result
}

// collectCallsFromText scans body texts and returns one CALLS edge per unique callee.
// Erlang calls can be:
//   - Qualified: module:function(...)  → ToID = "module:function"
//   - Bare: function(...)              → ToID = "function"
func collectCallsFromText(bodies []string, callerName string, importedFn, importedFnAr map[string]string) []types.RelationshipRecord {
	seen := make(map[string]bool)
	var rels []types.RelationshipRecord

	addCall := func(target string, lineNum int, props map[string]string) {
		if target == "" || target == callerName || seen[target] {
			return
		}
		seen[target] = true
		p := map[string]string{"line": strconv.Itoa(lineNum)}
		for k, v := range props {
			p[k] = v
		}
		rels = append(rels, types.RelationshipRecord{
			ToID:       target,
			Kind:       "CALLS",
			Properties: p,
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
			addCall(mod+":"+fn, lineNum, nil)
		}

		// Bare calls name( — only lowercase-starting names. A bare call whose
		// name was -import'ed resolves to the source module ("mod:fn") so it
		// binds to the imported function, not a local one.
		for _, m := range callBareRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 || m[2] < 0 || m[3] < 0 {
				continue
			}
			fn := scrubbed[m[2]:m[3]]
			if erlangKeywords[fn] {
				continue
			}
			lineNum := 1 + strings.Count(scrubbed[:m[0]], "\n")
			if mod, ok := importedFn[fn]; ok {
				addCall(mod+":"+fn, lineNum, map[string]string{
					"resolved_via":  "import",
					"imported_from": mod,
				})
				continue
			}
			addCall(fn, lineNum, nil)
		}
	}

	// Sort for determinism.
	sort.Slice(rels, func(i, j int) bool { return rels[i].ToID < rels[j].ToID })
	return rels
}

// ---------------------------------------------------------------------------
// Macro support (-define / ?NAME expansion)
// ---------------------------------------------------------------------------

// parseObjectMacros recovers object-form macros (-define(NAME, Value).) from
// the source. Only argument-less object macros are tracked — function-form
// macros (-define(NAME(A), ...)) are intentionally skipped because their
// parameterised expansion is out of scope for the regex extractor; their
// `?NAME(...)` uses are left intact (and still scanned as ordinary calls).
// ?MODULE is seeded to the declared module name so `?MODULE`/`?SERVER` chains
// resolve.
func parseObjectMacros(src string) map[string]string {
	macros := make(map[string]string)
	if m := moduleRE.FindStringSubmatch(src); m != nil {
		macros["MODULE"] = m[1]
	}
	for _, m := range defineSimpleRE.FindAllStringSubmatch(src, -1) {
		name := m[1]
		val := strings.TrimSpace(m[2])
		// A `?OTHER` reference in the value is resolved transitively below.
		macros[name] = val
	}
	// Resolve macros whose value is itself a single macro reference (e.g.
	// -define(SERVER, ?MODULE).), up to a small fixed depth to avoid cycles.
	for i := 0; i < 5; i++ {
		changed := false
		for name, val := range macros {
			if strings.HasPrefix(val, "?") {
				ref := strings.TrimPrefix(val, "?")
				if rv, ok := macros[ref]; ok && rv != val {
					macros[name] = rv
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return macros
}

// macroUseRE matches a `?NAME` macro use (object-macro form: not immediately
// followed by '(' which would be a function-macro application).
var macroUseRE = regexp.MustCompile(`\?([A-Za-z_][A-Za-z0-9_@]*)`)

// expandMacros replaces every object-macro use `?NAME` whose NAME is a known
// object macro with its recovered replacement text, so macro-wrapped call
// targets/atoms (e.g. gen_server:call(?SERVER, ...)) are recovered by the
// downstream scanners. Unknown macros and function-macro applications
// (`?NAME(...)`) are left untouched. Line count is preserved (replacements are
// single-line values) so reported line numbers stay accurate.
func expandMacros(src string, macros map[string]string) string {
	if len(macros) == 0 {
		return src
	}
	return macroUseRE.ReplaceAllStringFunc(src, func(use string) string {
		name := strings.TrimPrefix(use, "?")
		if v, ok := macros[name]; ok && !strings.ContainsAny(v, "\n") {
			return v
		}
		return use
	})
}

// specText returns the text of an attribute starting at byte offset start,
// running to the terminating top-level '.' (period followed by whitespace/EOL
// or EOF), with interior newlines collapsed to single spaces. Strings, quoted
// atoms and comments are respected so a '.' inside them does not terminate.
func specText(src string, start int) string {
	depth := 0
	end := len(src)
	i := start
	for i < len(src) {
		ch := src[i]
		switch ch {
		case '%':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		case '"', '\'':
			q := ch
			i++
			for i < len(src) && src[i] != q {
				if src[i] == '\\' && i+1 < len(src) {
					i += 2
					continue
				}
				i++
			}
			i++
			continue
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth > 0 {
				depth--
			}
		case '.':
			if depth == 0 {
				// Erlang clause/attribute terminator: '.' at end-of-token.
				if i+1 >= len(src) || src[i+1] == '\n' || src[i+1] == ' ' ||
					src[i+1] == '\t' || src[i+1] == '\r' {
					end = i + 1
					i = len(src)
					continue
				}
			}
		}
		i++
	}
	raw := src[start:end]
	// Collapse interior whitespace runs (incl. newlines) to single spaces.
	return strings.Join(strings.Fields(raw), " ")
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
