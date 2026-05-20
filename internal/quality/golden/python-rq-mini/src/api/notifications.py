from rq import Queue
from redis import Redis
from workers.email import send_email, generate_report

redis_conn = Redis()
notification_queue = Queue("notifications", connection=redis_conn)
report_queue = Queue("reports", connection=redis_conn)


def notify_user(user_id: int, message: str) -> None:
    """Producer: enqueue an email notification."""
    notification_queue.enqueue(send_email, "user@example.com", "Notification", message)


def request_report(report_id: int) -> None:
    """Producer: enqueue a report generation job."""
    report_queue.enqueue_call(func="workers.email.generate_report", args=[report_id])
