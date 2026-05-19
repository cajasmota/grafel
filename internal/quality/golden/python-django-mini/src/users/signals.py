"""Signal handlers for the users app."""
from django.db.models.signals import post_save
from django.dispatch import receiver

from users.models import User


@receiver(post_save, sender=User)
def user_post_save(sender, instance, created, **kwargs):
    """Log on user create/update."""
    if created:
        instance.full_label()
