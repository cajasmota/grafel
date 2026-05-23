package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var osMkdirAll = os.MkdirAll

// TestResolveGroup_CWDFromRegistry verifies that when cwd is inside a
// registered repo path, the group is inferred without an explicit group=
// argument and without a .archigraph/group.json marker (#1650).
func TestResolveGroup_CWDFromRegistry(t *testing.T) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "upvate_core")
	if err := mkdirp(repoPath); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	reg := &Registry{
		Groups: map[string]RegistryGroup{
			"upvate": {
				Repos: map[string]RegistryRepo{
					"upvate-core": {Path: repoPath},
				},
			},
			"polyglot-platform": {
				Repos: map[string]RegistryRepo{
					"other": {Path: filepath.Join(tmp, "other-root")},
				},
			},
		},
	}
	st := NewState(reg)

	// Bare cwd inside the registered repo path → resolve "upvate" via registry.
	subdir := filepath.Join(repoPath, "src", "views")
	if err := mkdirp(subdir); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	g, src, err := resolveGroup(st, "", subdir)
	if err != nil {
		t.Fatalf("resolveGroup: %v", err)
	}
	if g != "upvate" {
		t.Errorf("group: want upvate, got %s", g)
	}
	if src != "cwd_registry" {
		t.Errorf("source: want cwd_registry, got %s", src)
	}

	// Outside any registered repo → ambiguous error (two groups registered).
	_, _, err = resolveGroup(st, "", filepath.Join(tmp, "nowhere"))
	if err == nil {
		t.Errorf("expected ambiguous-group error for cwd outside any repo")
	}
}

// TestResolveGroup_InferFromCWD is the table-driven suite for #1746 CWD group
// inference. It covers all four key cases described in the issue:
//
//  1. explicit group= is set → returns that group, no cwd lookup.
//  2. cwd inside exactly one registered repo's group → inferred unambiguously.
//  3. cwd inside repos registered to two different groups → error listing
//     the candidate groups.
//  4. cwd is under no registered repo (and multiple groups exist) → ambiguous
//     error listing all registered groups.
func TestResolveGroup_InferFromCWD(t *testing.T) {
	tmp := t.TempDir()

	repoA := filepath.Join(tmp, "repo_a") // group alpha
	repoB := filepath.Join(tmp, "repo_b") // group beta
	repoC := filepath.Join(tmp, "repo_c") // ALSO group beta (multi-repo group)
	// shared sits inside both repoA AND repoB trees by having both as ancestors
	// — achieved by making repoShared a subdir of repoA AND registering repoA
	// under a second group (gamma) to force cross-group ambiguity.
	repoAGamma := repoA // same physical path, second group (gamma)

	for _, p := range []string{repoA, repoB, repoC} {
		if err := mkdirp(p); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	reg := &Registry{
		Groups: map[string]RegistryGroup{
			"alpha": {
				Repos: map[string]RegistryRepo{
					"repo-a": {Path: repoA},
				},
			},
			"beta": {
				Repos: map[string]RegistryRepo{
					"repo-b": {Path: repoB},
					"repo-c": {Path: repoC},
				},
			},
			"gamma": {
				// gamma also registers repoA → cwd inside repoA is ambiguous
				// between alpha and gamma.
				Repos: map[string]RegistryRepo{
					"repo-a-mirror": {Path: repoAGamma},
				},
			},
		},
	}
	st := NewState(reg)

	subdirA := filepath.Join(repoA, "pkg", "handler")
	subdirB := filepath.Join(repoB, "src")
	nowhere := filepath.Join(tmp, "totally-unrelated")
	for _, p := range []string{subdirA, subdirB} {
		if err := mkdirp(p); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	cases := []struct {
		name         string
		explicit     string
		cwd          string
		wantGroup    string
		wantSource   string
		wantErrFrag  string // non-empty → expect error containing this substring
	}{
		{
			name:       "explicit group overrides cwd",
			explicit:   "beta",
			cwd:        subdirA, // would resolve alpha/gamma if not for explicit
			wantGroup:  "beta",
			wantSource: "explicit",
		},
		{
			name:       "cwd inside single-group repo resolves unambiguously",
			explicit:   "",
			cwd:        subdirB,
			wantGroup:  "beta",
			wantSource: "cwd_registry",
		},
		{
			name:        "cwd inside repo registered to two groups errors with candidates",
			explicit:    "",
			cwd:         subdirA,
			wantErrFrag: "ambiguous group",
		},
		{
			name:        "cwd under no registered repo errors with all groups",
			explicit:    "",
			cwd:         nowhere,
			wantErrFrag: "ambiguous group",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, src, err := resolveGroup(st, tc.explicit, tc.cwd)
			if tc.wantErrFrag != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil (group=%q)", tc.wantErrFrag, g)
				}
				if !strings.Contains(err.Error(), tc.wantErrFrag) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrFrag)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if g != tc.wantGroup {
				t.Errorf("group: want %q, got %q", tc.wantGroup, g)
			}
			if src != tc.wantSource {
				t.Errorf("source: want %q, got %q", tc.wantSource, src)
			}
		})
	}
}

// TestResolveGroup_AmbiguousCWDIncludesCandidates verifies that when cwd is
// inside repos registered to multiple groups, the error message names those
// specific candidate groups (not all registered groups) (#1746).
func TestResolveGroup_AmbiguousCWDIncludesCandidates(t *testing.T) {
	tmp := t.TempDir()
	sharedRepo := filepath.Join(tmp, "shared")
	if err := mkdirp(sharedRepo); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	reg := &Registry{
		Groups: map[string]RegistryGroup{
			"groupA": {
				Repos: map[string]RegistryRepo{"r": {Path: sharedRepo}},
			},
			"groupB": {
				Repos: map[string]RegistryRepo{"r": {Path: sharedRepo}},
			},
			"unrelated": {
				Repos: map[string]RegistryRepo{"other": {Path: filepath.Join(tmp, "other")}},
			},
		},
	}
	st := NewState(reg)

	cwd := filepath.Join(sharedRepo, "sub")
	if err := mkdirp(cwd); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, _, err := resolveGroup(st, "", cwd)
	if err == nil {
		t.Fatal("expected ambiguous-group error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "groupA") {
		t.Errorf("error %q missing groupA", msg)
	}
	if !strings.Contains(msg, "groupB") {
		t.Errorf("error %q missing groupB", msg)
	}
	// "unrelated" is NOT a candidate (its repo path doesn't cover cwd) — it
	// must NOT appear in the targeted error message.
	if strings.Contains(msg, "unrelated") {
		t.Errorf("error %q unexpectedly lists unrelated group", msg)
	}
}

func mkdirp(p string) error {
	return osMkdirAll(p, 0o755)
}
