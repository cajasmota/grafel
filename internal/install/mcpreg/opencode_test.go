package mcpreg

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

// ── opencode tests (#6730) ────────────────────────────────────────────────────
//
// opencode's config is `.json`, so without a dedicated arm it would fall into
// the generic JSON writer and be written wrong in four ways at once: the
// top-level key would be `mcpServers` rather than `mcp`, `command` would be a
// string with a sibling `args` array rather than a single argv array, and
// `type` would be "stdio" rather than "local".
//
// Since opencode v1.18.16 unknown top-level fields are IGNORED rather than
// failing config parsing, so every one of those mistakes writes successfully,
// passes any "did the file get written" check, and simply never loads the
// server. That is why these tests decode and assert the exact shape.

// opencodePath returns the user-global opencode config path under the
// isolated home, creating its parent directory (so the host reads as
// "installed").
func opencodePath(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "opencode.json")
}

// decodeOpencode reads path and returns the raw decoded document. Assertions
// are made on the parsed structure, never on a substring of the file: a string
// match would pass on `"mcpServers"` too, which is precisely the bug.
func decodeOpencode(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("opencode config is not valid JSON: %v\n%s", err, b)
	}
	return doc
}

// TestOpencode_SettingsPathIsXDG pins the user-global path.
func TestOpencode_SettingsPathIsXDG(t *testing.T) {
	home := withHome(t)
	got, err := SettingsPath(Opencode)
	if err != nil {
		t.Fatalf("SettingsPath(Opencode): %v", err)
	}
	want := filepath.Join(home, ".config", "opencode", "opencode.json")
	if got != want {
		t.Fatalf("SettingsPath(Opencode) = %q, want %q", got, want)
	}
}

// TestOpencode_WritesExactSchemaShape is the load-bearing test for this arm.
// It asserts every axis of the opencode McpLocalConfig schema independently:
// the container key, the absence of the JSON-world key, the `type` enum value,
// `command` being a two-element argv array, and the absence of `args`.
func TestOpencode_WritesExactSchemaShape(t *testing.T) {
	home := withHome(t)
	path := opencodePath(t, home)

	if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	doc := decodeOpencode(t, path)

	if _, bad := doc["mcpServers"]; bad {
		t.Fatalf("opencode config contains the JSON-world key \"mcpServers\"; "+
			"opencode ignores unknown top-level fields, so this would silently "+
			"never load. doc = %v", doc)
	}
	mcp, ok := doc["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("top-level \"mcp\" object missing; doc = %v", doc)
	}
	entry, ok := mcp[ServerName].(map[string]any)
	if !ok {
		t.Fatalf("mcp.%s missing or not an object; mcp = %v", ServerName, mcp)
	}

	if got := entry["type"]; got != "local" {
		t.Fatalf("mcp.%s.type = %v, want \"local\" (the schema's only local enum value)", ServerName, got)
	}
	if _, bad := entry["args"]; bad {
		t.Fatalf("mcp.%s has an \"args\" key; McpLocalConfig sets additionalProperties:false "+
			"and has no args — argv belongs in command. entry = %v", ServerName, entry)
	}
	cmd, ok := entry["command"].([]any)
	if !ok {
		t.Fatalf("mcp.%s.command = %#v, want a JSON array (opencode's command is argv, not a string)",
			ServerName, entry["command"])
	}
	if len(cmd) != 2 || cmd[0] != "/usr/local/bin/grafel" || cmd[1] != "mcp-bridge" {
		t.Fatalf("mcp.%s.command = %v, want [/usr/local/bin/grafel mcp-bridge]", ServerName, cmd)
	}
	// Only the two required keys; `enabled` is deliberately omitted (it
	// defaults true) to keep the merge minimal.
	if len(entry) != 2 {
		t.Fatalf("mcp.%s has %d keys (%v), want exactly type+command", ServerName, len(entry), entry)
	}

	// HasGrafelEntry must learn the `mcp` key or mcptools/doctor reports a
	// false negative on a correctly-registered opencode.
	if !HasGrafelEntry(path) {
		t.Fatalf("HasGrafelEntry = false for a correctly-registered opencode config")
	}
}

