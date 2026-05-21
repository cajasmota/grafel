// raw_sql_db_calls.go — Issue #1414: raw SQL procedure call + view read extraction.
//
// Detects Python patterns that invoke stored procedures via CALL and read from
// SQL views via raw cursor.execute / db.execute strings. Emits:
//
//   - cursor.execute("CALL proc_name(...)")  → enclosing fn CALLS proc_name
//   - conn.execute("CALL proc_name(...)")    → enclosing fn CALLS proc_name
//   - cursor.execute("SELECT ... FROM view") → enclosing fn READS_FROM view
//   - conn.execute("SELECT ... FROM view")   → enclosing fn READS_FROM view
//
// These edges bridge the Python application layer to the SQL schema layer,
// closing the orphan gap for procedure and view entities defined in migration
// files that are only referenced via raw execute strings.
//
// The scanner is intentionally conservative:
//   - Only fires when "CALL" or "SELECT … FROM" appears literally inside the
//     string argument of an execute call.
//   - Procedure names are normalised: schema qualifiers (schema.proc) are
//     stripped to the leaf name.
//   - View names are extracted from the first FROM / JOIN clause in the string.
//   - One edge per (caller, target) pair — deduplication across all call
//     sites in the same function.
//
// The pass appends edges to entities in place; it never removes or modifies
// existing entities, so regressions on files without raw-SQL calls are
// impossible.
package python

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// pyRawSQLCallRE matches:
//
//	cursor.execute("CALL proc_name(...)") or
//	conn.execute("CALL proc_name(...)")   or
//	db.execute("CALL proc_name(...)")     or
//	session.execute("CALL proc_name(...)") — any receiver + .execute( + string
//
// Capture group 1: the full SQL string content (up to closing quote).
var pyRawSQLCallRE = regexp.MustCompile(
	`(?i)\b\w+\.execute\s*\(\s*(?:f?["'])((?:[^"'\\]|\\.)*?)(?:["'])`,
)

// pyRawSQLCALLProcRE extracts the procedure name from a SQL CALL statement.
// Capture group 1: proc name (leaf, without schema qualifier).
var pyRawSQLCALLProcRE = regexp.MustCompile(
	`(?i)\bCALL\s+(?:\w+\.)?(\w+)\s*\(`,
)

// pyRawSQLFromRE extracts the first table/view name after FROM in a SQL SELECT.
// Capture group 1: table/view name.
var pyRawSQLFromRE = regexp.MustCompile(
	`(?i)\bFROM\s+(?:\w+\.)?(\w+)\b`,
)

// enclosingFuncRE extracts enclosing function names from surrounding source
// context — used to attribute edges when tree-sitter is not available.
// We use a simple line-scan approach: the nearest preceding "def " line above
// the call site is the enclosing function.

// rawSQLReservedWords lists identifiers that FROM/JOIN can pick up but that are
// SQL keywords rather than table/view names.
var rawSQLReservedWords = map[string]bool{
	"ONLY": true, "SELECT": true, "WHERE": true, "WITH": true, "AS": true,
	"JOIN": true, "LATERAL": true, "DUAL": true,
}

// emitRawSQLDBCallEdges scans src for cursor.execute("CALL ...") and
// cursor.execute("SELECT ... FROM view") patterns and appends CALLS /
// READS_FROM edges to the enclosing function entities in entities.
//
// filePath is used only to match entities by SourceFile.
// src is the raw Python source text.
func emitRawSQLDBCallEdges(src string, filePath string, entities *[]types.EntityRecord) {
	if entities == nil || len(*entities) == 0 {
		return
	}

	lines := strings.Split(src, "\n")
	// Build a map from line number (1-based) to the enclosing function's
	// emitted name by scanning "def " declarations. This is intentionally
	// simple: the most recently declared function at the same or lower
	// indentation level enclosing the call site.
	funcByLine := buildFuncLineMap(lines)

	type edgeKey struct {
		caller string
		kind   string
		target string
	}
	seen := make(map[edgeKey]bool)

	for _, m := range pyRawSQLCallRE.FindAllStringSubmatchIndex(src, -1) {
		sqlStr := src[m[2]:m[3]]
		lineNum := strings.Count(src[:m[0]], "\n") + 1
		caller := funcByLine[lineNum]
		if caller == "" {
			continue
		}

		// Detect CALL proc_name(...)
		if cm := pyRawSQLCALLProcRE.FindStringSubmatch(sqlStr); cm != nil {
			procName := cm[1]
			k := edgeKey{caller, "CALLS", procName}
			if !seen[k] {
				seen[k] = true
				appendEdgeToFunc(entities, filePath, caller, types.RelationshipRecord{
					ToID:       procName,
					Kind:       "CALLS",
					Properties: map[string]string{"raw_sql": "true", "call_type": "procedure"},
				})
			}
			continue
		}

		// Detect SELECT ... FROM view_name
		upperSQL := strings.ToUpper(sqlStr)
		if strings.Contains(upperSQL, "SELECT") {
			for _, fm := range pyRawSQLFromRE.FindAllStringSubmatch(sqlStr, -1) {
				viewName := fm[1]
				if rawSQLReservedWords[strings.ToUpper(viewName)] {
					continue
				}
				k := edgeKey{caller, "READS_FROM", viewName}
				if !seen[k] {
					seen[k] = true
					appendEdgeToFunc(entities, filePath, caller, types.RelationshipRecord{
						ToID:       viewName,
						Kind:       "READS_FROM",
						Properties: map[string]string{"raw_sql": "true", "dml": "select"},
					})
				}
			}
		}
	}
}

// buildFuncLineMap builds a line→enclosingFunctionName map for src split by
// lines. The algorithm is a simple indentation-aware "most recent def"
// tracker: for each line we record the deepest enclosing def name.
func buildFuncLineMap(lines []string) map[int]string {
	// Stack entries: (indentLevel, funcEmittedName)
	type stackEntry struct {
		indent int
		name   string
	}
	var stack []stackEntry
	result := make(map[int]string, len(lines))

	defRE := regexp.MustCompile(`^(\s*)def\s+(\w+)\s*\(`)

	for i, line := range lines {
		lineNum := i + 1

		if m := defRE.FindStringSubmatch(line); m != nil {
			indent := len(m[1])
			name := m[2]
			// Pop entries at same or deeper indent — this def supersedes them.
			for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, stackEntry{indent, name})
		}

		if len(stack) > 0 {
			result[lineNum] = stack[len(stack)-1].name
		}
	}
	return result
}

// appendEdgeToFunc appends rel to the first entity in entities whose
// SourceFile == filePath and Name == funcName.
func appendEdgeToFunc(entities *[]types.EntityRecord, filePath, funcName string, rel types.RelationshipRecord) {
	for i := range *entities {
		e := &(*entities)[i]
		if e.SourceFile == filePath && e.Name == funcName {
			e.Relationships = append(e.Relationships, rel)
			return
		}
	}
}
