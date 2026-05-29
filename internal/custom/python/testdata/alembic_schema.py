"""create users and orders

Revision ID: a1b2c3d4e5f6
Revises: 0f1e2d3c4b5a
Create Date: 2026-05-29 12:00:00.000000
"""
from alembic import op
import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

revision = "a1b2c3d4e5f6"
down_revision = "0f1e2d3c4b5a"
branch_labels = None
depends_on = None


def upgrade():
    op.create_table(
        "users",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("username", sa.String(length=100), nullable=False),
        sa.Column("email", sa.String(length=255), nullable=False),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now()),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("email"),
    )
    op.create_table(
        "orders",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("user_id", sa.Integer(), sa.ForeignKey("users.id")),
        sa.Column("total", sa.Numeric(10, 2), nullable=False),
        sa.Column("metadata", postgresql.JSONB(), nullable=True),
        sa.PrimaryKeyConstraint("id"),
    )

    # Mutate an existing table.
    op.add_column("users", sa.Column("is_active", sa.Boolean(), server_default="true"))

    op.create_index("ix_users_email", "users", ["email"], unique=True)
    op.create_index(op.f("ix_orders_user_id"), "orders", ["user_id"])


def downgrade():
    op.drop_index("ix_orders_user_id", table_name="orders")
    op.drop_index("ix_users_email", table_name="users")
    op.drop_column("users", "is_active")
    op.drop_table("orders")
    op.drop_table("users")
