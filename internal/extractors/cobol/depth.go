// Phase-2 depth extraction for COBOL (#2838): copybook (COPY) resolution
// against on-disk .cpy files, embedded-SQL entity extraction (tables +
// cursors as SCOPE.DataAccess with ACCESSES_TABLE edges), EXEC CICS depth
// (program-transfer LINK/XCTL/START as CALLS, file/queue I/O surfaced through
// the effect sniffer), and the 01/05/10 data hierarchy with REDEFINES /
// OCCURS as structured-field metadata.
//
// This file builds on extractor.go (Phase 1, #2743). Phase 1 already emits the
// program / division / section / paragraph entities and PERFORM/CALL/COPY
// edges; the helpers here are invoked from extractCOBOL to deepen those into
// resolvable, drift-detectable entities.
package cobol

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Copybook (COPY) resolution — #2838 Phase 2 item 1
// ---------------------------------------------------------------------------

// copyDirectiveRe captures a full COPY directive including an optional
// REPLACING clause. Group 1: copybook name. The remainder (REPLACING ...) is
// parsed separately so the bare-name copyRe in extractor.go keeps working for
// the common case.
//
// Examples handled:
//
//	COPY EMPREC.
//	COPY EMPREC REPLACING ==:PFX:== BY ==WS==.
//	COPY EMPREC OF MYLIB.
var copyDirectiveRe = regexp.MustCompile(
	`(?i)\bCOPY\s+([A-Za-z0-9][A-Za-z0-9-]*)` +
		`(?:\s+(?:OF|IN)\s+[A-Za-z0-9][A-Za-z0-9-]*)?` +
		`(\s+REPLACING\b[^.]*)?`,
)

// replacingPairRe extracts ==pseudo-text== BY ==pseudo-text== pairs from a
// REPLACING clause (also supports bare-word operands).
var replacingPairRe = regexp.MustCompile(
	`(?i)(==[^=]*==|[A-Za-z0-9:#$@-]+)\s+BY\s+(==[^=]*==|[A-Za-z0-9:#$@-]+)`,
)

// copybookExtensions are the conventional on-disk suffixes a COPY name may
// resolve to, in priority order.
var copybookExtensions = []string{".cpy", ".CPY", ".cbl", ".CBL", ".cob", ".COB", ".cobol", ""}

// copyResolution is the outcome of resolving a COPY directive against disk.
type copyResolution struct {
	book      string // copybook name as written
	resolved  bool   // a matching file was found on disk
	path      string // repo-relative resolved path (when resolved)
	replacing string // normalized REPLACING clause text (when present)
}

// resolveCopybook tries to resolve a COPY name to an on-disk copybook file,
// searching the directory of the using program and common copybook
// sub-directories under repoRoot. Returns resolved=false when no candidate
// exists (the IMPORTS edge is still emitted, but as an unresolved reference).
func resolveCopybook(repoRoot, usingFile, book string) (string, bool) {
	if repoRoot == "" {
		return "", false
	}
	// Directories to probe, relative to repoRoot, most-specific first.
	dirs := []string{}
	if usingFile != "" {
		dirs = append(dirs, filepath.Dir(usingFile))
	}
	dirs = append(dirs,
		"", "copybook", "copybooks", "copylib", "cpy", "include", "copy",
	)
	// COBOL COPY names are case-insensitive; probe both as-written and the
	// upper-cased form (the IBM convention).
	names := []string{book, strings.ToUpper(book), strings.ToLower(book)}
	seen := map[string]bool{}
	for _, d := range dirs {
		for _, n := range names {
			for _, ext := range copybookExtensions {
				rel := filepath.Join(d, n+ext)
				if seen[rel] {
					continue
				}
				seen[rel] = true
				abs := filepath.Join(repoRoot, rel)
				if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
					return filepath.ToSlash(rel), true
				}
			}
		}
	}
	return "", false
}

