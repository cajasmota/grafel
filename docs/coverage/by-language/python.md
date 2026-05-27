<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# python

**Frameworks**: 12 · **Tools**: 3 · **ORMs**: 6 · **Other**: 4

Back to [summary](../summary.md).

## Frameworks

| Name | auth_coverage | endpoint_synthesis | handler_attribution | middleware_coverage | Notes |
|---|---|---|---|---|---|
| [Bottle](../detail/lang.python.framework.bottle.md) | — | ❌ | — | — | |
| [Django (URLconf)](../detail/lang.python.framework.django.md) | ⚠️ | ✅ | ✅ | — | |
| [Django REST Framework](../detail/lang.python.framework.django-drf.md) | ⚠️ | ✅ | ✅ | — | |
| [FastAPI](../detail/lang.python.framework.fastapi.md) | ❌ | ✅ | — | — | |
| [Flask](../detail/lang.python.framework.flask.md) | ❌ | ✅ | — | — | |
| [Litestar](../detail/lang.python.framework.litestar.md) | — | ❌ | — | — | |
| [Pyramid](../detail/lang.python.framework.pyramid.md) | — | ✅ | ✅ | — | |
| [Robyn](../detail/lang.python.framework.robyn.md) | — | ❌ | — | — | |
| [Sanic](../detail/lang.python.framework.sanic.md) | — | ❌ | — | — | |
| [Starlette](../detail/lang.python.framework.starlette.md) | — | ✅ | ✅ | — | |
| [Tornado](../detail/lang.python.framework.tornado.md) | — | ✅ | ✅ | — | |
| [aiohttp](../detail/lang.python.framework.aiohttp.md) | — | ❌ | — | — | |

## Tools

| Name | dependency_graph | lockfile_parsing | manifest_parsing | target_extraction | Notes |
|---|---|---|---|---|---|
| [Pipfile / Pipfile.lock](../detail/pkg.pipfile.md) | — | ❌ | ❌ | — | |
| [pyproject.toml](../detail/pkg.pyproject.md) | — | ❌ | ✅ | — | |
| [requirements.txt](../detail/pkg.requirements.md) | — | — | ✅ | — | |

## ORMs

| Name | migration_parsing | model_extraction | query_attribution | Notes |
|---|---|---|---|---|
| [Django ORM](../detail/lang.python.orm.django.md) | ✅ | ✅ | ✅ | |
| [MongoEngine](../detail/lang.python.orm.mongoengine.md) | — | ❌ | ❌ | |
| [Peewee](../detail/lang.python.orm.peewee.md) | — | ❌ | ✅ | |
| [SQLAlchemy](../detail/lang.python.orm.sqlalchemy.md) | ⚠️ | ✅ | ✅ | |
| [SQLModel](../detail/lang.python.orm.sqlmodel.md) | — | ❌ | ⚠️ | |
| [Tortoise ORM](../detail/lang.python.orm.tortoise.md) | — | ❌ | ✅ | |

## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Celery (Python task queue)](../detail/msg.celery.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Django signals (intra-repo pub/sub)](../detail/msg.django-signals.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [Dramatiq (Python task queue)](../detail/msg.dramatiq.md) | [message_broker](../by-category/message_broker.md) | ❌ | |
| [Python](../detail/lang.python.md) | [language](../by-category/language.md) | ❌ | |
