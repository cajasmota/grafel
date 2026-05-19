"""Admin registration for the users app."""
from django.contrib import admin

from users.models import User


@admin.register(User)
class UserAdmin(admin.ModelAdmin):
    list_display = ("email", "name", "is_active")
    search_fields = ("email", "name")