// parseCopyDirective extracts the copybook name + a normalized REPLACING
// clause from a COPY line. Returns ok=false when the line is not a COPY.
func parseCopyDirective(code string) (book, replacing string, ok bool) {
	m := copyDirectiveRe.FindStringSubmatch(code)
	if m == nil {
		return "", "", false
	}
	book = m[1]
	if rep := strings.TrimSpace(m[2]); rep != "" {
		// Collapse whitespace and drop the leading REPLACING keyword for a
		// compact, comparable property value.
		rep = strings.Join(strings.Fields(rep), " ")
		rep = strings.TrimPrefix(rep, "REPLACING ")
		rep = strings.TrimPrefix(rep, "replacing ")
		replacing = rep
	}
	return book, replacing, true
}

// buildCopyImportEdge constructs the IMPORTS relationship for a resolved (or
// unresolved) COPY. When the copybook resolves on disk, ToID is the resolved
// file path so the edge binds to the copybook's file/component entity; the
// per-name placeholder entity emitted alongside keeps the bare-name resolution
// path working for unresolved books.
func buildCopyImportEdge(usingFile string, cr copyResolution, line int) types.RelationshipRecord {
	toID := cr.book
	if cr.resolved {
		toID = cr.path
	}
	props := map[string]string{
		"line":     strconv.Itoa(line),
		"copybook": cr.book,
		"resolved": strconv.FormatBool(cr.resolved),
	}
	if cr.resolved {
		props["copybook_path"] = cr.path
	}
	if cr.replacing != "" {
		props["replacing"] = cr.replacing
		// Record the REPLACING pairs in a structured form for drift analysis.
		if pairs := parseReplacingPairs(cr.replacing); pairs != "" {
			props["replacing_pairs"] = pairs
		}
	}
	return types.RelationshipRecord{
		FromID:     usingFile,
		ToID:       toID,
		Kind:       "IMPORTS",
		Properties: props,
	}
}

// parseReplacingPairs renders the REPLACING operand pairs as a compact
// "from=>to;from=>to" string.
func parseReplacingPairs(clause string) string {
	var b strings.Builder
	for _, m := range replacingPairRe.FindAllStringSubmatch(clause, -1) {
		if b.Len() > 0 {
			b.WriteByte(';')
		}
		b.WriteString(trimPseudo(m[1]))
		b.WriteString("=>")
		b.WriteString(trimPseudo(m[2]))
	}
	return b.String()
}

// trimPseudo strips the ==...== pseudo-text delimiters from a REPLACING
// operand, leaving the inner text trimmed.
func trimPseudo(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "==") && strings.HasSuffix(s, "==") && len(s) >= 4 {
		return strings.TrimSpace(s[2 : len(s)-2])
	}
	return s
}

// ---------------------------------------------------------------------------
// Embedded SQL (EXEC SQL ... END-EXEC) — #2838 Phase 2 item 2
// ---------------------------------------------------------------------------

// execBlock is one EXEC SQL / EXEC CICS block accumulated across lines.
type execBlock struct {
	kind      string // "SQL" or "CICS"
	startLine int
	text      string // joined block body (between EXEC <kind> and END-EXEC)
}

var (
	execStartRe = regexp.MustCompile(`(?i)\bEXEC\s+(SQL|CICS)\b`)
	execEndRe   = regexp.MustCompile(`(?i)\bEND-EXEC\b`)

	// SQL DML / cursor patterns. Table names follow FROM / INTO / UPDATE /
	// JOIN / DELETE FROM. Host-variable colons and schema-qualified names are
	// tolerated.
	sqlSelectFromRe = regexp.MustCompile(`(?is)\bFROM\s+([A-Za-z][A-Za-z0-9_$.]*)`)
	sqlInsertIntoRe = regexp.MustCompile(`(?is)\bINSERT\s+INTO\s+([A-Za-z][A-Za-z0-9_$.]*)`)
	sqlUpdateRe     = regexp.MustCompile(`(?is)\bUPDATE\s+([A-Za-z][A-Za-z0-9_$.]*)`)
	sqlDeleteFromRe = regexp.MustCompile(`(?is)\bDELETE\s+FROM\s+([A-Za-z][A-Za-z0-9_$.]*)`)
	sqlJoinRe       = regexp.MustCompile(`(?is)\bJOIN\s+([A-Za-z][A-Za-z0-9_$.]*)`)

	// DECLARE <name> CURSOR FOR ... — captures the cursor name.
	sqlDeclareCursorRe = regexp.MustCompile(`(?is)\bDECLARE\s+([A-Za-z][A-Za-z0-9_-]*)\s+CURSOR\b`)
	// OPEN / FETCH / CLOSE <cursor> — cursor traffic.
	sqlCursorOpRe = regexp.MustCompile(`(?is)\b(OPEN|FETCH|CLOSE)\s+([A-Za-z][A-Za-z0-9_-]*)`)
)

