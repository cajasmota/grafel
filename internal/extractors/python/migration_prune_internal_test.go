package python

import "testing"

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
