// Package jcl implements a line-oriented extractor for IBM Job Control
// Language (JCL) — the mainframe batch-orchestration DSL that drives z/OS
// JES2 / JES3 job submission. JCL is the operational glue around COBOL: a
// JOB declares a unit of work, each EXEC step names a program (EXEC PGM=) or
// a cataloged/inline procedure (EXEC PROC=), and DD statements bind the
// datasets a step's program reads and writes.
//
// This extractor is the JCL half of the JCL→COBOL cross-language bridge
// (#2843), the mainframe analog of the HTTP cross-repo linker: an
// `EXEC PGM=PAYROLL` step emits a CALLS edge whose bare ToID is the program
// name, which the by-name resolver binds to the COBOL PROGRAM-ID entity
// `PAYROLL` extracted from a sibling .cbl source. No new linker code is
// needed — the same intra-repo by-name resolution that joins COBOL's own
// inter-program CALL '<prog>' edges joins the JCL step → COBOL program edge.
//
// JCL has no community tree-sitter grammar; its rigid card-oriented layout
// (statements begin with `//` in columns 1-2, the optional name field in
// column 3, an operation keyword, then a comma-separated operand list) makes
// a pragmatic line parser the right tool. This mirrors the regex/line-based
// extractor precedent (cobol, verilog, vhdl).
//
// Extracted entities:
//   - //NAME JOB ...                       → Kind="SCOPE.Component", Subtype="job"
//   - //STEP EXEC PGM=<program> ...        → Kind="SCOPE.Operation", Subtype="step"
//   - //STEP EXEC PROC=<proc> / EXEC <proc>→ Kind="SCOPE.Operation", Subtype="step"
//   - //NAME PROC ... // PEND (inline proc) → Kind="SCOPE.Component", Subtype="proc"
//   - //DD DSN=<dataset> ...               → Kind="SCOPE.Datastore", Subtype="dataset"
//
// Relationships emitted:
//   - CALLS    — EXEC PGM=<program>  (step → COBOL program; the bridge edge,
//     Properties: via="EXEC PGM", external="true", cross_language="cobol")
//   - CALLS    — EXEC PROC=<proc>    (step → procedure; via="EXEC PROC")
//   - CALLS    — TSO CALL <member>   (IKJEFTxx terminal-monitor step → the
//     program named on its SYSTSIN instream control card; via="TSO CALL",
//     recovers the indirect JCL→program edge a shell utility hides)
//   - CONTAINS — job → its steps; proc → its steps
//   - READS_FROM / WRITES_TO — step → dataset (DD DISP governs direction)
//   - IMPORTS  — INCLUDE MEMBER=<name> (job/proc → spliced PROCLIB/JCLLIB
//     member; import_kind="include", a real cross-file dependency)
//
// Registers itself via init() and is imported by registry_gen.go.
package jcl

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("jcl", &Extractor{})
}

// Extractor implements extractor.Extractor for JCL.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "jcl" }

// ---------------------------------------------------------------------------
// Compiled regular expressions. JCL statements are line-oriented: `//` in
// columns 1-2, an optional name token, an operation verb, then operands. We
// match against the operand-bearing portion of each (already-joined) logical
// statement.
// ---------------------------------------------------------------------------

