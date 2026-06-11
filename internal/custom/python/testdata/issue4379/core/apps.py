from django.apps import AppConfig


class CoreConfig(AppConfig):
    default_auto_field = "django.db.models.BigAutoField"
    name = "core"

    def ready(self):
        # Imperative signal wiring lives here in real apps; covered by a
        # follow-up ticket for the `post_save.connect(...)` form.
        import core.signals  # noqa: F401
