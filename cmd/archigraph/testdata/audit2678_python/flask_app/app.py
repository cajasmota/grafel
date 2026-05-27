"""Flask attribution fixture for issue #2678.

Endpoints declared via @app.route / @bp.<verb>; the handler def line
recorded on each http_endpoint_definition must point at the `def` line,
not the decorator line. Line numbers below are stable — adjust the
integration test's expected lines if this file changes.
"""

from flask import Flask, Blueprint

app = Flask(__name__)
bp = Blueprint("api", __name__)


@app.route("/health")
def health():
    return "ok"


@bp.get("/users/<int:user_id>")
def get_user(user_id):
    return {}


@bp.post("/users")
def create_user():
    return {}
