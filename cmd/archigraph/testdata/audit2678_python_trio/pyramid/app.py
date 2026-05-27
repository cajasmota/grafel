"""Pyramid attribution fixture for issue #2690.

URL configuration is two-step: config.add_route names a URL, then
@view_config(route_name=...) on a handler binds the verb. The
synthesizer pairs same-file add_route + view_config and emits one
http_endpoint_definition per (verb, path) tuple, attributed to the
handler `def` line.

Line numbers below are stable — keep the integration test expectations in
sync with this file's layout.
"""

from pyramid.config import Configurator
from pyramid.view import view_config


@view_config(route_name="health", request_method="GET")
def health(request):
    return {"status": "ok"}


@view_config(route_name="get_user", request_method="GET")
def get_user(request):
    return {}


@view_config(route_name="create_user", request_method="POST")
def create_user(request):
    return {}


def make_app():
    config = Configurator()
    config.add_route("health", "/health")
    config.add_route("get_user", "/users/{user_id}")
    config.add_route("create_user", "/users")
    config.scan()
    return config.make_wsgi_app()