var (
	// stmtRe splits a JCL statement into (name, verb, operands). The name
	// field is optional (DD/EXEC continuation overrides aside). Group 1: name
	// (may be empty); group 2: verb (JOB/EXEC/DD/PROC/PEND/...); group 3:
	// operands (may be empty).
	stmtRe = regexp.MustCompile(`(?i)^//\s*([A-Za-z$#@][A-Za-z0-9$#@]*)?\s+([A-Za-z]+)\b\s*(.*)$`)

	// execPgmRe matches the PGM= operand of an EXEC statement.
	// Group 1: program name.
	execPgmRe = regexp.MustCompile(`(?i)\bPGM\s*=\s*([A-Za-z$#@][A-Za-z0-9$#@]*)`)

	// execProcRe matches the explicit PROC= operand of an EXEC statement.
	// Group 1: procedure name.
	execProcRe = regexp.MustCompile(`(?i)\bPROC\s*=\s*([A-Za-z$#@][A-Za-z0-9$#@]*)`)

	// ddDsnRe matches the DSN= / DSNAME= operand of a DD statement.
	// Group 1: dataset name (may be a qualified name like PROD.PAYROLL.MASTER
	// or a temp name like &&TEMP).
	ddDsnRe = regexp.MustCompile(`(?i)\bDSN(?:AME)?\s*=\s*(&{0,2}[A-Za-z0-9$#@.()-]+)`)

	// ddDispRe captures the leading DISP disposition token (NEW/OLD/SHR/MOD).
	// Group 1: status.
	ddDispRe = regexp.MustCompile(`(?i)\bDISP\s*=\s*\(?\s*([A-Za-z]+)`)

	// includeMemberRe matches the MEMBER= operand of an INCLUDE statement
	// (`//   INCLUDE MEMBER=SHRPROC`). INCLUDE textually splices the named
	// PROCLIB/JCLLIB member into the job stream at this point — a real
	// cross-file dependency the by-name resolver can bind to the member's
	// own JCL/proc entity. Group 1: member name.
	includeMemberRe = regexp.MustCompile(`(?i)\bMEMBER\s*=\s*([A-Za-z$#@][A-Za-z0-9$#@]*)`)

	// utilProgRe recognises the small set of z/OS "shell" utilities that do
	// not do the real work themselves but invoke a SECOND program (or a
	// subsystem program) named on their instream SYSTSIN/SYSIN control
	// cards. IKJEFT01/IKJEFT1B/IKJEFT1A are the TSO/E terminal monitor
	// program; without recognising them, a `PGM=IKJEFT01` step with a
	// `CALL 'lib(PAYROLL)'` control card hides the real JCL→COBOL/DB2 edge.
	utilProgRe = regexp.MustCompile(`(?i)^(IKJEFT01|IKJEFT1B|IKJEFT1A)$`)

	// tsoCallParenRe matches a TSO `CALL 'dsn(MEMBER)'` control card — the
	// load module is the parenthesised member of a load library. Group 1:
	// member.
	tsoCallParenRe = regexp.MustCompile(`(?i)^\s*CALL\s+'?[^'(]*\(\s*([A-Za-z$#@][A-Za-z0-9$#@]*)\s*\)`)

	// tsoCallBareRe matches a TSO `CALL MEMBER` control card — the load
	// module named directly (resolved against the TSO search order / a
	// STEPLIB). Group 1: member.
	tsoCallBareRe = regexp.MustCompile(`(?i)^\s*CALL\s+'?([A-Za-z$#@][A-Za-z0-9$#@]*)'?\s*$`)
)

// Extract processes a JCL source file and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractJCL(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "jcl")
	extractor.TagEntitiesLanguage(out, "jcl")
	return out, nil
}

// logicalStmt is one JCL statement after continuation-line joining.
type logicalStmt struct {
	startLine int    // 1-indexed physical line where the statement begins
	text      string // joined statement text (`//NAME VERB operands`)
}

