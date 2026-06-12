package cobol

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// run is a small helper that extracts entities from a source string.
func run(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	return runWithRoot(t, path, src, "")
}

// runWithRoot extracts with an explicit RepoRoot so the COPY resolver can
// bind copybook references to on-disk .cpy files (#2838).
func runWithRoot(t *testing.T, path, src, repoRoot string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	recs, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "cobol",
		RepoRoot: repoRoot,
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	return recs
}

// importEdge returns the first IMPORTS relationship whose copybook property
// matches book, or false.
func importEdge(recs []types.EntityRecord, book string) (types.RelationshipRecord, bool) {
	for _, rel := range relationsByKind(recs, "IMPORTS") {
		if rel.Properties["copybook"] == book {
			return rel, true
		}
	}
	return types.RelationshipRecord{}, false
}

// findByKind returns entities matching kind (and optional subtype).
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

// relationsOf returns every relationship across all records of a given kind.
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

func hasCallTo(recs []types.EntityRecord, target string) bool {
	for _, rel := range relationsByKind(recs, "CALLS") {
		if rel.ToID == target {
			return true
		}
	}
	return false
}

func TestExtractor_Language(t *testing.T) {
	if got := (&Extractor{}).Language(); got != "cobol" {
		t.Fatalf("Language() = %q, want cobol", got)
	}
}

func TestExtractor_RegisteredInGlobalRegistry(t *testing.T) {
	// The package init() must register the cobol extractor — this is the
	// assertion that retires the in-repo "cobol = unsupported" joke.
	got, ok := extractor.Get("cobol")
	if !ok {
		t.Fatal("expected cobol extractor to be registered")
	}
	if got.Language() != "cobol" {
		t.Fatalf("registered extractor Language() = %q, want cobol", got.Language())
	}
}

func TestExtractor_ProgramAndDivisions(t *testing.T) {
	src := loadFixture(t, "payroll.cbl")
	recs := run(t, "payroll.cbl", src)

	if !hasEntity(recs, "SCOPE.Component", "program", "PAYROLL") {
		t.Error("expected program PAYROLL")
	}
	for _, div := range []string{
		"IDENTIFICATION DIVISION", "ENVIRONMENT DIVISION",
		"DATA DIVISION", "PROCEDURE DIVISION",
	} {
		if !hasEntity(recs, "SCOPE.Component", "division", div) {
			t.Errorf("expected division %q", div)
		}
	}
}

func TestExtractor_Paragraphs(t *testing.T) {
	src := loadFixture(t, "payroll.cbl")
	recs := run(t, "payroll.cbl", src)

	for _, para := range []string{
		"MAIN-PROCESS", "INIT-PROGRAM", "PROCESS-EMPLOYEES",
		"CALCULATE-PAY", "PERSIST-PAY", "FINALIZE-PROGRAM",
	} {
		if !hasEntity(recs, "SCOPE.Operation", "paragraph", para) {
			t.Errorf("expected paragraph %q", para)
		}
	}
	// Verbs / reserved heads must NOT be misread as paragraphs.
	for _, notPara := range []string{"OPEN", "READ", "WRITE", "GOBACK", "PERFORM"} {
		if hasEntity(recs, "SCOPE.Operation", "paragraph", notPara) {
			t.Errorf("verb %q must not be a paragraph", notPara)
		}
	}
}

func TestExtractor_PerformIsIntraCall(t *testing.T) {
	src := loadFixture(t, "payroll.cbl")
	recs := run(t, "payroll.cbl", src)

	for _, target := range []string{"INIT-PROGRAM", "PROCESS-EMPLOYEES", "CALCULATE-PAY", "PERSIST-PAY", "FINALIZE-PROGRAM"} {
		if !hasCallTo(recs, target) {
			t.Errorf("expected PERFORM CALLS edge to %q", target)
		}
	}
	// Inline PERFORM UNTIL must not produce a CALLS edge to "UNTIL".
	if hasCallTo(recs, "UNTIL") {
		t.Error("PERFORM ... UNTIL produced a spurious CALLS edge to UNTIL")
	}
	// PERFORM edges carry via=PERFORM.
	var foundVia bool
	for _, rel := range relationsByKind(recs, "CALLS") {
		if rel.ToID == "CALCULATE-PAY" && rel.Properties["via"] == "PERFORM" {
			foundVia = true
		}
	}
	if !foundVia {
		t.Error("expected PERFORM edge to CALCULATE-PAY tagged via=PERFORM")
	}
}

