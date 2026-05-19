"""AppConfig for the users app."""
from django.apps import AppConfig


class UsersConfig(AppConfig):
    default_auto_field = "django.db.models.BigAutoField"
    name = "users"

    def ready(self):
        # Import signal handlers so they bind to the post_save dispatcher.
        from users import signals  # noqa: F401
