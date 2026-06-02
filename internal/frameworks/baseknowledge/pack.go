package baseknowledge

import (
	"sort"
	"strings"
	"sync"
)

// Pack is the extension interface every framework knowledge pack
// implements. A pack is a curated, read-only bundle of BaseClassContracts
// for one framework (DRF, Spring Data, NestJS Crud, ActiveRecord,
// Eloquent, ...). New framework packs (ticket T9 #3841) slot in by
// implementing this interface and registering through Register — no change
// to the registry or to downstream consumers is required.
type Pack interface {
	// Framework is the pack's framework key ("drf", "spring-data", ...).
	// Must be stable and unique across registered packs.
	Framework() string

	// Contracts returns every base-class / mixin contract the pack knows.
	// The slice must be stable across calls (packs are curated data).
	Contracts() []BaseClassContract
}

// Registry is an indexed, query-friendly view over one or more Packs. It
// builds FQN and leaf-name indexes so lookups by either the dotted import
// path or the bare class name resolve in O(1). A Registry is immutable
// once built; build it once (e.g. via Default) and share it.
type Registry struct {
	packs []Pack

	byFQN  map[string]BaseClassContract // exact FQN -> contract
	byLeaf map[string]BaseClassContract // bare leaf name -> contract (last wins on collision; see note)
}

// NewRegistry builds a Registry from the given packs. Later packs override
// earlier packs on FQN collision (so a more specific framework pack can
// shadow a generic one). Leaf-index collisions keep the first registration
// to stay deterministic and to avoid a generic framework silently winning
// a bare-name lookup.
func NewRegistry(packs ...Pack) *Registry {
	r := &Registry{
		packs:  append([]Pack(nil), packs...),
		byFQN:  map[string]BaseClassContract{},
		byLeaf: map[string]BaseClassContract{},
	}
	for _, p := range r.packs {
		for _, c := range p.Contracts() {
			for _, fqn := range c.FQNs {
				r.byFQN[fqn] = c
				lf := leaf(fqn)
				if _, exists := r.byLeaf[lf]; !exists {
					r.byLeaf[lf] = c
				}
			}
		}
	}
	return r
}

// Lookup resolves a base class to its contract. It matches on the exact
// dotted FQN first, then falls back to the bare leaf name (so
// `class X(ModelViewSet)` resolves even without the import path). The
// boolean reports whether a contract was found.
func (r *Registry) Lookup(name string) (BaseClassContract, bool) {
	if name == "" {
		return BaseClassContract{}, false
	}
	if c, ok := r.byFQN[name]; ok {
		return c, true
	}
	if c, ok := r.byLeaf[leaf(name)]; ok {
		return c, true
	}
	return BaseClassContract{}, false
}

// MembersOf returns the inherited members a base class contributes, keyed
// by member name. Returns nil when the base class is unknown.
func (r *Registry) MembersOf(baseName string) map[string]Member {
	c, ok := r.Lookup(baseName)
	if !ok {
		return nil
	}
	return c.Members
}

// Member resolves a single (base-class, member-name) pair to its contract.
// This is the lookup-by-(framework,base-class,verb) entry point the
// effective-contract synthesizer (#3835) uses to fetch the default status
// and behaviour for one inherited verb. The boolean reports a match.
func (r *Registry) Member(baseName, memberName string) (Member, bool) {
	c, ok := r.Lookup(baseName)
	if !ok {
		return Member{}, false
	}
	m, ok := c.Members[memberName]
	return m, ok
}

// MemberNames returns the union of inherited member names contributed by
// the given base classes, deduplicated and sorted. Unknown bases
// contribute nothing. This reproduces the old cbvBaseInheritedMethods
// union semantics from a single source of truth.
func (r *Registry) MemberNames(baseNames ...string) []string {
	seen := map[string]struct{}{}
	for _, b := range baseNames {
		for name := range r.MembersOf(b) {
			seen[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// KnownBases returns, from the given candidate base names, those the
// registry recognises, preserving input order and de-duplicating. Mirrors
// the old `cbv_bases` annotation: the recognised subset of a class's bases.
func (r *Registry) KnownBases(candidates ...string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, c := range candidates {
		if _, ok := r.Lookup(c); !ok {
			continue
		}
		key := leaf(c)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

// Packs returns the packs the registry was built from, in registration
// order.
func (r *Registry) Packs() []Pack { return append([]Pack(nil), r.packs...) }

// --- default registry ---------------------------------------------------

var (
	defaultOnce     sync.Once
	defaultRegistry *Registry
	registeredPacks []Pack
)

// Register adds a pack to the set the Default registry is built from. Call
// it from a pack's init() (see drf.go). Registration is process-global and
// must happen before the first Default() call; once Default is built,
// further Register calls have no effect on it.
func Register(p Pack) {
	registeredPacks = append(registeredPacks, p)
}

// Default returns the process-wide Registry built from every pack
// registered via Register (DRF and any future framework packs). It is
// built lazily and once.
func Default() *Registry {
	defaultOnce.Do(func() {
		defaultRegistry = NewRegistry(registeredPacks...)
	})
	return defaultRegistry
}

// --- helpers ------------------------------------------------------------

func leaf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func sortStrings(s []string) { sort.Strings(s) }
