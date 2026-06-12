package substrate

import "testing"

// effectsByFn collapses sniffer output into fn -> set-of-effects.
func cobolEffectsByFn(content string) map[string]map[Effect]bool {
	out := map[string]map[Effect]bool{}
	for _, m := range sniffEffectsCobol(content) {
		if out[m.Function] == nil {
			out[m.Function] = map[Effect]bool{}
		}
		out[m.Function][m.Effect] = true
	}
	return out
}

func TestSniffEffectsCobol_FileIO(t *testing.T) {
	src := `       PROCEDURE DIVISION.
       READ-DATA.
           OPEN INPUT EMP-FILE
           READ EMP-FILE.
       WRITE-DATA.
           WRITE PAY-RECORD
           REWRITE PAY-RECORD.
`
	got := cobolEffectsByFn(src)
	if !got["READ-DATA"][EffectFSRead] {
		t.Errorf("READ-DATA expected fs_read, got %v", got["READ-DATA"])
	}
	if !got["WRITE-DATA"][EffectFSWrite] {
		t.Errorf("WRITE-DATA expected fs_write, got %v", got["WRITE-DATA"])
	}
}

func TestSniffEffectsCobol_EmbeddedSQL(t *testing.T) {
	src := `       PROCEDURE DIVISION.
       READ-LEDGER.
           EXEC SQL
               SELECT AMOUNT INTO :WS-AMT FROM LEDGER
           END-EXEC.
       WRITE-LEDGER.
           EXEC SQL
               INSERT INTO LEDGER (AMOUNT) VALUES (:WS-AMT)
           END-EXEC.
`
	got := cobolEffectsByFn(src)
	if !got["READ-LEDGER"][EffectDBRead] {
		t.Errorf("READ-LEDGER expected db_read, got %v", got["READ-LEDGER"])
	}
	if !got["WRITE-LEDGER"][EffectDBWrite] {
		t.Errorf("WRITE-LEDGER expected db_write, got %v", got["WRITE-LEDGER"])
	}
}

func TestSniffEffectsCobol_CICS(t *testing.T) {
	src := `       PROCEDURE DIVISION.
       CALL-SERVICE.
           EXEC CICS LINK PROGRAM('SUBPGM') END-EXEC.
`
	got := cobolEffectsByFn(src)
	if !got["CALL-SERVICE"][EffectHTTPOut] {
		t.Errorf("CALL-SERVICE expected http_out (CICS LINK), got %v", got["CALL-SERVICE"])
	}
}

func TestSniffEffectsCobol_Mutation(t *testing.T) {
	src := `       PROCEDURE DIVISION.
       UPDATE-COUNT.
           MOVE ZERO TO WS-COUNT.
`
	got := cobolEffectsByFn(src)
	if !got["UPDATE-COUNT"][EffectMutation] {
		t.Errorf("UPDATE-COUNT expected mutation, got %v", got["UPDATE-COUNT"])
	}
}

// TestSniffEffectsCobol_MutationExpanded proves the expanded mutation verb set
// (#4946): arithmetic GIVING, STRING/UNSTRING INTO, INITIALIZE, and INSPECT
// REPLACING each register a mutation effect on their paragraph.
func TestSniffEffectsCobol_MutationExpanded(t *testing.T) {
	cases := map[string]string{
		"ADD-GIVING": `       PROCEDURE DIVISION.
       ADD-GIVING.
           ADD WS-A TO WS-B GIVING WS-TOTAL.
`,
		"SUB-GIVING": `       PROCEDURE DIVISION.
       SUB-GIVING.
           SUBTRACT WS-A FROM WS-B GIVING WS-NET.
`,
		"MUL-GIVING": `       PROCEDURE DIVISION.
       MUL-GIVING.
           MULTIPLY WS-A BY WS-B GIVING WS-PROD.
`,
		"DIV-GIVING": `       PROCEDURE DIVISION.
       DIV-GIVING.
           DIVIDE WS-A INTO WS-B GIVING WS-Q.
`,
		"STRING-INTO": `       PROCEDURE DIVISION.
       STRING-INTO.
           STRING WS-A WS-B DELIMITED BY SIZE INTO WS-OUT.
`,
		"UNSTRING-INTO": `       PROCEDURE DIVISION.
       UNSTRING-INTO.
           UNSTRING WS-IN DELIMITED BY ',' INTO WS-A WS-B.
`,
		"INIT": `       PROCEDURE DIVISION.
       INIT.
           INITIALIZE WS-RECORD.
`,
		"INSPECT-REPL": `       PROCEDURE DIVISION.
       INSPECT-REPL.
           INSPECT WS-TEXT REPLACING ALL ' ' BY '-'.
`,
	}
	for fn, src := range cases {
		got := cobolEffectsByFn(src)
		if !got[fn][EffectMutation] {
			t.Errorf("%s expected mutation effect, got %v", fn, got[fn])
		}
	}
}

// TestSniffEffectsCobol_CICSFileIO proves EXEC CICS file/queue I/O
// (READ/READQ → fs_read, WRITE/WRITEQ/REWRITE → fs_write) is surfaced as
// filesystem effects, deepening CICS beyond the http_out flag (#2838).
func TestSniffEffectsCobol_CICSFileIO(t *testing.T) {
	src := `       PROCEDURE DIVISION.
       LOAD-ORDER.
           EXEC CICS READ FILE('ORDFILE') INTO(WS-REC) END-EXEC
           EXEC CICS READQ TS QUEUE(WS-Q) INTO(WS-REC) END-EXEC.
       PERSIST-ORDER.
           EXEC CICS WRITE FILE('ORDFILE') FROM(WS-REC) END-EXEC
           EXEC CICS WRITEQ TS QUEUE(WS-Q) FROM(WS-REC) END-EXEC.
       CALL-SVC.
           EXEC CICS LINK PROGRAM('PRICESVC') END-EXEC.
`
	got := cobolEffectsByFn(src)
	if !got["LOAD-ORDER"][EffectFSRead] {
		t.Errorf("LOAD-ORDER expected fs_read (CICS READ/READQ), got %v", got["LOAD-ORDER"])
	}
	if !got["PERSIST-ORDER"][EffectFSWrite] {
		t.Errorf("PERSIST-ORDER expected fs_write (CICS WRITE/WRITEQ), got %v", got["PERSIST-ORDER"])
	}
	// LINK still registers as http_out (transaction/service transfer).
	if !got["CALL-SVC"][EffectHTTPOut] {
		t.Errorf("CALL-SVC expected http_out (CICS LINK), got %v", got["CALL-SVC"])
	}
}

func TestSniffEffectsCobol_Registered(t *testing.T) {
	if EffectSnifferFor("cobol") == nil {
		t.Fatal("cobol effect sniffer not registered")
	}
}

func TestSniffEffectsCobol_Empty(t *testing.T) {
	if got := sniffEffectsCobol(""); got != nil {
		t.Errorf("empty content must yield nil, got %v", got)
	}
}
