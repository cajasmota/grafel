-- Fixture for Issue #389: DML emission (READS_FROM / WRITES_TO).

CREATE TABLE accounts (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255)
);

CREATE TABLE sessions (
    id SERIAL PRIMARY KEY,
    account_id INTEGER
);

CREATE TABLE audit_log (
    id BIGSERIAL PRIMARY KEY,
    account_id INTEGER,
    event_kind VARCHAR(64)
);

-- View pulling from accounts and sessions: should emit
--   active_sessions READS_FROM accounts
--   active_sessions READS_FROM sessions
CREATE VIEW active_sessions AS
SELECT a.id, a.email, s.id AS session_id
FROM accounts a
JOIN sessions s ON s.account_id = a.id
WHERE s.account_id IS NOT NULL;

-- Function with SELECT + INSERT + UPDATE + DELETE in body: should emit
--   record_event READS_FROM accounts
--   record_event WRITES_TO audit_log
--   record_event WRITES_TO sessions   (UPDATE)
--   record_event WRITES_TO sessions   (DELETE — dedupe to one edge)
CREATE FUNCTION record_event(p_account_id INTEGER, p_kind VARCHAR) RETURNS VOID AS $$
BEGIN
    PERFORM id FROM accounts WHERE id = p_account_id;
    INSERT INTO audit_log (account_id, event_kind) VALUES (p_account_id, p_kind);
    UPDATE sessions SET account_id = p_account_id WHERE id = 1;
    DELETE FROM sessions WHERE account_id IS NULL;
END;
$$ LANGUAGE plpgsql;
