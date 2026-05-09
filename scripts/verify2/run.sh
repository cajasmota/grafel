#!/usr/bin/env bash
# scripts/verify2/run.sh
#
# VERIFY-2 (Refs #58, #88) — bug-rate / resolution-rate measurement harness.
#
# Clones a small set of public OSS repositories into
# $ARCHIGRAPH_CORPORA_DIR (default: $HOME/Documents/Projects/archigraph-corpora)
# and runs `archigraph index --json-stats` over each. Aggregates the
# per-disposition counts and writes a Markdown report into
# $ARCHIGRAPH_CORPORA_DIR/_reports/<ISO-timestamp>.md.
#
# This script never writes inside the archigraph repo. The corpora and
# reports live entirely outside it so we don't blow up the worktree with
# vendored third-party source.
#
# Usage:
#   scripts/verify2/run.sh
#
# Env vars:
#   ARCHIGRAPH_CORPORA_DIR   target dir for clones + reports
#                            (default: $HOME/Documents/Projects/archigraph-corpora)
#   ARCHIGRAPH_BIN           path to archigraph binary (default: ./archigraph
#                            built ad-hoc into the corpora dir)
#   ARCHIGRAPH_VERBOSE       set to 1 to forward verbose stderr from indexer
set -euo pipefail

CORPORA_DIR="${ARCHIGRAPH_CORPORA_DIR:-$HOME/Documents/Projects/archigraph-corpora}"
REPORTS_DIR="$CORPORA_DIR/_reports"
mkdir -p "$CORPORA_DIR" "$REPORTS_DIR"

