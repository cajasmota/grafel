// IMS DBD/PSB (DBDGEN/PSBGEN) hierarchy extraction + IMS DL/I working-storage
// VALUE/SSA resolution + IO-PCB message-queue binding — #5057 / #5054 / #5053.
//
// This file extends the per-program IMS DL/I segment-access support added in
// #4948 (depth.go: extractDLIEntities / extractDLICall) along three axes:
//
//   #5057 — DBD/PSB macro decks. The IMS database *schema* lives in DBDGEN
//           (DBD/SEGM/FIELD) and PSBGEN (PCB/SENSEG) assembler-macro source,
//           not in the COBOL program. A DBD deck declares the segment hierarchy
//           (a tree rooted at a database) with parent links and key FIELDs; a
//           PSB deck declares a program's *view* (PCBs and the sensitive
//           segments each PCB sees). We model the DBD database as a
//           SCOPE.Datastore/ims-database, each SEGM as a SCOPE.Schema/ims-segment
//           with a CONTAINS edge from its PARENT segment (the hierarchy), each
//           FIELD as a SCOPE.Schema/field CONTAINed by its segment, each PCB as
//           a SCOPE.Component/ims-pcb, and each SENSEG as an ACCESSES_TABLE edge
//           PCB → segment. This is the IMS analog of a relational table schema,
//           so a program's per-statement segment access (#4948) resolves to a
//           canonical DBD segment by name, enabling cross-program IMS coupling.
//
//   #5054 — Data-name SSA + function-code resolution. #4948 recovered the DL/I
//           function code and SSA segment only from *inline literals*. In real
//           code they are data items: CALL 'CBLTDLI' USING WS-FUNC DB-PCB
//           IO-AREA PART-SSA, where WS-FUNC PIC X(4) VALUE 'GU' and the SSA item
//           carries a VALUE 'PARTROOT(...'. We trace working-storage VALUE
//           clauses (data-name → literal) so the data-name form of the function
//           code and SSA resolves to the same operation + segment as the literal
//           form.
//
//   #5053 — IO-PCB message-queue binding. A CALL against the IO-PCB (or an
//           alternate TP PCB) reads/writes the IMS *message queue*, not a DB
//           segment — there is no SSA segment name on a message call. We surface
//           a SCOPE.Datastore/message-queue entity and a READS_FROM (GU/GN) /
//           WRITES_TO (ISRT) edge from the enclosing paragraph, distinguishing
//           the IO-PCB (message) PCB from a DB-PCB by data-name (IO-PCB /
//           *-IO-PCB / TP-PCB) or by first-argument position (the IO-PCB is the
//           first PCB in the linkage section / PSB).
// ---------------------------------------------------------------------------

package cobol

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ===========================================================================
// #5054 — working-storage VALUE tracing for data-name SSA + function codes
// ===========================================================================

var (
	// wsValueRe captures a data item carrying a VALUE literal in the DATA
	// DIVISION (WORKING-STORAGE / LINKAGE). Group 1: level number; group 2:
	// data-name; group 3: the quoted literal value (quotes stripped by the
	// caller). VALUE IS / VALUE both accepted.
	wsValueRe = regexp.MustCompile(
		`(?i)^\s*(0[1-9]|[1-7][0-9]|88)\s+([A-Za-z][A-Za-z0-9#@$-]*)\b[^.]*?\bVALUE\s+(?:IS\s+)?['"]([^'"]*)['"]`,
	)

	// dliUsingArgRe captures the positional argument list of a CBLTDLI/AIBTDLI
	// CALL. The leading `CALL '<module>' USING` is stripped by the caller; this
	// then tokenises the remaining operands (bare data-names and quoted
	// literals) in order.
	dliUsingArgRe = regexp.MustCompile(`(?i)(['"][^'"]*['"]|[A-Za-z][A-Za-z0-9#@$-]*)`)
)

// dliArg is one positional operand of a CBLTDLI USING list, after stripping the
// `CALL '<module>' USING` prefix.
type dliArg struct {
	raw     string // operand text as written
	literal bool   // true when the operand was a quoted literal
	value   string // literal value (quotes stripped) or the data-name's traced VALUE
	name    string // the data-name (upper-cased) when not a literal; "" for literals
}

