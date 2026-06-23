package main

import "testing"

// TestLoadLock_RealManifest parses the committed grammars.lock to guard against
// the manifest and the parser drifting apart.
func TestLoadLock_RealManifest(t *testing.T) {
	l, err := loadLock("../../grammars.lock")
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}
	if l.Binding.PinnedDate != "2024-08-27" {
		t.Errorf("binding pinned_date = %q, want 2024-08-27", l.Binding.PinnedDate)
	}
	if len(l.Grammars) != 27 {
		t.Errorf("grammar count = %d, want 27", len(l.Grammars))
	}
	for _, g := range l.Grammars {
		if g.Language == "" || g.Source == "" {
			t.Errorf("grammar entry missing language/source: %+v", g)
		}
	}
}