const (
	// kindDataAccess mirrors the cross/dbmap extractor so embedded-SQL access
	// entities resolve through the same ACCESSES_TABLE pipeline.
	kindDataAccess = "SCOPE.DataAccess"
	relAccessesTab = "ACCESSES_TABLE"
	ormEmbeddedSQL = "embedded-sql"
)

// cursorOpReserved are keywords that OPEN/FETCH/CLOSE may be followed by but
// which are not cursor names (so they are not turned into cursor references).
var cursorOpReserved = map[string]bool{
	"FOR": true, "INTO": true, "ALL": true, "CURSOR": true,
}

// sqlOpFor classifies an EXEC SQL block body into a primary DML operation.
func sqlOpFor(body string) string {
	up := strings.ToUpper(body)
	switch {
	case strings.Contains(up, "INSERT"):
		return "INSERT"
	case strings.Contains(up, "UPDATE"):
		return "UPDATE"
	case strings.Contains(up, "DELETE"):
		return "DELETE"
	case strings.Contains(up, "MERGE"):
		return "UPSERT"
	case strings.Contains(up, "SELECT"), strings.Contains(up, "FETCH"):
		return "SELECT"
	default:
		return "EXEC"
	}
}

// dataAccessRef builds a stable identity string for an embedded-SQL
// SCOPE.DataAccess entity, matching the shape cross/dbmap uses so the resolver
// binds the ACCESSES_TABLE edge toID to this entity.
func dataAccessRef(filePath, op, table string) string {
	return "scope:dataaccess:" + filepath.ToSlash(filePath) + "#" + ormEmbeddedSQL + ":" + op + ":" + table
}

