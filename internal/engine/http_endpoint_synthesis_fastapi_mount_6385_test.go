package engine

import (
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
