<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# clojure

**Frameworks**: 4 · **Tools**: 4 · **ORMs**: 5 · **Other**: 1

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
| [Compojure](../detail/lang.clojure.framework.compojure.md) | 🟡 3/6 | 🔴 0/1 | 🔴 0/4 | ✅ 1/1 | 🔴 0/24 | 🔴 0/13 | |
| [Pedestal](../detail/lang.clojure.framework.pedestal.md) | 🔴 0/6 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/24 | 🔴 0/13 | |
| [Reitit](../detail/lang.clojure.framework.reitit.md) | 🟡 3/6 | 🔴 0/1 | 🔴 0/4 | ✅ 1/1 | 🔴 0/24 | 🔴 0/13 | |
| [Ring](../detail/lang.clojure.framework.ring.md) | 🔴 0/5 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/24 | 🔴 0/13 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [Boot](../detail/build.boot.md) | 🟢 | — | — | — | 🟢 | |
| [Leiningen](../detail/build.leiningen.md) | 🟢 | — | — | — | 🟢 | |
| [shadow-cljs](../detail/build.shadow-cljs.md) | 🟢 | — | — | — | 🟢 | |
| [tools.deps / deps.edn](../detail/build.tools-deps.md) | 🟢 | — | — | — | 🟢 | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [DataScript](../detail/lang.clojure.driver.datascript.md) | 🔴 0/11 | |
| [Datomic](../detail/lang.clojure.driver.datomic.md) | 🔴 0/11 | |
| [HoneySQL](../detail/lang.clojure.driver.honeysql.md) | 🔴 0/11 | |
| [clojure.java.jdbc (legacy)](../detail/lang.clojure.driver.clojure-java-jdbc.md) | 🔴 0/11 | |
| [next.jdbc](../detail/lang.clojure.driver.next-jdbc.md) | 🔴 0/11 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Clojure (base language)](../detail/lang.clojure.base.md) | [language](../by-category/language.md) | ✅ | |
