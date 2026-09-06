"""Invoice service tests.

Opens with a docstring and an import block — the shape every real pytest module
has, and the shape the pre-fix `^def test_` anchor could not see past.
"""

import pytest

from app.models import Customer


@pytest.fixture
def customer():
    return Customer(name="acme")


def test_creates_customer(customer):
    assert customer.name == "acme"


class TestInvoiceTotals:
    def test_sums_lines(self, customer):
        assert customer is not None


def test_rejects_negative_amount(customer):
    with pytest.raises(ValueError):
        raise ValueError("negative")
