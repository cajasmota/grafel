-- Issue #4295 negative fixture: a query-only file (no DDL) must not emit
-- any table model entity.
SELECT u.id, u.email, o.name
FROM users u
JOIN orgs o ON o.id = u.org_id
WHERE u.status = 'active';
