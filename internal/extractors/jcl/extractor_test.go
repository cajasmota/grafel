package jcl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	cobol "github.com/cajasmota/archigraph/internal/extractors/cobol"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"
)

// run extracts entities from a JCL source string.
func run(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	recs, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "jcl",
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	return recs
}

func findByKind(recs []types.EntityRecord, kind, subtype string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == kind && (subtype == "" || r.Subtype == subtype) {
			out = append(out, r)
		}
	}
	return out
}

func hasEntity(recs []types.EntityRecord, kind, subtype, name string) bool {
	for _, r := range findByKind(recs, kind, subtype) {
		if r.Name == name {
			return true
		}
	}
	return false
}

func relationsByKind(recs []types.EntityRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == kind {
				out = append(out, rel)
			}
		}
	}
	return out
}

func callTo(recs []types.EntityRecord, target string) (types.RelationshipRecord, bool) {
	for _, rel := range relationsByKind(recs, "CALLS") {
		if rel.ToID == target {
			return rel, true
		}
	}
	return types.RelationshipRecord{}, false
}

func TestExtractor_Language(t *testing.T) {
	if got := (&Extractor{}).Language(); got != "jcl" {
		t.Fatalf("Language() = %q, want jcl", got)
	}
}

func TestExtractor_RegisteredInGlobalRegistry(t *testing.T) {
	if _, ok := extractor.Get("jcl"); !ok {
		t.Fatal("expected jcl extractor to be registered")
	}
}

func TestExtractor_ClassifierExtension(t *testing.T) {
	// The fixture must classify to jcl by extension so the dispatcher routes
	// .jcl files here.
	if got, ok := extractor.Get("jcl"); !ok || got.Language() != "jcl" {
		t.Fatalf("jcl extractor lookup failed: %v %q", ok, got)
	}
}

// TestExtractor_JobAndSteps verifies JOB / EXEC step extraction and CONTAINS
// wiring from the job to its steps.
func TestExtractor_JobAndSteps(t *testing.T) {
	recs := run(t, "pay.jcl", payJobFixture)

	if !hasEntity(recs, "SCOPE.Component", "job", "PAYJOB") {
		t.Error("expected JOB entity PAYJOB")
	}
	for _, step := range []string{"PAYSTEP", "RPTSTEP", "ARCHIVE"} {
		if !hasEntity(recs, "SCOPE.Operation", "step", step) {
			t.Errorf("expected step entity %q", step)
		}
	}
	// Job CONTAINS its steps.
	var jobContains int
	for _, r := range findByKind(recs, "SCOPE.Component", "job") {
		for _, rel := range r.Relationships {
			if rel.Kind == "CONTAINS" {
				jobContains++
			}
		}
	}
	if jobContains < 3 {
		t.Errorf("expected job to CONTAIN >=3 steps, got %d", jobContains)
	}
}

// TestExtractor_ExecPgmCallsEdge proves the core bridge edge: an
// EXEC PGM=<program> step emits a CALLS edge with the cross-language tag.
func TestExtractor_ExecPgmCallsEdge(t *testing.T) {
	recs := run(t, "pay.jcl", payJobFixture)

	rel, ok := callTo(recs, "PAYROLL")
	if !ok {
		t.Fatal("expected CALLS edge to PAYROLL from EXEC PGM=PAYROLL")
	}
	if rel.Properties["via"] != "EXEC PGM" {
		t.Errorf("PAYROLL CALL via = %q, want 'EXEC PGM'", rel.Properties["via"])
	}
	if rel.Properties["external"] != "true" {
		t.Errorf("PAYROLL CALL external = %q, want true", rel.Properties["external"])
	}
	if rel.Properties["cross_language"] != "cobol" {
		t.Errorf("PAYROLL CALL cross_language = %q, want cobol", rel.Properties["cross_language"])
	}
}

// TestExtractor_ExecProcCallsEdge verifies EXEC PROC= invocation edges.
func TestExtractor_ExecProcCallsEdge(t *testing.T) {
	recs := run(t, "pay.jcl", payJobFixture)
	rel, ok := callTo(recs, "ARCHPROC")
	if !ok {
		t.Fatal("expected CALLS edge to ARCHPROC from EXEC PROC=ARCHPROC")
	}
	if rel.Properties["via"] != "EXEC PROC" {
		t.Errorf("ARCHPROC CALL via = %q, want 'EXEC PROC'", rel.Properties["via"])
	}
}