// extractSQLEntities turns one EXEC SQL block into SCOPE.DataAccess entities:
// one per referenced table, plus a cursor entity for DECLARE CURSOR. The
// enclosing paragraph (fnRef) is the FromID of each ACCESSES_TABLE edge.
func extractSQLEntities(filePath, fnQName string, blk execBlock) []types.EntityRecord {
	var out []types.EntityRecord
	op := sqlOpFor(blk.text)

	// Collect referenced tables (dedup).
	tables := map[string]bool{}
	collect := func(re *regexp.Regexp) {
		for _, m := range re.FindAllStringSubmatch(blk.text, -1) {
			t := strings.TrimSuffix(m[1], ".")
			if t != "" {
				tables[t] = true
			}
		}
	}
	collect(sqlSelectFromRe)
	collect(sqlInsertIntoRe)
	collect(sqlUpdateRe)
	collect(sqlDeleteFromRe)
	collect(sqlJoinRe)

	fnRef := sqlFunctionRef(filePath, fnQName)
	for table := range tables {
		ref := dataAccessRef(filePath, op, table)
		rec := types.EntityRecord{
			Name:          op + " " + table,
			Kind:          kindDataAccess,
			QualifiedName: ref,
			SourceFile:    filePath,
			Language:      "cobol",
			Subtype:       ormEmbeddedSQL,
			StartLine:     blk.startLine,
			EndLine:       blk.startLine,
			Properties: map[string]string{
				"table":        table,
				"operation":    op,
				"orm":          ormEmbeddedSQL,
				"ref":          ref,
				"function_ref": fnQName,
				"provenance":   "INFERRED_FROM_EXEC_SQL",
			},
			QualityScore: 0.8,
		}
		rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
			FromID: fnRef,
			ToID:   ref,
			Kind:   relAccessesTab,
			Properties: map[string]string{
				"function_qname": fnQName,
				"orm":            ormEmbeddedSQL,
				"operation":      op,
				"table":          table,
			},
		})
		out = append(out, rec)
	}

	// DECLARE CURSOR — emit a cursor SCOPE.DataAccess entity.
	declared := map[string]bool{}
	for _, m := range sqlDeclareCursorRe.FindAllStringSubmatch(blk.text, -1) {
		cursor := m[1]
		declared[strings.ToUpper(cursor)] = true
		ref := cursorRef(filePath, cursor)
		out = append(out, types.EntityRecord{
			Name:          cursor,
			Kind:          kindDataAccess,
			QualifiedName: ref,
			SourceFile:    filePath,
			Language:      "cobol",
			Subtype:       "cursor",
			StartLine:     blk.startLine,
			EndLine:       blk.startLine,
			Properties: map[string]string{
				"cursor":     cursor,
				"operation":  "DECLARE_CURSOR",
				"orm":        ormEmbeddedSQL,
				"ref":        ref,
				"provenance": "INFERRED_FROM_EXEC_SQL",
			},
			QualityScore: 0.8,
		})
	}

	// OPEN / FETCH / CLOSE <cursor> — emit a REFERENCES edge from the
	// enclosing paragraph to the cursor entity so cursor traffic is part of
	// the data-access graph (deduped per cursor+op).
	seenCursorOp := map[string]bool{}
	for _, m := range sqlCursorOpRe.FindAllStringSubmatch(blk.text, -1) {
		verb := strings.ToUpper(m[1])
		cursor := m[2]
		if cursorOpReserved[strings.ToUpper(cursor)] {
			continue
		}
		// DECLARE itself is handled above; skip the DECLARE-line OPEN noise.
		key := verb + ":" + strings.ToUpper(cursor)
		if seenCursorOp[key] {
			continue
		}
		seenCursorOp[key] = true
		out = append(out, types.EntityRecord{
			Name:          verb + " " + cursor,
			Kind:          kindDataAccess,
			QualifiedName: cursorRef(filePath, cursor) + ":" + verb,
			SourceFile:    filePath,
			Language:      "cobol",
			Subtype:       "cursor",
			StartLine:     blk.startLine,
			EndLine:       blk.startLine,
			Properties: map[string]string{
				"cursor":       cursor,
				"operation":    verb,
				"orm":          ormEmbeddedSQL,
				"function_ref": fnQName,
				"provenance":   "INFERRED_FROM_EXEC_SQL",
			},
			Relationships: []types.RelationshipRecord{{
				FromID:     fnRef,
				ToID:       cursorRef(filePath, cursor),
				Kind:       "REFERENCES",
				Properties: map[string]string{"cursor": cursor, "operation": verb},
			}},
			QualityScore: 0.75,
		})
	}

	return out
}

// cursorRef builds a stable identity for a cursor SCOPE.DataAccess entity.
func cursorRef(filePath, cursor string) string {
	return "scope:dataaccess:" + filepath.ToSlash(filePath) + "#cursor:" + cursor
}

// sqlFunctionRef builds the Format A operation ref for the enclosing paragraph
// so the ACCESSES_TABLE edge resolves to the paragraph entity.
func sqlFunctionRef(filePath, fnQName string) string {
	if fnQName == "" {
		return "scope:operation:" + filepath.ToSlash(filePath) + "#_file_scope"
	}
	return extractor.BuildOperationStructuralRef("cobol", filePath, fnQName)
}

// ---------------------------------------------------------------------------
// EXEC CICS depth — #2838 Phase 2 item 3
// ---------------------------------------------------------------------------

// cicsXferRe matches CICS program-transfer verbs and captures the target
// program name from the PROGRAM('NAME') operand. LINK / XCTL transfer control
// to another program; START schedules a transaction.
var cicsXferRe = regexp.MustCompile(
	`(?is)\b(LINK|XCTL)\b[^.]*?\bPROGRAM\s*\(\s*'?([A-Za-z0-9$#@][A-Za-z0-9$#@_-]*)'?\s*\)`,
)

// cicsStartRe matches CICS START TRANSID('TRAN') — schedules a transaction.
var cicsStartRe = regexp.MustCompile(
	`(?is)\bSTART\b[^.]*?\bTRANSID\s*\(\s*'?([A-Za-z0-9$#@][A-Za-z0-9$#@_-]*)'?\s*\)`,
)