// segmentFromSSA extracts the leading 1-8 char segment name from an SSA string
// (qualified `'SEG(KEY ='` or unqualified `'SEG'`). Returns "" when none.
func segmentFromSSA(ssa string) string {
	ssa = strings.TrimSpace(ssa)
	if ssa == "" {
		return ""
	}
	// The segment name is the leading token up to a '(' / space / end.
	end := len(ssa)
	for i, c := range ssa {
		if c == '(' || c == ' ' {
			end = i
			break
		}
	}
	seg := strings.ToUpper(strings.TrimSpace(ssa[:end]))
	if len(seg) == 0 || len(seg) > 8 {
		return ""
	}
	// Must look like a COBOL/IMS name (letter-led).
	if !((seg[0] >= 'A' && seg[0] <= 'Z')) {
		return ""
	}
	return seg
}

// parseDLIUsingArgs tokenises a CBLTDLI USING operand list (after the
// `CALL '<module>'` prefix has been stripped) into positional arguments,
// resolving each data-name's traced WORKING-STORAGE VALUE (#5054) when known.
func parseDLIUsingArgs(args string, wsValues map[string]string) []dliArg {
	// Drop a leading USING keyword if present so it is not mistaken for an arg.
	if i := strings.Index(strings.ToUpper(args), "USING"); i >= 0 {
		args = args[i+len("USING"):]
	}
	var out []dliArg
	for _, m := range dliUsingArgRe.FindAllString(args, -1) {
		tok := strings.TrimSpace(m)
		if tok == "" {
			continue
		}
		if (strings.HasPrefix(tok, "'") || strings.HasPrefix(tok, "\"")) && len(tok) >= 2 {
			out = append(out, dliArg{raw: tok, literal: true, value: tok[1 : len(tok)-1]})
			continue
		}
		up := strings.ToUpper(tok)
		if strings.EqualFold(tok, "USING") {
			continue
		}
		a := dliArg{raw: tok, name: up}
		if v, ok := wsValues[up]; ok {
			a.value = v
		}
		out = append(out, a)
	}
	return out
}

// ===========================================================================
// #5053 — IO-PCB (message-queue) PCB classification
// ===========================================================================

// isIOPCBName reports whether a PCB data-name is (by convention) the IMS IO-PCB
// or an alternate TP (message) PCB — i.e. a message-queue PCB rather than a
// DB-PCB. The IO-PCB is the first PCB passed to an IMS program; it is named
// IO-PCB / *-IO-PCB / TP-PCB / ALT-PCB / *-ALT-PCB by overwhelming convention.
func isIOPCBName(name string) bool {
	up := strings.ToUpper(name)
	switch {
	case up == "IO-PCB", up == "IOPCB", up == "TP-PCB", up == "TPPCB",
		up == "ALT-PCB", up == "ALTPCB", up == "ALTPCB1":
		return true
	case strings.HasSuffix(up, "-IO-PCB"), strings.HasSuffix(up, "-IOPCB"),
		strings.HasSuffix(up, "-TP-PCB"), strings.HasSuffix(up, "-ALT-PCB"):
		return true
	}
	return false
}

// imsMQRef is the canonical identity for an IMS message-queue SCOPE.Datastore.
func imsMQRef(filePath, pcb string) string {
	return "scope:datastore:cobol:" + filepath.ToSlash(filePath) + "#ims-mq:" + strings.ToUpper(pcb)
}

