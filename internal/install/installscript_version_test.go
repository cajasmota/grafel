// installscript_version_test.go locks the tag-extraction *string operations*
// performed by the Windows installer (install.bat) when it resolves the latest
// release version. We cannot run cmd.exe in CI, so instead we model the exact
// string transforms the batch script applies and assert they recover the tag
// from the real GitHub "location:" redirect header — guarding against the
// regression in #5318, where `%~nx` (a '\'-separated Windows PATH modifier) was
// applied to a '/'-separated URL and produced garbage (e.g. "LOC:=") → a 404
// download.
//
// install.bat resolves the tag with the equivalent of:
//
//	LOC     = <value of the "location:" header, CR stripped>
//	VERSION = LOC with everything up to and including "/tag/" removed
//	          ( batch: set "VERSION=!LOC:*/tag/=!" )
//
// followed by a sanity guard: VERSION must start with 'v', must NOT contain
// '/', and must contain a digit.
package install

import (
	"strings"
	"testing"
)

// batExtractTagFromLocation mirrors `set "VERSION=!LOC:*/tag/=!"` after the CR
// scrub: it strips everything up to and including the first "/tag/". When the
// marker is absent the batch substring replace is a no-op, so we return the
// input unchanged (which the sanity guard then rejects).
func batExtractTagFromLocation(loc string) string {
	loc = strings.TrimRight(loc, "\r")
	const marker = "/tag/"
	if i := strings.Index(loc, marker); i >= 0 {
		return loc[i+len(marker):]
	}
	return loc
}

// batVersionLooksValid mirrors the install.bat sanity guard: starts with 'v',
// contains no '/', and contains at least one digit.
func batVersionLooksValid(v string) bool {
	if !strings.HasPrefix(v, "v") {
		return false
	}
	if strings.Contains(v, "/") {
		return false
	}
	return strings.ContainsAny(v, "0123456789")
}

func TestBatExtractTagFromLocation(t *testing.T) {
	cases := []struct {
		name   string
		loc    string
		want   string
		wantOK bool
	}{
		{
			name:   "real redirect header value",
			loc:    "https://github.com/cajasmota/grafel/releases/tag/v0.1.2",
			want:   "v0.1.2",
			wantOK: true,
		},
		{
			name:   "trailing CR is scrubbed before extraction",
			loc:    "https://github.com/cajasmota/grafel/releases/tag/v0.1.2\r",
			want:   "v0.1.2",
			wantOK: true,
		},
		{
			name:   "two-digit minor/patch",
			loc:    "https://github.com/cajasmota/grafel/releases/tag/v10.20.30\r",
			want:   "v10.20.30",
			wantOK: true,
		},
		{
			name:   "unexpected redirect without /tag/ is left intact and rejected",
			loc:    "https://github.com/login\r",
			want:   "https://github.com/login",
			wantOK: false, // contains '/', sanity guard rejects → triggers fallback/error
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := batExtractTagFromLocation(tc.loc)
			if got != tc.want {
				t.Fatalf("batExtractTagFromLocation(%q) = %q, want %q", tc.loc, got, tc.want)
			}
			if ok := batVersionLooksValid(got); ok != tc.wantOK {
				t.Fatalf("batVersionLooksValid(%q) = %v, want %v", got, ok, tc.wantOK)
			}
		})
	}
}

// TestBatRejectsNxStyleGarbage documents the #5318 regression: the OLD logic
// applied `%~nx` to the URL and yielded garbage. Whatever garbage slips through,
// the sanity guard must reject it BEFORE building a (404) download URL.
func TestBatRejectsNxStyleGarbage(t *testing.T) {
	garbage := []string{
		"LOC:=",  // the exact value the tester observed on Windows 11 26200
		"",       // empty
		"grafel", // no leading v, no digit
		"https://github.com/cajasmota/grafel/releases/tag/v0.1.2", // full URL kept (no extraction)
	}
	for _, g := range garbage {
		if batVersionLooksValid(g) {
			t.Fatalf("sanity guard accepted garbage version %q; it must be rejected", g)
		}
	}
}

// batStripQuotes mirrors `set "RAW=!RAW:"=!"` in the API fallback: remove all
// double quotes from the JSON string token.
func batStripQuotes(s string) string { return strings.ReplaceAll(s, `"`, "") }

// TestBatAPIFallbackTagExtraction models the API fallback's token handling:
// `for /f "tokens=2 delims=:, "` over a `"tag_name": "vX.Y.Z",` line yields the
// quoted version token, then the quotes are stripped.
func TestBatAPIFallbackTagExtraction(t *testing.T) {
	// token2 as cmd.exe would split `"tag_name": "v0.1.2",` on delims `:`,`,`,` `.
	token2 := `"v0.1.2"`
	got := batStripQuotes(token2)
	if got != "v0.1.2" {
		t.Fatalf("API fallback extraction = %q, want %q", got, "v0.1.2")
	}
	if !batVersionLooksValid(got) {
		t.Fatalf("API fallback produced invalid version %q", got)
	}
}
