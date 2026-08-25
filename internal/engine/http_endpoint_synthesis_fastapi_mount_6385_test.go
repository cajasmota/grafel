package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6385 (@arthurgeron) — FastAPI routes mounted via
// `app.include_router(router, prefix="/network")` were emitted short by the
// mount prefix: only the `APIRouter(prefix=...)` constructor form was read.
//
// The FIRST arm of the fix does NOT fold the prefix into the route path (that
// is #6414 and needs a cross-file read). It emits one ADDITIVE `http_endpoint`
// `url_mount_point` synthetic per mount site, attributed to the mount file,
// carrying the literal prefix in `url_prefix`. That is the exact shape the
// Django nested-urlconf pass already emits
// (internal/engine/django_urlconf_nested.go:140-158) and the exact shape the
// linker's #2702 mount-prefix retry harvests
// (internal/links/http_pass.go:1047) — which keys ONLY on
// `pattern_type == url_mount_point`, never on `framework`.

// fastapiMountSynths collects the url_mount_point synthetics emitted for a
// file, keyed by their `url_prefix` property.
func fastapiMountSynths(res *DetectResult) map[string]types.EntityRecord {
	out := map[string]types.EntityRecord{}
	if res == nil {
		return out
	}
	for _, e := range res.Entities {
		if e.Kind != httpEndpointKind || e.Properties == nil {
			continue
		}
		if e.Properties["pattern_type"] != "url_mount_point" {
			continue
		}
		out[e.Properties["url_prefix"]] = e
	}
	return out
}

// TestSynth_FastAPI_IncludeRouterMountPoint_6385 is the core case from the
// issue: a `main.py` that mounts an imported router under a prefix must emit a
// url_mount_point synthetic carrying that prefix, attributed to the mount file.
func TestSynth_FastAPI_IncludeRouterMountPoint_6385(t *testing.T) {
	src := `from fastapi import FastAPI
from app.api import markets

app = FastAPI()
app.include_router(markets.router, prefix="/network")
`
	got, res := runDetect(t, "python", "app/main.py", src)

	mounts := fastapiMountSynths(res)
	e, ok := mounts["/network"]
	if !ok {
		t.Fatalf("#6385: no url_mount_point synthetic with url_prefix=/network (mounts: %v, ids: %v)", mounts, got)
	}
	if e.Properties["path"] != "/network" {
		t.Errorf("#6385: path = %q, want %q", e.Properties["path"], "/network")
	}
	if e.Properties["verb"] != "ANY" {
		t.Errorf("#6385: verb = %q, want ANY", e.Properties["verb"])
	}
	if e.Properties["framework"] != "fastapi" {
		t.Errorf("#6385: framework = %q, want fastapi", e.Properties["framework"])
	}
	if e.SourceFile != "app/main.py" {
		t.Errorf("#6385: SourceFile = %q, want app/main.py (the mount file)", e.SourceFile)
	}
	if e.Language != "python" {
		t.Errorf("#6385: Language = %q, want python", e.Language)
	}
	if e.StartLine != 5 {
		t.Errorf("#6385: StartLine = %d, want 5 (the include_router line)", e.StartLine)
	}
	requireContains(t, got, []string{"http:ANY:/network:mount"},
		"#6385 FastAPI include_router mount-point synthetic ID")
}

// TestSynth_FastAPI_IncludeRouterMountPoint_QuotesKwargOrderAndMultiple_6385
// covers single quotes, `prefix=` appearing after other kwargs, and several
// include_router calls in one file.
func TestSynth_FastAPI_IncludeRouterMountPoint_QuotesKwargOrderAndMultiple_6385(t *testing.T) {
	src := `from fastapi import FastAPI
from app.api import markets, users, health

app = FastAPI()
app.include_router(markets.router, prefix='/network')
app.include_router(users.router, tags=["users"], prefix="/api/v1/users")
app.include_router(health.router)
`
	_, res := runDetect(t, "python", "app/main.py", src)
	mounts := fastapiMountSynths(res)

	for _, want := range []string{"/network", "/api/v1/users"} {
		if _, ok := mounts[want]; !ok {
			t.Errorf("#6385: missing url_mount_point for %q (got: %v)", want, mounts)
		}
	}
	if len(mounts) != 2 {
		t.Errorf("#6385: got %d url_mount_point synthetics, want 2 (the bare include_router must emit nothing): %v",
			len(mounts), mounts)
	}
}