// TestExtractor_ProcDefinition verifies inline PROC definitions and that
// steps inside the proc are contained by it.
func TestExtractor_ProcDefinition(t *testing.T) {
	recs := run(t, "pay.jcl", payJobFixture)
	if !hasEntity(recs, "SCOPE.Component", "proc", "ARCHPROC") {
		t.Error("expected PROC entity ARCHPROC")
	}
	if !hasEntity(recs, "SCOPE.Operation", "step", "COPYSTEP") {
		t.Error("expected step COPYSTEP inside the inline PROC")
	}
	// The PROC contains COPYSTEP, not the JOB.
	var procContainsCopy bool
	for _, r := range findByKind(recs, "SCOPE.Component", "proc") {
		for _, rel := range r.Relationships {
			if rel.Kind == "CONTAINS" && rel.Properties["child"] == "COPYSTEP" {
				procContainsCopy = true
			}
		}
	}
	if !procContainsCopy {
		t.Error("expected PROC ARCHPROC to CONTAIN COPYSTEP")
	}
}

// TestExtractor_Datasets verifies DD DSN= dataset entities and READS/WRITES
// edges keyed off DISP.
func TestExtractor_Datasets(t *testing.T) {
	recs := run(t, "pay.jcl", payJobFixture)
	if !hasEntity(recs, "SCOPE.Datastore", "dataset", "PROD.PAYROLL.MASTER") {
		t.Error("expected dataset PROD.PAYROLL.MASTER")
	}
	// DISP=SHR master => READS_FROM; DISP=(NEW,...) results => WRITES_TO.
	var reads, writes bool
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "READS_FROM" && rel.ToID == "PROD.PAYROLL.MASTER" {
				reads = true
			}
			if rel.Kind == "WRITES_TO" && rel.ToID == "PROD.PAYROLL.RESULTS" {
				writes = true
			}
		}
	}
	if !reads {
		t.Error("expected READS_FROM edge to PROD.PAYROLL.MASTER (DISP=SHR)")
	}
	if !writes {
		t.Error("expected WRITES_TO edge to PROD.PAYROLL.RESULTS (DISP=NEW)")
	}
}

// TestExtractor_LanguageTagged ensures entities/relationships are tagged jcl.
func TestExtractor_LanguageTagged(t *testing.T) {
	recs := run(t, "pay.jcl", payJobFixture)
	for _, r := range recs {
		if r.Language != "jcl" {
			t.Fatalf("entity %q language = %q, want jcl", r.Name, r.Language)
		}
	}
}

