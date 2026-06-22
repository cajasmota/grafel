//go:build windows

package wiztui

// console_windows.go switches the Windows console to the UTF-8 code page
// (65001) for the duration of the TUI so that — on a modern CMD/conhost with a
// TrueType font, or in Windows Terminal — the Unicode glyphs render instead of
// showing as mojibake. The previous input/output code pages are restored when
// the returned function runs (deferred by the caller), so we don't leave the
// user's console in an unexpected state after the wizard exits (#5340).

import "golang.org/x/sys/windows"

// enableUTF8Console sets the console output AND input code pages to UTF-8 and
// returns a restore func that puts the previous code pages back. Any failure is
// non-fatal: we simply skip that half and the restore func becomes a no-op for
// it (the ASCII glyph fallback still keeps legacy consoles readable).
func enableUTF8Console() func() {
	const cpUTF8 = 65001

	prevOut, outErr := windows.GetConsoleOutputCP()
	if outErr == nil {
		if err := windows.SetConsoleOutputCP(cpUTF8); err != nil {
			outErr = err
		}
	}

	prevIn, inErr := windows.GetConsoleCP()
	if inErr == nil {
		if err := windows.SetConsoleCP(cpUTF8); err != nil {
			inErr = err
		}
	}

	return func() {
		if outErr == nil {
			_ = windows.SetConsoleOutputCP(prevOut)
		}
		if inErr == nil {
			_ = windows.SetConsoleCP(prevIn)
		}
	}
}
