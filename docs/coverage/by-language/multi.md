<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# Uncategorized

**Frameworks**: 0 · **Tools**: 4 · **ORMs**: 0 · **Other**: 46

Back to [summary](../summary.md).

## Tools

| Name | dependency_graph | lockfile_parsing | manifest_parsing | target_extraction | Notes |
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
| [AWS CDK](../detail/infra.resource.aws-cdk.md) | [infrastructure](../by-category/infrastructure.md) | ✅ | |
| [AWS CloudFormation](../detail/infra.resource.cloudformation.md) | [infrastructure](../by-category/infrastructure.md) | ⚠️ | |
| [AWS EventBridge](../detail/msg.broker.eventbridge.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [AWS SNS (IaC-declared)](../detail/msg.broker.sns.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [AWS SQS](../detail/msg.broker.sqs.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Apache Kafka](../detail/msg.broker.kafka.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Apache Pulsar](../detail/msg.broker.pulsar.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Auth policy resolver (Python / NestJS / Go / Ruby / ASP.NET — Phases 2-4 of #1942)](../detail/security.auth-other.md) | [security](../by-category/security.md) | ❌ | |
| [Azure Event Grid](../detail/msg.broker.eventgrid.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [CSRF heuristic detector](../detail/security.csrf.md) | [security](../by-category/security.md) | ✅ | |
| [CloudEvents](../detail/msg.broker.cloudevents.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Datadog APM / StatsD](../detail/infra.observability.datadog.md) | [observability](../by-category/observability.md) | ❌ | |
| [Debezium / Kafka Connect CDC](../detail/msg.broker.debezium.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [GCP Pub/Sub](../detail/msg.broker.gcp-pubsub.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Generic logging-config extractor (Python logging, Go slog, Node winston/pino, .NET NLog/Serilog, log4j/logback)](../detail/infra.observability.logging-config.md) | [observability](../by-category/observability.md) | ✅ | |
| [GitHub Actions workflows](../detail/config.github-actions.md) | [configuration](../by-category/configuration.md) | ✅ | |
| [GitLab CI](../detail/config.gitlab-ci.md) | [configuration](../by-category/configuration.md) | ⚠️ | |
| [GraphQL SDL (Query/Mutation/Subscription)](../detail/protocol.graphql.md) | [protocol](../by-category/protocol.md) | ⚠️ | |
| [GraphQL subscriptions](../detail/msg.graphql-subscriptions.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Helm charts](../detail/infra.resource.helm.md) | [infrastructure](../by-category/infrastructure.md) | ❌ | |
| [Jenkinsfile / Jenkins Pipeline DSL](../detail/config.jenkins.md) | [configuration](../by-category/configuration.md) | ❌ | |
| [Kafka Streams / Faust](../detail/msg.kafka-streams.md) | [message_broker](../by-category/message_broker.md) | ❌ | |
| [Kubernetes manifests](../detail/infra.resource.kubernetes.md) | [infrastructure](../by-category/infrastructure.md) | ✅ | |
| [MQTT](../detail/msg.broker.mqtt.md) | [message_broker](../by-category/message_broker.md) | ❌ | |
| [NATS](../detail/msg.broker.nats.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [New Relic](../detail/infra.observability.newrelic.md) | [observability](../by-category/observability.md) | ❌ | |
| [OpenAPI / Swagger spec](../detail/protocol.openapi.md) | [protocol](../by-category/protocol.md) | ✅ | |
| [OpenTelemetry instrumentation](../detail/infra.observability.opentelemetry.md) | [observability](../by-category/observability.md) | ❌ | |
| [Prometheus client libraries](../detail/infra.observability.prometheus.md) | [observability](../by-category/observability.md) | ❌ | |
| [Protocol Buffers (.proto)](../detail/protocol.protobuf.md) | [protocol](../by-category/protocol.md) | ✅ | |
| [Pulumi](../detail/infra.resource.pulumi.md) | [infrastructure](../by-category/infrastructure.md) | ✅ | |
| [RabbitMQ](../detail/msg.broker.rabbitmq.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Redis pub/sub + Streams](../detail/msg.broker.redis.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [SQL injection heuristic (f-string / .format() / % interpolation into SQL)](../detail/security.sql-injection.md) | [security](../by-category/security.md) | ✅ | |
| [Secret material extraction (Phase 1 security audit)](../detail/security.secrets.md) | [security](../by-category/security.md) | ✅ | |
| [Sentry SDK](../detail/infra.observability.sentry.md) | [observability](../by-category/observability.md) | ❌ | |
| [Server-Sent Events](../detail/msg.sse.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Terraform / OpenTofu / Vault / Nomad / Packer / Waypoint](../detail/infra.resource.terraform.md) | [infrastructure](../by-category/infrastructure.md) | ✅ | |
| [WebSocket channels](../detail/msg.websocket.md) | [message_broker](../by-category/message_broker.md) | ⚠️ | |
| [Webhook inbound (Stripe, GitHub, Twilio, Slack, SendGrid, Mailgun, ...)](../detail/msg.webhook.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [docker-compose.yml](../detail/config.docker-compose.md) | [configuration](../by-category/configuration.md) | ✅ | |
| [gRPC services](../detail/protocol.grpc.md) | [protocol](../by-category/protocol.md) | ✅ | |
