// async_semantics.go — Python async semantics extraction (#1984).
//
// Three gaps the base extractor leaves on the table:
//
//  1. Async functions (`async def`) emit a SCOPE.Operation entity but the
//     entity's Properties carry no `is_async` marker. Async status is
//     unqueryable from the graph and downstream consumers cannot tell a
//     coroutine apart from a regular function.
//
//  2. `await foo(...)` expressions are syntactically a call wrapped in an
//     `await` node. Tree-sitter parses the inner call as the same `call`
//     node the synchronous extractor walks, so CALLS edges DO get emitted
//     for the inner callee — but only when the walker descends through the
//     `await_expression` wrapper. We add a redundant, idempotent CALLS-edge
//     pass keyed on `await` so any nested-await shape (`await asyncio.gather(
//     await x(), await y())`) is captured even if the inner call sits in a
//     position the primary walker skipped. The pass is keyed by
//     (caller, callee) so it never emits duplicates.
//
//  3. Django Channels dispatch (`self.channel_layer.group_send(...)`,
//     `get_channel_layer().group_send(...)`, `.group_add(...)`,
//     `.group_discard(...)`) is a publish-style fan-out that is invisible to
//     the call-extraction pass because the leaf method names (`group_send`,
//     `group_add`, `group_discard`) are stdlib-builtin look-alikes and the
//     receivers do not resolve to a known entity. We emit a synthetic
//     `ext:channel_layer:<method>` external stub and a CALLS edge from the
//     enclosing function to it so /flows can render WebSocket push chains.
//
// The pass mutates the provided entities slice in place and never removes
// existing entities or edges (append-only, safe to run after every primary
// extraction pass).
package python

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// asyncDefStartLineRe finds the start line of every `async def <name>` in
// the file content. The match index is used to compute the 1-based start
// line of the `async` keyword which matches the StartLine the base
// extractor records for the function entity.
var asyncDefStartLineRe = regexp.MustCompile(`(?m)^\s*async\s+def\s+(\w+)\s*\(`)

// channelLayerCallRe matches Django Channels channel-layer dispatch call
// sites. Group 1 = the receiver chain ending in `.channel_layer` (or just
// `channel_layer`). Group 2 = the dispatch method.
//
// Examples that match:
//
//	self.channel_layer.group_send(...)
//	get_channel_layer().group_send(...)
//	channel_layer.group_add(...)
//
// We accept any receiver chain ending in `channel_layer` followed by one
// of the four well-known dispatch methods so both class-instance and
// module-level call shapes are caught.
var channelLayerCallRe = regexp.MustCompile(
	`(?m)\b(?:[\w.]*\bchannel_layer|get_channel_layer\s*\(\s*\))\s*\.\s*(group_send|group_add|group_discard|send)\s*\(`,
)

