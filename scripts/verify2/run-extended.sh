#!/usr/bin/env bash
# scripts/verify2/run-extended.sh
#
# VERIFY-2 EXTENDED corpus harness (Refs #44, #87, #96).
#
# This is the TIER-2 / extended-coverage sibling of scripts/verify2/run.sh.
# It re-runs the 168 corpus entries that the 2026-05-19 curation moved out
# of the per-change ship-gate. Use it pre-release or on a weekly cron — the
# primary scripts/verify2/run.sh ship-gate corpus is the fast 114-entry
# tier-1 set and is the one that gates every change.
#
# Why two tiers:
#   - Most dropped entries are intra-family redundancies (e.g. 8 state-mgmt
#     libs, 12 web-framework alternates per language, 5 CDK flavors). They
#     do not exercise new extractor code paths; running them every change
#     burned wall-clock without changing the bug-rate signal.
#   - But they DO catch framework-specific annotation surfaces (Quarkus
#     `@QuarkusTest`, NgRx dispatchers, Liquibase changelog XML, etc.) so
#     we keep them around for pre-release safety.
#
# See docs/verify2/corpus-curation-2026-05-19.md for the full per-entry
# decision log. Hard rule: this script reuses the run.sh harness verbatim
# except for the REPOS array.
#
# Usage:
#   scripts/verify2/run-extended.sh
#
# Env vars: same as run.sh (ARCHIGRAPH_CORPORA_DIR, ARCHIGRAPH_BIN,
# ARCHIGRAPH_VERIFY2_TIMEOUT, ARCHIGRAPH_VERBOSE).
set -euo pipefail

CORPORA_DIR="${ARCHIGRAPH_CORPORA_DIR:-$HOME/Documents/Projects/archigraph-corpora}"
REPORTS_DIR="$CORPORA_DIR/_reports"
mkdir -p "$CORPORA_DIR" "$REPORTS_DIR"

