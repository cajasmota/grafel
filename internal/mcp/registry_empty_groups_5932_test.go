package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Issue #5932: Registry.UnmarshalJSON discriminated CLI-array format from legacy
// map format on `len(raw.Groups) > 0` rather than on the JSON shape. A CLI-format
// registry with zero groups — {"version":1,"groups":[]}, exactly what a fresh
// install or a `grafel remove` of the last group writes — therefore fell through
// to the legacy branch, where unmarshalling a JSON array into
// map[string]RegistryGroup fails. LoadRegistry returned an error, NewServer
// aborted, and the MCP session degraded to a single sentinel tool for the rest of
// its life (a client reconnect does not recover it).
//
// MUTATION ORACLE: restore the `len(raw.Groups) > 0` condition → every subtest
// here fails with "invalid format (neither CLI array nor legacy map)".
func TestRegistryUnmarshal_EmptyCLIGroupsIsNotAnError(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"cli empty array with version", `{"version":1,"groups":[]}`},
		{"cli empty array no version", `{"groups":[]}`},
		{"cli empty array whitespace", `{"version":1,"groups": [ ]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r Registry
			if err := json.Unmarshal([]byte(tc.data), &r); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.data, err)
			}
			if r.Groups == nil {
				t.Fatal("Groups is nil; callers range over it and expect a usable empty map")
			}
			if len(r.Groups) != 0 {
				t.Fatalf("Groups has %d entries, want 0", len(r.Groups))
			}
		})
	}
}

// The populated CLI-array and legacy-map formats must keep working unchanged —
// the shape discriminator must not have traded one format for another.
func TestRegistryUnmarshal_FormatsStillDiscriminated(t *testing.T) {
	t.Run("cli array populated", func(t *testing.T) {
		var r Registry
		if err := json.Unmarshal([]byte(`{"version":1,"groups":[{"name":"acme","config_path":""}]}`), &r); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		g, ok := r.Groups["acme"]
		if !ok {
			t.Fatalf("group 'acme' missing; got %v", r.Groups)
		}
		if g.Repos == nil {
			t.Fatal("CLI-format group has nil Repos map")
		}
	})
	t.Run("legacy map populated", func(t *testing.T) {
		var r Registry
		data := `{"groups":{"acme":{"repos":{"api":{"path":"/tmp/api"}}}}}`
		if err := json.Unmarshal([]byte(data), &r); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got := r.Groups["acme"].Repos["api"].Path; got != "/tmp/api" {
			t.Fatalf("legacy repo path = %q, want /tmp/api", got)
		}
	})
	t.Run("legacy empty map", func(t *testing.T) {
		var r Registry
		if err := json.Unmarshal([]byte(`{"groups":{}}`), &r); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if r.Groups == nil || len(r.Groups) != 0 {
			t.Fatalf("Groups = %v, want empty non-nil map", r.Groups)
		}
	})
	t.Run("genuinely malformed still errors", func(t *testing.T) {
		var r Registry
		if err := json.Unmarshal([]byte(`{"groups":"nope"}`), &r); err == nil {
			t.Fatal("a string 'groups' unmarshalled without error; the format guard is gone")
		}
	})
}

// End-to-end on the path that actually bricked the session: LoadRegistry over a
// real on-disk empty CLI registry must succeed, so NewServer does not abort and
// the full toolset gets registered.
func TestLoadRegistry_EmptyCLIRegistryFileLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"groups":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry on an empty CLI registry: %v", err)
	}
	if reg.Groups == nil {
		t.Fatal("LoadRegistry returned a nil Groups map")
	}
	if len(reg.Groups) != 0 {
		t.Fatalf("LoadRegistry returned %d groups, want 0", len(reg.Groups))
	}
	if reg.Path != path {
		t.Fatalf("Path = %q, want %q", reg.Path, path)
	}
}
