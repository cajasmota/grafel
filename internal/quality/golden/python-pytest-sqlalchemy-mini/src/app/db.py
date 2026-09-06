"""Declarative registry.

A SECOND declarative base, declared with a mixin so its base list holds two
names. That matters: the Model pattern requires a single simple base
(`\(\w+\)`), so it cannot match this line, and the Schema entity here is
therefore produced by the DeclarativeBase pattern and nothing else. In
models.py the two patterns overlap on `class Base(DeclarativeBase):` and the
downstream class-shadow fold collapses the result, which leaves that pattern
unobservable there.
"""

from sqlalchemy.orm import DeclarativeBase, MappedAsDataclass


class Registry(MappedAsDataclass, DeclarativeBase):
    pass
