"""Declarative registry, plus the two-name base list the Model capture must reject.

`Registry` exists because `models.py` cannot grade the DeclarativeBase pattern:
there both class patterns match `class Base(DeclarativeBase):` and the
downstream class-shadow fold collapses the result, so removing `(?m)` from the
DeclarativeBase pattern changed NOTHING observable — measured, identical entity
count and identical recall. `Registry`'s base list holds two names, which the
Model pattern's `\(\w+\)` capture does not accept, so its Schema entity has
exactly one producer.

`SessionFactory` grades that capture in the FORBIDDEN direction, and it is a
separate class on purpose: widening `\(\w+\)` to `\([^)]*\)` would mint a
Model for `Registry` too, but `Registry` already has a Schema entity for the
same class and the fold absorbs one of the pair, so the over-fire showed up
only as an accidental recall loss with zero forbidden hits. `SessionFactory`
has no second producer and no DeclarativeBase in its bases, so the same
widening lands where it can be seen.
"""

from sqlalchemy.orm import DeclarativeBase, MappedAsDataclass


class LoggingMixin:
    pass


class BaseFactory:
    pass


class Registry(MappedAsDataclass, DeclarativeBase):
    pass


class SessionFactory(LoggingMixin, BaseFactory):
    """Also a two-name base list, and NOT a declarative base.

    Registry alone cannot grade the Model pattern's `\(\w+\)` capture: it
    matches the DeclarativeBase pattern too, so widening `\(\w+\)` to
    `\([^)]*\)` mints a second entity for the same class and the class-shadow
    fold absorbs one of them — the over-fire surfaces as an accidental RECALL
    loss with zero forbidden hits. This class has no second producer, so a
    widened capture shows up where it belongs, as a false Model.
    """
