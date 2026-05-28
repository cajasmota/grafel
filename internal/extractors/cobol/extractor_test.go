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
	e := &Extractor{}
	recs, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "cobol",
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	return recs
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

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return string(b)
}
