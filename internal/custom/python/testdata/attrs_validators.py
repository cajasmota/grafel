"""Dependency-free proving fixture for the attrs extractor (issue #2985).

Exercises attrs class declarations (@attr.s, @attr.define, @define),
attribute declarations (attrib / field), @<field>.validator decorator
validators, validator= kwarg, and converter= for type coercion.
"""
import attr
import attrs
from attrs import define, field, Factory


# Classic @attr.s style
@attr.s
class Address:
    street = attr.ib(validator=attr.validators.instance_of(str))
    city = attr.ib(validator=attr.validators.instance_of(str))
    zip_code = attr.ib(default="")


# attrs.define style (attrs ≥ 21)
@attrs.define
class User:
    name: str = field()
    email: str = field()
    age: int = field(default=0, validator=attr.validators.instance_of(int))
    address: Address = field(factory=Address)

    @email.validator
    def validate_email(self, attribute, value):
        if "@" not in value:
            raise ValueError(f"Invalid email: {value}")

    @age.validator
    def validate_age(self, attribute, value):
        if value < 0:
            raise ValueError("Age must be non-negative")


# @define shorthand
@define
class Order:
    amount: float = field(converter=float)
    currency: str = field(default="USD")
    user_id: int = field(factory=int)

    @amount.validator
    def validate_amount(self, attribute, value):
        if value <= 0:
            raise ValueError("Amount must be positive")


# Nested attrs class (nested_model_extraction evidence)
@attr.s(auto_attribs=True)
class Invoice:
    order: Order = attr.ib()
    billing_address: Address = attr.ib()
    total: float = attr.ib(default=0.0, converter=float)