# Repo list. Same entry format as run.sh:
#   <name>|<git-url>|<ref>|<primary-language>[|<sparse-path>]
# These are the 168 entries dropped from the tier-1 ship-gate by the
# 2026-05-19 curation (see docs/verify2/corpus-curation-2026-05-19.md).
REPOS=(
  # Corpus policy (Refs #96): prefer SAMPLE APPLICATIONS that USE a framework
  # over the framework's own source tree. We measure how the indexer handles
  # framework-using user code, not how it handles framework internals.
  # --- Python ---
  # --- Go ---
  # --- JavaScript / TypeScript ---
  # --- Java ---
  # --- Kotlin ---
  # --- Scala ---
  # --- Groovy ---
  "ratpack-example-books|https://github.com/ratpack/example-books.git|master|groovy"               # Ratpack sample app
  # --- Clojure ---
  # --- Ruby ---
  # --- PHP ---
  # --- Rust ---
  # --- Swift ---
  # --- C# ---
  # --- C++ ---
  # --- Zig ---
  # --- Dart ---
  "dart-samples|https://github.com/dart-lang/samples.git|main|dart"                               # Dart sample apps
  # --- Lua ---
  # --- Elixir ---
  # --- Razor ---
  # --- Fish ---
  # --- Just ---
  # --- Proto ---
  # --- GraphQL ---
  # --- HCL ---
  # --- K8s YAML ---
  # --- Helm ---
  # --- GHA ---
  # --- OpenAPI ---
  # --- ORMs/Frameworks (chunk G, umbrella #301) ---
  # Sample apps that USE each ORM/framework, per Refs #96 corpus policy.
  # django-realworld (#176) and node-express-realworld (#178/Prisma) are already
  # listed above under Python and JavaScript respectively — not duplicated here.
  "sequelize-express-example|https://github.com/sequelize/express-example.git|master|javascript"   # Sequelize sample app (#177)
  # --- Build tools (chunk H, umbrella #309) ---
  # Manifest fixtures: each repo's root Cargo.toml / pom.xml / package.json /
  # tsconfig is the canonical artifact for the cross/manifest extractor.
  "maven|https://github.com/apache/maven.git|master|java"                                            # pom.xml multi-module manifest (#170)
  "nx|https://github.com/nrwl/nx.git|master|typescript"                                              # tsconfig + nx.json manifests (#173)
  # --- Java enterprise (chunk I, umbrella #302) ---
  # Sample apps that USE each Java enterprise framework, per Refs #96 corpus policy.
  "quarkus-quickstarts|https://github.com/quarkusio/quarkus-quickstarts.git|main|java"                # Quarkus sample apps (#181)
  "micronaut-examples|https://github.com/micronaut-projects/micronaut-examples.git|master|java"       # Micronaut sample apps (#183)
  "helidon-examples|https://github.com/helidon-io/helidon-examples.git|helidon-4.x|java"              # Helidon sample apps (#186)
  "vertx-examples|https://github.com/vert-x3/vertx-examples.git|5.x|java"                             # Vert.x sample apps (#190)
  "dropwizard-example|https://github.com/dropwizard/dropwizard.git|release/5.0.x|java|dropwizard-example" # Dropwizard sample app subtree (#193)
  "play-java-starter|https://github.com/playframework/play-java-starter-example.git|2.7.x|java"       # Play Framework Java sample app (#197)
  "spark-examples|https://github.com/perwendel/spark.git|master|java|examples"                        # Spark Java examples subtree (#203)
  # --- Mobile native (chunk L, umbrella #313) ---
  # Sample applications across the major mobile native stacks, per Refs #96
  # corpus policy. Each entry pinned to the SHA recorded in its child issue.
  "react-native|https://github.com/facebook/react-native.git|main|javascript|template"                 # React Native: template/ subtree per umbrella body (#212)
  "ionic-conference-app|https://github.com/ionic-team/ionic-conference-app.git|main|typescript"        # Ionic / Capacitor sample app (#215)
  "maui-samples|https://github.com/dotnet/maui-samples.git|main|csharp"                                # .NET MAUI sample apps (#217)
  # --- Declarative IaC (chunk N, umbrella #308) ---
  # Sample/canonical fixtures for declarative IaC + container orchestration
  # languages, per Refs #96 corpus policy. Each entry pinned to the SHA
  # recorded in its child issue. ArgoCD (#211) is intentionally NOT re-listed
  # here: argocd-example-apps is already present above under K8s YAML
  # (chunk F) and #211 closes via that single fixture.
  "kustomize|https://github.com/kubernetes-sigs/kustomize.git|master|yaml|examples"                            # Kustomize examples/ subtree (#189)
  "chef-runit|https://github.com/chef-cookbooks/runit.git|main|ruby"                                           # Chef cookbook (Ruby DSL) (#195)
  "puppet-control-repo|https://github.com/puppetlabs/control-repo.git|production|ruby"                         # Puppet control repo (Ruby DSL) (#199)
  "awesome-compose|https://github.com/docker/awesome-compose.git|master|yaml"                                  # Docker Compose canonical samples (#204)
  # --- Relational ORMs missing (chunk O, umbrella #315) ---
  # Sample apps that USE each relational ORM / query builder, per Refs #96
  # corpus policy. Each entry pinned to the SHA recorded in its child issue.
  "spring-framework-petclinic|https://github.com/spring-petclinic/spring-framework-petclinic.git|main|java"            # Hibernate-only sample app (#251)
  "express-bookshelf-realworld|https://github.com/tanem/express-bookshelf-realworld-example-app.git|master|javascript" # Knex sample app (#269)
  "nestjs-realworld-mikroorm|https://github.com/mikro-orm/nestjs-realworld-example-app.git|master|typescript"          # MikroORM sample app (#274)
  "fabric-ca|https://github.com/hyperledger/fabric-ca.git|main|go"                                                     # sqlx (Go) sample app (#287)
  "lean|https://github.com/jenssegers/lean.git|master|php"                                                             # Eloquent (standalone) sample app (#288)
  "sea-orm-examples|https://github.com/SeaQL/sea-orm.git|master|rust|examples"                                         # SeaORM examples/ subtree (#289)
  # --- NoSQL/cache/streaming (chunk P, umbrella #316) ---
  # Canonical client/driver fixtures for the major NoSQL, cache, and streaming
  # ecosystems, per Refs #96 corpus policy. Each entry pinned to the SHA
  # recorded in its child issue.
  "pymongo|https://github.com/mongodb/mongo-python-driver.git|master|python"                                  # MongoDB Python driver (#216)
  "motor|https://github.com/mongodb/motor.git|master|python"                                                  # MongoDB async Python driver (#219)
  "mongo-java-driver|https://github.com/mongodb/mongo-java-driver.git|main|java"                              # MongoDB Java driver (#221)
  "ioredis|https://github.com/redis/ioredis.git|main|typescript"                                              # Redis Node.js TypeScript client (#223)
  "go-redis|https://github.com/redis/go-redis.git|master|go"                                                  # Redis Go client (#225)
  "lettuce|https://github.com/redis/lettuce.git|main|java"                                                    # Redis Java client (#226)
  "couchbase-gocb|https://github.com/couchbase/gocb.git|master|go"                                            # Couchbase Go SDK (#232)
  "nats.go|https://github.com/nats-io/nats.go.git|main|go"                                                    # NATS Go client (#236)
  "aws-sdk-js-v3|https://github.com/aws/aws-sdk-js-v3.git|main|typescript"                                    # AWS SQS / AWS JS SDK v3 (#237)
  # --- Programmatic IaC (chunk M, umbrella #314) ---
  # Programmatic IaC samples across CDK / Pulumi / Bicep / SAM / Serverless
  # Framework / Crossplane, per Refs #96 corpus policy. Each entry pinned to
  # the SHA recorded in its child issue. Multi-flavor monorepos
  # (aws-cdk-examples, pulumi/examples) get one entry per language flavor with
  # a unique name and a flavor-scoped sparse-path so cone-mode sparse-checkout
  # keeps each working tree small.
  "aws-cdk-examples-python|https://github.com/aws-samples/aws-cdk-examples.git|main|python|python"             # AWS CDK Python (#184) SHA f4143ebe9746
  "aws-cdk-examples-java|https://github.com/aws-samples/aws-cdk-examples.git|main|java|java"                   # AWS CDK Java (#187) SHA f4143ebe9746
  "aws-cdk-examples-csharp|https://github.com/aws-samples/aws-cdk-examples.git|main|csharp|csharp"             # AWS CDK .NET (#188) SHA f4143ebe9746
  "aws-cdk-examples-go|https://github.com/aws-samples/aws-cdk-examples.git|main|go|go"                         # AWS CDK Go (#191) SHA f4143ebe9746
  "pulumi-examples-typescript|https://github.com/pulumi/examples.git|master|typescript|aws-ts-webserver"       # Pulumi TypeScript (#194) SHA d9173c2ed496
  "pulumi-examples-python|https://github.com/pulumi/examples.git|master|python|aws-py-webserver"               # Pulumi Python (#196) SHA d9173c2ed496
  "pulumi-examples-csharp|https://github.com/pulumi/examples.git|master|csharp|aws-cs-webserver"               # Pulumi .NET (#202) SHA d9173c2ed496
  # --- DB migration tools (chunk Y, umbrella #317) ---
  # Versioned-SQL migration tooling across major ecosystems, per Refs #87
  # corpus policy. Each entry pinned to the SHA recorded in the umbrella body.
  # Sparse-paths are applied to the two large monorepos (flyway, liquibase);
  # the remaining repos are small enough to clone in full.
  "flyway|https://github.com/flyway/flyway.git|main|java|flyway-core"                                          # Flyway versioned/repeatable SQL migrations (#317) SHA ce65ee118f01
  "liquibase|https://github.com/liquibase/liquibase.git|main|java|liquibase-standard"                          # Liquibase changelog formats + changesets (#317) SHA e9c06031abc8
  "knex|https://github.com/knex/knex.git|master|javascript"                                                    # Knex schema-builder migrations + query builder (#317) SHA af57d1ec662a
  "goose|https://github.com/pressly/goose.git|main|go"                                                         # Goose -- +goose Up/Down SQL annotations + Go migrations (#317) SHA e3235f7041e1
  "migrate-mongo|https://github.com/seppevs/migrate-mongo.git|master|javascript"                               # MongoDB migration scripts up/down handler (#317) SHA 1f5e5f953491
  "sequel|https://github.com/jeremyevans/sequel.git|master|ruby"                                               # Sequel.migration { up/down } blocks (#317) SHA 694ea7798374
  # --- CI/CD pipelines (chunk Q, umbrella #318) ---
  # Canonical fixtures for CI/CD pipeline formats not yet exercised by the
  # corpus, per Refs #87. Each entry pinned to the SHA recorded in umbrella
  # #318. Sparse-paths keep working trees small for the larger monorepos
  # (gitlab-runner, jenkins, tektoncd/pipeline, skaffold, tilt).
  "drone|https://github.com/drone/drone.git|main|yaml"                                                          # Drone pipeline steps + plugins (#318) SHA 9f37938bc913
  "buildkite-agent|https://github.com/buildkite/agent.git|main|yaml|.buildkite"                                 # Buildkite pipeline.yml + plugins (#318) SHA 561444c7fc59
  "skaffold|https://github.com/GoogleContainerTools/skaffold.git|main|yaml|examples"                            # Skaffold build/deploy/test profiles (#318) SHA 95531aa9b308
  # --- Build tools missing (chunk R, umbrella #320) ---
  # Build-tool manifest fixtures not yet exercised: Gradle, Bazel, Make, CMake,
  # Poetry, Composer, Bundler, Mix, sbt, Leiningen, Lerna (npm workspaces),
  # Turborepo. Each entry pinned to the SHA recorded in the umbrella body.
  # Dedup notes: pnpm/pnpm is already present above under chunk H (#172) — the
  # same clone exercises both package.json and pnpm-workspace.yaml extractors,
  # so we do NOT re-list it here. Gradle/spring-petclinic overlap is partial:
  # spring-petclinic is a Gradle-using sample, gradle/gradle is the canonical
  # multi-project Groovy + Kotlin DSL build source — kept as separate fixture.
  "gradle|https://github.com/gradle/gradle.git|master|java|subprojects"                                        # Gradle multi-project Groovy + Kotlin DSL (#320) SHA 62000451ad7b
  "gnu-make|https://git.savannah.gnu.org/git/make.git|master|c"                                                # GNU Make rules, vars, includes (#320) SHA b3802782de3e
  "poetry|https://github.com/python-poetry/poetry.git|main|python"                                             # Poetry pyproject + lock semantics (#320) SHA 811a12dae0fe
  "composer|https://github.com/composer/composer.git|main|php"                                                 # PHP Composer manifest + lock (#320) SHA 37825e985129
  "bundler|https://github.com/rubygems/bundler.git|master|ruby"                                                # Ruby Bundler Gemfile + gemspec (#320) SHA 35be6d9a6030
  "elixir|https://github.com/elixir-lang/elixir.git|main|elixir"                                               # Elixir Mix project + deps (#320) SHA b8723fea1ec5
  "sbt|https://github.com/sbt/sbt.git|develop|scala"                                                           # Scala sbt build definition (#320) SHA 76992ed3f62f
  "leiningen|https://github.com/technomancy/leiningen.git|main|clojure"                                        # Clojure Leiningen project.clj (#320) SHA 40227328d4a9
  "lerna|https://github.com/lerna/lerna.git|main|javascript"                                                   # npm/yarn workspaces canonical (#320) SHA f4387d673bfd
  "turborepo|https://github.com/vercel/turborepo.git|main|typescript"                                          # Turborepo pipeline config (#320) SHA c1f923a4abe5
  # NOTE: pnpm-workspace coverage is satisfied by the existing pnpm entry under
  # chunk H above (same repo @ same branch).
  # --- Reverse-proxy/gateway (chunk V, umbrella #326) ---
  # Canonical fixtures for reverse-proxy and API-gateway configuration formats
  # not yet exercised by the corpus, per Refs #87. Each entry pinned to the SHA
  # recorded in umbrella #326. Sparse-paths keep working trees small for the
  # larger monorepos (nginx, httpd, envoy, kong, traefik).
  # --- API/Spec/IDL alts (chunk U, umbrella #325) ---
  # API spec / IDL alternatives beyond OpenAPI, per Refs #87 corpus policy.
  # Each entry pinned to the SHA verified in the umbrella body via
  # git ls-remote --symref against canonical upstream branches.
  # Sparse-paths are applied to the three large monorepos (smithy, avro,
  # thrift); the remaining four are small enough to clone in full. Where
  # the umbrella listed multiple sparse subtrees (or glob patterns not
  # supported by cone-mode sparse-checkout), one canonical IDL/schema
  # subtree per repo is selected — broad extractor coverage is preserved.
  # --- Auth/security/observability (chunk S, umbrella #322) ---
  # Auth (OAuth2/OIDC/JWT/Keycloak/Auth0), security (Vault), and observability
  # (OpenTelemetry, Prometheus, Sentry, Datadog) client-library consumer apps.
  # Each entry pinned to the SHA recorded in the umbrella body.
  "node-openid-client|https://github.com/panva/node-openid-client.git|main|typescript|lib"                       # OAuth2/OIDC client flows, discovery (#322) SHA d3721c2cab93
  "node-jsonwebtoken|https://github.com/auth0/node-jsonwebtoken.git|master|javascript"                           # JWT sign/verify consumer patterns (#322) SHA 02688b982132
  "keycloak|https://github.com/keycloak/keycloak.git|main|java|core"                                             # Keycloak Java SDK adapters (#322) SHA 8938558fa5c7
  "auth0-spa-js|https://github.com/auth0/auth0-spa-js.git|main|typescript|src"                                   # Auth0 SPA SDK login/logout/token (#322) SHA c66e8b7400f2
  "vault|https://github.com/hashicorp/vault.git|main|go|api"                                                     # HashiCorp Vault Go client (#322) SHA 54a22347c3fa
  "opentelemetry-js|https://github.com/open-telemetry/opentelemetry-js.git|main|typescript|packages"             # OpenTelemetry tracing/metrics SDK (#322) SHA 95e48e7afcc4
  "prometheus-client-golang|https://github.com/prometheus/client_golang.git|main|go|prometheus"                  # Prometheus Go client metrics (#322) SHA 0ac87e14c303
  "sentry-javascript|https://github.com/getsentry/sentry-javascript.git|develop|typescript|packages"             # Sentry SDK init + capture (#322) SHA 298807727c81
  "dd-trace-js|https://github.com/DataDog/dd-trace-js.git|master|javascript|packages"                            # Datadog APM tracer instrumentations (#322) SHA 807fceb14d1a
  # --- Validation + Lint configs (chunk T, umbrella #324) ---
  # Validation libraries (Zod, Joi, Yup, Pydantic, class-validator) and lint
  # configurations (ESLint, Prettier, Ruff, RuboCop, golangci-lint, Clippy)
  # per Refs #87. Each entry pinned to the SHA recorded in the umbrella body.
  # Sparse paths from the umbrella are documented in comments; the harness
  # entry uses a full clone because the parser supports a single sparse path
  # only and these fixtures list multiple paths each.
  "joi|https://github.com/hapijs/joi.git|master|javascript"                                                    # Joi schema + extensions (#324) SHA 048fe05b8235 paths lib/ test/
  "yup|https://github.com/jquense/yup.git|master|typescript"                                                   # Yup object/array validation (#324) SHA ff31eee8a2b1 paths src/ test/
  "class-validator|https://github.com/typestack/class-validator.git|develop|typescript"                        # class-validator decorators (#324) SHA 2e1a5c27dbd6 paths src/ test/
  "eslint|https://github.com/eslint/eslint.git|main|javascript"                                                # ESLint flat + legacy config (#324) SHA a4297918d264 paths lib/ conf/ eslint.config.js
  "prettier|https://github.com/prettier/prettier.git|main|javascript"                                          # Prettier config + plugin discovery (#324) SHA f3db616d8389 paths .prettierrc* src/config/
  "ruff|https://github.com/astral-sh/ruff.git|main|python"                                                     # ruff config + rule selectors (#324) SHA 8091ad11d15f paths ruff.toml pyproject.toml crates/
  "rubocop|https://github.com/rubocop/rubocop.git|master|ruby"                                                 # RuboCop cop config + inheritance (#324) SHA 11262e1cdb45 paths .rubocop.yml config/
  "golangci-lint|https://github.com/golangci/golangci-lint.git|main|go"                                        # golangci-lint linters + presets (#324) SHA ef3710ea5470 paths .golangci.yml pkg/config/
  "rust-clippy|https://github.com/rust-lang/rust-clippy.git|master|rust"                                       # Clippy lint config + categories (#324) SHA f763854b8bd3 paths clippy.toml clippy_lints/
  # --- Serverless (chunk Z, umbrella #319) ---
  # Function-only repos covering all major serverless platforms and Lambda
  # runtimes. Exercises handler detection, function-config parsing
  # (SAM/serverless/wrangler/netlify/vercel), and per-runtime entry points.
  # Each entry pinned to the SHA recorded in umbrella #319.
  "aws-lambda-developer-guide|https://github.com/awsdocs/aws-lambda-developer-guide.git|main|javascript|sample-apps"  # Lambda Node handler shape, SAM template.yaml (#319) SHA 8a681ab924e4
  "aws-lambda-java-libs|https://github.com/aws/aws-lambda-java-libs.git|main|java|aws-lambda-java-core"               # Java Lambda handler interfaces, event POJOs (#319) SHA c4dcbab4ffed
  "aws-lambda-go|https://github.com/aws/aws-lambda-go.git|main|go|lambda"                                              # Go Lambda handler signatures, event types (#319) SHA 815d21f41769
  "aws-lambda-rust-runtime|https://github.com/awslabs/aws-lambda-rust-runtime.git|main|rust|lambda-runtime"            # Rust Lambda runtime crate, handler macros (#319) SHA 01237499db5f
  "vercel-examples|https://github.com/vercel/examples.git|main|typescript|edge-functions"                              # Vercel Edge/Serverless function shape, vercel.json (#319) SHA 72aaac1ba427
  "netlify-functions|https://github.com/netlify/functions.git|main|typescript|src"                                     # Netlify Functions handler types, netlify.toml (#319) SHA c3f47247079e
  "functions-framework-nodejs|https://github.com/GoogleCloudPlatform/functions-framework-nodejs.git|main|typescript|src"  # GCF Functions Framework HTTP/CloudEvent handlers (#319) SHA 243202d5e133
  # --- State management (chunk AA, umbrella #321) ---
  # Frontend state-management libraries exercising store/atom/machine detection
  # across Redux, MobX, Zustand, Pinia, NgRx, Recoil, XState, Effector per
  # Refs #87. Each entry pinned to the SHA recorded in the umbrella body.
  "redux|https://github.com/reduxjs/redux.git|master|typescript|packages/toolkit"                              # Redux reducers/actions, Toolkit slices (#321) SHA 38faff513dc2
  "mobx|https://github.com/mobxjs/mobx.git|main|typescript|packages/mobx"                                     # MobX observables, computed, reactions (#321) SHA 03f420ac4a29
  "zustand|https://github.com/pmndrs/zustand.git|main|typescript|src"                                         # Zustand `create` stores, middleware (#321) SHA 3fca84617984
  "pinia|https://github.com/vuejs/pinia.git|v4|typescript|packages/pinia"                                     # Pinia `defineStore`, Vue 3 stores (#321) SHA e329b3805486 (Vue SFC + TS dual extraction)
  "ngrx-platform|https://github.com/ngrx/platform.git|main|typescript|modules/store"                          # NgRx actions/reducers/effects (#321) SHA a469cbf01562
  "recoil|https://github.com/facebookexperimental/Recoil.git|main|typescript|packages/recoil"                 # Recoil atoms/selectors (#321) SHA c1b97f3a0117
  "effector|https://github.com/effector/effector.git|master|typescript|src"                                   # Effector stores/events/effects (#321) SHA 29553bb13dc3
  # --- Static-site generators (chunk W, umbrella #298) ---
  # Static-site generator fixtures across the major SSG ecosystems
  # (Hugo, Jekyll, Hexo, MkDocs, Sphinx, VitePress, Docusaurus, Nextra,
  # Eleventy), per Refs #87 corpus policy. Each entry pinned to the SHA
  # recorded in umbrella #298. Where the umbrella listed multiple sparse
  # subtrees, one canonical content/template subtree per repo is selected
  # (cone-mode sparse-checkout supports a single path); the harness language
  # tag is the dominant code-extractor for the repo, with Markdown / front-
  # matter / template-engine extractors exercised via the sparse subtree.
  # Dedup notes: Gatsby is intentionally excluded here per the umbrella body
  # (already covered by chunk K, per the master plan).
  "jekyll|https://github.com/jekyll/jekyll.git|master|ruby|lib"                                               # Liquid templating, Jekyll plugin model (#298) SHA 202df571314b
  "hexo-site|https://github.com/hexojs/site.git|master|javascript|source"                                     # Hexo theming + Markdown content tree (#298) SHA fcf644467039
  "mkdocs|https://github.com/mkdocs/mkdocs.git|master|python|mkdocs"                                          # MkDocs plugin/theme system, mkdocs.yml config (#298) SHA 2862536793b3
  "vitepress|https://github.com/vuejs/vitepress.git|main|typescript|src"                                      # VitePress theme, Vue SFC + Markdown blend (#298) SHA 6ee01bf30534
  "docusaurus|https://github.com/facebook/docusaurus.git|main|typescript|packages/docusaurus"                 # MDX extractor, Docusaurus plugin packages (#298) SHA 5f60ae9c872d
  "nextra|https://github.com/shuding/nextra.git|main|typescript|packages/nextra"                              # Nextra theme + MDX content trees (#298) SHA e604c7fe937e
  "eleventy|https://github.com/11ty/eleventy.git|main|javascript|src"                                         # Eleventy template engines, .eleventy.js config (#298) SHA 4a6b85f64eef
  # --- Testing frameworks (chunk X, umbrella #312) ---
  # Testing-framework-dominated repos so the harness exercises test-file
  # detection, fixture parsing, and framework-specific patterns (Jest, Mocha,
  # Cypress, Playwright, Selenium, JUnit 5, RSpec, pytest, Karate, Cucumber).
  # Each entry pinned to the SHA recorded in the umbrella body and verified
  # via git ls-remote --symref. Sparse paths from the umbrella are documented
  # in trailing comments; entries use a full clone where the umbrella lists
  # multiple sparse paths because the parser supports a single sparse path
  # only. seleniumhq.github.io uses its single /examples sparse-path.
  "jest|https://github.com/jestjs/jest.git|main|typescript"                                                   # Jest config, snapshot tests, mock factories (#312) SHA 746b14333fbd paths packages/ e2e/
  "mocha|https://github.com/mochajs/mocha.git|main|javascript"                                                # Mocha BDD/TDD interfaces, hooks, reporters (#312) SHA 441c32aa076f paths lib/ test/
  "cypress-realworld-app|https://github.com/cypress-io/cypress-realworld-app.git|develop|typescript"          # Cypress E2E specs, custom commands, fixtures (#312) SHA 84008795de05 paths cypress/ src/
  "playwright|https://github.com/microsoft/playwright.git|main|typescript"                                    # Playwright test runner, fixtures, projects (#312) SHA c17e0b45c12f paths packages/playwright/ tests/
  "junit5-samples|https://github.com/junit-team/junit5-samples.git|main|java"                                 # JUnit 5 Jupiter API, parameterized tests (#312) SHA c8828652e601 paths junit5-jupiter-starter-gradle/ junit5-jupiter-starter-maven/
  "rspec-rails|https://github.com/rspec/rspec-rails.git|main|ruby"                                            # RSpec describe/it/expect, Rails matchers (#312) SHA 810f2a48950c paths lib/ spec/
  "karate|https://github.com/karatelabs/karate.git|main|java"                                                 # Karate .feature files, API/UI test DSL (#312) SHA 38db45133982 paths karate-core/ examples/
  "cucumber-js|https://github.com/cucumber/cucumber-js.git|main|typescript"                                   # Cucumber.js step defs, Gherkin .feature parsing (#312) SHA 1c9e7022eae1 paths src/ features/
  # --- Realtime/WebSocket/messaging (chunk BB, umbrella #323) ---
  # Consumer apps and libraries showcasing realtime/messaging protocols so the
  # harness exercises socket-handler detection and event-driven patterns. Each
  # entry pinned to the SHA recorded in the umbrella body and verified via
  # git ls-remote --symref. Sparse paths from the umbrella are documented in
  # trailing comments; entries use a full clone or a single canonical sparse
  # subtree because cone-mode sparse-checkout supports a single path. The
  # mdn/dom-examples substitution (no canonical SSE-only repo exists) is
  # scoped to its WebSocket subtree as the dominant realtime surface; the
  # /server-sent-events sibling is exercised via separate JS/HTML extractors
  # when the WS/SSE patterns coincide in scope.
  "pusher-js|https://github.com/pusher/pusher-js.git|master|typescript|src"                                    # Pusher channel subscribe/bind, presence channels (#323) SHA e58a5d22e063 paths src/ spec/
  "ably-js|https://github.com/ably/ably-js.git|main|typescript|src"                                            # Ably Realtime channels, message envelopes (#323) SHA 498d26dfb16b paths src/ test/
  "MQTT.js|https://github.com/mqttjs/MQTT.js.git|main|typescript|src"                                          # MQTT publish/subscribe, topic filters, QoS (#323) SHA a4e9a92c8710 paths src/ test/
  "pion-webrtc|https://github.com/pion/webrtc.git|main|go"                                                     # WebRTC peer connection, data channels in Go (#323) SHA 9654161f9b69 paths /. examples/
  "mdn-dom-examples|https://github.com/mdn/dom-examples.git|main|javascript|web-sockets"                       # Native WebSocket usage examples (#323) SHA f8aa45c93985 paths server-sent-events/ web-sockets/
  # --- Game/embedded (chunk CC, umbrella #291) ---
  # Game-engine and embedded-systems sample fixtures, per Refs #87 corpus
  # policy. Each entry pinned to the SHA recorded in umbrella #291 and
  # verified via git ls-remote --symref against canonical upstream branches.
  # Sparse-paths keep working trees small for the larger monorepos
  # (EntityComponentSystemSamples, esp-idf); the remaining repos are small
  # enough to clone in full. EpicGames/UnrealEngine is private; substituted
  # 20tab/UnrealEnginePython (public C++ Unreal bindings) for `.h`/`.cpp`
  # coverage per the umbrella body.
  "UnrealEnginePython|https://github.com/20tab/UnrealEnginePython.git|master|cpp|Source/UnrealEnginePython/Public"                                            # C++17 Unreal-style headers — UCLASS/UPROPERTY macros, .h/.cpp split, namespaced bindings (#291) SHA 4b5da5bf4ca5
  "bevy|https://github.com/bevyengine/bevy.git|main|rust|crates/bevy_ecs/src"                                                                                  # Rust ECS with heavy trait-bound generics, derive macros, workspace crates (#291) SHA 7dda2bc7fcd2
  "arduino-examples|https://github.com/arduino/arduino-examples.git|main|cpp|examples"                                                                         # Arduino sketches — .ino (C++ flavour) global setup()/loop(), embedded idioms (#291) SHA 4c5fa7a66b42
  "micropython|https://github.com/micropython/micropython.git|master|python|examples"                                                                          # MicroPython firmware-side scripts — top-level imports, no-stdlib subset (#291) SHA a595bbba6727
  "cortex-m-quickstart|https://github.com/rust-embedded/cortex-m-quickstart.git|master|rust|src"                                                                # Embedded no_std Rust — #![no_main], #[entry] attributes, panic handlers (#291) SHA 7a6a7c2c8b94

  # --- Workflow/orchestration (chunk EE, umbrella #293) ---
  # Workflow- and orchestration-engine repos so the harness exercises
  # workflow/activity definitions, DAG schedulers, decorators, and BPMN
  # process XML alongside Java service-task implementations. Each entry is
  # pinned to the SHA recorded in the umbrella body and verified via
  # git ls-remote --symref. Sparse paths from the umbrella are documented
  # in trailing comments; entries use a full clone where the umbrella lists
  # multiple sparse paths because the parser supports a single sparse path
  # only.
  "temporalio-samples-go|https://github.com/temporalio/samples-go.git|main|go"                                 # Temporal Go workflow.ExecuteActivity, signal/query handlers (#293) SHA c5c710404fbf paths helloworld/ child_workflow/ mutex/
  "cadence-client|https://github.com/uber-go/cadence-client.git|master|go|internal"                            # Cadence Go client workflow registration, activity options (#293) SHA df799e0e8164
  "prefect|https://github.com/PrefectHQ/prefect.git|main|python"                                               # Prefect @flow/@task decorators, deployment specs (#293) SHA fe44ad330730 paths src/prefect/flows.py examples/
  "dagster|https://github.com/dagster-io/dagster.git|master|python|examples/quickstart_etl"                    # Dagster @asset/@op/@job decorators, IO managers (#293) SHA 50915f9cab16

  # --- Data/ML/notebook (chunk DD, umbrella #292) ---
  # Data, ML, and notebook ecosystems exercising scikit-learn / PyTorch / Keras
  # / Transformers consumer idioms, the .ipynb extractor, Polars/Spark big-data
  # APIs, and dbt SQL+Jinja models. Each entry pinned to the SHA recorded in
  # umbrella #292 and verified via git ls-remote --symref on 2026-05-09.
  "scikit-learn|https://github.com/scikit-learn/scikit-learn.git|main|python|examples"                        # scikit-learn Pipeline/fit/transform consumer idioms (#292) SHA a421648c9d5b
  "pytorch-examples|https://github.com/pytorch/examples.git|main|python|mnist"                                # PyTorch nn.Module/forward/DataLoader patterns (#292) SHA acc295dc7b90
  "keras-io|https://github.com/keras-team/keras-io.git|master|python|examples"                                # TF/Keras functional API, Model.fit, custom layers (#292) SHA 524c0d7ae593
  "transformers|https://github.com/huggingface/transformers.git|main|python|examples/pytorch"                 # HF Transformers AutoModel/tokenizer/Trainer (#292) SHA c9de1097eed9
  "polars|https://github.com/pola-rs/polars.git|main|python|py-polars/tests/unit"                             # Polars lazy frames, pl.col expressions, chained transforms (#292) SHA a84168f0a4f1
  # --- Frontend SPA frameworks (chunk K, umbrella #303) ---
  # Sample applications across the major frontend SPA / meta-framework stacks,
  # per Refs #96 corpus policy. Each entry pinned to the SHA recorded in its
  # child issue and verified via git ls-remote --symref on 2026-05-10. Gatsby
  # is intentionally NOT in chunk W (#298) — it is owned by chunk K via #279.
  "react-redux-realworld|https://github.com/gothinkster/react-redux-realworld-example-app.git|master|javascript"   # React (CRA) sample app (#227) SHA ee72eba40563
  "vite|https://github.com/vitejs/vite.git|main|typescript"                                                        # Vite (React+TS bundler) (#229) SHA cf0ff4154b26
  "vue-realworld|https://github.com/gothinkster/vue-realworld-example-app.git|master|javascript"                   # Vue sample app (#231) SHA 3df3773b6be7
  "svelte-realworld|https://github.com/sveltejs/realworld.git|master|javascript"                                   # Svelte sample app (#233) SHA e80873cac4e6
  "solid-templates|https://github.com/solidjs/templates.git|main|typescript"                                       # SolidJS templates (#238) SHA a48da0c79ecf
  "preact-cli|https://github.com/developit/preact-cli.git|master|javascript"                                       # Preact CLI scaffold (#239) SHA e826f7caab0d
  "ember-super-rentals|https://github.com/ember-learn/super-rentals.git|main|javascript"                           # Ember sample app (#243) SHA 5d11a767bdc1
  "astro|https://github.com/withastro/astro.git|main|typescript"                                                   # Astro framework (#247) SHA d365c975ba2d
  "remix-indie-stack|https://github.com/remix-run/indie-stack.git|main|typescript"                                 # Remix indie-stack sample (#252) SHA 56abb93bf81f
  "nuxt-starter|https://github.com/nuxt/starter.git|templates|typescript"                                          # Nuxt starter templates (#256) SHA cc96964ee7a1
  "lit-element-starter|https://github.com/lit/lit-element-starter-ts.git|main|typescript"                          # Lit / Web Components starter (#261) SHA 6bf882733abd
  "htmx|https://github.com/bigskysoftware/htmx.git|master|javascript"                                              # HTMX library (#266) SHA dbf77dd5207d
  "alpine|https://github.com/alpinejs/alpine.git|main|javascript"                                                  # Alpine.js library (#271) SHA 3b125f96058a
  "qwik|https://github.com/QwikDev/qwik.git|main|typescript"                                                       # Qwik framework (#276) SHA 38620076e10e
  "gatsby-starter-blog|https://github.com/gatsbyjs/gatsby-starter-blog.git|master|javascript"                      # Gatsby starter blog (#279) SHA 04ec3e642112

  # --- Web framework alts (chunk J, umbrella #311) ---
  # Alternate web framework sample apps spanning Go (Echo/Fiber/Beego),
  # Node (Fastify/Koa/Hapi/Sails/AdonisJS), Python (Tornado/Starlette/
  # Pyramid/Bottle), PHP (Slim/CodeIgniter/Yii), Ruby (Sinatra/Hanami/
  # Grape), Rust (Axum/Rocket/Warp/Tide), Scala (Akka HTTP/http4s),
  # Clojure (Compojure/Pedestal), Elixir (Plug/Phoenix LiveView), and
  # Kotlin (Javalin/http4k). Each entry pinned to the SHA recorded in
  # the umbrella body and verified via git ls-remote --symref on
  # 2026-05-10. Sparse paths are not used for these entries — the
  # repos are sample apps or single-framework sources at HEAD size.
  "echox|https://github.com/labstack/echox.git|master|go"                                                     # Echo v4 sample app: e.Group, middleware, route handlers (#240) SHA f938a8c5e04e
  "gofiber-recipes|https://github.com/gofiber/recipes.git|master|go"                                          # Fiber app.Get/Post, middleware chains, sub-routers (#241) SHA 041c1976144d
  "beego-example|https://github.com/beego/beego-example.git|master|go"                                        # Beego controllers, web.Router, ORM models (#242) SHA 7b8dfaf49040
  "fastify-demo|https://github.com/fastify/demo.git|main|javascript"                                          # Fastify fastify.register, schema-validated routes (#244) SHA 5fa922df34d0
  "koa-examples|https://github.com/koajs/examples.git|master|javascript"                                      # Koa app.use middleware, ctx.body, koa-router (#245) SHA 40c77dbecec6
  "hapi|https://github.com/hapijs/hapi.git|master|javascript"                                                 # Hapi server.route, Joi validation, plugins (#246) SHA d4f93d80e6ac
  "sails-examples|https://github.com/balderdashy/sails-examples.git|master|javascript"                        # Sails MVC controllers, blueprints, Waterline models (#248) SHA c8e7c8c41640
  "adonis-blog-demo|https://github.com/adonisjs-community/adonis-blog-demo.git|master|javascript"             # AdonisJS Route.get, Controllers, Lucid ORM (#249) SHA 2262706c39a6
  "tornado|https://github.com/tornadoweb/tornado.git|master|python"                                           # Tornado RequestHandler subclasses, async get/post (#250) SHA 87508512caab
  "starlette|https://github.com/encode/starlette.git|main|python"                                             # Starlette Route()/Mount(), ASGI middleware (#253) SHA 7793b925a88e
  "pyramid-shootout|https://github.com/Pylons/shootout.git|master|python"                                     # Pyramid config.add_route, view_config decorators (#254) SHA e5691d6f5ac0
  "bottle|https://github.com/bottlepy/bottle.git|master|python"                                               # Bottle @route/@get/@post decorators, Bottle() apps (#255) SHA 2a743a302a71
  "slim-skeleton|https://github.com/slimphp/Slim-Skeleton.git|main|php"                                       # Slim 4 $app->get/post, PSR-15 middleware, DI container (#258) SHA 0ef01549870b
  "codeigniter-shield|https://github.com/codeigniter4/shield.git|develop|php"                                 # CodeIgniter 4 Controllers, $routes->group, filters (#260) SHA 34be62ba858d
  "yii2-app-basic|https://github.com/yiisoft/yii2-app-basic.git|master|php"                                   # Yii2 Controllers, ActiveRecord models, URL rules (#262) SHA 660f9bd23cba
  "sinatra-recipes|https://github.com/sinatra/sinatra-recipes.git|main|ruby"                                  # Sinatra get/post DSL, modular Sinatra::Base apps (#263) SHA 5430c1a5b613
  "hanami|https://github.com/hanami/hanami.git|main|ruby"                                                     # Hanami actions, Hanami::Router, slice architecture (#265) SHA 116847bfdf9f
  "grape-on-rack|https://github.com/ruby-grape/grape-on-rack.git|master|ruby"                                 # Grape API DSL: resource/get/post, params validation (#267) SHA 27b711871229
  "rocket|https://github.com/rwf2/Rocket.git|master|rust"                                                     # Rocket #[get]/#[post] attribute macros, Form/Json (#270) SHA 3a54d079aef0
  "warp|https://github.com/seanmonstar/warp.git|master|rust"                                                  # Warp Filter combinators: warp::path, .and(), .map() (#272) SHA 707866ca367d
  "tide|https://github.com/http-rs/tide.git|main|rust"                                                        # Tide app.at()/get()/post(), async middleware (#273) SHA b32f680d5bd1
  "akka-http|https://github.com/akka/akka-http.git|main|scala"                                                # Akka HTTP Route DSL: path/get/post directives (#275) SHA d3ef24b4d0a3
  "http4s|https://github.com/http4s/http4s.git|series/0.23|scala"                                             # http4s HttpRoutes.of partial functions, cats-effect (#277) SHA 134ca10c55c3
  "compojure|https://github.com/weavejester/compojure.git|master|clojure"                                     # Compojure defroutes, GET/POST macros, ring middleware (#278) SHA 8a4758d28e8f
  "pedestal|https://github.com/pedestal/pedestal.git|master|clojure"                                          # Pedestal interceptor chains, table-routes, service map (#281) SHA 4bda462b501c
  "elixir-plug|https://github.com/elixir-plug/plug.git|main|elixir"                                           # Plug.Router, plug pipelines, Plug.Conn transformations (#282) SHA a447199b337d
  "javalin-samples|https://github.com/javalin/javalin-samples.git|main|kotlin"                                # Javalin app.get/post lambdas, route handlers, Jackson DTOs (#285) SHA 3064bf479c58
)

