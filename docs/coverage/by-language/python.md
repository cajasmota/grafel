<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# python

**Frameworks**: 20 · **Tools**: 15 · **ORMs**: 17 · **Other**: 3

Back to [summary](../summary.md).

## Frameworks


### Backend HTTP

| Name | Routing | Security | Validation | Middleware | Testing | Observability | Data | Substrate | Notes |
|---|---|---|---|---|---|---|---|---|---|
| [Bottle](../detail/lang.python.framework.bottle.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Celery (task queue)](../detail/lang.python.framework.celery.md) | ✅ 1/2 | — 0/1 | — | — 0/1 | — | — | — | ⚠️ 5/6 | |
| [CherryPy](../detail/lang.python.framework.cherrypy.md) | ❌ 0/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Django](../detail/lang.python.framework.django.md) | ✅ 2/2 | ⚠️ 0/1 | — | ⚠️ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Django REST Framework](../detail/lang.python.framework.django-drf.md) | ✅ 2/2 | ⚠️ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 7/16 | |
| [Dramatiq (task queue)](../detail/lang.python.framework.dramatiq.md) | ⚠️ 0/2 | — 0/1 | — | — 0/1 | — | — | — | ⚠️ 5/6 | |
| [Falcon](../detail/lang.python.framework.falcon.md) | ❌ 0/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [FastAPI](../detail/lang.python.framework.fastapi.md) | ✅ 2/2 | ⚠️ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Flask](../detail/lang.python.framework.flask.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Hug](../detail/lang.python.framework.hug.md) | ❌ 0/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Litestar](../detail/lang.python.framework.litestar.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Pyramid](../detail/lang.python.framework.pyramid.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Quart](../detail/lang.python.framework.quart.md) | ❌ 0/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Robyn](../detail/lang.python.framework.robyn.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Sanic](../detail/lang.python.framework.sanic.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Starlette](../detail/lang.python.framework.starlette.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Strawberry GraphQL](../detail/lang.python.framework.strawberry-graphql.md) | ⚠️ 0/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [Tornado](../detail/lang.python.framework.tornado.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |
| [aiohttp](../detail/lang.python.framework.aiohttp.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | ⚠️ 5/6 | |


### AI Integration

| Name | Prompts | Composition | Tracking | Notes |
|---|---|---|---|---|
| [LangChain (LLM agent framework)](../detail/lang.python.framework.langchain.md) | ❌ 0/1 | ❌ 0/2 | — | |


## Tools

| Name | Dependency Graph | Lockfile Parsing | Manifest Parsing | Target Extraction | Notes |
|---|---|---|---|---|---|
| [Flit](../detail/build.flit.md) | ❌ | — | — | ❌ | |
| [Hatch](../detail/build.hatch.md) | ❌ | — | — | ❌ | |
| [Hypothesis (property tests)](../detail/test.hypothesis.md) | ❌ | — | — | ❌ | |
| [Pipenv](../detail/build.pipenv.md) | ⚠️ | — | — | ⚠️ | |
| [Pipfile / Pipfile.lock](../detail/pkg.pipfile.md) | — | ❌ | ❌ | — | |
| [Poetry](../detail/build.poetry.md) | ✅ | — | — | ✅ | |
| [doctest (stdlib)](../detail/test.doctest.md) | ❌ | — | — | ❌ | |
| [nose2](../detail/test.nose2.md) | ❌ | — | — | ❌ | |
| [pip (requirements.txt)](../detail/build.pip.md) | ✅ | — | — | ✅ | |
| [pyproject.toml](../detail/pkg.pyproject.md) | — | ❌ | ✅ | — | |
| [pytest](../detail/test.pytest.md) | ✅ | — | — | ✅ | |
| [requirements.txt](../detail/pkg.requirements.md) | — | — | ✅ | — | |
| [setuptools / setup.py](../detail/build.setuptools.md) | ⚠️ | — | — | ⚠️ | |
| [unittest (stdlib)](../detail/test.unittest.md) | ⚠️ | — | — | ⚠️ | |
| [uv (Astral)](../detail/build.uv.md) | ⚠️ | — | — | ⚠️ | |

## ORMs

| Name | Migration Parsing | Model Extraction | Query Attribution | Notes |
|---|---|---|---|---|
| [Alembic (migration tool)](../detail/lang.python.orm.alembic.md) | ⚠️ | — | — | |
| [Beanie (async MongoDB ODM)](../detail/lang.python.orm.beanie.md) | — | ⚠️ | ⚠️ | |
| [Django ORM](../detail/lang.python.orm.django.md) | ✅ | ✅ | ✅ | |
| [MongoEngine](../detail/lang.python.orm.mongoengine.md) | — | ⚠️ | ⚠️ | |
| [MySQL (PyMySQL / mysqlclient)](../detail/lang.python.driver.mysql.md) | — | — | ⚠️ | |
| [Peewee](../detail/lang.python.orm.peewee.md) | ❌ | ⚠️ | ⚠️ | |
| [Pony ORM](../detail/lang.python.orm.pony.md) | ❌ | ⚠️ | ⚠️ | |
| [SQLAlchemy](../detail/lang.python.orm.sqlalchemy.md) | ⚠️ | ✅ | ✅ | |
| [SQLModel](../detail/lang.python.orm.sqlmodel.md) | ❌ | ✅ | ✅ | |
| [Tortoise ORM](../detail/lang.python.orm.tortoise.md) | ❌ | ✅ | ⚠️ | |
| [boto3 DynamoDB](../detail/lang.python.driver.dynamodb.md) | — | — | ⚠️ | |
| [cassandra-driver](../detail/lang.python.driver.cassandra.md) | — | — | ⚠️ | |
| [elasticsearch-py](../detail/lang.python.driver.elastic.md) | — | — | ⚠️ | |
| [neo4j (Python driver)](../detail/lang.python.driver.neo4j.md) | — | — | ⚠️ | |
| [psycopg / asyncpg (PostgreSQL drivers)](../detail/lang.python.driver.postgres.md) | — | — | ⚠️ | |
| [redis-py](../detail/lang.python.driver.redis.md) | — | — | ⚠️ | |
| [sqlite3 (stdlib)](../detail/lang.python.driver.sqlite.md) | — | — | ⚠️ | |

## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Celery (Python task queue)](../detail/msg.celery.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Django signals (intra-repo pub/sub)](../detail/msg.django-signals.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [Dramatiq (Python task queue)](../detail/msg.dramatiq.md) | [message_broker](../by-category/message_broker.md) | ❌ | |
