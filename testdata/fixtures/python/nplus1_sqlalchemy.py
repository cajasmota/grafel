# fixture: nplus1_sqlalchemy.py
# Synthetic fixture for N+1 query detector — SQLAlchemy ORM.
# Expected: load_order_items is flagged (session.query inside for-loop).
# Expected: efficient_load is NOT flagged (uses joinedload / selectinload).

from sqlalchemy.orm import Session, joinedload, selectinload
from myapp.models import Order, Item, User


def load_order_items(session: Session, order_ids: list[int]):
    """N+1: queries Item for each order inside a for-loop."""
    result = []
    for oid in order_ids:  # for_loop — N+1 context
        items = session.query(Item).filter(Item.order_id == oid).all()  # N+1
        result.extend(items)
    return result


def load_users_naive(session: Session, user_ids: list[int]):
    """N+1: session.query per user id."""
    users = []
    for uid in user_ids:
        u = session.query(User).filter(User.id == uid).one_or_none()  # N+1
        if u:
            users.append(u)
    return users


def efficient_load(session: Session, order_ids: list[int]):
    """Correct: eager-loads items in a single query using selectinload."""
    orders = (
        session.query(Order)
        .filter(Order.id.in_(order_ids))
        .options(selectinload(Order.items))  # safe — batch load
        .all()
    )
    return orders


def joined_load_is_safe(session: Session, user_ids: list[int]):
    """Correct: joinedload collapses to a single JOIN query."""
    return (
        session.query(User)
        .filter(User.id.in_(user_ids))
        .options(joinedload(User.orders))  # safe
        .all()
    )
