"""Suite helpers. Declares NO tests.

Every test-shaped line below sits in a position that `(?m)` newly reaches — a
line that is not the file's first — so only the LINE ANCHOR keeps it out. A
decoy file that also held a genuine test could not distinguish "the decoy was
rejected" from "the real one was accepted", so there is none here.
"""

import pytest

# def test_commented_out(client):
# class TestCommentedOut:

USAGE = """
def test_from_a_docstring(client):
class TestFromADocstring:
"""


def build_client():
    def test_nested_helper(inner):
        return inner

    return test_nested_helper
