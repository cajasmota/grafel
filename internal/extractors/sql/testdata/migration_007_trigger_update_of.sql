-- Fixture for Issue #1708: PostgreSQL UPDATE OF column trigger form.
-- Mirrors polyglot-platform/services/orders/migrations/002_views_procs_triggers.sql
-- which uses `AFTER UPDATE OF status ON orders` — the original triggerRE
-- failed to match the column-list form.

CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    status TEXT
);

CREATE TABLE order_status_audit (
    id BIGSERIAL PRIMARY KEY,
    order_id TEXT NOT NULL,
    old_status TEXT,
    new_status TEXT
);

CREATE OR REPLACE FUNCTION log_order_status_change()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO order_status_audit(order_id, old_status, new_status)
    VALUES (OLD.id, OLD.status, NEW.status);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- The column-list trigger form (`UPDATE OF status`) is what broke #1708.
CREATE TRIGGER trg_order_status_change
    AFTER UPDATE OF status ON orders
    FOR EACH ROW
    EXECUTE FUNCTION log_order_status_change();

-- Multi-column list form, also covered.
CREATE TRIGGER trg_orders_multi_col
    AFTER UPDATE OF status, total_cents ON orders
    FOR EACH ROW
    EXECUTE FUNCTION log_order_status_change();
