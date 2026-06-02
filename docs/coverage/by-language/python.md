<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# python

**Frameworks**: 23 · **Tools**: 15 · **ORMs**: 18 · **Other**: 8

Back to [summary](../summary.md).

### Legend

Each group column shows `glyph covered/applicable` — **covered** = capabilities with extraction, **applicable** = covered + missing (not-applicable capabilities are excluded from both). The glyph is the group's **support level**:

| Glyph | Level | Meaning |
|---|---|---|
| ✅ | **Comprehensive** | every applicable capability is `full` — fixture-proven, resolves the general case |
| 🟢 | **Supported** | every applicable capability is extracted; some only *heuristically* (detected by pattern, not full AST/data-flow resolution) |
| 🟡 | **Partial** | some capabilities extracted, some still missing |
| 🔴 | **Not extracted** | nothing extracted yet |
| — | **N/A** | capability does not apply to this framework |

Examples: `🟢 20/20` = fully supported, some capabilities heuristic · `🟡 12/20` = 8 not yet extracted. Detail pages use the same palette **per cell** (✅ full · 🟢 heuristic/partial · 🔴 missing · — n/a).

## Frameworks


### Backend HTTP

| Name | Routing | Auth | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|---|
| [Ariadne GraphQL](../detail/lang.python.framework.ariadne.md) | 🟡 3/4 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/23 | 🟡 1/11 | |
| [Bottle](../detail/lang.python.framework.bottle.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 21/23 | 🟡 7/10 | |
| [CherryPy](../detail/lang.python.framework.cherrypy.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 21/23 | 🟡 7/10 | |
| [Django](../detail/lang.python.framework.django.md) | 🟡 3/4 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/23 | 🟡 7/10 | |
| [Django REST Framework](../detail/lang.python.framework.django-drf.md) | 🟡 3/4 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟢 24/24 | 🟡 12/15 | |
| [Falcon](../detail/lang.python.framework.falcon.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 21/23 | 🟡 7/10 | |
| [FastAPI](../detail/lang.python.framework.fastapi.md) | ✅ 4/4 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟢 9/9 | |
| [Flask](../detail/lang.python.framework.flask.md) | 🟡 3/4 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/23 | 🟡 8/11 | |
| [Graphene GraphQL](../detail/lang.python.framework.graphene.md) | 🟡 3/4 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/23 | 🔴 0/10 | |
| [Hug](../detail/lang.python.framework.hug.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 21/23 | 🟡 7/10 | |
| [Litestar](../detail/lang.python.framework.litestar.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 7/10 | |
| [Pyramid](../detail/lang.python.framework.pyramid.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 21/23 | 🟡 7/10 | |
| [Quart](../detail/lang.python.framework.quart.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 7/10 | |
| [Robyn](../detail/lang.python.framework.robyn.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 7/10 | |
| [Sanic](../detail/lang.python.framework.sanic.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 7/10 | |
| [Starlette](../detail/lang.python.framework.starlette.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 7/10 | |
| [Strawberry GraphQL](../detail/lang.python.framework.strawberry-graphql.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 7/10 | |
| [Tornado](../detail/lang.python.framework.tornado.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 21/23 | 🟡 7/10 | |
| [aiohttp](../detail/lang.python.framework.aiohttp.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 7/10 | |


### AI Integration

| Name | Other capabilities | Notes |
|---|---|---|
| [LangChain (LLM agent framework)](../detail/lang.python.framework.langchain.md) | 🟢 4/4 | |


### Task Queue

| Name | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|
| [Celery (task queue)](../detail/lang.python.framework.celery.md) | 🟢 1/1 | 🟡 22/23 | ✅ 7/7 | |
| [Dramatiq (task queue)](../detail/lang.python.framework.dramatiq.md) | 🟢 1/1 | 🟡 22/23 | 🟢 5/5 | |
| [RQ (Redis Queue)](../detail/lang.python.framework.rq.md) | 🟢 1/1 | 🟡 22/23 | 🟢 6/6 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [Flit](../detail/build.flit.md) | 🟢 | — | — | — | 🟢 | |
| [Hatch](../detail/build.hatch.md) | 🟢 | — | — | — | 🟢 | |
| [Hypothesis (property tests)](../detail/test.hypothesis.md) | — | — | — | — | 🔴 | |
| [Pipenv](../detail/build.pipenv.md) | 🟢 | — | — | — | 🟢 | |
| [Pipfile / Pipfile.lock](../detail/pkg.pipfile.md) | — | — | 🟢 | 🟢 | — | |
| [Poetry](../detail/build.poetry.md) | ✅ | — | — | — | ✅ | |
| [doctest (stdlib)](../detail/test.doctest.md) | — | — | — | — | 🔴 | |
| [nose2](../detail/test.nose2.md) | — | — | — | — | 🔴 | |
| [pip (requirements.txt)](../detail/build.pip.md) | ✅ | — | — | — | ✅ | |
| [pyproject.toml](../detail/pkg.pyproject.md) | — | — | 🟢 | ✅ | — | |
| [pytest](../detail/test.pytest.md) | ✅ | — | — | — | ✅ | |
| [requirements.txt](../detail/pkg.requirements.md) | — | — | — | ✅ | — | |
| [setuptools / setup.py](../detail/build.setuptools.md) | 🟢 | — | — | — | 🟢 | |
| [unittest (stdlib)](../detail/test.unittest.md) | ✅ | — | — | — | ✅ | |
| [uv (Astral)](../detail/build.uv.md) | ✅ | — | — | — | ✅ | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Alembic (migration tool)](../detail/lang.python.orm.alembic.md) | 🟢 2/2 | |
| [Beanie (async MongoDB ODM)](../detail/lang.python.orm.beanie.md) | 🟡 5/6 | |
| [Django ORM](../detail/lang.python.orm.django.md) | ✅ 8/8 | |
| [MongoEngine](../detail/lang.python.orm.mongoengine.md) | 🟡 5/6 | |
| [MySQL (PyMySQL / mysqlclient)](../detail/lang.python.driver.mysql.md) | 🟢 2/2 | |
| [Peewee](../detail/lang.python.orm.peewee.md) | 🟢 6/6 | |
| [Pony ORM](../detail/lang.python.orm.pony.md) | 🟢 6/6 | |
| [SQLAlchemy](../detail/lang.python.orm.sqlalchemy.md) | ✅ 8/8 | |
| [SQLModel](../detail/lang.python.orm.sqlmodel.md) | 🟢 8/8 | |
| [Tortoise ORM](../detail/lang.python.orm.tortoise.md) | 🟢 7/7 | |
| [boto3 DynamoDB](../detail/lang.python.driver.dynamodb.md) | ✅ 1/1 | |
| [cassandra-driver](../detail/lang.python.driver.cassandra.md) | ✅ 1/1 | |
| [elasticsearch-py](../detail/lang.python.driver.elastic.md) | ✅ 1/1 | |
| [neo4j (Python driver) / neomodel OGM](../detail/lang.python.driver.neo4j.md) | 🟡 3/4 | |
| [psycopg / asyncpg (PostgreSQL drivers)](../detail/lang.python.driver.postgres.md) | 🟢 2/2 | |
| [pymongo / motor](../detail/lang.python.driver.mongodb.md) | ✅ 1/1 | |
| [redis-py](../detail/lang.python.driver.redis.md) | ✅ 1/1 | |
| [sqlite3 (stdlib)](../detail/lang.python.driver.sqlite.md) | 🟢 2/2 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [APScheduler (Python advanced scheduler)](../detail/msg.apscheduler.md) | [message_broker](../by-category/message_broker.md) | 🟢 | |
| [Celery (Python task queue)](../detail/msg.celery.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Django signals (intra-repo pub/sub)](../detail/msg.django-signals.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Dramatiq (Python task queue)](../detail/msg.dramatiq.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Python transitions (FSM topology)](../detail/infra.state-machine.python-transitions.md) | [platform](../by-category/platform.md) | 🟢 | |

### Validation

| Name | Testing | Other capabilities | Notes |
|---|---|---|---|
| [Pydantic](../detail/lang.python.validation.pydantic.md) | 🟢 1/1 | 🟢 5/5 | |
| [attrs](../detail/lang.python.validation.attrs.md) | 🟢 1/1 | 🟢 5/5 | |
| [marshmallow](../detail/lang.python.validation.marshmallow.md) | 🟢 1/1 | 🟢 5/5 | |
