package javascript

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #5497 (stack epic #5479): Zod custom-validator / transform chain
// extraction. On a `z.object({...})` (or any zod schema) the author may chain
// runtime refinements and transforms that the field-constraint folder
// (parseChainConstraints) does not model:
//
//   - `.refine(fn, msg?)`      → a refinement entity (subtype zod_refinement)
//   - `.superRefine(fn)`       → a refinement entity (refinement_kind=superRefine)
//   - `.transform(fn)`         → a transform entity   (subtype zod_transform)
//   - `.pipe(otherSchema)`     → a pipe attribute + REFERENCES edge to the target
//
// Each refinement/transform is emitted as a SCOPE.Schema child entity linked to
// its host schema via a CONTAINS(member=refinement|transform) edge, mirroring
// how nested sub-schemas (#5496) and flat field members (#4606) attach. Multiple
// chained refinements/transforms produce multiple entities, order-preserved via
// a chain_index property. `.pipe(Other)` records both a `pipe_target` attribute
// on the host schema and a REFERENCES edge to the piped schema.
//
// Honest-partial: the inline arrow/function body of a refine/transform is NOT
// deep-analyzed — only the node and (for .refine) its literal/object message are
// captured. A refinement whose message is dynamic (a function, template literal
// with interpolation, or an identifier) yields the entity with no `message`.

// zodRefineCallRe matches a `.refine(` / `.superRefine(` / `.transform(` /
// `.pipe(` chain link. Group 1 = the method name. The match anchors the start of
// the call's argument list; the balanced argument text is recovered separately so
// nested parens/braces/arrows in the callback don't truncate it.
var zodRefineCallRe = regexp.MustCompile(`\.\s*(refine|superRefine|transform|pipe)\s*\(`)

// zodRefineMessageRe pulls a literal string message out of a `.refine(fn, msg)`
// argument list when the second argument is a quoted string literal, or an
// object literal carrying a `message:` string property (the `{ message: "..." }`
// form). It is applied to the text *after* the first (callback) argument.
var (
	zodRefineStringMsgRe = regexp.MustCompile(`^\s*,\s*(['"` + "`" + `])((?:\\.|[^\\])*?)['"` + "`" + `]`)
	zodRefineObjMsgRe    = regexp.MustCompile(`message\s*:\s*(['"` + "`" + `])((?:\\.|[^\\])*?)['"` + "`" + `]`)
)

// schemaTailChain returns the method-chain text that follows a `z.object(...)`
// (or other zod factory) call — i.e. everything after the balanced close-paren of
// the factory call up to the end of the statement (the next top-level `;` or
// newline at brace/paren depth 0, or EOF). objOpenParenIdx is the index of the
// `(` that opens the factory call whose body we already consumed. The returned
// text begins at the factory's close-paren so a leading `.refine(...)` link is
// included.
func schemaTailChain(src string, objOpenParenIdx int) string {
	if objOpenParenIdx < 0 || objOpenParenIdx >= len(src) || src[objOpenParenIdx] != '(' {
		return ""
	}
	// Find the matching close-paren of the factory call.
	depth := 0
	closeIdx := -1
	for i := objOpenParenIdx; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				closeIdx = i
				break
			}
		}
		if closeIdx >= 0 {
			break
		}
	}
	if closeIdx < 0 {
		return ""
	}
	// Walk from the factory close-paren to the end of the statement. A `;` or a
	// newline at depth 0 (outside any (), {}, []) terminates the chain. We keep
	// reading across newlines while depth>0 so a multi-line chain stays intact.
	depth = 0
	end := len(src)
	for i := closeIdx + 1; i < len(src); i++ {
		switch src[i] {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
			if depth < 0 {
				// We've exited the enclosing scope (e.g. the schema is an inline
				// argument) — stop here.
				end = i
				i = len(src)
				continue
			}
		case ';':
			if depth <= 0 {
				end = i
				i = len(src)
				continue
			}
		case '\n':
			if depth <= 0 {
				// A newline at depth 0 ends the chain only when the next
				// non-space character is not a `.` continuation.
				j := i + 1
				for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\r' || src[j] == '\n') {
					j++
				}
				if j >= len(src) || src[j] != '.' {
					end = i
					i = len(src)
					continue
				}
			}
		}
	}
	return src[closeIdx+1 : end]
}

