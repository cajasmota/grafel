// Package cobol implements a line/division-oriented extractor for COBOL
// source files (#2743), targeting COBOL85 / IBM Enterprise COBOL — the
// dialect that dominates real banking, insurance, and government mainframe
// codebases.
//
// COBOL's rigid, division-structured layout makes a pragmatic line parser
// the right tool: there is no community tree-sitter COBOL grammar vendored
// in smacker/go-tree-sitter, and the constructs needed for call-graph
// mapping (PROGRAM-ID, DIVISIONs, SECTIONs, PARAGRAPHs, PERFORM, CALL,
// COPY) are all line-anchored. This mirrors the established regex-based
// extractor precedent (crystal, dart, vhdl).
//
// Extracted entities:
//   - PROGRAM-ID                         → Kind="SCOPE.Component", Subtype="program"
//   - IDENTIFICATION/ENVIRONMENT/DATA/
//     PROCEDURE DIVISION                 → Kind="SCOPE.Component", Subtype="division"
//   - SECTION (any division)             → Kind="SCOPE.Component", Subtype="section"
//   - PARAGRAPH (PROCEDURE DIVISION)     → Kind="SCOPE.Operation",  Subtype="paragraph"
//   - 01/05/... data items (WORKING-
//     STORAGE / LINKAGE)                 → Kind="SCOPE.Schema",     Subtype="field"
//
// Relationships emitted:
//   - CALLS    — PERFORM <paragraph> (intra-program control flow)
//   - CALLS    — CALL '<program>' (inter-program dynamic call; external=true)
//   - IMPORTS  — COPY <copybook> (.cpy copybook inclusion — the COBOL analog
//                of #include / import)
//   - CONTAINS — program → its paragraphs (Format A structural ref)
//
// COBOL column sensitivity is respected for fixed-format source: columns
// 1-6 are the sequence-number area, column 7 is the indicator area (a `*`
// or `/` there marks a comment line; `-` marks a continuation), and columns
// 8-72 hold Area A / Area B code. The parser strips the sequence area and
// honours the indicator column so comments and sequence numbers never leak
// into entity names. Free-format source (no sequence numbers) is handled by
// the same logic since the strip is bounded and tolerant.
//
// Registers itself via init() and is imported by registry_gen.go.
package cobol

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("cobol", &Extractor{})
}

// Extractor implements extractor.Extractor for COBOL.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "cobol" }

// ---------------------------------------------------------------------------
// Compiled regular expressions (matched against the code area of each line,
// already stripped of the sequence-number area and upper-cased for keyword
// matching). Names preserve original case via separate capture handling.
// ---------------------------------------------------------------------------

