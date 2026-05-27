from rest_framework import viewsets
from rest_framework.decorators import action
from rest_framework.response import Response


class ThingViewSet(viewsets.ModelViewSet):
    """Demo ViewSet exercising both inherited CRUD and a custom @action."""

    queryset = []
    serializer_class = None

    def list(self, request, *args, **kwargs):
        # The endpoint GET /api/v1/things must attribute to THIS def line.
        return Response([])

    @action(detail=False, url_path="custom_action", methods=["post"])
    def custom(self, request, *args, **kwargs):
        # The endpoint POST /api/v1/things/custom_action must attribute to
        # THIS def line, not to routers.py.
        return Response({"ok": True})
