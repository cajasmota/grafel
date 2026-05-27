from django.test import TestCase


class ScheduleTests(TestCase):
    """Test schedule-related functionality.

    This file has NO test_ prefix — it lives in core/tests/schedule.py
    (idiomatic Django layout). Before #2604, the testmap extractor ignored
    files in tests/ directories whose basenames didn't start with test_.
    """

    def test_list(self):
        resp = self.client.get('/api/v1/schedule/')
        self.assertEqual(resp.status_code, 200)

    def test_import_csv(self):
        resp = self.client.post('/api/v1/schedule/import', {}, content_type='application/json')
        self.assertEqual(resp.status_code, 200)
