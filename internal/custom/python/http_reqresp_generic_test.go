package python_test

// http_reqresp_generic_test.go — tests for http_reqresp_generic.go
//
// Covers:
//   - dto_extraction: Pydantic BaseModel type-annotated params detected in
//     route/handler functions for generic Python web frameworks.
//   - request_validation: Django Form.is_valid() + cleaned_data patterns.
//   - request_validation: DRF request.data / .validated_data / .is_valid().
//   - request_validation: Pydantic model_validate / parse_obj calls in handler body.
//   - Framework gate: files without target framework imports produce 0 entities.
//
// Issue #3185 — T12: Python generic HTTP dto_extraction + request_validation.

import (
	"os"
	"strings"
	"testing"
)

const ghrExtractor = "python_http_reqresp_generic"

// fixtureGHR loads a testdata file for the generic HTTP extractor tests.
func fixtureGHR(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("fixtureGHR: %v", err)
	}
	return string(b)
}

// hasGHREntity returns true if any extracted entity matches name+kind+props.
func hasGHREntity(result []extractResult, name, kind string, props map[string]string) bool {
	for _, e := range result {
		if name != "" && e.Name != name {
			continue
		}
		if kind != "" && e.Kind != kind {
			continue
		}
		match := true
		for k, v := range props {
			if e.Props[k] != v {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// countGHREntities returns the count of entities matching kind+props.
func countGHREntities(result []extractResult, kind string, props map[string]string) int {
	n := 0
	for _, e := range result {
		if kind != "" && e.Kind != kind {
			continue
		}
		match := true
		for k, v := range props {
			if e.Props[k] != v {
				match = false
				break
			}
		}
		if match {
			n++
		}
	}
	return n
}

// ============================================================================
// Framework gate tests
// ============================================================================

func TestGHR_NoMatchWithoutFrameworkImport(t *testing.T) {
	src := `from pydantic import BaseModel

class UserCreate(BaseModel):
    name: str

def create_user(user: UserCreate):
    pass
`
	ents := extract(t, ghrExtractor, src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities for file without framework import, got %d", len(ents))
	}
}

func TestGHR_EmptyFile(t *testing.T) {
	ents := extract(t, ghrExtractor, "")
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities for empty file, got %d", len(ents))
	}
}

// ============================================================================
// Litestar — dto_extraction
// ============================================================================

func TestGHR_Litestar_PydanticParam(t *testing.T) {
	src := `from litestar import post
from pydantic import BaseModel

class BookRequest(BaseModel):
    title: str
    author: str

@post("/books")
async def create_book(data: BookRequest) -> dict:
    return {"id": 1}
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":    "litestar",
		"pattern_type": "request_dto",
		"dto_type":     "BookRequest",
	}) {
		t.Fatal("expected dto_extraction entity for BookRequest in litestar handler")
	}

	if !hasGHREntity(ents, "create_book:accepts:BookRequest", "SCOPE.Operation", map[string]string{
		"framework":    "litestar",
		"pattern_type": "accepts_input",
		"dto_type":     "BookRequest",
	}) {
		t.Fatal("expected ACCEPTS_INPUT operation entity for create_book:accepts:BookRequest")
	}
}

func TestGHR_Litestar_PrimitiveParamNotEmitted(t *testing.T) {
	src := `from litestar import get

@get("/items/{item_id:int}")
async def get_item(item_id: int) -> dict:
    return {"id": item_id}
`
	ents := extract(t, ghrExtractor, src)
	for _, e := range ents {
		if e.Props["pattern_type"] == "request_dto" {
			t.Fatalf("primitive type param should not be emitted as dto, got entity: %+v", e)
		}
	}
}

// ============================================================================
// Sanic — dto_extraction
// ============================================================================

func TestGHR_Sanic_PydanticParam(t *testing.T) {
	src := `from sanic import Sanic
from sanic.response import json as sanic_json
from pydantic import BaseModel

app = Sanic("test")

class OrderRequest(BaseModel):
    item_id: int
    quantity: int

@app.post("/orders")
async def create_order(request):
    order = OrderRequest.parse_obj(request.json)
    return sanic_json({"id": 1})
`
	ents := extract(t, ghrExtractor, src)

	// parse_obj call should emit request_validation
	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":    "sanic",
		"pattern_type": "request_validation",
		"via":          "request_validation",
	}) {
		t.Fatal("expected request_validation entity for sanic parse_obj call")
	}
}

// ============================================================================
// Falcon — dto_extraction via on_post handler
// ============================================================================

func TestGHR_Falcon_OnPostHandler(t *testing.T) {
	src := `import falcon
from pydantic import BaseModel

class ProductRequest(BaseModel):
    title: str
    price: float

class ProductResource:
    def on_post(self, req, resp):
        product = ProductRequest.model_validate(req.media)
        resp.media = {"id": 1}
`
	ents := extract(t, ghrExtractor, src)

	// model_validate in body → request_validation
	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":       "falcon",
		"pattern_type":    "request_validation",
		"validation_kind": "pydantic_model_validate",
	}) {
		t.Fatal("expected request_validation for falcon on_post handler with model_validate")
	}
}

func TestGHR_Falcon_OnGetNoDTO(t *testing.T) {
	src := `import falcon

class ItemResource:
    def on_get(self, req, resp):
        resp.media = []
`
	ents := extract(t, ghrExtractor, src)
	for _, e := range ents {
		if e.Props["pattern_type"] == "request_dto" {
			t.Fatalf("on_get should not emit request_dto, got: %+v", e)
		}
	}
}

// ============================================================================
// Tornado — dto_extraction
// ============================================================================

func TestGHR_Tornado_PostHandler(t *testing.T) {
	src := `import tornado.web
import tornado.escape
from pydantic import BaseModel

class ArticleRequest(BaseModel):
    title: str
    body: str

class ArticleHandler(tornado.web.RequestHandler):
    def post(self):
        data = tornado.escape.json_decode(self.request.body)
        article = ArticleRequest.model_validate(data)
        self.write({"id": 1})
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":       "tornado",
		"pattern_type":    "request_validation",
		"validation_kind": "pydantic_model_validate",
	}) {
		t.Fatal("expected request_validation for tornado post handler with model_validate")
	}
}

// ============================================================================
// Starlette — dto_extraction + request_validation
// ============================================================================

func TestGHR_Starlette_ModelValidate(t *testing.T) {
	src := `from starlette.applications import Starlette
from starlette.routing import Route
from starlette.requests import Request
from starlette.responses import JSONResponse
from pydantic import BaseModel

class CreateItemRequest(BaseModel):
    name: str
    price: float

async def create_item_endpoint(request: Request):
    payload = CreateItemRequest.model_validate(await request.json())
    return JSONResponse({"id": 1})

app = Starlette(routes=[Route("/items", create_item_endpoint, methods=["POST"])])
`
	ents := extract(t, ghrExtractor, src)

	// model_validate → request_validation
	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":       "starlette",
		"pattern_type":    "request_validation",
		"validation_kind": "pydantic_model_validate",
	}) {
		t.Fatal("expected request_validation for starlette handler with model_validate")
	}
}

