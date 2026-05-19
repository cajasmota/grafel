"""Class-based and function-based views for the users app."""
from django.http import JsonResponse
from django.views import View

from users.models import User


class UserListView(View):
    """List active users."""

    def get(self, request):
        qs = User.active.all()
        data = [{"id": u.id, "email": u.email} for u in qs]
        return JsonResponse({"users": data})


class UserDetailView(View):
    """Return a single user by pk."""

    def get(self, request, pk):
        user = User.objects.get(pk=pk)
        return JsonResponse({"id": user.id, "label": user.full_label()})


def health_check(request):
    """Liveness probe."""
    return JsonResponse({"status": "ok"})