// buildIMSMessageQueueEntity emits a SCOPE.Datastore/message-queue entity for an
// IO-PCB message call and wires a READS_FROM (GU/GN) / WRITES_TO (ISRT) edge
// from the enclosing paragraph (fnRef). op is the resolved DL/I operation.
func buildIMSMessageQueueEntity(filePath, fnQName, fnRef, pcb, op string, line int, via string) types.EntityRecord {
	ref := imsMQRef(filePath, pcb)
	rec := types.EntityRecord{
		Name:          pcb,
		Kind:          "SCOPE.Datastore",
		Subtype:       "message-queue",
		QualifiedName: ref,
		SourceFile:    filePath,
		Language:      "cobol",
		StartLine:     line,
		EndLine:       line,
		Properties: map[string]string{
			"pcb":          pcb,
			"queue_type":   "ims-message-queue",
			"storage":      "ims-message-queue",
			"orm":          ormIMSDLI,
			"operation":    op,
			"function_ref": fnQName,
			"via":          via,
			"provenance":   "INFERRED_FROM_IMS_DLI",
			"ref":          ref,
		},
		QualityScore: 0.75,
	}
	edgeKind := "READS_FROM"
	if op == "INSERT" {
		edgeKind = "WRITES_TO"
	}
	rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
		FromID: fnRef,
		ToID:   ref,
		Kind:   edgeKind,
		Properties: map[string]string{
			"function_qname": fnQName,
			"orm":            ormIMSDLI,
			"operation":      op,
			"pcb":            pcb,
			"via":            via,
			"line":           strconv.Itoa(line),
		},
	})
	return rec
}

// extractDLICallResolved classifies a `CALL 'CBLTDLI'/'AIBTDLI' USING ...`
// statement with working-storage VALUE tracing (#5054) and IO-PCB message-queue
// binding (#5053). It supersedes the inline-literal-only extractDLICall: the
// function code and SSA segment are resolved from data-name VALUE clauses when
// they are not inline literals, and a call against an IO-PCB (message) PCB is
// bound to a SCOPE.Datastore/message-queue instead of a DB segment.
//
// Returns nil when nothing is statically recoverable — the abstract db_effect
// (effect_sinks_cobol.go) still records the read/write so the call is never
// wholly invisible. wsValues maps upper-cased data-name → traced VALUE literal.
func extractDLICallResolved(filePath, fnQName, code, module string, line int, wsValues map[string]string) []types.EntityRecord {
	fnRef := sqlFunctionRef(filePath, fnQName)

	// Strip the leading `CALL '<module>'` so the interface-module name is never
	// mistaken for an operand, then parse the positional USING list.
	args := code
	if loc := dliCallModuleRe.FindStringIndex(args); loc != nil {
		args = args[loc[1]:]
	}
	parsed := parseDLIUsingArgs(args, wsValues)
	if len(parsed) == 0 {
		return nil
	}

	// Argument 1 is the function code (literal or data-name VALUE-traced).
	// Argument 2 is the PCB. Remaining args carry the SSA(s).
	op := "EXEC"
	if fc := strings.ToUpper(strings.TrimSpace(parsed[0].value)); fc != "" {
		if dliFuncCodeReserved[fc] {
			op = dliOpFor(fc)
		}
	}

	// Identify the PCB (argument 2) and decide message vs DB.
	pcbName := ""
	if len(parsed) >= 2 && !parsed[1].literal {
		pcbName = parsed[1].name
	}

	via := "CALL-" + strings.ToUpper(module)
	if pcbName != "" && isIOPCBName(pcbName) {
		// #5053: IO-PCB message-queue call — bind to a message-queue datastore.
		return []types.EntityRecord{
			buildIMSMessageQueueEntity(filePath, fnQName, fnRef, pcbName, op, line, via+"-IOPCB"),
		}
	}

	// DB-PCB call: recover the SSA segment from arguments 3+ (a literal SSA or a
	// data-name whose traced VALUE is the SSA). #5054 resolves the data-name
	// form; the inline-literal form is preserved from #4948.
	segments := map[string]bool{}
	for i := 2; i < len(parsed); i++ {
		if seg := segmentFromSSA(parsed[i].value); seg != "" && !dliFuncCodeReserved[seg] {
			segments[seg] = true
		}
	}

	var out []types.EntityRecord
	for seg := range segments {
		resolvedVia := via
		// Tag whether the segment came from a data-name VALUE trace vs a literal.
		dataName := false
		for i := 2; i < len(parsed); i++ {
			if !parsed[i].literal && segmentFromSSA(parsed[i].value) == seg {
				dataName = true
			}
		}
		if dataName {
			resolvedVia = via + "-WS-VALUE"
		}
		rec := buildDLISegmentEntity(filePath, fnQName, fnRef, op, seg, line, resolvedVia)
		if dataName {
			rec.Properties["resolved_via"] = "ws-value"
		}
		out = append(out, rec)
	}
	return out
}

