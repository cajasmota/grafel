-- Negative-case fixture: statements that look table-like but must NOT
-- produce SCOPE.Datastore/table entities.

-- Postgres CREATE TYPE ... AS ENUM should NOT be parsed as a table.
CREATE TYPE order_status AS ENUM ('new', 'paid', 'shipped', 'cancelled');

-- Postgres CREATE TYPE ... AS (composite) should NOT be parsed as a table.
CREATE TYPE address AS (
    street VARCHAR(255),
    city VARCHAR(128),
    zipcode VARCHAR(16)
);

-- CREATE FUNCTION should NOT be parsed as a table; the body's "RETURN ..."
-- and column-list lookalikes must not leak into table extraction.
CREATE OR REPLACE FUNCTION compute_total(qty INTEGER, unit_price NUMERIC)
RETURNS NUMERIC AS $$
BEGIN
    RETURN qty * unit_price;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION upper_email(email VARCHAR(255)) RETURNS VARCHAR(255) AS $$
BEGIN
    RETURN UPPER(email);
END;
$$ LANGUAGE plpgsql;

-- A real CREATE TABLE alongside the negatives, so we can assert that the
-- extractor still finds it (i.e. negative-guard didn't over-suppress).
CREATE TABLE customers (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    status order_status NOT NULL DEFAULT 'new'
);