// zodChainLink is a single refine/superRefine/transform/pipe link recovered from
// a schema's tail chain, with its method, raw argument text, and source order.
type zodChainLink struct {
	method string // refine | superRefine | transform | pipe
	args   string // balanced argument text inside the call's parentheses
	index  int    // 1-based source order among captured links
}

// parseZodSchemaChain walks a schema's tail chain and returns the
// refine/superRefine/transform/pipe links in source order. Field-level chain
// methods (.min/.max/.email/.optional/...) are not returned — only the
// schema-level custom-validator links this issue targets.
func parseZodSchemaChain(chain string) []zodChainLink {
	var links []zodChainLink
	idx := 0
	for _, m := range zodRefineCallRe.FindAllStringSubmatchIndex(chain, -1) {
		method := chain[m[2]:m[3]]
		// m[1] is just past the `(` of the call.
		args := balancedObjectBody(chain, m[1]-1)
		idx++
		links = append(links, zodChainLink{method: method, args: args, index: idx})
	}
	return links
}

// refineMessage extracts a literal/object message from a `.refine(...)` /
// `.superRefine(...)` argument list. Zod's signature is
// `.refine(check, message?)` where message is a string, an object
// `{ message, path }`, or (ignored here) a function. Returns "" when no literal
// message is statically recoverable (honest-partial).
func refineMessage(args string) string {
	// Skip the first (callback) argument so a quoted string *inside* the arrow
	// body isn't misread as the message. Find the top-level comma that separates
	// the callback from the message argument.
	rest := afterFirstTopLevelArg(args)
	if rest == "" {
		return ""
	}
	if m := zodRefineStringMsgRe.FindStringSubmatch(rest); m != nil {
		return m[2]
	}
	if m := zodRefineObjMsgRe.FindStringSubmatch(rest); m != nil {
		return m[2]
	}
	return ""
}

// afterFirstTopLevelArg returns the argument text starting at (and including)
// the top-level comma that follows the first argument, or "" when there is only
// one argument. Depth tracking skips commas nested in the callback's
// parens/braces/brackets and ignores commas inside string literals.
func afterFirstTopLevelArg(args string) string {
	depth := 0
	var quote byte
	for i := 0; i < len(args); i++ {
		c := args[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case ',':
			if depth == 0 {
				return args[i:]
			}
		}
	}
	return ""
}

// pipeTarget extracts the piped schema identifier from a `.pipe(arg)` argument
// list. `.pipe(z.number())` yields "" (an inline factory, no named target),
// `.pipe(OtherSchema)` yields "OtherSchema". Returns the leading identifier when
// the argument is a bare schema reference; for an inline `z.<factory>(...)` it
// returns the factory expression token (e.g. "z.number") so the attribute still
// records what the value is piped into.
func pipeTarget(args string) string {
	t := strings.TrimSpace(args)
	if t == "" {
		return ""
	}
	// Bare identifier (named schema): up to the first non-identifier char.
	end := 0
	for end < len(t) {
		c := t[end]
		if c == '_' || c == '$' || c == '.' || (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			end++
			continue
		}
		break
	}
	return t[:end]
}

