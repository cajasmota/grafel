def send_email(to: str, subject: str, body: str) -> None:
    """RQ consumer: send an email. Called via queue.enqueue()."""
    pass


def generate_report(report_id: int) -> None:
    """RQ consumer: generate a report asynchronously."""
    pass
