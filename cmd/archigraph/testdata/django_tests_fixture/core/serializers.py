from rest_framework import serializers
from core.models import Schedule


class ScheduleSerializer(serializers.ModelSerializer):
    class Meta:
        model = Schedule
        fields = ['id', 'name', 'created_at']