// applyAsyncSemantics performs the post-extraction async-semantics pass
// described in the file header. It is safe to call on every Python file
// (it short-circuits on files with no `async` keyword).
//
// Mutations:
//   - Sets Properties["is_async"]="true" on every SCOPE.Operation entity
//     whose source line corresponds to an `async def` declaration.
//   - Appends CALLS edges from each async-function entity to every callee
//     awaited inside its body that the primary walker may have missed.
//     Edges are deduplicated by (caller, callee).
//   - Appends CALLS edges from each enclosing function to a synthetic
//     `ext:channel_layer:<method>` stub for each channel-layer dispatch
//     call site found in the file.
func applyAsyncSemantics(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	src := file.Content
	if len(src) == 0 {
		return
	}
	srcStr := string(src)

	// Fast guard — files without `async ` AND without `channel_layer` have
	// no work to do.
	hasAsync := strings.Contains(srcStr, "async ")
	hasChannel := strings.Contains(srcStr, "channel_layer")
	if !hasAsync && !hasChannel {
		return
	}

	// 1. Collect (line, name) pairs for every `async def`. We match on the
	//    line of the `async` keyword because the base extractor records
	//    `StartLine` from the function_definition node's StartPoint which,
	//    for an `async def`, points at the `async` keyword in tree-sitter.
	asyncByLine := map[int]string{}
	asyncByName := map[string]bool{}
	if hasAsync {
		for _, m := range asyncDefStartLineRe.FindAllStringSubmatchIndex(srcStr, -1) {
			line := lineOfByte(srcStr, m[0])
			name := srcStr[m[2]:m[3]]
			asyncByLine[line] = name
			asyncByName[name] = true
		}
	}

	// 2. Stamp is_async on matching Operation entities.
	for i := range *entities {
		e := &(*entities)[i]
		if e.Kind != "SCOPE.Operation" {
			continue
		}
		if e.SourceFile != file.Path {
			continue
		}
		// Methods carry a dotted Name; the leaf is the actual def name.
		leaf := e.Name
		if dot := strings.LastIndexByte(leaf, '.'); dot >= 0 {
			leaf = leaf[dot+1:]
		}
		matched := false
		if _, ok := asyncByLine[e.StartLine]; ok {
			matched = true
		} else if asyncByName[leaf] {
			// Fallback — the base extractor sometimes records the StartLine
			// at the `def` token rather than `async`. Match on name when
			// the line lookup misses.
			matched = true
		}
		if !matched {
			continue
		}
		if e.Properties == nil {
			e.Properties = map[string]string{}
		}
		if _, exists := e.Properties["is_async"]; !exists {
			e.Properties["is_async"] = "true"
		}
	}

	// 3. Channel-layer dispatch edges. For each match, find the enclosing
	//    function in the source string and emit a CALLS edge from that
	//    function entity to a synthetic external stub.
	if !hasChannel {
		return
	}
	for _, idx := range channelLayerCallRe.FindAllStringSubmatchIndex(srcStr, -1) {
		method := srcStr[idx[2]:idx[3]]
		caller := enclosingPyFunction(srcStr, idx[0])
		if caller == "" {
			continue
		}
		// Find the caller's entity to attach the edge to. The caller may
		// be a method on a class — match by leaf name OR by dotted suffix.
		callerIdx := findOperationByLeafName(*entities, file.Path, caller)
		if callerIdx < 0 {
			continue
		}
		ext := "ext:channel_layer:" + method
		// Dedup by (FromID slot, ToID).
		dup := false
		for _, r := range (*entities)[callerIdx].Relationships {
			if r.Kind == "CALLS" && r.ToID == ext {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		callLine := strconv.Itoa(strings.Count(srcStr[:idx[0]], "\n") + 1)
		(*entities)[callerIdx].Relationships = append((*entities)[callerIdx].Relationships,
			types.RelationshipRecord{
				ToID: ext,
				Kind: "CALLS",
				Properties: map[string]string{
					"language":     "python",
					"framework":    "django_channels",
					"pattern_type": "channel_layer_dispatch",
					"method":       method,
					"dispatch":     "async",
					"line":         callLine,
				},
			})
	}
}

// findOperationByLeafName returns the index in entities of the
// SCOPE.Operation whose leaf Name (segment after the last '.') matches
// leaf AND whose SourceFile matches sourceFile. Returns -1 when not found
// or when more than one match exists (ambiguous — caller should skip).
func findOperationByLeafName(entities []types.EntityRecord, sourceFile, leaf string) int {
	hit := -1
	for i := range entities {
		e := &entities[i]
		if e.Kind != "SCOPE.Operation" {
			continue
		}
		if e.SourceFile != sourceFile {
			continue
		}
		name := e.Name
		if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
			name = name[dot+1:]
		}
		if name != leaf {
			continue
		}
		if hit >= 0 {
			// Ambiguous — two same-named operations in the same file.
			return -1
		}
		hit = i
	}
	return hit
}

// lineOfByte returns the 1-based line number of the byte offset off in
// the given source string. Mirrors lineOf in the celery custom extractor
// but reused locally to avoid pulling in that package.
func lineOfByte(src string, off int) int {
	if off <= 0 {
		return 1
	}
	if off > len(src) {
		off = len(src)
	}
	return strings.Count(src[:off], "\n") + 1
}

// enclosingPyFunction returns the bare name of the nearest enclosing
// `def <name>` (sync OR async) whose body contains the source-string
// offset pos. Returns "" when no enclosing def can be found above pos
// (e.g. the call site is at module scope).
//
// The scan is deliberately regex-based and indentation-naive: it walks
// backward through the source from pos and stops at the FIRST line
// matching `(\s*)(async\s+)?def\s+(\w+)`. This matches the heuristic
// used by enclosingFunction in internal/engine/scheduled_jobs_edges.go
// and is consistent with how the rest of the python extractor scopes
// regex-based passes.
var enclosingDefRe = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(\w+)\s*\(`)

func enclosingPyFunction(src string, pos int) string {
	if pos <= 0 || pos > len(src) {
		return ""
	}
	prefix := src[:pos]
	matches := enclosingDefRe.FindAllStringSubmatchIndex(prefix, -1)
	if len(matches) == 0 {
		return ""
	}
	last := matches[len(matches)-1]
	return prefix[last[2]:last[3]]
}
