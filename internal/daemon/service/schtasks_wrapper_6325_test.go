package service

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// decodeUTF16LEBOM decodes b as UTF-16LE with a mandatory BOM. It is the
// test-side mirror of what Windows Script Host does when it sees the BOM.
func decodeUTF16LEBOM(t *testing.T, b []byte) string {
	t.Helper()
	if len(b) < 2 || b[0] != 0xFF || b[1] != 0xFE {
		t.Fatalf("wrapper does not start with a UTF-16LE BOM (FF FE); first bytes = % X", b[:min(8, len(b))])
	}
	if len(b)%2 != 0 {
		t.Fatalf("wrapper byte length %d is odd — not UTF-16", len(b))
	}
	u := make([]uint16, 0, (len(b)-2)/2)
	for i := 2; i < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u))
}

// #6325 F2: WSH decodes a .vbs using the system ANSI codepage unless the file
// carries a UTF-16LE BOM. install.ps1 puts the binary under %USERPROFILE%, so
// any user with an accented or non-Latin account name has a non-ASCII BinPath
// by default. Mis-decoded, shell.Run cannot find the executable, //B suppresses
// the error dialog, RestartOnFailure burns its three attempts, and the daemon
// is dead with no console and no log.
func TestGenerateDaemonWrapper_UTF16LEWithBOM(t *testing.T) {
	for _, tc := range []struct {
		name string
		bin  string
	}{
		{"ascii", `C:\Program Files\grafel\grafel.exe`},
		{"latin1_accent", `C:\Users\José\.grafel\bin\grafel.exe`},
		{"cjk", `C:\Users\張偉\.grafel\bin\grafel.exe`},
		{"astral_plane", `C:\Users\𝕵ose\.grafel\bin\grafel.exe`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GenerateDaemonWrapper(Options{BinPath: tc.bin})
			if err != nil {
				t.Fatalf("GenerateDaemonWrapper: %v", err)
			}
			// 1. BOM present and little-endian.
			if len(got) < 2 || got[0] != 0xFF || got[1] != 0xFE {
				t.Fatalf("wrapper is not UTF-16LE-with-BOM; WSH would decode it as ANSI and "+
					"a non-ASCII BinPath would be mangled (#6325 F2). first bytes = % X", got[:min(8, len(got))])
			}
			// 2. Really 16-bit code units, not UTF-8 with a BOM glued on:
			//    every ASCII character must be followed by a 0x00 high byte.
			if got[2] != 'O' || got[3] != 0x00 {
				t.Fatalf("wrapper body is not UTF-16LE encoded (expected 'O' 0x00 after the BOM, got % X)", got[2:min(8, len(got))])
			}
			// 3. Round-trips back to the original path.
			text := decodeUTF16LEBOM(t, got)
			if !strings.Contains(text, tc.bin) {
				t.Fatalf("BinPath did not round-trip through the wrapper encoding.\nwant substring: %q\ngot:\n%s", tc.bin, text)
			}
			if !strings.Contains(text, `""`+tc.bin+`"" serve`) {
				t.Fatalf("wrapper does not invoke the quoted binary with `serve`:\n%s", text)
			}
		})
	}
}

// TestGenerateDaemonWrapper_RejectsControlBytes mirrors validateUnitFields in
// internal/install/watchers/watchers.go:577-605 (#6185): there is no per-value
// escape for a control character inside a VBScript string literal, so the only
// correct fix is to refuse to render the wrapper at all. vbsString doubles `"`
// and nothing else — a newline terminates the literal early and everything
// after it becomes executable VBScript.
func TestGenerateDaemonWrapper_RejectsControlBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		bin  string
	}{
		{"newline_injection", "C:\\g.exe\", 0, True\nCreateObject(\"WScript.Shell\").Run \"calc.exe\"\n'"},
		{"bare_lf", "C:\\g\n.exe"},
		{"cr", "C:\\g\r.exe"},
		{"nul", "C:\\g\x00.exe"},
		{"tab", "C:\\g\t.exe"},
		{"del", "C:\\g\x7f.exe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GenerateDaemonWrapper(Options{BinPath: tc.bin})
			if err == nil {
				t.Fatalf("GenerateDaemonWrapper accepted a BinPath containing a control byte "+
					"and rendered:\n%q\nIt must refuse (#6325 F4, mirroring #6185)", got)
			}
			if !strings.Contains(err.Error(), "control character") {
				t.Errorf("error does not name the cause: %v", err)
			}
			if got != nil {
				t.Errorf("GenerateDaemonWrapper returned %d bytes alongside its error; it must render nothing", len(got))
			}
		})
	}
}

// TestGenerateDaemonWrapper_RejectsDoubleQuote covers the third escaping
// context #6325 F4 calls out. vbsString correctly doubles `"` for VBScript,
// but the resulting WScript.Shell.Run argument is then unbalanced for
// CreateProcess. `"` is illegal in a Win32 path, so refusing costs nothing and
// keeps the renderer honest about what it can escape.
func TestGenerateDaemonWrapper_RejectsDoubleQuote(t *testing.T) {
	got, err := GenerateDaemonWrapper(Options{BinPath: `C:\a"b\grafel.exe`})
	if err == nil {
		t.Fatalf("GenerateDaemonWrapper accepted a BinPath containing a double quote:\n%q", got)
	}
	if !strings.Contains(err.Error(), "double quote") {
		t.Errorf("error does not name the cause: %v", err)
	}
}

// TestGenerateDaemonWrapper_Deterministic — WriteUnit's idempotency contract
// depends on the renderer being a pure function of Options.
func TestGenerateDaemonWrapper_Deterministic(t *testing.T) {
	opts := Options{BinPath: `C:\Users\José\.grafel\bin\grafel.exe`}
	a, err := GenerateDaemonWrapper(opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateDaemonWrapper(opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Error("GenerateDaemonWrapper is not deterministic")
	}
}