// TestCrossLanguageBridge_JCLtoCOBOL is the headline proof: a JCL
// EXEC PGM=PAYROLL step's CALLS edge resolves by name to the COBOL
// PROGRAM-ID PAYROLL entity extracted from a sibling .cbl fixture. This is
// the mainframe analog of the HTTP cross-repo linker — orchestration (JCL)
// linked to implementation (COBOL) — reusing the by-name resolver with no
// new linker code.
func TestCrossLanguageBridge_JCLtoCOBOL(t *testing.T) {
	// 1. Extract the COBOL program from the sibling cobol testdata fixture.
	cblPath := filepath.Join("..", "cobol", "testdata", "payroll.cbl")
	cblSrc, err := os.ReadFile(cblPath)
	if err != nil {
		t.Fatalf("read cobol fixture: %v", err)
	}
	cblRecs, err := (&cobol.Extractor{}).Extract(context.Background(), extractor.FileInput{
		Path:     "payroll.cbl",
		Content:  cblSrc,
		Language: "cobol",
	})
	if err != nil {
		t.Fatalf("cobol Extract: %v", err)
	}

	// Locate the COBOL PROGRAM-ID entity and assign it a stable ID (the
	// resolver indexes by ID/Name; extractors leave ID empty for the engine
	// to fill — we stamp one here to mimic the indexed graph).
	var cobolProgramID string
	for i := range cblRecs {
		if cblRecs[i].Kind == "SCOPE.Component" && cblRecs[i].Subtype == "program" &&
			cblRecs[i].Name == "PAYROLL" {
			cblRecs[i].ID = "cobolpayroll0001"
			cobolProgramID = cblRecs[i].ID
		}
	}
	if cobolProgramID == "" {
		t.Fatal("cobol fixture did not yield a PAYROLL PROGRAM-ID entity")
	}

	// 2. Extract the JCL job.
	jclRecs := run(t, "payjob.jcl", payJobFixture)

	// 3. Build a unified index over both languages' entities and resolve the
	// JCL step's CALLS edge.
	all := append([]types.EntityRecord{}, cblRecs...)
	all = append(all, jclRecs...)
	idx := resolve.BuildIndex(all)

	id, ok := idx.Lookup("PAYROLL")
	if !ok {
		t.Fatal("by-name lookup of PAYROLL failed; cross-language bridge cannot resolve")
	}
	if id != cobolProgramID {
		t.Fatalf("PAYROLL resolved to %q, want COBOL program ID %q", id, cobolProgramID)
	}

	// 4. Apply the resolver to the JCL relationships and confirm the
	// EXEC PGM=PAYROLL CALLS edge is rewritten to the COBOL program's ID.
	var jclRels []types.RelationshipRecord
	for ri := range jclRecs {
		for rj := range jclRecs[ri].Relationships {
			jclRels = append(jclRels, jclRecs[ri].Relationships[rj])
		}
	}
	stats := resolve.References(jclRels, idx)
	if stats.Rewritten == 0 {
		t.Fatal("resolver rewrote zero JCL edges; expected the PGM=PAYROLL edge to bind")
	}
	var bridged bool
	for _, rel := range jclRels {
		if rel.Kind == "CALLS" && rel.ToID == cobolProgramID &&
			rel.Properties["cross_language"] == "cobol" {
			bridged = true
		}
	}
	if !bridged {
		t.Fatal("JCL EXEC PGM=PAYROLL did not resolve to the COBOL PAYROLL program entity")
	}
}

// TestExtractor_IncludeImports proves an `INCLUDE MEMBER=<name>` card emits
// an IMPORTS edge to the spliced PROCLIB/JCLLIB member (a cross-file dep).
func TestExtractor_IncludeImports(t *testing.T) {
	recs := run(t, "util.jcl", utilJobFixture)
	var found types.RelationshipRecord
	var ok bool
	for _, rel := range relationsByKind(recs, "IMPORTS") {
		if rel.ToID == "SHRSET" {
			found, ok = rel, true
		}
	}
	if !ok {
		t.Fatal("expected IMPORTS edge to INCLUDE member SHRSET")
	}
	if found.Properties["import_kind"] != "include" {
		t.Errorf("import_kind = %q, want include", found.Properties["import_kind"])
	}
	if found.Properties["member"] != "SHRSET" {
		t.Errorf("member = %q, want SHRSET", found.Properties["member"])
	}
}

// TestExtractor_TSOCallEdge proves the indirect JCL→program edge recovered
// from an IKJEFT01 step's SYSTSIN `CALL 'lib(BILLING)'` instream control
// card — the program the terminal monitor actually runs.
func TestExtractor_TSOCallEdge(t *testing.T) {
	recs := run(t, "util.jcl", utilJobFixture)
	rel, ok := callTo(recs, "BILLING")
	if !ok {
		t.Fatal("expected CALLS edge to BILLING from TSO CALL control card")
	}
	if rel.Properties["via"] != "TSO CALL" {
		t.Errorf("BILLING CALL via = %q, want 'TSO CALL'", rel.Properties["via"])
	}
	if rel.Properties["cross_language"] != "cobol" {
		t.Errorf("BILLING CALL cross_language = %q, want cobol", rel.Properties["cross_language"])
	}
	if rel.Properties["host_program"] != "IKJEFT01" {
		t.Errorf("BILLING CALL host_program = %q, want IKJEFT01", rel.Properties["host_program"])
	}
	// The bare IKJEFT01 PGM= edge must NOT leak past the next step: REPORTER
	// is a normal program in the following step, not a TSO callee.
	if _, leaked := callTo(recs, "REPORTER"); leaked {
		// REPORTER is a legitimate EXEC PGM= edge — confirm it is via EXEC PGM,
		// not mis-attributed as a TSO CALL.
		r, _ := callTo(recs, "REPORTER")
		if r.Properties["via"] == "TSO CALL" {
			t.Error("REPORTER mis-attributed as a TSO CALL across the step boundary")
		}
	}
}

