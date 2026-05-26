from enum import Enum
from pydantic import BaseModel


class OrderStatus(str, Enum):
    PENDING = "PENDING"
    PAID = "PAID"
    SHIPPED = "SHIPPED"
    DELIVERED = "DELIVERED"
    CANCELLED = "CANCELLED"


class OrderItem(BaseModel):
    sku: str
    quantity: int
    unit_price_cents: int


# Shared Order model — referenced by orders, workers, analytics services.
class Order(BaseModel):
    id: str
    user_id: str
    status: OrderStatus = OrderStatus.PENDING
    total_cents: int
    items: list[OrderItem] = []


class User(BaseModel):
    id: str
    email: str
    roles: list[str] = []
