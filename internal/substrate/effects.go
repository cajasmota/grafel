// Effect-sink substrate (#2764 Phase 1A).
//
// Effect classification tags every function with the set of side-effect
// primitives it executes directly:
//
//   {db_read, db_write, http_out, fs_read, fs_write, mutation}
//
// A function with none of these is pure (low confidence — absence of
// detection does not prove absence of effect).
//
// Per the substrate split:
//
//   - Per-language sniffers identify the syntactic primitives that
//     constitute each effect (ORM calls, fetch/axios, fs.read,
//     receiver-field assignment, ...) and return EffectMatch records
//     pinned to a function declaration. They are stateless and pure.
//   - The generic propagation pass in internal/links/effect_propagation.go
//     unions effects up the CALLS graph in reverse (callers absorb
//     callees' effects with a per-hop confidence drop).
//
// Storage model: same as constant propagation — no new entity kind.
// Effects are stamped as properties on existing Function/Operation
// entities by the propagation pass; the MCP surface (archigraph_effects)
// reads them off the properties map.
package substrate

import "sort"

// Effect is one element of the effect lattice. Bottom of the lattice is
// the empty set (pure); top is the union of every primitive.
type Effect string

const (
	// EffectDBRead — direct read against a database (ORM .filter(),
	// raw cursor execute SELECT, GORM Find, Hibernate find, ...).
	EffectDBRead Effect = "db_read"
	// EffectDBWrite — direct write (save/update/delete/INSERT/UPDATE/
	// DELETE in raw SQL, .save() in ORMs, ...).
	EffectDBWrite Effect = "db_write"
	// EffectHTTPOut — outbound HTTP request (fetch, axios, requests,
	// http.Client.Do, RestTemplate, HttpClient.send, ...).
	EffectHTTPOut Effect = "http_out"
	// EffectFSRead — filesystem read (fs.readFile, open(...).read,
	// ioutil.ReadFile, Files.readAllBytes, ...).
	EffectFSRead Effect = "fs_read"
	// EffectFSWrite — filesystem write (fs.writeFile, open(..., "w"),
	// ioutil.WriteFile, Files.write, ...).
	EffectFSWrite Effect = "fs_write"
	// EffectMutation — observable state mutation (assignment to a
	// receiver field in OO, struct-field write through a pointer in
	// Go, ...). Not local-variable assignment.
	EffectMutation Effect = "mutation"
)

// AllEffects returns the canonical sorted list of effect names.
func AllEffects() []Effect {
	return []Effect{
		EffectDBRead, EffectDBWrite, EffectHTTPOut,
		EffectFSRead, EffectFSWrite, EffectMutation,
	}
}

// EffectMatch is one detected sink primitive inside a function body.
//
// The sniffer populates Function (the declaring identifier name) and
// Line (1-indexed). The propagation pass uses Function to bind the
// match back to a CALLS-graph entity by (file, name).
type EffectMatch struct {
	// Function is the declaring function/method name that owns the
	// sink. Empty when the sink occurs at module scope (uncommon —
	// the propagation pass treats these as belonging to a synthetic
	// "<module-init>" function and skips them).
	Function string
	// Line is the 1-indexed source line of the sink primitive.
	Line int
	// Effect is the lattice element this match contributes.
	Effect Effect
	// Sink is a short tag identifying which primitive matched
	// (e.g. "fetch", "axios.get", "requests.post", "open()",
	// "os.Open", "EntityManager.find"). Used by archigraph_effects
	// to explain why a function has a given effect.
	Sink string
	// Confidence is the per-match confidence in [0, 1]. Direct
	// matches against well-known APIs default to 1.0; heuristics
	// (e.g. ORM `.save()` on an unknown receiver) drop to 0.7.
	Confidence float64
}

// EffectSnifferFn is the contract for per-language effect-sink sniffers.
// Input: raw file content. Output: every detected EffectMatch in source
// order (deterministic — identical content yields identical slices, so
// graph output stays byte-identical across runs).
type EffectSnifferFn func(content string) []EffectMatch

// effectRegistry holds the registered per-language effect sniffers.
// Populated via init() in each per-language effect_sinks_*.go file.
var effectRegistry = map[string]EffectSnifferFn{}

// RegisterEffectSniffer installs a per-language effect sniffer. Mirrors
// substrate.Register for the constant-binding sniffers; both registries
// are independent so a language can ship one without the other.
func RegisterEffectSniffer(lang string, fn EffectSnifferFn) {
	if lang == "" || fn == nil {
		return
	}
	effectRegistry[lang] = fn
}

// EffectSnifferFor returns the per-language effect sniffer, or nil when
// none is registered. Callers must nil-check before invocation.
func EffectSnifferFor(lang string) EffectSnifferFn {
	return effectRegistry[lang]
}

