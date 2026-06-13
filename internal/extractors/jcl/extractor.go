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
	ddDsnRe = regexp.MustCompile(`(?i)\bDSN(?:AME)?\s*=\s*([&A-Za-z0-9$#@.()+-]+)`)

	// gdgGenRe matches a GDG relative-generation suffix on a dataset name —
	// `PROD.GDG(+1)` (next generation, a write), `(0)` (current), `(-1)`
	// (prior generation, a read of an existing gen). Group 1: the GDG base
	// (generation-data-group base name); group 2: the signed relative
	// generation number (e.g. +1, 0, -1). A bare member that is not a signed
	// integer (a PDS member, see pdsMemberRe) does not match.
	gdgGenRe = regexp.MustCompile(`^(.+)\(([+-]?\d+)\)$`)

	// pdsMemberRe matches a partitioned-data-set member reference —
	// `DSN=PROD.LOADLIB(PAYROLL)` names the PAYROLL member of the PROD.LOADLIB
	// library. Group 1: the library (PDS) name; group 2: the member name (1-8
	// national/alphanumeric chars). A signed-integer parenthetical is a GDG
	// generation (gdgGenRe), not a member.
	pdsMemberRe = regexp.MustCompile(`^(.+)\(([A-Za-z$#@][A-Za-z0-9$#@]{0,7})\)$`)

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

	// condRe matches a `COND=(code,op[,stepname])` operand on an EXEC card —
	// the classic JCL step-skip predicate: the step is SKIPPED when the
	// relation `<code> <op> <prior-step return code>` is TRUE (e.g.
	// `COND=(4,LT,STEP1)` skips this step when 4 < STEP1's RC). Group 1: the
	// comparison code; group 2: the operator (LT/LE/EQ/NE/GE/GT); group 3: the
	// optional referenced step name (empty → tested against every prior step).
	condRe = regexp.MustCompile(`(?i)\bCOND\s*=\s*\(\s*(\d+)\s*,\s*(LT|LE|EQ|NE|GE|GT)\s*(?:,\s*([A-Za-z$#@][A-Za-z0-9$#@.]*)\s*)?\)`)

	// condOnlyRe matches the COND=EVEN / COND=ONLY abnormal-termination forms
	// (run this step EVEN if a prior step abended / ONLY if one did). Group 1:
	// the keyword.
	condOnlyRe = regexp.MustCompile(`(?i)\bCOND\s*=\s*(EVEN|ONLY)\b`)

	// ifCondRe matches the predicate of a `// IF (cond) THEN` control card.
	// Group 1: the raw condition text between the parentheses (e.g.
	// `STEP1.RC = 0`). The THEN keyword is optional in submitted JCL.
	ifCondRe = regexp.MustCompile(`(?i)^\s*\(?(.*?)\)?\s*(?:\bTHEN\b)?\s*$`)

	// restartRe matches the `RESTART=stepname` operand on a JOB card — the JOB
	// is re-submitted starting at the named step; all steps BEFORE it are
	// skipped. Group 1: the restart step (may be `proc.step` qualified).
	restartRe = regexp.MustCompile(`(?i)\bRESTART\s*=\s*([A-Za-z$#@][A-Za-z0-9$#@.]*)`)
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

	// #5043 concatenation tracking. A named DD opens a logical DD; nameless
	// `//  DD DSN=...` continuation cards extend it. currentDDName is the open
	// logical DD's ddname; ddConcatSeq counts the concatenants after the first.
	currentDDName := ""
	ddConcatSeq := 0

	// #5044 conditional-flow tracking. stepNames preserves EXEC step order so a
	// COND=/IF predicate can be wired to the prior step(s) and a RESTART= entry
	// point can skip the steps before it. The open `// IF (cond) THEN` block's
	// condition (ifCond) and branch (ifBranch: "then"/"else") tag the steps
	// declared inside it.
	var stepNames []string            // EXEC step names in declaration order
	stepRefByName := map[string]int{} // step name → entity index
	ifCond := ""                      // open IF predicate, "" when none
	ifBranch := ""                    // "then" / "else" inside an open IF
	restartStep := ""                 // RESTART= target step (upper), "" when none

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
			// #5044 RESTART=stepname: the job is re-submitted starting at the
			// named step; all earlier steps are skipped. Record the restart
			// point on the JOB and arm step-skip flagging for the steps below.
			jobProps := map[string]string{}
			if rm := restartRe.FindStringSubmatch(operands); rm != nil {
				restartStep = strings.ToUpper(rm[1])
				jobProps["restart"] = restartStep
			}
			job := types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "job",
				SourceFile: filePath,
				Language:   "jcl",
				StartLine:  st.startLine,
				EndLine:    st.startLine,
				Signature:  truncSig(st.text),
			}
			if len(jobProps) > 0 {
				job.Properties = jobProps
			}
			entities = append(entities, job)

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

			// A new step closes any open DD concatenation.
			currentDDName = ""
			ddConcatSeq = 0

			// An EXEC proc,SYMBOL=override invocation overrides the symbol table
			// for the proc body that follows. PGM/PROC keyword operands are
			// harmlessly re-learned but are never referenced as &symbols.
			symbols.learnPairs(operands)

			// #5044 conditional flow — COND= step-skip predicate on the EXEC.
			// `COND=(code,op[,step])` SKIPS this step when the relation holds
			// against a prior step's return code; COND=EVEN/ONLY are the
			// abnormal-termination forms. Record the predicate on the step and,
			// when a specific prior step is named (or the immediately-preceding
			// step is the implicit referent), emit a conditional PRECEDES edge
			// FROM the referenced step TO this one carrying the condition.
			if cm := condRe.FindStringSubmatch(operands); cm != nil {
				code := cm[1]
				op := strings.ToUpper(cm[2])
				refStep := strings.ToUpper(cm[3])
				step.Properties["cond"] = code + "," + op
				if refStep != "" {
					step.Properties["cond_step"] = refStep
				}
				condProps := map[string]string{
					"line":      strconv.Itoa(st.startLine),
					"flow":      "conditional",
					"cond_code": code,
					"cond_op":   op,
					"cond_kind": "COND",
					"to_step":   stepName,
				}
				// Wire FROM the named prior step (its RC is tested) to this step.
				if refStep != "" {
					if fromIdx, ok := stepRefByName[refStep]; ok {
						ref := extractor.BuildOperationStructuralRef("jcl", filePath, stepName)
						entities[fromIdx].Relationships = append(entities[fromIdx].Relationships,
							types.RelationshipRecord{
								ToID:       ref,
								Kind:       string(types.RelationshipKindPrecedes),
								Properties: condProps,
							})
					}
				} else if len(stepNames) > 0 {
					// No step named: tested against every prior step; attribute
					// the predicate to the immediately-preceding step's edge.
					prev := stepNames[len(stepNames)-1]
					if fromIdx, ok := stepRefByName[prev]; ok {
						condProps["cond_scope"] = "all_prior"
						ref := extractor.BuildOperationStructuralRef("jcl", filePath, stepName)
						entities[fromIdx].Relationships = append(entities[fromIdx].Relationships,
							types.RelationshipRecord{
								ToID:       ref,
								Kind:       string(types.RelationshipKindPrecedes),
								Properties: condProps,
							})
					}
				}
			} else if om := condOnlyRe.FindStringSubmatch(operands); om != nil {
				step.Properties["cond"] = strings.ToUpper(om[1])
			}

			// #5044 — a step declared inside an open `// IF (cond) THEN ... ELSE`
			// block carries the predicate + branch so its conditional grouping
			// is visible (the IF/ELSE/ENDIF cards themselves emit no entity).
			if ifCond != "" {
				step.Properties["if_cond"] = ifCond
				step.Properties["if_branch"] = ifBranch
			}

			// #5044 RESTART= step-skip: a JOB RESTART=stepX skips every step
			// before stepX. Flag steps preceding the restart point as skipped on
			// restart; flag the restart step itself as the entry point.
			if restartStep != "" {
				up := strings.ToUpper(stepName)
				if up == restartStep {
					step.Properties["restart_entry"] = "true"
				} else if !restartReached(stepNames, restartStep) {
					step.Properties["restart_skipped"] = "true"
				}
			}

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
							ToID:       callee.member,
							Kind:       "CALLS",
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

			stepRefByName[strings.ToUpper(stepName)] = len(entities)
			entities = append(entities, step)
			stepNames = append(stepNames, strings.ToUpper(stepName))
			// CONTAINS: job/proc → step.
			ref := extractor.BuildOperationStructuralRef("jcl", filePath, stepName)
			addContains(ownerForStep(), ref, stepName)

		case "DD":
			// A DD statement binds a dataset to the current step. Only DSN-
			// bearing DDs name a real dataset (SYSOUT/DUMMY/instream `*` DDs
			// have no DSN and are skipped for dataset-entity emission).
			//
			// #5043: a DD card may be the first card of a CONCATENATION — it is
			// followed by one or more NAMELESS `//  DD DSN=...` continuation
			// cards that add further datasets to the SAME logical DD (read in
			// sequence). We track the logical DD's ddname: a named DD opens a
			// new logical DD; a nameless DD card extends the most-recent one.
			// Every concatenant carries Properties["ddname"] (the logical DD)
			// and, beyond the first, concatenation_position / concatenated=true
			// so the multi-DSN membership is visible on the graph.
			if name != "" {
				currentDDName = name
				ddConcatSeq = 0
			} else if currentDDName != "" {
				ddConcatSeq++ // a nameless concatenation continuation card
			}
			dm := ddDsnRe.FindStringSubmatch(operands)
			if dm == nil {
				continue
			}
			// Resolve &SYM symbolic references in the DSN operand before the
			// dataset identity is fixed (`DSN=&HLQ..FILE` → PROD.FILE). Record
			// the outcome on the dataset entity Properties so a substitution /
			// unresolved-symbol is visible on the node.
			dsnProps := map[string]string{}
			rawDsn := strings.ToUpper(strings.TrimRight(applySymbols(dm[1], dsnProps, "dataset"), "."))
			disp := ""
			if dispM := ddDispRe.FindStringSubmatch(operands); dispM != nil {
				disp = strings.ToUpper(dispM[1])
			}

			// #5043: split GDG relative generations and PDS members so the
			// dataset identity carries the right granularity. A GDG `(+1)`
			// resolves to the base name (the generation-data group) with a
			// relative-generation property; `(+1)` also implies a write of a
			// freshly-created generation. A PDS `LIB(MEMBER)` is modelled as a
			// distinct library + member: the entity Name is the LIB(MEMBER)
			// granular identity, with library / member properties.
			dsn := rawDsn
			gran := datasetGranularity(rawDsn)
			for k, v := range gran.props {
				dsnProps[k] = v
			}
			if gran.name != "" {
				dsn = gran.name
			}

			dsProps := map[string]string{"dsn": dsn, "disp": disp}
			if currentDDName != "" {
				dsProps["ddname"] = currentDDName
			}
			if ddConcatSeq > 0 {
				dsProps["concatenated"] = "true"
				dsProps["concatenation_position"] = strconv.Itoa(ddConcatSeq)
			}
			for k, v := range dsnProps { // symbolic_source / symbolic_unresolved / gdg_* / pds_*
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
			// write; OLD/SHR/default = read. A GDG (+n) relative generation is
			// a freshly-created generation → a write even absent DISP=NEW. A
			// dataset is also CONTAINS-ed by the step so it isn't an orphan.
			if currentStepIdx >= 0 {
				kind := string(types.RelationshipKindReadsFrom)
				if disp == "NEW" || disp == "MOD" || gran.props["gdg_write"] == "true" {
					kind = string(types.RelationshipKindWritesTo)
				}
				edgeProps := map[string]string{"dataset": dsn, "disp": disp}
				if currentDDName != "" {
					edgeProps["ddname"] = currentDDName
				}
				if ddConcatSeq > 0 {
					edgeProps["concatenated"] = "true"
				}
				if g := gran.props["gdg_generation"]; g != "" {
					edgeProps["gdg_generation"] = g
					edgeProps["gdg_base"] = gran.props["gdg_base"]
				}
				if m := gran.props["pds_member"]; m != "" {
					edgeProps["pds_member"] = m
					edgeProps["pds_library"] = gran.props["pds_library"]
				}
				entities[currentStepIdx].Relationships = append(entities[currentStepIdx].Relationships,
					types.RelationshipRecord{ToID: dsn, Kind: kind, Properties: edgeProps})
			}
			// Extend the enclosing step's EndLine.
			if currentStepIdx >= 0 && st.startLine > entities[currentStepIdx].EndLine {
				entities[currentStepIdx].EndLine = st.startLine
			}

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

		case "IF":
			// #5044 — `// IF (cond) THEN` opens a conditional block. Steps
			// declared until the matching ELSE/ENDIF are tagged with this
			// predicate + the "then" branch (the IF card emits no entity;
			// the grouping is carried on the contained steps' Properties).
			ifCond = strings.TrimSpace(ifCondRe.ReplaceAllString(operands, "$1"))
			ifBranch = "then"
			currentDDName = ""
			ddConcatSeq = 0

		case "ELSE":
			// The complementary branch of the open IF block.
			if ifCond != "" {
				ifBranch = "else"
			}
			currentDDName = ""
			ddConcatSeq = 0

		case "ENDIF":
			// Close the conditional block.
			ifCond = ""
			ifBranch = ""
			currentDDName = ""
			ddConcatSeq = 0
		}

		// Extend the JOB's EndLine to the furthest statement seen.
		if jobIdx >= 0 && st.startLine > entities[jobIdx].EndLine {
			entities[jobIdx].EndLine = st.startLine
		}
	}

	return entities
}