// ===========================================================================
// #5057 — DBD/PSB (DBDGEN/PSBGEN) macro-deck hierarchy extraction
// ===========================================================================

const (
	kindIMSDatabase = "SCOPE.Datastore" // subtype=ims-database
	kindIMSSegment  = "SCOPE.Schema"    // subtype=ims-segment
	kindIMSPCB      = "SCOPE.Component" // subtype=ims-pcb
)

var (
	// dbdNameRe matches the DBD macro: `DBD NAME=PARTSDB,ACCESS=HDAM`.
	dbdNameRe = regexp.MustCompile(`(?i)^\s*DBD\s+NAME=([A-Za-z][A-Za-z0-9#@$]*)`)
	// dbdSegmRe matches a SEGM macro: `SEGM NAME=PARTDETL,...,PARENT=PARTROOT`.
	dbdSegmNameRe = regexp.MustCompile(`(?i)^\s*SEGM\s+.*?\bNAME=([A-Za-z][A-Za-z0-9#@$]*)`)
	// dbdParentRe captures the PARENT= operand (segment name, 0, or
	// ((name,...)) logical form — we take the leading name token).
	dbdParentRe = regexp.MustCompile(`(?i)\bPARENT=\(*\s*([A-Za-z0][A-Za-z0-9#@$]*)`)
	// dbdFieldRe matches a FIELD macro: `FIELD NAME=PARTKEY,...` or
	// `FIELD NAME=(PARTKEY,SEQ,U),...`.
	dbdFieldRe = regexp.MustCompile(`(?i)^\s*FIELD\s+NAME=\(*\s*([A-Za-z][A-Za-z0-9#@$]*)`)
	// dbdFieldSeqRe detects the SEQ (key) flag in a FIELD macro's NAME operand.
	dbdFieldSeqRe = regexp.MustCompile(`(?i)^\s*FIELD\s+NAME=\([A-Za-z][A-Za-z0-9#@$]*,\s*SEQ`)

	// psbPcbRe matches a PCB macro and captures its TYPE (DB or TP).
	psbPcbRe = regexp.MustCompile(`(?i)^\s*PCB\s+TYPE=([A-Za-z]+)`)
	// psbPcbDBDRe captures the DBDNAME operand of a DB-PCB.
	psbPcbDBDRe = regexp.MustCompile(`(?i)\bDBDNAME=([A-Za-z][A-Za-z0-9#@$]*)`)
	// psbPcbNameRe captures the PCBNAME operand (the label by which COBOL
	// references this PCB), when present.
	psbPcbNameRe = regexp.MustCompile(`(?i)\bPCBNAME=([A-Za-z][A-Za-z0-9#@$]*)`)
	// psbSensegRe matches a SENSEG macro: `SENSEG NAME=PARTDETL,PARENT=PARTROOT`.
	psbSensegRe = regexp.MustCompile(`(?i)^\s*SENSEG\s+.*?\bNAME=([A-Za-z][A-Za-z0-9#@$]*)`)
	// psbGenRe / dbdGenRe / psbPgmRe detect the terminating GEN macro + program.
	psbPgmRe = regexp.MustCompile(`(?i)\bPGMNAME=([A-Za-z][A-Za-z0-9#@$]*)`)

	// imsDeckMarkerRe detects whether a file is a DBDGEN or PSBGEN macro deck
	// (so a misclassified .cbl/.cpy is not parsed as one, and vice versa).
	imsDeckMarkerRe = regexp.MustCompile(`(?im)^\s*(DBD\s+NAME=|PSBGEN\b|DBDGEN\b|PCB\s+TYPE=)`)
)

