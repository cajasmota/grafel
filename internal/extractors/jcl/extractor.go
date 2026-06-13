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
//   - CALLS    — DSN RUN PROGRAM     (DB2 batch step → the application program
//     named on a `DSN SYSTEM(ssid) RUN PROGRAM(p) PLAN(pl)` control card;
//     via="DSN RUN PROGRAM", db2_plan/db2_system properties)
//   - READS_FROM / WRITES_TO — IDCAMS REPRO/IMPORT/EXPORT IN/OUTDATASET(dsn)
//     (the dataset I/O a `PGM=IDCAMS` step performs via its SYSIN control
//     cards; via="IDCAMS")
//   - CONTAINS — job → its steps; proc → its steps
//   - READS_FROM / WRITES_TO — step → dataset (DD DISP governs direction)
//   - IMPORTS  — INCLUDE MEMBER=<name> (job/proc → spliced PROCLIB/JCLLIB
//     member; import_kind="include", a real cross-file dependency)
//
// Symbolic-parameter substitution (#5042): a `// SET SYM=VAL` card and a PROC
// statement's `SYMBOL=default` operands seed a job-level symbol table; an
// `EXEC proc,SYMBOL=override` overrides them for that invocation. Before any
// edge is emitted, `&SYM` / `&HLQ..FILE` tokens in PGM=/PROC=/DSN= operands are
// resolved against the table so the CALLS/READS_FROM/WRITES_TO target carries
// the substituted program / dataset identity (the literal `&VAR` token would
// otherwise break COBOL by-name binding and dataset identity). The resolved
// operand keeps a `symbolic_source="<original>"` property when a substitution
// fired, and unresolved `&VAR` tokens are left intact (and flagged
// `symbolic_unresolved="true"`). `// JCLLIB ORDER=(...)` is recorded as a
// `jcllib_order` property / proc-resolution search-order hint on the JOB.
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

	// execPgmRe matches the PGM= operand of an EXEC statement. The operand may
	// be a literal program name or an unresolved `&SYM` symbolic reference
	// (resolved later against the symbol table). Group 1: program token.
	execPgmRe = regexp.MustCompile(`(?i)\bPGM\s*=\s*(&?[A-Za-z$#@][A-Za-z0-9$#@.]*)`)

	// execProcRe matches the explicit PROC= operand of an EXEC statement. The
	// operand may be a `&SYM` symbolic reference. Group 1: procedure token.
	execProcRe = regexp.MustCompile(`(?i)\bPROC\s*=\s*(&?[A-Za-z$#@][A-Za-z0-9$#@.]*)`)

	// ddDsnRe matches the DSN= / DSNAME= operand of a DD statement.
	// Group 1: dataset name (may be a qualified name like PROD.PAYROLL.MASTER
	// or a temp name like &&TEMP).
	ddDsnRe = regexp.MustCompile(`(?i)\bDSN(?:AME)?\s*=\s*([&A-Za-z0-9$#@.()-]+)`)

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
	// IDCAMS (the access-method services utility) and DSNUTILB (the DB2
	// stand-alone utility invoker) similarly hide dataset I/O and program/
	// plan invocation behind their SYSIN control cards.
	utilProgRe = regexp.MustCompile(`(?i)^(IKJEFT01|IKJEFT1B|IKJEFT1A|IDCAMS|DSNUTILB)$`)

	// tsoCallParenRe matches a TSO `CALL 'dsn(MEMBER)'` control card — the
	// load module is the parenthesised member of a load library. Group 1:
	// member.
	tsoCallParenRe = regexp.MustCompile(`(?i)^\s*CALL\s+'?[^'(]*\(\s*([A-Za-z$#@][A-Za-z0-9$#@]*)\s*\)`)

	// tsoCallBareRe matches a TSO `CALL MEMBER` control card — the load
	// module named directly (resolved against the TSO search order / a
	// STEPLIB). Group 1: member.
	tsoCallBareRe = regexp.MustCompile(`(?i)^\s*CALL\s+'?([A-Za-z$#@][A-Za-z0-9$#@]*)'?\s*$`)

	// dsnRunProgramRe matches a DB2 `DSN ... RUN PROGRAM(name) ...` control
	// card (run under IKJEFT01 as the TSO command processor, or under
	// DSNUTILB). It names the application program a DB2 batch step runs —
	// the JCL→COBOL/DB2 edge a `PGM=IKJEFT01` + `DSN SYSTEM(...)` step hides.
	// Group 1: program name.
	dsnRunProgramRe = regexp.MustCompile(`(?i)\bRUN\s+PROGRAM\s*\(\s*([A-Za-z$#@][A-Za-z0-9$#@]*)\s*\)`)

	// dsnPlanRe matches the `PLAN(name)` operand of a DB2 RUN command. The
	// plan is the bound DB2 access package the program executes under.
	// Group 1: plan name.
	dsnPlanRe = regexp.MustCompile(`(?i)\bPLAN\s*\(\s*([A-Za-z$#@][A-Za-z0-9$#@]*)\s*\)`)

	// dsnSystemRe matches the DB2 subsystem id on a `DSN SYSTEM(ssid)` card.
	// Group 1: subsystem id.
	dsnSystemRe = regexp.MustCompile(`(?i)^\s*DSN\s+SYSTEM\s*\(\s*([A-Za-z0-9$#@]+)\s*\)`)

	// idcamsReproInRe / idcamsReproOutRe match the source/target datasets of
	// an IDCAMS `REPRO INFILE(dd)/INDATASET(dsn) OUTFILE(dd)/OUTDATASET(dsn)`
	// (or `IMPORT`/`EXPORT`) control card — dataset I/O the utility performs
	// that the DD cards alone do not attribute as a read or a write. We match
	// the IN*/OUT*-DATASET forms (a literal dataset name; the INFILE/OUTFILE
	// DD-reference forms already surface as ordinary DD entities). Group 1:
	// dataset name.
	idcamsReproInRe  = regexp.MustCompile(`(?i)\b(?:IN|FROM)DATASET\s*\(\s*'?([A-Za-z0-9$#@.()-]+?)'?\s*[,)]`)
	idcamsReproOutRe = regexp.MustCompile(`(?i)\b(?:OUT|TO)DATASET\s*\(\s*'?([A-Za-z0-9$#@.()-]+?)'?\s*[,)]`)

	// symbolRefRe matches one `&SYM` symbolic-parameter reference. A trailing
	// period is the JCL concatenation terminator (`&HLQ..FILE` → value of HLQ,
	// then a literal `.`, then `FILE`); it is consumed as part of the token so
	// the remaining text concatenates correctly. Group 1: symbol name (1-8
	// national/alphanumeric chars, leading alpha/national); the optional
	// trailing `.` is group 2 (the concatenation period, dropped on substitute).
	symbolRefRe = regexp.MustCompile(`&([A-Za-z$#@][A-Za-z0-9$#@]{0,7})(\.?)`)

	// setOperandsRe / kvPairRe split a `// SET`, PROC-default, or EXEC-override
	// operand list into NAME=VALUE pairs. JCL SET allows comma-separated
	// assignments on one card (`// SET HLQ=PROD,ENV=PRD`). VALUE may be quoted.
	kvPairRe = regexp.MustCompile(`([A-Za-z$#@][A-Za-z0-9$#@]{0,7})\s*=\s*('[^']*'|[^,\s]+)`)

	// jcllibOrderRe captures the parenthesised library list of a
	// `// JCLLIB ORDER=(LIB1,LIB2)` card (or the single unparenthesised form).
	// Group 1: the raw order list (without the surrounding parens).
	jcllibOrderRe = regexp.MustCompile(`(?i)\bORDER\s*=\s*\(([^)]*)\)`)
	jcllibOneRe   = regexp.MustCompile(`(?i)\bORDER\s*=\s*([A-Za-z0-9$#@.]+)`)
)

