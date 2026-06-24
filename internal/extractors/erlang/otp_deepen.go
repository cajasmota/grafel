// OTP / BEAM deepening for the Erlang extractor (issue #5363, epic #5360).
//
// This file adds three honest capability layers on top of the core regex
// extractor in extractor.go, mirroring the well-covered Elixir/BEAM idioms:
//
//  1. gen_server / gen_statem / gen_event CLIENT MESSAGE EDGES — call sites
//     such as gen_server:call(?SERVER, {get, Key}) and gen_server:cast(Srv,
//     flush) are recovered and the CALLS edge is enriched with the message
//     kind (call|cast|info|notify) and the message TAG (get / flush), so the
//     client side of a gen_server protocol is traversable and pairs with the
//     server-side per-clause otp_dispatch_tags already emitted by extractor.go.
//
//  2. SUPERVISION RESTART STRATEGY — a supervisor's init/1 SupFlags (the
//     one_for_one / one_for_all / rest_for_one / simple_one_for_one strategy,
//     plus intensity/period) is recovered and stamped on the supervisor module
//     entity (Properties["otp_sup_strategy"], …) so the supervision tree carries
//     its restart semantics, not just the child topology.
//
//  3. MNESIA / ETS TABLE RECORDS — mnesia:create_table(Name, …) and
//     ets:new(Name, …) table declarations become SCOPE.Datastore entities
//     (engine="mnesia"|"ets"), and table traffic (mnesia:read/write/…,
//     ets:lookup/insert/…) emits ACCESSES_TABLE edges from the accessing
//     function to the table, converging with the cross-language datastore
//     pipeline.
package erlang

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// 1. gen_server / gen_statem / gen_event client message edges
// ---------------------------------------------------------------------------

// otpSendKind maps an OTP "Mod:Fn" send primitive to its canonical message
// kind. These are the synchronous/asynchronous request primitives whose 2nd
// argument carries a concrete message worth recovering as a tag.
var otpSendKind = map[string]string{
	"gen_server:call":         "call",
	"gen_server:cast":         "cast",
	"gen_statem:call":         "call",
	"gen_statem:cast":         "cast",
	"gen_event:notify":        "notify",
	"gen_event:call":          "call",
	"gen_fsm:send_event":      "cast",
	"gen_fsm:sync_send_event": "call",
}

// otpSendRE matches an OTP client send primitive call:
//
//	gen_server:call(Server, Message)            (call/2)
//	gen_server:call(Server, Message, Timeout)   (call/3)
//	gen_server:cast(Server, Message)
//	gen_event:notify(Mgr, Event)
//
// Group 1: "Mod:Fn" (e.g. gen_server:call); Group 2: the parenthesised
// argument text "(...)" of the call (used to recover Server + Message).
var otpSendRE = regexp.MustCompile(
	`\b(gen_server:call|gen_server:cast|gen_statem:call|gen_statem:cast|gen_event:notify|gen_event:call|gen_fsm:send_event|gen_fsm:sync_send_event)\s*(\([^.]*?\))`,
)

// otpMessageEdge is one recovered client-side gen_server send.
type otpMessageEdge struct {
	target  string // the send primitive "gen_server:call" (the CALLS ToID)
	kind    string // call | cast | notify
	server  string // the server reference (1st arg), best-effort
	tag     string // the message tag (2nd arg first element), best-effort
	lineNum int
}

// recoverOTPMessages scans clause bodies for OTP client send primitives and
// returns the recovered message edges (in source order, de-duplicated by
// kind+server+tag so repeated identical sends collapse).
func recoverOTPMessages(bodies []string) []otpMessageEdge {
	var out []otpMessageEdge
	seen := make(map[string]bool)
	for _, body := range bodies {
		scrubbed := stripCommentsAndStrings(body)
		for _, m := range otpSendRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			prim := scrubbed[m[2]:m[3]]
			argText := scrubbed[m[4]:m[5]]
			kind := otpSendKind[prim]
			server := otpSendServer(argText)
			tag := otpSendTag(argText)
			lineNum := 1 + strings.Count(scrubbed[:m[0]], "\n")
			dedup := kind + "|" + server + "|" + tag
			if seen[dedup] {
				continue
			}
			seen[dedup] = true
			out = append(out, otpMessageEdge{
				target:  prim,
				kind:    kind,
				server:  server,
				tag:     tag,
				lineNum: lineNum,
			})
		}
	}
	return out
}