// relByViaTo returns the first CALLS edge to target whose via property equals
// via, or false.
func relByViaTo(recs []types.EntityRecord, target, via string) (types.RelationshipRecord, bool) {
	for _, rel := range relationsByKind(recs, "CALLS") {
		if rel.ToID == target && rel.Properties["via"] == via {
			return rel, true
		}
	}
	return types.RelationshipRecord{}, false
}

// TestExtractor_PerformThruRange proves PERFORM <a> THRU <b> emits CALLS edges
// to both range endpoints (#4946): the start via=PERFORM and the end
// via=PERFORM-THRU carrying range_start.
func TestExtractor_PerformThruRange(t *testing.T) {
	src := "" +
		"       IDENTIFICATION DIVISION.\n" +
		"       PROGRAM-ID. RANGER.\n" +
		"       PROCEDURE DIVISION.\n" +
		"       MAIN-PARA.\n" +
		"           PERFORM STEP-A THRU STEP-C.\n" +
		"           PERFORM STEP-D THROUGH STEP-E.\n" +
		"           GOBACK.\n" +
		"       STEP-A.\n" +
		"           CONTINUE.\n" +
		"       STEP-C.\n" +
		"           CONTINUE.\n" +
		"       STEP-D.\n" +
		"           CONTINUE.\n" +
		"       STEP-E.\n" +
		"           CONTINUE.\n"
	recs := run(t, "ranger.cbl", src)

	// Start endpoints emitted via=PERFORM.
	if _, ok := relByViaTo(recs, "STEP-A", "PERFORM"); !ok {
		t.Error("expected PERFORM edge to range-start STEP-A")
	}
	// THRU end emitted via=PERFORM-THRU with range_start.
	end, ok := relByViaTo(recs, "STEP-C", "PERFORM-THRU")
	if !ok {
		t.Fatal("expected PERFORM-THRU edge to range-end STEP-C")
	}
	if end.Properties["range_start"] != "STEP-A" {
		t.Errorf("PERFORM-THRU edge to STEP-C missing range_start=STEP-A: %v", end.Properties)
	}
	// THROUGH spelling also works.
	if _, ok := relByViaTo(recs, "STEP-E", "PERFORM-THRU"); !ok {
		t.Error("expected PERFORM-THRU edge for THROUGH spelling to STEP-E")
	}
}

// TestExtractor_GoToControlFlow proves GO TO emits intra-program CALLS edges
// tagged via=GO-TO, including the DEPENDING ON multi-target form (#4946).
func TestExtractor_GoToControlFlow(t *testing.T) {
	src := "" +
		"       IDENTIFICATION DIVISION.\n" +
		"       PROGRAM-ID. BRANCHER.\n" +
		"       PROCEDURE DIVISION.\n" +
		"       MAIN-PARA.\n" +
		"           GO TO EXIT-PARA.\n" +
		"       DISPATCH-PARA.\n" +
		"           GO TO OPT-ONE OPT-TWO OPT-THREE DEPENDING ON WS-IDX.\n" +
		"       EXIT-PARA.\n" +
		"           GOBACK.\n" +
		"       OPT-ONE.\n" +
		"           CONTINUE.\n" +
		"       OPT-TWO.\n" +
		"           CONTINUE.\n" +
		"       OPT-THREE.\n" +
		"           CONTINUE.\n"
	recs := run(t, "brancher.cbl", src)

	for _, target := range []string{"EXIT-PARA", "OPT-ONE", "OPT-TWO", "OPT-THREE"} {
		if _, ok := relByViaTo(recs, target, "GO-TO"); !ok {
			t.Errorf("expected GO-TO CALLS edge to %q", target)
		}
	}
	// DEPENDING / ON must not become spurious targets.
	for _, notTarget := range []string{"DEPENDING", "ON", "WS-IDX"} {
		if _, ok := relByViaTo(recs, notTarget, "GO-TO"); ok {
			t.Errorf("GO TO produced a spurious GO-TO edge to %q", notTarget)
		}
	}
}

