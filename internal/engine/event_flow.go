// Event-Flow walker (#1944 Phase 1).
//
// RunEventFlow walks the pub/sub graph seeded by a Channel entity
// (SCOPE.MessageTopic or SCOPE.EventBusEvent) and emits multi-hop
// process chains describing the propagation of an event across one
// or more services.
//
// Walk pattern (linear, Phase 1):
//
//	channel  -- (reverse SUBSCRIBES_TO) -->  consumer-operation
//	consumer -- CALLS* -->                   next-channel-publisher
//	publisher -- PUBLISHES_TO -->            next-channel
//	... (repeat until terminal channel or depth cap)
//
// Out of scope for Phase 1 (tracked separately):
//   - Branching / DAG walker (#1945 Phase 2 for events).
//   - Cross-stack channel bridges via companion walker (Phase 3).
//   - Per-language pub/sub extractor coverage gaps (Phase 4).
//   - Conditional routing on saga success/failure (Phase 5).
//
// The walker mirrors RunProcessFlow's emission shape (EntityKindEventFlow
// reuses the same Properties keys: chain, chain_labels, step_count,
// entry_id, entry_name, terminal_id, branches_dag) so the existing Flows
// DAG renderer (#2028 / flows.tsx) can drive Event-Flow visualisations
// with no client-side changes.
package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// EntityKindEventFlow identifies an EventFlow entity — a linearised
// multi-hop pub/sub chain emitted by RunEventFlow. Name format:
// "<seed-channel> → <terminal>". Mirrors EntityKindProcess.
const EntityKindEventFlow = "SCOPE.EventFlow"

// RelationshipKindStepInEventFlow links an EventFlow entity to each of
// its chain steps in order. Step index lives on the edge Properties
// (`step_index`, 0-based). Mirrors STEP_IN_PROCESS.
const RelationshipKindStepInEventFlow = "STEP_IN_EVENT_FLOW"

// RelationshipKindSeedOfEventFlow links the seed Channel entity to the
// EventFlow it kicked off (Channel → EventFlow). Mirrors ENTRY_POINT_OF.
const RelationshipKindSeedOfEventFlow = "SEED_OF_EVENT_FLOW"

// Channel entity kind aliases used by the walker. These match the
// canonical kinds emitted by the per-language pub/sub extractors and the
// managed-event-bus synthesisers. Compared case-insensitively so legacy
// kind strings ("scope.messagetopic") still match.
const (
	eventFlowChannelKindMessageTopic = "SCOPE.MessageTopic"
	eventFlowChannelKindEventBus     = "SCOPE.EventBusEvent"
)

// EventFlowConfig controls the linear walker.
type EventFlowConfig struct {
	// MaxDepth caps the chain length in HOPS past the seed channel. ≤6.
	// A "hop" is one pub→channel or channel→sub transition; the default
	// 6 lets a 3-channel chain (chan → sub → call* → pub → chan → sub →
	// call* → pub → chan) materialise fully without runaway expansion.
	MaxDepth int
	// MaxIntraCallDepth caps the CALLS walk between a subscriber and the
	// next publisher it eventually reaches. ≤8 keeps the inner walk
	// tractable on dense graphs.
	MaxIntraCallDepth int
	// MaxFlows is the global cap on EventFlow entities emitted per pass.
	MaxFlows int
	// MinSteps is the minimum chain length for an EventFlow to be emitted.
	// A bare sub→pub bridge (4 steps incl. both channels) is the smallest
	// meaningful flow.
	MinSteps int
}

// DefaultEventFlowConfig returns the Phase-1 tuning per #1944.
func DefaultEventFlowConfig() EventFlowConfig {
	return EventFlowConfig{
		MaxDepth:          6,
		MaxIntraCallDepth: 8,
		MaxFlows:          200,
		MinSteps:          3,
	}
}

