"""
Fixture for http_reqresp_generic.go — exercises:
  - Pydantic BaseModel DTO params in various framework handlers (dto_extraction)
  - Django Form.is_valid() + cleaned_data (request_validation)
  - DRF request.data / .validated_data / .is_valid() (request_validation)
  - Pydantic model_validate / parse_obj calls (request_validation)
  - Marshmallow schema.load() in handler body (dto_extraction)
"""

# ─── Starlette fixture ────────────────────────────────────────────────────────
from starlette.applications import Starlette
from starlette.routing import Route
from starlette.requests import Request
from starlette.responses import JSONResponse
from pydantic import BaseModel

class CreateItemRequest(BaseModel):
    name: str
    price: float

class UpdateItemRequest(BaseModel):
    name: str

async def create_item(request: Request):
    payload = CreateItemRequest.model_validate(await request.json())
    return JSONResponse({"id": 1})

async def update_item(request: Request):
    body = await request.json()
    return JSONResponse({})


# ─── Sanic fixture ────────────────────────────────────────────────────────────
from sanic import Sanic, Blueprint
from sanic.response import json as sanic_json
from pydantic import BaseModel as PydanticModel

app_sanic = Sanic("test")

class OrderRequest(PydanticModel):
    item_id: int
    quantity: int

@app_sanic.post("/orders")
async def create_order(request):
    order = OrderRequest.parse_obj(request.json)
    return sanic_json({"id": 1})

@app_sanic.get("/orders/<id>")
async def get_order(request, id: int):
    return sanic_json({"id": id})


# ─── Django Form fixture ──────────────────────────────────────────────────────
from django.http import HttpResponse
from django import forms

class ContactForm(forms.Form):
    name = forms.CharField(max_length=100)
    email = forms.EmailField()

class ContactModelForm(forms.ModelForm):
    class Meta:
        model = None
        fields = ["name", "email"]

def contact_view(request):
    if request.method == "POST":
        form = ContactForm(request.POST)
        if form.is_valid():
            name = form.cleaned_data["name"]
            return HttpResponse("OK")
    return HttpResponse("form")


# ─── DRF fixture ─────────────────────────────────────────────────────────────
from rest_framework.views import APIView
from rest_framework.response import Response

class UserView(APIView):
    def post(self, request):
        data = request.data
        serializer_data = serializer.validated_data
        return Response({"ok": True})

    def put(self, request):
        if serializer.is_valid():
            return Response({"ok": True})
        return Response({}, status=400)


# ─── Falcon fixture ───────────────────────────────────────────────────────────
import falcon
from pydantic import BaseModel

class ProductRequest(BaseModel):
    title: str
    price: float

class ProductResource:
    def on_post(self, req, resp):
        body = req.media
        product = ProductRequest.model_validate(body)
        resp.media = {"id": 1}

    def on_get(self, req, resp):
        resp.media = []


# ─── Tornado fixture ─────────────────────────────────────────────────────────
import tornado.web
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

    def get(self):
        self.write([])


# ─── Litestar fixture ─────────────────────────────────────────────────────────
from litestar import Litestar, post, get
from pydantic import BaseModel

class BookRequest(BaseModel):
    title: str
    author: str

@post("/books")
async def create_book(data: BookRequest) -> dict:
    return {"id": 1}

@get("/books/{book_id:int}")
async def get_book(book_id: int) -> dict:
    return {"id": book_id}