# Repo list. Keep entries public and well-known. Each entry is:
#   <name>|<git-url>|<ref>|<primary-language>[|<sparse-path>]
#
# The optional 5th field selects a single sub-tree via partial clone +
# git sparse-checkout (cone mode). This is REQUIRED for monorepos where
# a full clone exceeds ~200 MB at HEAD on the chosen ref — the comment
# next to each entry records the rough estimate at the time of authoring.
#
# Coverage targets the full 32-language extractor matrix + frameworks +
# ORMs + manifests + tools. Stack-characteristic diversity (Refs #87, #96):
#   - ORM-heavy             rails-realworld, django-realworld
#   - HTTP routing          gin, chi, express-realworld, actix-examples, vapor
#   - microservice / RPC    etcd, kafka-streams-examples
#   - CLI tool              click
#   - config-heavy          pandas (mixed), spring-petclinic, nestjs-starter
REPOS=(
  # Corpus policy (Refs #96): prefer SAMPLE APPLICATIONS that USE a framework
  # over the framework's own source tree. We measure how the indexer handles
  # framework-using user code, not how it handles framework internals.
  # --- Python ---
  "requests|https://github.com/psf/requests.git|main|python"                                       # library; small enough to keep
  "flask-realworld|https://github.com/gothinkster/flask-realworld-example-app.git|master|python"   # Flask sample app
  "click|https://github.com/pallets/click.git|main|python"                                         # CLI tool source; small
  "django-realworld|https://github.com/gothinkster/django-realworld-example-app.git|master|python" # Django sample app
  "pandas|https://github.com/pandas-dev/pandas.git|main|python|pandas/core"                        # full ~400 MB; sparse subset
  # --- Go ---
  "gin|https://github.com/gin-gonic/gin.git|master|go"                                             # framework src; small
  "chi|https://github.com/go-chi/chi.git|master|go"                                                # framework src; small
  "etcd|https://github.com/etcd-io/etcd.git|main|go|server/etcdserver"                             # full ~250 MB; sparse subset
  # --- JavaScript / TypeScript ---
  "express-realworld|https://github.com/gothinkster/node-express-realworld-example-app.git|master|javascript" # Express sample app
  "nestjs-starter|https://github.com/nestjs/typescript-starter.git|master|typescript"              # NestJS sample app
  "nextjs-commerce|https://github.com/vercel/commerce.git|main|typescript"                         # Next.js sample app
  # --- Java ---
  "spring-petclinic|https://github.com/spring-projects/spring-petclinic.git|main|java"             # Spring Boot sample app
  "kafka-streams-examples|https://github.com/confluentinc/kafka-streams-examples.git|master|java"  # Kafka sample app
  # --- Kotlin ---
  "exposed|https://github.com/JetBrains/Exposed.git|main|kotlin"                                   # ORM source; small
  "ktor-samples|https://github.com/ktorio/ktor-samples.git|main|kotlin"                            # Ktor sample apps
  # --- Scala ---
  "play-scala-starter|https://github.com/playframework/play-scala-starter-example.git|2.7.x|scala" # Play Framework sample app
  # --- Groovy ---
  "ratpack-example-books|https://github.com/ratpack/example-books.git|master|groovy"               # Ratpack sample app
  # --- Clojure ---
  "usermanager-example|https://github.com/seancorfield/usermanager-example.git|develop|clojure"    # Ring/Compojure sample app
  # --- Ruby ---
  "rails-realworld|https://github.com/gothinkster/rails-realworld-example-app.git|master|ruby"     # Rails sample app
  "sidekiq|https://github.com/sidekiq/sidekiq.git|main|ruby"                                       # library; small
  # --- PHP ---
  "laravel-quickstart|https://github.com/laravel/quickstart-basic.git|master|php"                  # Laravel sample app
  "symfony-demo|https://github.com/symfony/demo.git|main|php"                                      # Symfony sample app
  # --- Rust ---
  "mini-redis|https://github.com/tokio-rs/mini-redis.git|master|rust"                              # Tokio sample app
  "actix-examples|https://github.com/actix/examples.git|main|rust"                                 # Actix sample apps
  # --- Swift ---
  "vapor-api-template|https://github.com/vapor/api-template.git|master|swift"                      # Vapor sample app (Controllers/Routes/Migrations)
  # --- C# ---
  "aspnetcore-realworld|https://github.com/gothinkster/aspnetcore-realworld-example-app.git|master|csharp" # ASP.NET Core MVC sample app
  # --- C++ ---
  "spdlog|https://github.com/gabime/spdlog.git|v1.x|cpp"                                          # header-only logging library; small
  # --- Zig ---
  "http.zig|https://github.com/karlseguin/http.zig.git|master|zig"                                # Zig HTTP server library; small
  # --- Dart ---
  "dart-samples|https://github.com/dart-lang/samples.git|main|dart"                               # Dart sample apps
  # --- Lua ---
  "kickstart.nvim|https://github.com/nvim-lua/kickstart.nvim.git|master|lua"                      # Neovim config sample
  # --- Elixir ---
  "phoenix-todo-list|https://github.com/dwyl/phoenix-todo-list-tutorial.git|main|elixir"          # Phoenix sample app
  # --- Razor ---
  "aspnetcore-docs-samples|https://github.com/dotnet/AspNetCore.Docs.Samples.git|main|razor"     # ASP.NET Core docs companion: 737 .cshtml/.razor view templates
  # --- Fish ---
  "tide|https://github.com/IlanCosman/tide.git|main|fish"                                        # Pure-fish prompt theme: 117 .fish files (functions, completions, conf.d)
  # --- Just ---
  "just|https://github.com/casey/just.git|master|just"                                           # Just build runner: dogfooded top-level justfile + tests/ parser fixtures
  # --- Proto ---
  "grpc-go-examples|https://github.com/grpc/grpc-go.git|master|proto|examples"                   # gRPC-Go examples/ subtree: dozens of .proto service/message definitions
  # --- GraphQL ---
  "apollo-server|https://github.com/apollographql/apollo-server.git|main|graphql"                # Apollo Server: GraphQL schema SDL + resolvers across packages
  # --- HCL ---
  "terraform-aws-vpc|https://github.com/terraform-aws-modules/terraform-aws-vpc.git|master|hcl"  # Terraform AWS VPC module: canonical HCL resource/variable/output definitions
  # --- K8s YAML ---
  "argocd-example-apps|https://github.com/argoproj/argocd-example-apps.git|master|yaml"          # Argo CD example apps: canonical Deployment/Service/Ingress/ConfigMap manifests
  # --- Helm ---
  "prometheus-helm|https://github.com/prometheus-community/helm-charts.git|main|yaml|charts/prometheus" # Prometheus Helm chart: templates/, values.yaml, Chart.yaml — sparse subtree
  # --- GHA ---
  "starter-workflows|https://github.com/actions/starter-workflows.git|main|yaml"                 # Official GitHub Actions starter workflows: ci/, deployments/, automation/, code-scanning/, pages/
  # --- OpenAPI ---
  "openapi-stripe|https://github.com/APIs-guru/openapi-directory.git|main|yaml|APIs/stripe.com"  # OpenAPI directory — Stripe API spec subtree (yaml extractor handles OpenAPI documents)
  # --- ORMs/Frameworks (chunk G, umbrella #301) ---
  # Sample apps that USE each ORM/framework, per Refs #96 corpus policy.
  # django-realworld (#176) and node-express-realworld (#178/Prisma) are already
  # listed above under Python and JavaScript respectively — not duplicated here.
  "microblog|https://github.com/miguelgrinberg/microblog.git|main|python"                          # SQLAlchemy sample app (#174)
  "fastapi-realworld|https://github.com/nsidnev/fastapi-realworld-example-app.git|master|python"   # FastAPI sample app (#175)
  "sequelize-express-example|https://github.com/sequelize/express-example.git|master|javascript"   # Sequelize sample app (#177)
  "golang-gin-realworld|https://github.com/gothinkster/golang-gin-realworld-example-app.git|main|go" # GORM sample app (#179)
  "actix-diesel-realworld|https://github.com/snamiki1212/realworld-v1-rust-actix-web-diesel.git|main|rust" # Diesel sample app (#180)
  # --- Build tools (chunk H, umbrella #309) ---
  # Manifest fixtures: each repo's root Cargo.toml / pom.xml / package.json /
  # tsconfig is the canonical artifact for the cross/manifest extractor.
  "tokio|https://github.com/tokio-rs/tokio.git|master|rust"                                          # Cargo.toml workspace manifest (#168)
  "maven|https://github.com/apache/maven.git|master|java"                                            # pom.xml multi-module manifest (#170)
  "pnpm|https://github.com/pnpm/pnpm.git|main|javascript"                                            # package.json workspace manifest (#172)
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
  "ios-oss|https://github.com/kickstarter/ios-oss.git|main|swift"                                      # iOS UIKit sample app (#198)
  "sample-food-truck|https://github.com/apple/sample-food-truck.git|main|swift"                        # iOS SwiftUI sample app (#201)
  "android-architecture|https://github.com/googlesamples/android-architecture.git|main|java"           # Android Java sample app (#205)
  "compose-samples|https://github.com/android/compose-samples.git|main|kotlin"                         # Android Kotlin Compose sample apps (#207)
  "flutter-samples|https://github.com/flutter/samples.git|main|dart"                                   # Flutter (Dart) sample apps (#209)
  "react-native|https://github.com/facebook/react-native.git|main|javascript|template"                 # React Native: template/ subtree per umbrella body (#212)
  "ionic-conference-app|https://github.com/ionic-team/ionic-conference-app.git|main|typescript"        # Ionic / Capacitor sample app (#215)
  "maui-samples|https://github.com/dotnet/maui-samples.git|main|csharp"                                # .NET MAUI sample apps (#217)
  # --- Declarative IaC (chunk N, umbrella #308) ---
  # Sample/canonical fixtures for declarative IaC + container orchestration
  # languages, per Refs #96 corpus policy. Each entry pinned to the SHA
  # recorded in its child issue. ArgoCD (#211) is intentionally NOT re-listed
  # here: argocd-example-apps is already present above under K8s YAML
  # (chunk F) and #211 closes via that single fixture.
  "aws-cloudformation-samples|https://github.com/aws-cloudformation/aws-cloudformation-samples.git|main|yaml"  # CloudFormation sample templates (#185)
  "kustomize|https://github.com/kubernetes-sigs/kustomize.git|master|yaml|examples"                            # Kustomize examples/ subtree (#189)
  "ansible-for-devops|https://github.com/geerlingguy/ansible-for-devops.git|master|yaml"                       # Ansible playbooks/roles sample (#192)
  "chef-runit|https://github.com/chef-cookbooks/runit.git|main|ruby"                                           # Chef cookbook (Ruby DSL) (#195)
  "puppet-control-repo|https://github.com/puppetlabs/control-repo.git|production|ruby"                         # Puppet control repo (Ruby DSL) (#199)
  "awesome-compose|https://github.com/docker/awesome-compose.git|master|yaml"                                  # Docker Compose canonical samples (#204)
  "nomad-pack|https://github.com/hashicorp/nomad-pack.git|main|hcl|registry"                                   # Nomad Pack registry/ subtree (#208)
  # --- Relational ORMs missing (chunk O, umbrella #315) ---
  # Sample apps that USE each relational ORM / query builder, per Refs #96
  # corpus policy. Each entry pinned to the SHA recorded in its child issue.
  "spring-framework-petclinic|https://github.com/spring-petclinic/spring-framework-petclinic.git|main|java"            # Hibernate-only sample app (#251)
  "joal|https://github.com/anthonyraymond/joal.git|master|java"                                                        # jOOQ sample app (#257)
  "jpetstore-6|https://github.com/mybatis/jpetstore-6.git|master|java"                                                 # MyBatis sample app (#259)
  "nestjs-realworld-typeorm|https://github.com/lujakob/nestjs-realworld-example-app.git|master|typescript"             # TypeORM sample app (#264)
  "express-bookshelf-realworld|https://github.com/tanem/express-bookshelf-realworld-example-app.git|master|javascript" # Knex sample app (#269)
  "nestjs-realworld-mikroorm|https://github.com/mikro-orm/nestjs-realworld-example-app.git|master|typescript"          # MikroORM sample app (#274)
  "ent|https://github.com/ent/ent.git|master|go|entc/integration/ent"                                                  # Ent sample (entc/integration/ent subtree) (#280)
  "sqlc-examples|https://github.com/sqlc-dev/sqlc.git|main|go|examples"                                                # sqlc examples/ subtree (#284)
  "fabric-ca|https://github.com/hyperledger/fabric-ca.git|main|go"                                                     # sqlx (Go) sample app (#287)
  "lean|https://github.com/jenssegers/lean.git|master|php"                                                             # Eloquent (standalone) sample app (#288)
  "sea-orm-examples|https://github.com/SeaQL/sea-orm.git|master|rust|examples"                                         # SeaORM examples/ subtree (#289)
  "netcore-boilerplate|https://github.com/lkurzyniec/netcore-boilerplate.git|master|csharp"                            # Dapper sample app (#290)
  # --- NoSQL/cache/streaming (chunk P, umbrella #316) ---
  # Canonical client/driver fixtures for the major NoSQL, cache, and streaming
  # ecosystems, per Refs #96 corpus policy. Each entry pinned to the SHA
  # recorded in its child issue.
  "mongoose|https://github.com/Automattic/mongoose.git|master|javascript"                                     # MongoDB Node.js ODM (#213)
  "pymongo|https://github.com/mongodb/mongo-python-driver.git|master|python"                                  # MongoDB Python driver (#216)
  "motor|https://github.com/mongodb/motor.git|master|python"                                                  # MongoDB async Python driver (#219)
  "mongo-go-driver|https://github.com/mongodb/mongo-go-driver.git|master|go"                                  # MongoDB Go driver (#220)
  "mongo-java-driver|https://github.com/mongodb/mongo-java-driver.git|main|java"                              # MongoDB Java driver (#221)
  "redis-py|https://github.com/redis/redis-py.git|master|python"                                              # Redis Python client (#222)
  "ioredis|https://github.com/redis/ioredis.git|main|typescript"                                              # Redis Node.js TypeScript client (#223)
  "go-redis|https://github.com/redis/go-redis.git|master|go"                                                  # Redis Go client (#225)
  "lettuce|https://github.com/redis/lettuce.git|main|java"                                                    # Redis Java client (#226)
  "cassandra-java-driver|https://github.com/datastax/java-driver.git|4.x|java"                                # Cassandra DataStax Java driver (#228)
  "aws-sdk-go-v2|https://github.com/aws/aws-sdk-go-v2.git|main|go"                                            # DynamoDB / AWS Go SDK v2 (#230)
  "couchbase-gocb|https://github.com/couchbase/gocb.git|master|go"                                            # Couchbase Go SDK (#232)
  "rabbitmq-tutorials|https://github.com/rabbitmq/rabbitmq-tutorials.git|main|python"                         # RabbitMQ amqp client tutorials (#234)
  "nats.go|https://github.com/nats-io/nats.go.git|main|go"                                                    # NATS Go client (#236)
  "aws-sdk-js-v3|https://github.com/aws/aws-sdk-js-v3.git|main|typescript"                                    # AWS SQS / AWS JS SDK v3 (#237)
  # --- Programmatic IaC (chunk M, umbrella #314) ---
  # Programmatic IaC samples across CDK / Pulumi / Bicep / SAM / Serverless
  # Framework / Crossplane, per Refs #96 corpus policy. Each entry pinned to
  # the SHA recorded in its child issue. Multi-flavor monorepos
  # (aws-cdk-examples, pulumi/examples) get one entry per language flavor with
  # a unique name and a flavor-scoped sparse-path so cone-mode sparse-checkout
  # keeps each working tree small.
  "aws-cdk-examples-typescript|https://github.com/aws-samples/aws-cdk-examples.git|main|typescript|typescript" # AWS CDK TypeScript (#182) SHA f4143ebe9746
  "aws-cdk-examples-python|https://github.com/aws-samples/aws-cdk-examples.git|main|python|python"             # AWS CDK Python (#184) SHA f4143ebe9746
  "aws-cdk-examples-java|https://github.com/aws-samples/aws-cdk-examples.git|main|java|java"                   # AWS CDK Java (#187) SHA f4143ebe9746
  "aws-cdk-examples-csharp|https://github.com/aws-samples/aws-cdk-examples.git|main|csharp|csharp"             # AWS CDK .NET (#188) SHA f4143ebe9746
  "aws-cdk-examples-go|https://github.com/aws-samples/aws-cdk-examples.git|main|go|go"                         # AWS CDK Go (#191) SHA f4143ebe9746
  "pulumi-examples-typescript|https://github.com/pulumi/examples.git|master|typescript|aws-ts-webserver"       # Pulumi TypeScript (#194) SHA d9173c2ed496
  "pulumi-examples-python|https://github.com/pulumi/examples.git|master|python|aws-py-webserver"               # Pulumi Python (#196) SHA d9173c2ed496
  "pulumi-examples-go|https://github.com/pulumi/examples.git|master|go|aws-go-webserver"                       # Pulumi Go (#200) SHA d9173c2ed496
  "pulumi-examples-csharp|https://github.com/pulumi/examples.git|master|csharp|aws-cs-webserver"               # Pulumi .NET (#202) SHA d9173c2ed496
  "azure-quickstart-templates|https://github.com/Azure/azure-quickstart-templates.git|master|bicep|quickstarts/microsoft.storage" # Azure Bicep (#206) SHA d4267860bf57
  "aws-sam-cli-app-templates|https://github.com/aws/aws-sam-cli-app-templates.git|master|yaml|python3.12"      # AWS SAM (#210) SHA c7285973a74a
  "serverless-examples|https://github.com/serverless/examples.git|v4|yaml|aws-node-http-api"                   # Serverless Framework (#214) SHA 631c0739a793
  "crossplane|https://github.com/crossplane/crossplane.git|main|yaml|cluster/meta"                             # Crossplane (#218) SHA 2ddc36457725
  # --- DB migration tools (chunk Y, umbrella #317) ---
  # Versioned-SQL migration tooling across major ecosystems, per Refs #87
  # corpus policy. Each entry pinned to the SHA recorded in the umbrella body.
  # Sparse-paths are applied to the two large monorepos (flyway, liquibase);
  # the remaining repos are small enough to clone in full.
  "flyway|https://github.com/flyway/flyway.git|main|java|flyway-core"                                          # Flyway versioned/repeatable SQL migrations (#317) SHA ce65ee118f01
  "liquibase|https://github.com/liquibase/liquibase.git|main|java|liquibase-standard"                          # Liquibase changelog formats + changesets (#317) SHA e9c06031abc8
  "alembic|https://github.com/sqlalchemy/alembic.git|main|python"                                              # Alembic env.py + revision graph + op.* DSL (#317) SHA 4d1e38cac108
  "knex|https://github.com/knex/knex.git|master|javascript"                                                    # Knex schema-builder migrations + query builder (#317) SHA af57d1ec662a
  "goose|https://github.com/pressly/goose.git|main|go"                                                         # Goose -- +goose Up/Down SQL annotations + Go migrations (#317) SHA e3235f7041e1
  "migrate-mongo|https://github.com/seppevs/migrate-mongo.git|master|javascript"                               # MongoDB migration scripts up/down handler (#317) SHA 1f5e5f953491
  "sequel|https://github.com/jeremyevans/sequel.git|master|ruby"                                               # Sequel.migration { up/down } blocks (#317) SHA 694ea7798374
  # --- CI/CD pipelines (chunk Q, umbrella #318) ---
  # Canonical fixtures for CI/CD pipeline formats not yet exercised by the
  # corpus, per Refs #87. Each entry pinned to the SHA recorded in umbrella
  # #318. Sparse-paths keep working trees small for the larger monorepos
  # (gitlab-runner, jenkins, tektoncd/pipeline, skaffold, tilt).
  "gitlab-runner|https://gitlab.com/gitlab-org/gitlab-runner.git|main|yaml|ci"                                  # GitLab CI pipeline (#318) SHA d098a819531c
  "circleci-demo-python-django|https://github.com/CircleCI-Public/circleci-demo-python-django.git|master|yaml|.circleci" # CircleCI orbs/workflows (#318) SHA 2bbf84b270e6
  "jenkins|https://github.com/jenkinsci/jenkins.git|master|groovy"                                              # Jenkins declarative + scripted Groovy (#318) SHA 8fef7afff3e6
  "tektoncd-pipeline|https://github.com/tektoncd/pipeline.git|main|yaml|examples"                               # Tekton Task/Pipeline/PipelineRun CRDs (#318) SHA bdd46842b047
  "drone|https://github.com/drone/drone.git|main|yaml"                                                          # Drone pipeline steps + plugins (#318) SHA 9f37938bc913
  "buildkite-agent|https://github.com/buildkite/agent.git|main|yaml|.buildkite"                                 # Buildkite pipeline.yml + plugins (#318) SHA 561444c7fc59
  "skaffold|https://github.com/GoogleContainerTools/skaffold.git|main|yaml|examples"                            # Skaffold build/deploy/test profiles (#318) SHA 95531aa9b308
  "tilt|https://github.com/tilt-dev/tilt.git|master|starlark|integration"                                       # Tilt Starlark Tiltfile resources (#318) SHA 98108ca6664b
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
  "bazel|https://github.com/bazelbuild/bazel.git|master|java"                                                  # Bazel BUILD/Starlark targets, deps (#320) SHA bca007ec74f2
  "gnu-make|https://git.savannah.gnu.org/git/make.git|master|c"                                                # GNU Make rules, vars, includes (#320) SHA b3802782de3e
  "cmake|https://github.com/Kitware/CMake.git|master|cpp"                                                      # CMake list files, modules, find-packages (#320) SHA 7e1b633a8978
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
  "nginx|https://github.com/nginx/nginx.git|master|nginx-conf|conf"                                            # Nginx server/location/upstream blocks (#326) SHA 631bfa194d5a
  "apache-httpd|https://github.com/apache/httpd.git|trunk|apache-httpd-conf|docs/conf"                         # Apache HTTPD VirtualHost/Directory directives (#326) SHA c11a7f9994f6
  "caddy|https://github.com/caddyserver/caddy.git|master|caddyfile|caddyconfig"                                # Caddyfile + JSON config (#326) SHA 9c78b97f9e79
  "traefik|https://github.com/traefik/traefik.git|master|traefik-dynamic|integration/fixtures"                 # Traefik static + dynamic YAML config (#326) SHA edd7d2eb333c
  "kong|https://github.com/Kong/kong.git|master|kong-declarative|spec/fixtures"                                # Kong declarative config services/routes/plugins (#326) SHA 58f2daa56b90
  "envoy|https://github.com/envoyproxy/envoy.git|main|envoy-yaml|configs"                                      # Envoy listener/cluster/route YAML (#326) SHA 3ddecad194fc
  "haproxy|https://github.com/haproxy/haproxy.git|master|haproxy-cfg|examples"                                 # HAProxy frontend/backend/acl config (#326) SHA efb36c0dafd5
  # --- API/Spec/IDL alts (chunk U, umbrella #325) ---
  # API spec / IDL alternatives beyond OpenAPI, per Refs #87 corpus policy.
  # Each entry pinned to the SHA verified in the umbrella body via
  # git ls-remote --symref against canonical upstream branches.
  # Sparse-paths are applied to the three large monorepos (smithy, avro,
  # thrift); the remaining four are small enough to clone in full. Where
  # the umbrella listed multiple sparse subtrees (or glob patterns not
  # supported by cone-mode sparse-checkout), one canonical IDL/schema
  # subtree per repo is selected — broad extractor coverage is preserved.
  "asyncapi-spec|https://github.com/asyncapi/spec.git|master|asyncapi"                                          # AsyncAPI channels/messages/bindings (#325) SHA 94ff695acb10
  "smithy|https://github.com/smithy-lang/smithy.git|main|smithy|smithy-model"                                   # Smithy IDL services/operations/shapes (#325) SHA 22a34991defb
  "avro|https://github.com/apache/avro.git|main|avro|lang"                                                      # Avro schemas (.avsc) and IDL (.avdl) (#325) SHA 892d6997dcb6
  "thrift|https://github.com/apache/thrift.git|master|thrift|tutorial"                                          # Thrift IDL services/structs (#325) SHA f39cecc4d5b6
  "json-schema-spec|https://github.com/json-schema-org/json-schema-spec.git|main|json-schema"                   # JSON Schema draft definitions (#325) SHA 5794814cca9e
  "raml-spec|https://github.com/raml-org/raml-spec.git|master|raml"                                             # RAML 1.0 API definitions (#325) SHA 3ba244ade44c
  "api-blueprint|https://github.com/apiaryio/api-blueprint.git|master|api-blueprint"                            # API Blueprint markdown specs (#325) SHA 86fc3a128a93
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
  "zod|https://github.com/colinhacks/zod.git|main|typescript"                                                  # Zod schema definitions, refinements (#324) SHA b6071fc0ad2b paths src/ tests/
  "joi|https://github.com/hapijs/joi.git|master|javascript"                                                    # Joi schema + extensions (#324) SHA 048fe05b8235 paths lib/ test/
  "yup|https://github.com/jquense/yup.git|master|typescript"                                                   # Yup object/array validation (#324) SHA ff31eee8a2b1 paths src/ test/
  "pydantic|https://github.com/pydantic/pydantic.git|main|python"                                              # pydantic v2 BaseModel + validators (#324) SHA 7a369fb502a4 paths pydantic/ tests/
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
  "aws-lambda-python-runtime-interface-client|https://github.com/aws/aws-lambda-python-runtime-interface-client.git|main|python|awslambdaric"  # Python Lambda runtime interface, handler bootstrap (#319) SHA f11e7c5c5cc7
  "aws-lambda-java-libs|https://github.com/aws/aws-lambda-java-libs.git|main|java|aws-lambda-java-core"               # Java Lambda handler interfaces, event POJOs (#319) SHA c4dcbab4ffed
  "aws-lambda-go|https://github.com/aws/aws-lambda-go.git|main|go|lambda"                                              # Go Lambda handler signatures, event types (#319) SHA 815d21f41769
  "aws-lambda-rust-runtime|https://github.com/awslabs/aws-lambda-rust-runtime.git|main|rust|lambda-runtime"            # Rust Lambda runtime crate, handler macros (#319) SHA 01237499db5f
  "vercel-examples|https://github.com/vercel/examples.git|main|typescript|edge-functions"                              # Vercel Edge/Serverless function shape, vercel.json (#319) SHA 72aaac1ba427
  "netlify-functions|https://github.com/netlify/functions.git|main|typescript|src"                                     # Netlify Functions handler types, netlify.toml (#319) SHA c3f47247079e
  "cloudflare-workers-sdk|https://github.com/cloudflare/workers-sdk.git|main|typescript|packages/wrangler"             # Cloudflare Workers fetch handler, wrangler.toml (#319) SHA dba84c225f41
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
  "xstate|https://github.com/statelyai/xstate.git|main|typescript|packages/core"                              # XState `createMachine`, statecharts (#321) SHA f79ea13febe4
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
  "hugoDocs|https://github.com/gohugoio/hugoDocs.git|master|go|content"                                       # Hugo templates, shortcodes, TOML/YAML front-matter (#298) SHA e6abf5644f3f
  "jekyll|https://github.com/jekyll/jekyll.git|master|ruby|lib"                                               # Liquid templating, Jekyll plugin model (#298) SHA 202df571314b
  "hexo-site|https://github.com/hexojs/site.git|master|javascript|source"                                     # Hexo theming + Markdown content tree (#298) SHA fcf644467039
  "mkdocs|https://github.com/mkdocs/mkdocs.git|master|python|mkdocs"                                          # MkDocs plugin/theme system, mkdocs.yml config (#298) SHA 2862536793b3
  "sphinx|https://github.com/sphinx-doc/sphinx.git|master|python|sphinx"                                      # RST extractor, Sphinx directives, conf.py (#298) SHA cc7c6f435ad3
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
  "seleniumhq-examples|https://github.com/SeleniumHQ/seleniumhq.github.io.git|trunk|multi|examples"           # Selenium WebDriver examples across languages (#312) SHA 203cffca2951
  "junit5-samples|https://github.com/junit-team/junit5-samples.git|main|java"                                 # JUnit 5 Jupiter API, parameterized tests (#312) SHA c8828652e601 paths junit5-jupiter-starter-gradle/ junit5-jupiter-starter-maven/
  "rspec-rails|https://github.com/rspec/rspec-rails.git|main|ruby"                                            # RSpec describe/it/expect, Rails matchers (#312) SHA 810f2a48950c paths lib/ spec/
  "pytest|https://github.com/pytest-dev/pytest.git|main|python"                                               # pytest fixtures, parametrize, conftest, plugins (#312) SHA b10612810dc6 paths src/ testing/
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
  "socket.io|https://github.com/socketio/socket.io.git|main|typescript|packages/socket.io"                    # Socket.IO server/client emit/on, namespaces (#323) SHA 439a8f669c67 paths packages/socket.io/ examples/
  "pusher-js|https://github.com/pusher/pusher-js.git|master|typescript|src"                                    # Pusher channel subscribe/bind, presence channels (#323) SHA e58a5d22e063 paths src/ spec/
  "ably-js|https://github.com/ably/ably-js.git|main|typescript|src"                                            # Ably Realtime channels, message envelopes (#323) SHA 498d26dfb16b paths src/ test/
  "MQTT.js|https://github.com/mqttjs/MQTT.js.git|main|typescript|src"                                          # MQTT publish/subscribe, topic filters, QoS (#323) SHA a4e9a92c8710 paths src/ test/
  "pion-webrtc|https://github.com/pion/webrtc.git|main|go"                                                     # WebRTC peer connection, data channels in Go (#323) SHA 9654161f9b69 paths /. examples/
  "mdn-dom-examples|https://github.com/mdn/dom-examples.git|main|javascript|web-sockets"                       # Native WebSocket usage examples (#323) SHA f8aa45c93985 paths server-sent-events/ web-sockets/
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
TMPDIR_AGG="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_AGG"' EXIT

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

echo "==> wrote report: $REPORT"
echo "$REPORT"
