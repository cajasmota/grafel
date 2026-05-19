"""URLconf for the users app."""
from django.urls import path

from users import views


urlpatterns = [
    path("", views.UserListView.as_view(), name="user-list"),
    path("<int:pk>/", views.UserDetailView.as_view(), name="user-detail"),
    path("health/", views.health_check, name="user-health"),
]