// joinStatements collapses JCL continuation lines into logical statements.
// A statement continues when its operand field ends with a trailing comma;
// the continuation line begins with `//` followed by blanks (the name/verb
// fields are blank on a continuation). Comment lines (`//*`) and the `/*`
// delimiter / `//` null statement are skipped. Instream data following
// `DD *` is not parsed for statements but is bounded by the next `//` card.
func joinStatements(src string) []logicalStmt {
	rawLines := strings.Split(src, "\n")
	var out []logicalStmt
	for i := 0; i < len(rawLines); i++ {
		line := strings.TrimRight(rawLines[i], "\r")
		// Only columns 1-72 are significant in fixed JCL; 73-80 are the
		// sequence area. Bound conservatively so sequence numbers never leak.
		if len(line) > 72 {
			line = line[:72]
		}
		trimmed := strings.TrimRight(line, " ")
		// Comment card: `//*`. Null statement: a bare `//`. Delimiter: `/*`.
		if strings.HasPrefix(trimmed, "//*") {
			continue
		}
		if trimmed == "//" || strings.HasPrefix(trimmed, "/*") {
			continue
		}
		if !strings.HasPrefix(trimmed, "//") {
			// Instream data or unrelated text — not a JCL statement card.
			continue
		}

		startLine := i + 1
		stmt := trimmed
		// Join continuation lines while the operand field ends with a comma.
		for endsWithContinuation(stmt) && i+1 < len(rawLines) {
			next := strings.TrimRight(rawLines[i+1], "\r")
			if len(next) > 72 {
				next = next[:72]
			}
			nextTrim := strings.TrimRight(next, " ")
			// A continuation card starts with `//` and blanks (no name/verb).
			if !strings.HasPrefix(nextTrim, "//") || strings.HasPrefix(nextTrim, "//*") {
				break
			}
			cont := strings.TrimSpace(strings.TrimPrefix(nextTrim, "//"))
			stmt = stmt + cont
			i++
		}
		out = append(out, logicalStmt{startLine: startLine, text: stmt})
	}
	return out
}

// endsWithContinuation reports whether a JCL statement's operand field ends
// with a trailing comma (the classic continuation signal).
func endsWithContinuation(stmt string) bool {
	s := strings.TrimRight(stmt, " ")
	return strings.HasSuffix(s, ",")
}

// ---------------------------------------------------------------------------
// Core extraction
// ---------------------------------------------------------------------------