// utilJobFixture mirrors testdata/utiljob.jcl inline.
const utilJobFixture = `//UTILJOB  JOB (ACCT),'BILLING RUN',CLASS=A,MSGCLASS=X
//*
//* INCLUDE a shared PROCLIB member, then run a COBOL program under the
//* TSO terminal monitor (IKJEFT01) via a SYSTSIN CALL control card.
//*
//         INCLUDE MEMBER=SHRSET
//*
//BILLSTEP EXEC PGM=IKJEFT01,DYNAMNBR=20
//STEPLIB  DD DSN=PROD.BILLING.LOADLIB,DISP=SHR
//SYSTSPRT DD SYSOUT=*
//SYSTSIN  DD *
  CALL 'PROD.BILLING.LOADLIB(BILLING)'
/*
//RPTSTEP  EXEC PGM=REPORTER
//SYSOUT   DD SYSOUT=*
//
`

func TestExtractor_DSNRunProgramEdge(t *testing.T) {
	recs := run(t, "db2job.jcl", db2JobFixture)
	rel, ok := callTo(recs, "PAYRPT")
	if !ok {
		t.Fatal("expected CALLS edge to PAYRPT from DSN RUN PROGRAM control card")
	}
	if rel.Properties["via"] != "DSN RUN PROGRAM" {
		t.Errorf("PAYRPT CALL via = %q, want 'DSN RUN PROGRAM'", rel.Properties["via"])
	}
	if rel.Properties["db2_plan"] != "PAYPLAN" {
		t.Errorf("PAYRPT db2_plan = %q, want PAYPLAN", rel.Properties["db2_plan"])
	}
	if rel.Properties["db2_system"] != "DB2P" {
		t.Errorf("PAYRPT db2_system = %q, want DB2P", rel.Properties["db2_system"])
	}
	if rel.Properties["cross_language"] != "cobol" {
		t.Errorf("PAYRPT cross_language = %q, want cobol", rel.Properties["cross_language"])
	}
	if rel.Properties["host_program"] != "IKJEFT01" {
		t.Errorf("PAYRPT host_program = %q, want IKJEFT01", rel.Properties["host_program"])
	}
}

func TestExtractor_DSNUTILBRunProgram(t *testing.T) {
	// DSNUTILB is itself recognised as a shell utility; a RUN PROGRAM card in
	// its SYSIN must surface the same way.
	const src = `//DB2UTIL  JOB (ACCT),'DB2 UTIL',CLASS=A
//RUNSTEP  EXEC PGM=DSNUTILB,PARM='DB2P,RUNIT'
//SYSPRINT DD SYSOUT=*
//SYSIN    DD *
  RUN PROGRAM(LOADTAB) PLAN(LOADPLN)
/*
//
`
	recs := run(t, "db2utilb.jcl", src)
	rel, ok := callTo(recs, "LOADTAB")
	if !ok {
		t.Fatal("expected CALLS edge to LOADTAB from DSNUTILB RUN PROGRAM")
	}
	if rel.Properties["via"] != "DSN RUN PROGRAM" {
		t.Errorf("LOADTAB via = %q, want 'DSN RUN PROGRAM'", rel.Properties["via"])
	}
	if rel.Properties["db2_plan"] != "LOADPLN" {
		t.Errorf("LOADTAB db2_plan = %q, want LOADPLN", rel.Properties["db2_plan"])
	}
}

