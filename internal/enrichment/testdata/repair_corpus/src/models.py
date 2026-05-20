# repair_corpus/src/models.py
# Two classes named with a "save" method — this is why the resolver
# sees multiple candidates and emits a bug-resolver disposition for
# any bare "save" reference.

class ProposalService:
    """Service layer for proposals."""

    def save(self, data):
        # The correct binding target for ProposalViewSet.create's self.service.save.
        return {"id": 1, **data}

    def get_all(self):
        return []


class DraftService:
    """Service layer for drafts — also has a save method."""

    def save(self, draft):
        return {"draft_id": 99, **draft}