// datasetGran is the resolved granularity of a DSN operand: a refined entity
// Name (when GDG/PDS granularity applies) plus the properties that record the
// generation / library+member split. name=="" means the raw DSN stands.
type datasetGran struct {
	name  string
	props map[string]string
}

// datasetGranularity splits #5043 GDG relative generations and PDS members
// out of a (symbol-resolved, upper-cased) DSN. A GDG `PROD.GDG(+1)` yields the
// base group name + gdg_base / gdg_generation properties (and gdg_write=true
// for a `(+n)` next-generation create). A PDS `PROD.LOADLIB(PAYROLL)` keeps the
// granular `LIB(MEMBER)` identity as the entity Name plus pds_library /
// pds_member properties. A plain DSN returns an empty datasetGran.
func datasetGranularity(dsn string) datasetGran {
	g := datasetGran{props: map[string]string{}}
	if !strings.HasSuffix(dsn, ")") {
		return g
	}
	// GDG relative generation: the parenthetical is a signed integer.
	if m := gdgGenRe.FindStringSubmatch(dsn); m != nil {
		base := strings.TrimRight(m[1], ".")
		gen := m[2]
		g.name = base // the dataset identity is the GDG base group
		g.props["gdg_base"] = base
		g.props["gdg_generation"] = gen
		// A positive relative generation `(+n)` creates a new generation — a
		// write — even when DISP is omitted/defaulted.
		if strings.HasPrefix(gen, "+") {
			g.props["gdg_write"] = "true"
		}
		return g
	}
	// PDS member: the parenthetical is a member name.
	if m := pdsMemberRe.FindStringSubmatch(dsn); m != nil {
		lib := strings.TrimRight(m[1], ".")
		member := m[2]
		g.name = lib + "(" + member + ")" // granular library+member identity
		g.props["pds_library"] = lib
		g.props["pds_member"] = member
		return g
	}
	return g
}

// restartReached reports whether the RESTART= target step has already appeared
// in the steps seen so far — i.e. the restart entry point is at or before the
// current position, so the current step is NOT skipped on restart.
func restartReached(seen []string, target string) bool {
	for _, s := range seen {
		if s == target {
			return true
		}
	}
	return false
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
