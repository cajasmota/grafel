# Byte-derived trim of core/views/schedule_viewset.py from the upvate_core
# legacy Django backend, kept to the declarations the DRF route synthesizer
# reads: the ScheduleViewset class header and the @action(url_path="import")
# method whose route the test file exercises. Verbatim copies of the real
# decorators/signatures; method bodies elided.
from rest_framework import viewsets
from rest_framework.decorators import action
from rest_framework.pagination import LimitOffsetPagination


class ScheduleViewset(viewsets.ModelViewSet):

    serializer_class = DeviceSerializer
    pagination_class = LimitOffsetPagination
    http_method_names = ['get', 'post', 'put', 'delete']

    def retrieve(self, request, pk=None, **kwargs):
        return None

    @action(detail=False, methods=["post"], url_path="import", url_name="import")
    def import_csv(self, request):
        """Import a MASS scheduled inspection CSV."""
        return None
