package engine

import "testing"

// #6414 (@arthurgeron) — the mount synthetic emitted by #6385 carries the
// literal prefix and nothing that says WHICH router it mounts, and the route
// endpoint carries no router identity either. The two entities have no join
// key, so nothing can pair a `url_mount_point` with the routes it prefixes.
//
// This pins the mount side of that key: `mounted_module` (the dotted Python
// module that defines the router) and `mounted_router` (its name in that
// module), resolved from the MOUNT FILE'S OWN IMPORTS only. That keeps the pass per-file, so it
// behaves identically under a full index, `--incremental` and the daemon —
// no incremental contract is involved and no path moves.
//
// Both properties are ABSENT, never empty or guessed, whenever the argument is
// not a plain name, the binding is not an absolute `from X import Y`, or the
// same prefix is mounted by two different routers in one file.

// mountProp reads one property off the url_mount_point synthetic carrying
// urlPrefix, reporting whether the property is present at all.
func mountProp(t *testing.T, src, relPath, urlPrefix, key string) (string, bool) {
	t.Helper()
	_, res := runDetect(t, "python", relPath, src)
	e, ok := fastapiMountSynths(res)[urlPrefix]
	if !ok {
		t.Fatalf("#6414: no url_mount_point synthetic with url_prefix=%q (got %v)",
			urlPrefix, fastapiMountSynths(res))
	}
	v, present := e.Properties[key]
	return v, present
}

func requireMountedRouter(t *testing.T, src, relPath, urlPrefix, wantModule, wantRouter string) {
	t.Helper()
	if got, present := mountProp(t, src, relPath, urlPrefix, "mounted_module"); !present || got != wantModule {
		t.Errorf("#6414: mounted_module = %q (present=%v), want %q", got, present, wantModule)
	}
	if got, present := mountProp(t, src, relPath, urlPrefix, "mounted_router"); !present || got != wantRouter {
		t.Errorf("#6414: mounted_router = %q (present=%v), want %q", got, present, wantRouter)
	}
}

func requireNoMountedRouter(t *testing.T, label, src, relPath, urlPrefix string) {
	t.Helper()
	for _, key := range []string{"mounted_module", "mounted_router"} {
		if got, present := mountProp(t, src, relPath, urlPrefix, key); present {
			t.Errorf("#6414 %s: %s present as %q, want absent", label, key, got)
		}
	}
}

// TestSynth_FastAPI_MountedRouter_DottedModuleAttr_6414 is the case from the
// issue: `from app.api import markets` + `markets.router`.
func TestSynth_FastAPI_MountedRouter_DottedModuleAttr_6414(t *testing.T) {
	src := `from fastapi import FastAPI
from app.api import markets

app = FastAPI()
app.include_router(markets.router, prefix="/network")
`
	requireMountedRouter(t, src, "app/main.py", "/network", "app.api.markets", "router")
}

// TestSynth_FastAPI_MountedRouter_BareImportedName_6414 covers the other real
// spelling: the router itself is imported, so the mount argument is a bare
// name and the module path is the import's own module.
func TestSynth_FastAPI_MountedRouter_BareImportedName_6414(t *testing.T) {
	src := `from fastapi import FastAPI
from app.api.markets import router

app = FastAPI()
app.include_router(router, prefix="/network")
`
	requireMountedRouter(t, src, "app/main.py", "/network", "app.api.markets", "router")
}

// TestSynth_FastAPI_MountedRouter_AsAliases_6414 is the trap that rules
// parseImports out for the dotted form: it binds the ALIAS and discards the
// original name, so `from app.api import markets as mkts` would resolve to the
// module `app.api.mkts`, which does not exist. Both properties must name the
// identifiers as they exist in the DEFINING module, not as bound here.
func TestSynth_FastAPI_MountedRouter_AsAliases_6414(t *testing.T) {
	t.Run("aliased-module", func(t *testing.T) {
		src := `from fastapi import FastAPI
from app.api import markets as mkts

app = FastAPI()
app.include_router(mkts.router, prefix="/network")
`
		requireMountedRouter(t, src, "app/main.py", "/network", "app.api.markets", "router")
	})
	t.Run("aliased-router", func(t *testing.T) {
		src := `from fastapi import FastAPI
from app.api.markets import router as markets_router

app = FastAPI()
app.include_router(markets_router, prefix="/network")
`
		requireMountedRouter(t, src, "app/main.py", "/network", "app.api.markets", "router")
	})
}

// TestSynth_FastAPI_MountedRouter_ParenthesisedImport_6414 pins that a
// multi-line parenthesised import resolves, since that is the shape a wiring
// module with many routers actually has.
func TestSynth_FastAPI_MountedRouter_ParenthesisedImport_6414(t *testing.T) {
	src := `from fastapi import FastAPI
from app.api import (
    markets,
    users,
)

app = FastAPI()
app.include_router(markets.router, prefix="/network")
app.include_router(users.router, prefix="/people")
`
	requireMountedRouter(t, src, "app/main.py", "/network", "app.api.markets", "router")
	requireMountedRouter(t, src, "app/main.py", "/people", "app.api.users", "router")
}