func clampEventFlowConfig(cfg EventFlowConfig) EventFlowConfig {
	if cfg.MaxDepth <= 0 || cfg.MaxDepth > 6 {
		cfg.MaxDepth = 6
	}
	if cfg.MaxIntraCallDepth <= 0 || cfg.MaxIntraCallDepth > 8 {
		cfg.MaxIntraCallDepth = 8
	}
	if cfg.MaxFlows <= 0 {
		cfg.MaxFlows = 200
	}
	if cfg.MinSteps < 2 {
		cfg.MinSteps = 3
	}
	return cfg
}

// eventFlowStats summarises one pass for verbose/test consumers.
type eventFlowStats struct {
	SeedChannels   int
	EventFlows     int
	StepEdges      int
	SeedEdges      int
	TruncatedDepth int
}

// isChannelEntity reports whether the entity is a recognised pub/sub
// channel. Phase 1 covers MessageTopic + EventBusEvent — the two kinds
// that drive ETL and saga chains end-to-end today. Other channel-like
// kinds (websocket / SSE / gRPC) are intentionally excluded from Phase
// 1 to keep the seed surface focused; future phases can extend this.
func isChannelEntity(e *graph.Entity) bool {
	if e == nil {
		return false
	}
	k := strings.ToLower(e.Kind)
	return k == strings.ToLower(eventFlowChannelKindMessageTopic) ||
		k == strings.ToLower(eventFlowChannelKindEventBus)
}

// channelLabel returns a stable human-readable label for a channel
// entity (e.g. "kafka:payments.settled"). Falls back to Name then ID.
func channelLabel(e *graph.Entity) string {
	if e == nil {
		return ""
	}
	if e.Name != "" {
		return e.Name
	}
	if e.QualifiedName != "" {
		return e.QualifiedName
	}
	return e.ID
}

// pubSubAdjacency captures the pub/sub edges relevant to event flows.
//   - publishers[channelID]   = caller IDs that PUBLISHES_TO this channel.
//   - subscribers[channelID]  = consumer IDs that SUBSCRIBES_TO this channel.
//   - publishesOf[callerID]   = channels that a caller publishes to.
type pubSubAdjacency struct {
	publishers  map[string][]string
	subscribers map[string][]string
	publishesOf map[string][]string
}

// buildPubSubAdjacency indexes PUBLISHES_TO and SUBSCRIBES_TO edges
// across `doc`. Self-loops and duplicate edges are deduped. Output
// slices are sorted for determinism.
func buildPubSubAdjacency(doc *graph.Document) *pubSubAdjacency {
	a := &pubSubAdjacency{
		publishers:  make(map[string][]string),
		subscribers: make(map[string][]string),
		publishesOf: make(map[string][]string),
	}
	if doc == nil {
		return a
	}
	seenPub := make(map[edgeKey]bool)
	seenSub := make(map[edgeKey]bool)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.FromID == "" || r.ToID == "" || r.FromID == r.ToID {
			continue
		}
		switch r.Kind {
		case "PUBLISHES_TO":
			k := edgeKey{r.FromID, r.ToID}
			if seenPub[k] {
				continue
			}
			seenPub[k] = true
			a.publishers[r.ToID] = append(a.publishers[r.ToID], r.FromID)
			a.publishesOf[r.FromID] = append(a.publishesOf[r.FromID], r.ToID)
		case "SUBSCRIBES_TO":
			k := edgeKey{r.FromID, r.ToID}
			if seenSub[k] {
				continue
			}
			seenSub[k] = true
			a.subscribers[r.ToID] = append(a.subscribers[r.ToID], r.FromID)
		}
	}
	for k := range a.publishers {
		sort.Strings(a.publishers[k])
	}
	for k := range a.subscribers {
		sort.Strings(a.subscribers[k])
	}
	for k := range a.publishesOf {
		sort.Strings(a.publishesOf[k])
	}
	return a
}

