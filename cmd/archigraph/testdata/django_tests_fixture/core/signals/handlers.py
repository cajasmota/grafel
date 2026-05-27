from django.db.models.signals import post_save
from django.dispatch import receiver

from core.models import Schedule


@receiver(post_save, sender=Schedule)
def replicate_schedule(sender, instance, created, **kwargs):
    """Replicate schedule changes to the data lake."""
    pass
