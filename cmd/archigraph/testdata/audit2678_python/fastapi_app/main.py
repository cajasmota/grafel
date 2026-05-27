"""FastAPI attribution fixture for issue #2678.

Endpoints are declared via @app.<verb> / @router.<verb>; the handler def
line recorded on each http_endpoint_definition must point at the `def`
line, not the decorator line. Line numbers are stable — keep the
integration test's expected lines in sync with the layout below.
"""

from fastapi import FastAPI, APIRouter

app = FastAPI()
router = APIRouter(prefix="/v1")


@app.get("/health")
def health():
    return {"status": "ok"}


@router.post("/items")
async def create_item():
    return {}


@app.delete("/users/{user_id}")
def delete_user(user_id: int):
    return None
