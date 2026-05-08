-- Initial schema migration.
CREATE TABLE accounts (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE sessions (
    id SERIAL PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    token VARCHAR(64) NOT NULL,
    expires_at TIMESTAMP NOT NULL
);

CREATE TABLE audit_log (
    id BIGSERIAL PRIMARY KEY,
    account_id INTEGER NOT NULL,
    session_id INTEGER,
    event_kind VARCHAR(64) NOT NULL,
    payload JSONB,
    CONSTRAINT fk_audit_account FOREIGN KEY (account_id) REFERENCES accounts(id),
    CONSTRAINT fk_audit_session FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE INDEX idx_sessions_account ON sessions (account_id);
CREATE UNIQUE INDEX idx_audit_event ON audit_log (event_kind);