// TestSynth_FastAPI_MountedRouter_AbsentWhenUnresolvable_6414 pins every shape
// that must produce NO join key rather than a guessed one. A wrong file path
// is worse than a missing one: it joins a mount to the wrong routes silently.
func TestSynth_FastAPI_MountedRouter_AbsentWhenUnresolvable_6414(t *testing.T) {
	for _, tc := range []struct{ name, src, prefix string }{
		// Nothing binds the name in this file — the router may be defined
		// here, injected, or imported by a form this does not read.
		{"unbound-name", `from fastapi import FastAPI

app = FastAPI()
app.include_router(markets.router, prefix="/network")
`, "/network"},
		// A relative import is anchored on the mount file's own package, so
		// `.api` is not an absolute module path and publishing it as one
		// would name a module that does not exist. A dot-depth walk-up to
		// make it absolute is separate work.
		{"relative-import", `from fastapi import FastAPI
from .api import markets

app = FastAPI()
app.include_router(markets.router, prefix="/network")
`, "/network"},
		{"relative-import-parent", `from fastapi import FastAPI
from ..api import markets

app = FastAPI()
app.include_router(markets.router, prefix="/network")
`, "/network"},
		// `import app.api.markets` is not a `from` import; parsing it is not
		// in this slice.
		{"plain-import", `from fastapi import FastAPI
import app.api.markets

app = FastAPI()
app.include_router(app.api.markets.router, prefix="/network")
`, "/network"},
		// The argument is a call, not a name. The prefix still resolves (#6385
		// pins that) but there is no router to name.
		{"call-expression", `from fastapi import FastAPI
from app.api import markets

app = FastAPI()
app.include_router(markets.build_router(prefix="/internal"), prefix="/api/v1")
`, "/api/v1"},
		// A commented-out import must not bind the name.
		{"commented-import", `from fastapi import FastAPI
# from app.api import markets

app = FastAPI()
app.include_router(markets.router, prefix="/network")
`, "/network"},
		// Two DIFFERENT routers at one prefix collapse into one synthetic,
		// because the mount ID keys on the prefix alone. Naming whichever
		// mount was scanned first would assert a join that is only half true.
		{"two-routers-one-prefix", `from fastapi import FastAPI
from app.api import markets, users

app = FastAPI()
app.include_router(markets.router, prefix="/network")
app.include_router(users.router, prefix="/network")
`, "/network"},
		// The collapse must not care WHY the second mount is unresolvable. One
		// resolvable router and one call expression at the same prefix is the
		// same half-true join as two named routers: the surviving record would
		// claim `app.api.markets` while also covering the auth router mounted
		// there. Guarding the ambiguity drop on resolvability leaves the whole
		// package green, and the call-expression class is the largest absence
		// on the measured corpus (15 of 16), so this is the shape that shows up.
		{"mixed-resolvability-same-prefix", `from fastapi import FastAPI
import fastapi_users
from app.api import markets

app = FastAPI()
app.include_router(markets.router, prefix="/network")
app.include_router(fastapi_users.get_auth_router(backend), prefix="/network")
`, "/network"},
		// The same file with the mounts swapped. Only the order above can carry
		// a stale key into the surviving record, so this row pins that the drop
		// does not depend on which mount was scanned first.
		{"mixed-resolvability-same-prefix-reversed", `from fastapi import FastAPI
import fastapi_users
from app.api import markets

app = FastAPI()
app.include_router(fastapi_users.get_auth_router(backend), prefix="/network")
app.include_router(markets.router, prefix="/network")
`, "/network"},
		// An attribute chain deeper than one level names an object this pass
		// cannot follow, so `container` is what would have to resolve, not
		// `markets`. The leading anchor on fastapiMountedRouterArgRe is what
		// rejects it: without the anchor the tail `markets.router` matches, and
		// because `markets` IS from-import bound here it would publish
		// `app.api.markets`/`router` for an unrelated object. The plain-import
		// row above cannot see that, since nothing from-import binds the name
		// there and it returns absent either way.
		{"deep-attribute-chain", `from fastapi import FastAPI
from app.api import markets

app = FastAPI()
app.include_router(container.markets.router, prefix="/network")
`, "/network"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireNoMountedRouter(t, tc.name, tc.src, "app/main.py", tc.prefix)
		})
	}
}

// TestSynth_FastAPI_MountedRouter_SamePrefixSameRouterTwice_6414 is the other
// side of the collapse: the same router mounted twice at one prefix is not an
// ambiguity, so the key survives.
func TestSynth_FastAPI_MountedRouter_SamePrefixSameRouterTwice_6414(t *testing.T) {
	src := `from fastapi import FastAPI
from app.api import markets

app = FastAPI()
app.include_router(markets.router, prefix="/network")
app.include_router(markets.router, prefix="/network", tags=["dup"])
`
	requireMountedRouter(t, src, "app/main.py", "/network", "app.api.markets", "router")
}

// TestSynth_FastAPI_MountedRouter_PathIsStillUnfolded_6414 restates the scope
// boundary at the point where it now matters most: this slice creates the join
// key and does NOT use it. Every path is byte-identical to before.
func TestSynth_FastAPI_MountedRouter_PathIsStillUnfolded_6414(t *testing.T) {
	src := `from fastapi import FastAPI, APIRouter

app = FastAPI()
router = APIRouter()

@router.get("/items")
async def list_items():
    return []

app.include_router(router, prefix="/network")
`
	got, _ := runDetect(t, "python", "app/main.py", src)
	requireContains(t, got, []string{"http:GET:/items"},
		"#6414: stamping the join key must not fold the path")
	requireNotContains(t, got, []string{"http:GET:/network/items"},
		"#6414: the fold itself is still out of scope")
}