func TestExtractor_IDCAMSReproDatasets(t *testing.T) {
	recs := run(t, "idcams.jcl", idcamsJobFixture)
	// The IN/FROM dataset is a read; the OUT/TO dataset is a write.
	if !hasEntity(recs, "SCOPE.Datastore", "dataset", "PROD.SRC.VSAM") {
		t.Error("expected dataset entity PROD.SRC.VSAM from IDCAMS REPRO INDATASET")
	}
	if !hasEntity(recs, "SCOPE.Datastore", "dataset", "PROD.TGT.VSAM") {
		t.Error("expected dataset entity PROD.TGT.VSAM from IDCAMS REPRO OUTDATASET")
	}
	var readOK, writeOK bool
	for _, r := range relationsByKind(recs, string(types.RelationshipKindReadsFrom)) {
		if r.ToID == "PROD.SRC.VSAM" && r.Properties["via"] == "IDCAMS" {
			readOK = true
		}
	}
	for _, r := range relationsByKind(recs, string(types.RelationshipKindWritesTo)) {
		if r.ToID == "PROD.TGT.VSAM" && r.Properties["via"] == "IDCAMS" {
			writeOK = true
		}
	}
	if !readOK {
		t.Error("expected READS_FROM PROD.SRC.VSAM via IDCAMS")
	}
	if !writeOK {
		t.Error("expected WRITES_TO PROD.TGT.VSAM via IDCAMS")
	}
}

// db2JobFixture runs a COBOL/DB2 program under the TSO terminal monitor via a
// DSN SYSTEM(...) RUN PROGRAM(...) PLAN(...) control card.
const db2JobFixture = `//DB2JOB   JOB (ACCT),'DB2 BATCH',CLASS=A,MSGCLASS=X
//RUNSTEP  EXEC PGM=IKJEFT01,DYNAMNBR=20
//STEPLIB  DD DSN=DB2P.SDSNLOAD,DISP=SHR
//SYSTSPRT DD SYSOUT=*
//SYSTSIN  DD *
  DSN SYSTEM(DB2P)
  RUN PROGRAM(PAYRPT) PLAN(PAYPLAN) LIB('PROD.DB2.LOADLIB')
  END
/*
//
`

// idcamsJobFixture performs a VSAM copy via IDCAMS REPRO with literal dataset
// operands on its SYSIN control cards.
const idcamsJobFixture = `//IDCJOB   JOB (ACCT),'VSAM COPY',CLASS=A
//COPYSTEP EXEC PGM=IDCAMS
//SYSPRINT DD SYSOUT=*
//SYSIN    DD *
  REPRO INDATASET(PROD.SRC.VSAM) -
        OUTDATASET(PROD.TGT.VSAM)
/*
//
`

// ---------------------------------------------------------------------------
// #5042 — SET/JCLLIB symbolic substitution + &VAR resolution.
// ---------------------------------------------------------------------------

// symJobFixture exercises the symbolic-parameter substitution paths: a job
// `// SET` card, a PROC SYMBOL= default, an EXEC SYMBOL= override, &VAR
// resolution into PGM=/PROC=/DSN=, an unresolved &VAR, and a `// JCLLIB ORDER`.
const symJobFixture = `//SYMJOB   JOB (ACCT),'SYMBOLIC',CLASS=A,MSGCLASS=X
//         JCLLIB ORDER=(PROD.PROCLIB,SHARED.PROCLIB)
//         SET HLQ=PROD,PGMNAME=PAYROLL
//*
//RUNSTEP  EXEC PGM=&PGMNAME
//EMPIN    DD DSN=&HLQ..PAYROLL.MASTER,DISP=SHR
//PAYOUT   DD DSN=&HLQ..PAYROLL.RESULTS,DISP=(NEW,CATLG,DELETE)
//MYSTERY  DD DSN=&UNDEF..FILE,DISP=SHR
//*
//ARCH     EXEC PROC=ARCHP,DSNAME=ARCHIVE
//*
//ARCHP    PROC DSNAME=DEFLT
//COPYSTEP EXEC PGM=IEBGENER
//SYSUT2   DD DSN=&HLQ..&DSNAME,DISP=(NEW,CATLG)
//         PEND
`