// RunEventFlow walks every channel in `doc` and emits one EventFlow
// entity per surviving multi-hop chain. EventFlow entities + edges are
// appended to doc in place. Safe to call on a doc with no pub/sub
// edges (returns an empty stats record).
func RunEventFlow(doc *graph.Document, cfg EventFlowConfig) eventFlowStats {
	stats := eventFlowStats{}
	if doc == nil {
		return stats
	}
	cfg = clampEventFlowConfig(cfg)

	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		byID[e.ID] = e
	}

	psAdj := buildPubSubAdjacency(doc)
	callsAdj := buildCallsAdjacency(doc)

	// Seed list: every channel entity in deterministic ID order.
	seeds := make([]string, 0)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if isChannelEntity(e) {
			seeds = append(seeds, e.ID)
		}
	}
	sort.Strings(seeds)
	stats.SeedChannels = len(seeds)
	if len(seeds) == 0 {
		return stats
	}

	type emit struct {
		seed     string
		terminal string
		chain    []string
	}
	emits := make([]emit, 0, len(seeds))

	for _, seedID := range seeds {
		chains := walkEventFlow(seedID, byID, psAdj, callsAdj, cfg, &stats)
		for _, ch := range chains {
			if len(ch) < cfg.MinSteps {
				continue
			}
			emits = append(emits, emit{
				seed:     seedID,
				terminal: ch[len(ch)-1],
				chain:    ch,
			})
		}
	}

	// Deterministic order: longest chain first, then seed ID, then
	// terminal ID. Matches the Process-Flow tie-break so determinism
	// tests can share fixtures.
	sort.Slice(emits, func(i, j int) bool {
		li := len(emits[i].chain)
		lj := len(emits[j].chain)
		if li != lj {
			return li > lj
		}
		if emits[i].seed != emits[j].seed {
			return emits[i].seed < emits[j].seed
		}
		return emits[i].terminal < emits[j].terminal
	})
	if len(emits) > cfg.MaxFlows {
		emits = emits[:cfg.MaxFlows]
	}

	for _, em := range emits {
		chain := em.chain
		seedEnt := byID[chain[0]]
		terminalEnt := byID[chain[len(chain)-1]]
		if seedEnt == nil {
			continue
		}
		terminalLabel := chain[len(chain)-1]
		if terminalEnt != nil {
			terminalLabel = channelLabel(terminalEnt)
			if terminalLabel == "" {
				terminalLabel = terminalEnt.Name
			}
		}
		seedLabel := channelLabel(seedEnt)
		label := fmt.Sprintf("%s → %s", seedLabel, terminalLabel)
		flowID := computeEventFlowID(doc.Repo, chain)

		labels := chainLabels(chain, byID)

		// Build a trivial linear ChainStep DAG so the dashboard's DAG
		// renderer can consume EventFlows with no special-case code.
		root := buildLinearChainStepDAG(chain, byID)

		channelCount := 0
		for _, id := range chain {
			if isChannelEntity(byID[id]) {
				channelCount++
			}
		}

		props := map[string]string{
			"entry_id":      seedEnt.ID,
			"entry_name":    seedLabel,
			"terminal_id":   chain[len(chain)-1],
			"step_count":    strconv.Itoa(len(chain)),
			"channel_count": strconv.Itoa(channelCount),
			"chain":         strings.Join(chain, ","),
			"chain_labels":  strings.Join(labels, " → "),
			"entry_kind":    "channel",
			// Mirror the Process-Flow DAG metadata so flows.tsx can
			// drive the renderer with the same code path.
			"dag_node_count": strconv.Itoa(len(chain)),
			"branch_count":   "0",
			"is_dag":         "false",
		}
		if dagJSON := encodeDAGJSON(root); dagJSON != "" {
			props["branches_dag"] = dagJSON
		}

		doc.Entities = append(doc.Entities, graph.Entity{
			ID:         flowID,
			Name:       label,
			Kind:       EntityKindEventFlow,
			SourceFile: seedEnt.SourceFile,
			StartLine:  seedEnt.StartLine,
			EndLine:    seedEnt.EndLine,
			Language:   seedEnt.Language,
		}.WithProperties(props))
		stats.EventFlows++

		// SEED_OF_EVENT_FLOW: channel → EventFlow.
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     graph.RelationshipID(seedEnt.ID, flowID, RelationshipKindSeedOfEventFlow),
			FromID: seedEnt.ID,
			ToID:   flowID,
			Kind:   RelationshipKindSeedOfEventFlow,
		})
		stats.SeedEdges++

		// STEP_IN_EVENT_FLOW edges in order.
		for i, stepID := range chain {
			rel := graph.Relationship{
				ID:     graph.RelationshipID(flowID, stepID, RelationshipKindStepInEventFlow+":"+strconv.Itoa(i)),
				FromID: flowID,
				ToID:   stepID,
				Kind:   RelationshipKindStepInEventFlow,
			}.WithProperties(map[string]string{
				"step_index": strconv.Itoa(i),
			},
			)
			doc.Relationships = append(doc.Relationships, rel)
			stats.StepEdges++
		}
	}

	return stats
}

