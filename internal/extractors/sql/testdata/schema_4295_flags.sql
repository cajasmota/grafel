-- Issue #4295: two tables with a FK, exercising column metadata flags.
CREATE TABLE orgs (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE
);

CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email TEXT NOT NULL,
    org_id INT REFERENCES orgs(id),
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMP DEFAULT now()
);
