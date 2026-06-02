<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# C/C++

**Frameworks**: 19 · **Tools**: 16 · **ORMs**: 7 · **Other**: 4

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
| [ACE (Adaptive Communication Environment)](../detail/lang.c-cpp.framework.ace.md) | 🔴 0/1 | — | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 3/6 | |
| [Boost (Boost.Asio + utilities)](../detail/lang.c-cpp.framework.boost.md) | 🔴 0/1 | — | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 3/6 | |
| [Boost.Asio](../detail/lang.c-cpp.framework.boost-asio.md) | 🔴 0/1 | — | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 3/6 | |
| [Crow](../detail/lang.c-cpp.framework.crow.md) | 🟡 3/4 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 6/9 | |
| [Drogon](../detail/lang.c-cpp.framework.drogon.md) | 🟡 3/4 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 6/9 | |
| [Oat++](../detail/lang.c-cpp.framework.oatpp.md) | 🟡 3/4 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 6/9 | |
| [POCO C++ Libraries](../detail/lang.c-cpp.framework.poco.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 6/9 | |
| [Pistache](../detail/lang.c-cpp.framework.pistache.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 6/9 | |
| [RESTinio](../detail/lang.c-cpp.framework.restinio.md) | 🟡 3/4 | — | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 6/9 | |
| [Restbed](../detail/lang.c-cpp.framework.restbed.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 6/9 | |
| [cpprestsdk (Casablanca)](../detail/lang.c-cpp.framework.cpprestsdk.md) | 🟡 3/4 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 6/9 | |
| [libev](../detail/lang.c-cpp.framework.libev.md) | 🔴 0/1 | — | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 3/6 | |
| [libevent](../detail/lang.c-cpp.framework.libevent.md) | 🔴 0/1 | — | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 3/6 | |
| [libuv](../detail/lang.c-cpp.framework.libuv.md) | 🔴 0/1 | — | ✅ 4/4 | ✅ 1/1 | 🟡 21/24 | 🟡 3/6 | |


### UI Frontend

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Qt](../detail/lang.c-cpp.framework.qt.md) | ✅ 3/3 | ✅ 1/1 | 🟡 21/23 | 🟢 8/8 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [ROS (Robot Operating System)](../detail/lang.c-cpp.framework.ros.md) | 🟡 10/12 | 🟢 2/2 | |
| [Unreal Engine](../detail/lang.c-cpp.framework.unreal-engine.md) | 🟡 10/12 | 🟢 2/2 | |


### RPC Framework

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Protocol Buffers (C++)](../detail/lang.c-cpp.framework.protobuf.md) | 🟡 2/23 | 🟡 3/4 | |
| [gRPC C++ (grpc++)](../detail/lang.c-cpp.framework.grpc.md) | 🟡 2/23 | 🟢 4/4 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [Boost.Test](../detail/test.boost-test.md) | 🔴 | — | — | — | 🔴 | |
| [Buck2](../detail/lang.c-cpp.tool.buck2.md) | 🔴 | — | — | — | 🔴 | |
| [CMake](../detail/lang.c-cpp.tool.cmake.md) | 🟢 | — | — | — | 🟢 | |
| [Catch2](../detail/test.catch2.md) | 🔴 | — | — | — | 🔴 | |
| [Conan](../detail/lang.c-cpp.tool.conan.md) | — | — | — | 🟢 | — | |
| [CppUTest](../detail/test.cpputest.md) | 🔴 | — | — | — | 🔴 | |
| [CppUnit](../detail/test.cppunit.md) | 🔴 | — | — | — | 🔴 | |
| [GNU Make](../detail/lang.c-cpp.tool.make.md) | 🔴 | — | — | — | 🔴 | |
| [GoogleTest (gtest)](../detail/test.gtest.md) | 🔴 | — | — | — | 🔴 | |
| [Hunter](../detail/lang.c-cpp.tool.hunter.md) | — | — | 🔴 | 🔴 | — | |
| [Meson](../detail/lang.c-cpp.tool.meson.md) | 🔴 | — | — | — | 🔴 | |
| [Ninja](../detail/lang.c-cpp.tool.ninja.md) | 🔴 | — | — | — | 🔴 | |
| [build2](../detail/lang.c-cpp.tool.build2.md) | — | — | 🔴 | 🔴 | — | |
| [doctest (C++)](../detail/test.doctest-cpp.md) | 🔴 | — | — | — | 🔴 | |
| [vcpkg](../detail/lang.c-cpp.tool.vcpkg.md) | — | — | 🟢 | 🟢 | — | |
| [xmake](../detail/lang.c-cpp.tool.xmake.md) | 🔴 | — | — | — | 🔴 | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [MySQL Connector/C++](../detail/lang.c-cpp.driver.mysql-connector-cpp.md) | ✅ 3/3 | |
| [ODB](../detail/lang.c-cpp.orm.odb.md) | ✅ 7/7 | |
| [SOCI](../detail/lang.c-cpp.orm.soci.md) | 🟢 3/3 | |
| [libpqxx (PostgreSQL)](../detail/lang.c-cpp.driver.libpqxx.md) | ✅ 3/3 | |
| [mongocxx](../detail/lang.c-cpp.driver.mongocxx.md) | 🟢 3/3 | |
| [redis-plus-plus](../detail/lang.c-cpp.driver.redis-plus-plus.md) | ✅ 1/1 | |
| [sqlpp11](../detail/lang.c-cpp.orm.sqlpp11.md) | ✅ 3/3 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [MQTT (Paho C/C++ / Mosquitto)](../detail/lang.c-cpp.framework.mqtt.md) | [message_broker](../by-category/message_broker.md) | 🟢 | |
| [ZeroMQ (libzmq/cppzmq)](../detail/lang.c-cpp.framework.zeromq.md) | [message_broker](../by-category/message_broker.md) | 🟢 | |
| [librdkafka (C/C++ Kafka client)](../detail/lang.c-cpp.framework.librdkafka.md) | [message_broker](../by-category/message_broker.md) | 🟢 | |

### Validation

| Name | Testing | Other capabilities | Notes |
|---|---|---|---|
| [nlohmann/json (C++)](../detail/lang.c-cpp.framework.nlohmann-json.md) | ✅ 1/1 | 🟡 3/5 | |
