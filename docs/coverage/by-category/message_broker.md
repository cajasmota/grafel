<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# message_broker

**Total**: 62 records · **C/C++**: 3 · **C#**: 9 · **elixir**: 2 · **go**: 5 · **JS/TS**: 4 · **multi**: 22 · **php**: 1 · **python**: 6 · **ruby**: 7 · **rust**: 3

Back to [summary](../summary.md). Bucket: **Other**.



## Schedulers

| Language | Name | Consumer extraction | Status | Notes |
|---|---|---|---|---|
| [C#](../by-language/csharp.md) | [Coravel (.NET task scheduler / queue / mailer)](../detail/msg.coravel.md) | ✅ | ✅ | |
| [C#](../by-language/csharp.md) | [Hangfire RecurringJob (.NET scheduled jobs)](../detail/msg.hangfire-recurring.md) | 🟢 | 🟢 | |
| [C#](../by-language/csharp.md) | [Quartz.NET (.NET job scheduler)](../detail/msg.quartz-net.md) | ✅ | ✅ | |
| [go](../by-language/go.md) | [robfig/cron (Go scheduler)](../detail/msg.go-cron.md) | 🟢 | 🟢 | |
| [JS/TS](../by-language/jsts.md) | [node-schedule (Node scheduled jobs)](../detail/msg.node-schedule.md) | 🟢 | 🟢 | |
| [python](../by-language/python.md) | [APScheduler (Python advanced scheduler)](../detail/msg.apscheduler.md) | 🟢 | 🟢 | |
| [ruby](../by-language/ruby.md) | [rufus-scheduler (Ruby in-process scheduler)](../detail/msg.rufus-scheduler.md) | 🟢 | 🟢 | |
| [ruby](../by-language/ruby.md) | [whenever (Ruby cron / config/schedule.rb)](../detail/msg.whenever.md) | 🟢 | 🟢 | |

## Task Queues

| Language | Name | Consumer extraction | Producer extraction | Topic attribution | Status | Notes |
|---|---|---|---|---|---|---|
| [go](../by-language/go.md) | [asynq (Go task queue)](../detail/msg.asynq.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [JS/TS](../by-language/jsts.md) | [BullMQ / bull (Node task queue)](../detail/msg.bullmq.md) | ✅ | ✅ | ✅ | ✅ | |
| [JS/TS](../by-language/jsts.md) | [Inngest (durable functions / event-driven jobs)](../detail/msg.inngest.md) | 🟢 | — | 🟢 | 🟢 | |
| [php](../by-language/php.md) | [Laravel Queue (queued Jobs / dispatch)](../detail/msg.broker.laravel-queue.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [python](../by-language/python.md) | [Celery (Python task queue)](../detail/msg.celery.md) | ✅ | ✅ | ✅ | ✅ | |
| [python](../by-language/python.md) | [Dramatiq (Python task queue)](../detail/msg.dramatiq.md) | ✅ | ✅ | — | ✅ | |
| [ruby](../by-language/ruby.md) | [Rails ActiveJob (queue abstraction)](../detail/msg.broker.activejob.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [ruby](../by-language/ruby.md) | [Resque (Ruby task queue)](../detail/msg.resque.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [ruby](../by-language/ruby.md) | [Sidekiq (Ruby task queue)](../detail/msg.sidekiq.md) | 🟢 | 🟢 | — | 🟢 | |

## Brokers

| Language | Name | Consumer extraction | Producer extraction | Topic attribution | Status | Notes |
|---|---|---|---|---|---|---|
| [C/C++](../by-language/c-cpp.md) | [MQTT (Paho C/C++ / Mosquitto)](../detail/lang.c-cpp.framework.mqtt.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [C/C++](../by-language/c-cpp.md) | [ZeroMQ (libzmq/cppzmq)](../detail/lang.c-cpp.framework.zeromq.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [C/C++](../by-language/c-cpp.md) | [librdkafka (C/C++ Kafka client)](../detail/lang.c-cpp.framework.librdkafka.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [C#](../by-language/csharp.md) | [Kafka — C# (Confluent.Kafka)](../detail/msg.broker.kafka-dotnet.md) | 🟢 | 🟢 | ✅ | 🟢 | |
| [C#](../by-language/csharp.md) | [MassTransit (.NET cross-process service bus)](../detail/msg.masstransit.md) | ✅ | ✅ | 🟢 | 🟢 | |
| [C#](../by-language/csharp.md) | [MediatR (.NET in-process CQRS / mediator)](../detail/msg.mediatr.md) | ✅ | ✅ | ✅ | ✅ | |
| [C#](../by-language/csharp.md) | [NServiceBus / Rebus (IHandleMessages<T> convention)](../detail/msg.nservicebus.md) | ✅ | ✅ | 🟢 | 🟢 | |
| [C#](../by-language/csharp.md) | [RabbitMQ — C# (RabbitMQ.Client)](../detail/msg.broker.rabbitmq-dotnet.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [C#](../by-language/csharp.md) | [Wolverine (.NET convention-based message bus)](../detail/msg.wolverine.md) | ✅ | ✅ | 🟢 | 🟢 | |
| [elixir](../by-language/elixir.md) | [Broadway (Elixir data pipelines)](../detail/lang.elixir.framework.broadway.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [go](../by-language/go.md) | [Kafka — Go (Sarama / segmentio/kafka-go)](../detail/msg.broker.kafka-go.md) | 🟢 | ✅ | 🟢 | 🟢 | |
| [go](../by-language/go.md) | [NATS — Go (nats.go / JetStream)](../detail/msg.broker.nats-go.md) | ✅ | ✅ | ✅ | ✅ | |
| [go](../by-language/go.md) | [RabbitMQ — Go (amqp091-go)](../detail/msg.broker.rabbitmq-go.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [JS/TS](../by-language/jsts.md) | [ORM model lifecycle-hook → handler TRIGGERS (TypeORM, Sequelize, Mongoose)](../detail/msg.orm-lifecycle-hooks-jsts.md) | ✅ | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [AMQP (generic)](../detail/msg.broker.amqp.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [AWS EventBridge](../detail/msg.broker.eventbridge.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [AWS SNS](../detail/msg.broker.sns.md) | ✅ | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [AWS SQS](../detail/msg.broker.sqs.md) | ✅ | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [Apache Kafka](../detail/msg.broker.kafka.md) | ✅ | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [Apache Pulsar](../detail/msg.broker.pulsar.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [Azure Event Grid](../detail/msg.broker.eventgrid.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [Azure Service Bus / Event Hubs](../detail/msg.broker.azure-service-bus.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [BullMQ / Bull cross-repo queue topic attribution](../detail/analysis.orchestration.bullmq.md) | ✅ | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [CloudEvents](../detail/msg.broker.cloudevents.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [Debezium (CDC)](../detail/msg.broker.debezium.md) | ✅ | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [GCP Pub/Sub](../detail/msg.broker.gcp-pubsub.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [Kafka Streams / Faust](../detail/msg.kafka-streams.md) | 🔴 | 🔴 | — | 🔴 | |
| [multi](../by-language/multi.md) | [MQTT](../detail/msg.broker.mqtt.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [NATS](../detail/msg.broker.nats.md) | ✅ | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [RabbitMQ](../detail/msg.broker.rabbitmq.md) | ✅ | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [Redis pub/sub & streams](../detail/msg.broker.redis.md) | ✅ | ✅ | ✅ | ✅ | |
| [python](../by-language/python.md) | [Django signals (intra-repo pub/sub)](../detail/msg.django-signals.md) | ✅ | ✅ | ✅ | ✅ | |
| [python](../by-language/python.md) | [ORM model lifecycle-hook → handler TRIGGERS (Django signals, SQLAlchemy events)](../detail/msg.orm-lifecycle-hooks-py.md) | ✅ | ✅ | ✅ | ✅ | |
| [ruby](../by-language/ruby.md) | [ORM model lifecycle-hook → handler TRIGGERS (ActiveRecord callbacks)](../detail/msg.orm-lifecycle-hooks-ruby.md) | ✅ | ✅ | ✅ | ✅ | |
| [rust](../by-language/rust.md) | [async-nats (NATS)](../detail/lang.rust.framework.async-nats.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [rust](../by-language/rust.md) | [lapin (AMQP/RabbitMQ)](../detail/lang.rust.framework.lapin.md) | 🟢 | 🟢 | 🟢 | 🟢 | |
| [rust](../by-language/rust.md) | [rdkafka (Kafka)](../detail/lang.rust.framework.rdkafka.md) | 🟢 | 🟢 | 🟢 | 🟢 | |

## Realtime Channels

| Language | Name | Consumer extraction | Producer extraction | Room channel grouping | Topic attribution | Status | Notes |
|---|---|---|---|---|---|---|---|
| [elixir](../by-language/elixir.md) | [Phoenix Channels](../detail/msg.phoenix-channels.md) | 🔴 | ✅ | ✅ | 🟢 | 🔴 | |
| [multi](../by-language/multi.md) | [GraphQL subscriptions](../detail/msg.graphql-subscriptions.md) | ✅ | ✅ | — | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [Server-Sent Events](../detail/msg.sse.md) | ✅ | ✅ | — | — | ✅ | |
| [multi](../by-language/multi.md) | [SignalR](../detail/msg.signalr.md) | 🔴 | ✅ | — | ✅ | 🔴 | |
| [multi](../by-language/multi.md) | [WebSocket](../detail/msg.websocket.md) | ✅ | ✅ | ✅ | 🟢 | 🟢 | |
| [python](../by-language/python.md) | [Django Channels](../detail/msg.django-channels.md) | — | — | ✅ | — | ✅ | |
| [ruby](../by-language/ruby.md) | [Rails ActionCable](../detail/msg.actioncable.md) | — | — | ✅ | — | ✅ | |

## Webhooks

| Language | Name | Consumer extraction | Producer extraction | Signature verification | Topic attribution | Status | Notes |
|---|---|---|---|---|---|---|---|
| [multi](../by-language/multi.md) | [Webhooks](../detail/msg.webhook.md) | ✅ | ✅ | ✅ | 🟢 | 🟢 | |