// otpSendServer returns the first argument (the server reference) of a send
// primitive's argument text, best-effort. The first arg is returned verbatim
// (trimmed) so {local, foo}, a ?SERVER-expanded atom, or a Pid variable are all
// surfaced; an empty string is returned when it can't be isolated.
func otpSendServer(argText string) string {
	inner := strings.TrimSpace(argText)
	inner = strings.TrimPrefix(inner, "(")
	inner = strings.TrimSuffix(inner, ")")
	first := strings.TrimSpace(splitTopLevelFirst(inner))
	return first
}

// otpSendTag returns the message tag (the SECOND argument's first element) of a
// send primitive's argument text. The 2nd arg is the Message: a {tag, …} tuple
// yields "tag", a bare lowercase atom yields itself, a variable/unknown yields
// "".
func otpSendTag(argText string) string {
	inner := strings.TrimSpace(argText)
	inner = strings.TrimPrefix(inner, "(")
	inner = strings.TrimSuffix(inner, ")")
	// Drop the first top-level arg (the server) to reach the message.
	first := splitTopLevelFirst(inner)
	rest := strings.TrimSpace(inner[len(first):])
	rest = strings.TrimPrefix(rest, ",")
	msg := strings.TrimSpace(splitTopLevelFirst(strings.TrimSpace(rest)))
	if msg == "" {
		return ""
	}
	if strings.HasPrefix(msg, "{") {
		body := strings.TrimPrefix(msg, "{")
		elem := strings.TrimSpace(splitTopLevelFirst(body))
		if isAtom(elem) {
			return elem
		}
		return ""
	}
	if isAtom(msg) {
		return msg
	}
	return ""
}

// enrichOTPMessageEdges stamps OTP-message metadata onto the CALLS edges of a
// function entity. For each recovered client send to a gen_* primitive, the
// matching CALLS edge (ToID == "gen_server:call", etc.) is annotated with
// otp_msg_kind, otp_msg_server and otp_msg_tag so the protocol direction
// (this function SENDS a `get` call to ?SERVER) is queryable. Tags
// "otp_send:<kind>" and (when a tag is known) "otp_msg_out:<tag>" are added to
// the entity. Returns true when at least one edge was enriched.
func enrichOTPMessageEdges(rec *types.EntityRecord, msgs []otpMessageEdge) bool {
	if len(msgs) == 0 {
		return false
	}
	// Index recovered messages by primitive, preferring the first concrete tag.
	byPrim := make(map[string]otpMessageEdge)
	for _, msg := range msgs {
		if ex, ok := byPrim[msg.target]; ok {
			if ex.tag == "" && msg.tag != "" {
				byPrim[msg.target] = msg
			}
			continue
		}
		byPrim[msg.target] = msg
	}
	enriched := false
	for i := range rec.Relationships {
		rel := &rec.Relationships[i]
		if rel.Kind != "CALLS" {
			continue
		}
		msg, ok := byPrim[rel.ToID]
		if !ok {
			continue
		}
		if rel.Properties == nil {
			rel.Properties = map[string]string{}
		}
		rel.Properties["otp_msg_kind"] = msg.kind
		if msg.server != "" {
			rel.Properties["otp_msg_server"] = msg.server
		}
		if msg.tag != "" {
			rel.Properties["otp_msg_tag"] = msg.tag
		}
		rel.Properties["provenance"] = "otp_client_send"
		enriched = true
	}
	if !enriched {
		return false
	}
	// Stamp summary tags on the entity (deterministic, de-duplicated).
	kindSet := make(map[string]bool)
	tagSet := make(map[string]bool)
	for _, msg := range msgs {
		kindSet["otp_send:"+msg.kind] = true
		if msg.tag != "" {
			tagSet["otp_msg_out:"+msg.tag] = true
		}
	}
	var newTags []string
	for k := range kindSet {
		newTags = append(newTags, k)
	}
	for k := range tagSet {
		newTags = append(newTags, k)
	}
	sort.Strings(newTags)
	rec.Tags = append(rec.Tags, newTags...)
	return true
}

// ---------------------------------------------------------------------------
// 2. Supervision restart strategy
// ---------------------------------------------------------------------------

// supStrategyMapRE matches a modern map-form SupFlags strategy key:
//
//	#{strategy => one_for_one, intensity => 5, period => 10}
var supStrategyMapRE = regexp.MustCompile(`strategy\s*=>\s*([a-z][a-zA-Z0-9_]*)`)
var supIntensityMapRE = regexp.MustCompile(`intensity\s*=>\s*(\d+)`)
var supPeriodMapRE = regexp.MustCompile(`period\s*=>\s*(\d+)`)

