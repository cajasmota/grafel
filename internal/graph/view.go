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
	// Property returns the value for key and whether it was present. Storage-
	// agnostic by contract (ADR-0027 §Interaction): compatible with both the
	// map-backed and the in-flight []propKV backing, and later with values
	// aliased straight out of the mmap.
	Property(key string) (string, bool)
	// Properties returns a snapshot copy of all properties. Kept storage-agnostic
	// for the same reason as Property.
	Properties() map[string]string
}

// RelationshipView is the read-only view of a graph relationship.
type RelationshipView interface {
	ID() string
	FromID() string
	ToID() string
	Kind() string
	Property(key string) (string, bool)
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

func (v materializedEntityView) Property(key string) (string, bool) {
	return v.e.PropLookup(key)
}

func (v materializedEntityView) Properties() map[string]string {
	return v.e.PropsSnapshot()
}

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

func (v materializedRelationshipView) Property(key string) (string, bool) {
	return v.r.PropLookup(key)
}

// Compile-time assertions that the materialized wrappers satisfy the interfaces.
var (
	_ EntityView       = materializedEntityView{}
	_ RelationshipView = materializedRelationshipView{}
)
