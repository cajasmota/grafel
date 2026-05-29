<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# python

**Frameworks**: 21 · **Tools**: 15 · **ORMs**: 17 · **Other**: 6

Back to [summary](../summary.md).

> Group columns show `glyph covered/applicable`: **covered** = capabilities with extraction (✅ full + ⚠️ partial), **applicable** = covered + ❌ missing (not-applicable cells are excluded). The glyph is the group's worst cell — ✅ all full · ⚠️ some heuristic/partial · ❌ some missing. So `20/20 ⚠️` means every applicable capability is extracted, some only heuristically.

## Frameworks


### Backend HTTP

| Name | Routing | Auth | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|---|
| [Bottle](../detail/lang.python.framework.bottle.md) | ✅ 3/3 | ⚠️ 1/1 | ✅ 4/4 | ⚠️ 1/1 | ❌ 19/20 | ❌ 5/7 | |
| [CherryPy](../detail/lang.python.framework.cherrypy.md) | ✅ 3/3 | ⚠️ 1/1 | ✅ 4/4 | ⚠️ 1/1 | ❌ 19/20 | ❌ 5/7 | |
| [Django](../detail/lang.python.framework.django.md) | ✅ 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | ❌ 19/20 | ❌ 6/7 | |
| [Django REST Framework](../detail/lang.python.framework.django-drf.md) | ✅ 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | ❌ 20/21 | ❌ 7/8 | |
| [Falcon](../detail/lang.python.framework.falcon.md) | ✅ 3/3 | ⚠️ 1/1 | ✅ 4/4 | ⚠️ 1/1 | ❌ 19/20 | ❌ 5/7 | |
| [FastAPI](../detail/lang.python.framework.fastapi.md) | ✅ 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | ❌ 19/20 | ⚠️ 7/7 | |
| [Flask](../detail/lang.python.framework.flask.md) | ✅ 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | ❌ 19/20 | ⚠️ 7/7 | |
| [Hug](../detail/lang.python.framework.hug.md) | ✅ 3/3 | ⚠️ 1/1 | ✅ 4/4 | ⚠️ 1/1 | ❌ 19/20 | ❌ 5/7 | |
| [Litestar](../detail/lang.python.framework.litestar.md) | ✅ 3/3 | ⚠️ 1/1 | ✅ 4/4 | ✅ 1/1 | ❌ 19/20 | ❌ 5/7 | |
| [Pyramid](../detail/lang.python.framework.pyramid.md) | ✅ 3/3 | ⚠️ 1/1 | ✅ 4/4 | ⚠️ 1/1 | ❌ 19/20 | ❌ 5/7 | |
| [Quart](../detail/lang.python.framework.quart.md) | ✅ 3/3 | ⚠️ 1/1 | ✅ 4/4 | ✅ 1/1 | ❌ 19/20 | ❌ 5/7 | |
| [Robyn](../detail/lang.python.framework.robyn.md) | ✅ 3/3 | ⚠️ 1/1 | ✅ 4/4 | ✅ 1/1 | ❌ 19/20 | ❌ 5/7 | |
| [Sanic](../detail/lang.python.framework.sanic.md) | ✅ 3/3 | ⚠️ 1/1 | ✅ 4/4 | ✅ 1/1 | ❌ 19/20 | ❌ 5/7 | |
| [Starlette](../detail/lang.python.framework.starlette.md) | ✅ 3/3 | ⚠️ 1/1 | ✅ 4/4 | ✅ 1/1 | ❌ 19/20 | ❌ 5/7 | |
| [Strawberry GraphQL](../detail/lang.python.framework.strawberry-graphql.md) | ❌ 2/3 | ⚠️ 1/1 | ✅ 4/4 | ✅ 1/1 | ❌ 19/20 | ❌ 5/7 | |
| [Tornado](../detail/lang.python.framework.tornado.md) | ✅ 3/3 | ⚠️ 1/1 | ✅ 4/4 | ⚠️ 1/1 | ❌ 19/20 | ❌ 5/7 | |
| [aiohttp](../detail/lang.python.framework.aiohttp.md) | ✅ 3/3 | ⚠️ 1/1 | ✅ 4/4 | ✅ 1/1 | ❌ 19/20 | ❌ 5/7 | |