// isIMSMacroDeck reports whether the source looks like a DBDGEN/PSBGEN macro
// deck (vs an ordinary COBOL program / copybook). Extension is the primary
// signal; a content marker is the fallback for .cbl-named decks.
func isIMSMacroDeck(filePath, src string) bool {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".dbd", ".psb", ".dbdgen", ".psbgen":
		return true
	}
	// A COBOL program has a PROGRAM-ID / IDENTIFICATION DIVISION; a macro deck
	// does not. Only treat as a deck when a deck marker is present AND there is
	// no PROGRAM-ID (so an embedded reference in a comment never misfires).
	if imsDeckMarkerRe.MatchString(src) && !programIDRe.MatchString(src) {
		return true
	}
	return false
}

// imsSegmentRef is the canonical identity of a DBD segment SCOPE.Schema. It is
// keyed by segment name only (not file) so a program's per-statement segment
// access (#4948, dliSegmentRef) can resolve to it by name across files.
func imsSegmentRef(db, segment string) string {
	return "ims:segment:" + strings.ToUpper(db) + ":" + strings.ToUpper(segment)
}

// imsDatabaseRef is the canonical identity of a DBD database SCOPE.Datastore.
func imsDatabaseRef(db string) string {
	return "ims:database:" + strings.ToUpper(db)
}

// imsPCBRef is the canonical identity of a PSB PCB SCOPE.Component.
func imsPCBRef(filePath string, idx int, label string) string {
	if label != "" {
		return "ims:pcb:" + filepath.ToSlash(filePath) + ":" + strings.ToUpper(label)
	}
	return "ims:pcb:" + filepath.ToSlash(filePath) + ":#" + strconv.Itoa(idx)
}