// TestSynth_FastAPI_IncludeRouterMountPoint_DegenerateAndAbsent_6385 asserts
// that an empty or root prefix, and a mount with no prefix kwarg at all, emit
// no mount-point synthetic rather than a degenerate one.
func TestSynth_FastAPI_IncludeRouterMountPoint_DegenerateAndAbsent_6385(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"empty", `from fastapi import FastAPI
app = FastAPI()
app.include_router(other.router, prefix="")
`},
		{"root", `from fastapi import FastAPI
app = FastAPI()
app.include_router(other.router, prefix="/")
`},
		{"absent", `from fastapi import FastAPI
app = FastAPI()
app.include_router(other.router)
`},
		{"trailing-slash-only", `from fastapi import FastAPI
app = FastAPI()
app.include_router(other.router, prefix="//")
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, res := runDetect(t, "python", "app/main.py", tc.src)
			if mounts := fastapiMountSynths(res); len(mounts) != 0 {
				t.Errorf("#6385 %s: expected no url_mount_point synthetic, got %v", tc.name, mounts)
			}
		})
	}
}

// TestSynth_FastAPI_IncludeRouterMountPoint_NoRouteRegression_6385 pins the
// scope boundary: the mount synthetic is ADDITIVE. Routes declared in the same
// file keep their unfolded paths — folding is #6414 and explicitly out of
// scope here.
func TestSynth_FastAPI_IncludeRouterMountPoint_NoRouteRegression_6385(t *testing.T) {
	src := `from fastapi import FastAPI, APIRouter

app = FastAPI()
router = APIRouter()

@router.get("/items")
async def list_items():
    return []

app.include_router(router, prefix="/network")
`
	got, res := runDetect(t, "python", "app/main.py", src)
	requireContains(t, got, []string{"http:GET:/items"},
		"#6385: mount synthetic must be additive — the route path is unchanged")
	requireNotContains(t, got, []string{"http:GET:/network/items"},
		"#6385: path folding is #6414 and out of scope")
	if _, ok := fastapiMountSynths(res)["/network"]; !ok {
		t.Errorf("#6385: expected the /network mount synthetic alongside the unfolded route")
	}
}

// ---------------------------------------------------------------------------
// PR #6415 review blockers.
// ---------------------------------------------------------------------------

// TestSynth_FastAPI_MountPrefix_InnerCallPrefixNotStolen_6385 is the HIGH
// blocker: the argument tail contains a NESTED call that itself carries a
// `prefix=` kwarg. Taking the first `prefix=` anywhere in the tail publishes
// the inner router's prefix as the mount prefix — a plausible-but-WRONG value
// that reaches the linker's cross-repo mount-prefix retry union. Only the
// TOP-LEVEL kwarg counts.
func TestSynth_FastAPI_MountPrefix_InnerCallPrefixNotStolen_6385(t *testing.T) {
	src := `from fastapi import FastAPI

app = FastAPI()
app.include_router(build_router(prefix="/internal"), prefix="/api/v1")
`
	_, res := runDetect(t, "python", "app/main.py", src)
	mounts := fastapiMountSynths(res)
	if _, bad := mounts["/internal"]; bad {
		t.Errorf("#6385: published the INNER call's prefix /internal as the mount prefix: %v", mounts)
	}
	if _, ok := mounts["/api/v1"]; !ok {
		t.Errorf("#6385: missing the real mount prefix /api/v1 (got %v)", mounts)
	}
}

// TestSynth_FastAPI_MountPrefix_TwoLevelsOfNesting_6385 pins the deeper form:
// two levels of nesting must neither abort the match nor leak the innermost
// `prefix=`.
func TestSynth_FastAPI_MountPrefix_TwoLevelsOfNesting_6385(t *testing.T) {
	src := `from fastapi import FastAPI, Depends

app = FastAPI()
app.include_router(users.router, dependencies=[Depends(auth(prefix="/nope"))], prefix="/deep")
`
	_, res := runDetect(t, "python", "app/main.py", src)
	mounts := fastapiMountSynths(res)
	if _, bad := mounts["/nope"]; bad {
		t.Errorf("#6385: leaked a doubly-nested prefix: %v", mounts)
	}
	if _, ok := mounts["/deep"]; !ok {
		t.Errorf("#6385: two nesting levels aborted the match, /deep missing (got %v)", mounts)
	}
}

// TestSynth_FastAPI_MountPrefix_TwoCallsOnOneLine_6385 pins that a second
// mount site on the SAME line is not swallowed by a line anchor.
func TestSynth_FastAPI_MountPrefix_TwoCallsOnOneLine_6385(t *testing.T) {
	src := `from fastapi import FastAPI

app = FastAPI()
app.include_router(a.router, prefix="/one"); app.include_router(b.router, prefix="/two")
`
	_, res := runDetect(t, "python", "app/main.py", src)
	mounts := fastapiMountSynths(res)
	for _, want := range []string{"/one", "/two"} {
		if _, ok := mounts[want]; !ok {
			t.Errorf("#6385: missing %q from a two-calls-on-one-line file (got %v)", want, mounts)
		}
	}
}

// TestSynth_FastAPI_MountPrefix_PureWiringFileNoDecoratorMarker_6385 is the
// load-bearing fixture for the PR's central design decision: the mount
// emission runs BEFORE the decorator marker guard because a pure wiring file
// may carry no decorator marker at all. Every other fixture contains
// `FastAPI`/`APIRouter`, so it passes the guard and cannot tell the two
// placements apart. This one carries ONLY a lowercase `fastapi` import — it
// fails the marker guard, so it only produces a mount if the emission really
// is ahead of that guard.
func TestSynth_FastAPI_MountPrefix_PureWiringFileNoDecoratorMarker_6385(t *testing.T) {
	src := `from fastapi import Depends
from app.core import app
from app.api import users

app.include_router(users.router, prefix="/wiring", dependencies=[Depends(auth)])
`
	for _, marker := range []string{"FastAPI", "APIRouter", "@app.", "@router.", ".add_api_route("} {
		if strings.Contains(src, marker) {
			t.Fatalf("#6385: fixture is not marker-free — it contains %q, so it cannot pin the emission order", marker)
		}
	}
	_, res := runDetect(t, "python", "app/main.py", src)
	if _, ok := fastapiMountSynths(res)["/wiring"]; !ok {
		t.Errorf("#6385: a pure wiring file with no decorator marker emitted no mount synthetic (got %v)",
			fastapiMountSynths(res))
	}
}

// TestSynth_FastAPI_MountPrefix_RequiresFastAPIEvidence_6385 is the other side
// of that placement: running ahead of the marker guard must not turn
// `include_router` alone into FastAPI evidence. A file with no FastAPI signal
// whatsoever — a Flask module, or an `include_router` that only appears inside
// a docstring — must emit nothing.
func TestSynth_FastAPI_MountPrefix_RequiresFastAPIEvidence_6385(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"flask-file", `from flask import Flask

app = Flask(__name__)
routers.include_router(x, prefix="/flaskbogus")
`},
		{"docstring-only", `from fastapi import FastAPI

app = FastAPI()

def helper():
    """Usage:

    app.include_router(other.router, prefix="/docbogus")
    """
    return None
`},
		{"triple-single-quoted-literal", `from fastapi import FastAPI

app = FastAPI()
SAMPLE = '''app.include_router(other.router, prefix="/litbogus")'''
`},
		{"commented-out", `from fastapi import FastAPI

app = FastAPI()
# app.include_router(other.router, prefix="/cmtbogus")
`},
		{"lookalike-method", `from fastapi import FastAPI

app = FastAPI()
my_include_router(other.router, prefix="/lookalike")
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, res := runDetect(t, "python", "app/main.py", tc.src)
			if mounts := fastapiMountSynths(res); len(mounts) != 0 {
				t.Errorf("#6385 %s: expected no url_mount_point synthetic, got %v", tc.name, mounts)
			}
		})
	}
}