// cicsCmd is a detected CICS command with its transfer target (if any).
type cicsCmd struct {
	verb    string // LINK | XCTL | START
	target  string // program (LINK/XCTL) or transid (START)
	transid bool   // true when target is a TRANSID rather than a program
}

// extractCICSTransfers scans a CICS block body for program-transfer commands
// and returns CALLS targets. LINK/XCTL → external program CALLS; START
// TRANSID → external transaction CALLS.
func extractCICSTransfers(body string) []cicsCmd {
	var out []cicsCmd
	for _, m := range cicsXferRe.FindAllStringSubmatch(body, -1) {
		out = append(out, cicsCmd{verb: strings.ToUpper(m[1]), target: m[2]})
	}
	for _, m := range cicsStartRe.FindAllStringSubmatch(body, -1) {
		out = append(out, cicsCmd{verb: "START", target: m[1], transid: true})
	}
	return out
}

// ---------------------------------------------------------------------------
// EXEC CICS TS/TD queue + BMS/MFS screen-map depth — #4947
//
// READQ/WRITEQ/DELETEQ TS|TD carry an fs_effect but, until now, no resolvable
// queue entity — so cross-program coupling through a shared temporary-storage
// (TS) or transient-data (TD) queue was invisible (unlike native VSAM SELECT,
// #4908). We emit one SCOPE.Datastore/queue entity per queue name and wire a
// READS_FROM (READQ) / WRITES_TO (WRITEQ, DELETEQ) edge from the enclosing
// paragraph, mirroring the file-I/O data-flow model. The queue NAME may be a
// literal ('ORDERQ') or a data-item holding the queue name at run time — the
// latter is flagged dynamic_ref so the by-name resolver does not over-bind.
//
// SEND/RECEIVE MAP surface the BMS (CICS) / MFS (IMS) screen map as a
// SCOPE.View/screen entity. SEND MAP renders output to the terminal (a RENDERS
// edge, paragraph → screen); RECEIVE MAP reads operator input back (a
// REFERENCES edge). This makes the online-transaction presentation layer — the
// 3270/terminal screens a CICS/IMS program drives — a first-class node.
// ---------------------------------------------------------------------------

var (
	// cicsQueueRe matches a TS/TD queue verb and captures its QUEUE / QNAME
	// operand. The operand may be a literal ('NAME') or a data-item reference.
	// Group 1: verb (READQ|WRITEQ|DELETEQ); group 2: TS|TD; group 3: operand.
	cicsQueueRe = regexp.MustCompile(
		`(?is)\b(READQ|WRITEQ|DELETEQ)\s+(TS|TD)\b[^.]*?\b(?:QUEUE|QNAME)\s*\(\s*('?[A-Za-z0-9$#@][A-Za-z0-9$#@_.-]*'?)\s*\)`,
	)

	// cicsMapRe matches SEND/RECEIVE MAP('NAME') and optionally MAPSET('SET').
	// Group 1: verb (SEND|RECEIVE); group 2: map name (literal or data-item).
	cicsMapRe = regexp.MustCompile(
		`(?is)\b(SEND|RECEIVE)\b[^.]*?\bMAP\s*\(\s*('?[A-Za-z0-9$#@][A-Za-z0-9$#@_.-]*'?)\s*\)`,
	)

	// cicsMapsetRe captures the MAPSET operand on the same command (the BMS map
	// physically lives in a load-module mapset; recorded for resolution).
	cicsMapsetRe = regexp.MustCompile(
		`(?is)\bMAPSET\s*\(\s*('?[A-Za-z0-9$#@][A-Za-z0-9$#@_.-]*'?)\s*\)`,
	)
)

// cicsQueueOp is a detected TS/TD queue access.
type cicsQueueOp struct {
	verb    string // READQ | WRITEQ | DELETEQ
	qtype   string // TS | TD
	name    string // queue name (literal text, quotes stripped) or data-item
	dynamic bool   // true when the operand is a data-item, not a literal
}

