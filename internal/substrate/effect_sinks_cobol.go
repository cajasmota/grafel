// COBOL effect-sink sniffer (#2743, Phase 1A participation).
//
// COBOL's side-effect primitives are verbs, not library calls, which makes
// them unusually clean to detect:
//
//   - fs_read  : READ <file> / OPEN INPUT / OPEN I-O / START / RETURN
//     (sequential / indexed / relative file I/O on an FD record)
//   - fs_write : WRITE <rec> / REWRITE <rec> / DELETE <file> / OPEN OUTPUT /
//     OPEN EXTEND / RELEASE
//   - db_read  : EXEC SQL SELECT / OPEN <cursor> / FETCH (embedded DB2);
//     EXEC DLI GU|GN|GHU|GNP|GHN / CALL 'CBLTDLI'/'AIBTDLI' get-function
//     (IMS DB/DC segment retrieval, #4948)
//   - db_write : EXEC SQL INSERT|UPDATE|DELETE|MERGE (embedded DB2);
//     EXEC DLI ISRT|REPL|DLET / CALL CBLTDLI/AIBTDLI mutate-function
//     (IMS DB/DC segment insert/replace/delete, #4948)
//   - http_out : EXEC CICS LINK / XCTL / WEB / INVOKE — outbound transaction
//     / service calls treated as the COBOL analog of an outbound
//     request (the closest effect in the lattice for CICS
//     inter-program / service control transfers)
//   - mutation : MOVE ... TO <ident> / SET <ident> / COMPUTE <ident> /
//     ADD|SUBTRACT|MULTIPLY|DIVIDE ... GIVING <ident> /
//     STRING|UNSTRING ... INTO <ident> / INITIALIZE <ident> /
//     INSPECT ... REPLACING (#4946) — an observable working-storage
//     state write (low confidence; the most common COBOL statement
//     so it is intentionally weak)
//
// Function attribution binds each sink to the nearest preceding PARAGRAPH
// header (a lone identifier + period in Area A inside PROCEDURE DIVISION).
// COBOL paragraphs are the unit of structured logic — the COBOL "function" —
// so this matches the extractor's SCOPE.Operation(subtype=paragraph) entities
// and lets the generic effect-propagation pass union effects up the
// PERFORM/CALL graph.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("cobol", sniffEffectsCobol) }

// cobolParagraphHeaderRe matches a paragraph header: an identifier followed
// by a period, alone on the line. Bounded to typical COBOL identifiers.
// Reserved division/section words are filtered in scanCobolParagraphHeaders.
var cobolParagraphHeaderRe = regexp.MustCompile(
	`(?im)^[ \t]*([A-Za-z][A-Za-z0-9-]*)\s*\.\s*$`,
)

// cobolFSReadRe matches file-read verbs / read-mode OPEN.
var cobolFSReadRe = regexp.MustCompile(
	`(?im)\bREAD\s+[A-Za-z]` +
		`|\bOPEN\s+(?:INPUT|I-O)\b` +
		`|\bSTART\s+[A-Za-z]` +
		`|\bRETURN\s+[A-Za-z]`,
)

// cobolFSWriteRe matches file-write verbs / write-mode OPEN.
var cobolFSWriteRe = regexp.MustCompile(
	`(?im)\bWRITE\s+[A-Za-z]` +
		`|\bREWRITE\s+[A-Za-z]` +
		`|\bDELETE\s+[A-Za-z]` +
		`|\bRELEASE\s+[A-Za-z]` +
		`|\bOPEN\s+(?:OUTPUT|EXTEND|I-O)\b`,
)

// cobolDBReadRe matches embedded SQL reads / cursor traffic.
var cobolDBReadRe = regexp.MustCompile(
	`(?is)EXEC\s+SQL\b[^.]*?\bSELECT\b` +
		`|(?i)EXEC\s+SQL\b[^.]*?\bFETCH\b` +
		`|(?i)EXEC\s+SQL\b[^.]*?\bOPEN\b` +
		`|(?i)EXEC\s+SQL\b[^.]*?\bDECLARE\b[^.]*?\bCURSOR\b`,
)