### AI Integration

| Name | Other capabilities | Notes |
|---|---|---|
| [LangChain (LLM agent framework)](../detail/lang.python.framework.langchain.md) | ❌ 0/3 | |


### Task Queue

| Name | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|
| [Celery (task queue)](../detail/lang.python.framework.celery.md) | ⚠️ 1/1 | ❌ 20/21 | ❌ 4/6 | |
| [Dramatiq (task queue)](../detail/lang.python.framework.dramatiq.md) | ⚠️ 1/1 | ❌ 20/21 | ❌ 1/6 | |
| [RQ (Redis Queue)](../detail/lang.python.framework.rq.md) | ⚠️ 1/1 | ❌ 20/21 | ❌ 2/6 | |


## Tools

| Name | Dependency graph | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|
| [Flit](../detail/build.flit.md) | ❌ | — | — | ❌ | |
| [Hatch](../detail/build.hatch.md) | ⚠️ | — | — | ⚠️ | |
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
| [unittest (stdlib)](../detail/test.unittest.md) | ✅ | — | — | ✅ | |
| [uv (Astral)](../detail/build.uv.md) | ✅ | — | — | ✅ | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Alembic (migration tool)](../detail/lang.python.orm.alembic.md) | ❌ 1/6 | |
| [Beanie (async MongoDB ODM)](../detail/lang.python.orm.beanie.md) | ❌ 5/6 | |
| [Django ORM](../detail/lang.python.orm.django.md) | ✅ 8/8 | |
| [MongoEngine](../detail/lang.python.orm.mongoengine.md) | ❌ 5/6 | |
| [MySQL (PyMySQL / mysqlclient)](../detail/lang.python.driver.mysql.md) | ❌ 1/2 | |
| [Peewee](../detail/lang.python.orm.peewee.md) | ❌ 5/8 | |
| [Pony ORM](../detail/lang.python.orm.pony.md) | ❌ 5/8 | |
| [SQLAlchemy](../detail/lang.python.orm.sqlalchemy.md) | ✅ 8/8 | |
| [SQLModel](../detail/lang.python.orm.sqlmodel.md) | ⚠️ 8/8 | |
| [Tortoise ORM](../detail/lang.python.orm.tortoise.md) | ❌ 6/8 | |
| [boto3 DynamoDB](../detail/lang.python.driver.dynamodb.md) | ⚠️ 1/1 | |
| [cassandra-driver](../detail/lang.python.driver.cassandra.md) | ⚠️ 1/1 | |
| [elasticsearch-py](../detail/lang.python.driver.elastic.md) | ⚠️ 1/1 | |
| [neo4j (Python driver)](../detail/lang.python.driver.neo4j.md) | ⚠️ 1/1 | |
| [psycopg / asyncpg (PostgreSQL drivers)](../detail/lang.python.driver.postgres.md) | ❌ 1/2 | |
| [redis-py](../detail/lang.python.driver.redis.md) | ✅ 1/1 | |
| [sqlite3 (stdlib)](../detail/lang.python.driver.sqlite.md) | ❌ 1/2 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Celery (Python task queue)](../detail/msg.celery.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Django signals (intra-repo pub/sub)](../detail/msg.django-signals.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Dramatiq (Python task queue)](../detail/msg.dramatiq.md) | [message_broker](../by-category/message_broker.md) | ✅ | |

### Validation

| Name | Testing | Other capabilities | Notes |
|---|---|---|---|
| [Pydantic](../detail/lang.python.validation.pydantic.md) | ⚠️ 1/1 | ⚠️ 5/5 | |
| [attrs](../detail/lang.python.validation.attrs.md) | ⚠️ 1/1 | ❌ 3/5 | |
| [marshmallow](../detail/lang.python.validation.marshmallow.md) | ⚠️ 1/1 | ❌ 4/5 | |