// ============================================================================
// Aiohttp — dto_extraction
// ============================================================================

func TestGHR_Aiohttp_RouteHandler(t *testing.T) {
	src := `from aiohttp import web
from pydantic import BaseModel

class UserRequest(BaseModel):
    name: str
    email: str

async def create_user_handler(request: web.Request) -> web.Response:
    body = await request.json()
    user = UserRequest.model_validate(body)
    return web.json_response({"id": 1})

app = web.Application()
app.router.add_post("/users", create_user_handler)
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":    "aiohttp",
		"pattern_type": "request_validation",
	}) {
		t.Fatal("expected request_validation entity for aiohttp handler")
	}
}

// ============================================================================
// Django — request_validation (Form.is_valid / cleaned_data)
// ============================================================================

func TestGHR_Django_FormIsValid(t *testing.T) {
	src := `from django.http import HttpResponse
from django import forms

class ContactForm(forms.Form):
    name = forms.CharField(max_length=100)
    email = forms.EmailField()

def contact_view(request):
    if request.method == "POST":
        form = ContactForm(request.POST)
        if form.is_valid():
            name = form.cleaned_data["name"]
            return HttpResponse("OK")
    return HttpResponse("form")
`
	ents := extract(t, ghrExtractor, src)

	// Form class definition → dto_extraction
	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":    "django",
		"pattern_type": "request_dto",
		"dto_type":     "ContactForm",
	}) {
		t.Fatal("expected dto_extraction entity for ContactForm class")
	}

	// form.is_valid() → request_validation
	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":       "django",
		"pattern_type":    "request_validation",
		"validation_kind": "django_form_is_valid",
	}) {
		t.Fatal("expected request_validation entity for form.is_valid()")
	}
}

func TestGHR_Django_CleanedData(t *testing.T) {
	src := `from django.http import HttpResponse
from django import forms

class SignupForm(forms.Form):
    username = forms.CharField()

def signup_view(request):
    form = SignupForm(request.POST)
    if form.is_valid():
        username = form.cleaned_data["username"]
        return HttpResponse("ok")
    return HttpResponse("err")
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":       "django",
		"pattern_type":    "request_validation",
		"validation_kind": "django_form_cleaned_data",
	}) {
		t.Fatal("expected request_validation entity for cleaned_data access")
	}
}