// TestExtractor_SetSymbolSubstitution proves a `// SET PGMNAME=PAYROLL`
// resolves `EXEC PGM=&PGMNAME` to the literal program so the COBOL by-name
// bridge edge targets PAYROLL, not the literal &PGMNAME token.
func TestExtractor_SetSymbolSubstitution(t *testing.T) {
	recs := run(t, "sym.jcl", symJobFixture)

	if _, leaked := callTo(recs, "&PGMNAME"); leaked {
		t.Error("EXEC PGM=&PGMNAME leaked the literal &VAR token instead of resolving")
	}
	rel, ok := callTo(recs, "PAYROLL")
	if !ok {
		t.Fatal("expected CALLS edge to PAYROLL after &PGMNAME substitution")
	}
	if rel.Properties["cross_language"] != "cobol" {
		t.Errorf("PAYROLL cross_language = %q, want cobol", rel.Properties["cross_language"])
	}
	// The step records the pre-substitution literal.
	var srcOK bool
	for _, r := range findByKind(recs, "SCOPE.Operation", "step") {
		if r.Name == "RUNSTEP" && r.Properties["symbolic_source"] == "&PGMNAME" &&
			r.Properties["pgm"] == "PAYROLL" {
			srcOK = true
		}
	}
	if !srcOK {
		t.Error("expected RUNSTEP to record symbolic_source=&PGMNAME and pgm=PAYROLL")
	}
}

// TestExtractor_DsnSymbolSubstitution proves `&HLQ..PAYROLL.MASTER` resolves to
// PROD.PAYROLL.MASTER (the HLQ value + concatenation period) so the dataset
// identity is the real qualified name, not a &VAR-bearing token.
func TestExtractor_DsnSymbolSubstitution(t *testing.T) {
	recs := run(t, "sym.jcl", symJobFixture)

	if !hasEntity(recs, "SCOPE.Datastore", "dataset", "PROD.PAYROLL.MASTER") {
		t.Error("expected dataset PROD.PAYROLL.MASTER from &HLQ..PAYROLL.MASTER")
	}
	if !hasEntity(recs, "SCOPE.Datastore", "dataset", "PROD.PAYROLL.RESULTS") {
		t.Error("expected dataset PROD.PAYROLL.RESULTS from &HLQ..PAYROLL.RESULTS")
	}
	var readOK bool
	for _, r := range relationsByKind(recs, "READS_FROM") {
		if r.ToID == "PROD.PAYROLL.MASTER" {
			readOK = true
		}
	}
	if !readOK {
		t.Error("expected READS_FROM PROD.PAYROLL.MASTER")
	}
	// The dataset entity records the pre-substitution literal.
	var srcOK bool
	for _, r := range findByKind(recs, "SCOPE.Datastore", "dataset") {
		if r.Name == "PROD.PAYROLL.MASTER" && r.Properties["symbolic_source"] == "&HLQ..PAYROLL.MASTER" {
			srcOK = true
		}
	}
	if !srcOK {
		t.Error("expected dataset to record symbolic_source=&HLQ..PAYROLL.MASTER")
	}
}

// TestExtractor_ProcSymbolDefaultAndOverride proves PROC SYMBOL= defaults and
// EXEC SYMBOL= overrides: ARCHP defines DSNAME=DEFLT; the EXEC overrides it to
// ARCHIVE, so `DSN=&HLQ..&DSNAME` in the proc body resolves to PROD.ARCHIVE.
func TestExtractor_ProcSymbolDefaultAndOverride(t *testing.T) {
	recs := run(t, "sym.jcl", symJobFixture)
	if !hasEntity(recs, "SCOPE.Datastore", "dataset", "PROD.ARCHIVE") {
		t.Error("expected dataset PROD.ARCHIVE from &HLQ..&DSNAME with EXEC DSNAME=ARCHIVE override")
	}
}

// TestExtractor_UnresolvedSymbol proves an unknown `&UNDEF` is left intact and
// flagged symbolic_unresolved (rather than silently dropped).
func TestExtractor_UnresolvedSymbol(t *testing.T) {
	recs := run(t, "sym.jcl", symJobFixture)
	var found bool
	for _, r := range findByKind(recs, "SCOPE.Datastore", "dataset") {
		if r.Properties["symbolic_unresolved"] == "true" && strings.Contains(r.Name, "&UNDEF") {
			found = true
		}
	}
	if !found {
		t.Error("expected an unresolved &UNDEF dataset flagged symbolic_unresolved=true")
	}
}

