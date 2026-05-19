"""User model + a custom manager for the golden fixture."""
from django.db import models


class ActiveUserManager(models.Manager):
    """Custom manager that filters to active users only."""

    def get_queryset(self):
        return super().get_queryset().filter(is_active=True)

    def by_email(self, email):
        return self.get_queryset().filter(email=email)


class User(models.Model):
    email = models.EmailField(unique=True)
    name = models.CharField(max_length=200)
    is_active = models.BooleanField(default=True)

    objects = models.Manager()
    active = ActiveUserManager()

    class Meta:
        ordering = ["email"]

    def full_label(self):
        return f"{self.name} <{self.email}>"
