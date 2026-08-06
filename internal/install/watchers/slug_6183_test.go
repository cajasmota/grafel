package watchers

import (
	"path/filepath"
	"testing"
)

// TestLabel_DistinctPathsSameBasenameDoNotCollide is the core of #6183.
//
// slugify used to reduce a repo path to filepath.Base, so /x/my-repo and
// /y/my_repo (both basename-slugged to "my-repo") produced ONE label — hence
// one plist filename and one launchd job identity — for two different repos.
func TestLabel_DistinctPathsSameBasenameDoNotCollide(t *testing.T) {
	a := Unit{Group: "g", Repo: filepath.FromSlash("/x/my-repo"), BinPath: "/bin/grafel"}
	b := Unit{Group: "g", Repo: filepath.FromSlash("/y/my_repo"), BinPath: "/bin/grafel"}

	if a.Label() == b.Label() {
		t.Fatalf("distinct repo paths share a label: %s", a.Label())
	}

	pa, err := UnitPath(a)
	if err != nil {
		t.Skipf("unsupported OS: %v", err)
	}
	pb, _ := UnitPath(b)
	if pa == pb {
		t.Fatalf("distinct repo paths share a unit path: %s", pa)
	}
}

// TestLabel_StableAndReadable pins the two properties the disambiguation must
// not trade away: the basename stays visible in the label (a bare hash for
// every repo would be a usability regression), and the label is a pure
// function of (group, path) so it does not drift between runs.
func TestLabel_StableAndReadable(t *testing.T) {
	u := Unit{Group: "g", Repo: filepath.FromSlash("/x/my-repo"), BinPath: "/bin/grafel"}
	got := u.Label()
	if got != u.Label() {
		t.Fatalf("Label() is not deterministic")
	}
	if !contains(got, "com.grafel.watcher.g.my-repo") {
		t.Fatalf("label lost the human-readable basename: %s", got)
	}
	// Adding an unrelated repo to the fleet must not change this one's label:
	// Label() depends on nothing but the unit's own fields.
	other := Unit{Group: "g", Repo: filepath.FromSlash("/y/my-repo"), BinPath: "/bin/grafel"}
	if other.Label() == got {
		t.Fatalf("collision persists: %s", got)
	}
	if u.Label() != got {
		t.Fatalf("label changed after another unit was derived")
	}
}

// TestLabel_CharsetIsLabelSafe keeps the disambiguator inside the character
// set launchd/systemd/schtasks tolerate in a job identity and a filename.
func TestLabel_CharsetIsLabelSafe(t *testing.T) {
	u := Unit{Group: "g", Repo: filepath.FromSlash("/x/My Repo & Co"), BinPath: "/bin/grafel"}
	for _, c := range u.Label() {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '.':
		default:
			t.Fatalf("label contains unsafe rune %q: %s", c, u.Label())
		}
	}
}

// TestLegacyOf_ReproducesPreFixLabel guarantees the migration can name the unit
// it has to clean up. Without an exact legacy derivation the only alternative
// is globbing com.grafel.watcher.* and guessing, which risks booting out units
// that belong to somebody else's repo.
func TestLegacyOf_ReproducesPreFixLabel(t *testing.T) {
	u := Unit{Group: "g", Repo: filepath.FromSlash("/x/my-repo"), BinPath: "/bin/grafel"}
	if got, want := LegacyOf(u).Label(), "com.grafel.watcher.g.my-repo"; got != want {
		t.Fatalf("legacy label = %q, want %q", got, want)
	}
	if LegacyOf(u).Label() == u.Label() {
		t.Fatalf("legacy label equals current label; migration would be a no-op")
	}
	// LegacyOf must not mutate the receiver or lose fields.
	l := LegacyOf(u)
	if l.Repo != u.Repo || l.Group != u.Group || l.BinPath != u.BinPath {
		t.Fatalf("LegacyOf dropped fields: %+v", l)
	}
	if u.Label() == l.Label() {
		t.Fatalf("receiver was mutated")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s[:len(sub)] == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
