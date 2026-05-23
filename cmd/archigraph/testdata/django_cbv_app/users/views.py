from django.views import View
from django.http import JsonResponse
from .models import User

class UserListView(View):
    def get(self, request):
        return JsonResponse({"users": []})

class UserDetailView(View):
    def get(self, request, pk):
        return JsonResponse({"id": pk})