// supStrategyTupleRE matches a legacy tuple-form SupFlags:
//
//	{ok, {{one_for_one, 5, 10}, [ChildSpecs]}}
//
// Group 1: the strategy atom; Group 2: intensity; Group 3: period.
var supStrategyTupleRE = regexp.MustCompile(
	`\{\s*(one_for_one|one_for_all|rest_for_one|simple_one_for_one)\s*,\s*(\d+)\s*,\s*(\d+)\s*\}`,
)

var validSupStrategies = map[string]bool{
	"one_for_one": true, "one_for_all": true,
	"rest_for_one": true, "simple_one_for_one": true,
}

// supFlags is a recovered supervisor restart-strategy descriptor.
type supFlags struct {
	strategy  string
	intensity string
	period    string
}

// recoverSupFlags parses a supervisor init/1 body for its SupFlags (both the
// modern map form and the legacy tuple form). Returns ok=false when no strategy
// is recoverable.
func recoverSupFlags(body string) (supFlags, bool) {
	scrubbed := stripCommentsAndStrings(body)
	// Legacy tuple form is the most specific signal; check it first.
	if m := supStrategyTupleRE.FindStringSubmatch(scrubbed); m != nil {
		return supFlags{strategy: m[1], intensity: m[2], period: m[3]}, true
	}
	// Modern map form.
	if m := supStrategyMapRE.FindStringSubmatch(scrubbed); m != nil {
		if !validSupStrategies[m[1]] {
			return supFlags{}, false
		}
		sf := supFlags{strategy: m[1]}
		if im := supIntensityMapRE.FindStringSubmatch(scrubbed); im != nil {
			sf.intensity = im[1]
		}
		if pm := supPeriodMapRE.FindStringSubmatch(scrubbed); pm != nil {
			sf.period = pm[1]
		}
		return sf, true
	}
	return supFlags{}, false
}

// stampSupFlags writes the recovered restart strategy onto the supervisor
// module entity's Properties and Tags.
func stampSupFlags(rec *types.EntityRecord, sf supFlags) {
	if rec.Properties == nil {
		rec.Properties = map[string]string{}
	}
	rec.Properties["otp_sup_strategy"] = sf.strategy
	if sf.intensity != "" {
		rec.Properties["otp_sup_intensity"] = sf.intensity
	}
	if sf.period != "" {
		rec.Properties["otp_sup_period"] = sf.period
	}
	rec.Tags = append(rec.Tags, "otp_sup_strategy:"+sf.strategy)
}

// ---------------------------------------------------------------------------
// 3. Mnesia / ETS table records
// ---------------------------------------------------------------------------

// tableCreateRE matches a table-creation primitive:
//
//	mnesia:create_table(person, [{attributes, …}])
//	ets:new(my_cache, [named_table, set])
//
// Group 1: "mnesia:create_table" | "ets:new"; Group 2: the parenthesised
// argument text "(...)".
var tableCreateRE = regexp.MustCompile(
	`\b(mnesia:create_table|ets:new|dets:open_file)\s*(\([^.]*?\))`,
)

// tableAccessKind maps a table-access primitive "Mod:Fn" to a coarse DML op.
var tableAccessKind = map[string]string{
	"mnesia:read":         "read",
	"mnesia:write":        "write",
	"mnesia:delete":       "delete",
	"mnesia:match_object": "read",
	"mnesia:select":       "read",
	"mnesia:dirty_read":   "read",
	"mnesia:dirty_write":  "write",
	"ets:lookup":          "read",
	"ets:insert":          "write",
	"ets:insert_new":      "write",
	"ets:delete":          "delete",
	"ets:match":           "read",
	"ets:select":          "read",
	"dets:lookup":         "read",
	"dets:insert":         "write",
}

// tableAccessRE matches a table-traffic primitive whose first argument is the
// table name. Group 1: "Mod:Fn"; Group 2: the parenthesised argument text.
var tableAccessRE = regexp.MustCompile(
	`\b(mnesia:read|mnesia:write|mnesia:delete|mnesia:match_object|mnesia:select|mnesia:dirty_read|mnesia:dirty_write|ets:lookup|ets:insert_new|ets:insert|ets:delete|ets:match|ets:select|dets:lookup|dets:insert)\s*(\([^.]*?\))`,
)

// tableDecl is a recovered datastore table declaration.
type tableDecl struct {
	name    string
	engine  string // mnesia | ets | dets
	line    int
	primSig string // the creating primitive, e.g. "mnesia:create_table"
}