func extractJCL(src, filePath string) []types.EntityRecord {
	stmts := joinStatements(src)
	rawLines := strings.Split(src, "\n")
	var entities []types.EntityRecord

	// Scope tracking. The current JOB owns its steps; an inline PROC (between
	// `<name> PROC` and `PEND`) temporarily owns its steps instead.
	jobIdx := -1
	procIdx := -1 // index of the open inline PROC, -1 when none
	currentStepIdx := -1
	stepSeq := 0 // disambiguates anonymous steps for stable names

	addContains := func(ownerIdx int, ref string, child string) {
		if ownerIdx < 0 || ownerIdx >= len(entities) {
			return
		}
		entities[ownerIdx].Relationships = append(entities[ownerIdx].Relationships,
			types.RelationshipRecord{ToID: ref, Kind: "CONTAINS",
				Properties: map[string]string{"child": child}})
	}

	// ownerForStep returns the index that should CONTAIN the next step: the
	// open inline PROC if any, else the JOB.
	ownerForStep := func() int {
		if procIdx >= 0 {
			return procIdx
		}
		return jobIdx
	}

	for _, st := range stmts {
		m := stmtRe.FindStringSubmatch(st.text)
		if m == nil {
			continue
		}
		name := strings.ToUpper(m[1])
		verb := strings.ToUpper(m[2])
		operands := m[3]

		// Operand-only cards (no name field) — e.g. `//  INCLUDE MEMBER=X`,
		// `//  SET A=B`, `//  JCLLIB ORDER=(...)`. stmtRe greedily binds the
		// operation keyword to the name group, leaving the first operand token
		// as the "verb". When the name field holds a known statement keyword,
		// re-shift: the name IS the verb and there is no statement name.
		if isStatementKeyword(name) && !isStatementKeyword(verb) {
			operands = strings.TrimSpace(m[2] + m[3])
			verb = name
			name = ""
		}

		switch verb {
		case "JOB":
			jobIdx = len(entities)
			procIdx = -1
			currentStepIdx = -1
			entities = append(entities, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "job",
				SourceFile: filePath,
				Language:   "jcl",
				StartLine:  st.startLine,
				EndLine:    st.startLine,
				Signature:  truncSig(st.text),
			})

		case "PROC":
			// An inline PROC definition: `//NAME PROC ...`. A named PROC opens
			// a procedure scope closed by PEND. (A `// PROC` with no name on a
			// cataloged-proc member is rare in submitted JCL; we require a name.)
			if name == "" {
				continue
			}
			procIdx = len(entities)
			currentStepIdx = -1
			entities = append(entities, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "proc",
				SourceFile: filePath,
				Language:   "jcl",
				StartLine:  st.startLine,
				EndLine:    st.startLine,
				Signature:  truncSig(st.text),
			})

		case "PEND":
			// Close the inline PROC scope.
			if procIdx >= 0 && st.startLine > entities[procIdx].EndLine {
				entities[procIdx].EndLine = st.startLine
			}
			procIdx = -1
			currentStepIdx = -1

		case "EXEC":
			stepSeq++
			stepName := name
			if stepName == "" {
				stepName = "STEP" + strconv.Itoa(stepSeq)
			}
			currentStepIdx = len(entities)
			step := types.EntityRecord{
				Name:       stepName,
				Kind:       "SCOPE.Operation",
				Subtype:    "step",
				SourceFile: filePath,
				Language:   "jcl",
				StartLine:  st.startLine,
				EndLine:    st.startLine,
				Signature:  truncSig(st.text),
				Properties: map[string]string{},
			}

			// EXEC PGM=<program> — the cross-language bridge edge to COBOL.
			if pm := execPgmRe.FindStringSubmatch(operands); pm != nil {
				prog := strings.ToUpper(pm[1])
				step.Properties["pgm"] = prog
				step.Relationships = append(step.Relationships, types.RelationshipRecord{
					ToID: prog,
					Kind: "CALLS",
					Properties: map[string]string{
						"line":           strconv.Itoa(st.startLine),
						"via":            "EXEC PGM",
						"external":       "true",
						"cross_language": "cobol",
					},
				})
				// A z/OS "shell" utility (the TSO terminal monitor IKJEFTxx)
				// runs a SECOND program named on its SYSTSIN instream control
				// cards. Recover that indirect JCL→program edge by scanning the
				// step's instream block for `CALL <member>` cards.
				if utilProgRe.MatchString(prog) {
					for _, callee := range tsoCalledPrograms(rawLines, st.startLine) {
						step.Properties["tso_call"] = callee
						step.Relationships = append(step.Relationships, types.RelationshipRecord{
							ToID: callee,
							Kind: "CALLS",
							Properties: map[string]string{
								"line":           strconv.Itoa(st.startLine),
								"via":            "TSO CALL",
								"external":       "true",
								"cross_language": "cobol",
								"host_program":   prog,
							},
						})
					}
				}
			} else if pr := execProcRe.FindStringSubmatch(operands); pr != nil {
				// EXEC PROC=<proc> — procedure invocation.
				proc := strings.ToUpper(pr[1])
				step.Properties["proc"] = proc
				step.Relationships = append(step.Relationships, types.RelationshipRecord{
					ToID: proc,
					Kind: "CALLS",
					Properties: map[string]string{
						"line": strconv.Itoa(st.startLine),
						"via":  "EXEC PROC",
					},
				})
			} else if positional := firstPositionalProc(operands); positional != "" {
				// EXEC <proc> — positional procedure name (PROC= keyword
				// omitted, the common shorthand for invoking a cataloged proc).
				step.Properties["proc"] = positional
				step.Relationships = append(step.Relationships, types.RelationshipRecord{
					ToID: positional,
					Kind: "CALLS",
					Properties: map[string]string{
						"line": strconv.Itoa(st.startLine),
						"via":  "EXEC PROC",
					},
				})
			}

			entities = append(entities, step)
			// CONTAINS: job/proc → step.
			ref := extractor.BuildOperationStructuralRef("jcl", filePath, stepName)
			addContains(ownerForStep(), ref, stepName)

		case "DD":
			// A DD statement binds a dataset to the current step. Only DSN-
			// bearing DDs name a real dataset (SYSOUT/DUMMY/instream `*` DDs
			// have no DSN and are skipped for dataset-entity emission).
			dm := ddDsnRe.FindStringSubmatch(operands)
			if dm == nil {
				continue
			}
			dsn := strings.ToUpper(strings.TrimRight(dm[1], "."))
			disp := ""
			if dispM := ddDispRe.FindStringSubmatch(operands); dispM != nil {
				disp = strings.ToUpper(dispM[1])
			}
			dsIdx := len(entities)
			entities = append(entities, types.EntityRecord{
				Name:       dsn,
				Kind:       "SCOPE.Datastore",
				Subtype:    "dataset",
				SourceFile: filePath,
				Language:   "jcl",
				StartLine:  st.startLine,
				EndLine:    st.startLine,
				Signature:  truncSig(st.text),
				Properties: map[string]string{"dsn": dsn, "disp": disp},
			})
			// Attribute the dataset access to the current step. NEW/MOD =
			// write; OLD/SHR/default = read. A dataset is also CONTAINS-ed by
			// the step so it isn't an orphan node.
			if currentStepIdx >= 0 {
				kind := string(types.RelationshipKindReadsFrom)
				if disp == "NEW" || disp == "MOD" {
					kind = string(types.RelationshipKindWritesTo)
				}
				entities[currentStepIdx].Relationships = append(entities[currentStepIdx].Relationships,
					types.RelationshipRecord{ToID: dsn, Kind: kind,
						Properties: map[string]string{"dataset": dsn, "disp": disp}})
			}
			// Extend the enclosing step's EndLine.
			if currentStepIdx >= 0 && st.startLine > entities[currentStepIdx].EndLine {
				entities[currentStepIdx].EndLine = st.startLine
			}
			_ = dsIdx

		case "INCLUDE":
			// `//  INCLUDE MEMBER=<name>` textually splices a PROCLIB/JCLLIB
			// member into the job stream — a real cross-file dependency. Emit
			// an IMPORTS edge whose bare ToID is the member name, which the
			// by-name resolver binds to that member's own JCL/proc entity
			// (mirrors the COBOL COPY → copybook include edge). Attribute it to
			// the enclosing JOB/PROC scope so it is not an orphan.
			if im := includeMemberRe.FindStringSubmatch(operands); im != nil {
				member := strings.ToUpper(im[1])
				ownerIdx := jobIdx
				if procIdx >= 0 {
					ownerIdx = procIdx
				}
				if ownerIdx >= 0 && ownerIdx < len(entities) {
					entities[ownerIdx].Relationships = append(entities[ownerIdx].Relationships,
						types.RelationshipRecord{
							ToID: member,
							Kind: string(types.RelationshipKindImports),
							Properties: map[string]string{
								"line":        strconv.Itoa(st.startLine),
								"import_kind": "include",
								"member":      member,
							},
						})
				}
			}
		}

		// Extend the JOB's EndLine to the furthest statement seen.
		if jobIdx >= 0 && st.startLine > entities[jobIdx].EndLine {
			entities[jobIdx].EndLine = st.startLine
		}
	}

	return entities
}