func TestGHR_Django_ModelForm(t *testing.T) {
	src := `from django import forms
from myapp.models import Post

class PostForm(forms.ModelForm):
    class Meta:
        model = Post
        fields = ["title", "body"]
`
	ents := extract(t, ghrExtractor, src)

	// ModelForm → dto_extraction
	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":    "django",
		"pattern_type": "request_dto",
		"dto_type":     "PostForm",
	}) {
		t.Fatal("expected dto_extraction entity for PostForm ModelForm class")
	}
}

// ============================================================================
// Django DRF — request_validation
// ============================================================================

func TestGHR_DRF_RequestData(t *testing.T) {
	src := `from django.http import HttpResponse
from rest_framework.views import APIView
from rest_framework.response import Response

class UserView(APIView):
    def post(self, request):
        data = request.data
        return Response({"ok": True})
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":       "django",
		"pattern_type":    "request_validation",
		"validation_kind": "drf_request_data",
	}) {
		t.Fatal("expected request_validation entity for DRF request.data")
	}
}

func TestGHR_DRF_ValidatedData(t *testing.T) {
	src := `from django.http import HttpResponse
from rest_framework.views import APIView
from rest_framework.response import Response

class OrderView(APIView):
    def post(self, request):
        if order_serializer.is_valid():
            data = order_serializer.validated_data
            return Response({"ok": True})
        return Response({}, status=400)
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":       "django",
		"pattern_type":    "request_validation",
		"validation_kind": "drf_serializer_is_valid",
	}) {
		t.Fatal("expected request_validation entity for serializer.is_valid()")
	}
}

// ============================================================================
// Marshmallow schema.load() — dto_extraction
// ============================================================================

func TestGHR_Marshmallow_SchemaLoad(t *testing.T) {
	src := `from sanic import Sanic
from sanic.response import json as sanic_json
import marshmallow as ma

class UserSchema(ma.Schema):
    name = ma.fields.Str()

user_schema = UserSchema()
app = Sanic("test")

@app.post("/users")
async def create_user(request):
    user_data = user_schema.load(request.json)
    return sanic_json({"ok": True})
`
	ents := extract(t, ghrExtractor, src)

	dtoCount := countGHREntities(ents, "SCOPE.Pattern", map[string]string{
		"pattern_type": "request_dto",
		"framework":    "sanic",
	})
	if dtoCount == 0 {
		t.Fatal("expected at least one dto_extraction entity for marshmallow schema.load()")
	}
}

// ============================================================================
// Bottle — dto_extraction
// ============================================================================

func TestGHR_Bottle_RouteHandler(t *testing.T) {
	src := `import bottle
from bottle import request, response
from pydantic import BaseModel

app = bottle.Bottle()

class ItemRequest(BaseModel):
    name: str
    price: float

@app.post("/items")
def create_item():
    body = request.json
    item = ItemRequest.model_validate(body)
    return {"id": 1}
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":    "bottle",
		"pattern_type": "request_validation",
	}) {
		t.Fatal("expected request_validation entity for bottle route handler")
	}
}

// ============================================================================
// CherryPy — dto_extraction via @expose
// ============================================================================

func TestGHR_CherryPy_ExposedHandler(t *testing.T) {
	src := `import cherrypy
from pydantic import BaseModel

class UserRequest(BaseModel):
    name: str
    role: str

class UserController:
    @cherrypy.expose
    @cherrypy.tools.json_in()
    def create(self):
        data = cherrypy.request.json
        user = UserRequest.model_validate(data)
        return {"id": 1}
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":    "cherrypy",
		"pattern_type": "request_validation",
	}) {
		t.Fatal("expected request_validation entity for cherrypy @expose handler")
	}
}

// ============================================================================
// Quart — dto_extraction (async Flask-like)
// ============================================================================

func TestGHR_Quart_RouteHandler(t *testing.T) {
	src := `from quart import Quart, request, jsonify
from pydantic import BaseModel

app = Quart(__name__)

class CreateRequest(BaseModel):
    title: str

@app.post("/items")
async def create_item():
    data = await request.get_json()
    item = CreateRequest.model_validate(data)
    return jsonify({"id": 1})
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":    "quart",
		"pattern_type": "request_validation",
	}) {
		t.Fatal("expected request_validation entity for quart route handler")
	}
}

// ============================================================================
// Robyn — dto_extraction
// ============================================================================

func TestGHR_Robyn_RouteHandler(t *testing.T) {
	src := `from robyn import Robyn, Request
from pydantic import BaseModel

app = Robyn(__file__)

class PostRequest(BaseModel):
    content: str

@app.post("/posts")
async def create_post(request: Request):
    body = request.json()
    post = PostRequest.model_validate(body)
    return {"id": 1}
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":    "robyn",
		"pattern_type": "request_validation",
	}) {
		t.Fatal("expected request_validation entity for robyn route handler")
	}
}

