package watchers

// guard_allowlist_test.go pins the guard's inversion from a denylist of known
// mutating verbs to an allowlist of known read-only ones, and the env belt that
// covers the case testing.Testing() cannot see.

import (
	"strings"
	"testing"
)

// TestGuard_UnlistedVerbsArePanics: the first cut called itself "fail-closed"
// while documenting "anything not listed is allowed" — the opposite. It was
// complete for the verbs in use and silently open to every other one.
// `launchctl submit`, `setenv`, `kill` and `config` all mutate and were all
// absent from the denylist.
func TestGuard_UnlistedVerbsArePanics(t *testing.T) {
	for _, verb := range []string{"submit", "setenv", "kill", "config", "reboot", "somethingnew"} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("guardServiceCall(%q) must panic — an unrecognised verb is not known to be safe", verb)
				}
			}()
			guardServiceCall("launchctl", []string{verb, "gui/0/x"})
		}()
	}
}

// TestGuard_ReadOnlyVerbsPassForEveryTool: the allowlist must be keyed on the
// VERB, not on every argument, or the flags and unit names that systemctl and
// schtasks pass would each look like an unrecognised verb.
func TestGuard_ReadOnlyVerbsPassForEveryTool(t *testing.T) {
	cases := []struct {
		tool string
		args []string
	}{
		{"launchctl", []string{"list", "com.grafel.watcher.a.b"}},
		{"launchctl", []string{"print", "gui/501/com.grafel.watcher.a.b"}},
		{"launchctl", []string{"print-disabled", "gui/501"}},
		{"systemctl", []string{"--user", "is-active", "--quiet", "com.grafel.watcher.a.b.service"}},
		{"systemctl", []string{"--user", "is-enabled", "x.service"}},
		{"schtasks", []string{"/query", "/tn", "com.grafel.watcher.a.b"}},
		{"schtasks", []string{"/query", "/fo", "csv", "/v", "/tn", "x"}},
	}
	for _, c := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("guardServiceCall(%q, %v) must not panic: %v", c.tool, c.args, r)
				}
			}()
			guardServiceCall(c.tool, c.args)
		}()
	}
}

// TestGuard_MutatingVerbsPanicForEveryTool: schtasks was listed in the verb
// table but nothing ever called guardServiceCall("schtasks", ...) — the entries
// were dead and misleading. The windows loader is guarded now, so the table
// entries have to mean something.
func TestGuard_MutatingVerbsPanicForEveryTool(t *testing.T) {
	cases := []struct {
		tool string
		args []string
	}{
		{"launchctl", []string{"bootout", "gui/501/x"}},
		{"launchctl", []string{"bootstrap", "gui/501", "/tmp/x.plist"}},
		{"systemctl", []string{"--user", "disable", "--now", "x.service"}},
		{"systemctl", []string{"--user", "daemon-reload"}},
		{"schtasks", []string{"/create", "/xml", "/tmp/x.xml", "/tn", "x", "/f"}},
		{"schtasks", []string{"/delete", "/tn", "x", "/f"}},
		{"schtasks", []string{"/end", "/tn", "x"}},
	}
	for _, c := range cases {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("guardServiceCall(%q, %v) did not panic", c.tool, c.args)
				}
				if !strings.Contains(r.(string), c.tool) {
					t.Fatalf("panic message does not name the tool: %v", r)
				}
			}()
			guardServiceCall(c.tool, c.args)
		}()
	}
}

// TestGuard_EnvBeltActivatesOutsideGoTest: testing.Testing() is false in a
// binary produced by `go build`, and several tests build ./cmd/grafel and exec
// it. Today those harnesses only run `grafel daemon`; the moment one runs
// `grafel stop`/`install`/`update` the guard is bypassed completely and
// silently. The env belt is what those harnesses export.
func TestGuard_EnvBeltActivatesOutsideGoTest(t *testing.T) {
	t.Setenv(NoServiceMutationEnv, "1")
	if !serviceMutationGuardActive() {
		t.Fatal("guard must be active when the env belt is set")
	}
	t.Setenv(NoServiceMutationEnv, "")
	if !serviceMutationGuardActive() {
		t.Fatal("guard must still be active under `go test` with the env unset")
	}
}