// TestSynth_FastAPI_MountPrefix_RejectsMalformedLiterals_6385 pins that a
// prefix literal carrying an escaped quote, a backslash, or whitespace never
// reaches an entity ID or the linker's retry-candidate list.
func TestSynth_FastAPI_MountPrefix_RejectsMalformedLiterals_6385(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"escaped-quote", `from fastapi import FastAPI

app = FastAPI()
app.include_router(r.router, prefix="/a\"b")
`},
		{"spaced", `from fastapi import FastAPI

app = FastAPI()
app.include_router(r.router, prefix=" /spaced ")
`},
		{"inner-space", `from fastapi import FastAPI

app = FastAPI()
app.include_router(r.router, prefix="/two words")
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, res := runDetect(t, "python", "app/main.py", tc.src)
			if mounts := fastapiMountSynths(res); len(mounts) != 0 {
				t.Errorf("#6385 %s: malformed prefix reached a mount synthetic: %v (ids %v)", tc.name, mounts, got)
			}
		})
	}
}

// TestSynth_FastAPI_MountPrefix_SingleLineStringIsAKnownFalsePositive_6418 pins
// a KNOWN LIMITATION, not a desired property. #6418: `pythonMaskInertRegions`
// deliberately leaves single-line string literals INTACT — it must, because the
// mount prefix itself lives inside one (`prefix="/x"`), and blanking short
// literals would blank the very value the scan reads out. The cost of that
// trade-off is this: a single-line string whose *contents* happen to spell a
// whole `include_router(..., prefix=...)` call is not inert to the scan, so it
// mints a `url_mount_point` synthetic for a mount that does not exist.
//
// This test exists so the trade-off is a recorded decision rather than an
// accident, and so a masker change that silently altered it would be noticed.
// It is NOT a guard against fixing the problem. The real fix is a Python
// tokenizer that can tell a literal's contents from code; whoever writes one
// SHOULD expect this assertion to flip to "emits nothing" and should invert it
// deliberately. A flip here is the fix landing, not a regression.
func TestSynth_FastAPI_MountPrefix_SingleLineStringIsAKnownFalsePositive_6418(t *testing.T) {
	src := `from fastapi import FastAPI

app = FastAPI()
TEMPLATE = "app.include_router(other.router, prefix='/strbogus')"
`
	// Guard the premise: the masker must still be leaving this literal intact.
	// If it ever blanks single-line literals, the expectation below is stale
	// for a reason the reader needs to see spelled out.
	if masked := pythonMaskInertRegions(src); !strings.Contains(masked, "include_router") {
		t.Fatalf("#6418: pythonMaskInertRegions now blanks single-line literals — the known limitation this test pins is GONE. "+
			"That is the fix, not a break: invert this test to assert no url_mount_point is emitted. masked=%q", masked)
	}

	_, res := runDetect(t, "python", "app/main.py", src)

	mounts := fastapiMountSynths(res)
	if _, ok := mounts["/strbogus"]; !ok {
		t.Errorf("#6418: expected the KNOWN-LIMITATION url_mount_point with url_prefix=/strbogus "+
			"(a single-line string literal is not masked, so its contents reach the mount scan), got %v. "+
			"If you just taught the masker to tokenize Python properly, this is the intended improvement — "+
			"flip this test to assert no mount synthetic is emitted.", mounts)
	}
}
