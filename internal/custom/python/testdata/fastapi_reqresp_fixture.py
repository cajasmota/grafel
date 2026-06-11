"""
FastAPI fixture exercising fastapi_reqresp.go extraction:
  - Pydantic BaseModel DTO parameters (ACCEPTS_INPUT)
  - response_model= kwarg (RETURNS)
  - Return type annotation (RETURNS)
  - Depends() injection (skipped — not a body DTO)

Also exercises fastapi.go:
  - @app.middleware("http")
  - Depends() parameters
"""
from fastapi import FastAPI, APIRouter, Depends, BackgroundTasks
from pydantic import BaseModel
from typing import Optional, List

app = FastAPI()
router = APIRouter(prefix="/orders", tags=["orders"])


# --- Pydantic DTOs ---

class CreateOrderRequest(BaseModel):
    sku: str
    quantity: int
    user_id: str


class OrderResponse(BaseModel):
    order_id: str
    status: str
    total_cents: int


class UpdateOrderRequest(BaseModel):
    status: str
    notes: Optional[str] = None


# --- Query-model DTO (#4476: Depends() request shape) ---

class OrderFilterParams(BaseModel):
    status: Optional[str] = None
    min_total_cents: Optional[int] = None


# --- Dependency ---

def get_current_user(token: str = ""):
    return {"user": token}


# --- Routes exercising ACCEPTS_INPUT / RETURNS ---

@app.post("/orders", response_model=OrderResponse)
async def create_order(payload: CreateOrderRequest, background_tasks: BackgroundTasks):
    background_tasks.add_task(notify_user, payload.user_id)
    return {"order_id": "123", "status": "pending", "total_cents": 0}


@router.put("/{order_id}")
async def update_order(order_id: str, body: UpdateOrderRequest, user=Depends(get_current_user)) -> OrderResponse:
    return {"order_id": order_id, "status": body.status, "total_cents": 0}


@router.get("/{order_id}", response_model=OrderResponse)
async def get_order(order_id: str):
    return {"order_id": order_id, "status": "pending", "total_cents": 0}


# #4476: a Pydantic model bound as a query model via Depends() — the FastAPI
# analog of the NestJS @Query() DTO. Must get an ACCEPTS_INPUT edge; the
# get_current_user provider dependency above must NOT.
@router.get("", response_model=OrderResponse)
async def list_orders(filters: OrderFilterParams = Depends(), user=Depends(get_current_user)):
    return {"order_id": "1", "status": "pending", "total_cents": 0}


# --- Middleware ---

@app.middleware("http")
async def add_request_id(request, call_next):
    response = await call_next(request)
    return response


# --- Lifecycle ---

@app.on_event("startup")
async def on_startup():
    pass


@app.on_event("shutdown")
async def on_shutdown():
    pass


# --- WebSocket ---

@app.websocket("/ws/{client_id}")
async def websocket_endpoint(websocket):
    await websocket.accept()


def notify_user(user_id: str):
    pass