// decodeJSONC standardizes a HuJSON document (dropping comments and trailing
// commas) and decodes it, so a test can assert on the STRUCTURE of a file that
// encoding/json alone could not parse.
//
// It copies its input first. hujson.Standardize blanks comments IN PLACE in the
// caller's slice — it overwrites each comment's bytes with spaces rather than
// building a new buffer — so passing the same []byte to both this helper and a
// comment assertion silently destroys the evidence the assertion is checking.
// That is a trap for the reader, not a product bug: nothing in opencode.go
// calls Standardize.
func decodeJSONC(t *testing.T, b []byte) map[string]any {
	t.Helper()
	std, err := hujson.Standardize(bytes.Clone(b))
	if err != nil {
		t.Fatalf("config is not valid HuJSON: %v\n%s", err, b)
	}
	var doc map[string]any
	if err := json.Unmarshal(std, &doc); err != nil {
		t.Fatalf("standardized config is not valid JSON: %v\n%s", err, std)
	}
	return doc
}

// jsoncComments are the comments the round-trip seed carries. They are checked
// as a group in both directions.
var jsoncComments = []string{
	"// my opencode config",
	"/* block comment about the model */",
	"// a server I already had",
}

// assertCommentsSurvive re-reads the file from DISK rather than trusting a
// buffer an earlier assertion may have handed to a mutating parser. The claim
// is about what grafel left on the user's filesystem.
func assertCommentsSurvive(t *testing.T, stage, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range jsoncComments {
		// EXACTLY once, in both directions. Losing a comment is the obvious
		// failure; DUPLICATING one is the failure indentInsertedMember could
		// introduce, since it copies whitespace out of a sibling's extra and
		// that extra can contain the sibling's comment.
		if n := strings.Count(string(b), c); n != 1 {
			t.Fatalf("%s left %d copies of the user's comment %q, want exactly 1:\n%s",
				stage, n, c, b)
		}
	}
}

// TestOpencode_JSONCRoundTripPreservesComments: opencode's docs actively
// encourage comments and trailing commas. encoding/json errors on both, and
// MarshalIndent would silently destroy the comments even if the read
// succeeded. A user's comments must survive a register/unregister round trip.
//
// Every comment assertion here is paired with a POSITIVE CONTROL on the same
// file — grafel present after register, absent after unregister. Without them
// this test passes with registerOpencode and unregisterOpencode both stubbed
// to `return nil`: it would only be checking that a file still contains the
// string the test itself wrote into it.
func TestOpencode_JSONCRoundTripPreservesComments(t *testing.T) {
	home := withHome(t)
	path := opencodePath(t, home)

	seed := `{
  // my opencode config
  "$schema": "https://opencode.ai/config.json",
  /* block comment about the model */
  "model": "anthropic/claude-opus-4",
  "mcp": {
    // a server I already had
    "other": {
      "type": "local",
      "command": ["/bin/other"],
    },
  },
}
`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
		t.Fatalf("RegisterPath on a JSONC config: %v", err)
	}

	afterRegister, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// POSITIVE CONTROL: the register actually happened, in the right shape.
	regDoc := decodeJSONC(t, afterRegister)
	regMCP, _ := regDoc["mcp"].(map[string]any)
	entry, ok := regMCP[ServerName].(map[string]any)
	if !ok {
		t.Fatalf("register added no mcp.%s to the JSONC config; mcp = %v", ServerName, regMCP)
	}
	if entry["type"] != "local" {
		t.Fatalf("JSONC register wrote type = %v, want \"local\"", entry["type"])
	}
	cmd, _ := entry["command"].([]any)
	if len(cmd) != 2 || cmd[0] != "/usr/local/bin/grafel" || cmd[1] != "mcp-bridge" {
		t.Fatalf("JSONC register wrote command = %v, want [/usr/local/bin/grafel mcp-bridge]", cmd)
	}
	if _, still := regMCP["other"]; !still {
		t.Fatalf("register dropped the foreign server from the JSONC config: %v", regMCP)
	}
	if !HasGrafelEntry(path) {
		t.Fatalf("HasGrafelEntry = false after registering into a JSONC config")
	}
	assertCommentsSurvive(t, "register", path)

	if err := UnregisterPath(path); err != nil {
		t.Fatalf("UnregisterPath on a JSONC config: %v", err)
	}
	afterUnregister, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// POSITIVE CONTROL: the unregister actually happened, and only to grafel.
	unregDoc := decodeJSONC(t, afterUnregister)
	unregMCP, ok := unregDoc["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("unregister dropped mcp even though the foreign server remained: %v", unregDoc)
	}
	if _, still := unregMCP[ServerName]; still {
		t.Fatalf("unregister left mcp.%s in the JSONC config: %v", ServerName, unregMCP)
	}
	if _, gone := unregMCP["other"]; !gone {
		t.Fatalf("unregister removed the foreign server from the JSONC config: %v", unregMCP)
	}
	if HasGrafelEntry(path) {
		t.Fatalf("HasGrafelEntry = true after unregistering from a JSONC config")
	}
	assertCommentsSurvive(t, "unregister", path)
}

