"""Plain domain code. No SQLAlchemy anywhere in this file.

`^class X(Base):` describes plain Python syntax, not an ORM construct, and the
detector resolves rule sets by file LANGUAGE alone. So without a framework gate
the widened Model pattern would type every class below as a `Model`.
"""

import json


class ConfigError(Exception):
    pass


class Serializer(object):
    def dump(self, value):
        return json.dumps(value)
