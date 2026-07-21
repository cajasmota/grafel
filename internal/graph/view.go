package graph

// F2 of ADR-0027 (mmap + zero-copy resident graph): the read-only view seam.
//
// EntityView / RelationshipView decouple the ~3,700 consumer read sites from the
// concrete heap structs, so the migration (M-series) can move package-by-package
// while the build stays green, and so F3 can drop in an mmap-backed impl of the
// SAME interface without any consumer change. The method set is deliberately
// MINIMAL for F2: the HOT accessors the resident hot index needs first
// (ID/Kind/Name/QualifiedName, and From/To for relationships) plus the handful of
// COLD accessors a pilot call site reads (Subtype/SourceFile/Language/Signature
// and property access). Later M-PRs widen it one field at a time — it is additive.
//
// Why a wrapper and not methods on *Entity: a Go struct that has an exported
// field `Name` cannot also expose a `Name()` method (name collision), and every
// hot accessor collides with an existing exported field. Renaming the fields is
// the whole M/C-series migration (it rewrites every consumer), which F2 must NOT
// do. ADR-0027 blesses exactly this escape hatch: "each method returns the
// corresponding struct field ... after a mechanical field rename, OR a thin
// wrapper type." F2 takes the wrapper: materializedEntityView adapts the concrete
// *Entity today; F3 adds mmapEntityView. Both satisfy the interface; the resident
// hot index and every migrated consumer bind only to the interface.

// EntityView is the read-only view of a graph entity. See package doc above for
// the HOT/COLD rationale and why the concrete type is adapted via a wrapper.
type EntityView interface {
	ID() string
	Kind() string
	Name() string
	QualifiedName() string
	Subtype() string
	SourceFile() string
	Language() string
	Signature() string

	// The read-only property surface below is NAME- AND SIGNATURE-IDENTICAL to
	// graph.Entity's own methods. That identity is the whole point of W2
	// (ADR-0027): *graph.Entity satisfies these for free, so migrating a
	// property-reading consumer to EntityView is a pure TYPE change — its
	// PropGet/PropLookup/PropLen/PropRange/PropsSnapshot calls compile unchanged.
	// The WRITE methods (PropSet/PropDelete) are deliberately EXCLUDED: writes are
	// cut-set and stay on the concrete type. Storage-agnostic by contract
	// (ADR-0027 §Interaction): compatible with the map-backed and []propKV
	// backings and with values aliased straight out of the mmap.
	//
	// PropGet returns the value for key, or "" if absent.
	PropGet(key string) string
	// PropLookup returns the value for key and whether it was present.
	PropLookup(key string) (string, bool)
	// PropLen returns the number of properties.
	PropLen() int
	// PropRange calls f for every key/value pair in key-sorted order, stopping
	// early if f returns false.
	PropRange(f func(k, v string) bool)
	// PropsSnapshot returns an independent copy of all properties as a map, or nil
	// if there are none. The returned map (and its strings) are safe to retain
	// past any borrow — an mmap-backed impl heap-copies the values.
	PropsSnapshot() map[string]string
}

// RelationshipView is the read-only view of a graph relationship. Its property
// read surface is identical to graph.Relationship's, for the same reason as
// EntityView's (see there).
type RelationshipView interface {
	ID() string
	FromID() string
	ToID() string
	Kind() string

	PropGet(key string) string
	PropLookup(key string) (string, bool)
	PropLen() int
	PropRange(f func(k, v string) bool)
	PropsSnapshot() map[string]string
}

// materializedEntityView adapts a concrete *Entity (materialized on the heap) to
// EntityView. Value-typed wrapper over a pointer: zero heap cost beyond the
// interface box, and behavior-neutral — every method returns the underlying
// field verbatim.
type materializedEntityView struct{ e *Entity }

// EntityViewOf wraps a concrete *Entity as an EntityView. Returns a nil interface
// for a nil entity so callers can nil-check the result.
func EntityViewOf(e *Entity) EntityView {
	if e == nil {
		return nil
	}
	return materializedEntityView{e: e}
}

func (v materializedEntityView) ID() string            { return v.e.ID }
func (v materializedEntityView) Kind() string          { return v.e.Kind }
func (v materializedEntityView) Name() string          { return v.e.Name }
func (v materializedEntityView) QualifiedName() string { return v.e.QualifiedName }
func (v materializedEntityView) Subtype() string       { return v.e.Subtype }
func (v materializedEntityView) SourceFile() string    { return v.e.SourceFile }
func (v materializedEntityView) Language() string      { return v.e.Language }
func (v materializedEntityView) Signature() string     { return v.e.Signature }

// Property read surface: trivial pass-throughs to the wrapped *Entity's own
// methods (identical names + signatures — the wrapper exists only for the
// Name/Kind field-vs-method collision, not for these).
func (v materializedEntityView) PropGet(key string) string            { return v.e.PropGet(key) }
func (v materializedEntityView) PropLookup(key string) (string, bool) { return v.e.PropLookup(key) }
func (v materializedEntityView) PropLen() int                         { return v.e.PropLen() }
func (v materializedEntityView) PropRange(f func(k, v string) bool)   { v.e.PropRange(f) }
func (v materializedEntityView) PropsSnapshot() map[string]string     { return v.e.PropsSnapshot() }

// materializedRelationshipView adapts a concrete *Relationship to
// RelationshipView. See materializedEntityView for the wrapper rationale.
type materializedRelationshipView struct{ r *Relationship }

// RelationshipViewOf wraps a concrete *Relationship as a RelationshipView.
// Returns a nil interface for a nil relationship.
func RelationshipViewOf(r *Relationship) RelationshipView {
	if r == nil {
		return nil
	}
	return materializedRelationshipView{r: r}
}

func (v materializedRelationshipView) ID() string     { return v.r.ID }
func (v materializedRelationshipView) FromID() string { return v.r.FromID }
func (v materializedRelationshipView) ToID() string   { return v.r.ToID }
func (v materializedRelationshipView) Kind() string   { return v.r.Kind }

func (v materializedRelationshipView) PropGet(key string) string { return v.r.PropGet(key) }
func (v materializedRelationshipView) PropLookup(key string) (string, bool) {
	return v.r.PropLookup(key)
}
func (v materializedRelationshipView) PropLen() int                       { return v.r.PropLen() }
func (v materializedRelationshipView) PropRange(f func(k, v string) bool) { v.r.PropRange(f) }
func (v materializedRelationshipView) PropsSnapshot() map[string]string   { return v.r.PropsSnapshot() }

// Compile-time assertions that the materialized wrappers satisfy the interfaces.
var (
	_ EntityView       = materializedEntityView{}
	_ RelationshipView = materializedRelationshipView{}
)