// TestOpencode_DoesNotReflowAUserOwnedFile pins the no-reflow asymmetry: a file
// grafel CREATES is formatted, a file the USER owns is only patched.
//
// This is the property the parser was chosen to protect, and it was previously
// unobserved — deleting the `if format` gate in writeOpencode so hujson's
// whole-file formatter runs on every write left the entire package green.
// Reflowing somebody's hand-formatted config in order to add one entry is
// exactly the non-surgical edit the TOML and mcpServers arms refuse to make.
//
// The assertion is byte-level and deliberately strict: `after` must be `before`
// with ONE contiguous run inserted. Any reflow elsewhere in the file — a
// re-indent, a collapsed array, a moved brace — breaks the common
// prefix/suffix and fails.
func TestOpencode_DoesNotReflowAUserOwnedFile(t *testing.T) {
	home := withHome(t)
	path := opencodePath(t, home)

	// Deliberately non-canonical: 4-space indent, aligned values, an array
	// split over lines, a tab, and odd spacing before a colon. hujson.Format
	// would rewrite every one of these.
	seed := "{\n" +
		"    \"$schema\":  \"https://opencode.ai/config.json\",\n" +
		"    /* keep my layout */\n" +
		"    \"model\":    \"anthropic/claude-opus-4\",\n" +
		"    \"theme\" :   \"tokyonight\",\n" +
		"    \"mcp\": {\n" +
		"        \"other\": {\n" +
		"            \"type\":\t\"local\",\n" +
		"            \"command\": [\n" +
		"                \"/bin/other\",\n" +
		"                \"serve\"\n" +
		"            ]\n" +
		"        }\n" +
		"    }\n" +
		"}\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	before := []byte(seed)

	if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Longest common prefix, then longest common suffix over what is left.
	pre := 0
	for pre < len(before) && pre < len(after) && before[pre] == after[pre] {
		pre++
	}
	suf := 0
	for suf < len(before)-pre && suf < len(after)-pre &&
		before[len(before)-1-suf] == after[len(after)-1-suf] {
		suf++
	}
	if len(before) != pre+suf {
		t.Fatalf("register reflowed the user's file instead of inserting into it.\n"+
			"%d of %d original bytes survived as a common prefix/suffix.\nbefore:\n%s\nafter:\n%s",
			pre+suf, len(before), before, after)
	}
	inserted := after[pre : len(after)-suf]
	if !bytes.Contains(inserted, []byte(ServerName)) {
		t.Fatalf("the inserted run does not contain the grafel entry: %q", inserted)
	}

	// Sanity: the result is still a valid config with both servers.
	doc := decodeJSONC(t, after)
	mcp, _ := doc["mcp"].(map[string]any)
	if _, ok := mcp[ServerName]; !ok {
		t.Fatalf("no mcp.%s after register: %v", ServerName, mcp)
	}
	if _, ok := mcp["other"]; !ok {
		t.Fatalf("foreign server lost: %v", mcp)
	}
}

