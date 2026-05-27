# Fixture for issue #2670 — verify the production extract pipeline emits
# DISCRIMINATES_ON edges for discriminator-pattern comparisons in real files.


def process_checklist(checklist_type, role, status):
    is_cat5 = checklist_type == 2
    is_admin = role == "admin"
    is_active = status == "active"
    return is_cat5 and is_admin and is_active
