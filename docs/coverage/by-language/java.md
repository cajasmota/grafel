<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# java

**Frameworks**: 6 · **Tools**: 2 · **ORMs**: 2 · **Other**: 3

Back to [summary](../summary.md).

## Frameworks

| Name | auth_coverage | endpoint_synthesis | handler_attribution | middleware_coverage | Notes |
|---|---|---|---|---|---|
| [Dropwizard (JAX-RS subset)](../detail/lang.java.framework.dropwizard.md) | — | ⚠️ | ⚠️ | — | |
| [JAX-RS / Jakarta EE](../detail/lang.java.framework.jaxrs.md) | ✅ | ✅ | ✅ | — | |
| [Micronaut](../detail/lang.java.framework.micronaut.md) | — | ❌ | — | — | |
| [Quarkus (JAX-RS-backed)](../detail/lang.java.framework.quarkus.md) | ✅ | ✅ | ✅ | — | |
| [Spring Boot / Spring MVC](../detail/lang.java.framework.spring-boot.md) | ✅ | ✅ | ✅ | — | |
| [Spring WebFlux](../detail/lang.java.framework.spring-webflux.md) | ⚠️ | ✅ | ✅ | — | |

## Tools

| Name | dependency_graph | lockfile_parsing | manifest_parsing | target_extraction | Notes |
|---|---|---|---|---|---|
| [build.gradle / build.gradle.kts](../detail/pkg.gradle.md) | — | ❌ | ❌ | — | |
| [pom.xml](../detail/pkg.pom.md) | — | — | ✅ | — | |

## ORMs

| Name | migration_parsing | model_extraction | query_attribution | Notes |
|---|---|---|---|---|
| [Hibernate / JPA](../detail/lang.java.orm.hibernate.md) | — | ✅ | ⚠️ | |
| [Spring Data JPA](../detail/lang.java.orm.spring-data-jpa.md) | — | ✅ | ⚠️ | |

## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [.properties (application.properties)](../detail/config.properties.md) | [configuration](../by-category/configuration.md) | ✅ | |
| [Auth policy resolver (Java/Kotlin — Phase 1 of #1942)](../detail/security.auth-java.md) | [security](../by-category/security.md) | ✅ | |
| [Java](../detail/lang.java.md) | [language](../by-category/language.md) | ❌ | |