// cicsMapOp is a detected BMS/MFS screen-map access.
type cicsMapOp struct {
	verb    string // SEND | RECEIVE
	name    string // map name (literal text, quotes stripped) or data-item
	mapset  string // MAPSET name when present
	dynamic bool   // true when the map operand is a data-item, not a literal
}

// unquoteOperand strips matching single quotes from a CICS operand and reports
// whether the operand was a quoted literal (vs a data-item reference).
func unquoteOperand(s string) (val string, literal bool) {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'") {
		return s[1 : len(s)-1], true
	}
	return s, false
}

// extractCICSQueues scans a CICS block body for TS/TD queue verbs.
func extractCICSQueues(body string) []cicsQueueOp {
	var out []cicsQueueOp
	for _, m := range cicsQueueRe.FindAllStringSubmatch(body, -1) {
		name, literal := unquoteOperand(m[3])
		out = append(out, cicsQueueOp{
			verb:    strings.ToUpper(m[1]),
			qtype:   strings.ToUpper(m[2]),
			name:    name,
			dynamic: !literal,
		})
	}
	return out
}

// extractCICSMaps scans a CICS block body for SEND/RECEIVE MAP commands.
func extractCICSMaps(body string) []cicsMapOp {
	var out []cicsMapOp
	mapset := ""
	if sm := cicsMapsetRe.FindStringSubmatch(body); sm != nil {
		if v, _ := unquoteOperand(sm[1]); v != "" {
			mapset = v
		}
	}
	for _, m := range cicsMapRe.FindAllStringSubmatch(body, -1) {
		name, literal := unquoteOperand(m[2])
		out = append(out, cicsMapOp{
			verb:    strings.ToUpper(m[1]),
			name:    name,
			mapset:  mapset,
			dynamic: !literal,
		})
	}
	return out
}

// cicsQueueRefName is the canonical name used for a TS/TD queue datastore. A
// dynamic (data-item) operand keeps the data-item name so the same variable
// across programs still couples; a literal keeps the literal queue name.
func cicsQueueRef(filePath, qtype, name string) string {
	return "scope:datastore:cobol:" + filepath.ToSlash(filePath) + "#queue:" + qtype + ":" + strings.ToUpper(name)
}

// cicsMapRef is the canonical identity for a BMS/MFS screen-map View entity.
func cicsMapRef(filePath, name string) string {
	return "scope:view:cobol:" + filepath.ToSlash(filePath) + "#map:" + strings.ToUpper(name)
}

// buildCICSQueueEntity emits the SCOPE.Datastore/queue entity for a TS/TD queue.
func buildCICSQueueEntity(filePath string, q cicsQueueOp, line int) types.EntityRecord {
	props := map[string]string{
		"queue":      q.name,
		"queue_type": q.qtype, // TS (temporary storage) or TD (transient data)
		"storage":    "cics-" + strings.ToLower(q.qtype) + "-queue",
		"provenance": "INFERRED_FROM_EXEC_CICS",
	}
	if q.dynamic {
		props["dynamic_ref"] = "true"
	}
	return types.EntityRecord{
		Name:          q.name,
		Kind:          "SCOPE.Datastore",
		Subtype:       "queue",
		QualifiedName: cicsQueueRef(filePath, q.qtype, q.name),
		SourceFile:    filePath,
		Language:      "cobol",
		StartLine:     line,
		EndLine:       line,
		Signature:     "EXEC CICS " + q.verb + " " + q.qtype + " QUEUE(" + q.name + ")",
		Properties:    props,
		QualityScore:  0.75,
	}
}

// buildCICSMapEntity emits the SCOPE.View/screen entity for a BMS/MFS map.
func buildCICSMapEntity(filePath string, mp cicsMapOp, line int) types.EntityRecord {
	props := map[string]string{
		"map":        mp.name,
		"ui":         "bms", // BMS (CICS 3270) / MFS (IMS) screen map
		"provenance": "INFERRED_FROM_EXEC_CICS",
	}
	if mp.mapset != "" {
		props["mapset"] = mp.mapset
	}
	if mp.dynamic {
		props["dynamic_ref"] = "true"
	}
	return types.EntityRecord{
		Name:          mp.name,
		Kind:          "SCOPE.View",
		Subtype:       "screen",
		QualifiedName: cicsMapRef(filePath, mp.name),
		SourceFile:    filePath,
		Language:      "cobol",
		StartLine:     line,
		EndLine:       line,
		Signature:     "EXEC CICS " + mp.verb + " MAP(" + mp.name + ")",
		Properties:    props,
		QualityScore:  0.75,
	}
}

