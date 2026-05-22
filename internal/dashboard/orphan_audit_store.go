package dashboard

// orphan_audit_store.go — persistence for the per-group orphan audit (#1574).
//
// The orphan audit is expensive (it re-loads every repo's graph from disk), so
// it must NOT run on every page load. Instead:
//
//	POST /api/quality/orphans/{group}  runs the audit + writes the result here.
//	GET  /api/quality/orphans/{group}  reads the last result back (or never-run).
//
// Results are stored one JSON file per group under
// ~/.archigraph/orphan-audits/<group>.json. This keeps the "Last audited"
// timestamp and the real per-kind / orphan numbers stable across reloads, so
// the client can distinguish a real measurement from an un-run default.

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// orphanAuditDir returns the directory holding per-group orphan-audit results.
func orphanAuditDir(root string) string {
	return filepath.Join(root, "orphan-audits")
}

// orphanAuditPath returns the JSON file path for a group's persisted audit.
// The group name is base-sanitised so a malicious "../" cannot escape the dir.
func orphanAuditPath(root, group string) string {
	safe := filepath.Base(filepath.Clean("/" + group))
	return filepath.Join(orphanAuditDir(root), safe+".json")
}

// saveOrphanAudit writes the audit reply to disk for later GETs.
func saveOrphanAudit(root, group string, reply OrphanAuditReply) error {
	if root == "" {
		return nil
	}
	dir := orphanAuditDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(reply, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(orphanAuditPath(root, group), data, 0o644)
}

// loadOrphanAudit reads a previously-persisted audit reply. The second return
// is false when no audit has ever been persisted for the group.
func loadOrphanAudit(root, group string) (OrphanAuditReply, bool) {
	var reply OrphanAuditReply
	if root == "" {
		return reply, false
	}
	data, err := os.ReadFile(orphanAuditPath(root, group))
	if err != nil {
		return reply, false
	}
	if err := json.Unmarshal(data, &reply); err != nil {
		return reply, false
	}
	// A persisted file with HasRun=false should never exist, but guard anyway.
	if !reply.HasRun {
		return reply, false
	}
	return reply, true
}
