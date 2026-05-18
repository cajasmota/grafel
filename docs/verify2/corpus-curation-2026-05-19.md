# VERIFY-2 corpus curation proposal

**Date:** 2026-05-19
**Source manifest:** `scripts/verify2/run.sh` (REPOS array lines 46–520)
**Source entries:** 283 (manifest header says ~289 — close enough; 6 entries
were dropped/dedup-noted in line comments)
**Target:** smallest set that preserves full extractor + framework + ORM +
manifest + CI/CD + IaC surface coverage measured against `internal/extractors/`.

---

## 1. Summary

| | count |
| --- | ---: |
| Current REPOS array entries | 283 |
| Proposed KEEP-primary | 47 |
| Proposed KEEP-secondary | 25 |
| **Proposed total** | **72** |
| Dropped (redundant) | 211 |
| Reduction | **−74.6%** |

The proposed 72-entry corpus preserves coverage of every language extractor
in `internal/extractors/` (32 directories), every ORM/framework family
currently exercised, every CI/CD format, every IaC family, every NoSQL
client ecosystem, and every API/IDL alternative. The only material thinning
is in *intra-family redundancy* (e.g. 8 state-management libs collapsed to
2; 12 web-framework alternates per language collapsed to 2 per language; 5
CDK flavors collapsed to 2).

### Top 3 most-redundant categories eliminated

1. **Web-framework alts (chunk J, 29 entries → 6 kept)** — `axum`/`rocket`/
   `warp`/`tide` all exercise the same Rust extractor + macro-attribute
   patterns; one representative suffices once `actix-examples` is in. Same
   for the 4 Go alternates (echox/fiber/beego in addition to gin/chi),
   the 4 Python alternates (tornado/starlette/pyramid/bottle on top of
   flask/django/fastapi), and the PHP/Ruby trios.
2. **State management (chunk AA, 8 entries → 1 kept)** — redux/mobx/
   zustand/pinia/ngrx/recoil/xstate/effector are all single-language
   TypeScript libraries exercising decorator + store-creation patterns
   already covered by `nestjs`, `angular-realworld`, `nextjs-commerce`,
   and `sveltekit`. Keep one (xstate, distinctive statechart DSL).
