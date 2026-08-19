package substrate

import "testing"

// Epic #6327 S2 — the substrate half of the classification mapping.
//
// This pins the path→slug resolution only. No sniffer is registered for
// "vbnet" (S3+), so substrate propagation still does nothing for a `.vb` file;
// TestVBNetHasNoSnifferYet states that plainly so the mapping is not misread
// as substrate support.
func TestLanguageForPathResolvesVB(t *testing.T) {
	for _, path := range []string{"legacy/Form1.vb", "a.vb"} {
		if got := LanguageForPath(path); got != "vbnet" {
			t.Fatalf("LanguageForPath(%q) = %q, want %q", path, got, "vbnet")
		}
	}
}

// The extensions S2 deliberately did not claim. `.vbproj` is MSBuild XML
// (`.csproj` is likewise absent here); `.bas`/`.cls` are VB6/VBA and, for
// `.cls`, also Apex and LaTeX. See internal/classifier/classifier.go.
func TestLanguageForPathDoesNotClaimVBAdjacentExtensions(t *testing.T) {
	for _, path := range []string{"App.vbproj", "Module1.bas", "Class1.cls", "s.vbs"} {
		if got := LanguageForPath(path); got == "vbnet" {
			t.Errorf("LanguageForPath(%q) = %q, want anything but vbnet", path, got)
		}
	}
}

// TestVBNetHasNoSnifferYet is the anti-overclaim guard for this package: the
// mapping exists, the substrate capability does not. S3+ registers a sniffer
// and deletes this test.
func TestVBNetHasNoSnifferYet(t *testing.T) {
	if SnifferFor("vbnet") != nil {
		t.Fatal("a vbnet sniffer is registered — S2 adds a path mapping only; " +
			"delete this test in the change that registers the sniffer")
	}
	for _, l := range Languages() {
		if l == "vbnet" {
			t.Fatal("Languages() lists vbnet; see above")
		}
	}
}
