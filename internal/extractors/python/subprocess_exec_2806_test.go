// subprocess_exec_2806_test.go — issue #2806 coverage.
//
// `subprocess.run(['libreoffice', ...])` and friends invoke an external OS
// binary, not an indexed Python symbol. The extractor must NOT emit a CALLS
// edge whose ToID is the bare method leaf (run / Popen / system / ...) because
// the resolver would then bind it to an unrelated same-named in-repo entity
// (the phantom edge convert_to_pdf -> scripts/migrate_and_seed.py:293 in the
// iter9 bench q04). Instead the call resolves to an ext:<binary> External node
// (when the binary name is a readable string/list literal) or is dropped.

package python

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// callToIDs returns the set of CALLS ToIDs emitted from callerName.
func callToIDs(ents []types.EntityRecord, callerName string) map[string]bool {
	out := map[string]bool{}
	for _, c := range findCallsFrom(ents, callerName) {
		out[c.ToID] = true
	}
	return out
}

// TestSubprocessRunListLiteral_ResolvesToExternal is the canonical #2806
// repro: convert_to_pdf calls subprocess.run(['libreoffice', ...]) and there
// is an unrelated module-level `run()` function in the SAME file (modelling
// the migrate_and_seed.py:293 collision). The CALLS edge must go to
// ext:libreoffice, never to the in-repo `run`.
func TestSubprocessRunListLiteral_ResolvesToExternal(t *testing.T) {
	src := `import subprocess

def run():
    """Unrelated in-repo function whose name collides with subprocess.run."""
    return 1

def convert_to_pdf(path):
    subprocess.run(['libreoffice', '--headless', '--convert-to', 'pdf', path])
`
	ents := extractEntities(t, "app/pdf.py", src)
	ids := callToIDs(ents, "convert_to_pdf")
	if !ids["ext:libreoffice"] {
		t.Errorf("convert_to_pdf should CALL ext:libreoffice, got %v", ids)
	}
	if ids["run"] {
		t.Errorf("phantom edge: convert_to_pdf must NOT CALL the in-repo `run` leaf; got %v", ids)
	}
}

// TestSubprocessVariants covers the subprocess.* and os.* exec families,
// string-command and list-argv shapes, aliased imports, program-path
// basenames, and the dynamic-argv drop (no edge).
func TestSubprocessVariants(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		caller string
		want   string // expected ext: ToID; "" means "no subprocess edge at all"
	}{
		{
			name:   "os.system string command",
			src:    "import os\ndef f():\n    os.system('convert in.png out.pdf')\n",
			caller: "f",
			want:   "ext:convert",
		},
		{
			name:   "subprocess.Popen list argv",
			src:    "import subprocess\ndef f():\n    subprocess.Popen(['ffmpeg', '-i', 'x'])\n",
			caller: "f",
			want:   "ext:ffmpeg",
		},
		{
			name:   "subprocess.check_output string",
			src:    "import subprocess\ndef f():\n    subprocess.check_output('git status')\n",
			caller: "f",
			want:   "ext:git",
		},
		{
			name:   "aliased subprocess import",
			src:    "import subprocess as sp\ndef f():\n    sp.run(['rsync', '-a'])\n",
			caller: "f",
			want:   "ext:rsync",
		},
		{
			name:   "os.execv program path basename",
			src:    "import os\ndef f():\n    os.execv('/usr/bin/python3', ['python3', '-V'])\n",
			caller: "f",
			want:   "ext:python3",
		},
		{
			name:   "dynamic argv variable - dropped",
			src:    "import subprocess\ndef f(cmd):\n    subprocess.run(cmd)\n",
			caller: "f",
			want:   "", // no readable binary, and no leaf edge either
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := extractEntities(t, "app/x.py", tc.src)
			ids := callToIDs(ents, tc.caller)
			// The leaf method name must never appear as a CALLS target.
			for _, leaf := range []string{"run", "Popen", "system", "check_output", "execv"} {
				if ids[leaf] {
					t.Errorf("%s: phantom leaf edge %q present in %v", tc.name, leaf, ids)
				}
			}
			if tc.want == "" {
				for id := range ids {
					if strings.HasPrefix(id, "ext:") {
						t.Errorf("%s: expected no subprocess edge, got %q", tc.name, id)
					}
				}
				return
			}
			if !ids[tc.want] {
				t.Errorf("%s: want CALLS %q, got %v", tc.name, tc.want, ids)
			}
		})
	}
}