# Locate or build the archigraph binary. We build into the corpora dir
# (outside the repo) so this script is safe to run from any worktree.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

if [[ -n "${ARCHIGRAPH_BIN:-}" ]]; then
  BIN="$ARCHIGRAPH_BIN"
else
  BIN="$CORPORA_DIR/_bin/archigraph"
  mkdir -p "$(dirname "$BIN")"
  echo "==> building archigraph -> $BIN" >&2
  ( cd "$REPO_ROOT" && go build -o "$BIN" ./cmd/archigraph )
fi

if [[ ! -x "$BIN" ]]; then
  echo "archigraph binary not executable: $BIN" >&2
  exit 1
fi

TIMESTAMP="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
REPORT="$REPORTS_DIR/$TIMESTAMP.md"
# Per-run scratch dir lives under _reports/ (NOT mktemp) so partial state
# survives abort/SIGKILL/timeout — earlier behavior wiped per-repo JSON on
# EXIT, making partial runs unsalvageable. The dir is only deleted at the
# end of a successful run, gated by a `.complete` sentinel written by the
# final aggregation step. Anything without a sentinel is preserved for
# post-mortem; operators can `rm -rf` stale `*-partial/` dirs by hand.
TMPDIR_AGG="$REPORTS_DIR/${TIMESTAMP}-partial"
mkdir -p "$TMPDIR_AGG"
trap '[[ -f "$TMPDIR_AGG/.complete" ]] && rm -rf "$TMPDIR_AGG"' EXIT

