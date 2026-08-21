package classifier

// #6448 — `.aspx` was named in the unsupported-language table but its four
// siblings were not, so half a WebForms repo reported as "ASP.NET Web Forms"
// and the other half as a bare extension tally. Two problems on screen, one
// problem in fact.
//
// Direction 1 pins each sibling to the SAME label `.aspx` carries. Direction 2
// pins that the label is looked up per extension rather than defaulted: an
// unknown extension must name nothing, or these rows would only be asserting
// "some string came back".

import (
	"context"
	"testing"
)

// webFormsSiblings is the #6448 batch: extension → the name the report prints.
var webFormsSiblings = map[string]string{
	".ascx":   "ASP.NET Web Forms", // user controls
	".asmx":   "ASP.NET Web Forms", // legacy web services
	".ashx":   "ASP.NET Web Forms", // HTTP handlers
	".master": "ASP.NET Web Forms", // master pages
}

func TestWebFormsSiblings_ReportedWithALanguageName(t *testing.T) {
	cls := New(nil)
	for ext, want := range webFormsSiblings {
		if got := LanguageDisplayName(ext); got != want {
			t.Errorf("LanguageDisplayName(%q) = %q, want %q", ext, got, want)
		}
		res := cls.ClassifyWithSize(context.Background(), "webforms/probe"+ext, 512)
		if !res.Skip {
			t.Errorf("%s: Skip = false, want true", ext)
			continue
		}
		if res.SkipReason != SkipReasonUnsupportedLanguage {
			t.Errorf("%s: SkipReason = %q, want %q (the generic reason hides the technology)", ext, res.SkipReason, SkipReasonUnsupportedLanguage)
		}
		tal := NewUnsupportedTally()
		tal.Observe("webforms/probe"+ext, res)
		if tal.Counts()[ext] != 1 {
			t.Errorf("%s: not tallied, counts = %v", ext, tal.Counts())
		}
	}
}

// The permissive direction: a label must come from the table, not from a
// fallback. If an unknown extension can borrow the WebForms label, the rows
// above prove nothing about WHICH label was returned.
func TestWebFormsSiblings_LabelIsNotADefault(t *testing.T) {
	for _, ext := range []string{".aspq", ".ascxx", ".masterpage", ".webforms"} {
		if got := LanguageDisplayName(ext); got != "" {
			t.Errorf("LanguageDisplayName(%q) = %q, want \"\" — the label must be per-extension, not a fallback", ext, got)
		}
	}
}

// Same registry invariant the #6344 batch carries: none of these may be an
// extension the router already claims, or the entry is dead weight.
func TestWebFormsSiblings_AreNotRoutedExtensions(t *testing.T) {
	for ext := range webFormsSiblings {
		if lang := LanguageForExtension(ext); lang != "" {
			t.Errorf("%s routes to %q — supported extensions must not be listed as unsupported languages", ext, lang)
		}
	}
}
