package service

import (
	"fmt"
	"os"
	"strings"
	"text/template"
	"unicode/utf16"

	"github.com/cajasmota/grafel/internal/atomicfile"
)

// This file holds the VBScript-wrapper renderer for the Windows scheduled
// task. It carries no build tag on purpose: the renderer is a pure function of
// Options, and #6320's review found that keeping it behind //go:build windows
// left it with zero substantive test coverage on the machines contributors
// actually develop on. The genuinely Windows-specific code — the schtasks
// calls, the %LOCALAPPDATA% path derivation, WriteUnit — stays in
// schtasks_windows.go.

const daemonWrapperTemplate = `Option Explicit
Dim shell, exitCode
Set shell = CreateObject("WScript.Shell")
exitCode = shell.Run("{{.Command}}", 0, True)
WScript.Quit exitCode
`

// vbsString escapes a value for embedding in a VBScript double-quoted string
// literal. Doubling the quote character is the ONLY escape VBScript string
// literals have — there is no backslash escape and no way to represent a
// newline inside a literal — which is why validateWrapperFields refuses the
// characters this cannot cover rather than pretending to escape them.
func vbsString(value string) string {
	return strings.ReplaceAll(value, `"`, `""`)
}

// hasControlByte reports whether s contains a C0 control byte or DEL.
func hasControlByte(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// validateWrapperFields rejects BinPath values the VBScript renderer cannot
// represent. It mirrors validateUnitFields in internal/install/watchers
// (#6185) deliberately, including its conclusion: "There is no per-value
// escape for a control character the way there is for '%', so the only
// correct fix is to refuse to render/persist the unit at all."
//
//   - Control bytes: a VBScript string literal is line-terminated. An embedded
//     newline closes the literal early and everything after it on the next
//     physical line is parsed as executable VBScript.
//   - Double quote: vbsString escapes it correctly for the VBScript layer, but
//     the string it builds is then handed to WScript.Shell.Run, which passes it
//     to CreateProcess — a second, independent quoting context in which the
//     embedded quote leaves the argument unbalanced. Rather than claim to
//     handle two escaping layers with one escape (the pre-#6325 comment did),
//     refuse. Neither a control byte nor '"' is legal in a Win32 path
//     component, so nothing reachable via os.Executable() is being rejected.
//
// The XML layer needs no equivalent guard: xml.EscapeText keeps a control
// character inside character data instead of creating new structure.
func validateWrapperFields(opts Options) error {
	if hasControlByte(opts.BinPath) {
		return fmt.Errorf("daemon wrapper field BinPath contains a control character, refusing to render " +
			"(it would terminate the VBScript string literal and inject executable statements)")
	}
	if strings.Contains(opts.BinPath, `"`) {
		return fmt.Errorf("daemon wrapper field BinPath contains a double quote, refusing to render " +
			"(the VBScript escape cannot also balance the CreateProcess command line)")
	}
	return nil
}

// utf16LEWithBOM encodes s as UTF-16 little-endian prefixed with a U+FEFF BOM.
//
// Windows Script Host decodes a .vbs using the system ANSI codepage unless the
// file opens with a UTF-16LE BOM. install.ps1 places the binary under
// %USERPROFILE%, so every user whose account name is non-ASCII has a non-ASCII
// BinPath by default; decoded as cp1252 a UTF-8 "José" becomes "JosÃ©",
// shell.Run cannot find the executable, //B suppresses the error dialog,
// RestartOnFailure burns its three attempts, and the daemon is dead with no
// console and no log to say why. See #6325.
func utf16LEWithBOM(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, 2+2*len(units))
	out = append(out, 0xFF, 0xFE) // U+FEFF, little-endian
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// GenerateDaemonWrapper renders the .vbs launcher that the scheduled task
// executes, as UTF-16LE-with-BOM bytes ready to write to disk.
// Exported for testing; production code calls WriteUnit.
func GenerateDaemonWrapper(opts Options) ([]byte, error) {
	if err := validateWrapperFields(opts); err != nil {
		return nil, err
	}
	// WScript.Shell.Run receives one command-line string. Quote the executable
	// path so CreateProcess treats a path with spaces as one argument, then
	// escape the whole thing for the VBScript literal that carries it.
	command := `"` + opts.BinPath + `" serve`
	tmpl, err := template.New("wrapper").Parse(daemonWrapperTemplate)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, struct{ Command string }{Command: vbsString(command)}); err != nil {
		return nil, err
	}
	return utf16LEWithBOM(buf.String()), nil
}

// writeServiceArtifact publishes one on-disk task artifact via temp+rename.
//
// os.WriteFile truncates the destination in place, and both artifact paths are
// fixed (taskWrapperPath is a pure function of the constant taskName), so the
// destination is precisely the file the already-registered task points at. The
// truncate-then-write window is readable by the OS at arbitrary times: a
// LogonTrigger firing or a RestartOnFailure retry landing inside it hands
// wscript.exe a truncated .vbs, or schtasks a truncated XML. atomicfile.WriteFile
// is the repo-standard helper for this class and carries the Windows
// rename-retry handling (#6018/#6053). See #6325.
func writeServiceArtifact(path string, data []byte, perm os.FileMode) error {
	return atomicfile.WriteFile(path, data, perm)
}
