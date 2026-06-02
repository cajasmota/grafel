package python

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// pyEndpointProps runs the named framework extractor and indexes the emitted
// route endpoints by handler name.
func pyEndpointProps(t *testing.T, ex extractor.Extractor, path, src string) map[string]types.EntityRecord {
	t.Helper()
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	out := map[string]types.EntityRecord{}
	for _, e := range ents {
		if e.Subtype == "endpoint" {
			out[e.Name] = e
		}
	}
	return out
}

// slowapi @limiter.limit("5/minute") on a FastAPI route → rate_limit="5/minute".
func TestRateLimit_SlowapiFastAPI(t *testing.T) {
	src := `
from fastapi import FastAPI
from slowapi import Limiter

app = FastAPI()
limiter = Limiter(key_func=lambda: "x")

@app.get("/read")
@limiter.limit("5/minute")
async def read(request):
    return {}

@app.get("/free")
async def free(request):
    return {}
`
	eps := pyEndpointProps(t, &FastAPIExtractor{}, "main.py", src)
	read, ok := eps["read"]
	if !ok {
		t.Fatalf("read endpoint not emitted (got %v)", keysOf(eps))
	}
	if read.Properties["rate_limited"] != "true" {
		t.Errorf("read: rate_limited=%q, want true", read.Properties["rate_limited"])
	}
	if read.Properties["rate_limit"] != "5/minute" {
		t.Errorf("read: rate_limit=%q, want 5/minute", read.Properties["rate_limit"])
	}
	if read.Properties["rate_limit_source"] != "limiter.limit" {
		t.Errorf("read: rate_limit_source=%q, want limiter.limit", read.Properties["rate_limit_source"])
	}
	// Negative: a route without a limiter is not stamped.
	free := eps["free"]
	if free.Properties["rate_limited"] != "" {
		t.Errorf("free: rate_limited=%q, want empty (not fabricated)", free.Properties["rate_limited"])
	}
}

// flask-limiter @limiter.limit("100/hour") on a Flask route.
func TestRateLimit_FlaskLimiter(t *testing.T) {
	src := `
from flask import Flask
from flask_limiter import Limiter

app = Flask(__name__)
limiter = Limiter(app)

@app.route("/ping")
@limiter.limit("100/hour")
def ping():
    return "ok"
`
	eps := pyEndpointProps(t, &FlaskExtractor{}, "app.py", src)
	ping, ok := eps["ping"]
	if !ok {
		t.Fatalf("ping endpoint not emitted (got %v)", keysOf(eps))
	}
	if ping.Properties["rate_limited"] != "true" || ping.Properties["rate_limit"] != "100/hour" {
		t.Errorf("ping: limited=%q rate=%q, want true 100/hour",
			ping.Properties["rate_limited"], ping.Properties["rate_limit"])
	}
}

// django-ratelimit @ratelimit(key='ip', rate='5/m') on a Flask-shaped route
// (the decorator resolver is framework-agnostic; exercised via Flask here).
func TestRateLimit_DjangoRatelimitDecorator(t *testing.T) {
	src := `
from flask import Flask
from django_ratelimit.decorators import ratelimit

app = Flask(__name__)

@app.route("/limited")
@ratelimit(key='ip', rate='5/m')
def limited():
    return "ok"
`
	eps := pyEndpointProps(t, &FlaskExtractor{}, "app.py", src)
	e, ok := eps["limited"]
	if !ok {
		t.Fatalf("limited endpoint not emitted (got %v)", keysOf(eps))
	}
	if e.Properties["rate_limited"] != "true" {
		t.Errorf("limited: rate_limited=%q, want true", e.Properties["rate_limited"])
	}
	if e.Properties["rate_limit"] != "5/m" {
		t.Errorf("limited: rate_limit=%q, want 5/m", e.Properties["rate_limit"])
	}
	if e.Properties["rate_limit_scope"] != "ip" {
		t.Errorf("limited: rate_limit_scope=%q, want ip", e.Properties["rate_limit_scope"])
	}
}

// DRF throttle_classes resolver unit tests (value-asserting).
func TestResolveDRFThrottle(t *testing.T) {
	// Built-in throttle: scope resolved, rate honest-partial (lives in settings).
	block := `@throttle_classes([UserRateThrottle])`
	r := resolveDRFThrottle(block, "")
	if !r.found {
		t.Fatalf("UserRateThrottle: not found")
	}
	if r.Scope != "user" {
		t.Errorf("UserRateThrottle: scope=%q, want user", r.Scope)
	}
	if r.Rate != "" {
		t.Errorf("UserRateThrottle: rate=%q, want empty (settings-driven, honest-partial)", r.Rate)
	}
	if r.Source != "UserRateThrottle" {
		t.Errorf("source=%q, want UserRateThrottle", r.Source)
	}

	// Custom throttle subclass declaring rate='1000/day' → resolved.
	source := `
class DailyThrottle(UserRateThrottle):
    rate = '1000/day'

class V(APIView):
    throttle_classes = [DailyThrottle]
`
	r2 := resolveDRFThrottle(`throttle_classes = [DailyThrottle]`, source)
	if !r2.found || r2.Rate != "1000/day" {
		t.Errorf("DailyThrottle: found=%v rate=%q, want true 1000/day", r2.found, r2.Rate)
	}
}

// Negative: a non-throttle decorator block yields no rate-limit posture.
func TestResolveRateLimit_NoMarker(t *testing.T) {
	if r := resolvePyEndpointRateLimit(`@app.get("/x")\n@cache(60)`, ""); r.found {
		t.Errorf("non-throttle decorators: found=true, want false (rate_limited must be absent)")
	}
}

func keysOf(m map[string]types.EntityRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