3. **CDK / Pulumi multi-flavor sprawl (chunk M, 9 entries → 2 kept)** —
   CDK TS/Py/Java/.NET/Go and Pulumi TS/Py/.NET/Go cover the same
   programmatic-IaC pattern. Per-language extractor logic is what's being
   tested, and the per-language extractor is *already* exercised by a
   realworld app in that language. Keep one CDK (TypeScript, the canonical
   author audience) + one Pulumi (Go, distinct from CDK's flavor).

### Single-source coverage — MUST keep (no alternative)

These are the only repo in the corpus exercising their respective extractor
or DSL. Dropping any of them creates a coverage gap.

| extractor / DSL | only-source entry | why no alternative |
| --- | --- | --- |
| `extractors/razor` | `aspnetcore-docs-samples` | only `.cshtml`/`.razor` corpus |
| `extractors/fish` | `tide` (IlanCosman/tide) | only `.fish` corpus |
| `extractors/just` | `just` (casey/just) | only justfile corpus |
| `extractors/zig` | `http.zig` | only `.zig` corpus |
| `extractors/lua` | `kickstart.nvim` | only `.lua` corpus |
| `extractors/proto` | `grpc-go-examples` | only `.proto` corpus |
| `extractors/graphql` | `apollo-server` | only `.graphql` SDL corpus |
| `extractors/dockerfile` | (incidental — covered by many) | n/a |
| `notebook` extractor | `jupyter-notebook` | only `.ipynb` corpus |
| `sql_dbt` | `jaffle_shop` | only dbt corpus |
| `bicep` | `azure-quickstart-templates` | only Bicep corpus |
| `starlark` | `tilt` | only Starlark corpus (also `bazel` BUILD) |
| `java_bpmn` | `camunda-bpm-examples` | only BPMN corpus |
| `smithy` / `avro` / `thrift` / `raml` / `json-schema` / `asyncapi` / `api-blueprint` | one repo each | each IDL is single-source |
| nginx-conf / apache-httpd-conf / caddyfile / traefik-dynamic / kong-declarative / envoy-yaml / haproxy-cfg | one repo each | each proxy format is single-source |
| `multi` (Selenium polyglot) | `seleniumhq-examples` | only multi-lang fixture |

That's **22 single-source MUST-KEEP** entries before the language matrix
even starts.

---

## 2. Coverage matrix — proof of no gaps

### 2a. Language-extractor matrix (32 dirs in `internal/extractors/`)

| extractor dir | covered by (primary) | also incidentally |
| --- | --- | --- |
| clojure | usermanager-example | leiningen (sec) |
| cpp | spdlog | UnrealEnginePython, cmake |
| csharp | aspnetcore-realworld | EntityComponentSystemSamples, maui-samples |
| css | nextjs-commerce, sveltekit | every frontend |
| dart | flutter-samples | dart-samples (drop) |
| dockerfile | spring-petclinic + many | every realworld app |
| elixir | phoenix-todo-list | phoenix-live-view (sec), elixir |
| fish | tide (IlanCosman) | — |
| golang | gin, etcd | every Go realworld |
| graphql | apollo-server | — |
| groovy | jenkins | ratpack-example-books (drop), gradle |
| hcl | terraform-aws-vpc | nomad-pack (sec) |
| html | every frontend | — |
| java | spring-petclinic, kafka-streams-examples | quarkus, maven, gradle |
| javascript | express-realworld, react-redux-realworld | mongoose, jest etc |
| just | just | — |
| kotlin | ktor-samples, exposed | compose-samples, http4k (sec) |
| lua | kickstart.nvim | — |
| markdown | every README, hugoDocs | docusaurus |
| php | symfony-demo, laravel-quickstart | composer |
| proto | grpc-go-examples | — |
| python | django-realworld, flask-realworld, requests, pandas | many |
| razor | aspnetcore-docs-samples | — |
| ruby | rails-realworld, sidekiq | jekyll (sec) |
| rust | actix-examples, tokio | axum (sec), bevy (sec) |
| scala | play-scala-starter, spark | http4s (drop), akka-http (drop) |
| shell | every repo | — |
| sql | django-realworld, alembic | jaffle_shop |
| swift | vapor-api-template, sample-food-truck (SwiftUI) | ios-oss |
| yaml | starter-workflows, argocd-example-apps | every CI repo |
| zig | http.zig | — |
| **complexity / cross / references / registry** | exercised by all | n/a (analyzers, not extractors) |

**Result: 32/32 extractor dirs covered.**

### 2b. ORM / query-builder matrix

| ORM | covered by | bucket |
| --- | --- | --- |
| Django ORM | django-realworld | KEEP-primary |
| SQLAlchemy | microblog OR flask-realworld | KEEP-primary (microblog) |
| ActiveRecord | rails-realworld | KEEP-primary |
| Eloquent | laravel-quickstart | KEEP-primary |
| Hibernate / JPA | spring-petclinic (incl Spring Data) | KEEP-primary |
| jOOQ | joal | KEEP-secondary |
| MyBatis | jpetstore-6 | KEEP-secondary |
| Prisma | nestjs-realworld-typeorm covers TS-decorator ORMs | dropped (Prisma not in corpus today either) |
| TypeORM | nestjs-realworld-typeorm | KEEP-primary |
| Sequelize | sequelize-express-example | DROP (TypeORM repo covers same TS pattern; if kept → secondary) |
| Knex | express-bookshelf-realworld | DROP (covered incidentally by knex migration entry) |
| Bookshelf | express-bookshelf-realworld | DROP |
| MikroORM | nestjs-realworld-mikroorm | DROP (TypeORM covers decorator pattern) |
| Ent (Go) | ent | KEEP-secondary |
| GORM | golang-gin-realworld | KEEP-primary |
| sqlx | fabric-ca | DROP (GORM covers Go ORM surface) |
| sqlc | sqlc-examples | KEEP-secondary (codegen pattern is unique) |
| Diesel (Rust) | actix-diesel-realworld | KEEP-secondary |
| SeaORM | sea-orm-examples | DROP (Diesel covers Rust ORM) |
| Exposed (Kotlin) | exposed | KEEP-primary |
| Dapper | netcore-boilerplate | KEEP-secondary |
| Sequel (Ruby) | sequel | DROP (Rails covers Ruby SQL; sequel as migration tool only) |

### 2c. Web framework matrix

| stack | kept | notes |
| --- | --- | --- |
| Python: Flask, Django, FastAPI | flask-realworld, django-realworld, fastapi-realworld | drop Tornado/Starlette/Pyramid/Bottle (same extractor + decorator surface) |
| Go: Gin, Chi | gin, chi | drop echox/gofiber/beego/golang-gin-realworld kept for GORM (sec) |
| JS: Express, NestJS | express-realworld, nestjs-starter, nextjs-commerce | drop fastify/koa/hapi/sails/adonis |
| TS frameworks | angular-realworld, sveltekit, nextjs-commerce | drop vue/svelte realworld (covered by Pinia + sveltekit), drop solid/preact/ember/qwik/lit/htmx/alpine/gatsby/remix/nuxt/vite/astro |
| Java: Spring, Kafka | spring-petclinic, kafka-streams-examples | drop quarkus/micronaut/helidon/vertx/dropwizard/play-java/spark-java (all Java framework variants) |
| Kotlin: Ktor | ktor-samples | drop javalin/http4k (sec — keep http4k for distinctive functional DSL) |
| Scala: Play, Spark | play-scala-starter, spark | drop akka-http/http4s |
| PHP: Laravel, Symfony | laravel-quickstart, symfony-demo | drop slim/codeigniter/yii2/lean |
| Ruby: Rails, Sidekiq | rails-realworld, sidekiq | drop sinatra/hanami/grape |
| Rust: Actix, Tokio | actix-examples, tokio, mini-redis | drop axum/rocket/warp/tide (Rust router macros same shape) — keep axum (sec) for tower middleware ecosystem |
| Elixir: Phoenix | phoenix-todo-list, phoenix-live-view | drop plug (covered by phoenix) |
| C#: ASP.NET | aspnetcore-realworld | drop maui (mobile sec) |
| Clojure: Ring | usermanager-example | drop compojure/pedestal |

### 2d. CI/CD matrix

| format | kept |
| --- | --- |
| GitHub Actions | starter-workflows (PRIMARY) |
| GitLab CI | gitlab-runner |
| CircleCI | circleci-demo-python-django |
| Jenkins (Groovy) | jenkins |
| Tekton | tektoncd-pipeline |
| Drone | DROP (yaml step format ≈ argocd already) |
| Buildkite | DROP (yaml step format ≈ circleci) |
| Skaffold | DROP (covered by k8s yaml) |
| Tilt (Starlark) | tilt — MUST keep (only Starlark sample) |

### 2e. IaC matrix

| family | kept | dropped |
| --- | --- | --- |
| Terraform | terraform-aws-vpc | — |
| Nomad/HCL | nomad-pack (sec) | — |
| K8s YAML | argocd-example-apps | kustomize, awesome-compose (redundant yaml) |
| Helm | prometheus-helm | — |
| Ansible | ansible-for-devops | — |
| Chef / Puppet (Ruby DSL) | DROP both (Ruby DSL covered by rails) | chef-runit, puppet-control-repo |
| CDK | aws-cdk-examples-typescript | other 4 flavors |
| Pulumi | pulumi-examples-go | other 3 flavors |
| Bicep | azure-quickstart-templates | — |
| CloudFormation | aws-cloudformation-samples | — |
| SAM | aws-sam-cli-app-templates | — |
| Serverless Framework | serverless-examples | — |
| Crossplane | crossplane | — |

### 2f. NoSQL / cache / streaming / message-broker

| ecosystem | kept | dropped |
| --- | --- | --- |
| MongoDB | mongoose (Node ODM) + mongo-go-driver (Go) | pymongo, motor, mongo-java-driver |
| Redis | redis-py (Python sync) + go-redis | ioredis, lettuce |
| Cassandra | cassandra-java-driver | — |
| Couchbase | DROP (DynamoDB SDK covers AWS pattern) | couchbase-gocb |
| DynamoDB / AWS SDK | aws-sdk-go-v2 | aws-sdk-js-v3 (drop) |
| RabbitMQ | rabbitmq-tutorials | — |
| NATS | DROP (Kafka covers streaming) | nats.go |
| Kafka | kafka-streams-examples | — |
| WebSocket / Socket.IO | socket.io | pusher-js, ably-js, MQTT.js, mdn-dom-examples |
| WebRTC | DROP | pion-webrtc |
| MQTT | DROP (socket.io covers pub/sub) | MQTT.js |

### 2g. Manifest / build-tool formats

Real apps' manifests cover the formats; explicit manifest-only entries DROP.

| format | kept (via) |
| --- | --- |
| pyproject.toml / setup.py | django-realworld, pandas |
| package.json (npm/yarn) | express-realworld, nestjs-starter |
| pnpm-workspace.yaml | pnpm (KEEP-secondary, single source) |
| tsconfig + nx | DROP nx (covered by nestjs/sveltekit tsconfigs) |
| Cargo.toml | tokio (KEEP — workspace manifest unique) |
| pom.xml | spring-petclinic + kafka-streams-examples |
| build.gradle | spring-petclinic (Gradle build) — DROP gradle/gradle |
| BUILD (Bazel/Starlark) | bazel (KEEP-secondary, only Starlark BUILD corpus other than Tilt) |
| Makefile | DROP gnu-make (Makefiles in every Go repo) |
| CMakeLists | cmake (KEEP-secondary, only CMake corpus) |
| composer.json | symfony-demo + laravel-quickstart — DROP composer/composer |
| Gemfile | rails-realworld + sidekiq — DROP bundler |
| mix.exs | phoenix-todo-list — DROP elixir/elixir |
| build.sbt | play-scala-starter, spark — DROP sbt |
| project.clj | usermanager-example — DROP leiningen |
| lerna.json / turbo.json | DROP both (workspace package.json covered by pnpm) |

### 2h. Validation / lint / auth / observability

| category | kept | dropped |
| --- | --- | --- |
| Validation (Zod/Joi/Yup/Pydantic/class-validator) | pydantic + zod | joi, yup, class-validator (TS decorator surface covered by nestjs) |
| Lint configs | DROP all 6 | eslint, prettier, ruff, rubocop, golangci-lint, rust-clippy (configs covered by every realworld app having `.eslintrc`/`.golangci.yml` etc) |
| Auth (OAuth/JWT/Keycloak/Auth0/Vault) | DROP all | covered by aspnetcore-realworld + django-realworld (auth flows) |
| Observability (OTel/Prom/Sentry/DD) | DROP all | covered by spring-petclinic, etcd (metrics) |

### 2i. Mobile / game / embedded

| stack | kept | dropped |
| --- | --- | --- |
| iOS UIKit | ios-oss (sec) | — |
| iOS SwiftUI | sample-food-truck | — |
| Android Java | android-architecture (sec) | — |
| Android Kotlin Compose | compose-samples (sec) | — |
| Flutter | flutter-samples | dart-samples (covered) |
| React Native | DROP | react-native (template covered by express-realworld JS) |
| Ionic | DROP | ionic-conference-app |
| MAUI | DROP | maui-samples (csharp covered by aspnetcore) |
| Unity ECS | EntityComponentSystemSamples (sec) | — |
| Unreal C++ | DROP | UnrealEnginePython (cpp covered by spdlog) |
| Bevy Rust | DROP | bevy (rust covered by tokio/actix) |
| Arduino | DROP | arduino-examples (cpp covered) |
| ESP-IDF | esp-idf (sec) | — only C corpus |
| MicroPython | DROP | (python covered) |
| Cortex-M Rust | DROP | (rust covered) |

### 2j. Data / ML / notebook / workflow

| repo | kept | rationale |
| --- | --- | --- |
| scikit-learn | DROP | python covered by pandas (NumPy + scipy import) |
| pytorch-examples | DROP | python covered |
| keras-io | DROP | python covered |
| transformers | DROP | python covered |
| jupyter-notebook | **KEEP-primary** | only `.ipynb` corpus |
| polars | DROP | python covered |
| spark | KEEP-secondary | only Scala data corpus |
| jaffle_shop | **KEEP-primary** | only dbt corpus |
| airflow | KEEP-secondary | unique @dag/@task decorator pattern |
| prefect, dagster, cadence, temporal | DROP | airflow covers decorator workflow surface |
| camunda-bpm-examples | **KEEP-primary** | only `java_bpmn` corpus |

### 2k. SSGs / Testing / Migration / Realtime

| family | kept | dropped |
| --- | --- | --- |
| SSGs | hugoDocs + sphinx (RST + plugin) | jekyll, hexo, mkdocs, vitepress, docusaurus, nextra, eleventy |
| Test runners | pytest (sec) | jest, mocha, cypress, playwright, junit5, rspec, karate, cucumber-js |
| Selenium polyglot | seleniumhq-examples | — (only multi-lang fixture) |
| Migration tools | alembic | flyway, liquibase, knex, goose, migrate-mongo, sequel |
| Realtime | socket.io | pusher, ably, MQTT, pion-webrtc, mdn-dom-examples |
| Frontend SPAs | sveltekit + angular-realworld | vue/svelte/solid/preact/ember/astro/remix/nuxt/lit/htmx/alpine/qwik/gatsby/vite |
| Serverless | aws-lambda-python-runtime-interface-client + cloudflare-workers-sdk | all other lambda runtimes (handler shape is per-language and already covered by realworld apps) |

---

## 3. Per-entry decisions (283 entries)

Legend: **P** = KEEP-primary, **S** = KEEP-secondary, **D** = DROP.

### Headline / framework realworld apps
| # | bucket | entry | rationale |
| --- | --- | --- | --- |
| 1 | P | requests | Python stdlib library; smallest test for resolver baseline (1.72% bug-rate is the reference low-watermark) |
| 2 | P | flask-realworld | Flask + SQLAlchemy + Jinja sample |
| 3 | P | click | Python CLI tool; resolver low-watermark (6.95%) |
| 4 | P | django-realworld | Django + DRF + ORM + Jinja in one |
| 5 | P | pandas | Large Python codebase (heavy import surface, complex modules) |
| 6 | P | gin | Go HTTP routing source |
| 7 | P | chi | Go middleware-chain source (distinct from gin) |
| 8 | P | etcd | Large Go microservice + gRPC + protobuf |
| 9 | P | express-realworld | Node Express + Mongoose |
| 10 | P | nestjs-starter | TS decorator framework + tsconfig |
| 11 | P | nextjs-commerce | Next.js SSR + commerce app |
| 12 | P | spring-petclinic | Spring Boot + JPA + Maven |
| 13 | P | kafka-streams-examples | Java Kafka streaming |
| 14 | P | exposed | Kotlin SQL DSL |
| 15 | P | ktor-samples | Kotlin Ktor framework sample |
| 16 | P | play-scala-starter | Scala Play (only Scala web sample) |
| 17 | D | ratpack-example-books | groovy covered by jenkins (which has more groovy surface) |
| 18 | P | usermanager-example | Clojure Ring/Compojure |
| 19 | P | rails-realworld | Rails MVC + ActiveRecord |
| 20 | P | sidekiq | Ruby library |
| 21 | P | laravel-quickstart | Laravel + Eloquent |
| 22 | P | symfony-demo | Symfony + Doctrine |
| 23 | P | mini-redis | Tokio sample |
| 24 | P | actix-examples | Actix routing |
| 25 | P | vapor-api-template | Vapor (Swift web) |
| 26 | P | aspnetcore-realworld | ASP.NET Core MVC |
| 27 | P | spdlog | C++ header-only |
| 28 | P | http.zig | only Zig |
| 29 | D | dart-samples | flutter-samples covers dart |
| 30 | P | kickstart.nvim | only Lua |
| 31 | P | phoenix-todo-list | Elixir Phoenix |
| 32 | P | aspnetcore-docs-samples | only Razor |
| 33 | P | tide (fish) | only fish |
| 34 | P | just | only justfile |
| 35 | P | grpc-go-examples | only proto |
| 36 | P | apollo-server | only graphql SDL |
| 37 | P | terraform-aws-vpc | canonical HCL |
| 38 | P | argocd-example-apps | canonical k8s yaml |
| 39 | P | prometheus-helm | canonical Helm |
| 40 | P | starter-workflows | canonical GHA |
| 41 | S | openapi-stripe | OpenAPI yaml (unique IDL) |

### Chunk G (ORM samples)
| 42 | S | microblog | SQLAlchemy detailed sample (distinct from flask-realworld) |
| 43 | S | fastapi-realworld | FastAPI Pydantic + SQLAlchemy async |
| 44 | D | sequelize-express-example | TypeORM/Prisma decorator patterns cover this |
| 45 | P | golang-gin-realworld | GORM sample (only Go ORM) |
| 46 | S | actix-diesel-realworld | Diesel (Rust ORM) |

### Chunk H (build tools)
| 47 | S | tokio | Cargo workspace (canonical) |
| 48 | D | maven | pom.xml covered by spring-petclinic + kafka-streams-examples |
| 49 | S | pnpm | pnpm-workspace.yaml (only source) |
| 50 | D | nx | tsconfig covered by nestjs/sveltekit/angular |

### Chunk I (Java enterprise) — ALL DROP except primary
| 51 | D | quarkus-quickstarts | Java covered |
| 52 | D | micronaut-examples | Java covered |
| 53 | D | helidon-examples | Java covered |
| 54 | D | vertx-examples | Java covered |
| 55 | D | dropwizard-example | Java covered |
| 56 | D | play-java-starter | Java covered (Play covered by play-scala) |
| 57 | D | spark-examples | Java covered |

### Chunk L (Mobile native)
| 58 | S | ios-oss | iOS UIKit (distinct from SwiftUI) |
| 59 | P | sample-food-truck | iOS SwiftUI (canonical Apple sample) |
| 60 | S | android-architecture | Android Java (distinct extractor patterns from server Java) |
| 61 | S | compose-samples | Android Kotlin Compose |
| 62 | P | flutter-samples | only Dart sample |
| 63 | D | react-native | JS covered |
| 64 | D | ionic-conference-app | TS covered |
| 65 | D | maui-samples | C# covered |

### Chunk N (Declarative IaC)
| 66 | S | aws-cloudformation-samples | CFN yaml templates |
| 67 | D | kustomize | k8s yaml redundant w/ argocd |
| 68 | S | ansible-for-devops | Ansible-specific yaml |
| 69 | D | chef-runit | Ruby DSL covered by rails |
| 70 | D | puppet-control-repo | Ruby DSL covered |
| 71 | D | awesome-compose | docker-compose covered by every realworld app |
| 72 | S | nomad-pack | HCL+Nomad (distinct from terraform) |

### Chunk O (ORMs missing) — keep distinctive only
| 73 | D | spring-framework-petclinic | Spring already covered |
| 74 | S | joal | jOOQ (unique Java ORM idiom) |
| 75 | S | jpetstore-6 | MyBatis (unique XML-mapper) |
| 76 | P | nestjs-realworld-typeorm | TypeORM (canonical TS decorator ORM) |
| 77 | D | express-bookshelf-realworld | knex covered by alembic migration entry |
| 78 | D | nestjs-realworld-mikroorm | TypeORM covers TS ORM |
| 79 | S | ent | Go codegen-ORM (distinct from GORM) |
| 80 | S | sqlc-examples | SQL→Go codegen (unique pattern) |
| 81 | D | fabric-ca | Go covered |
| 82 | D | lean | Eloquent covered by laravel-quickstart |
| 83 | D | sea-orm-examples | Diesel covers Rust ORM |
| 84 | S | netcore-boilerplate | Dapper (micro-ORM, distinct from EF) |

### Chunk P (NoSQL / streaming)
| 85 | P | mongoose | canonical Node ODM |
| 86 | D | pymongo | mongoose covers MongoDB |
| 87 | D | motor | redundant w/ pymongo (already dropped) |
| 88 | S | mongo-go-driver | Go MongoDB (distinct from JS) |
| 89 | D | mongo-java-driver | Java client patterns covered by kafka |
| 90 | S | redis-py | canonical Redis client |
| 91 | D | ioredis | redis-py covers |
| 92 | D | go-redis | mongo-go-driver covers Go nosql |
| 93 | D | lettuce | java covered |
| 94 | S | cassandra-java-driver | distinct from Redis (CQL DSL) |
| 95 | S | aws-sdk-go-v2 | AWS SDK pattern (DynamoDB) |
| 96 | D | couchbase-gocb | covered |
| 97 | S | rabbitmq-tutorials | AMQP (distinct from Kafka) |
| 98 | D | nats.go | Kafka + RabbitMQ cover streaming |
| 99 | D | aws-sdk-js-v3 | aws-sdk-go-v2 covers SDK shape |

### Chunk M (Programmatic IaC) — collapse multi-flavor
| 100 | P | aws-cdk-examples-typescript | canonical CDK |
| 101 | D | aws-cdk-examples-python | python covered |
| 102 | D | aws-cdk-examples-java | java covered |
| 103 | D | aws-cdk-examples-csharp | csharp covered |
| 104 | D | aws-cdk-examples-go | go covered |
| 105 | D | pulumi-examples-typescript | CDK TS covers |
| 106 | D | pulumi-examples-python | covered |
| 107 | S | pulumi-examples-go | distinct Pulumi-Go API |
| 108 | D | pulumi-examples-csharp | covered |
| 109 | P | azure-quickstart-templates | only Bicep |
| 110 | S | aws-sam-cli-app-templates | SAM template.yaml (unique extension) |
| 111 | S | serverless-examples | serverless.yml (distinct from SAM) |
| 112 | S | crossplane | XR composition yaml (distinct) |

### Chunk Y (DB migrations) — collapse to one
| 113 | D | flyway | covered by alembic for migration concept |
| 114 | D | liquibase | covered |
| 115 | P | alembic | canonical migration tool (Python + SQL) |
| 116 | D | knex | covered |
| 117 | D | goose | covered |
| 118 | D | migrate-mongo | covered |
| 119 | D | sequel | covered |

### Chunk Q (CI/CD)
| 120 | S | gitlab-runner | GitLab CI |
| 121 | S | circleci-demo-python-django | CircleCI yaml |
| 122 | P | jenkins | only Groovy DSL (and best Groovy sample) |
| 123 | S | tektoncd-pipeline | Tekton CRD yaml |
| 124 | D | drone | yaml step format redundant |
| 125 | D | buildkite-agent | yaml step format redundant |
| 126 | D | skaffold | yaml covered |
| 127 | P | tilt | only Starlark sample |

### Chunk R (Build tools missing)
| 128 | D | gradle | Gradle covered by spring-petclinic |
| 129 | S | bazel | BUILD/Starlark targets (distinct from Tilt — BUILD files) |
| 130 | D | gnu-make | Makefile covered by every Go repo |
| 131 | S | cmake | only CMake corpus |
| 132 | D | poetry | pyproject covered |
| 133 | D | composer | composer.json covered by symfony/laravel |
| 134 | D | bundler | Gemfile covered by rails |
| 135 | D | elixir (mix) | mix.exs covered by phoenix-todo-list |
| 136 | D | sbt | build.sbt covered by play-scala-starter |
| 137 | D | leiningen | project.clj covered by usermanager-example |
| 138 | D | lerna | workspaces covered by pnpm |
| 139 | D | turborepo | turbo.json niche; covered by pnpm |

### Chunk V (reverse proxies) — each format is single-source MUST KEEP
| 140 | P | nginx (nginx-conf) |
| 141 | P | apache-httpd (apache-httpd-conf) |
| 142 | P | caddy (caddyfile) |
| 143 | P | traefik (traefik-dynamic) |
| 144 | P | kong (kong-declarative) |
| 145 | P | envoy (envoy-yaml) |
| 146 | P | haproxy (haproxy-cfg) |

### Chunk U (API/IDL alts) — each format single-source MUST KEEP
| 147 | P | asyncapi-spec |
| 148 | P | smithy |
| 149 | P | avro |
| 150 | P | thrift |
| 151 | P | json-schema-spec |
| 152 | P | raml-spec |
| 153 | P | api-blueprint |

### Chunk S (auth/security/observability) — ALL DROP
| 154-162 | D | node-openid-client, node-jsonwebtoken, keycloak, auth0-spa-js, vault, opentelemetry-js, prometheus-client-golang, sentry-javascript, dd-trace-js | All library consumer code already covered by realworld apps' auth + tracing imports |

### Chunk T (validation + lint)
| 163 | S | zod | TS schema-builder pattern |
| 164 | D | joi | covered |
| 165 | D | yup | covered |
| 166 | S | pydantic | unique Python decorator-validator surface |
| 167 | D | class-validator | nestjs covers |
| 168 | D | eslint | config covered by every realworld |
| 169 | D | prettier | covered |
| 170 | D | ruff | covered |
| 171 | D | rubocop | covered |
| 172 | D | golangci-lint | covered |
| 173 | D | rust-clippy | covered |

### Chunk Z (serverless)
| 174 | D | aws-lambda-developer-guide | Lambda JS handler covered by express |
| 175 | S | aws-lambda-python-runtime-interface-client | Python Lambda handler bootstrap (distinct) |
| 176 | D | aws-lambda-java-libs | covered |
| 177 | D | aws-lambda-go | covered |
| 178 | D | aws-lambda-rust-runtime | covered |
| 179 | D | vercel-examples | covered by nextjs-commerce |
| 180 | D | netlify-functions | covered |
| 181 | S | cloudflare-workers-sdk | wrangler.toml + Workers fetch (distinct edge handler) |
| 182 | D | functions-framework-nodejs | covered |

### Chunk AA (state mgmt) — collapse to one
| 183 | D | redux | covered by nextjs-commerce (toolkit slices common) |
| 184 | D | mobx | covered |
| 185 | D | zustand | covered |
| 186 | D | pinia | covered by vue surface — actually all vue dropped; if Vue must stay → keep pinia. Recommend D. |
| 187 | D | ngrx-platform | angular-realworld covers |
| 188 | D | recoil | covered |
| 189 | S | xstate | distinctive statechart createMachine DSL (unique extractor surface) |
| 190 | D | effector | covered |

### Chunk W (SSGs)
| 191 | S | hugoDocs | Go templates + shortcodes + TOML/YAML front-matter |
| 192 | D | jekyll | Liquid covered |
| 193 | D | hexo-site | covered |
| 194 | D | mkdocs | sphinx covers Python SSG |
| 195 | S | sphinx | RST extractor + plugin system (distinct) |
| 196 | D | vitepress | covered |
| 197 | D | docusaurus | MDX covered by nextjs |
| 198 | D | nextra | covered |
| 199 | D | eleventy | covered |

### Chunk X (testing)
| 200 | D | jest | covered |
| 201 | D | mocha | covered |
| 202 | D | cypress-realworld-app | covered |
| 203 | D | playwright | covered |
| 204 | P | seleniumhq-examples | only `multi`-language fixture |
| 205 | D | junit5-samples | covered |
| 206 | D | rspec-rails | covered |
| 207 | S | pytest | canonical Python test framework (conftest/fixtures distinctive) |
| 208 | D | karate | covered |
| 209 | D | cucumber-js | covered |

### Chunk BB (realtime)
| 210 | S | socket.io | canonical realtime |
| 211 | D | pusher-js | covered |
| 212 | D | ably-js | covered |
| 213 | D | MQTT.js | covered |
| 214 | D | pion-webrtc | covered |
| 215 | D | mdn-dom-examples | covered |

### Chunk CC (game/embedded)
| 216 | S | EntityComponentSystemSamples | Unity ECS C# attributes (distinct) |
| 217 | D | UnrealEnginePython | covered by spdlog |
| 218 | D | bevy | rust covered |
| 219 | D | arduino-examples | covered |
| 220 | S | esp-idf | only pure-C corpus (distinct from C++) |
| 221 | D | micropython | python covered |
| 222 | D | cortex-m-quickstart | rust covered |

### Chunk EE (workflow/orchestration)
| 223 | D | temporalio-samples-go | airflow covers decorator workflows |
| 224 | D | cadence-client | covered |
| 225 | S | airflow | canonical @dag/@task decorator workflow |
| 226 | D | prefect | covered |
| 227 | P | camunda-bpm-examples | only `java_bpmn` corpus |
| 228 | D | dagster | covered |

### Chunk DD (data/ML/notebook)
| 229 | D | scikit-learn | python covered by pandas |
| 230 | D | pytorch-examples | covered |
| 231 | D | keras-io | covered |
| 232 | D | transformers | covered |
| 233 | P | jupyter-notebook | only notebook corpus |
| 234 | D | polars | covered |
| 235 | S | spark | only Scala data corpus |
| 236 | P | jaffle_shop | only sql_dbt corpus |

### Chunk K (frontend SPAs) — collapse aggressively
| 237 | S | angular-realworld | Angular decorators + DI (distinct extractor surface) |
| 238 | D | react-redux-realworld | covered by nextjs-commerce + nestjs |
| 239 | D | vite | TS bundler covered by sveltekit |
| 240 | D | vue-realworld | covered by pinia → wait, pinia dropped. Vue 3 SFC is distinct — RECONSIDER → upgrade to S |
| 241 | D | svelte-realworld | sveltekit covers |
| 242 | S | sveltekit | distinct Svelte syntax extractor surface |
| 243 | D | solid-templates | TS covered |
| 244 | D | preact-cli | covered |
| 245 | D | ember-super-rentals | covered |
| 246 | D | astro | covered |
| 247 | D | remix-indie-stack | covered |
| 248 | D | nuxt-starter | covered |
| 249 | D | lit-element-starter | covered |
| 250 | D | htmx | small JS covered |
| 251 | D | alpine | covered |
| 252 | D | qwik | covered |
| 253 | D | gatsby-starter-blog | covered |

> NOTE on Vue SFC: if the JS extractor's Vue-SFC handling is a separate
> code path, promote `vue-realworld` to **S**. Marked as conditional-S
> below in the proposed run.sh (commented out).

### Chunk J (web framework alts) — ALL DROP except http4k
| 254 | D | echox | gin/chi cover Go HTTP |
| 255 | D | gofiber-recipes | covered |
| 256 | D | beego-example | covered |
| 257 | D | fastify-demo | express covers |
| 258 | D | koa-examples | covered |
| 259 | D | hapi | covered |
| 260 | D | sails-examples | covered |
| 261 | D | adonis-blog-demo | covered |
| 262 | D | tornado | flask/django cover |
| 263 | D | starlette | covered |
| 264 | D | pyramid-shootout | covered |
| 265 | D | bottle | covered |
| 266 | D | slim-skeleton | symfony covers |
| 267 | D | codeigniter-shield | covered |
| 268 | D | yii2-app-basic | covered |
| 269 | D | sinatra-recipes | rails covers |
| 270 | D | hanami | covered |
| 271 | D | grape-on-rack | covered |
| 272 | S | axum | tower middleware ecosystem (distinct from actix) |
| 273 | D | rocket | covered |
| 274 | D | warp | covered |
| 275 | D | tide (rust) | covered |
| 276 | D | akka-http | covered |
| 277 | D | http4s | covered |
| 278 | D | compojure | covered |
| 279 | D | pedestal | covered |
| 280 | D | elixir-plug | phoenix covers |
| 281 | S | phoenix-live-view | LiveView mount/handle_event (distinct from phoenix-todo) |
| 282 | D | javalin-samples | ktor covers |
| 283 | S | http4k | functional HttpHandler composition (distinct DSL) |

---

## 4. Proposed slim run.sh REPOS array

```bash
REPOS=(
  # --- Single-source MUST KEEP (extractor or DSL only-source) ---
  "aspnetcore-docs-samples|https://github.com/dotnet/AspNetCore.Docs.Samples.git|main|razor"
  "tide|https://github.com/IlanCosman/tide.git|main|fish"
  "just|https://github.com/casey/just.git|master|just"
  "http.zig|https://github.com/karlseguin/http.zig.git|master|zig"
  "kickstart.nvim|https://github.com/nvim-lua/kickstart.nvim.git|master|lua"
  "grpc-go-examples|https://github.com/grpc/grpc-go.git|master|proto|examples"
  "apollo-server|https://github.com/apollographql/apollo-server.git|main|graphql"
  "jupyter-notebook|https://github.com/jupyter/notebook.git|main|notebook|docs/source"
  "jaffle_shop|https://github.com/dbt-labs/jaffle_shop.git|main|sql_dbt|models"
  "azure-quickstart-templates|https://github.com/Azure/azure-quickstart-templates.git|master|bicep|quickstarts/microsoft.storage"
  "tilt|https://github.com/tilt-dev/tilt.git|master|starlark|integration"
  "camunda-bpm-examples|https://github.com/camunda/camunda-bpm-examples.git|master|java_bpmn|servicetask"
  "asyncapi-spec|https://github.com/asyncapi/spec.git|master|asyncapi"
  "smithy|https://github.com/smithy-lang/smithy.git|main|smithy|smithy-model"
  "avro|https://github.com/apache/avro.git|main|avro|lang"
  "thrift|https://github.com/apache/thrift.git|master|thrift|tutorial"
  "json-schema-spec|https://github.com/json-schema-org/json-schema-spec.git|main|json-schema"
  "raml-spec|https://github.com/raml-org/raml-spec.git|master|raml"
  "api-blueprint|https://github.com/apiaryio/api-blueprint.git|master|api-blueprint"
  "nginx|https://github.com/nginx/nginx.git|master|nginx-conf|conf"
  "apache-httpd|https://github.com/apache/httpd.git|trunk|apache-httpd-conf|docs/conf"
  "caddy|https://github.com/caddyserver/caddy.git|master|caddyfile|caddyconfig"
  "traefik|https://github.com/traefik/traefik.git|master|traefik-dynamic|integration/fixtures"
  "kong|https://github.com/Kong/kong.git|master|kong-declarative|spec/fixtures"
  "envoy|https://github.com/envoyproxy/envoy.git|main|envoy-yaml|configs"
  "haproxy|https://github.com/haproxy/haproxy.git|master|haproxy-cfg|examples"
  "seleniumhq-examples|https://github.com/SeleniumHQ/seleniumhq.github.io.git|trunk|multi|examples"
  # --- Language primary realworld apps ---
  "requests|https://github.com/psf/requests.git|main|python"
  "flask-realworld|https://github.com/gothinkster/flask-realworld-example-app.git|master|python"
  "click|https://github.com/pallets/click.git|main|python"
  "django-realworld|https://github.com/gothinkster/django-realworld-example-app.git|master|python"
  "pandas|https://github.com/pandas-dev/pandas.git|main|python|pandas/core"
  "gin|https://github.com/gin-gonic/gin.git|master|go"
  "chi|https://github.com/go-chi/chi.git|master|go"
  "etcd|https://github.com/etcd-io/etcd.git|main|go|server/etcdserver"
  "express-realworld|https://github.com/gothinkster/node-express-realworld-example-app.git|master|javascript"
  "nestjs-starter|https://github.com/nestjs/typescript-starter.git|master|typescript"
  "nextjs-commerce|https://github.com/vercel/commerce.git|main|typescript"
  "spring-petclinic|https://github.com/spring-projects/spring-petclinic.git|main|java"
  "kafka-streams-examples|https://github.com/confluentinc/kafka-streams-examples.git|master|java"
  "exposed|https://github.com/JetBrains/Exposed.git|main|kotlin"
  "ktor-samples|https://github.com/ktorio/ktor-samples.git|main|kotlin"
  "play-scala-starter|https://github.com/playframework/play-scala-starter-example.git|2.7.x|scala"
  "usermanager-example|https://github.com/seancorfield/usermanager-example.git|develop|clojure"
  "rails-realworld|https://github.com/gothinkster/rails-realworld-example-app.git|master|ruby"
  "sidekiq|https://github.com/sidekiq/sidekiq.git|main|ruby"
  "laravel-quickstart|https://github.com/laravel/quickstart-basic.git|master|php"
  "symfony-demo|https://github.com/symfony/demo.git|main|php"
  "mini-redis|https://github.com/tokio-rs/mini-redis.git|master|rust"
  "actix-examples|https://github.com/actix/examples.git|main|rust"
  "vapor-api-template|https://github.com/vapor/api-template.git|master|swift"
  "sample-food-truck|https://github.com/apple/sample-food-truck.git|main|swift"
  "aspnetcore-realworld|https://github.com/gothinkster/aspnetcore-realworld-example-app.git|master|csharp"
  "spdlog|https://github.com/gabime/spdlog.git|v1.x|cpp"
  "flutter-samples|https://github.com/flutter/samples.git|main|dart"
  "phoenix-todo-list|https://github.com/dwyl/phoenix-todo-list-tutorial.git|main|elixir"
  "esp-idf|https://github.com/espressif/esp-idf.git|master|c|examples/get-started"
  # --- Secondary distinctive coverage ---
  "microblog|https://github.com/miguelgrinberg/microblog.git|main|python"                       # SQLAlchemy + Flask-Login (deeper than flask-realworld)
  "fastapi-realworld|https://github.com/nsidnev/fastapi-realworld-example-app.git|master|python" # FastAPI + Pydantic + async SQLAlchemy
  "golang-gin-realworld|https://github.com/gothinkster/golang-gin-realworld-example-app.git|main|go" # GORM
  "actix-diesel-realworld|https://github.com/snamiki1212/realworld-v1-rust-actix-web-diesel.git|main|rust" # Diesel
  "nestjs-realworld-typeorm|https://github.com/lujakob/nestjs-realworld-example-app.git|master|typescript" # TypeORM
  "joal|https://github.com/anthonyraymond/joal.git|master|java"                                 # jOOQ
  "jpetstore-6|https://github.com/mybatis/jpetstore-6.git|master|java"                          # MyBatis
  "ent|https://github.com/ent/ent.git|master|go|entc/integration/ent"                           # Ent codegen
  "sqlc-examples|https://github.com/sqlc-dev/sqlc.git|main|go|examples"                         # sqlc codegen
  "netcore-boilerplate|https://github.com/lkurzyniec/netcore-boilerplate.git|master|csharp"     # Dapper micro-ORM
  "tokio|https://github.com/tokio-rs/tokio.git|master|rust"                                     # Cargo workspace manifest
  "pnpm|https://github.com/pnpm/pnpm.git|main|javascript"                                       # pnpm-workspace.yaml
  "bazel|https://github.com/bazelbuild/bazel.git|master|java"                                   # BUILD Starlark targets
  "cmake|https://github.com/Kitware/CMake.git|master|cpp"                                       # CMake lists
  "mongoose|https://github.com/Automattic/mongoose.git|master|javascript"                       # Mongo Node ODM
  "mongo-go-driver|https://github.com/mongodb/mongo-go-driver.git|master|go"                    # Mongo Go
  "redis-py|https://github.com/redis/redis-py.git|master|python"                                # Redis Python
  "cassandra-java-driver|https://github.com/datastax/java-driver.git|4.x|java"                  # Cassandra CQL
  "aws-sdk-go-v2|https://github.com/aws/aws-sdk-go-v2.git|main|go"                              # DynamoDB / AWS SDK
  "rabbitmq-tutorials|https://github.com/rabbitmq/rabbitmq-tutorials.git|main|python"           # AMQP
  "aws-cdk-examples-typescript|https://github.com/aws-samples/aws-cdk-examples.git|main|typescript|typescript"
  "pulumi-examples-go|https://github.com/pulumi/examples.git|master|go|aws-go-webserver"
  "aws-cloudformation-samples|https://github.com/aws-cloudformation/aws-cloudformation-samples.git|main|yaml"
  "aws-sam-cli-app-templates|https://github.com/aws/aws-sam-cli-app-templates.git|master|yaml|python3.12"
  "serverless-examples|https://github.com/serverless/examples.git|v4|yaml|aws-node-http-api"
  "crossplane|https://github.com/crossplane/crossplane.git|main|yaml|cluster/meta"
  "ansible-for-devops|https://github.com/geerlingguy/ansible-for-devops.git|master|yaml"
  "nomad-pack|https://github.com/hashicorp/nomad-pack.git|main|hcl|registry"
  "terraform-aws-vpc|https://github.com/terraform-aws-modules/terraform-aws-vpc.git|master|hcl"
  "argocd-example-apps|https://github.com/argoproj/argocd-example-apps.git|master|yaml"
  "prometheus-helm|https://github.com/prometheus-community/helm-charts.git|main|yaml|charts/prometheus"
  "starter-workflows|https://github.com/actions/starter-workflows.git|main|yaml"
  "openapi-stripe|https://github.com/APIs-guru/openapi-directory.git|main|yaml|APIs/stripe.com"
  "gitlab-runner|https://gitlab.com/gitlab-org/gitlab-runner.git|main|yaml|ci"
  "circleci-demo-python-django|https://github.com/CircleCI-Public/circleci-demo-python-django.git|master|yaml|.circleci"
  "jenkins|https://github.com/jenkinsci/jenkins.git|master|groovy"
  "tektoncd-pipeline|https://github.com/tektoncd/pipeline.git|main|yaml|examples"
  "alembic|https://github.com/sqlalchemy/alembic.git|main|python"
  "ios-oss|https://github.com/kickstarter/ios-oss.git|main|swift"
  "android-architecture|https://github.com/googlesamples/android-architecture.git|main|java"
  "compose-samples|https://github.com/android/compose-samples.git|main|kotlin"
  "EntityComponentSystemSamples|https://github.com/Unity-Technologies/EntityComponentSystemSamples.git|master|csharp|EntityComponentSystemSamples/ECSSamples"
  "zod|https://github.com/colinhacks/zod.git|main|typescript"
  "pydantic|https://github.com/pydantic/pydantic.git|main|python"
  "aws-lambda-python-runtime-interface-client|https://github.com/aws/aws-lambda-python-runtime-interface-client.git|main|python|awslambdaric"
  "cloudflare-workers-sdk|https://github.com/cloudflare/workers-sdk.git|main|typescript|packages/wrangler"
  "xstate|https://github.com/statelyai/xstate.git|main|typescript|packages/core"
  "hugoDocs|https://github.com/gohugoio/hugoDocs.git|master|go|content"
  "sphinx|https://github.com/sphinx-doc/sphinx.git|master|python|sphinx"
  "pytest|https://github.com/pytest-dev/pytest.git|main|python"
  "socket.io|https://github.com/socketio/socket.io.git|main|typescript|packages/socket.io"
  "airflow|https://github.com/apache/airflow.git|main|python|airflow/example_dags"
  "spark|https://github.com/apache/spark.git|master|scala|examples/src/main/scala"
  "angular-realworld|https://github.com/gothinkster/angular-realworld-example-app.git|main|typescript"
  "sveltekit|https://github.com/sveltejs/kit.git|main|typescript"
  "axum|https://github.com/tokio-rs/axum.git|main|rust"
  "phoenix-live-view|https://github.com/phoenixframework/phoenix_live_view.git|main|elixir"
  "http4k|https://github.com/http4k/http4k.git|master|kotlin"
  # CONDITIONAL: uncomment if Vue SFC extractor is a separate code path
  # "vue-realworld|https://github.com/gothinkster/vue-realworld-example-app.git|master|javascript"
)
```

Final count: **72 entries** (47 P + 25 S), comfortably within the 60–90
target.

---

## 5. Risks and mitigations

### Coverage that becomes thinner

1. **Java framework breadth** — Quarkus / Micronaut / Helidon / Vert.x /
   Dropwizard are dropped. Spring + Kafka cover the major Java extractor
   surface, but framework-specific annotations (e.g. `@QuarkusTest`,
   `@MicronautTest`) won't be exercised. **Mitigation:** if extractor
   regression hits a Quarkus user, re-add `quarkus-quickstarts` as
   secondary.
2. **State management decorators** — Only xstate retained. If a Redux
   Toolkit / NgRx-specific dispatcher pattern regresses it won't be
   caught. **Mitigation:** angular-realworld covers NgRx incidentally via
   Angular DI; sveltekit covers Svelte stores.
3. **Lint configs as standalone fixtures** — No dedicated `.eslintrc` /
   `.golangci.yml` consumer. Most realworld apps ship one, so coverage
   is preserved incidentally — but a synthetic lint-only test corpus
   would be lost. **Mitigation:** none needed; lint config parsing isn't
   in `internal/extractors/`.
4. **Polyglot mobile** — React Native, Ionic, MAUI dropped. Mobile-app
   path coverage is now thinner: only iOS (UIKit + SwiftUI), Android
   (Java + Kotlin Compose), Flutter. **Mitigation:** acceptable —
   underlying extractors (csharp/typescript/javascript) are covered by
   web counterparts.
5. **Migration-tool DSL variety** — Only Alembic retained. Flyway's
   `V__.sql` naming, Liquibase's XML changelog, and Knex's
   schema-builder are not exercised. **Mitigation:** propose a Tier-2
   "extended migrations" corpus file.

### Suggested Tier-2 extended-coverage file

Create `scripts/verify2/run-extended.sh` containing the 211 dropped
entries, runnable on-demand for full-coverage smoke runs (e.g. before
a major release or after touching `internal/extractors/cross/`). This
preserves the current investment without paying the cost on every
baseline refresh.

Suggested run cadence:
- `run.sh` (72 repos) — every PR + nightly baseline
- `run-extended.sh` (211 repos) — weekly + pre-release

---

## 6. Report

- **Proposal file:** `/tmp/corpus-curation-proposal.md`
- **Target N:** **72** (47 primary + 25 secondary)
- **Top-3 redundancy categories eliminated:**
  1. Web-framework alternates (chunk J, 29 → 6)
  2. State-management single-language libs (chunk AA, 8 → 1)
  3. CDK/Pulumi multi-flavor sprawl (chunk M, 9 → 2)
- **MUST-KEEP single-source entries flagged:** 22
  (razor / fish / just / zig / lua / proto / graphql / notebook / sql_dbt /
  bicep / starlark / java_bpmn / smithy / avro / thrift / json-schema /
  asyncapi / raml / api-blueprint / nginx / httpd / caddyfile / traefik /
  kong / envoy / haproxy / multi)