// recoverTableDecls scans the (already macro-expanded) source for table-creation
// primitives and returns the declared tables, de-duplicated by (engine, name).
func recoverTableDecls(src string) []tableDecl {
	scrubbed := stripCommentsAndStrings(src)
	var out []tableDecl
	seen := make(map[string]bool)
	for _, m := range tableCreateRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		prim := scrubbed[m[2]:m[3]]
		argText := scrubbed[m[4]:m[5]]
		name := tableNameArg(argText)
		if name == "" {
			continue
		}
		engine := strings.SplitN(prim, ":", 2)[0]
		key := engine + ":" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		line := 1 + strings.Count(scrubbed[:m[0]], "\n")
		out = append(out, tableDecl{name: name, engine: engine, line: line, primSig: prim})
	}
	return out
}

// tableNameArg returns the first argument (the table name) of a table primitive's
// argument text. mnesia/ets/dets table names are atoms (person, my_cache); a
// non-atom first arg (a Tid variable, e.g. ets:insert(Tab, …) where Tab is a
// binding) yields "".
//
// mnesia:write/1 and ets/dets:insert take a RECORD (or {Table, …} tuple) as the
// first arg rather than the table atom — `mnesia:write({person, Id, Name})`. In
// that case the table name is the tuple's first element (the record tag), so a
// `{tag, …}` first arg is unwrapped to its leading atom.
func tableNameArg(argText string) string {
	inner := strings.TrimSpace(argText)
	inner = strings.TrimPrefix(inner, "(")
	inner = strings.TrimSuffix(inner, ")")
	first := strings.TrimSpace(splitTopLevelFirst(inner))
	if isAtom(first) {
		return first
	}
	if strings.HasPrefix(first, "{") {
		body := strings.TrimPrefix(first, "{")
		elem := strings.TrimSpace(splitTopLevelFirst(body))
		if isAtom(elem) {
			return elem
		}
	}
	return ""
}

// erlangTableRef builds the stable identity string for an Erlang datastore
// table entity. The shape mirrors the cross-language datastore convention so
// ACCESSES_TABLE edges resolve to the table node.
func erlangTableRef(engine, name string) string {
	return "scope:datastore:erlang:" + engine + ":" + name
}

// buildTableEntities turns recovered table declarations into SCOPE.Datastore
// entities.
func buildTableEntities(decls []tableDecl, filePath string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, d := range decls {
		out = append(out, types.EntityRecord{
			Name:       d.name,
			Kind:       "SCOPE.Datastore",
			Subtype:    d.engine + "_table",
			SourceFile: filePath,
			Language:   "erlang",
			StartLine:  d.line,
			EndLine:    d.line,
			Signature:  d.primSig + "(" + d.name + ", ...)",
			Properties: map[string]string{
				"engine":     d.engine,
				"table":      d.name,
				"table_ref":  erlangTableRef(d.engine, d.name),
				"provenance": "erlang_table_decl",
			},
			Tags: []string{"erlang_table", "erlang_table:" + d.engine},
		})
	}
	return out
}

// recoverTableAccessEdges scans a function's clause bodies for table-traffic
// primitives and returns one ACCESSES_TABLE edge per (engine, table, op),
// de-duplicated. The edge ToID is the table-ref identity so it binds to the
// SCOPE.Datastore node (whether declared in this module or elsewhere).
func recoverTableAccessEdges(bodies []string) []types.RelationshipRecord {
	var rels []types.RelationshipRecord
	seen := make(map[string]bool)
	for _, body := range bodies {
		scrubbed := stripCommentsAndStrings(body)
		for _, m := range tableAccessRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			prim := scrubbed[m[2]:m[3]]
			argText := scrubbed[m[4]:m[5]]
			name := tableNameArg(argText)
			if name == "" {
				continue
			}
			engine := strings.SplitN(prim, ":", 2)[0]
			op := tableAccessKind[prim]
			key := engine + ":" + name + ":" + op
			if seen[key] {
				continue
			}
			seen[key] = true
			lineNum := 1 + strings.Count(scrubbed[:m[0]], "\n")
			rels = append(rels, types.RelationshipRecord{
				ToID: erlangTableRef(engine, name),
				Kind: "ACCESSES_TABLE",
				Properties: map[string]string{
					"engine":     engine,
					"table":      name,
					"op":         op,
					"primitive":  prim,
					"line":       strconv.Itoa(lineNum),
					"provenance": "erlang_table_access",
				},
			})
		}
	}
	sort.Slice(rels, func(i, j int) bool {
		if rels[i].ToID != rels[j].ToID {
			return rels[i].ToID < rels[j].ToID
		}
		return rels[i].Properties["op"] < rels[j].Properties["op"]
	})
	return rels
}