# Optional per-repo wall-clock cap (seconds). Set ARCHIGRAPH_VERIFY2_TIMEOUT=0
# to disable. Uses gtimeout (coreutils) if available, then timeout, then
# silently skips capping on systems with neither.
PER_REPO_TIMEOUT="${ARCHIGRAPH_VERIFY2_TIMEOUT:-600}"
TIMEOUT_BIN=""
if command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_BIN="gtimeout"
elif command -v timeout >/dev/null 2>&1; then
  TIMEOUT_BIN="timeout"
fi

# Write the language manifest used by the per-language aggregation step.
LANG_MANIFEST="$TMPDIR_AGG/_languages.tsv"
: >"$LANG_MANIFEST"
for entry in "${REPOS[@]}"; do
  IFS='|' read -r name url ref lang sparse <<<"$entry"
  printf '%s\t%s\n' "$name" "$lang" >>"$LANG_MANIFEST"
done

# Markdown report header.
{
  echo "# VERIFY-2 bug-rate report"
  echo
  echo "- generated_at: \`$TIMESTAMP\`"
  echo "- corpora_dir: \`$CORPORA_DIR\`"
  echo "- archigraph_bin: \`$BIN\`"
  echo
  echo "## Per-repo results"
  echo
  echo "| repo | files | entities | relationships | bug_rate | resolution_rate |"
  echo "| --- | ---: | ---: | ---: | ---: | ---: |"
} >"$REPORT"

