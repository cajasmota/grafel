from django.test import TestCase


class APIViewTests(TestCase):
    """API view tests — lives in api/tests/views.py (no test_ prefix)."""

    def test_schedule_list(self):
        resp = self.client.get('/api/v1/schedule/')
        self.assertEqual(resp.status_code, 200)