// symbolTable holds JCL symbolic-parameter values seeded by `// SET` cards and
// PROC SYMBOL= defaults / EXEC SYMBOL= overrides. Lookups are case-insensitive
// (keys are stored upper-cased).
type symbolTable map[string]string

// set records a symbol value, upper-casing the name and stripping surrounding
// quotes from the value (JCL quotes a value containing special characters).
func (t symbolTable) set(name, value string) {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		value = value[1 : len(value)-1]
	}
	t[name] = value
}

// learnPairs parses a comma/space-separated NAME=VALUE operand list (a SET
// card or an EXEC override list) into the table, overwriting existing values.
func (t symbolTable) learnPairs(operands string) {
	for _, m := range kvPairRe.FindAllStringSubmatch(operands, -1) {
		t.set(m[1], m[2])
	}
}

// learnDefaults parses a PROC statement's SYMBOL=default operand list with
// fill-if-absent semantics: a default only applies when the symbol was not
// already supplied by an invoking EXEC override or a `// SET` card (the JCL
// precedence — EXEC override / SET beats the PROC default).
func (t symbolTable) learnDefaults(operands string) {
	for _, m := range kvPairRe.FindAllStringSubmatch(operands, -1) {
		name := strings.ToUpper(strings.TrimSpace(m[1]))
		if _, exists := t[name]; exists {
			continue
		}
		t.set(m[1], m[2])
	}
}

