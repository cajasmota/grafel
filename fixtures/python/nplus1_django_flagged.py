# fixture: nplus1_django_flagged.py
# Synthetic fixture for N+1 query detector.
# Expected: get_user_posts is flagged as N+1 (User.objects.get inside a for-loop).
# Expected: optimized_view is NOT flagged (uses prefetch_related before the loop).

from django.contrib.auth.models import User
from myapp.models import Post


def get_user_posts(user_ids):
    """Classic N+1: queries User inside a for-loop. Should be flagged."""
    results = []
    for uid in user_ids:  # archigraph entity: subtype=for_loop
        user = User.objects.get(id=uid)  # ORM query inside loop — N+1
        results.append(user.username)
    return results


def batch_get_posts(post_ids):
    """Another N+1: Post.objects.filter inside a while-loop."""
    out = []
    i = 0
    while i < len(post_ids):  # archigraph entity: subtype=while_loop
        post = Post.objects.filter(id=post_ids[i]).first()  # N+1
        out.append(post.title if post else None)
        i += 1
    return out


def list_comprehension_nplus1(ids):
    """N+1 via list comprehension — semantically equivalent to a for-loop."""
    # archigraph entity: subtype=list_comprehension
    return [User.objects.get(id=i).email for i in ids]  # N+1


def optimized_view(user_ids):
    """Correct: pre-fetches all users in one query before the loop."""
    users = User.objects.filter(id__in=user_ids).prefetch_related("posts")
    return [u.username for u in users]


def select_related_is_safe(post_ids):
    """Correct: select_related loads author in a single JOIN — not N+1."""
    posts = Post.objects.filter(id__in=post_ids).select_related("author")
    return [(p.title, p.author.username) for p in posts]