// TestOpencode_ToleratesNullContainer: `"mcp": null` is treated as ABSENT and
// replaced, exactly as mcpServersOf treats `"mcpServers": null`. A user who
// commented out their whole mcp block leaves this behind, and refusing it would
// give opencode a stricter contract than every other host for no reason. A null
// carries no data, so replacing it destroys nothing.
func TestOpencode_ToleratesNullContainer(t *testing.T) {
	home := withHome(t)

	t.Run("register replaces null", func(t *testing.T) {
		path := opencodePath(t, home)
		if err := os.WriteFile(path, []byte(`{"theme":"tokyonight","mcp":null}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
			t.Fatalf("RegisterPath over a null mcp: %v", err)
		}
		doc := decodeOpencode(t, path)
		mcp, ok := doc["mcp"].(map[string]any)
		if !ok {
			t.Fatalf("mcp is still not an object after register: %#v", doc["mcp"])
		}
		if _, ok := mcp[ServerName]; !ok {
			t.Fatalf("register over a null mcp added nothing: %v", mcp)
		}
		if doc["theme"] != "tokyonight" {
			t.Fatalf("register over a null mcp clobbered theme: %v", doc["theme"])
		}
		// Parity check: the JSON arm tolerates the same shape, which is the
		// contract this arm is being held to.
		jsonPath := filepath.Join(home, ".cursor", "mcp.json")
		if err := os.MkdirAll(filepath.Dir(jsonPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(jsonPath, []byte(`{"mcpServers":null}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := RegisterPath(jsonPath, "/usr/local/bin/grafel"); err != nil {
			t.Fatalf("the JSON arm rejects a null container, so this parity premise is wrong: %v", err)
		}
	})

	t.Run("unregister is a no-op on null", func(t *testing.T) {
		dir := filepath.Join(home, ".config", "opencode-null")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "opencode.json")
		seed := `{"theme":"tokyonight","mcp":null}`
		if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := UnregisterPath(path); err != nil {
			t.Fatalf("UnregisterPath over a null mcp: %v", err)
		}
		b, _ := os.ReadFile(path)
		if string(b) != seed {
			t.Fatalf("unregister rewrote a file with nothing to remove:\n got: %s\nwant: %s", b, seed)
		}
		if HasGrafelEntry(path) {
			t.Fatalf("HasGrafelEntry = true for a null mcp container")
		}
	})
}

// TestOpencode_RefusesNonObjectContainer pins the branch that separates the
// tolerated null from every other non-object.
//
// registerOpencode's comment claims: "Any OTHER non-object (array, string,
// number, bool) is real user data whose replacement would destroy something."
// That is a claim about NOT DESTROYING A USER'S CONFIG, and until this test it
// was pure prose — widening `case isJSONNull(found)` to `case
// !isJSONObject(found)` made the refusal branch unreachable and silently
// replaced any such value with an empty object, and the whole package stayed
// green.
//
// This is a DIFFERENT path from TestOpencode_MalformedJSONCIsRefusedNotRewritten:
// there the file does not parse at all. Here it is perfectly well-formed
// HuJSON and the value under `mcp` is simply not a container grafel may write
// into. Arrays and scalars are covered separately because they take different
// hujson paths (*hujson.Array vs hujson.Literal).
func TestOpencode_RefusesNonObjectContainer(t *testing.T) {
	home := withHome(t)

	cases := []struct {
		name string
		mcp  string // the JSON value placed under "mcp"
	}{
		{"array", `["something"]`},
		{"string", `"grafel"`},
		{"number", `42`},
		{"bool", `true`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(home, ".config", "opencode-"+tc.name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "opencode.json")
			seed := `{"theme":"tokyonight","mcp":` + tc.mcp + `}`
			if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
				t.Fatal(err)
			}

			// Sanity: the seed really is well-formed, so this test is about the
			// VALUE's type and not about a parse failure.
			if _, err := hujson.Parse([]byte(seed)); err != nil {
				t.Fatalf("seed is not well-formed HuJSON, so this test is not exercising what it claims: %v", err)
			}

			_, err := RegisterPath(path, "/usr/local/bin/grafel")
			if err == nil {
				b, _ := os.ReadFile(path)
				t.Fatalf("RegisterPath overwrote a %s under mcp instead of refusing it.\n"+
					"That value is the user's data; replacing it destroys something.\nfile is now: %s", tc.name, b)
			}
			if !errors.Is(err, ErrMalformedConfig) {
				t.Fatalf("RegisterPath error = %v, want ErrMalformedConfig", err)
			}
			if b, _ := os.ReadFile(path); string(b) != seed {
				t.Fatalf("RegisterPath modified a file it refused:\n got: %s\nwant: %s", b, seed)
			}

			// UnregisterPath is a NO-OP here, not an error, and that is
			// deliberate parity rather than laxity: the mcpServers arm reaches
			// `servers, _ := doc["mcpServers"].(map[string]any); if servers ==
			// nil { return nil }` for exactly this shape. There is no grafel
			// entry inside a non-object, so there is nothing to remove, and an
			// uninstall must not fail on a config it never wrote to.
			if err := UnregisterPath(path); err != nil {
				t.Fatalf("UnregisterPath on a %s under mcp = %v, want nil (no-op)", tc.name, err)
			}
			if b, _ := os.ReadFile(path); string(b) != seed {
				t.Fatalf("UnregisterPath modified a file with nothing to remove:\n got: %s\nwant: %s", b, seed)
			}

			// Parity control: the JSON arm behaves the same way for the
			// equivalent mcpServers shape. If this ever stops holding, the
			// premise of the two assertions above is wrong, not just their
			// expected values.
			jsonPath := filepath.Join(dir, "mcp.json")
			jsonSeed := `{"mcpServers":` + tc.mcp + `}`
			if err := os.WriteFile(jsonPath, []byte(jsonSeed), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := RegisterPath(jsonPath, "/usr/local/bin/grafel"); !errors.Is(err, ErrMalformedConfig) {
				t.Fatalf("the JSON arm accepts a %s container (err = %v), so the parity premise is wrong", tc.name, err)
			}
			if err := UnregisterPath(jsonPath); err != nil {
				t.Fatalf("the JSON arm errors unregistering a %s container (err = %v), so the parity premise is wrong", tc.name, err)
			}

			// And a refused config is never reported as registered.
			if HasGrafelEntry(path) {
				t.Fatalf("HasGrafelEntry = true for a %s under mcp", tc.name)
			}
		})
	}
}