func TestExtractor_CallIsExternal(t *testing.T) {
	src := loadFixture(t, "payroll.cbl")
	recs := run(t, "payroll.cbl", src)

	for _, prog := range []string{"TAXCALC", "AUDITLOG"} {
		if !hasCallTo(recs, prog) {
			t.Errorf("expected CALL CALLS edge to external program %q", prog)
		}
	}
	var external bool
	for _, rel := range relationsByKind(recs, "CALLS") {
		if rel.ToID == "TAXCALC" {
			if rel.Properties["external"] != "true" || rel.Properties["via"] != "CALL" {
				t.Errorf("CALL edge to TAXCALC missing external/via props: %v", rel.Properties)
			}
			external = true
		}
	}
	if !external {
		t.Error("expected external CALL edge to TAXCALC")
	}
}

func TestExtractor_CopyIsImport(t *testing.T) {
	src := loadFixture(t, "payroll.cbl")
	recs := run(t, "payroll.cbl", src)

	imports := relationsByKind(recs, "IMPORTS")
	want := map[string]bool{"EMPREC": false, "TAXRULES": false}
	for _, rel := range imports {
		if _, ok := want[rel.ToID]; ok {
			want[rel.ToID] = true
		}
	}
	for book, found := range want {
		if !found {
			t.Errorf("expected COPY IMPORTS edge for copybook %q", book)
		}
	}
}

func TestExtractor_DataItems(t *testing.T) {
	src := loadFixture(t, "payroll.cbl")
	recs := run(t, "payroll.cbl", src)

	for _, field := range []string{"WS-EMP-COUNT", "WS-TOTAL-PAY", "EMP-ID", "EMP-NAME"} {
		if !hasEntity(recs, "SCOPE.Schema", "field", field) {
			t.Errorf("expected field %q", field)
		}
	}
	// FILLER and procedure-area identifiers must not be fields.
	if hasEntity(recs, "SCOPE.Schema", "field", "FILLER") {
		t.Error("FILLER must not be emitted as a field")
	}
}

func TestExtractor_CopybookFile(t *testing.T) {
	// A .cpy copybook (data only, no PROCEDURE DIVISION) still yields field
	// entities and never crashes on the absence of a PROGRAM-ID.
	src := loadFixture(t, "emprec.cpy")
	recs := run(t, "emprec.cpy", src)

	if !hasEntity(recs, "SCOPE.Schema", "field", "EMPLOYEE-MASTER") {
		t.Error("expected copybook field EMPLOYEE-MASTER")
	}
	if !hasEntity(recs, "SCOPE.Schema", "field", "EM-SALARY") {
		t.Error("expected copybook field EM-SALARY")
	}
	if len(findByKind(recs, "SCOPE.Operation", "paragraph")) != 0 {
		t.Error("data-only copybook must yield no paragraphs")
	}
}

func TestExtractor_CommentsAndSequenceArea(t *testing.T) {
	// A comment line ('*' in col 7) that mentions PERFORM must not produce
	// an edge; the sequence-number area must be stripped.
	src := "000100 IDENTIFICATION DIVISION.\n" +
		"000200 PROGRAM-ID. DEMO.\n" +
		"000300* PERFORM SHOULD-NOT-FIRE in a comment\n" +
		"000400 PROCEDURE DIVISION.\n" +
		"000500 MAIN-PARA.\n" +
		"000600     PERFORM REAL-PARA.\n" +
		"000700 REAL-PARA.\n" +
		"000800     DISPLAY 'HI'.\n"
	recs := run(t, "demo.cob", src)

	if !hasEntity(recs, "SCOPE.Component", "program", "DEMO") {
		t.Error("expected program DEMO after sequence-area strip")
	}
	if hasCallTo(recs, "SHOULD-NOT-FIRE") {
		t.Error("PERFORM in a comment line must not produce a CALLS edge")
	}
	if !hasCallTo(recs, "REAL-PARA") {
		t.Error("expected real PERFORM edge to REAL-PARA")
	}
}

func TestExtractor_EmptyInput(t *testing.T) {
	recs := run(t, "empty.cob", "")
	if len(recs) != 0 {
		t.Errorf("empty input must yield no entities, got %d", len(recs))
	}
}

