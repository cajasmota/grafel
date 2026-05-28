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