// cobolDBWriteRe matches embedded SQL writes.
var cobolDBWriteRe = regexp.MustCompile(
	`(?is)EXEC\s+SQL\b[^.]*?\b(?:INSERT|UPDATE|DELETE|MERGE|CREATE|DROP|ALTER)\b`,
)

// cobolDLIReadRe matches IMS DB/DC (DL/I) segment retrieval — the 2nd most
// common mainframe data layer (#4948). Both call shapes are covered: the
// command-level EXEC DLI GU|GN|GHU|GNP|GHN form, and the call-level
// CALL 'CBLTDLI'/'AIBTDLI' ... with a get function-code literal.
var cobolDLIReadRe = regexp.MustCompile(
	`(?is)\bEXEC\s+DLI\s+(?:GU|GN|GHU|GNP|GHN|GHNP)\b` +
		`|(?is)\bCALL\s+['"](?:CBL|AIB|ASM|PLI)TDLI['"][^.]*?['"]\s*(?:GU|GN|GHU|GNP|GHN|GHNP)\s*['"]`,
)

// cobolDLIWriteRe matches IMS DL/I segment mutation: ISRT (insert), REPL
// (replace/update), DLET (delete) — via EXEC DLI or the CBLTDLI/AIBTDLI call.
var cobolDLIWriteRe = regexp.MustCompile(
	`(?is)\bEXEC\s+DLI\s+(?:ISRT|REPL|DLET)\b` +
		`|(?is)\bCALL\s+['"](?:CBL|AIB|ASM|PLI)TDLI['"][^.]*?['"]\s*(?:ISRT|REPL|DLET)\s*['"]`,
)

// cobolCICSRe matches EXEC CICS outbound transaction / service primitives:
// program transfer (LINK/XCTL), transaction scheduling (START), and terminal
// / web service traffic (SEND/RECEIVE/WEB/INVOKE) — all modelled as the COBOL
// analog of an outbound request (#2838 Phase 2 deepens this from a flag into
// the EXEC-CICS verb set).
var cobolCICSRe = regexp.MustCompile(
	`(?is)EXEC\s+CICS\b[^.]*?\b(?:LINK|XCTL|START|INVOKE|WEB|SEND|RECEIVE)\b`,
)

// cobolCICSFSReadRe matches EXEC CICS READ / READQ (file or transient/temp
// storage queue read) — a filesystem-class read effect on the CICS region's
// VSAM files and queues.
var cobolCICSFSReadRe = regexp.MustCompile(
	`(?is)EXEC\s+CICS\b[^.]*?\b(?:READQ|READ)\b`,
)

// cobolCICSFSWriteRe matches EXEC CICS WRITE / WRITEQ / REWRITE / DELETE /
// DELETEQ — a filesystem-class write effect on CICS files and queues.
var cobolCICSFSWriteRe = regexp.MustCompile(
	`(?is)EXEC\s+CICS\b[^.]*?\b(?:WRITEQ|WRITE|REWRITE|DELETEQ|DELETE)\b`,
)

// cobolMutationRe matches working-storage state writes. Beyond the core
// MOVE/SET/COMPUTE writes (#2743), it covers the arithmetic GIVING forms and
// the string / table mutation verbs (#4946):
//
//   - ADD/SUBTRACT/MULTIPLY/DIVIDE ... GIVING <ident> — the GIVING target is
//     the written receiving field (the non-GIVING form mutates the trailing
//     operand and is covered by the leading-verb alternatives).
//   - STRING ... INTO <ident> / UNSTRING ... INTO <ident> — concatenation /
//     split write into a receiving data item.
//   - INITIALIZE <ident> — resets one or more data items to their default.
//   - INSPECT <ident> ... REPLACING — in-place character substitution write.
var cobolMutationRe = regexp.MustCompile(
	`(?im)\bMOVE\b[^.]*?\bTO\s+[A-Za-z]` +
		`|\bSET\s+[A-Za-z][A-Za-z0-9-]*\s+TO\b` +
		`|\bCOMPUTE\s+[A-Za-z]` +
		`|\b(?:ADD|SUBTRACT|MULTIPLY|DIVIDE)\b[^.]*?\bGIVING\s+[A-Za-z]` +
		`|\b(?:STRING|UNSTRING)\b[^.]*?\bINTO\s+[A-Za-z]` +
		`|\bINITIALIZE\s+[A-Za-z]` +
		`|\bINSPECT\b[^.]*?\bREPLACING\b`,
)

