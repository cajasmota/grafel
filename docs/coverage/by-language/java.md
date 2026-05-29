<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# java

**Frameworks**: 19 · **Tools**: 10 · **ORMs**: 14 · **Other**: 3

Back to [summary](../summary.md).

> Group columns show `glyph covered/applicable`: **covered** = capabilities with extraction (✅ full + ⚠️ partial), **applicable** = covered + ❌ missing (not-applicable cells are excluded). The glyph is the group's worst cell — ✅ all full · ⚠️ some heuristic/partial · ❌ some missing. So `20/20 ⚠️` means every applicable capability is extracted, some only heuristically.

## Frameworks


### JVM Backend

| Name | Routing | Auth | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|---|
| [Akka HTTP (Java DSL)](../detail/lang.java.framework.akka-http.md) | ❌ 0/3 | ❌ 0/1 | ✅ 3/3 | ❌ 0/1 | ❌ 6/20 | ❌ 3/16 | |
| [Apache Struts](../detail/lang.java.framework.struts.md) | ❌ 2/3 | ❌ 0/1 | ✅ 3/3 | ❌ 0/1 | ❌ 6/20 | ❌ 3/16 | |
| [Dropwizard](../detail/lang.java.framework.dropwizard.md) | ⚠️ 3/3 | ❌ 0/1 | ✅ 3/3 | ❌ 0/1 | ❌ 19/20 | ❌ 4/16 | |
| [Eclipse MicroProfile](../detail/lang.java.framework.microprofile.md) | ⚠️ 3/3 | ⚠️ 1/1 | ✅ 3/3 | ⚠️ 1/1 | ❌ 19/20 | ❌ 12/16 | |
| [Helidon](../detail/lang.java.framework.helidon.md) | ⚠️ 3/3 | ⚠️ 1/1 | ✅ 3/3 | ⚠️ 1/1 | ❌ 19/20 | ⚠️ 13/13 | |
| [JAX-RS / Jakarta REST](../detail/lang.java.framework.jaxrs.md) | ⚠️ 3/3 | ⚠️ 1/1 | ✅ 3/3 | ⚠️ 1/1 | ❌ 19/20 | ❌ 12/16 | |
| [Jakarta EE (Servlet / EE Platform)](../detail/lang.java.framework.jakarta-ee.md) | ⚠️ 3/3 | ⚠️ 1/1 | ✅ 3/3 | ⚠️ 1/1 | ❌ 19/20 | ❌ 15/16 | |
| [Javalin](../detail/lang.java.framework.javalin.md) | ❌ 0/3 | ❌ 0/1 | ✅ 3/3 | ❌ 0/1 | ❌ 19/20 | ❌ 4/16 | |
| [Micronaut](../detail/lang.java.framework.micronaut.md) | ⚠️ 3/3 | ⚠️ 1/1 | ✅ 3/3 | ⚠️ 1/1 | ❌ 19/20 | ❌ 12/16 | |
| [Quarkus](../detail/lang.java.framework.quarkus.md) | ⚠️ 3/3 | ⚠️ 1/1 | ✅ 3/3 | ⚠️ 1/1 | ❌ 19/20 | ❌ 15/16 | |
| [Spring Boot / Spring MVC](../detail/lang.java.framework.spring-boot.md) | ⚠️ 3/3 | ✅ 1/1 | ✅ 3/3 | ⚠️ 1/1 | ⚠️ 21/21 | ⚠️ 18/18 | |
| [Spring WebFlux (reactive)](../detail/lang.java.framework.spring-webflux.md) | ❌ 2/3 | ⚠️ 1/1 | ✅ 3/3 | ⚠️ 1/1 | ❌ 19/20 | ❌ 14/16 | |
| [Vert.x](../detail/lang.java.framework.vertx.md) | ❌ 2/3 | ❌ 0/1 | ✅ 3/3 | ❌ 0/1 | ❌ 19/20 | ❌ 4/16 | |


### UI Frontend

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Google Web Toolkit (GWT)](../detail/lang.java.framework.gwt.md) | ❌ 0/3 | ❌ 0/1 | ❌ 3/21 | ❌ 0/8 | |
| [Vaadin (UI-as-server)](../detail/lang.java.framework.vaadin.md) | ❌ 0/3 | ❌ 0/1 | ❌ 3/21 | ❌ 0/8 | |


