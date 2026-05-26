# Source: https://github.com/celery/celery/tree/main/examples (synthetic based on real Celery task patterns) | License: BSD-3-Clause

from __future__ import annotations

import logging
from typing import Any

from celery import Celery, Task, group, chain
from celery.utils.log import get_task_logger
from kombu import Queue, Exchange

logger = get_task_logger(__name__)

# ============================================================
# App configuration
# ============================================================
app = Celery("myapp")
app.config_from_object("myapp.celeryconfig")

app.conf.task_queues = (
    Queue("default", Exchange("default"), routing_key="default"),
    Queue("emails", Exchange("emails"), routing_key="emails"),
    Queue("heavy", Exchange("heavy"), routing_key="heavy"),
)
app.conf.task_default_queue = "default"
app.conf.task_default_exchange = "default"
app.conf.task_default_routing_key = "default"

app.conf.task_routes = {
    "myapp.tasks.send_*": {"queue": "emails"},
    "myapp.tasks.index_*": {"queue": "heavy"},
}

# ============================================================
# Base task with retry logic
# ============================================================
class BaseTask(Task):
    abstract = True
    max_retries = 3
    default_retry_delay = 60  # seconds

    def on_failure(self, exc, task_id, args, kwargs, einfo):
        logger.error(
            "Task %s[%s] failed: %s",
            self.name, task_id, exc,
            exc_info=einfo,
        )
        super().on_failure(exc, task_id, args, kwargs, einfo)

    def on_retry(self, exc, task_id, args, kwargs, einfo):
        logger.warning(
            "Task %s[%s] retrying (attempt %d): %s",
            self.name, task_id, self.request.retries + 1, exc,
        )

    def on_success(self, retval, task_id, args, kwargs):
        logger.info("Task %s[%s] completed successfully", self.name, task_id)


# ============================================================
# Email tasks
# ============================================================
@app.task(
    bind=True,
    base=BaseTask,
    name="myapp.tasks.send_welcome_email",
    queue="emails",
)
def send_welcome_email(self, user_id: int) -> dict[str, Any]:
    from myapp.models import User
    from myapp.email import send_email

    try:
        user = User.objects.get(id=user_id)
        send_email(
            to=user.email,
            subject="Welcome to MyApp!",
            template="emails/welcome.html",
            context={"user": user},
        )
        return {"status": "sent", "email": user.email}
    except User.DoesNotExist:
        logger.error("User %d not found, skipping welcome email", user_id)
        return {"status": "skipped", "reason": "user_not_found"}
    except Exception as exc:
        raise self.retry(exc=exc, countdown=2 ** self.request.retries * 30)


@app.task(
    bind=True,
    base=BaseTask,
    name="myapp.tasks.send_notification_email",
    queue="emails",
)
def send_notification_email(self, user_id: int, subject: str, body: str) -> dict[str, Any]:
    from myapp.models import User
    from myapp.email import send_email

    try:
        user = User.objects.get(id=user_id)
        send_email(to=user.email, subject=subject, body=body)
        return {"status": "sent"}
    except Exception as exc:
        raise self.retry(exc=exc)


# ============================================================
# Indexing tasks
# ============================================================
@app.task(
    bind=True,
    base=BaseTask,
    name="myapp.tasks.index_post",
    queue="heavy",
    soft_time_limit=120,
    time_limit=150,
)
def index_post(self, post_id: int) -> dict[str, Any]:
    from myapp.models import Post
    from myapp.search import search_index

    try:
        post = Post.objects.select_related("author", "category").prefetch_related("tags").get(
            id=post_id
        )
        result = search_index.index_document(
            index="posts",
            document_id=str(post.id),
            body={
                "title": post.title,
                "body": post.body,
                "author": post.author.name,
                "tags": [t.name for t in post.tags.all()],
                "published_at": post.published_at.isoformat() if post.published_at else None,
            },
        )
        return {"status": "indexed", "post_id": post_id, "result": result}
    except Exception as exc:
        raise self.retry(exc=exc, countdown=60)


@app.task(name="myapp.tasks.reindex_all_posts", queue="heavy")
def reindex_all_posts() -> dict[str, Any]:
    """Trigger indexing for all published posts via a Celery group."""
    from myapp.models import Post

    post_ids = list(
        Post.objects.filter(status="published").values_list("id", flat=True)
    )
    logger.info("Reindexing %d posts", len(post_ids))

    job = group(index_post.s(post_id) for post_id in post_ids)
    result = job.apply_async()
    return {"status": "dispatched", "count": len(post_ids), "group_id": result.id}


# ============================================================
# Periodic tasks (beat)
# ============================================================
app.conf.beat_schedule = {
    "cleanup-expired-sessions": {
        "task": "myapp.tasks.cleanup_expired_sessions",
        "schedule": 3600.0,  # every hour
    },
    "reindex-all-posts": {
        "task": "myapp.tasks.reindex_all_posts",
        "schedule": 86400.0,  # daily
    },
}


@app.task(name="myapp.tasks.cleanup_expired_sessions")
def cleanup_expired_sessions() -> dict[str, Any]:
    from django.contrib.sessions.backends.db import SessionStore

    deleted, _ = SessionStore.clear_expired()
    logger.info("Cleaned up %d expired sessions", deleted)
    return {"deleted": deleted}
