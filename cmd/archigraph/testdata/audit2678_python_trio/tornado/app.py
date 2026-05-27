"""Tornado attribution fixture for issue #2690.

RequestHandler subclasses declare HTTP verbs as same-named methods. The
synthesizer enumerates the verbs from the class body and emits one
http_endpoint_definition per (verb, path) tuple, attributed to the
method's `def` line.

Line numbers below are stable — keep the integration test expectations in
sync with this file's layout.
"""

from tornado.web import Application, RequestHandler


class HealthHandler(RequestHandler):
    def get(self):
        self.write({"status": "ok"})


class UsersHandler(RequestHandler):
    def get(self, user_id):
        self.write({})

    def post(self):
        self.write({})


def make_app():
    return Application([
        (r"/health", HealthHandler),
        (r"/users/(?P<user_id>[0-9]+)", UsersHandler),
        (r"/users", UsersHandler),
    ])