// extractIMSMacroDeck parses a DBDGEN (DBD/SEGM/FIELD) or PSBGEN (PCB/SENSEG)
// assembler-macro deck into the IMS database/segment hierarchy + PCB view
// entities (#5057). Continuation lines (a non-blank in column 72 / trailing
// comma) are common in real decks; we join a macro that ends in a trailing
// comma with its successor before matching.
func extractIMSMacroDeck(src, filePath string) []types.EntityRecord {
	lines := joinMacroContinuations(src)
	var out []types.EntityRecord

	// DBD state.
	curDB := ""
	curDBIdx := -1
	curSeg := ""
	segByName := map[string]int{} // segment name → entity index

	// PSB state.
	pcbIdx := 0
	curPCBIdx := -1
	curPCBLabel := ""

	for _, ln := range lines {
		up := ln.text
		// Comment lines in assembler decks lead with '*'.
		trimmed := strings.TrimLeft(up, " ")
		if strings.HasPrefix(trimmed, "*") || trimmed == "" {
			continue
		}

		// ── DBD ───────────────────────────────────────────────────────────
		if m := dbdNameRe.FindStringSubmatch(up); m != nil {
			curDB = strings.ToUpper(m[1])
			curSeg = ""
			segByName = map[string]int{}
			curDBIdx = len(out)
			out = append(out, types.EntityRecord{
				Name:          curDB,
				Kind:          kindIMSDatabase,
				Subtype:       "ims-database",
				QualifiedName: imsDatabaseRef(curDB),
				SourceFile:    filePath,
				Language:      "cobol",
				StartLine:     ln.num,
				EndLine:       ln.num,
				Signature:     strings.TrimSpace(ln.text),
				Properties: map[string]string{
					"database":   curDB,
					"storage":    "ims-database",
					"provenance": "INFERRED_FROM_DBDGEN",
					"ref":        imsDatabaseRef(curDB),
				},
				QualityScore: 0.8,
			})
			continue
		}

		if m := dbdSegmNameRe.FindStringSubmatch(up); m != nil && curDB != "" {
			seg := strings.ToUpper(m[1])
			curSeg = seg
			parent := ""
			if pm := dbdParentRe.FindStringSubmatch(up); pm != nil {
				if p := strings.ToUpper(pm[1]); p != "0" {
					parent = p
				}
			}
			thisIdx := len(out)
			segByName[seg] = thisIdx
			props := map[string]string{
				"segment":    seg,
				"database":   curDB,
				"provenance": "INFERRED_FROM_DBDGEN",
				"ref":        imsSegmentRef(curDB, seg),
			}
			if parent != "" {
				props["parent"] = parent
			} else {
				props["root"] = "true"
			}
			out = append(out, types.EntityRecord{
				Name:          seg,
				Kind:          kindIMSSegment,
				Subtype:       "ims-segment",
				QualifiedName: imsSegmentRef(curDB, seg),
				SourceFile:    filePath,
				Language:      "cobol",
				StartLine:     ln.num,
				EndLine:       ln.num,
				Signature:     strings.TrimSpace(ln.text),
				Properties:    props,
				QualityScore:  0.8,
			})
			// CONTAINS edge: database → root segment; parent segment → child.
			if parent == "" && curDBIdx >= 0 {
				out[curDBIdx].Relationships = append(out[curDBIdx].Relationships,
					types.RelationshipRecord{
						ToID: imsSegmentRef(curDB, seg), Kind: "CONTAINS",
						Properties: map[string]string{"child": seg, "kind": "root-segment"},
					})
			} else if parent != "" {
				if pIdx, ok := segByName[parent]; ok {
					out[pIdx].Relationships = append(out[pIdx].Relationships,
						types.RelationshipRecord{
							ToID: imsSegmentRef(curDB, seg), Kind: "CONTAINS",
							Properties: map[string]string{"child": seg, "parent": parent, "kind": "child-segment"},
						})
				}
			}
			continue
		}

		if m := dbdFieldRe.FindStringSubmatch(up); m != nil && curSeg != "" {
			fld := strings.ToUpper(m[1])
			props := map[string]string{
				"field":      fld,
				"segment":    curSeg,
				"database":   curDB,
				"provenance": "INFERRED_FROM_DBDGEN",
			}
			if dbdFieldSeqRe.MatchString(up) {
				props["key"] = "true"
			}
			fldRef := imsSegmentRef(curDB, curSeg) + ":field:" + fld
			out = append(out, types.EntityRecord{
				Name:          fld,
				Kind:          kindIMSSegment,
				Subtype:       "field",
				QualifiedName: fldRef,
				SourceFile:    filePath,
				Language:      "cobol",
				StartLine:     ln.num,
				EndLine:       ln.num,
				Signature:     strings.TrimSpace(ln.text),
				Properties:    props,
				QualityScore:  0.75,
			})
			if sIdx, ok := segByName[curSeg]; ok {
				out[sIdx].Relationships = append(out[sIdx].Relationships,
					types.RelationshipRecord{
						ToID: fldRef, Kind: "CONTAINS",
						Properties: map[string]string{"child": fld, "kind": "field"},
					})
			}
			continue
		}

		// ── PSB ───────────────────────────────────────────────────────────
		if m := psbPcbRe.FindStringSubmatch(up); m != nil {
			pcbType := strings.ToUpper(m[1])
			pcbIdx++
			label := ""
			if nm := psbPcbNameRe.FindStringSubmatch(up); nm != nil {
				label = strings.ToUpper(nm[1])
			}
			dbName := ""
			if dm := psbPcbDBDRe.FindStringSubmatch(up); dm != nil {
				dbName = strings.ToUpper(dm[1])
			}
			curPCBLabel = label
			curPCBIdx = len(out)
			name := label
			if name == "" {
				if pcbType == "TP" {
					name = "IO-PCB"
				} else {
					name = "PCB#" + strconv.Itoa(pcbIdx)
				}
			}
			props := map[string]string{
				"pcb_type":   pcbType, // DB or TP
				"position":   strconv.Itoa(pcbIdx),
				"provenance": "INFERRED_FROM_PSBGEN",
				"ref":        imsPCBRef(filePath, pcbIdx, label),
			}
			// The first PCB (or any TP PCB) is the IO-PCB / message PCB (#5053).
			if pcbType == "TP" || pcbIdx == 1 {
				props["io_pcb"] = "true"
				props["queue"] = "ims-message-queue"
			}
			if dbName != "" {
				props["dbdname"] = dbName
			}
			rec := types.EntityRecord{
				Name:          name,
				Kind:          kindIMSPCB,
				Subtype:       "ims-pcb",
				QualifiedName: imsPCBRef(filePath, pcbIdx, label),
				SourceFile:    filePath,
				Language:      "cobol",
				StartLine:     ln.num,
				EndLine:       ln.num,
				Signature:     strings.TrimSpace(ln.text),
				Properties:    props,
				QualityScore:  0.8,
			}
			// A DB-PCB references its DBD database (ACCESSES_TABLE → database).
			if dbName != "" {
				rec.Relationships = append(rec.Relationships,
					types.RelationshipRecord{
						ToID: imsDatabaseRef(dbName), Kind: "ACCESSES_TABLE",
						Properties: map[string]string{"dbdname": dbName, "via": "PCB-DBDNAME"},
					})
			}
			out = append(out, rec)
			continue
		}

		if m := psbSensegRe.FindStringSubmatch(up); m != nil && curPCBIdx >= 0 {
			seg := strings.ToUpper(m[1])
			// SENSEG → the segment the PCB is sensitive to (ACCESSES_TABLE). The
			// DBD name is not always on the SENSEG; bind by segment name via the
			// PCB's DBDNAME when known, else a bare segment ref.
			db := out[curPCBIdx].Properties["dbdname"]
			to := imsSegmentRef(db, seg)
			out[curPCBIdx].Relationships = append(out[curPCBIdx].Relationships,
				types.RelationshipRecord{
					ToID: to, Kind: "ACCESSES_TABLE",
					Properties: map[string]string{"segment": seg, "via": "SENSEG", "pcb": curPCBLabel},
				})
			continue
		}

		// PSBGEN ... PGMNAME=<prog> binds the PSB to the program it serves.
		if pm := psbPgmRe.FindStringSubmatch(up); pm != nil {
			// Attach as a property on the last PCB / a synthetic note; we record
			// it on every PCB emitted so the PSB→program link is discoverable.
			prog := strings.ToUpper(pm[1])
			for i := range out {
				if out[i].Kind == kindIMSPCB && out[i].SourceFile == filePath {
					if out[i].Properties == nil {
						out[i].Properties = map[string]string{}
					}
					out[i].Properties["pgmname"] = prog
				}
			}
			continue
		}
	}
	return out
}

