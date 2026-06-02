// Package baseknowledge is a typed, shared catalog of framework
// base-class / mixin contracts.
//
// Motivation (epic #3829, ticket #3832 — PR A0)
//
// Many web frameworks ship base classes and mixins whose subclasses
// inherit a fixed contract of HTTP-handler methods, default status
// codes, and behavioural guarantees WITHOUT declaring any of it in the
// user's source. A Django REST Framework `class RoleViewSet(ModelViewSet)`
// exposes `list / retrieve / create / update / partial_update / destroy`,
// each with a well-known default HTTP status (create -> 201, destroy ->
// 204, ...) and behaviour (e.g. `UpdateModelMixin.update` calls
// `serializer.is_valid(raise_exception=True)`, so an invalid payload
// yields 400 — the #278 parity-defect fact). None of this appears in the
// subclass body.
//
// Until now this knowledge lived in ad-hoc, per-language maps scattered
// across the extractors (`python.cbvBaseInheritedMethods`, the Laravel
// Eloquent map, the JPA/Panache map). Those maps are name-only and cannot
// feed MRO resolution (#3833) or effective-contract synthesis (#3835),
// which need the DEFINING class per member plus the default status and
// behavioural facts so they can synthesize an external mixin body the
// user's code never declares.
//
// This package is the single-source-of-truth catalog those downstream
// passes consume. It is intentionally language- AND framework-agnostic:
// a DRF pack, a Spring Data pack, a NestJS Crud pack, an ActiveRecord
// pack and an Eloquent pack all register through the same Pack interface
// (see pack.go) and are queried through the same Registry lookups
// (Lookup / MembersOf / Member / ResolveVerb).
//
// QUALITY NOTE: packs are curated DATA. When a behavioural fact is
// unknown we leave the field zero/empty rather than guess — never
// fabricate a status. Callers must treat DefaultStatus == 0 as "unknown,
// fall back to a body parse", not as a real status.
package baseknowledge

// Member is the inherited contract a single base-class member contributes
// to its subclasses. A "member" is a method the subclass inherits without
// declaring it (e.g. the DRF `create` verb from `CreateModelMixin`).
//
// For route-handler members HTTPVerb / DefaultStatus / the *Applicable
// fields carry the per-verb default contract facts that effective-contract
// synthesis (#3835) stamps onto synthesized routes. For non-route members
// HTTPVerb is "" and DefaultStatus is 0 (StatusUnknown).
type Member struct {
	// Name is the inherited method name as the framework names it — the
	// DRF verb method ("create", "partial_update", ...) or a generic CBV
	// HTTP handler ("get", "post", ...).
	Name string

	// DefiningClass is the FQN of the base class / mixin that actually
	// defines this member's body. This is the load-bearing field for MRO
	// resolution (#3833): given an inherited member, it names the external
	// class whose body get_source should synthesize. Always set.
	DefiningClass string

	// HTTPVerb is the upper-case HTTP method this member handles when it is
	// a route handler ("POST", "PATCH", ...). Empty for members that are
	// not HTTP route handlers.
	HTTPVerb string

	// DefaultStatus is the default success HTTP status the member returns
	// (201 create, 200 update/list/retrieve, 204 destroy). StatusUnknown
	// (0) means "no curated default — do not fabricate one". Callers must
	// not treat 0 as a real status.
	DefaultStatus int

	// ErrorStatuses lists non-success statuses the member can produce as a
	// documented part of its contract (e.g. DRF update -> 400 on invalid
	// payload via is_valid(raise_exception=True), the #278 fact). May be
	// empty.
	ErrorStatuses []int

	// Behaviour is a short, human-readable description of the member's
	// load-bearing runtime behaviour — e.g.
	// "is_valid(raise_exception=True) -> 400 on invalid payload".
	// Empty when no notable behaviour is curated.
	Behaviour string

	// PaginationApplicable is true when the framework applies the project's
	// default pagination to this member's response by default (DRF list).
	PaginationApplicable bool

	// PermissionApplicable is true when the framework applies the class /
	// project default permission classes to this member by default (true
	// for every DRF route handler).
	PermissionApplicable bool

	// DocURL optionally links the upstream documentation for the member's
	// contract. May be empty.
	DocURL string
}

// StatusUnknown is the sentinel DefaultStatus value meaning "no curated
// default status is known for this member". Callers MUST NOT treat it as a
// real HTTP status; it signals a fallback (e.g. parse the explicit body)
// is required.
const StatusUnknown = 0

// IsRoute reports whether the member is an HTTP route handler (carries a
// verb). Non-route members (helper hooks, finders) return false.
func (m Member) IsRoute() bool { return m.HTTPVerb != "" }

// BaseClassContract is the inherited contract of a single framework base
// class or mixin. It is the catalog's primary record: one per known base
// class (ModelViewSet, CreateModelMixin, JpaRepository, ...).
type BaseClassContract struct {
	// FQNs are the match keys for this base class, most-qualified first:
	// e.g. {"rest_framework.viewsets.ModelViewSet", "ModelViewSet"}. Lookup
	// matches any entry; leaf-only matching is supported via the registry's
	// leaf index so a subclass written `class X(ModelViewSet)` resolves
	// even without the dotted import path.
	FQNs []string

	// Language is the source language the base class belongs to ("python",
	// "java", "php", "ruby", "typescript", "go").
	Language string

	// Framework is the owning framework key ("drf", "django", "spring-data",
	// "eloquent", "activerecord", "nestjsx-crud").
	Framework string

	// Members maps the inherited member name to its contract. Includes only
	// the members this base class contributes directly (NOT members it would
	// itself inherit — composition is the registry's job via the base's own
	// declared bases, kept flat here for the framework-mixin case).
	Members map[string]Member
}

// Leaf returns the bare class name (last dotted segment of the first FQN).
// Empty when the contract has no FQNs.
func (c BaseClassContract) Leaf() string {
	if len(c.FQNs) == 0 {
		return ""
	}
	return leaf(c.FQNs[0])
}

// MemberNames returns the inherited member names this base contributes, in
// stable sorted order. Useful for the name-set use (the old
// cbvBaseInheritedMethods consumer).
func (c BaseClassContract) MemberNames() []string {
	names := make([]string, 0, len(c.Members))
	for n := range c.Members {
		names = append(names, n)
	}
	sortStrings(names)
	return names
}