// TestOpencode_MalformedJSONCIsRefusedNotRewritten: genuinely unparseable
// content is reported as ErrMalformedConfig — the USER's file is broken, not
// grafel's write — and the file is left byte-identical. The failure mode this
// forbids is "grafel could not read it, so grafel replaced it".
func TestOpencode_MalformedJSONCIsRefusedNotRewritten(t *testing.T) {
	home := withHome(t)
	path := opencodePath(t, home)

	seed := "{\n  // a comment is fine, this is not:\n  \"model\": ,,,\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := RegisterPath(path, "/usr/local/bin/grafel")
	if err == nil {
		t.Fatalf("RegisterPath accepted an unparseable config")
	}
	if !errors.Is(err, ErrMalformedConfig) {
		t.Fatalf("RegisterPath error = %v, want ErrMalformedConfig", err)
	}
	if b, _ := os.ReadFile(path); string(b) != seed {
		t.Fatalf("RegisterPath rewrote a file it could not parse:\n got: %s\nwant: %s", b, seed)
	}

	if err := UnregisterPath(path); !errors.Is(err, ErrMalformedConfig) {
		t.Fatalf("UnregisterPath error = %v, want ErrMalformedConfig", err)
	}
	if b, _ := os.ReadFile(path); string(b) != seed {
		t.Fatalf("UnregisterPath rewrote a file it could not parse:\n got: %s\nwant: %s", b, seed)
	}

	// And it never claims a broken file is registered.
	if HasGrafelEntry(path) {
		t.Fatalf("HasGrafelEntry = true for an unparseable config")
	}
}

// TestOpencode_PreservesSiblings: an existing unrelated MCP server and
// unrelated top-level keys survive registration untouched.
func TestOpencode_PreservesSiblings(t *testing.T) {
	home := withHome(t)
	path := opencodePath(t, home)

	seed := `{"model":"anthropic/claude-opus-4","theme":"tokyonight",` +
		`"mcp":{"other":{"type":"local","command":["/bin/other","serve"]}}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	doc := decodeOpencode(t, path)
	if doc["model"] != "anthropic/claude-opus-4" {
		t.Fatalf("register clobbered unrelated key model: %v", doc["model"])
	}
	if doc["theme"] != "tokyonight" {
		t.Fatalf("register clobbered unrelated key theme: %v", doc["theme"])
	}
	mcp, _ := doc["mcp"].(map[string]any)
	other, ok := mcp["other"].(map[string]any)
	if !ok {
		t.Fatalf("register dropped the foreign server; mcp = %v", mcp)
	}
	cmd, _ := other["command"].([]any)
	if len(cmd) != 2 || cmd[0] != "/bin/other" || cmd[1] != "serve" {
		t.Fatalf("register mutated the foreign server's command: %v", other)
	}
	if _, ok := mcp[ServerName]; !ok {
		t.Fatalf("register did not add mcp.%s; mcp = %v", ServerName, mcp)
	}
}

// TestOpencode_UnregisterRemovesOnlyGrafel mirrors the mcpServers behaviour:
// siblings survive, and the container key is dropped entirely when grafel was
// its last member.
func TestOpencode_UnregisterRemovesOnlyGrafel(t *testing.T) {
	home := withHome(t)

	t.Run("keeps siblings", func(t *testing.T) {
		path := opencodePath(t, home)
		seed := `{"theme":"tokyonight","mcp":{"other":{"type":"local","command":["/bin/other"]}}}`
		if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
			t.Fatal(err)
		}
		if err := UnregisterPath(path); err != nil {
			t.Fatal(err)
		}
		doc := decodeOpencode(t, path)
		mcp, ok := doc["mcp"].(map[string]any)
		if !ok {
			t.Fatalf("unregister dropped mcp even though a sibling remained: %v", doc)
		}
		if _, still := mcp[ServerName]; still {
			t.Fatalf("unregister left mcp.%s behind: %v", ServerName, mcp)
		}
		if _, ok := mcp["other"]; !ok {
			t.Fatalf("unregister removed the foreign server: %v", mcp)
		}
		if doc["theme"] != "tokyonight" {
			t.Fatalf("unregister clobbered unrelated key theme: %v", doc["theme"])
		}
		if HasGrafelEntry(path) {
			t.Fatalf("HasGrafelEntry = true after unregister")
		}
	})

	t.Run("drops empty mcp", func(t *testing.T) {
		dir := filepath.Join(home, ".config", "opencode2")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "opencode.json")
		if err := os.WriteFile(path, []byte(`{"theme":"tokyonight"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
			t.Fatal(err)
		}
		if err := UnregisterPath(path); err != nil {
			t.Fatal(err)
		}
		doc := decodeOpencode(t, path)
		if _, still := doc["mcp"]; still {
			t.Fatalf("unregister left an orphan empty mcp object: %v", doc)
		}
		if doc["theme"] != "tokyonight" {
			t.Fatalf("unregister clobbered unrelated key theme: %v", doc["theme"])
		}
	})
}