var (
	// programIDRe matches `PROGRAM-ID. NAME.` (the period after the name is
	// optional in some dialects). Group 1: program name.
	programIDRe = regexp.MustCompile(`(?i)^\s*PROGRAM-ID\s*\.\s*([A-Za-z0-9][A-Za-z0-9-]*)`)

	// divisionRe matches the four standard divisions. PROCEDURE DIVISION may
	// carry a USING / RETURNING clause before its terminating period, so we
	// do not anchor on the period.
	// Group 1: division keyword (IDENTIFICATION/ENVIRONMENT/DATA/PROCEDURE).
	divisionRe = regexp.MustCompile(`(?i)^\s*(IDENTIFICATION|ENVIRONMENT|DATA|PROCEDURE)\s+DIVISION\b`)

	// sectionRe matches `<NAME> SECTION.` declarations.
	// Group 1: section name.
	sectionRe = regexp.MustCompile(`(?i)^\s*([A-Za-z0-9][A-Za-z0-9-]*)\s+SECTION\s*\.`)

	// performRe matches `PERFORM <paragraph>` control transfers. It captures
	// the first identifier after PERFORM. Inline PERFORM (PERFORM UNTIL /
	// VARYING / TIMES) is filtered via performInlineKeywords.
	// Group 1: target paragraph/section name.
	performRe = regexp.MustCompile(`(?i)\bPERFORM\s+([A-Za-z0-9][A-Za-z0-9-]*)`)

	// callRe matches `CALL '<program>'` / `CALL "<program>"` dynamic calls.
	// Group 1: literal program name (without quotes).
	callLiteralRe = regexp.MustCompile(`(?i)\bCALL\s+['"]([A-Za-z0-9$#@][A-Za-z0-9$#@_-]*)['"]`)

	// callIdentRe matches `CALL <data-item>` (dynamic call through a
	// variable holding the program name). Group 1: identifier.
	callIdentRe = regexp.MustCompile(`(?i)\bCALL\s+([A-Za-z][A-Za-z0-9-]*)`)

	// copyRe matches `COPY <copybook>` directives (optionally `COPY name.`).
	// Group 1: copybook name.
	copyRe = regexp.MustCompile(`(?i)\bCOPY\s+([A-Za-z0-9][A-Za-z0-9-]*)`)

	// dataItemRe matches level-numbered data items, e.g. `01 CUSTOMER-REC.`
	// or `05 CUST-ID PIC X(10).`. Group 1: level number; Group 2: item name.
	dataItemRe = regexp.MustCompile(`(?i)^\s*(0[1-9]|[1-4][0-9]|66|77|88)\s+([A-Za-z0-9][A-Za-z0-9-]*)`)

	// paragraphRe matches a PROCEDURE DIVISION paragraph header: an
	// identifier in Area A terminated by a period, alone on the line.
	// Group 1: paragraph name. The leading-whitespace bound is intentionally
	// loose; callers gate this to PROCEDURE DIVISION and reject SECTION lines.
	paragraphRe = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9-]*)\s*\.\s*$`)
)

// performInlineKeywords are tokens that follow PERFORM in an inline
// (non-procedural) PERFORM and therefore are NOT paragraph targets.
var performInlineKeywords = map[string]bool{
	"UNTIL": true, "VARYING": true, "TIMES": true, "WITH": true,
	"TEST": true, "FOREVER": true,
}

// cobolReservedHeads are reserved words / verbs that can begin a line and
// would otherwise be misread as a paragraph header (a bare identifier +
// period). Paragraph detection rejects these.
var cobolReservedHeads = map[string]bool{
	"IDENTIFICATION": true, "ENVIRONMENT": true, "DATA": true, "PROCEDURE": true,
	"PROGRAM-ID": true, "AUTHOR": true, "DATE-WRITTEN": true, "DATE-COMPILED": true,
	"INSTALLATION": true, "SECURITY": true, "REMARKS": true,
	"WORKING-STORAGE": true, "LINKAGE": true, "FILE": true, "LOCAL-STORAGE": true,
	"CONFIGURATION": true, "INPUT-OUTPUT": true, "SOURCE-COMPUTER": true,
	"OBJECT-COMPUTER": true, "SPECIAL-NAMES": true, "FILE-CONTROL": true,
	"I-O-CONTROL": true, "FD": true, "SD": true,
	"STOP": true, "GOBACK": true, "EXIT": true, "END": true,
	"IF": true, "ELSE": true, "MOVE": true, "ADD": true, "SUBTRACT": true,
	"MULTIPLY": true, "DIVIDE": true, "COMPUTE": true, "DISPLAY": true,
	"ACCEPT": true, "PERFORM": true, "CALL": true, "OPEN": true, "CLOSE": true,
	"READ": true, "WRITE": true, "REWRITE": true, "DELETE": true, "START": true,
	"EVALUATE": true, "WHEN": true, "CONTINUE": true, "INITIALIZE": true,
	"SET": true, "STRING": true, "UNSTRING": true, "INSPECT": true,
	"COPY": true, "GO": true, "RETURN": true, "SEARCH": true, "SORT": true,
	"MERGE": true, "RELEASE": true, "CANCEL": true, "EXEC": true,
}

// Extract processes a COBOL source file and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractCOBOL(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "cobol")
	extractor.TagEntitiesLanguage(out, "cobol")
	return out, nil
}

// codeLine is one logical source line after sequence-area stripping and
// comment/continuation classification.
type codeLine struct {
	num     int    // 1-indexed physical line number
	code    string // code-area text (sequence area stripped), original case
	upper   string // upper-cased code, for keyword matching
	comment bool   // true for comment lines (* or / in indicator column)
}

// stripSequenceArea removes the COBOL fixed-format sequence-number area
// (columns 1-6) and classifies the indicator column (7). Free-format lines
// (which lack a 6-char numeric prefix) are returned unmodified. The heuristic
// is deliberately conservative: we only strip columns 1-6 when the indicator
// column holds a recognised marker (space, *, /, -, or a digit run in 1-6),
// so free-format code beginning in column 1 is preserved.
func stripSequenceArea(raw string) (code string, isComment bool) {
	// Expand a leading run only enough to inspect the indicator column.
	if len(raw) >= 7 {
		seq := raw[:6]
		ind := raw[6]
		// Treat as fixed-format when the first 6 cols are blank or numeric
		// (the classic sequence area) — then the indicator column governs.
		if isSequenceArea(seq) {
			switch ind {
			case '*', '/':
				return "", true
			case '-', ' ', 'D', 'd':
				body := raw[7:]
				if len(body) > 66 { // columns 73+ are the identification area
					body = body[:66]
				}
				return body, false
			}
		}
	}
	// Free-format / short line: a `*` as the first non-space char is a
	// comment in free-format COBOL too.
	trimmed := strings.TrimLeft(raw, " \t")
	if strings.HasPrefix(trimmed, "*>") || strings.HasPrefix(trimmed, "*") {
		return "", true
	}
	return raw, false
}

// isSequenceArea reports whether the 6-char prefix looks like a fixed-format
// sequence-number area: all blanks or all digits/blanks.
func isSequenceArea(seq string) bool {
	allBlank := true
	for _, c := range seq {
		if c != ' ' {
			allBlank = false
		}
		if c != ' ' && (c < '0' || c > '9') {
			return false
		}
	}
	_ = allBlank
	return true
}

// splitLines classifies every physical line.
func splitLines(src string) []codeLine {
	raw := strings.Split(src, "\n")
	out := make([]codeLine, 0, len(raw))
	for i, line := range raw {
		line = strings.TrimRight(line, "\r")
		code, isComment := stripSequenceArea(line)
		out = append(out, codeLine{
			num:     i + 1,
			code:    code,
			upper:   strings.ToUpper(code),
			comment: isComment,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Core extraction
// ---------------------------------------------------------------------------

func extractCOBOL(src, filePath string) []types.EntityRecord {
	lines := splitLines(src)
	var entities []types.EntityRecord

	// Program scope: the index of the current PROGRAM-ID entity (-1 = none).
	programIdx := -1
	programName := ""
	// Tracks the current division so paragraph detection only fires inside
	// PROCEDURE DIVISION.
	inProcedureDivision := false
	// Tracks whether we are inside a data division area where 01/05 items
	// are meaningful (WORKING-STORAGE / LINKAGE).
	inDataItemArea := false

	// Deduplicate COPY / CALL / PERFORM targets at the program level so a
	// copybook referenced twice yields a single IMPORTS edge.
	seenCopy := map[string]bool{}

	// paragraphIdx maps paragraph name → entity index, for attaching CALLS
	// edges discovered while scanning paragraph bodies.
	currentParagraphIdx := -1

	addProgramRel := func(rel types.RelationshipRecord) {
		if programIdx >= 0 {
			entities[programIdx].Relationships = append(entities[programIdx].Relationships, rel)
		}
	}

	for _, ln := range lines {
		if ln.comment || strings.TrimSpace(ln.code) == "" {
			continue
		}

		// ── PROGRAM-ID ────────────────────────────────────────────────────
		if m := programIDRe.FindStringSubmatch(ln.code); m != nil {
			programName = m[1]
			programIdx = len(entities)
			currentParagraphIdx = -1
			entities = append(entities, types.EntityRecord{
				Name:       programName,
				Kind:       "SCOPE.Component",
				Subtype:    "program",
				SourceFile: filePath,
				Language:   "cobol",
				StartLine:  ln.num,
				EndLine:    ln.num,
				Signature:  "PROGRAM-ID. " + programName,
			})
			continue
		}

		// ── DIVISION ──────────────────────────────────────────────────────
		if m := divisionRe.FindStringSubmatch(ln.code); m != nil {
			div := strings.ToUpper(m[1])
			inProcedureDivision = div == "PROCEDURE"
			inDataItemArea = false
			currentParagraphIdx = -1
			entities = append(entities, types.EntityRecord{
				Name:       div + " DIVISION",
				Kind:       "SCOPE.Component",
				Subtype:    "division",
				SourceFile: filePath,
				Language:   "cobol",
				StartLine:  ln.num,
				EndLine:    ln.num,
				Signature:  div + " DIVISION.",
			})
			// Extend the program's EndLine to the furthest seen division.
			if programIdx >= 0 && ln.num > entities[programIdx].EndLine {
				entities[programIdx].EndLine = ln.num
			}
			continue
		}

		// ── WORKING-STORAGE / LINKAGE section markers gate data items ─────
		if hasSectionKeyword(ln.upper, "WORKING-STORAGE") ||
			hasSectionKeyword(ln.upper, "LINKAGE") ||
			hasSectionKeyword(ln.upper, "LOCAL-STORAGE") {
			inDataItemArea = true
		} else if hasSectionKeyword(ln.upper, "FILE") && strings.Contains(ln.upper, "SECTION") {
			inDataItemArea = true
		}

		// ── SECTION ───────────────────────────────────────────────────────
		if m := sectionRe.FindStringSubmatch(ln.code); m != nil {
			name := m[1]
			// `WORKING-STORAGE SECTION.` etc. are structural section headers,
			// emit them as section components too (skip the reserved division
			// header words which sectionRe won't match anyway).
			entities = append(entities, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "section",
				SourceFile: filePath,
				Language:   "cobol",
				StartLine:  ln.num,
				EndLine:    ln.num,
				Signature:  strings.TrimSpace(ln.code),
			})
			currentParagraphIdx = -1
			continue
		}

		// ── COPY (IMPORTS) — valid anywhere ──────────────────────────────
		if m := copyRe.FindStringSubmatch(ln.code); m != nil {
			book := m[1]
			if !seenCopy[strings.ToUpper(book)] {
				seenCopy[strings.ToUpper(book)] = true
				rel := types.RelationshipRecord{
					FromID: filePath,
					ToID:   book,
					Kind:   "IMPORTS",
					Properties: map[string]string{
						"line":     strconv.Itoa(ln.num),
						"copybook": book,
					},
				}
				addProgramRel(rel)
				// Also emit a placeholder entity for the copybook so the
				// import target is resolvable (mirrors crystal's require).
				entities = append(entities, types.EntityRecord{
					Name:       book,
					Kind:       "SCOPE.Component",
					Subtype:    "copybook",
					SourceFile: filePath,
					Language:   "cobol",
					StartLine:  ln.num,
					EndLine:    ln.num,
				})
			}
			// COPY may share a line with other code in rare cases; fall
			// through so PERFORM/CALL on the same line are still scanned.
		}

		// ── DATA ITEMS (fields) ──────────────────────────────────────────
		// Emit fields for level-numbered items outside PROCEDURE DIVISION.
		// This covers both DATA DIVISION sections and standalone copybooks
		// (.cpy) that carry data items with no surrounding division. The
		// inDataItemArea flag is retained for documentation/clarity but the
		// gate is the absence of a PROCEDURE DIVISION context.
		_ = inDataItemArea
		if !inProcedureDivision {
			if m := dataItemRe.FindStringSubmatch(ln.code); m != nil {
				level := m[1]
				fieldName := m[2]
				// FILLER is anonymous padding — skip.
				if strings.EqualFold(fieldName, "FILLER") {
					continue
				}
				entities = append(entities, types.EntityRecord{
					Name:       fieldName,
					Kind:       "SCOPE.Schema",
					Subtype:    "field",
					SourceFile: filePath,
					Language:   "cobol",
					StartLine:  ln.num,
					EndLine:    ln.num,
					Signature:  strings.TrimSpace(ln.code),
					Properties: map[string]string{"level": level},
				})
				continue
			}
		}

		// ── PROCEDURE DIVISION: paragraphs + PERFORM/CALL ────────────────
		if inProcedureDivision {
			// Paragraph header: a lone identifier + period in Area A.
			if m := paragraphRe.FindStringSubmatch(ln.code); m != nil {
				name := m[1]
				up := strings.ToUpper(name)
				if !cobolReservedHeads[up] && !isAllDigits(name) {
					currentParagraphIdx = len(entities)
					rec := types.EntityRecord{
						Name:       name,
						Kind:       "SCOPE.Operation",
						Subtype:    "paragraph",
						SourceFile: filePath,
						Language:   "cobol",
						StartLine:  ln.num,
						EndLine:    ln.num,
						Signature:  name,
					}
					entities = append(entities, rec)
					// CONTAINS: program → paragraph.
					if programIdx >= 0 {
						toID := extractor.BuildOperationStructuralRef("cobol", filePath, name)
						entities[programIdx].Relationships = append(entities[programIdx].Relationships,
							types.RelationshipRecord{ToID: toID, Kind: "CONTAINS"})
					}
					continue
				}
			}

			// Extend the enclosing paragraph's EndLine as its body scrolls by.
			if currentParagraphIdx >= 0 && ln.num > entities[currentParagraphIdx].EndLine {
				entities[currentParagraphIdx].EndLine = ln.num
			}

			// PERFORM <paragraph> → CALLS (intra-program).
			for _, pm := range performRe.FindAllStringSubmatch(ln.code, -1) {
				target := pm[1]
				if performInlineKeywords[strings.ToUpper(target)] {
					continue
				}
				rel := types.RelationshipRecord{
					ToID: target,
					Kind: "CALLS",
					Properties: map[string]string{
						"line": strconv.Itoa(ln.num),
						"via":  "PERFORM",
					},
				}
				attachCall(entities, currentParagraphIdx, programIdx, rel)
			}

			// CALL '<program>' → CALLS (external, inter-program).
			matchedLiteral := false
			for _, cm := range callLiteralRe.FindAllStringSubmatch(ln.code, -1) {
				matchedLiteral = true
				rel := types.RelationshipRecord{
					ToID: cm[1],
					Kind: "CALLS",
					Properties: map[string]string{
						"line":     strconv.Itoa(ln.num),
						"via":      "CALL",
						"external": "true",
					},
				}
				attachCall(entities, currentParagraphIdx, programIdx, rel)
			}
			// CALL <data-item> (dynamic call via a variable). Only when no
			// literal CALL matched on the line (avoids double-counting).
			if !matchedLiteral {
				for _, cm := range callIdentRe.FindAllStringSubmatch(ln.code, -1) {
					ident := cm[1]
					if strings.EqualFold(ident, "USING") {
						continue
					}
					rel := types.RelationshipRecord{
						ToID: ident,
						Kind: "CALLS",
						Properties: map[string]string{
							"line":        strconv.Itoa(ln.num),
							"via":         "CALL",
							"external":    "true",
							"dynamic_ref": "true",
						},
					}
					attachCall(entities, currentParagraphIdx, programIdx, rel)
				}
			}
		}
	}

	_ = programName
	return entities
}

// attachCall attaches a CALLS relationship to the current paragraph when one
// is open, otherwise to the program entity (CALLs in the implicit first
// paragraph / inline procedure body).
func attachCall(entities []types.EntityRecord, paragraphIdx, programIdx int, rel types.RelationshipRecord) {
	idx := paragraphIdx
	if idx < 0 {
		idx = programIdx
	}
	if idx < 0 || idx >= len(entities) {
		return
	}
	entities[idx].Relationships = append(entities[idx].Relationships, rel)
}

// hasSectionKeyword reports whether an upper-cased line begins (after
// leading blanks) with the given section keyword followed by a word break.
func hasSectionKeyword(upper, kw string) bool {
	s := strings.TrimLeft(upper, " \t")
	if !strings.HasPrefix(s, kw) {
		return false
	}
	rest := s[len(kw):]
	return rest == "" || rest[0] == ' ' || rest[0] == '.' || rest[0] == '\t'
}

// isAllDigits reports whether s is composed solely of ASCII digits (a bare
// COBOL sequence/section number that must not be treated as a paragraph).
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
