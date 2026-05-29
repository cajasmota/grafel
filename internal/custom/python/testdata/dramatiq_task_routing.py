"""Golden fixture for the dramatiq task_routing extractor (issue #3193).

Exercises queue->actor routing via:
  - @dramatiq.actor(queue_name="...") decorator routing
  - actor.send_with_options(queue_name="...") explicit dispatch override
"""
import dramatiq
from dramatiq.brokers.rabbitmq import RabbitmqBroker

rabbitmq_broker = RabbitmqBroker(url="amqp://localhost")
dramatiq.set_broker(rabbitmq_broker)


@dramatiq.actor(queue_name="emails", max_retries=3)
def send_email(recipient):
    pass


@dramatiq.actor(queue_name="reports")
def generate_report(report_id):
    pass


@dramatiq.actor
def default_queue_task():
    # No queue_name -> routed to the implicit "default" queue, not a routing marker.
    pass


def dispatch():
    # Producer using the actor's declared queue.
    send_email.send("user@example.com")
    # Explicit per-message queue override.
    generate_report.send_with_options(args=(42,), queue_name="reports_priority")