clone_or_update() {
  local name="$1" url="$2" ref="$3" sparse="${4:-}"
  local dest="$CORPORA_DIR/$name"
  if [[ -d "$dest/.git" ]]; then
    echo "==> updating $name" >&2
    ( cd "$dest" && git fetch --depth 1 origin "$ref" >/dev/null 2>&1 && git checkout -q FETCH_HEAD ) || true
    return 0
  fi
  if [[ -n "$sparse" ]]; then
    echo "==> sparse-cloning $name @ $ref (subset: $sparse)" >&2
    # Blob-less partial clone + cone-mode sparse checkout. We deliberately
    # do not pass --depth here because partial clones with --depth+--branch
    # are flaky on older git versions; the blob filter alone keeps the
    # working set small.
    if ! git clone --filter=blob:none --no-checkout --branch "$ref" "$url" "$dest" >/dev/null 2>&1; then
      git clone --filter=blob:none --no-checkout "$url" "$dest" >/dev/null 2>&1
    fi
    ( cd "$dest" \
      && git sparse-checkout init --cone >/dev/null 2>&1 \
      && git sparse-checkout set "$sparse" >/dev/null 2>&1 \
      && git checkout -q "$ref" 2>/dev/null \
        || git checkout -q FETCH_HEAD 2>/dev/null \
        || git checkout -q ) || true
  else
    echo "==> cloning $name @ $ref" >&2
    git clone --depth 1 --branch "$ref" "$url" "$dest" >/dev/null 2>&1 || \
      git clone --depth 1 "$url" "$dest" >/dev/null 2>&1
  fi
}

