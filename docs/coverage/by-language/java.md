<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# java

**Frameworks**: 22 · **Tools**: 10 · **ORMs**: 15 · **Other**: 4

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


### JVM Backend

| Name | Routing | Auth | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|---|
| [Akka HTTP (Java DSL)](../detail/lang.java.framework.akka-http.md) | 🟡 3/4 | 🔴 0/1 | ✅ 3/3 | 🔴 0/1 | 🟡 21/22 | 🟡 3/7 | |
| [Apache Struts](../detail/lang.java.framework.struts.md) | 🟡 3/4 | 🔴 0/1 | ✅ 3/3 | 🔴 0/1 | 🟡 21/22 | 🟡 3/7 | |
| [Dropwizard](../detail/lang.java.framework.dropwizard.md) | 🟡 3/4 | 🔴 0/1 | ✅ 3/3 | 🔴 0/1 | 🟡 21/22 | 🟡 1/13 | |
| [Eclipse MicroProfile](../detail/lang.java.framework.microprofile.md) | 🟡 3/4 | 🟢 1/1 | ✅ 3/3 | 🔴 0/1 | 🟡 21/22 | 🟡 2/16 | |
| [Google Guice (DI)](../detail/lang.java.framework.guice.md) | 🔴 0/4 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/22 | 🟡 3/16 | |
| [Helidon](../detail/lang.java.framework.helidon.md) | 🟡 3/4 | 🟢 1/1 | ✅ 3/3 | 🔴 0/1 | 🟡 21/22 | 🟡 1/13 | |
| [JAX-RS / Jakarta REST](../detail/lang.java.framework.jaxrs.md) | 🟡 3/4 | 🟢 1/1 | ✅ 3/3 | 🔴 0/1 | 🟡 21/22 | 🟡 13/16 | |
| [Jakarta EE (Servlet / EE Platform)](../detail/lang.java.framework.jakarta-ee.md) | 🟡 3/4 | 🟢 1/1 | ✅ 3/3 | 🔴 0/1 | 🟡 21/22 | 🟡 3/16 | |
| [Javalin](../detail/lang.java.framework.javalin.md) | 🟡 3/4 | 🔴 0/1 | ✅ 3/3 | 🔴 0/1 | 🟡 21/22 | 🟡 3/7 | |
| [Micronaut](../detail/lang.java.framework.micronaut.md) | 🟡 3/4 | 🟢 1/1 | ✅ 3/3 | 🔴 0/1 | 🟡 21/22 | 🟡 13/16 | |
| [Netflix DGS](../detail/lang.java.framework.dgs.md) | 🟡 3/4 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/22 | 🔴 0/16 | |
| [Quarkus](../detail/lang.java.framework.quarkus.md) | 🟡 3/4 | 🟢 1/1 | ✅ 3/3 | 🔴 0/1 | 🟡 21/22 | 🟡 4/16 | |
| [Spring Boot / Spring MVC](../detail/lang.java.framework.spring-boot.md) | 🟢 4/4 | ✅ 1/1 | ✅ 3/3 | 🔴 0/1 | 🟢 23/23 | 🟡 15/18 | |
| [Spring WebFlux (reactive)](../detail/lang.java.framework.spring-webflux.md) | 🟡 3/4 | 🟢 1/1 | ✅ 3/3 | 🔴 0/1 | 🟡 21/22 | 🟡 12/16 | |
| [Spring for GraphQL](../detail/lang.java.framework.spring-graphql.md) | 🟡 3/4 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/22 | 🔴 0/16 | |
| [Vert.x](../detail/lang.java.framework.vertx.md) | 🟡 3/4 | 🔴 0/1 | ✅ 3/3 | 🔴 0/1 | 🟡 21/22 | 🟡 3/7 | |


### UI Frontend

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Google Web Toolkit (GWT)](../detail/lang.java.framework.gwt.md) | 🟢 2/2 | 🔴 0/1 | 🟡 20/21 | 🔴 0/3 | |
| [Vaadin (UI-as-server)](../detail/lang.java.framework.vaadin.md) | 🟢 2/2 | 🔴 0/1 | 🟡 20/21 | 🔴 0/3 | |


### Meta Framework

