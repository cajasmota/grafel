<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# Uncategorized

**Frameworks**: 0 · **Tools**: 4 · **ORMs**: 0 · **Other**: 91

Back to [summary](../summary.md).

## Tools

| Name | Dependency Graph | Lockfile Parsing | Manifest Parsing | Target Extraction | Notes |
|---|---|---|---|---|---|
| [Bazel / BUCK / WORKSPACE](../detail/build.bazel.md) | ✅ | — | — | ✅ | |
| [Dockerfile](../detail/build.dockerfile.md) | ✅ | — | — | ✅ | |
| [Justfile](../detail/build.justfile.md) | ✅ | — | — | ✅ | |
| [Makefile](../detail/build.makefile.md) | ❌ | — | — | ⚠️ | |

## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [.env (names-only — values stripped at extraction boundary)](../detail/config.dotenv.md) | [configuration](../by-category/configuration.md) | ✅ | |
| [.ini / setup.cfg / flake8 / mypy / pytest.ini](../detail/config.ini.md) | [configuration](../by-category/configuration.md) | ⚠️ | |
| [.toml](../detail/config.toml.md) | [configuration](../by-category/configuration.md) | ✅ | |
| [.yaml / .yml](../detail/config.yaml.md) | [configuration](../by-category/configuration.md) | ✅ | |
| [AMQP (generic)](../detail/msg.broker.amqp.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [AWS CDK](../detail/infra.iac.cdk.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [AWS CDK](../detail/infra.resource.aws-cdk.md) | [infrastructure](../by-category/infrastructure.md) | ✅ | |
| [AWS CloudFormation](../detail/infra.iac.cloudformation.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [AWS CloudFormation](../detail/infra.resource.cloudformation.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [AWS DynamoDB](../detail/db.dynamodb.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [AWS EventBridge](../detail/msg.broker.eventbridge.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [AWS SNS](../detail/msg.broker.sns.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [AWS SQS](../detail/msg.broker.sqs.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Ansible (playbooks)](../detail/infra.iac.ansible.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [Apache Cassandra (schema)](../detail/db.cassandra.md) | [infrastructure](../by-category/infrastructure.md) | ❌ | |
| [Apache Kafka](../detail/msg.broker.kafka.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Apache Pulsar](../detail/msg.broker.pulsar.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [Auth policy resolver (Python / NestJS / Go / Ruby / ASP.NET — Phases 2-4 of #1942)](../detail/security.auth-other.md) | [security](../by-category/security.md) | ❌ | |
| [Azure Bicep](../detail/infra.iac.bicep.md) | [infrastructure](../by-category/infrastructure.md) | ❌ | |
| [Azure Event Grid](../detail/msg.broker.eventgrid.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [Azure Pipelines](../detail/ci.azure-pipelines.md) | [configuration](../by-category/configuration.md) | ⚠️ | |
| [Azure Service Bus](../detail/msg.broker.azure-service-bus.md) | [message_broker](../by-category/message_broker.md) | ❌ | |
| [Bitbucket Pipelines](../detail/ci.bitbucket.md) | [configuration](../by-category/configuration.md) | ⚠️ | |
| [Buildkite](../detail/ci.buildkite.md) | [configuration](../by-category/configuration.md) | ⚠️ | |
| [CSRF heuristic detector](../detail/security.csrf.md) | [security](../by-category/security.md) | ✅ | |
| [CircleCI](../detail/ci.circleci.md) | [configuration](../by-category/configuration.md) | ⚠️ | |
| [ClickHouse](../detail/db.clickhouse.md) | [infrastructure](../by-category/infrastructure.md) | ❌ | |
| [CloudEvents](../detail/msg.broker.cloudevents.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [Datadog](../detail/infra.observability.datadog.md) | [observability](../by-category/observability.md) | ❌ | |
| [Debezium (CDC)](../detail/msg.broker.debezium.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Dockerfile](../detail/infra.container.dockerfile.md) | [infrastructure](../by-category/infrastructure.md) | ✅ | |
| [Drone CI](../detail/ci.drone.md) | [configuration](../by-category/configuration.md) | ❌ | |
| [Elastic APM](../detail/infra.observability.elastic-apm.md) | [observability](../by-category/observability.md) | ❌ | |
| [Elasticsearch (indices)](../detail/db.elasticsearch.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [GCP Pub/Sub](../detail/msg.broker.gcp-pubsub.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [Generic logging-config extractor (Python logging, Go slog, Node winston/pino, .NET NLog/Serilog, log4j/logback)](../detail/infra.observability.logging-config.md) | [observability](../by-category/observability.md) | ✅ | |
| [GitHub Actions](../detail/ci.github-actions.md) | [configuration](../by-category/configuration.md) | ⚠️ | |
| [GitHub Actions workflows](../detail/config.github-actions.md) | [configuration](../by-category/configuration.md) | ✅ | |
| [GitLab CI](../detail/ci.gitlab.md) | [configuration](../by-category/configuration.md) | ⚠️ | |
| [GitLab CI](../detail/config.gitlab-ci.md) | [configuration](../by-category/configuration.md) | ⚠️ | |
| [Grafana Loki](../detail/infra.observability.grafana-loki.md) | [observability](../by-category/observability.md) | ❌ | |
| [GraphQL](../detail/protocol.graphql.md) | [protocol](../by-category/protocol.md) | ⚠️ | |
| [GraphQL subscriptions](../detail/msg.graphql-subscriptions.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [HTTP Basic Auth](../detail/security.auth.basic.md) | [security](../by-category/security.md) | ❌ | |
| [Helm charts](../detail/infra.container.helm.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [Helm charts](../detail/infra.resource.helm.md) | [infrastructure](../by-category/infrastructure.md) | ❌ | |
| [Honeycomb](../detail/infra.observability.honeycomb.md) | [observability](../by-category/observability.md) | ❌ | |
| [JSON-RPC](../detail/protocol.jsonrpc.md) | [protocol](../by-category/protocol.md) | ❌ | |
| [JWT](../detail/security.auth.jwt.md) | [security](../by-category/security.md) | ❌ | |
| [Jenkins (Jenkinsfile)](../detail/ci.jenkins.md) | [configuration](../by-category/configuration.md) | ❌ | |
| [Jenkinsfile / Jenkins Pipeline DSL](../detail/config.jenkins.md) | [configuration](../by-category/configuration.md) | ❌ | |
| [Kafka Streams / Faust](../detail/msg.kafka-streams.md) | [message_broker](../by-category/message_broker.md) | ❌ | |
| [Kubernetes manifests](../detail/infra.container.kubernetes.md) | [infrastructure](../by-category/infrastructure.md) | ✅ | |
| [Kubernetes manifests](../detail/infra.resource.kubernetes.md) | [infrastructure](../by-category/infrastructure.md) | ✅ | |
| [Kustomize](../detail/infra.container.kustomize.md) | [infrastructure](../by-category/infrastructure.md) | ❌ | |
| [MQTT](../detail/msg.broker.mqtt.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [MongoDB (collections)](../detail/db.mongodb.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [MySQL / MariaDB (schema)](../detail/db.mysql.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [NATS](../detail/msg.broker.nats.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Neo4j](../detail/db.neo4j.md) | [infrastructure](../by-category/infrastructure.md) | ❌ | |
| [New Relic](../detail/infra.observability.newrelic.md) | [observability](../by-category/observability.md) | ❌ | |
| [OAuth2](../detail/security.auth.oauth2.md) | [security](../by-category/security.md) | ❌ | |
| [OIDC (OpenID Connect)](../detail/security.auth.oidc.md) | [security](../by-category/security.md) | ❌ | |
| [OpenAPI / Swagger](../detail/protocol.openapi.md) | [protocol](../by-category/protocol.md) | ⚠️ | |
| [OpenTelemetry (OTEL)](../detail/infra.observability.opentelemetry.md) | [observability](../by-category/observability.md) | ❌ | |
| [PostgreSQL (schema)](../detail/db.postgres.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [Prometheus](../detail/infra.observability.prometheus.md) | [observability](../by-category/observability.md) | ⚠️ | |
| [Protocol Buffers](../detail/protocol.protobuf.md) | [protocol](../by-category/protocol.md) | ⚠️ | |
| [Pulumi](../detail/infra.iac.pulumi.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [Pulumi](../detail/infra.resource.pulumi.md) | [infrastructure](../by-category/infrastructure.md) | ✅ | |
| [RabbitMQ](../detail/msg.broker.rabbitmq.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Redis (keys)](../detail/db.redis.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [Redis pub/sub & streams](../detail/msg.broker.redis.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [SAML](../detail/security.auth.saml.md) | [security](../by-category/security.md) | ❌ | |
| [SOAP / WSDL](../detail/protocol.soap.md) | [protocol](../by-category/protocol.md) | ❌ | |
| [SQL injection heuristic (f-string / .format() / % interpolation into SQL)](../detail/security.sql-injection.md) | [security](../by-category/security.md) | ✅ | |
| [SQLite (schema)](../detail/db.sqlite.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [Secret material extraction (Phase 1 security audit)](../detail/security.secrets.md) | [security](../by-category/security.md) | ✅ | |
| [Sentry](../detail/infra.observability.sentry.md) | [observability](../by-category/observability.md) | ❌ | |
| [Server-Sent Events](../detail/msg.sse.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Serverless Framework](../detail/infra.iac.serverless-framework.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [Session cookies](../detail/security.auth.session.md) | [security](../by-category/security.md) | ❌ | |
| [Snowflake](../detail/db.snowflake.md) | [infrastructure](../by-category/infrastructure.md) | ❌ | |
| [Terraform (HCL)](../detail/infra.iac.terraform.md) | [infrastructure](../by-category/infrastructure.md) | ✅ | |
| [Terraform / OpenTofu / Vault / Nomad / Packer / Waypoint](../detail/infra.resource.terraform.md) | [infrastructure](../by-category/infrastructure.md) | ✅ | |
| [Travis CI](../detail/ci.travis.md) | [configuration](../by-category/configuration.md) | ⚠️ | |
| [WebSocket](../detail/msg.websocket.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [Webhooks](../detail/msg.webhook.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [docker-compose.yml](../detail/config.docker-compose.md) | [configuration](../by-category/configuration.md) | ✅ | |
| [docker-compose.yml](../detail/infra.container.docker-compose.md) | [infrastructure](../by-category/infrastructure.md) | ✅ | |
| [gRPC](../detail/protocol.grpc.md) | [protocol](../by-category/protocol.md) | ✅ | |
