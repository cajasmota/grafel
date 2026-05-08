-- Migration that splits FK declarations into ALTER TABLE statements.
-- Covers the four ALTER TABLE ADD FOREIGN KEY patterns supported by the
-- SQL extractor.

CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL
);

CREATE TABLE roles (
    id SERIAL PRIMARY KEY,
    code VARCHAR(64) NOT NULL,
    scope VARCHAR(64) NOT NULL
);

CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    total NUMERIC(10, 2) NOT NULL
);

CREATE TABLE invoices (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    amount NUMERIC(10, 2) NOT NULL
);

CREATE TABLE memberships (
    id SERIAL PRIMARY KEY,
    role_code VARCHAR(64) NOT NULL,
    role_scope VARCHAR(64) NOT NULL
);

CREATE TABLE shipments (
    id SERIAL PRIMARY KEY,
    customer_id INTEGER NOT NULL
);

-- Pattern 1: ALTER TABLE ADD CONSTRAINT FOREIGN KEY (named, no actions)
ALTER TABLE orders ADD CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES users(id);

-- Pattern 2: ALTER TABLE ADD CONSTRAINT FOREIGN KEY ... ON DELETE CASCADE
ALTER TABLE invoices ADD CONSTRAINT fk_invoice_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

-- Pattern 3: Multi-column FK
ALTER TABLE memberships ADD CONSTRAINT fk_membership_role FOREIGN KEY (role_code, role_scope) REFERENCES roles(code, scope);

-- Pattern 4: Postgres ALTER TABLE ADD FOREIGN KEY (no CONSTRAINT name)
ALTER TABLE shipments ADD FOREIGN KEY (customer_id) REFERENCES users(id);
