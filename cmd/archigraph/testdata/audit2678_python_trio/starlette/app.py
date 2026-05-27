"""Starlette attribution fixture for issue #2690.

Endpoints are registered as a flat Route(...) list. The handler functions
live in the same file in this fixture so the synthesizer can stamp the
def line directly; cross-file handler attribution is covered separately
by the resolver-rebind path exercised in the broader integration suite.

Line numbers below are stable — keep the integration test expectations in
sync with this file's layout.
"""

from starlette.applications import Starlette
from starlette.routing import Route


def health(request):
    return {"status": "ok"}


def get_user(request):
    return {}


async def create_item(request):
    return {}


routes = [
    Route("/health", endpoint=health, methods=["GET"]),
    Route("/users/{user_id}", endpoint=get_user, methods=["GET"]),
    Route("/items", endpoint=create_item, methods=["POST"]),
]

app = Starlette(routes=routes)
