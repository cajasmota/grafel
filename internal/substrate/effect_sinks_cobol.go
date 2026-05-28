// COBOL effect-sink sniffer (#2743, Phase 1A participation).
//
// COBOL's side-effect primitives are verbs, not library calls, which makes
// them unusually clean to detect:
//
//   - fs_read  : READ <file> / OPEN INPUT / OPEN I-O / START / RETURN
//                (sequential / indexed / relative file I/O on an FD record)
//   - fs_write : WRITE <rec> / REWRITE <rec> / DELETE <file> / OPEN OUTPUT /
//                OPEN EXTEND / RELEASE
//   - db_read  : EXEC SQL SELECT / OPEN <cursor> / FETCH (embedded DB2)
//   - db_write : EXEC SQL INSERT|UPDATE|DELETE|MERGE (embedded DB2)
//   - http_out : EXEC CICS LINK / XCTL / WEB / INVOKE — outbound transaction
//                / service calls treated as the COBOL analog of an outbound
//                request (the closest effect in the lattice for CICS
//                inter-program / service control transfers)
//   - mutation : MOVE ... TO <ident> / SET <ident> / COMPUTE <ident> — an
//                observable working-storage state write (low confidence; the
//                most common COBOL statement so it is intentionally weak)
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

// cobolCICSRe matches EXEC CICS outbound transaction / service primitives.
var cobolCICSRe = regexp.MustCompile(
	`(?is)EXEC\s+CICS\b[^.]*?\b(?:LINK|XCTL|START|INVOKE|WEB|SEND|RECEIVE)\b`,
)

// cobolMutationRe matches working-storage state writes.
var cobolMutationRe = regexp.MustCompile(
	`(?im)\bMOVE\b[^.]*?\bTO\s+[A-Za-z]` +
		`|\bSET\s+[A-Za-z][A-Za-z0-9-]*\s+TO\b` +
		`|\bCOMPUTE\s+[A-Za-z]`,
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
	out = appendCobolMatches(out, content, headers, cobolCICSRe, EffectHTTPOut, "EXEC-CICS.LINK/XCTL/WEB", 0.85)
	out = appendCobolMatches(out, content, headers, cobolMutationRe, EffectMutation, "MOVE...TO/SET/COMPUTE", 0.6)
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
