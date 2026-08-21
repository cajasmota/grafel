"""Dependency providers for the things API."""


def get_db():
    """Yield a database handle."""
    return {"conn": "sqlite://"}
