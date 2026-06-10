package auth

// Representative Go constant-collection shapes (#4426) — a stand-in for the
// kind of source-of-truth permission/status tables a rewrite parity-audit must
// diff. Mirrors the real Go idioms: a typed string-const group, an idiomatic
// untyped prefixed const group, and a package-level map-of-constants.

// Status is a same-file named type — its const group is owned by the existing
// enum_valueset.go go_iota path (NOT by the #4426 constant-set path), and is
// here only to prove the two paths do not double-emit.
type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
	StatusPending  Status = "pending"
)

// Idiomatic enum WITHOUT a named type: members share the `Role` prefix, so the
// untyped grouped const block is named `Role`.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleGuest  = "guest"
)

// Incidental const grouping (no shared prefix): must NOT become a value-set.
const (
	maxRetries = 3
	timeoutSec = 30
)

// Package-level map of constants — the Go sibling of the Django PERMISSION_PAGES
// dict / v3 PermissionPage const-object. The variable name `PermissionPages`
// names the value-set; each entry is a {key,value,line} member.
var PermissionPages = map[string]string{
	"CoreAdmin":         "core-admin",
	"ContractProposals": "contract-proposal",
	"Users":             "users",
	"Sync":              "sync",
}
