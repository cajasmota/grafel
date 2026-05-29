package python_test

import (
	"testing"
)

func TestHTTPMiddlewareExtractor_Aiohttp(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
from aiohttp import web

async def my_middleware(app, handler):
    async def middleware(request):
        return await handler(request)
    return middleware

app = web.Application()
app.middlewares = [my_middleware]
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for aiohttp middleware")
	}
	found := false
	for _, e := range entities {
		if e.Props["framework"] == "aiohttp" && e.Props["pattern_type"] == "middleware_list" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected aiohttp middleware_list entity, got %+v", entities)
	}
}

func TestHTTPMiddlewareExtractor_Bottle(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
import bottle
from bottle import Bottle

app = Bottle()

@bottle.hook('before_request')
def before():
    pass
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for bottle hook")
	}
	found := false
	for _, e := range entities {
		if e.Props["framework"] == "bottle" && e.Props["pattern_type"] == "hook" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected bottle hook entity, got %+v", entities)
	}
}

func TestHTTPMiddlewareExtractor_Falcon(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
import falcon

class AuthMiddleware:
    def process_request(self, req, resp):
        pass

app = falcon.App(middleware=[AuthMiddleware()])
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for falcon middleware")
	}
	found := false
	for _, e := range entities {
		if e.Props["framework"] == "falcon" && e.Props["pattern_type"] == "middleware_list" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected falcon middleware_list entity, got %+v", entities)
	}
}

func TestHTTPMiddlewareExtractor_Hug(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
import hug

@hug.request_middleware
def process_data(request, response):
    pass
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for hug middleware")
	}
	found := false
	for _, e := range entities {
		if e.Props["framework"] == "hug" && e.Name == "process_data" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected hug middleware entity, got %+v", entities)
	}
}

func TestHTTPMiddlewareExtractor_Litestar(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
from litestar import Litestar
from litestar.middleware import AbstractMiddleware

class LoggingMiddleware(AbstractMiddleware):
    async def __call__(self, scope, receive, send):
        pass

app = Litestar(middleware=[LoggingMiddleware])
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for litestar middleware")
	}
	var foundList, foundClass bool
	for _, e := range entities {
		if e.Props["framework"] == "litestar" && e.Props["pattern_type"] == "middleware_list" {
			foundList = true
		}
		if e.Props["framework"] == "litestar" && e.Props["pattern_type"] == "middleware_class" {
			foundClass = true
		}
	}
	if !foundList {
		t.Errorf("expected litestar middleware_list, got %+v", entities)
	}
	if !foundClass {
		t.Errorf("expected litestar middleware_class, got %+v", entities)
	}
}

func TestHTTPMiddlewareExtractor_Pyramid(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
from pyramid.config import Configurator

def main(global_config, **settings):
    config = Configurator(settings=settings)
    config.add_tween('myapp.tweens.timing_tween_factory')
    return config.make_wsgi_app()
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for pyramid tween")
	}
	found := false
	for _, e := range entities {
		if e.Props["framework"] == "pyramid" && e.Props["pattern_type"] == "tween" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected pyramid tween entity, got %+v", entities)
	}
}

func TestHTTPMiddlewareExtractor_Quart(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
from quart import Quart

app = Quart(__name__)

@app.before_request
async def before():
    pass
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for quart hook")
	}
	found := false
	for _, e := range entities {
		if e.Props["framework"] == "quart" && e.Props["pattern_type"] == "request_hook" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected quart request_hook entity, got %+v", entities)
	}
}

func TestHTTPMiddlewareExtractor_Robyn(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
from robyn import Robyn

app = Robyn(__file__)

@app.before_request
async def before(request):
    return request
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for robyn before_request")
	}
	found := false
	for _, e := range entities {
		if e.Props["framework"] == "robyn" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected robyn middleware entity, got %+v", entities)
	}
}

func TestHTTPMiddlewareExtractor_Sanic(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
from sanic import Sanic

app = Sanic(__name__)

@app.middleware('request')
async def add_start_time(request):
    pass
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for sanic middleware")
	}
	found := false
	for _, e := range entities {
		if e.Props["framework"] == "sanic" && e.Props["pattern_type"] == "middleware_decorator" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected sanic middleware_decorator entity, got %+v", entities)
	}
}

func TestHTTPMiddlewareExtractor_Starlette(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
from starlette.applications import Starlette
from starlette.middleware import Middleware
from starlette.middleware.cors import CORSMiddleware

app = Starlette(middleware=[
    Middleware(CORSMiddleware, allow_origins=["*"]),
])
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for starlette middleware")
	}
	found := false
	for _, e := range entities {
		if e.Props["framework"] == "starlette" && e.Props["pattern_type"] == "middleware_list" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected starlette middleware_list entity, got %+v", entities)
	}
}

func TestHTTPMiddlewareExtractor_Strawberry(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
import strawberry
from strawberry.extensions import QueryDepthLimiter

schema = strawberry.Schema(
    query=Query,
    extensions=[QueryDepthLimiter(max_depth=5)],
)
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for strawberry extensions")
	}
	found := false
	for _, e := range entities {
		if e.Props["framework"] == "strawberry-graphql" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected strawberry-graphql entity, got %+v", entities)
	}
}

func TestHTTPMiddlewareExtractor_Tornado(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
import tornado.web

class MainHandler(tornado.web.RequestHandler):
    def prepare(self):
        self.set_header("X-Request-ID", "123")

    def get(self):
        self.write("Hello")
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for tornado prepare")
	}
	found := false
	for _, e := range entities {
		if e.Props["framework"] == "tornado" && e.Props["pattern_type"] == "prepare_hook" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected tornado prepare_hook entity, got %+v", entities)
	}
}

func TestHTTPMiddlewareExtractor_CherryPy(t *testing.T) {
	entities := extract(t, "python_http_middleware", `
import cherrypy

cherrypy.tools.auth = cherrypy.Tool('before_handler', check_auth)

class Root:
    _cp_config = {'/': {'tools.auth.on': True}}

    @cherrypy.expose
    def index(self):
        return "Hello"
`)
	if len(entities) == 0 {
		t.Fatal("expected at least one entity for cherrypy tool")
	}
	found := false
	for _, e := range entities {
		if e.Props["framework"] == "cherrypy" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected cherrypy tool entity, got %+v", entities)
	}
}
