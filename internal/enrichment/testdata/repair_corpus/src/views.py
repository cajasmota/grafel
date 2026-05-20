# repair_corpus/src/views.py
# KNOWN BUG: DRF ViewSet — self.service.save() is a bug-resolver candidate.
# The resolver sees multiple "save" methods across models but cannot pick the
# right one without semantic context. The reference repair binds it to
# ProposalService.save (id: 2222222222222222).

from rest_framework.viewsets import ViewSet
from rest_framework.response import Response


class ProposalViewSet(ViewSet):

    def create(self, request):
        data = request.data
        result = self.service.save(data)  # BUG: bare "save" — ambiguous
        return Response(result)

    def list(self, request):
        return Response(self.service.get_all())  # BUG: bare "get_all" — extractor stub