// walkEventFlow runs the linear Phase-1 walk from `seedID`. Returns one
// chain per consumer-of-seed × distinct terminal pair. The walk is
// depth-first with cycle detection on channels (visiting the same
// channel twice closes a saga loop — we record the longest prefix and
// stop).
func walkEventFlow(
	seedID string,
	byID map[string]*graph.Entity,
	psAdj *pubSubAdjacency,
	callsAdj *callsAdjacency,
	cfg EventFlowConfig,
	stats *eventFlowStats,
) [][]string {
	var out [][]string

	// Subscribers of the seed channel are the entry consumers. When a
	// channel has no subscribers in this doc, no flow is emitted (Phase
	// 3 cross-stack walker will pick those up via companions).
	subs := psAdj.subscribers[seedID]
	if len(subs) == 0 {
		return out
	}

	// visitedChannels tracks channels reached on the current chain so a
	// pub/sub cycle (A → B → A) terminates after recording one full
	// pass instead of looping. Subscribers and intra-call walks are NOT
	// tracked here — they're constrained by depth caps.

	for _, subID := range subs {
		init := frame{
			chain:           []string{seedID, subID},
			visitedChannels: map[string]bool{seedID: true},
			channelHops:     0,
		}
		expandEventFlow(init, byID, psAdj, callsAdj, cfg, stats, &out)
	}
	return out
}

