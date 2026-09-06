"""ORM models for the invoicing service.

The docstring is load-bearing for #6927: it means NOTHING in this file sits at
byte 0, which is the only position the pre-fix `^class` anchors could reach.
"""

from sqlalchemy import Column, ForeignKey, Integer, String
from sqlalchemy.orm import DeclarativeBase, relationship


class Base(DeclarativeBase):
    """Declarative base. Second construct in the file, not the first."""


class Customer(Base):
    __tablename__ = "customers"

    id = Column(Integer, primary_key=True)
    name = Column(String)
    invoices = relationship("Invoice")


# A retired model, left as a comment. `#` opens the line, so the line anchor is
# the only thing keeping `LegacyInvoice` out of the graph.
# class LegacyInvoice(Base):
#     __tablename__ = "legacy_invoices"


class Invoice(Base):
    """Declared LAST — the furthest position in the file from start-of-text."""

    __tablename__ = "invoices"

    id = Column(Integer, primary_key=True)
    customer_id = Column(Integer, ForeignKey("customers.id"))