// EffectLanguages returns the slugs of every registered effect-sniffer
// language in sorted order.
func EffectLanguages() []string {
	out := make([]string, 0, len(effectRegistry))
	for k := range effectRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// EffectSet is a small set abstraction used by the propagation pass to
// union effects across CALLS edges without allocating a fresh map per
// merge. The zero value is the empty (pure) set.
type EffectSet struct {
	// bits packs the six lattice elements into one byte. Order matches
	// AllEffects() so position 0 = db_read ... position 5 = mutation.
	bits uint8
	// confidence stores the per-effect confidence (0..255 scaled by
	// 100 → 1.00 max). Index aligns with bits.
	confidence [6]uint8
	// sinks lists short sink-tag strings ("fetch", "requests.get", ...)
	// per effect; surfaced by archigraph_effects so the agent can see
	// which primitive triggered each effect. Bounded to a few entries
	// per effect — see addSink for the cap.
	sinks [6][]string
}

// effectIndex returns the bit position of e in EffectSet, or -1 when e
// is unknown.
func effectIndex(e Effect) int {
	for i, k := range AllEffects() {
		if k == e {
			return i
		}
	}
	return -1
}

// Has reports whether e is in the set.
func (s *EffectSet) Has(e Effect) bool {
	idx := effectIndex(e)
	if idx < 0 {
		return false
	}
	return s.bits&(1<<uint(idx)) != 0
}

// Add merges one (effect, confidence, sink) tuple into the set. The
// stored confidence is the MAX across merges (the most-direct evidence
// wins per effect). Sink tags are accumulated up to maxSinksPerEffect
// to keep the surface bounded.
const maxSinksPerEffect = 6

func (s *EffectSet) Add(e Effect, conf float64, sink string) {
	idx := effectIndex(e)
	if idx < 0 {
		return
	}
	s.bits |= 1 << uint(idx)
	// Clamp + scale confidence into uint8 (0..100).
	c := conf
	if c < 0 {
		c = 0
	}
	if c > 1 {
		c = 1
	}
	scaled := uint8(c*100 + 0.5)
	if scaled > s.confidence[idx] {
		s.confidence[idx] = scaled
	}
	if sink != "" {
		s.addSink(idx, sink)
	}
}

func (s *EffectSet) addSink(idx int, sink string) {
	for _, existing := range s.sinks[idx] {
		if existing == sink {
			return
		}
	}
	if len(s.sinks[idx]) >= maxSinksPerEffect {
		return
	}
	s.sinks[idx] = append(s.sinks[idx], sink)
}

// Confidence returns the stored confidence for e in [0, 1], or 0 when
// e is not in the set.
func (s *EffectSet) Confidence(e Effect) float64 {
	idx := effectIndex(e)
	if idx < 0 {
		return 0
	}
	return float64(s.confidence[idx]) / 100.0
}

// Sinks returns the recorded sink tags for e (may be nil).
func (s *EffectSet) Sinks(e Effect) []string {
	idx := effectIndex(e)
	if idx < 0 {
		return nil
	}
	return s.sinks[idx]
}

// Union merges other into s. Per-effect confidence is the max (taking
// the strongest evidence across the union). Sink tags are deduplicated
// up to maxSinksPerEffect.
func (s *EffectSet) Union(other EffectSet) {
	for i := 0; i < 6; i++ {
		if other.bits&(1<<uint(i)) == 0 {
			continue
		}
		s.bits |= 1 << uint(i)
		if other.confidence[i] > s.confidence[i] {
			s.confidence[i] = other.confidence[i]
		}
		for _, sink := range other.sinks[i] {
			s.addSink(i, sink)
		}
	}
}

// UnionScaled is like Union but multiplies each effect's confidence by
// scale before merging. Used by the propagation pass to drop confidence
// per CALLS hop (each hop multiplies by 0.95 per the issue spec, bounded
// at a floor of 0.5 so deeply transitive evidence never disappears).
func (s *EffectSet) UnionScaled(other EffectSet, scale float64) {
	for i := 0; i < 6; i++ {
		if other.bits&(1<<uint(i)) == 0 {
			continue
		}
		s.bits |= 1 << uint(i)
		scaled := uint8(float64(other.confidence[i])*scale + 0.5)
		if scaled > s.confidence[i] {
			s.confidence[i] = scaled
		}
		for _, sink := range other.sinks[i] {
			s.addSink(i, sink)
		}
	}
}

// IsEmpty reports whether the set contains no effects.
func (s *EffectSet) IsEmpty() bool { return s.bits == 0 }

// AsList returns the effects in canonical (AllEffects) order. Empty
// slice when the set is pure.
func (s *EffectSet) AsList() []Effect {
	if s.bits == 0 {
		return nil
	}
	out := make([]Effect, 0, 6)
	for i, e := range AllEffects() {
		if s.bits&(1<<uint(i)) != 0 {
			out = append(out, e)
		}
	}
	return out
}