// expandEventFlow extends one walk frame. From the latest consumer
// node, walk CALLS (bounded by MaxIntraCallDepth) to find downstream
// PUBLISHES_TO edges. Each such publish edge extends the chain by
// [...intra-calls, publisher, next-channel]. If next-channel has its
// own subscribers, the walk recurses; otherwise the chain terminates
// at next-channel.
func expandEventFlow(
	fr frame,
	byID map[string]*graph.Entity,
	psAdj *pubSubAdjacency,
	callsAdj *callsAdjacency,
	cfg EventFlowConfig,
	stats *eventFlowStats,
	out *[][]string,
) {
	// Always record the current chain as a candidate terminal. The
	// caller dedupes longer chains over shorter prefixes via the global
	// sort + MaxFlows truncation; emitting at each terminal preserves
	// "consumer with no downstream publish" flows.
	*out = append(*out, append([]string(nil), fr.chain...))

	if fr.channelHops >= cfg.MaxDepth {
		stats.TruncatedDepth++
		return
	}

	consumerID := fr.chain[len(fr.chain)-1]

	// BFS over CALLS from the consumer to find publishers reachable
	// within MaxIntraCallDepth. Each (publisher, channel) it lands on
	// becomes a continuation frame.
	type callHit struct {
		path      []string // CALLS path from consumer (exclusive) to publisher (inclusive)
		publisher string
		channel   string
	}
	var hits []callHit

	visited := map[string]bool{consumerID: true}
	type bfsNode struct {
		id   string
		path []string
	}
	queue := []bfsNode{{id: consumerID, path: nil}}
	for len(queue) > 0 && len(hits) < 32 {
		// pop front
		cur := queue[0]
		queue = queue[1:]
		// Does cur publish anywhere?
		if chans, ok := psAdj.publishesOf[cur.id]; ok && cur.id != consumerID {
			// cur is a downstream callee that publishes — every channel
			// it publishes to that we haven't already visited becomes a
			// continuation hit. (consumerID itself rarely publishes; if
			// it does, we still want it to count.)
			for _, ch := range chans {
				if fr.visitedChannels[ch] {
					continue
				}
				hits = append(hits, callHit{
					path:      append([]string(nil), cur.path...),
					publisher: cur.id,
					channel:   ch,
				})
			}
		}
		if len(cur.path) >= cfg.MaxIntraCallDepth {
			continue
		}
		for _, nb := range callsAdj.out[cur.id] {
			if visited[nb] {
				continue
			}
			visited[nb] = true
			nextPath := append(append([]string(nil), cur.path...), nb)
			queue = append(queue, bfsNode{id: nb, path: nextPath})
		}
	}

	// The consumer itself may publish directly to the next channel
	// (zero-intra-call hop). Capture that case explicitly so a
	// minimum-shape sub→pub bridge still produces a chain.
	if chans, ok := psAdj.publishesOf[consumerID]; ok {
		for _, ch := range chans {
			if fr.visitedChannels[ch] {
				continue
			}
			// Avoid duplicating a zero-path hit we may also have caught
			// via the BFS (we won't, since visited[consumerID]=true).
			hits = append(hits, callHit{
				path:      nil,
				publisher: consumerID,
				channel:   ch,
			})
		}
	}

	// Determinism: sort hits by (channel, publisher) so the chain order
	// is reproducible across runs.
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].channel != hits[j].channel {
			return hits[i].channel < hits[j].channel
		}
		return hits[i].publisher < hits[j].publisher
	})

	for _, h := range hits {
		// Build the extended chain: existing chain + intra-call path +
		// publisher (if not already last) + channel + subscribers of
		// channel (each forks a sub-frame).
		extended := append([]string(nil), fr.chain...)
		for _, n := range h.path {
			extended = append(extended, n)
		}
		if h.publisher != extended[len(extended)-1] {
			extended = append(extended, h.publisher)
		}
		extended = append(extended, h.channel)

		nextVisited := make(map[string]bool, len(fr.visitedChannels)+1)
		for k := range fr.visitedChannels {
			nextVisited[k] = true
		}
		nextVisited[h.channel] = true

		nextSubs := psAdj.subscribers[h.channel]
		if len(nextSubs) == 0 {
			// Terminal channel — record the chain ending on it.
			*out = append(*out, append([]string(nil), extended...))
			continue
		}
		for _, nextSub := range nextSubs {
			subChain := append(append([]string(nil), extended...), nextSub)
			expandEventFlow(frame{
				chain:           subChain,
				visitedChannels: nextVisited,
				channelHops:     fr.channelHops + 1,
			}, byID, psAdj, callsAdj, cfg, stats, out)
		}
	}
}

// frame is the walker's per-recursion state. Declared at package level
// so the helper functions can reference it without nesting.
type frame struct {
	chain           []string
	visitedChannels map[string]bool
	channelHops     int
}

// buildLinearChainStepDAG materialises a linear ChainStep DAG matching
// the shape produced by buildFlowDAG. Each step is a child of the
// previous; no Branches fan-out. Used so flows.tsx can render an
// EventFlow with the same component code path as a ProcessFlow.
func buildLinearChainStepDAG(chain []string, byID map[string]*graph.Entity) *ChainStep {
	if len(chain) == 0 {
		return nil
	}
	root := newChainStep(chain[0], 0, byID)
	cur := root
	for i := 1; i < len(chain); i++ {
		child := newChainStep(chain[i], i, byID)
		cur.Branches = append(cur.Branches, child)
		cur = child
	}
	return root
}

// computeEventFlowID derives a stable EventFlow entity ID from the repo
// tag and the full chain. Collision-resistant in the same way as
// computeProcessID.
func computeEventFlowID(repo string, chain []string) string {
	h := sha256.New()
	h.Write([]byte(repo))
	h.Write([]byte{0})
	h.Write([]byte("EventFlow"))
	h.Write([]byte{0})
	for _, c := range chain {
		h.Write([]byte(c))
		h.Write([]byte{0})
	}
	return "evflow:" + hex.EncodeToString(h.Sum(nil))[:16]
}