| Name | Routing | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|
| [Play Framework](../detail/lang.java.framework.play.md) | 🟡 1/2 | 🟢 2/2 | 🔴 0/1 | 🟡 20/23 | 🟡 2/4 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Android Jetpack (Compose / ViewModel / Room / Navigation / Hilt)](../detail/lang.java.framework.android-jetpack.md) | 🟢 2/2 | 🔴 0/1 | 🟡 19/20 | 🟡 7/8 | |
| [Android SDK (Activity/Fragment routing)](../detail/lang.java.framework.android-sdk.md) | 🟢 2/2 | 🔴 0/1 | 🟡 19/20 | 🟡 7/8 | |


### AI Integration

| Name | Other capabilities | Notes |
|---|---|---|
| [LangChain4J (LLM agent framework)](../detail/lang.java.framework.langchain4j.md) | 🟡 1/4 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [AssertJ](../detail/test.assertj.md) | 🔴 | — | — | — | 🔴 | |
| [Gradle (Groovy + Kotlin DSL)](../detail/build.gradle.md) | ✅ | — | — | — | ✅ | |
| [JUnit 4](../detail/test.junit4.md) | 🟢 | — | — | — | 🟢 | |
| [JUnit 5](../detail/test.junit5.md) | ✅ | — | — | — | ✅ | |
| [Maven (pom.xml)](../detail/build.maven.md) | ✅ | — | — | — | ✅ | |
| [Mockito](../detail/test.mockito.md) | 🔴 | — | — | — | 🟢 | |
| [REST-assured](../detail/test.restassured.md) | 🔴 | — | — | — | 🔴 | |
| [TestNG](../detail/test.testng.md) | 🔴 | — | — | — | 🟢 | |
| [build.gradle / build.gradle.kts](../detail/pkg.gradle.md) | — | — | 🔴 | 🔴 | — | |
| [pom.xml](../detail/pkg.pom.md) | — | — | — | ✅ | — | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [AWS SDK DynamoDB (Java)](../detail/lang.java.orm.dynamodb.md) | 🟢 3/3 | |
| [Ebean ORM](../detail/lang.java.orm.ebean.md) | 🟢 7/7 | |
| [EclipseLink](../detail/lang.java.orm.eclipselink.md) | 🟢 7/7 | |
| [Hibernate ORM](../detail/lang.java.orm.hibernate.md) | 🟢 7/7 | |
| [JPA / Jakarta Persistence API](../detail/lang.java.orm.jpa.md) | 🟢 7/7 | |
| [MongoDB Java Driver](../detail/lang.java.driver.mongodb.md) | ✅ 1/1 | |
| [MyBatis](../detail/lang.java.orm.mybatis.md) | 🟢 7/7 | |
| [Neo4j (Java driver)](../detail/lang.java.orm.neo4j.md) | 🟡 4/5 | |
| [Quarkus Panache (SQL + Reactive + MongoDB)](../detail/lang.java.orm.panache.md) | 🟢 7/7 | |
| [Spring Data Cassandra](../detail/lang.java.orm.spring-data-cassandra.md) | 🔴 0/3 | |
| [Spring Data Elasticsearch](../detail/lang.java.orm.spring-data-elastic.md) | 🔴 0/3 | |
| [Spring Data JPA](../detail/lang.java.orm.spring-data-jpa.md) | 🟡 5/7 | |
| [Spring Data MongoDB](../detail/lang.java.orm.spring-data-mongo.md) | 🔴 0/3 | |
| [Spring Data Redis](../detail/lang.java.orm.spring-data-redis.md) | 🔴 0/3 | |
| [jOOQ](../detail/lang.java.orm.jooq.md) | 🟢 6/6 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [.properties (application.properties)](../detail/config.properties.md) | [platform](../by-category/platform.md) | ✅ | |
| [Auth policy resolver (Java/Kotlin — Phase 1 of #1942)](../detail/security.auth-java.md) | [security](../by-category/security.md) | ✅ | |
| [Spring StateMachine (FSM topology)](../detail/infra.state-machine.spring-statemachine.md) | [platform](../by-category/platform.md) | 🟢 | |

### Validation

| Name | Testing | Other capabilities | Notes |
|---|---|---|---|
| [Bean Validation (Jakarta/javax)](../detail/lang.java.validation.bean-validation.md) | 🔴 0/1 | ✅ 4/4 | |
