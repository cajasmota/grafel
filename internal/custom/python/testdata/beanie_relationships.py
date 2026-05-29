"""Beanie ODM relationship fixture — issue #3070."""
from typing import List, Optional
from beanie import Document, Link, BackLink
from pydantic import Field


class Category(Document):
    name: str

    class Settings:
        name = "categories"


class Product(Document):
    title: str
    category: Link[Category]
    related: List[Link["Product"]] = []

    class Settings:
        name = "products"


class Order(Document):
    items: List[Link[Product]]
    category_ref: Optional[Link[Category]] = None

    class Settings:
        name = "orders"


# fetch_links usage for lazy_loading_recognition
async def get_order(order_id: str) -> Order:
    return await Order.get(order_id, fetch_links=True)