// macroLine is one logical (continuation-joined) macro-deck line.
type macroLine struct {
	num  int
	text string
}

// joinMacroContinuations splits the deck into physical lines, strips the
// fixed-format sequence area when present, and joins assembler continuations (a
// macro whose operand list ends in a trailing comma continues on the next
// non-comment line). The joined line keeps the FIRST physical line number.
func joinMacroContinuations(src string) []macroLine {
	raw := strings.Split(src, "\n")
	var out []macroLine
	var buf string
	var bufNum int
	for i, line := range raw {
		line = strings.TrimRight(line, "\r")
		// Strip the classic columns-1-6 sequence area when present (a digit/blank
		// run) so `DBD`/`SEGM` in area B is matched the same as area A.
		code := line
		if len(line) >= 7 && isSequenceArea(line[:6]) {
			// Keep the indicator column content as code (assembler decks use cols
			// 1-71 for statements, 72 for continuation, 73-80 for sequence).
			code = line[6:]
		}
		// Drop the column-73+ identification area when the line is long.
		trimmedRight := strings.TrimRight(code, " ")
		if buf != "" {
			buf += " " + strings.TrimSpace(code)
		} else {
			buf = trimmedRight
			bufNum = i + 1
		}
		// Continuation: the statement (ignoring trailing blanks) ends in a comma.
		if strings.HasSuffix(strings.TrimRight(buf, " "), ",") {
			continue
		}
		out = append(out, macroLine{num: bufNum, text: buf})
		buf = ""
	}
	if buf != "" {
		out = append(out, macroLine{num: bufNum, text: buf})
	}
	return out
}
