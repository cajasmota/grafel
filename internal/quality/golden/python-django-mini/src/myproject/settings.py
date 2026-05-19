"""Minimal Django settings for the golden fixture."""
INSTALLED_APPS = [
    "django.contrib.admin",
    "django.contrib.auth",
    "django.contrib.contenttypes",
    "users",
]

ROOT_URLCONF = "myproject.urls"
SECRET_KEY = "fixture-not-a-real-secret"
DATABASES = {
    "default": {
        "ENGINE": "django.db.backends.sqlite3",
        "NAME": "fixture.db",
    }
}
