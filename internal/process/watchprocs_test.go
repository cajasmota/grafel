package process

import "testing"

func TestParseWatchArgs(t *testing.T) {
	cases := []struct {
		name     string
		argv     []string
		wantRepo string
		wantOK   bool
	}{
		{
			name:     "plain watch repo",
			argv:     []string{"/opt/grafel", "watch", "/work/repo-a"},
			wantRepo: "/work/repo-a", wantOK: true,
		},
		{
			name:     "flag before repo (value form)",
			argv:     []string{"grafel", "watch", "--interval", "10s", "/work/repo-a"},
			wantRepo: "/work/repo-a", wantOK: true,
		},
		{
			name:     "flag=value before repo",
			argv:     []string{"grafel", "watch", "--group=mygroup", "/work/repo-a"},
			wantRepo: "/work/repo-a", wantOK: true,
		},
		{
			name:     "repo then trailing flags",
			argv:     []string{"grafel", "watch", "/work/repo-a", "--interval=30s"},
			wantRepo: "/work/repo-a", wantOK: true,
		},
		{
			name:   "not a watch invocation",
			argv:   []string{"grafel", "index", "/work/repo-a"},
			wantOK: false,
		},
		{
			name:   "watch with no repo arg",
			argv:   []string{"grafel", "watch", "--interval", "10s"},
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, ok := parseWatchArgs(tc.argv)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && repo != tc.wantRepo {
				t.Fatalf("repo = %q, want %q", repo, tc.wantRepo)
			}
		})
	}
}