run_one() {
  local name="$1"
  local dest="$CORPORA_DIR/$name"
  local out="$TMPDIR_AGG/$name.json"
  local stderr_log="$TMPDIR_AGG/$name.stderr"
  echo "==> indexing $name" >&2
  local rc=0
  if [[ -n "$TIMEOUT_BIN" && "$PER_REPO_TIMEOUT" != "0" ]]; then
    "$TIMEOUT_BIN" --foreground "${PER_REPO_TIMEOUT}s" \
      "$BIN" index --json-stats "$dest" >"$out" 2>"$stderr_log" || rc=$?
  else
    "$BIN" index --json-stats "$dest" >"$out" 2>"$stderr_log" || rc=$?
  fi
  if [[ $rc -ne 0 ]]; then
    if [[ -n "$TIMEOUT_BIN" && $rc -eq 124 ]]; then
      echo "  ! indexer timed out after ${PER_REPO_TIMEOUT}s for $name" >&2
    else
      echo "  ! indexer failed (rc=$rc); see $stderr_log" >&2
    fi
    return 1
  fi
  # Extract numbers via a small inline python (jq not assumed present).
  python3 - "$out" "$REPORT" "$name" <<'PY'
import json, sys
path, report, name = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path) as fh:
    d = json.load(fh)