// ---------------------------------------------------------------------------
// FILE-CONTROL / VSAM file-control extraction — #4908
//
// ENVIRONMENT DIVISION ▸ INPUT-OUTPUT SECTION ▸ FILE-CONTROL declares the
// mapping from a logical COBOL file (the name used by OPEN/READ/WRITE in the
// PROCEDURE DIVISION) to a physical dataset / VSAM cluster via
//
//	SELECT <logical-file> ASSIGN TO <ddname-or-dataset>
//	    ORGANIZATION IS {SEQUENTIAL|INDEXED|RELATIVE|LINE SEQUENTIAL}
//	    ACCESS MODE IS {SEQUENTIAL|RANDOM|DYNAMIC}
//	    RECORD KEY IS <field>   (VSAM KSDS).
//
// Until now the extractor emitted abstract fs_read / fs_write effects but no
// resolvable file entity, so `READ EMP-FILE` could not be bound to a physical
// dataset and shared-VSAM coupling between programs (and the JCL DD that
// allocates the same dataset) was invisible. We emit one SCOPE.Datastore/file
// entity per SELECT. Its `assign_to` is upper-cased so it matches the JCL DD
// name (the cross-language coupling key), mirroring the jcl extractor's
// SCOPE.Datastore/dataset naming so the by-name resolver can bridge them.
// ---------------------------------------------------------------------------

var (
	// fileControlRe marks the FILE-CONTROL paragraph that opens the SELECT
	// declarations within INPUT-OUTPUT SECTION.
	fileControlRe = regexp.MustCompile(`(?i)^\s*FILE-CONTROL\s*\.`)

	// selectAssignRe captures `SELECT <logical> ASSIGN TO <target>`. The
	// OPTIONAL keyword and a leading TO are tolerated; the target may be a
	// quoted literal, a DDname, or an environment-variable style name.
	selectAssignRe = regexp.MustCompile(
		`(?i)\bSELECT\s+(?:OPTIONAL\s+)?([A-Za-z0-9][A-Za-z0-9-]*)\s+ASSIGN\s+(?:TO\s+)?` +
			`['"]?([A-Za-z0-9$#@./_-]+)['"]?`,
	)

	// fileOrgRe / fileAccessRe / fileKeyRe capture the VSAM-relevant clauses
	// that may follow a SELECT across continuation lines (the SELECT statement
	// is buffered to its terminating period before these are scanned).
	fileOrgRe    = regexp.MustCompile(`(?i)\bORGANIZATION\s+(?:IS\s+)?(LINE\s+SEQUENTIAL|SEQUENTIAL|INDEXED|RELATIVE)`)
	fileAccessRe = regexp.MustCompile(`(?i)\bACCESS\s+(?:MODE\s+)?(?:IS\s+)?(SEQUENTIAL|RANDOM|DYNAMIC)`)
	fileKeyRe    = regexp.MustCompile(`(?i)\bRECORD\s+KEY\s+(?:IS\s+)?([A-Za-z0-9][A-Za-z0-9-]*)`)
)

var (
	// fileOpenRe captures `OPEN <mode> <file>...` — the mode governs whether
	// subsequent traffic reads or writes; we record per-file open intent.
	fileOpenRe = regexp.MustCompile(`(?i)\bOPEN\s+(INPUT|OUTPUT|I-O|EXTEND)\s+([A-Za-z][A-Za-z0-9-\s]*)`)
	// fileReadRe / fileWriteRe capture record-level I/O verbs on a logical file
	// (READ <file>; WRITE/REWRITE <record>; START/DELETE <file>). The operand
	// is resolved against the FILE-CONTROL logical-file set, so a WRITE of an
	// FD record name won't match (only file handles do).
	fileReadRe  = regexp.MustCompile(`(?i)\b(READ|START)\s+([A-Za-z][A-Za-z0-9-]*)`)
	fileWriteRe = regexp.MustCompile(`(?i)\b(WRITE|REWRITE|DELETE)\s+([A-Za-z][A-Za-z0-9-]*)`)
)

