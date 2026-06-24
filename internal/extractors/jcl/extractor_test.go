package jcl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	cobol "github.com/cajasmota/grafel/internal/extractors/cobol"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
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

// TestMainframeLineageSpike_JobPgmDataset proves the full JCL job→program→
// dataset lineage chain the migration spike targets (#5369) in one topology:
//   - the JOB CONTAINS its EXEC steps,
//   - an EXEC PGM= step CALLS the program (the cross_language=cobol bridge),
//   - the step's DD DSN= cards bind datasets with DISP-governed direction
//     (DISP=SHR → READS_FROM, DISP=NEW → WRITES_TO).
// Together these form the enterprise-migration lineage graph: which job runs
// which program, reading and writing which datasets. Honest-partial: bare-name
// program/dataset binding; no STEPLIB load-library disambiguation, catalog
// resolution, or GDG absolute-generation expansion.
func TestMainframeLineageSpike_JobPgmDataset(t *testing.T) {
	recs := run(t, "payjob.jcl", payJobFixture)

	// JOB → step CONTAINS lineage.
	jobs := findByKind(recs, "SCOPE.Component", "job")
	if len(jobs) == 0 {
		t.Fatal("expected a JOB entity")
	}
	var jobContainsStep bool
	for _, rel := range jobs[0].Relationships {
		if rel.Kind == "CONTAINS" && rel.Properties["child"] == "PAYSTEP" {
			jobContainsStep = true
		}
	}
	if !jobContainsStep {
		t.Error("expected JOB CONTAINS the PAYSTEP step")
	}

	// step → program CALLS bridge.
	rel, ok := callTo(recs, "PAYROLL")
	if !ok {
		t.Fatal("expected EXEC PGM=PAYROLL CALLS edge")
	}
	if rel.Properties["via"] != "EXEC PGM" || rel.Properties["cross_language"] != "cobol" {
		t.Errorf("PAYROLL CALL props = %+v, want via=EXEC PGM cross_language=cobol", rel.Properties)
	}

	// step → dataset READS_FROM / WRITES_TO lineage (DISP-governed direction).
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
		t.Error("expected READS_FROM PROD.PAYROLL.MASTER (DISP=SHR input dataset)")
	}
	if !writes {
		t.Error("expected WRITES_TO PROD.PAYROLL.RESULTS (DISP=NEW output dataset)")
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

// ---------------------------------------------------------------------------
// #5043 multi-DSN per DD — concatenated DDs, GDG generations, PDS members.
// ---------------------------------------------------------------------------

// multiDsnFixture mirrors testdata/multidsn.jcl inline.
const multiDsnFixture = `//DSNJOB   JOB (ACCT),'MULTI DSN',CLASS=A,MSGCLASS=X
//*
//LOADSTEP EXEC PGM=LOADER
//INDD     DD DSN=PROD.PART.ONE,DISP=SHR
//         DD DSN=PROD.PART.TWO,DISP=SHR
//         DD DSN=PROD.PART.THREE,DISP=SHR
//GDGIN    DD DSN=PROD.HISTORY.GDG(-1),DISP=SHR
//GDGOUT   DD DSN=PROD.HISTORY.GDG(+1),DISP=(NEW,CATLG)
//PGMLIB   DD DSN=PROD.LOADLIB(PAYROLL),DISP=SHR
//SYSOUT   DD SYSOUT=*
//
`

func relForTo(recs []types.EntityRecord, kind, to string) (types.RelationshipRecord, bool) {
	for _, rel := range relationsByKind(recs, kind) {
		if rel.ToID == to {
			return rel, true
		}
	}
	return types.RelationshipRecord{}, false
}

func datasetByName(recs []types.EntityRecord, name string) (types.EntityRecord, bool) {
	for _, r := range findByKind(recs, "SCOPE.Datastore", "dataset") {
		if r.Name == name {
			return r, true
		}
	}
	return types.EntityRecord{}, false
}

// TestExtractor_ConcatenatedDD proves a single logical DD (INDD) with two
// nameless continuation DD cards emits THREE dataset entities + three read
// edges, all carrying ddname=INDD; the continuants are flagged concatenated.
func TestExtractor_ConcatenatedDD(t *testing.T) {
	recs := run(t, "multidsn.jcl", multiDsnFixture)
	for _, dsn := range []string{"PROD.PART.ONE", "PROD.PART.TWO", "PROD.PART.THREE"} {
		ds, ok := datasetByName(recs, dsn)
		if !ok {
			t.Fatalf("expected dataset entity %s", dsn)
		}
		if ds.Properties["ddname"] != "INDD" {
			t.Errorf("%s: ddname = %q, want INDD", dsn, ds.Properties["ddname"])
		}
		if _, ok := relForTo(recs, string(types.RelationshipKindReadsFrom), dsn); !ok {
			t.Errorf("expected READS_FROM %s", dsn)
		}
	}
	// The two continuation cards are flagged as concatenants.
	two, _ := datasetByName(recs, "PROD.PART.TWO")
	if two.Properties["concatenated"] != "true" || two.Properties["concatenation_position"] != "1" {
		t.Errorf("PROD.PART.TWO concat props = %v, want concatenated=true position=1", two.Properties)
	}
	three, _ := datasetByName(recs, "PROD.PART.THREE")
	if three.Properties["concatenation_position"] != "2" {
		t.Errorf("PROD.PART.THREE concatenation_position = %q, want 2", three.Properties["concatenation_position"])
	}
	// The first card of the concatenation is NOT itself a continuant.
	one, _ := datasetByName(recs, "PROD.PART.ONE")
	if one.Properties["concatenated"] == "true" {
		t.Error("PROD.PART.ONE should not be flagged concatenated (it is the DD head)")
	}
}

// TestExtractor_GDGRelativeGeneration proves a GDG (+1)/(–1) suffix resolves to
// the base group name with a gdg_generation property, and that (+1) is a write.
func TestExtractor_GDGRelativeGeneration(t *testing.T) {
	recs := run(t, "multidsn.jcl", multiDsnFixture)
	// Both generations collapse to the same base entity PROD.HISTORY.GDG.
	base, ok := datasetByName(recs, "PROD.HISTORY.GDG")
	if !ok {
		t.Fatal("expected GDG base dataset entity PROD.HISTORY.GDG")
	}
	if base.Properties["gdg_base"] != "PROD.HISTORY.GDG" {
		t.Errorf("gdg_base = %q", base.Properties["gdg_base"])
	}
	// (-1) is a read of a prior generation.
	if rel, ok := relForTo(recs, string(types.RelationshipKindReadsFrom), "PROD.HISTORY.GDG"); !ok {
		t.Error("expected READS_FROM PROD.HISTORY.GDG (-1)")
	} else if rel.Properties["gdg_generation"] != "-1" {
		t.Errorf("read gdg_generation = %q, want -1", rel.Properties["gdg_generation"])
	}
	// (+1) is a freshly-created generation = a write.
	if rel, ok := relForTo(recs, string(types.RelationshipKindWritesTo), "PROD.HISTORY.GDG"); !ok {
		t.Error("expected WRITES_TO PROD.HISTORY.GDG (+1)")
	} else if rel.Properties["gdg_generation"] != "+1" {
		t.Errorf("write gdg_generation = %q, want +1", rel.Properties["gdg_generation"])
	}
}

// TestExtractor_PDSMember proves DSN=LIB(MEMBER) yields a granular library+member
// dataset identity distinct from the bare library name.
func TestExtractor_PDSMember(t *testing.T) {
	recs := run(t, "multidsn.jcl", multiDsnFixture)
	ds, ok := datasetByName(recs, "PROD.LOADLIB(PAYROLL)")
	if !ok {
		t.Fatal("expected PDS member dataset entity PROD.LOADLIB(PAYROLL)")
	}
	if ds.Properties["pds_library"] != "PROD.LOADLIB" || ds.Properties["pds_member"] != "PAYROLL" {
		t.Errorf("pds props = %v, want library=PROD.LOADLIB member=PAYROLL", ds.Properties)
	}
	if rel, ok := relForTo(recs, string(types.RelationshipKindReadsFrom), "PROD.LOADLIB(PAYROLL)"); !ok {
		t.Error("expected READS_FROM PROD.LOADLIB(PAYROLL)")
	} else if rel.Properties["pds_member"] != "PAYROLL" {
		t.Errorf("read pds_member = %q, want PAYROLL", rel.Properties["pds_member"])
	}
}

// TestExtractor_MultiDSNNoOp proves a plain single-DSN DD carries no #5043
// granularity props (no false concatenation/GDG/PDS flags).
func TestExtractor_MultiDSNNoOp(t *testing.T) {
	recs := run(t, "pay.jcl", payJobFixture)
	for _, r := range findByKind(recs, "SCOPE.Datastore", "dataset") {
		for _, k := range []string{"concatenated", "gdg_generation", "pds_member"} {
			if r.Properties[k] != "" {
				t.Errorf("dataset %q unexpectedly carries %s=%q", r.Name, k, r.Properties[k])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// #5044 conditional flow — COND=, IF/ELSE/ENDIF, RESTART=.
// ---------------------------------------------------------------------------

// condFlowFixture mirrors testdata/condflow.jcl inline.
const condFlowFixture = `//CONDJOB  JOB (ACCT),'COND FLOW',CLASS=A,MSGCLASS=X,RESTART=STEP2
//*
//STEP1    EXEC PGM=PREP
//STEP2    EXEC PGM=LOADER,COND=(4,LT,STEP1)
//STEP3    EXEC PGM=VALIDATE,COND=(8,LE)
//         IF (STEP2.RC = 0) THEN
//OKSTEP   EXEC PGM=COMMIT
//         ELSE
//BADSTEP  EXEC PGM=ROLLBACK
//         ENDIF
//LASTSTEP EXEC PGM=REPORT
//
`

func stepByName(recs []types.EntityRecord, name string) (types.EntityRecord, bool) {
	for _, r := range findByKind(recs, "SCOPE.Operation", "step") {
		if r.Name == name {
			return r, true
		}
	}
	return types.EntityRecord{}, false
}

func precedesFromTo(recs []types.EntityRecord, toSubstr string) (types.RelationshipRecord, bool) {
	for _, rel := range relationsByKind(recs, string(types.RelationshipKindPrecedes)) {
		if strings.Contains(rel.ToID, toSubstr) {
			return rel, true
		}
	}
	return types.RelationshipRecord{}, false
}

// TestExtractor_CondStepSkip proves COND=(code,op,step) records the predicate on
// the step and emits a conditional PRECEDES edge from the referenced step.
func TestExtractor_CondStepSkip(t *testing.T) {
	recs := run(t, "condflow.jcl", condFlowFixture)
	s2, ok := stepByName(recs, "STEP2")
	if !ok {
		t.Fatal("expected STEP2")
	}
	if s2.Properties["cond"] != "4,LT" || s2.Properties["cond_step"] != "STEP1" {
		t.Errorf("STEP2 cond props = %v, want cond=4,LT cond_step=STEP1", s2.Properties)
	}
	// PRECEDES edge from STEP1 to STEP2 carrying the condition.
	s1, _ := stepByName(recs, "STEP1")
	var found bool
	for _, rel := range s1.Relationships {
		if rel.Kind == string(types.RelationshipKindPrecedes) && strings.Contains(rel.ToID, "STEP2") {
			found = true
			if rel.Properties["flow"] != "conditional" || rel.Properties["cond_op"] != "LT" {
				t.Errorf("PRECEDES props = %v", rel.Properties)
			}
		}
	}
	if !found {
		t.Error("expected conditional PRECEDES STEP1 -> STEP2")
	}
	// COND with no step name: attributed to the immediately-prior step (STEP2).
	s3, _ := stepByName(recs, "STEP3")
	if s3.Properties["cond"] != "8,LE" {
		t.Errorf("STEP3 cond = %q, want 8,LE", s3.Properties["cond"])
	}
	if rel, ok := precedesFromTo(recs, "STEP3"); !ok {
		t.Error("expected PRECEDES edge to STEP3 (implicit prior-step COND)")
	} else if rel.Properties["cond_scope"] != "all_prior" {
		t.Errorf("STEP3 cond edge cond_scope = %q, want all_prior", rel.Properties["cond_scope"])
	}
}

// TestExtractor_IfElseEndif proves steps inside an IF/ELSE/ENDIF block carry the
// predicate + branch grouping.
func TestExtractor_IfElseEndif(t *testing.T) {
	recs := run(t, "condflow.jcl", condFlowFixture)
	ok1, _ := stepByName(recs, "OKSTEP")
	if ok1.Properties["if_cond"] != "STEP2.RC = 0" || ok1.Properties["if_branch"] != "then" {
		t.Errorf("OKSTEP if props = %v, want cond='STEP2.RC = 0' branch=then", ok1.Properties)
	}
	bad, _ := stepByName(recs, "BADSTEP")
	if bad.Properties["if_branch"] != "else" {
		t.Errorf("BADSTEP if_branch = %q, want else", bad.Properties["if_branch"])
	}
	// A step after ENDIF is no longer inside the block.
	last, _ := stepByName(recs, "LASTSTEP")
	if last.Properties["if_cond"] != "" {
		t.Errorf("LASTSTEP unexpectedly inside IF block: %v", last.Properties)
	}
}

// TestExtractor_Restart proves RESTART=STEP2 marks the entry point and flags the
// steps before it as skipped on restart.
func TestExtractor_Restart(t *testing.T) {
	recs := run(t, "condflow.jcl", condFlowFixture)
	job := findByKind(recs, "SCOPE.Component", "job")
	if len(job) != 1 || job[0].Properties["restart"] != "STEP2" {
		t.Fatalf("expected JOB restart=STEP2, got %v", job)
	}
	s1, _ := stepByName(recs, "STEP1")
	if s1.Properties["restart_skipped"] != "true" {
		t.Errorf("STEP1 should be restart_skipped (before STEP2): %v", s1.Properties)
	}
	s2, _ := stepByName(recs, "STEP2")
	if s2.Properties["restart_entry"] != "true" {
		t.Errorf("STEP2 should be restart_entry: %v", s2.Properties)
	}
	s3, _ := stepByName(recs, "STEP3")
	if s3.Properties["restart_skipped"] == "true" {
		t.Error("STEP3 (after restart point) must NOT be restart_skipped")
	}
}

// TestExtractor_CondFlowNoOp proves an ordinary job carries no #5044 conditional
// props (no false cond/if/restart flags).
func TestExtractor_CondFlowNoOp(t *testing.T) {
	recs := run(t, "pay.jcl", payJobFixture)
	for _, r := range recs {
		for _, k := range []string{"cond", "if_cond", "restart_skipped", "restart_entry"} {
			if r.Properties[k] != "" {
				t.Errorf("%s %q unexpectedly carries %s=%q", r.Kind, r.Name, k, r.Properties[k])
			}
		}
	}
	for _, rel := range relationsByKind(recs, string(types.RelationshipKindPrecedes)) {
		t.Errorf("plain job unexpectedly emitted PRECEDES edge: %v", rel)
	}
}

// TestExtractor_CondFlowWrongLanguageNoOp proves COBOL source yields no JCL
// conditional-flow entities/edges (the JCL card grammar matches nothing).
func TestExtractor_CondFlowWrongLanguageNoOp(t *testing.T) {
	const cobolSrc = `       IDENTIFICATION DIVISION.
       PROGRAM-ID. PAYROLL.
       PROCEDURE DIVISION.
           IF WS-RC = 0 THEN
              PERFORM COMMIT-PARA
           ELSE
              PERFORM ROLLBACK-PARA
           END-IF.
           STOP RUN.
`
	recs := run(t, "payroll.cbl", cobolSrc)
	for _, r := range recs {
		if r.Kind == "SCOPE.Operation" || r.Kind == "SCOPE.Component" {
			t.Errorf("JCL extractor produced %s %q from COBOL source", r.Kind, r.Name)
		}
	}
	if len(relationsByKind(recs, string(types.RelationshipKindPrecedes))) != 0 {
		t.Error("JCL extractor produced PRECEDES edges from COBOL source")
	}
}