row = "| {name} | {files} | {ent} | {rel} | {br:.2%} | {rr:.2%} |\n".format(
    name=name,
    files=d.get("files", 0),
    ent=d.get("entities", 0),
    rel=d.get("relationships", 0),
    br=d.get("bug_rate", 0.0),
    rr=d.get("resolution_rate", 0.0),
)
with open(report, "a") as fh:
    fh.write(row)
PY
}

for entry in "${REPOS[@]}"; do
  IFS='|' read -r name url ref lang sparse <<<"$entry"
  clone_or_update "$name" "$url" "$ref" "${sparse:-}"
  if ! run_one "$name"; then
    echo "| $name | ERROR | - | - | - | - |" >>"$REPORT"
    continue
  fi
done

# Fail-fast: if no per-repo JSON files were produced, or every produced
# JSON had files=0, exit 1 instead of writing an empty report. This guards
# against silent corpus drift (e.g., clone failures, every clone empty).
python3 - "$TMPDIR_AGG" <<'PY' || { echo "VERIFY-2: empty corpus — no repos indexed or all repos had files=0" >&2; exit 1; }
import json, os, sys, glob
tmp = sys.argv[1]
paths = sorted(glob.glob(os.path.join(tmp, "*.json")))
if not paths:
    sys.exit(1)