// firstPositionalProc returns a positional procedure name on an EXEC
// statement (e.g. `EXEC MYPROC,PARM=...`) — the operand before the first
// comma or `=` when it is a bare identifier and not a recognised keyword.
// Returns "" when the first operand is a keyword form (PGM=/PROC=/etc.).
func firstPositionalProc(operands string) string {
	s := strings.TrimSpace(operands)
	if s == "" {
		return ""
	}
	// Take the leading token up to the first comma or whitespace.
	end := len(s)
	for i := 0; i < len(s); i++ {
		if s[i] == ',' || s[i] == ' ' || s[i] == '\t' {
			end = i
			break
		}
	}
	tok := s[:end]
	// A keyword operand contains `=`; positional procs do not.
	if strings.ContainsRune(tok, '=') {
		return ""
	}
	// Validate the identifier shape (1-8 alphanumerics, mainframe charset).
	if !procNameRe.MatchString(tok) {
		return ""
	}
	return strings.ToUpper(tok)
}

var procNameRe = regexp.MustCompile(`^[A-Za-z$#@][A-Za-z0-9$#@]*$`)

// statementKeywords is the set of JCL operation verbs that may appear in an
// operand-only (nameless) card, where stmtRe would otherwise mis-bind the
// keyword to the name field. Used to re-shift name→verb for such cards.
var statementKeywords = map[string]bool{
	"JOB": true, "EXEC": true, "DD": true, "PROC": true, "PEND": true,
	"INCLUDE": true, "SET": true, "JCLLIB": true, "OUTPUT": true,
	"IF": true, "ELSE": true, "ENDIF": true,
}

