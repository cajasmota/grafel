-- Fixture for Issue #1414: VIEW / PROCEDURE / TRIGGER extraction.
-- Mirrors services/orders/migrations/002_views_procs_triggers.sql
-- from the ShipFast D23 corpus (MANIFEST §12).

CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    status VARCHAR(50) DEFAULT 'pending',
    total_amount NUMERIC(12,2) DEFAULT 0
);

CREATE TABLE daily_revenue (
    day DATE PRIMARY KEY,
    revenue NUMERIC(14,2) DEFAULT 0
);

CREATE TABLE order_status_audit (
    id BIGSERIAL PRIMARY KEY,
    order_id INTEGER NOT NULL,
    old_status VARCHAR(50),
    new_status VARCHAR(50),
    changed_at TIMESTAMPTZ DEFAULT NOW()
);

-- VIEW: order_summary → reads from orders
CREATE OR REPLACE VIEW order_summary AS
SELECT
    o.id,
    o.user_id,
    o.status,
    o.total_amount
FROM orders o
WHERE o.status != 'cancelled';

-- PROCEDURE: mark_order_paid(order_id, amount) — writes orders + daily_revenue
CREATE OR REPLACE PROCEDURE mark_order_paid(p_order_id TEXT, p_amount INT)
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE orders SET status = 'paid', total_amount = p_amount WHERE id = p_order_id::INT;
    INSERT INTO daily_revenue (day, revenue)
        VALUES (CURRENT_DATE, p_amount)
        ON CONFLICT (day) DO UPDATE SET revenue = daily_revenue.revenue + p_amount;
END;
$$;

-- TRIGGER FUNCTION: log_order_status_change() RETURNS TRIGGER
-- Writes to order_status_audit; reads from orders (implicit via NEW/OLD).
CREATE OR REPLACE FUNCTION log_order_status_change()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status IS DISTINCT FROM OLD.status THEN
        INSERT INTO order_status_audit (order_id, old_status, new_status)
        VALUES (NEW.id, OLD.status, NEW.status);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- TRIGGER: trg_order_status_change AFTER UPDATE ON orders
CREATE TRIGGER trg_order_status_change
AFTER UPDATE ON orders
FOR EACH ROW
EXECUTE FUNCTION log_order_status_change();
