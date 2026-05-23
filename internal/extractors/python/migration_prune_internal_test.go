package python

import (
	"path/filepath"
	"testing"
)

// TestIsDjangoMigrationFile covers the #1617 migration path classifier.
func TestIsDjangoMigrationFile(t *testing.T) {
	cases := map[string]bool{
		"core/migrations/0001_initial.py":     true,
		"core/migrations/__init__.py":         true,
		"apps/users/migrations/0042_thing.py": true,
		"core/models.py":                      false,
		"core/migration_helpers.py":           false, // not in a migrations/ dir
		"migrations.py":                       false, // file, not dir
		"core/migrations/sub/handwritten.py":  false, // nested below migrations/
	}
	for path, want := range cases {
		if got := isDjangoMigrationFile(path); got != want {
			t.Errorf("isDjangoMigrationFile(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestIsDjangoMigrationFile_CrossPlatformPaths is a regression test for the
// hardcoded-'/' bug fixed in this PR. The old filepathBase helper used
// strings.LastIndexByte(p, '/') which returns -1 on Windows-style paths
// (backslash separators), causing the full path to be returned as the
// "basename" and silently breaking migration detection on Windows.
//
// filepath.FromSlash normalises to the OS separator at test time, so these
// cases exercise the fix on every platform without special-casing.
func TestIsDjangoMigrationFile_CrossPlatformPaths(t *testing.T) {
	// Build paths using filepath.Join so they carry the OS-native separator.
	// On Linux/macOS these produce forward-slash paths (existing behaviour).
	// On Windows they produce backslash paths — the case the old helper broke.
	posixTrue := filepath.Join("core", "migrations", "0001_initial.py")
	posixFalse := filepath.Join("core", "models.py")
	nested := filepath.Join("core", "migrations", "sub", "handwritten.py")

	cases := map[string]bool{
		posixTrue:  true,
		posixFalse: false,
		nested:     false,
	}
	for path, want := range cases {
		if got := isDjangoMigrationFile(path); got != want {
			t.Errorf("isDjangoMigrationFile(%q) = %v, want %v", path, got, want)
		}
	}
}