func TestExtractor_LanguageTagging(t *testing.T) {
	src := loadFixture(t, "payroll.cbl")
	recs := run(t, "payroll.cbl", src)
	for _, r := range recs {
		if r.Language != "cobol" {
			t.Fatalf("entity %q language = %q, want cobol", r.Name, r.Language)
		}
		for _, rel := range r.Relationships {
			if rel.Properties["language"] != "cobol" {
				t.Fatalf("relationship %s->%s language not tagged cobol", rel.FromID, rel.ToID)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// #2838 Phase 2 — copybook resolution, embedded SQL, CICS depth, data hierarchy
// ---------------------------------------------------------------------------

// findDataAccess returns SCOPE.DataAccess entities (optionally by subtype).
func findDataAccess(recs []types.EntityRecord, subtype string) []types.EntityRecord {
	return findByKind(recs, "SCOPE.DataAccess", subtype)
}

func dataAccessFor(recs []types.EntityRecord, op, table string) bool {
	for _, r := range findDataAccess(recs, "embedded-sql") {
		if r.Properties["operation"] == op && r.Properties["table"] == table {
			return true
		}
	}
	return false
}

// TestExtractor_CopybookResolution proves COPY resolves against on-disk .cpy
// files: with RepoRoot=testdata, COPY EMPREC / COPY TAXRULES bind to the real
// files and the IMPORTS edge carries resolved=true + copybook_path. This is
// the import_resolution_quality partial→full move.
func TestExtractor_CopybookResolution(t *testing.T) {
	src := loadFixture(t, "ledger.cbl")
	recs := runWithRoot(t, "ledger.cbl", src, "testdata")

	for _, book := range []string{"EMPREC", "TAXRULES"} {
		rel, ok := importEdge(recs, book)
		if !ok {
			t.Fatalf("expected IMPORTS edge for %q", book)
		}
		if rel.Properties["resolved"] != "true" {
			t.Errorf("COPY %s expected resolved=true, got %q", book, rel.Properties["resolved"])
		}
		if rel.Properties["copybook_path"] == "" {
			t.Errorf("COPY %s expected copybook_path, got empty", book)
		}
		// A resolved COPY binds the edge ToID to the resolved file path.
		if rel.ToID != rel.Properties["copybook_path"] {
			t.Errorf("COPY %s ToID=%q should equal copybook_path=%q", book, rel.ToID, rel.Properties["copybook_path"])
		}
	}
}

// TestExtractor_CopybookUnresolvedStaysPartial proves that without a RepoRoot
// (no disk to resolve against) the edge is still emitted but marked
// resolved=false — honest degradation, no false "full".
func TestExtractor_CopybookUnresolved(t *testing.T) {
	src := loadFixture(t, "ledger.cbl")
	recs := run(t, "ledger.cbl", src) // no RepoRoot

	rel, ok := importEdge(recs, "EMPREC")
	if !ok {
		t.Fatal("expected IMPORTS edge for EMPREC even when unresolved")
	}
	if rel.Properties["resolved"] != "false" {
		t.Errorf("unresolved COPY expected resolved=false, got %q", rel.Properties["resolved"])
	}
	if rel.ToID != "EMPREC" {
		t.Errorf("unresolved COPY ToID should be bare name EMPREC, got %q", rel.ToID)
	}
}

// TestExtractor_CopybookReplacing proves the REPLACING clause is captured and
// its operand pairs normalized.
func TestExtractor_CopybookReplacing(t *testing.T) {
	src := loadFixture(t, "ledger.cbl")
	recs := runWithRoot(t, "ledger.cbl", src, "testdata")

	rel, ok := importEdge(recs, "EMPREC")
	if !ok {
		t.Fatal("expected IMPORTS edge for EMPREC")
	}
	if rel.Properties["replacing"] == "" {
		t.Error("expected REPLACING clause to be captured on COPY EMPREC")
	}
	if pairs := rel.Properties["replacing_pairs"]; pairs != "EM=>WS-EM" {
		t.Errorf("expected replacing_pairs=EM=>WS-EM, got %q", pairs)
	}
}

// TestExtractor_EmbeddedSQLTables proves EXEC SQL table references become
// SCOPE.DataAccess entities with ACCESSES_TABLE edges (db_effect precision).
func TestExtractor_EmbeddedSQLTables(t *testing.T) {
	src := loadFixture(t, "ledger.cbl")
	recs := runWithRoot(t, "ledger.cbl", src, "testdata")

	want := []struct{ op, table string }{
		{"SELECT", "LEDGER_ENTRY"},
		{"INSERT", "PAYROLL_LEDGER"},
		{"UPDATE", "ACCOUNT_BALANCE"},
		{"DELETE", "LEDGER_ENTRY"},
	}
	for _, w := range want {
		if !dataAccessFor(recs, w.op, w.table) {
			t.Errorf("expected SCOPE.DataAccess %s %s", w.op, w.table)
		}
	}
	// Every table access carries an ACCESSES_TABLE edge.
	if len(relationsByKind(recs, "ACCESSES_TABLE")) == 0 {
		t.Error("expected ACCESSES_TABLE edges for embedded SQL")
	}
}

// TestExtractor_EmbeddedSQLCursor proves DECLARE CURSOR yields a cursor
// SCOPE.DataAccess entity and OPEN/FETCH/CLOSE emit REFERENCES edges.
func TestExtractor_EmbeddedSQLCursor(t *testing.T) {
	src := loadFixture(t, "ledger.cbl")
	recs := runWithRoot(t, "ledger.cbl", src, "testdata")

	cursors := findDataAccess(recs, "cursor")
	var declared bool
	for _, c := range cursors {
		if c.Properties["operation"] == "DECLARE_CURSOR" && c.Properties["cursor"] == "LEDGER-CUR" {
			declared = true
		}
	}
	if !declared {
		t.Error("expected DECLARE_CURSOR SCOPE.DataAccess entity for LEDGER-CUR")
	}
	// OPEN / FETCH / CLOSE reference the cursor.
	ops := map[string]bool{}
	for _, rel := range relationsByKind(recs, "REFERENCES") {
		if rel.Properties["cursor"] == "LEDGER-CUR" {
			ops[rel.Properties["operation"]] = true
		}
	}
	for _, op := range []string{"OPEN", "FETCH", "CLOSE"} {
		if !ops[op] {
			t.Errorf("expected cursor REFERENCES edge for %s LEDGER-CUR", op)
		}
	}
}

// TestExtractor_CICSProgramTransfer proves EXEC CICS LINK/XCTL/START become
// external CALLS edges (CICS transaction-graph depth).
func TestExtractor_CICSProgramTransfer(t *testing.T) {
	src := loadFixture(t, "orderui.cbl")
	recs := run(t, "orderui.cbl", src)

	for _, prog := range []string{"PRICESVC", "ORDMENU"} {
		if !hasCallTo(recs, prog) {
			t.Errorf("expected CICS program-transfer CALLS edge to %q", prog)
		}
	}
	// START TRANSID('AUDT') schedules a transaction.
	var foundTransid bool
	for _, rel := range relationsByKind(recs, "CALLS") {
		if rel.ToID == "AUDT" && rel.Properties["transid"] == "AUDT" {
			foundTransid = true
		}
	}
	if !foundTransid {
		t.Error("expected CICS START TRANSID CALLS edge for AUDT")
	}
	// LINK edge carries via=EXEC-CICS-LINK + external.
	var linkTagged bool
	for _, rel := range relationsByKind(recs, "CALLS") {
		if rel.ToID == "PRICESVC" {
			if rel.Properties["via"] != "EXEC-CICS-LINK" || rel.Properties["external"] != "true" {
				t.Errorf("CICS LINK edge to PRICESVC missing via/external: %v", rel.Properties)
			}
			linkTagged = true
		}
	}
	if !linkTagged {
		t.Error("expected EXEC-CICS-LINK edge to PRICESVC")
	}
}

// TestExtractor_CICSQueues proves EXEC CICS READQ/WRITEQ TS surface a
// resolvable SCOPE.Datastore/queue and wire READS_FROM / WRITES_TO data-flow
// edges (cross-program queue coupling, #4947).
func TestExtractor_CICSQueues(t *testing.T) {
	src := loadFixture(t, "orderui.cbl")
	recs := run(t, "orderui.cbl", src)

	queues := findByKind(recs, "SCOPE.Datastore", "queue")
	if len(queues) != 1 {
		t.Fatalf("expected 1 queue datastore, got %d", len(queues))
	}
	q := queues[0]
	// The QUEUE operand is a data-item (WS-MSG-QUEUE), so dynamic_ref is set.
	if q.Properties["queue_type"] != "TS" {
		t.Errorf("queue_type = %q, want TS", q.Properties["queue_type"])
	}
	if q.Properties["storage"] != "cics-ts-queue" {
		t.Errorf("storage = %q, want cics-ts-queue", q.Properties["storage"])
	}
	if q.Properties["dynamic_ref"] != "true" {
		t.Errorf("expected dynamic_ref=true for data-item queue operand")
	}

	// READQ TS → READS_FROM; WRITEQ TS → WRITES_TO, both binding the queue.
	var readOK, writeOK bool
	for _, rel := range relationsByKind(recs, "READS_FROM") {
		if rel.ToID == q.QualifiedName && rel.Properties["via"] == "EXEC-CICS-READQ" {
			readOK = true
		}
	}
	for _, rel := range relationsByKind(recs, "WRITES_TO") {
		if rel.ToID == q.QualifiedName && rel.Properties["via"] == "EXEC-CICS-WRITEQ" {
			writeOK = true
		}
	}
	if !readOK {
		t.Error("expected READS_FROM edge for READQ TS queue")
	}
	if !writeOK {
		t.Error("expected WRITES_TO edge for WRITEQ TS queue")
	}
}

// TestExtractor_CICSScreenMaps proves EXEC CICS SEND/RECEIVE MAP surface a
// SCOPE.View/screen entity with RENDERS (SEND) / REFERENCES (RECEIVE) edges
// (BMS/MFS presentation layer, #4947).
func TestExtractor_CICSScreenMaps(t *testing.T) {
	src := loadFixture(t, "orderui.cbl")
	recs := run(t, "orderui.cbl", src)

	maps := findByKind(recs, "SCOPE.View", "screen")
	if len(maps) != 1 {
		t.Fatalf("expected 1 screen map view, got %d", len(maps))
	}
	m := maps[0]
	if m.Name != "ORDMAP" {
		t.Errorf("map name = %q, want ORDMAP", m.Name)
	}
	if m.Properties["ui"] != "bms" {
		t.Errorf("ui = %q, want bms", m.Properties["ui"])
	}
	// RECEIVE MAP('ORDMAP') → REFERENCES (operator input read back).
	var refOK bool
	for _, rel := range relationsByKind(recs, "REFERENCES") {
		if rel.ToID == m.QualifiedName && rel.Properties["via"] == "EXEC-CICS-RECEIVE" {
			refOK = true
		}
	}
	if !refOK {
		t.Error("expected REFERENCES edge for RECEIVE MAP ORDMAP")
	}
}

// TestExtractor_CICSScreenMapSend proves SEND MAP emits a RENDERS edge.
func TestExtractor_CICSScreenMapSend(t *testing.T) {
	src := "       IDENTIFICATION DIVISION.\n" +
		"       PROGRAM-ID. SCRNTEST.\n" +
		"       PROCEDURE DIVISION.\n" +
		"       SHOW-SCREEN.\n" +
		"           EXEC CICS SEND MAP('MENUMAP') MAPSET('MENUSET')\n" +
		"               FROM(WS-MENU-AREA)\n" +
		"           END-EXEC.\n"
	recs := run(t, "scrn.cbl", src)

	maps := findByKind(recs, "SCOPE.View", "screen")
	if len(maps) != 1 {
		t.Fatalf("expected 1 screen map view, got %d", len(maps))
	}
	m := maps[0]
	if m.Name != "MENUMAP" {
		t.Errorf("map name = %q, want MENUMAP", m.Name)
	}
	if m.Properties["mapset"] != "MENUSET" {
		t.Errorf("mapset = %q, want MENUSET", m.Properties["mapset"])
	}
	if m.Properties["dynamic_ref"] == "true" {
		t.Error("literal map operand must not be flagged dynamic_ref")
	}
	var renderOK bool
	for _, rel := range relationsByKind(recs, "RENDERS") {
		if rel.ToID == m.QualifiedName && rel.Properties["via"] == "EXEC-CICS-SEND" {
			renderOK = true
		}
	}
	if !renderOK {
		t.Error("expected RENDERS edge for SEND MAP MENUMAP")
	}
}

// TestExtractor_CICSQueueLiteralAndTD proves a literal TS/TD queue operand
// yields a non-dynamic queue and DELETEQ is a write-class mutation.
func TestExtractor_CICSQueueLiteralAndTD(t *testing.T) {
	src := "       IDENTIFICATION DIVISION.\n" +
		"       PROGRAM-ID. QTEST.\n" +
		"       PROCEDURE DIVISION.\n" +
		"       PROC-Q.\n" +
		"           EXEC CICS WRITEQ TD QUEUE('LOGQ')\n" +
		"               FROM(WS-REC)\n" +
		"           END-EXEC\n" +
		"           EXEC CICS DELETEQ TS QUEUE('TMPQ')\n" +
		"           END-EXEC.\n"
	recs := run(t, "qtest.cbl", src)

	byName := map[string]types.EntityRecord{}
	for _, q := range findByKind(recs, "SCOPE.Datastore", "queue") {
		byName[q.Name] = q
	}
	logq, ok := byName["LOGQ"]
	if !ok {
		t.Fatal("expected LOGQ TD queue")
	}
	if logq.Properties["queue_type"] != "TD" {
		t.Errorf("LOGQ queue_type = %q, want TD", logq.Properties["queue_type"])
	}
	if logq.Properties["dynamic_ref"] == "true" {
		t.Error("literal LOGQ operand must not be dynamic_ref")
	}
	// DELETEQ TS → WRITES_TO (a mutation of the queue).
	tmpq, ok := byName["TMPQ"]
	if !ok {
		t.Fatal("expected TMPQ TS queue")
	}
	var delOK bool
	for _, rel := range relationsByKind(recs, "WRITES_TO") {
		if rel.ToID == tmpq.QualifiedName && rel.Properties["via"] == "EXEC-CICS-DELETEQ" {
			delOK = true
		}
	}
	if !delOK {
		t.Error("expected WRITES_TO edge for DELETEQ TS TMPQ")
	}
}

// TestExtractor_DataHierarchy proves 01/05 nesting binds child fields to their
// parent group (parent property + CONTAINS edge) and captures REDEFINES/OCCURS.
func TestExtractor_DataHierarchy(t *testing.T) {
	src := loadFixture(t, "payroll.cbl")
	recs := run(t, "payroll.cbl", src)

	// EMP-ID (05) is nested under EMP-RECORD (01).
	var found bool
	for _, r := range findByKind(recs, "SCOPE.Schema", "field") {
		if r.Name == "EMP-ID" {
			found = true
			if r.Properties["parent"] != "EMP-RECORD" {
				t.Errorf("EMP-ID parent = %q, want EMP-RECORD", r.Properties["parent"])
			}
		}
	}
	if !found {
		t.Fatal("expected field EMP-ID")
	}
	// The 01 group records the CONTAINS edge to its children.
	var contains bool
	for _, r := range findByKind(recs, "SCOPE.Schema", "field") {
		if r.Name == "EMP-RECORD" {
			for _, rel := range r.Relationships {
				if rel.Kind == "CONTAINS" && rel.Properties["child"] == "EMP-ID" {
					contains = true
				}
			}
		}
	}
	if !contains {
		t.Error("expected CONTAINS edge EMP-RECORD -> EMP-ID")
	}
}

// TestExtractor_DataRedefinesOccurs proves REDEFINES/OCCURS structured-field
// metadata is captured.
func TestExtractor_DataRedefinesOccurs(t *testing.T) {
	src := "       DATA DIVISION.\n" +
		"       WORKING-STORAGE SECTION.\n" +
		"       01  WS-BUFFER.\n" +
		"           05  WS-LINES OCCURS 10 TIMES PIC X(80).\n" +
		"       01  WS-ALIAS REDEFINES WS-BUFFER PIC X(800).\n"
	recs := run(t, "redef.cbl", src)

	var occursOK, redefOK bool
	for _, r := range findByKind(recs, "SCOPE.Schema", "field") {
		if r.Name == "WS-LINES" && r.Properties["occurs"] == "10" {
			occursOK = true
		}
		if r.Name == "WS-ALIAS" && r.Properties["redefines"] == "WS-BUFFER" {
			redefOK = true
		}
	}
	if !occursOK {
		t.Error("expected OCCURS 10 on WS-LINES")
	}
	if !redefOK {
		t.Error("expected REDEFINES WS-BUFFER on WS-ALIAS")
	}
}

// TestExtractor_FileControlSelect proves FILE-CONTROL SELECT...ASSIGN clauses
// emit a resolvable SCOPE.Datastore/file resource whose assign_to is the
// JCL-DD-matching coupling key (#4908).
func TestExtractor_FileControlSelect(t *testing.T) {
	src := loadFixture(t, "payroll.cbl")
	recs := run(t, "payroll.cbl", src)

	files := findByKind(recs, "SCOPE.Datastore", "file")
	if len(files) != 2 {
		t.Fatalf("expected 2 file resources, got %d", len(files))
	}
	want := map[string]string{"EMP-FILE": "EMPIN", "PAY-FILE": "PAYOUT"}
	for _, f := range files {
		exp, ok := want[f.Name]
		if !ok {
			t.Errorf("unexpected file resource %q", f.Name)
			continue
		}
		if f.Properties["assign_to"] != exp {
			t.Errorf("%s assign_to = %q, want %q", f.Name, f.Properties["assign_to"], exp)
		}
		if f.Properties["organization"] != "SEQUENTIAL" {
			t.Errorf("%s organization = %q, want SEQUENTIAL", f.Name, f.Properties["organization"])
		}
		if f.Properties["storage"] != "sequential" {
			t.Errorf("%s storage = %q, want sequential", f.Name, f.Properties["storage"])
		}
	}
}

// TestExtractor_FileIODataFlow proves OPEN/READ/WRITE on a logical file wire
// READS_FROM / WRITES_TO edges to the file resource (#4908).
func TestExtractor_FileIODataFlow(t *testing.T) {
	src := loadFixture(t, "payroll.cbl")
	recs := run(t, "payroll.cbl", src)

	reads := relationsByKind(recs, "READS_FROM")
	writes := relationsByKind(recs, "WRITES_TO")
	hasFile := func(rels []types.RelationshipRecord, file string) bool {
		for _, r := range rels {
			if r.Properties["file"] == file {
				return true
			}
		}
		return false
	}
	// OPEN INPUT EMP-FILE + READ EMP-FILE → READS_FROM.
	if !hasFile(reads, "EMP-FILE") {
		t.Error("expected READS_FROM edge for EMP-FILE")
	}
	// OPEN OUTPUT PAY-FILE → WRITES_TO.
	if !hasFile(writes, "PAY-FILE") {
		t.Error("expected WRITES_TO edge for PAY-FILE")
	}
}

// TestExtractor_VSAMKsds proves an INDEXED ORGANIZATION with a RECORD KEY is
// classified as VSAM storage and captures access mode + key (#4908).
func TestExtractor_VSAMKsds(t *testing.T) {
	src := "       ENVIRONMENT DIVISION.\n" +
		"       INPUT-OUTPUT SECTION.\n" +
		"       FILE-CONTROL.\n" +
		"           SELECT CUST-MASTER ASSIGN TO CUSTVS\n" +
		"               ORGANIZATION IS INDEXED\n" +
		"               ACCESS MODE IS DYNAMIC\n" +
		"               RECORD KEY IS CM-CUST-ID.\n" +
		"       DATA DIVISION.\n"
	recs := run(t, "vsam.cbl", src)

	files := findByKind(recs, "SCOPE.Datastore", "file")
	if len(files) != 1 {
		t.Fatalf("expected 1 file resource, got %d", len(files))
	}
	f := files[0]
	if f.Name != "CUST-MASTER" {
		t.Errorf("file name = %q, want CUST-MASTER", f.Name)
	}
	if f.Properties["assign_to"] != "CUSTVS" {
		t.Errorf("assign_to = %q, want CUSTVS", f.Properties["assign_to"])
	}
	if f.Properties["storage"] != "vsam" {
		t.Errorf("storage = %q, want vsam", f.Properties["storage"])
	}
	if f.Properties["organization"] != "INDEXED" {
		t.Errorf("organization = %q, want INDEXED", f.Properties["organization"])
	}
	if f.Properties["access_mode"] != "DYNAMIC" {
		t.Errorf("access_mode = %q, want DYNAMIC", f.Properties["access_mode"])
	}
	if f.Properties["record_key"] != "CM-CUST-ID" {
		t.Errorf("record_key = %q, want CM-CUST-ID", f.Properties["record_key"])
	}
}

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return string(b)
}