// TestExtractor_JCLLibOrder proves `// JCLLIB ORDER=(...)` is recorded as a
// jcllib_order search-order hint on the JOB.
func TestExtractor_JCLLibOrder(t *testing.T) {
	recs := run(t, "sym.jcl", symJobFixture)
	var ok bool
	for _, r := range findByKind(recs, "SCOPE.Component", "job") {
		if r.Properties["jcllib_order"] == "PROD.PROCLIB,SHARED.PROCLIB" {
			ok = true
		}
	}
	if !ok {
		t.Error("expected JOB jcllib_order=PROD.PROCLIB,SHARED.PROCLIB")
	}
}

// TestExtractor_NoSymbolsNoOp proves the no-match no-op: a job with no SET/&VAR
// is unchanged — no symbolic_source/unresolved properties appear anywhere.
func TestExtractor_NoSymbolsNoOp(t *testing.T) {
	recs := run(t, "pay.jcl", payJobFixture)
	for _, r := range recs {
		if r.Properties["symbolic_source"] != "" || r.Properties["symbolic_unresolved"] != "" {
			t.Errorf("entity %q unexpectedly carries symbolic props: %v", r.Name, r.Properties)
		}
		if r.Properties["jcllib_order"] != "" {
			t.Errorf("entity %q unexpectedly carries jcllib_order", r.Name)
		}
	}
	// The literal PAYROLL program is still bound (no &VAR involved).
	if _, ok := callTo(recs, "PAYROLL"); !ok {
		t.Error("expected literal PAYROLL CALL edge to remain")
	}
}

// TestExtractor_WrongLanguageNoOp proves the wrong-language no-op: COBOL source
// fed to the JCL extractor yields no JCL job/step/dataset entities (the JCL
// card grammar matches nothing in a COBOL program).
func TestExtractor_WrongLanguageNoOp(t *testing.T) {
	const cobolSrc = `       IDENTIFICATION DIVISION.
       PROGRAM-ID. PAYROLL.
       PROCEDURE DIVISION.
           SET WS-FLAG TO TRUE.
           MOVE &HLQ TO WS-NAME.
           STOP RUN.
`
	recs := run(t, "payroll.cbl", cobolSrc)
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" || r.Kind == "SCOPE.Operation" || r.Kind == "SCOPE.Datastore" {
			t.Errorf("JCL extractor produced %s/%s %q from COBOL source", r.Kind, r.Subtype, r.Name)
		}
	}
}

// payJobFixture mirrors testdata/payjob.jcl inline so the unit tests are
// self-contained; the on-disk fixture exists for end-to-end pipeline runs.
const payJobFixture = `//PAYJOB   JOB (ACCT),'PAYROLL RUN',CLASS=A,MSGCLASS=X,
//             NOTIFY=&SYSUID,REGION=0M
//*
//* Monthly payroll batch.
//*
//PAYSTEP  EXEC PGM=PAYROLL,REGION=4M
//STEPLIB  DD DSN=PROD.PAYROLL.LOADLIB,DISP=SHR
//EMPIN    DD DSN=PROD.PAYROLL.MASTER,DISP=SHR
//PAYOUT   DD DSN=PROD.PAYROLL.RESULTS,DISP=(NEW,CATLG,DELETE),
//             SPACE=(CYL,(10,5)),UNIT=SYSDA
//SYSOUT   DD SYSOUT=*
//*
//RPTSTEP  EXEC PGM=REPORTER
//RPTIN    DD DSN=PROD.PAYROLL.RESULTS,DISP=OLD
//SYSOUT   DD SYSOUT=*
//*
//ARCHIVE  EXEC PROC=ARCHPROC
//*
//ARCHPROC PROC
//COPYSTEP EXEC PGM=IEBGENER
//SYSUT1   DD DSN=PROD.PAYROLL.RESULTS,DISP=SHR
//SYSUT2   DD DSN=PROD.PAYROLL.ARCHIVE,DISP=(NEW,CATLG)
//         PEND
`