// ============================================================================
// Strawberry-GraphQL — dto_extraction
// ============================================================================

func TestGHR_Strawberry_MutationInput(t *testing.T) {
	src := `import strawberry
from pydantic import BaseModel

@strawberry.input
class CreatePostInput:
    title: str
    body: str

@strawberry.type
class Mutation:
    @strawberry.mutation
    def create_post(self, input: CreatePostInput) -> str:
        return input.title
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "create_post:accepts:CreatePostInput", "SCOPE.Operation", map[string]string{
		"framework":    "strawberry-graphql",
		"pattern_type": "accepts_input",
		"dto_type":     "CreatePostInput",
	}) {
		t.Fatal("expected accepts_input entity for strawberry mutation input param")
	}
}

// ============================================================================
// Pyramid — dto_extraction via @view_config
// ============================================================================

func TestGHR_Pyramid_ViewConfig(t *testing.T) {
	src := `from pyramid.view import view_config
from pyramid.response import Response
from pydantic import BaseModel

class CreateUserRequest(BaseModel):
    username: str
    email: str

@view_config(route_name="create_user", request_method="POST", renderer="json")
def create_user_view(request):
    user = CreateUserRequest.model_validate(request.json_body)
    return {"id": 1}
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":    "pyramid",
		"pattern_type": "request_validation",
	}) {
		t.Fatal("expected request_validation entity for pyramid @view_config handler")
	}
}

// ============================================================================
// Hug — dto_extraction
// ============================================================================

func TestGHR_Hug_RouteHandler(t *testing.T) {
	src := `import hug
from pydantic import BaseModel

class ItemRequest(BaseModel):
    name: str
    price: float

@hug.post("/items")
def create_item(body: dict):
    item = ItemRequest.model_validate(body)
    return {"id": 1}
`
	ents := extract(t, ghrExtractor, src)

	if !hasGHREntity(ents, "", "SCOPE.Pattern", map[string]string{
		"framework":    "hug",
		"pattern_type": "request_validation",
	}) {
		t.Fatal("expected request_validation entity for hug route handler")
	}
}

// ============================================================================
// Full fixture smoke test
// The fixture mixes multiple frameworks in one file, so only the first
// detected framework is labelled.  The test validates that entities ARE
// produced (not zero) and that via-tags are correct — multi-framework files
// are edge-cases; realistic production files import exactly one framework.
// ============================================================================

func TestGHR_FullFixture_SmokeTest(t *testing.T) {
	src := fixtureGHR(t, "http_reqresp_generic_fixture.py")
	ents := extract(t, ghrExtractor, src)

	if len(ents) == 0 {
		t.Fatal("expected entities from full fixture, got 0")
	}

	// Should detect multiple dto or request_validation patterns
	patternCount := countGHREntities(ents, "SCOPE.Pattern", map[string]string{})
	if patternCount < 2 {
		t.Fatalf("expected at least 2 SCOPE.Pattern entities in full fixture, got %d", patternCount)
	}

	// Verify via tags are present and valid
	for _, e := range ents {
		via := e.Props["via"]
		if via != "" && via != "dto_extraction" && via != "request_validation" {
			t.Errorf("unexpected via=%q on entity %q", via, e.Name)
		}
	}

	_ = strings.Contains // ensure strings imported
}

// ============================================================================
// Via-tag integrity checks
// ============================================================================

func TestGHR_ViaTag_DtoExtraction(t *testing.T) {
	src := `from litestar import post
from pydantic import BaseModel

class CreateRequest(BaseModel):
    name: str

@post("/create")
async def create(data: CreateRequest) -> dict:
    return {}
`
	ents := extract(t, ghrExtractor, src)
	for _, e := range ents {
		if e.Props["pattern_type"] == "request_dto" && e.Props["via"] != "dto_extraction" {
			t.Errorf("dto entity %q: expected via=dto_extraction, got %q", e.Name, e.Props["via"])
		}
		if e.Props["pattern_type"] == "accepts_input" && e.Props["via"] != "dto_extraction" {
			t.Errorf("accepts_input entity %q: expected via=dto_extraction, got %q", e.Name, e.Props["via"])
		}
	}
}

func TestGHR_ViaTag_RequestValidation(t *testing.T) {
	src := `from sanic import Sanic
from pydantic import BaseModel

app = Sanic("t")

class Req(BaseModel):
    name: str

@app.post("/x")
async def handler(request):
    obj = Req.model_validate(request.json)
    return {}
`
	ents := extract(t, ghrExtractor, src)
	for _, e := range ents {
		if e.Props["pattern_type"] == "request_validation" && e.Props["via"] != "request_validation" {
			t.Errorf("request_validation entity %q: expected via=request_validation, got %q", e.Name, e.Props["via"])
		}
	}
}