func isStatementKeyword(s string) bool { return statementKeywords[s] }

// sysInDDRe matches a SYSTSIN/SYSIN DD card that introduces an instream
// control-card stream (`//SYSTSIN DD *` / `DD DATA`). These streams carry
// the indirect program names a shell utility runs.
var sysInDDRe = regexp.MustCompile(`(?i)^//\s*(SYSTSIN|SYSIN)\s+DD\b.*\*`)

// tsoCalledPrograms scans the instream SYSTSIN/SYSIN control cards belonging
// to the step that begins at startLine (1-indexed) and returns the load
// modules named by `CALL <member>` cards. The scan is bounded by the next
// statement card that starts a new step/job (a `//NAME EXEC|JOB` card),
// honouring the `/*` instream delimiter. This recovers the JCL→program edge
// that a "shell" utility (the TSO terminal monitor IKJEFTxx) hides behind its
// control cards — joinStatements drops these non-`//` data lines outright.
func tsoCalledPrograms(rawLines []string, startLine int) []string {
	var out []string
	seen := map[string]bool{}
	inStream := false
	for i := startLine; i < len(rawLines); i++ { // startLine is 1-indexed; skip the EXEC card itself
		line := strings.TrimRight(rawLines[i], "\r")
		if len(line) > 72 {
			line = line[:72]
		}
		trimmed := strings.TrimRight(line, " ")
		// `/*` ends the current instream block.
		if strings.HasPrefix(trimmed, "/*") {
			inStream = false
			continue
		}
		if strings.HasPrefix(trimmed, "//*") {
			continue // comment card
		}
		if strings.HasPrefix(trimmed, "//") {
			// A statement card. If it begins a new EXEC step or a new JOB, the
			// current step's instream is over — stop scanning.
			if m := stmtRe.FindStringSubmatch(trimmed); m != nil {
				v := strings.ToUpper(m[2])
				if v == "EXEC" || v == "JOB" {
					break
				}
			}
			inStream = sysInDDRe.MatchString(trimmed)
			continue
		}
		// A non-`//` line: instream data. Only parse it as a control card when
		// we are inside a SYSTSIN/SYSIN block.
		if !inStream {
			continue
		}
		var member string
		if cm := tsoCallParenRe.FindStringSubmatch(trimmed); cm != nil {
			member = strings.ToUpper(cm[1])
		} else if cm := tsoCallBareRe.FindStringSubmatch(trimmed); cm != nil {
			member = strings.ToUpper(cm[1])
		}
		if member != "" && !seen[member] {
			seen[member] = true
			out = append(out, member)
		}
	}
	return out
}

// truncSig trims a statement to a compact signature, collapsing runs of
// blanks and bounding the length so long operand lists don't bloat the graph.
func truncSig(stmt string) string {
	s := strings.Join(strings.Fields(stmt), " ")
	const max = 160
	if len(s) > max {
		return s[:max]
	}
	return s
}
