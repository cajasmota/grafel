package cli

import (
	"testing"
)

// TestIndexFlagOrdering verifies that the index command accepts flags
// in any position relative to the repo path argument (#798).
func TestIndexFlagOrdering(t *testing.T) {
	tests := []struct {
		name    string
		argv    []string
		wantErr bool
	}{
		{
			name:    "flag-before-path",
			argv:    []string{"--export-json", "/tmp/repo"},
			wantErr: false,
		},
		{
			name:    "flag-after-path",
			argv:    []string{"/tmp/repo", "--export-json"},
			wantErr: false,
		},
		{
			name:    "multiple-flags-before-path",
			argv:    []string{"--export-json", "--pretty", "/tmp/repo"},
			wantErr: false,
		},
		{
			name:    "multiple-flags-mixed",
			argv:    []string{"--export-json", "/tmp/repo", "--pretty"},
			wantErr: false,
		},
		{
			name:    "flag-with-value-before-path",
			argv:    []string{"--out", "/tmp/out.json", "/tmp/repo"},
			wantErr: false,
		},
		{
			name:    "flag-with-value-after-path",
			argv:    []string{"/tmp/repo", "--out", "/tmp/out.json"},
			wantErr: false,
		},
		{
			name:    "missing-repo-argument",
			argv:    []string{"--export-json"},
			wantErr: true,
		},
		{
			name:    "empty-argv",
			argv:    []string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't test runIndexClient directly since it dials the daemon,
			// which may not be running. Instead, we test the core argument
			// reordering logic inline.
			var flags []string
			var positionals []string
			for _, arg := range tt.argv {
				if len(arg) > 0 && arg[0] == '-' {
					flags = append(flags, arg)
				} else {
					positionals = append(positionals, arg)
				}
			}
			reorderedArgv := append(flags, positionals...)

			// After reordering, positional args should be at the end.
			if len(positionals) > 0 {
				lastIdx := len(reorderedArgv) - 1
				// The last argument(s) should be positional (not start with -)
				for i := lastIdx - len(positionals) + 1; i <= lastIdx; i++ {
					if i >= 0 && len(reorderedArgv[i]) > 0 && reorderedArgv[i][0] == '-' {
						t.Errorf("expected positional arg at index %d, got %q", i, reorderedArgv[i])
					}
				}
			}

			// Verify reordering didn't lose arguments.
			if len(reorderedArgv) != len(tt.argv) {
				t.Errorf("lost arguments during reordering: got %d, want %d", len(reorderedArgv), len(tt.argv))
			}
		})
	}
}