// emitZodSchemaChainEntities records the refine/superRefine/transform/pipe links
// chained on a host schema as child SCOPE.Schema entities and edges, appending
// the parent-side relationships to the owner in place. Each refinement/transform
// is a child entity (CONTAINS member edge); `.pipe(target)` records a
// `pipe_target` attribute on the owner plus a REFERENCES edge to the named target
// schema. Returns the child entities. zod-only.
func emitZodSchemaChainEntities(owner *types.EntityRecord, schemaName, chain, filePath, language string, line int) []types.EntityRecord {
	if owner == nil {
		return nil
	}
	links := parseZodSchemaChain(chain)
	if len(links) == 0 {
		return nil
	}
	var out []types.EntityRecord
	var pipeTargets []string
	for _, l := range links {
		switch l.method {
		case "refine", "superRefine":
			childName := fmt.Sprintf("%s.refine#%d", schemaName, l.index)
			child := makeEntity(childName, "SCOPE.Schema", "zod_refinement", filePath, language, line)
			refKind := "refine"
			if l.method == "superRefine" {
				refKind = "superRefine"
			}
			setProps(&child, "library", "zod", "pattern_type", "refinement",
				"refinement_kind", refKind, "parent_schema", schemaName,
				"chain_index", fmt.Sprintf("%d", l.index),
				"provenance", "INFERRED_FROM_ZOD_REFINE")
			if msg := refineMessage(l.args); msg != "" {
				setProps(&child, "message", msg)
			}
			owner.Relationships = append(owner.Relationships,
				zodChainMemberEdge(schemaName, child.ID, "refinement", refKind, l.index))
			out = append(out, child)
		case "transform":
			childName := fmt.Sprintf("%s.transform#%d", schemaName, l.index)
			child := makeEntity(childName, "SCOPE.Schema", "zod_transform", filePath, language, line)
			setProps(&child, "library", "zod", "pattern_type", "transform",
				"parent_schema", schemaName,
				"chain_index", fmt.Sprintf("%d", l.index),
				"provenance", "INFERRED_FROM_ZOD_TRANSFORM")
			owner.Relationships = append(owner.Relationships,
				zodChainMemberEdge(schemaName, child.ID, "transform", "transform", l.index))
			out = append(out, child)
		case "pipe":
			tgt := pipeTarget(l.args)
			if tgt == "" {
				continue
			}
			pipeTargets = append(pipeTargets, tgt)
			// A REFERENCES edge to a *named* target schema (not an inline
			// z.<factory> expression) so the pipe topology is navigable.
			if !strings.Contains(tgt, ".") {
				owner.Relationships = append(owner.Relationships,
					zodPipeEdge(owner.ID, tgt, l.index))
			}
		}
	}
	if len(pipeTargets) > 0 {
		setProps(owner, "pipe_target", strings.Join(pipeTargets, ","))
	}
	return out
}

// zodChainMemberEdge builds the parent→child CONTAINS membership edge for a
// refinement/transform link, mirroring the nested-schema membership model
// (#5496). member is "refinement" or "transform".
func zodChainMemberEdge(parentSchema, childID, member, kind string, index int) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: "Schema:" + parentSchema,
		ToID:   childID,
		Kind:   string(types.RelationshipKindContains),
		Properties: map[string]string{
			"framework":   "zod",
			"member":      member,
			"chain_kind":  kind,
			"chain_index": fmt.Sprintf("%d", index),
			"provenance":  "INFERRED_FROM_ZOD_CHAIN",
		},
	}
}

// zodPipeEdge builds a REFERENCES edge from a schema to the schema it is piped
// into via `.pipe(Other)`. ToID is the `Schema:<target>` stub the resolver binds
// to the real schema entity.
func zodPipeEdge(fromID, targetSchema string, index int) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: fromID,
		ToID:   "Schema:" + targetSchema,
		Kind:   string(types.RelationshipKindReferences),
		Properties: map[string]string{
			"framework":   "zod",
			"ref_kind":    "zod_pipe",
			"target_type": targetSchema,
			"chain_index": fmt.Sprintf("%d", index),
			"provenance":  "INFERRED_FROM_ZOD_PIPE",
		},
	}
}
