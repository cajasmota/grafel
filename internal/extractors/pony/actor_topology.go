// actor_topology.go — Pony actor-model topology enrichment (#5384, epic #5360).
//
// Pony is an actor language: an `actor` declares asynchronous message handlers
// as BEHAVIOURS (`be name(...)`), and code sends a message by calling a
// behaviour on an actor reference (`worker.run(payload)`). A behaviour call is
// the actor-model equivalent of an Erlang/Elixir `gen_server:cast` — fire-and-
// forget message delivery to another actor's mailbox. This pass surfaces that
// topology, mirroring the Erlang OTP-deepen edge-enrichment model
// (lang.erlang.runtime.otp): it adds NO new entity Kind. Instead it
//
//  1. tags `actor` Components with "actor" and each of their behaviours with
//     "pony_behaviour" (+ records the owning actor on the behaviour entity), and
//  2. recovers `receiver.behaviour(args)` message-sends from operation bodies and
//     stamps the matching CALLS edge (ToID == behaviour name) with the actor-
//     message metadata (pony_msg_send / pony_msg_receiver / pony_msg_behaviour),
//     tagging the sending operation "pony_msg_out:<behaviour>".
//
// Honest partial: the receiver expression is captured verbatim (best-effort —
// the receiver's STATIC actor TYPE is not resolved), and a send is only
// recovered when its behaviour name matches a behaviour declared in the SAME
// file (the regex extractor has no cross-file type table). Synchronous `fun`
// calls and constructor calls are intentionally excluded — only behaviours are
// asynchronous messages.
package pony

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// ponyMsgSendRE matches a behaviour message-send call site: a receiver
// expression, a dot, the behaviour name, and an opening paren. The receiver is
// any dotted/identifier chain (e.g. `worker`, `_pool`, `this.worker`,
// `env.out`). Group 1 = receiver expression, group 2 = behaviour name.
var ponyMsgSendRE = regexp.MustCompile(
	`([A-Za-z_][A-Za-z0-9_'.]*)\.([a-z_][a-zA-Z0-9_']*)\s*\(`,
)

// enrichActorTopology stamps actor-model topology metadata onto the entity set
// produced by extractPony. It is a no-op when the file declares no actors.
//
// `src` is the raw file content (used to recover message-send call sites);
// `entities` is mutated in place.
func enrichActorTopology(src string, entities []types.EntityRecord) {
	// 1. Identify actors and their behaviours from the entity set.
	actorNames := map[string]bool{}       // actor component name -> true
	behaviourOwner := map[string]string{} // behaviour name -> owning actor name
	for i := range entities {
		e := &entities[i]
		if e.Kind == "SCOPE.Component" && e.Subtype == "actor" {
			actorNames[e.Name] = true
			addTag(e, "actor")
		}
	}
	if len(actorNames) == 0 {
		return // not an actor file — nothing to enrich.
	}

	// 2. Tag behaviours and record their owning actor. A behaviour entity is
	// named "<Actor>.<behaviour>" (see extractPony); only behaviours owned by a
	// real actor are part of the message protocol.
	behaviourNames := map[string]bool{} // bare behaviour name -> true
	for i := range entities {
		e := &entities[i]
		if e.Kind != "SCOPE.Operation" || e.Subtype != "behavior" {
			continue
		}
		owner, bare := splitMember(e.Name)
		if owner == "" || !actorNames[owner] {
			continue
		}
		addTag(e, "pony_behaviour")
		if e.Properties == nil {
			e.Properties = map[string]string{}
		}
		e.Properties["actor"] = owner
		behaviourNames[bare] = true
		behaviourOwner[bare] = owner
	}
	if len(behaviourNames) == 0 {
		return
	}

	// 3. Recover message-sends. For each sending operation, scan its body slice
	// of the source for `receiver.behaviour(...)` where behaviour is a known
	// in-file behaviour name, and stamp the matching CALLS edge.
	scrubbed := stripStringsAndComments(src)
	for i := range entities {
		e := &entities[i]
		if e.Kind != "SCOPE.Operation" {
			continue
		}
		// The operation body is the source between its start and end lines.
		body := lineSlice(scrubbed, e.StartLine, e.EndLine)
		if body == "" {
			continue
		}
		// Recover behaviour sends -> receiver, keyed by behaviour name. First
		// concrete receiver wins per behaviour.
		sends := map[string]string{}
		for _, m := range ponyMsgSendRE.FindAllStringSubmatch(body, -1) {
			receiver, behaviour := m[1], m[2]
			if !behaviourNames[behaviour] {
				continue
			}
			// A receiver that is itself an actor TYPE name is a constructor /
			// static reference, not a message target; skip the self `this`/`me`
			// noise only if it adds nothing — keep verbatim otherwise.
			if _, ok := sends[behaviour]; !ok {
				sends[behaviour] = receiver
			}
		}
		if len(sends) == 0 {
			continue
		}
		// Stamp the matching CALLS edges (ToID == behaviour name) and tag the
		// sending operation.
		for j := range e.Relationships {
			rel := &e.Relationships[j]
			if rel.Kind != "CALLS" {
				continue
			}
			recv, ok := sends[rel.ToID]
			if !ok {
				continue
			}
			if rel.Properties == nil {
				rel.Properties = map[string]string{}
			}
			rel.Properties["pony_msg_send"] = "true"
			rel.Properties["pony_msg_behaviour"] = rel.ToID
			rel.Properties["pony_msg_receiver"] = recv
			if owner := behaviourOwner[rel.ToID]; owner != "" {
				rel.Properties["pony_msg_actor"] = owner
			}
			addTag(e, "pony_msg_out:"+rel.ToID)
		}
	}
}

// addTag appends tag to e.Tags if not already present.
func addTag(e *types.EntityRecord, tag string) {
	for _, t := range e.Tags {
		if t == tag {
			return
		}
	}
	e.Tags = append(e.Tags, tag)
}

// splitMember splits a "Type.member" entity name into its owner and bare member.
// Returns ("","") when there is no dot.
func splitMember(name string) (owner, member string) {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[:i], name[i+1:]
	}
	return "", ""
}

// lineSlice returns the [startLine, endLine] inclusive (1-based) slice of src.
// Returns "" for an invalid range.
func lineSlice(src string, startLine, endLine int) string {
	if startLine <= 0 || endLine < startLine {
		return ""
	}
	lines := strings.Split(src, "\n")
	if startLine > len(lines) {
		return ""
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}