total_files = 0
for p in paths:
    with open(p) as fh:
        d = json.load(fh)
    total_files += d.get("files", 0)
if total_files == 0:
    sys.exit(1)
sys.exit(0)
PY

# Aggregate dispositions across every per-repo JSON file. Adds:
#   - aggregate row inside the per-repo table
#   - per-repo disposition table for each repo
#   - corpus-wide aggregate metric table
#   - corpus-wide disposition breakdown
#   - per-language aggregate (using the LANG_MANIFEST written above)
#   - ship-gate check
python3 - "$TMPDIR_AGG" "$REPORT" "$LANG_MANIFEST" <<'PY'
import json, os, sys, glob
tmp, report, manifest = sys.argv[1], sys.argv[2], sys.argv[3]

DISPOSITIONS = [
    "resolved",
    "external-known",
    "external-unknown",
    "dynamic",
    "bug-extractor",
    "bug-resolver",
    "unclassified",
]

# Load language manifest: name -> language.
lang_of = {}
with open(manifest) as fh:
    for line in fh:
        line = line.rstrip("\n")
        if not line:
            continue
        parts = line.split("\t")
        if len(parts) != 2:
            continue
        lang_of[parts[0]] = parts[1]

per_repo = {}
for p in sorted(glob.glob(os.path.join(tmp, "*.json"))):
    with open(p) as fh:
        d = json.load(fh)
    name = os.path.splitext(os.path.basename(p))[0]
    per_repo[name] = d

# Aggregate row in the per-repo table (still inside ## Per-repo results).
totals = {"files": 0, "entities": 0, "relationships": 0}
endpoints_total = 0
endpoints_resolved = 0
endpoints_bug = 0
agg_dispo = {k: 0 for k in DISPOSITIONS}
for name, d in per_repo.items():
    totals["files"] += d.get("files", 0)
    totals["entities"] += d.get("entities", 0)
    totals["relationships"] += d.get("relationships", 0)
    for k, v in d.get("disposition_counts", {}).items():
        agg_dispo[k] = agg_dispo.get(k, 0) + v
        endpoints_total += v
        if k == "resolved":
            endpoints_resolved += v
        if k in ("bug-extractor", "bug-resolver"):
            endpoints_bug += v
agg_br = (endpoints_bug / endpoints_total) if endpoints_total else 0.0
agg_rr = (endpoints_resolved / endpoints_total) if endpoints_total else 0.0

with open(report, "a") as fh:
    # Aggregate row at the bottom of the per-repo table.
    fh.write("| **AGGREGATE** | **{f}** | **{e}** | **{r}** | **{br:.2%}** | **{rr:.2%}** |\n".format(
        f=totals["files"], e=totals["entities"], r=totals["relationships"],
        br=agg_br, rr=agg_rr))

    # Per-repo disposition tables.
    fh.write("\n## Per-repo disposition breakdown\n")
    for name in sorted(per_repo):
        d = per_repo[name]
        counts = d.get("disposition_counts", {})
        repo_total = sum(counts.get(k, 0) for k in DISPOSITIONS)
        fh.write(f"\n### {name}\n\n")
        fh.write("| disposition | count | pct |\n| --- | ---: | ---: |\n")
        for k in DISPOSITIONS:
            v = counts.get(k, 0)
            pct = (v / repo_total) if repo_total else 0.0
            fh.write(f"| {k} | {v} | {pct:.2%} |\n")
        fh.write(f"| **total** | **{repo_total}** | **100.00%** |\n")

    # Corpus-wide aggregate metric table.
    fh.write("\n## Aggregate\n\n")
    fh.write("| metric | value |\n| --- | ---: |\n")
    fh.write(f"| total_files | {totals['files']} |\n")
    fh.write(f"| total_entities | {totals['entities']} |\n")
    fh.write(f"| total_relationships | {totals['relationships']} |\n")
    fh.write(f"| endpoints_classified | {endpoints_total} |\n")
    fh.write(f"| bug_rate | {agg_br:.4%} |\n")
    fh.write(f"| resolution_rate | {agg_rr:.4%} |\n")

    # Corpus-wide disposition breakdown.
    fh.write("\n## Aggregate disposition breakdown\n\n")
    fh.write("| disposition | count | pct |\n| --- | ---: | ---: |\n")
    for k in DISPOSITIONS:
        v = agg_dispo.get(k, 0)
        pct = (v / endpoints_total) if endpoints_total else 0.0
        fh.write(f"| {k} | {v} | {pct:.2%} |\n")
    fh.write(f"| **total** | **{endpoints_total}** | **100.00%** |\n")

    # Per-language aggregate.
    fh.write("\n## Per-language aggregate\n\n")
    fh.write("| language | repos | files | entities | relationships | endpoints | bug_rate | resolution_rate |\n")
    fh.write("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
    by_lang = {}
    for name, d in per_repo.items():
        lang = lang_of.get(name, "unknown")
        bucket = by_lang.setdefault(lang, {
            "repos": 0, "files": 0, "entities": 0, "relationships": 0,
            "endpoints": 0, "resolved": 0, "bug": 0,
        })
        bucket["repos"] += 1
        bucket["files"] += d.get("files", 0)
        bucket["entities"] += d.get("entities", 0)
        bucket["relationships"] += d.get("relationships", 0)
        for k, v in d.get("disposition_counts", {}).items():
            bucket["endpoints"] += v
            if k == "resolved":
                bucket["resolved"] += v
            if k in ("bug-extractor", "bug-resolver"):
                bucket["bug"] += v
    for lang in sorted(by_lang):
        b = by_lang[lang]
        br = (b["bug"] / b["endpoints"]) if b["endpoints"] else 0.0
        rr = (b["resolved"] / b["endpoints"]) if b["endpoints"] else 0.0
        fh.write(f"| {lang} | {b['repos']} | {b['files']} | {b['entities']} | "
                 f"{b['relationships']} | {b['endpoints']} | {br:.4%} | {rr:.4%} |\n")

    # Ship-gate check.
    fh.write("\n## Ship-gate check (target bug_rate <= 1%)\n\n")
    status = "PASS" if agg_br <= 0.01 else "FAIL"
    fh.write(f"- status: **{status}** (bug_rate={agg_br:.4%})\n")
PY

# Mark this run complete — the EXIT trap will now garbage-collect
# $TMPDIR_AGG. Without this sentinel the per-repo JSON survives so a
# partial/aborted run can be inspected or resumed by hand.
: >"$TMPDIR_AGG/.complete"

echo "==> wrote report: $REPORT"
echo "$REPORT"