// fileSelect is one parsed SELECT ... ASSIGN clause.
type fileSelect struct {
	logical  string // logical file name (the OPEN/READ/WRITE handle)
	assignTo string // physical DDname / dataset (upper-cased coupling key)
	org      string // SEQUENTIAL | INDEXED | RELATIVE | LINE SEQUENTIAL
	access   string // SEQUENTIAL | RANDOM | DYNAMIC
	key      string // RECORD KEY (VSAM KSDS), when present
	line     int    // 1-based line of the SELECT
}

// fileResourceRef builds a stable identity for a COBOL file SCOPE.Datastore so
// READS_FROM / WRITES_TO edges from paragraphs resolve to it, and so the
// physical-dataset coupling key (assign_to) is recoverable.
func fileResourceRef(filePath, logical string) string {
	return "scope:datastore:cobol:" + filepath.ToSlash(filePath) + "#file:" + strings.ToUpper(logical)
}

// parseFileSelect extracts a SELECT ... ASSIGN clause (with any VSAM clauses
// gathered from the buffered statement body). Returns ok=false when the body
// is not a SELECT or carries no ASSIGN target.
func parseFileSelect(body string, line int) (fileSelect, bool) {
	m := selectAssignRe.FindStringSubmatch(body)
	if m == nil {
		return fileSelect{}, false
	}
	fs := fileSelect{
		logical:  m[1],
		assignTo: strings.ToUpper(strings.TrimRight(m[2], ".")),
		line:     line,
	}
	if om := fileOrgRe.FindStringSubmatch(body); om != nil {
		fs.org = strings.ToUpper(strings.Join(strings.Fields(om[1]), " "))
	}
	if am := fileAccessRe.FindStringSubmatch(body); am != nil {
		fs.access = strings.ToUpper(am[1])
	}
	if km := fileKeyRe.FindStringSubmatch(body); km != nil {
		fs.key = km[1]
	}
	return fs, true
}

// buildFileResourceEntity turns a SELECT clause into a SCOPE.Datastore/file
// entity. The org distinguishes a VSAM cluster (INDEXED/RELATIVE) from a flat
// sequential dataset; the presence of a RECORD KEY marks a KSDS.
func buildFileResourceEntity(filePath string, fs fileSelect) types.EntityRecord {
	props := map[string]string{
		"logical_file": fs.logical,
		"assign_to":    fs.assignTo,
	}
	if fs.org != "" {
		props["organization"] = fs.org
	}
	if fs.access != "" {
		props["access_mode"] = fs.access
	}
	if fs.key != "" {
		props["record_key"] = fs.key
	}
	// Classify the physical storage layer: VSAM for keyed/relative
	// organizations, sequential dataset otherwise.
	storage := "sequential"
	if fs.org == "INDEXED" || fs.org == "RELATIVE" || fs.key != "" {
		storage = "vsam"
	}
	props["storage"] = storage
	return types.EntityRecord{
		Name:          fs.logical,
		Kind:          "SCOPE.Datastore",
		Subtype:       "file",
		QualifiedName: fileResourceRef(filePath, fs.logical),
		SourceFile:    filePath,
		Language:      "cobol",
		StartLine:     fs.line,
		EndLine:       fs.line,
		Signature:     "SELECT " + fs.logical + " ASSIGN TO " + fs.assignTo,
		Properties:    props,
	}
}

// cicsCallEdge builds the CALLS relationship for a CICS program/transaction
// transfer. external=true (cross-program); via=EXEC-CICS-<VERB>.
func cicsCallEdge(c cicsCmd, line int) types.RelationshipRecord {
	props := map[string]string{
		"line":     strconv.Itoa(line),
		"via":      "EXEC-CICS-" + c.verb,
		"external": "true",
	}
	if c.transid {
		props["transid"] = c.target
	} else {
		props["program"] = c.target
	}
	return types.RelationshipRecord{
		ToID:       c.target,
		Kind:       "CALLS",
		Properties: props,
	}
}