// substitute resolves `&SYM` symbolic references in a PGM=/PROC=/DSN= operand
// value against the table. It returns the resolved string, whether any
// substitution fired, and whether any `&VAR` reference remained unresolved.
// Unknown symbols are left as the literal `&VAR` token (with its concatenation
// period preserved) so downstream binding can still see the unresolved name.
func (t symbolTable) substitute(s string) (out string, changed, unresolved bool) {
	out = symbolRefRe.ReplaceAllStringFunc(s, func(tok string) string {
		m := symbolRefRe.FindStringSubmatch(tok)
		key := strings.ToUpper(m[1])
		if v, ok := t[key]; ok {
			changed = true
			return v // the concatenation period (m[2]) is consumed/dropped
		}
		unresolved = true
		return tok // leave the literal &VAR (and its period) intact
	})
	return out, changed, unresolved
}

// hasSymbolRef reports whether s contains an `&SYM` symbolic reference (used to
// decide whether to attempt substitution at all).
func hasSymbolRef(s string) bool { return strings.ContainsRune(s, '&') }

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

	// Symbolic-parameter table (#5042). Seeded job-wide by `// SET` cards and
	// by a PROC statement's SYMBOL= default operands; an EXEC proc invocation's
	// SYMBOL= operands override for that step. `&SYM` references in PGM=/PROC=/
	// DSN= operands are resolved against this table before edge emission so the
	// edge target carries the substituted program/dataset identity rather than
	// the literal `&VAR` token.
	symbols := symbolTable{}

	// applySymbols resolves &SYM references in an operand value and records the
	// outcome on the given step's Properties (the original literal when a
	// substitution fired, an unresolved flag otherwise). value is the matched
	// operand; key is the Properties prefix ("pgm"/"proc"/"dataset").
	applySymbols := func(value string, props map[string]string, key string) string {
		if !hasSymbolRef(value) {
			return value
		}
		resolved, changed, unresolved := symbols.substitute(value)
		if changed && props != nil {
			props["symbolic_source"] = value
		}
		if unresolved && props != nil {
			props["symbolic_unresolved"] = "true"
		}
		return resolved
	}

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
			// PROC SYMBOL=default operands seed job-wide symbol defaults so a
			// `DSN=&HLQ..FILE` inside the proc body resolves even when the EXEC
			// did not override the symbol. Fill-if-absent: an EXEC override (or
			// `// SET`) already in the table wins over the PROC default.
			symbols.learnDefaults(operands)
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

			// An EXEC proc,SYMBOL=override invocation overrides the symbol table
			// for the proc body that follows. PGM/PROC keyword operands are
			// harmlessly re-learned but are never referenced as &symbols.
			symbols.learnPairs(operands)

			// EXEC PGM=<program> — the cross-language bridge edge to COBOL.
			if pm := execPgmRe.FindStringSubmatch(operands); pm != nil {
				prog := strings.ToUpper(applySymbols(pm[1], step.Properties, "pgm"))
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
				// A z/OS "shell" utility (the TSO terminal monitor IKJEFTxx,
				// IDCAMS, or DSNUTILB) does its real work via a SECOND program
				// and/or dataset I/O named on its SYSIN/SYSTSIN instream control
				// cards. Recover those indirect edges by scanning the step's
				// instream block under a per-utility control-card grammar.
				if utilProgRe.MatchString(prog) {
					scan := scanControlCards(rawLines, st.startLine)
					for _, callee := range scan.calls {
						step.Properties["sysin_call"] = callee.member
						step.Relationships = append(step.Relationships, types.RelationshipRecord{
							ToID: callee.member,
							Kind: "CALLS",
							Properties: callsProps(st.startLine, callee, prog),
						})
					}
					// IDCAMS REPRO/IMPORT/EXPORT dataset I/O — attribute the
					// operated DSNs to this step as reads/writes (the fs_effect
					// the bare DD cards do not express). Emit the dataset entity
					// too so the edge target is not an orphan.
					for _, ds := range scan.datasets {
						kind := string(types.RelationshipKindReadsFrom)
						if ds.write {
							kind = string(types.RelationshipKindWritesTo)
						}
						entities = append(entities, types.EntityRecord{
							Name:       ds.dsn,
							Kind:       "SCOPE.Datastore",
							Subtype:    "dataset",
							SourceFile: filePath,
							Language:   "jcl",
							StartLine:  st.startLine,
							EndLine:    st.startLine,
							Signature:  "IDCAMS " + ds.dsn,
							Properties: map[string]string{"dsn": ds.dsn, "via": "IDCAMS"},
						})
						step.Relationships = append(step.Relationships, types.RelationshipRecord{
							ToID: ds.dsn,
							Kind: kind,
							Properties: map[string]string{
								"dataset": ds.dsn,
								"via":     "IDCAMS",
							},
						})
					}
				}
			} else if pr := execProcRe.FindStringSubmatch(operands); pr != nil {
				// EXEC PROC=<proc> — procedure invocation.
				proc := strings.ToUpper(applySymbols(pr[1], step.Properties, "proc"))
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
			// Resolve &SYM symbolic references in the DSN operand before the
			// dataset identity is fixed (`DSN=&HLQ..FILE` → PROD.FILE). Record
			// the outcome on the dataset entity Properties so a substitution /
			// unresolved-symbol is visible on the node.
			dsnProps := map[string]string{}
			dsn := strings.ToUpper(strings.TrimRight(applySymbols(dm[1], dsnProps, "dataset"), "."))
			disp := ""
			if dispM := ddDispRe.FindStringSubmatch(operands); dispM != nil {
				disp = strings.ToUpper(dispM[1])
			}
			dsIdx := len(entities)
			dsProps := map[string]string{"dsn": dsn, "disp": disp}
			for k, v := range dsnProps { // symbolic_source / symbolic_unresolved
				dsProps[k] = v
			}
			entities = append(entities, types.EntityRecord{
				Name:       dsn,
				Kind:       "SCOPE.Datastore",
				Subtype:    "dataset",
				SourceFile: filePath,
				Language:   "jcl",
				StartLine:  st.startLine,
				EndLine:    st.startLine,
				Signature:  truncSig(st.text),
				Properties: dsProps,
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

		case "SET":
			// `// SET SYM=VAL[,SYM2=VAL2]` — job-level symbolic-parameter
			// assignment. Seeds the symbol table consulted when resolving
			// `&SYM` references in subsequent PGM=/PROC=/DSN= operands.
			symbols.learnPairs(operands)

		case "JCLLIB":
			// `// JCLLIB ORDER=(LIB1,LIB2)` declares the proc-resolution search
			// order. Record it as a property / search-order hint on the JOB so
			// the cataloged-proc-resolution order is visible on the graph.
			if jobIdx >= 0 && jobIdx < len(entities) {
				order := ""
				if om := jcllibOrderRe.FindStringSubmatch(operands); om != nil {
					order = om[1]
				} else if om := jcllibOneRe.FindStringSubmatch(operands); om != nil {
					order = om[1]
				}
				if order != "" {
					// Normalise to a comma-joined, blank-stripped library list.
					var libs []string
					for _, lib := range strings.Split(order, ",") {
						if l := strings.TrimSpace(lib); l != "" {
							libs = append(libs, strings.ToUpper(l))
						}
					}
					if len(libs) > 0 {
						if entities[jobIdx].Properties == nil {
							entities[jobIdx].Properties = map[string]string{}
						}
						entities[jobIdx].Properties["jcllib_order"] = strings.Join(libs, ",")
					}
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
// the indirect program names and dataset operations a shell utility runs.
var sysInDDRe = regexp.MustCompile(`(?i)^//\s*(SYSTSIN|SYSIN)\s+DD\b.*\*`)

// controlCall is one indirect program invocation recovered from an instream
// control card (a TSO `CALL`, or a DB2 `DSN ... RUN PROGRAM(...)`).
type controlCall struct {
	member string // the invoked load module / DB2 application program
	via    string // "TSO CALL" | "DSN RUN PROGRAM"
	plan   string // DB2 plan name (RUN PROGRAM only); "" otherwise
	system string // DB2 subsystem id from the enclosing DSN SYSTEM(...); "" otherwise
}

// controlDataset is one dataset I/O recovered from an IDCAMS REPRO/IMPORT/
// EXPORT control card. write distinguishes the OUT/TO target from the IN/FROM
// source.
type controlDataset struct {
	dsn   string
	write bool
}

// controlScan is the structured result of scanning one step's instream
// control-card stream under the per-utility grammar.
type controlScan struct {
	calls    []controlCall
	datasets []controlDataset
}

// callsProps builds the CALLS edge properties for a recovered control-card
// invocation, carrying the via verb, the DB2 plan/subsystem when present, and
// the host shell-utility program.
func callsProps(line int, c controlCall, host string) map[string]string {
	p := map[string]string{
		"line":           strconv.Itoa(line),
		"via":            c.via,
		"external":       "true",
		"cross_language": "cobol",
		"host_program":   host,
	}
	if c.plan != "" {
		p["db2_plan"] = c.plan
	}
	if c.system != "" {
		p["db2_system"] = c.system
	}
	return p
}

// scanControlCards scans the instream SYSIN/SYSTSIN control cards belonging to
// the step that begins at startLine (1-indexed) under a per-utility grammar
// and returns the indirect program invocations and dataset operations they
// name. The scan is bounded by the next statement card that starts a new
// step/job (a `//NAME EXEC|JOB` card), honouring the `/*` instream delimiter.
// This recovers the JCL→program/dataset edges that a "shell" utility (the TSO
// terminal monitor IKJEFTxx, the DB2 utility invoker DSNUTILB, or IDCAMS)
// hides behind its control cards — joinStatements drops these non-`//` data
// lines outright. Grammar covered:
//   - TSO        CALL 'lib(MEMBER)' / CALL MEMBER       → program call
//   - DB2        DSN SYSTEM(ssid) ... RUN PROGRAM(p) PLAN(pl) → program call
//   - IDCAMS     REPRO/IMPORT/EXPORT IN/OUTDATASET(dsn) → dataset read/write
func scanControlCards(rawLines []string, startLine int) controlScan {
	var scan controlScan
	seenCall := map[string]bool{}
	seenDS := map[string]bool{}
	inStream := false
	system := "" // last seen DSN SYSTEM(ssid), threaded onto RUN PROGRAM cards
	addCall := func(c controlCall) {
		if c.member == "" || seenCall[c.member] {
			return
		}
		seenCall[c.member] = true
		scan.calls = append(scan.calls, c)
	}
	addDS := func(dsn string, write bool) {
		dsn = strings.ToUpper(strings.TrimRight(dsn, "."))
		key := dsn
		if write {
			key += "\x00W"
		}
		if dsn == "" || seenDS[key] {
			return
		}
		seenDS[key] = true
		scan.datasets = append(scan.datasets, controlDataset{dsn: dsn, write: write})
	}
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
		// we are inside a SYSIN/SYSTSIN block.
		if !inStream {
			continue
		}
		// DB2 subsystem context: `DSN SYSTEM(ssid)` precedes its RUN cards.
		if sm := dsnSystemRe.FindStringSubmatch(trimmed); sm != nil {
			system = strings.ToUpper(sm[1])
		}
		// DB2 RUN PROGRAM(name) [PLAN(name)] — a JCL→DB2/COBOL program edge.
		if rm := dsnRunProgramRe.FindStringSubmatch(trimmed); rm != nil {
			c := controlCall{member: strings.ToUpper(rm[1]), via: "DSN RUN PROGRAM", system: system}
			if pm := dsnPlanRe.FindStringSubmatch(trimmed); pm != nil {
				c.plan = strings.ToUpper(pm[1])
			}
			addCall(c)
			continue
		}
		// TSO CALL 'lib(MEMBER)' / CALL MEMBER — a program call.
		if cm := tsoCallParenRe.FindStringSubmatch(trimmed); cm != nil {
			addCall(controlCall{member: strings.ToUpper(cm[1]), via: "TSO CALL"})
			continue
		}
		if cm := tsoCallBareRe.FindStringSubmatch(trimmed); cm != nil {
			addCall(controlCall{member: strings.ToUpper(cm[1]), via: "TSO CALL"})
			continue
		}
		// IDCAMS REPRO/IMPORT/EXPORT IN/OUTDATASET(dsn) — dataset read/write.
		if im := idcamsReproInRe.FindStringSubmatch(trimmed); im != nil {
			addDS(im[1], false)
		}
		if om := idcamsReproOutRe.FindStringSubmatch(trimmed); om != nil {
			addDS(om[1], true)
		}
	}
	return scan
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