func sniffEffectsCobol(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanCobolParagraphHeaders(content)
	var out []EffectMatch
	out = appendCobolMatches(out, content, headers, cobolFSReadRe, EffectFSRead, "READ/OPEN-INPUT", 1.0)
	out = appendCobolMatches(out, content, headers, cobolFSWriteRe, EffectFSWrite, "WRITE/REWRITE/DELETE", 1.0)
	out = appendCobolMatches(out, content, headers, cobolDBReadRe, EffectDBRead, "EXEC-SQL.SELECT/FETCH", 1.0)
	out = appendCobolMatches(out, content, headers, cobolDBWriteRe, EffectDBWrite, "EXEC-SQL.INSERT/UPDATE/DELETE", 1.0)
	out = appendCobolMatches(out, content, headers, cobolDLIReadRe, EffectDBRead, "IMS-DLI.GU/GN/GHU", 1.0)
	out = appendCobolMatches(out, content, headers, cobolDLIWriteRe, EffectDBWrite, "IMS-DLI.ISRT/REPL/DLET", 1.0)
	out = appendCobolMatches(out, content, headers, cobolCICSRe, EffectHTTPOut, "EXEC-CICS.LINK/XCTL/WEB", 0.85)
	out = appendCobolMatches(out, content, headers, cobolCICSFSReadRe, EffectFSRead, "EXEC-CICS.READ/READQ", 0.9)
	out = appendCobolMatches(out, content, headers, cobolCICSFSWriteRe, EffectFSWrite, "EXEC-CICS.WRITE/WRITEQ/REWRITE", 0.9)
	out = appendCobolMatches(out, content, headers, cobolMutationRe, EffectMutation, "MOVE/SET/COMPUTE/ARITH-GIVING/STRING/INITIALIZE/INSPECT", 0.6)
	return out
}

// cobolReservedHeaders are reserved words that the bare-identifier paragraph
// regex can match but are NOT paragraph headers (division/section heads and
// common single-word statements that may sit alone on a line).
var cobolReservedHeaders = map[string]bool{
	"IDENTIFICATION": true, "ENVIRONMENT": true, "DATA": true, "PROCEDURE": true,
	"CONFIGURATION": true, "INPUT-OUTPUT": true, "FILE-CONTROL": true,
	"WORKING-STORAGE": true, "LINKAGE": true, "LOCAL-STORAGE": true,
	"STOP": true, "GOBACK": true, "EXIT": true, "CONTINUE": true,
}

func scanCobolParagraphHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range cobolParagraphHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		name := content[m[2]:m[3]]
		up := upperASCII(name)
		if cobolReservedHeaders[up] {
			continue
		}
		// Reject SECTION/DIVISION header tails that slipped through (the
		// regex anchors on a lone token, but a leading "FOO SECTION" would
		// not match anyway; this is belt-and-suspenders for "DIVISION.").
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: name})
	}
	return hs
}

func appendCobolMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
	for _, m := range re.FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		out = append(out, EffectMatch{
			Function:   fn,
			Line:       line,
			Effect:     eff,
			Sink:       sink,
			Confidence: conf,
		})
	}
	return out
}

// upperASCII upper-cases an ASCII token without allocating via strings
// package churn for the common short-identifier case.
func upperASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}