### Meta Framework

| Name | Routing | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|
| [Play Framework](../detail/lang.java.framework.play.md) | ❌ 0/2 | ❌ 0/3 | ❌ 0/1 | ❌ 0/21 | ❌ 0/7 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Android Jetpack (Compose / ViewModel / Room / Navigation / Hilt)](../detail/lang.java.framework.android-jetpack.md) | ❌ 0/3 | ❌ 0/1 | ❌ 3/21 | ❌ 0/9 | |
| [Android SDK (Activity/Fragment routing)](../detail/lang.java.framework.android-sdk.md) | ❌ 0/3 | ❌ 0/1 | ❌ 3/21 | ❌ 0/9 | |


### AI Integration

| Name | Other capabilities | Notes |
|---|---|---|
| [LangChain4J (LLM agent framework)](../detail/lang.java.framework.langchain4j.md) | ⚠️ 3/3 | |


## Tools

| Name | Dependency graph | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|
| [AssertJ](../detail/test.assertj.md) | ❌ | — | — | ❌ | |
| [Gradle (Groovy + Kotlin DSL)](../detail/build.gradle.md) | ✅ | — | — | ✅ | |
| [JUnit 4](../detail/test.junit4.md) | ⚠️ | — | — | ⚠️ | |
| [JUnit 5](../detail/test.junit5.md) | ✅ | — | — | ✅ | |
| [Maven (pom.xml)](../detail/build.maven.md) | ✅ | — | — | ✅ | |
| [Mockito](../detail/test.mockito.md) | ❌ | — | — | ❌ | |
| [REST-assured](../detail/test.restassured.md) | ❌ | — | — | ❌ | |
| [TestNG](../detail/test.testng.md) | ❌ | — | — | ⚠️ | |
| [build.gradle / build.gradle.kts](../detail/pkg.gradle.md) | — | ❌ | ❌ | — | |
| [pom.xml](../detail/pkg.pom.md) | — | — | ✅ | — | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [AWS SDK DynamoDB (Java)](../detail/lang.java.orm.dynamodb.md) | ❌ 2/7 | |
| [Ebean ORM](../detail/lang.java.orm.ebean.md) | ❌ 7/8 | |
| [EclipseLink](../detail/lang.java.orm.eclipselink.md) | ❌ 7/8 | |
| [Hibernate ORM](../detail/lang.java.orm.hibernate.md) | ❌ 7/8 | |
| [JPA / Jakarta Persistence API](../detail/lang.java.orm.jpa.md) | ❌ 7/8 | |
| [MyBatis](../detail/lang.java.orm.mybatis.md) | ❌ 5/8 | |
| [Neo4j (Java driver)](../detail/lang.java.orm.neo4j.md) | ❌ 2/8 | |
| [Quarkus Panache (SQL + Reactive + MongoDB)](../detail/lang.java.orm.panache.md) | ❌ 5/8 | |
| [Spring Data Cassandra](../detail/lang.java.orm.spring-data-cassandra.md) | ❌ 3/4 | |
| [Spring Data Elasticsearch](../detail/lang.java.orm.spring-data-elastic.md) | ❌ 3/4 | |
| [Spring Data JPA](../detail/lang.java.orm.spring-data-jpa.md) | ❌ 7/8 | |
| [Spring Data MongoDB](../detail/lang.java.orm.spring-data-mongo.md) | ❌ 3/4 | |
| [Spring Data Redis](../detail/lang.java.orm.spring-data-redis.md) | ❌ 3/4 | |
| [jOOQ](../detail/lang.java.orm.jooq.md) | ❌ 2/8 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [.properties (application.properties)](../detail/config.properties.md) | [platform](../by-category/platform.md) | ✅ | |
| [Auth policy resolver (Java/Kotlin — Phase 1 of #1942)](../detail/security.auth-java.md) | [security](../by-category/security.md) | ✅ | |

### Validation

| Name | Testing | Other capabilities | Notes |
|---|---|---|---|
| [Bean Validation (Jakarta/javax)](../detail/lang.java.validation.bean-validation.md) | ⚠️ 1/1 | ❌ 3/5 | |