// TestOpencode_UnregisterIsIdempotent: a missing file and a file with no
// grafel entry are both no-ops (the uninstall loop only has a recorded path).
func TestOpencode_UnregisterIsIdempotent(t *testing.T) {
	home := withHome(t)
	path := opencodePath(t, home)

	if err := UnregisterPath(path); err != nil {
		t.Fatalf("UnregisterPath on absent file: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("UnregisterPath created %s out of nothing", path)
	}

	seed := `{"mcp":{"other":{"type":"local","command":["/bin/other"]}}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UnregisterPath(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "other") {
		t.Fatalf("UnregisterPath damaged a config with no grafel entry: %s", b)
	}
	if HasGrafelEntry(path) {
		t.Fatalf("HasGrafelEntry = true for a config with only a foreign server")
	}
}

// TestOpencode_RegisterIsIdempotent: re-registering does not duplicate the
// entry or accumulate keys.
func TestOpencode_RegisterIsIdempotent(t *testing.T) {
	home := withHome(t)
	path := opencodePath(t, home)

	if _, err := RegisterPath(path, "/old/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	doc := decodeOpencode(t, path)
	mcp, _ := doc["mcp"].(map[string]any)
	if len(mcp) != 1 {
		t.Fatalf("re-register produced %d servers, want 1: %v", len(mcp), mcp)
	}
	entry, _ := mcp[ServerName].(map[string]any)
	cmd, _ := entry["command"].([]any)
	if len(cmd) != 2 || cmd[0] != "/usr/local/bin/grafel" {
		t.Fatalf("re-register did not update command: %v", cmd)
	}
}

// TestInstall_SkipsAbsentOpencode: no ~/.config/opencode dir → detection is
// empty (mirrors TestInstall_SkipsAbsentAntigravity).
func TestInstall_SkipsAbsentOpencode(t *testing.T) {
	withHome(t)
	if targets := DetectOpencodePaths(); len(targets) != 0 {
		t.Fatalf("expected no opencode paths when dir absent, got %v", targets)
	}
}

// TestInstall_DetectsOpencode: ~/.config/opencode present → the user-global
// opencode.json is returned and matches SettingsPath.
func TestInstall_DetectsOpencode(t *testing.T) {
	home := withHome(t)
	want := opencodePath(t, home)

	targets := DetectOpencodePaths()
	if len(targets) != 1 || targets[0].Path != want {
		t.Fatalf("DetectOpencodePaths = %v, want [{%s}]", targets, want)
	}
	sp, err := SettingsPath(Opencode)
	if err != nil {
		t.Fatal(err)
	}
	if sp != want {
		t.Fatalf("SettingsPath(Opencode) = %q, want %q", sp, want)
	}
}
