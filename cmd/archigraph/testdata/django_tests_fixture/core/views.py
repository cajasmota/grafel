from rest_framework import viewsets
from rest_framework.decorators import action
from rest_framework.response import Response

from core.models import Schedule
from core.serializers import ScheduleSerializer


class ScheduleViewSet(viewsets.ModelViewSet):
    queryset = Schedule.objects.all()
    serializer_class = ScheduleSerializer

    @action(detail=False, methods=['post'])
    def import_csv(self, request):
        return Response({'status': 'ok'})
