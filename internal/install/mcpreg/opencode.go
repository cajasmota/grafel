package mcpreg

// opencode.go — the third config format this package writes (#6730).
//
// opencode's config file is `.json`, so extension dispatch alone (isTOML)
// would drop it into the generic JSON arm of RegisterPath, which writes
//
//	{"mcpServers": {"grafel": {"command": "<bin>", "args": ["mcp-bridge"],
//	                           "type": "stdio"}}}
//
// Every one of those four decisions is wrong for opencode. Its published
// schema (https://opencode.ai/config.json, $defs.McpLocalConfig) puts servers
// under the top-level key `mcp`, requires `type` to be the enum value
// "local", takes the whole argv as a single `command` ARRAY, and sets
// additionalProperties:false with no `args` member at all.
//
// The reason this matters more than a normal schema mismatch: opencode
// v1.18.16 changed to "ignore unknown top-level config fields instead of
// failing config parsing". So writing `mcpServers` succeeds, leaves a
// perfectly valid file on disk, satisfies any existence check — and the
// server simply never loads. There is no error anywhere for a user to find.
// That is why the tests decode and assert each axis of the shape rather than
// asserting that a file was written.
//
// Only the LOCAL server shape is emitted. Remote servers are a different
// schema branch (`type: "remote"`, requires `url`) and grafel's MCP bridge is
// a local stdio process.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/hujson"
)

// opencodeServersKey is the top-level key opencode reads MCP servers from.
// NOT "mcpServers" — see the package comment above for why that mistake is
// invisible rather than loud.
const opencodeServersKey = "mcp"

// opencodeLocalType is the `type` discriminator for a local (stdio) server.
// The schema's enum for McpLocalConfig has exactly this one value; "stdio" is
// the JSON-world spelling other hosts use and is not valid here.
const opencodeLocalType = "local"

// opencodeEntry is the McpLocalConfig grafel writes.
//
// `command` is the full argv as an array — opencode has no separate `args`
// key, and because the schema sets additionalProperties:false an `args`
// sibling is not merely ignored but invalid. The optional `enabled`, `cwd`,
// `environment` (note: NOT `env`) and `timeout` keys are deliberately omitted:
// `enabled` defaults to true, and omitting the rest keeps the merge into the
// user's file minimal.
type opencodeEntry struct {
	Type    string   `json:"type"`
	Command []string `json:"command"`
}

func newOpencodeEntry(binPath string) opencodeEntry {
	return opencodeEntry{
		Type:    opencodeLocalType,
		Command: []string{binPath, "mcp-bridge"},
	}
}

// isOpencode reports whether a config path must be edited with the opencode
// schema rather than the generic JSON one.
//
// Dispatch is on BASENAME, deliberately mirroring isTOML's dispatch on
// extension: the format has to be derivable from the path ALONE, because
// UnregisterPath and HasGrafelEntry are called by the uninstall loop and the
// wizard's tool detector with nothing but a recorded path and no tool
// identity. Extension cannot separate opencode from Cursor/Kiro — all three
// are `.json` — so the filename is the next-narrowest discriminator that
// preserves that property. `opencode.jsonc` is accepted too because opencode
// documents comments in its config and users name the file accordingly.
func isOpencode(path string) bool {
	switch strings.ToLower(filepath.Base(path)) {
	case "opencode.json", "opencode.jsonc":
		return true
	}
	return false
}

// readOpencode parses an opencode config as HuJSON (JSONC): comments and
// trailing commas are accepted, and the returned Value retains them so a
// re-serialisation via Pack() gives the user their file back. A missing or
// empty file yields an empty object.
//
// The second return reports whether the file had CONTENT of its own. It drives
// the one formatting decision here: a file grafel invents gets pretty-printed,
// whereas a file the user already owns is only ever patched, never reflowed.
func readOpencode(path string) (hujson.Value, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			raw = nil
		} else {
			return hujson.Value{}, false, err
		}
	}
	existing := len(strings.TrimSpace(string(raw))) > 0
	if !existing {
		raw = []byte("{}")
	}
	v, err := hujson.Parse(raw)
	if err != nil {
		// Same classification as readSettings: an unparseable file is the
		// USER's config being broken, not grafel failing to write.
		return hujson.Value{}, existing, fmt.Errorf("%s: %w: %v", filepath.Base(path), ErrMalformedConfig, err)
	}
	return v, existing, nil
}

// opencodeObject returns the object at ptr, or nil when the pointer resolves
// to something that is not an object (including a JSON null, which is what a
// user who commented their whole `mcp` block out leaves behind).
func opencodeObject(v *hujson.Value, ptr string) *hujson.Object {
	found := v.Find(ptr)
	if found == nil {
		return nil
	}
	obj, _ := found.Value.(*hujson.Object)
	return obj
}

// opencodePatch renders a single-operation RFC 6902 patch. hujson applies
// patches in place while preserving the surrounding comments and whitespace,
// which is the whole reason this arm does not go through
// readSettings/writeSettings (encoding/json would reject the file on read and
// MarshalIndent would discard every comment on write).
func opencodePatch(op, ptr string, value any) ([]byte, error) {
	type operation struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value,omitempty"`
	}
	return json.Marshal([]operation{{Op: op, Path: ptr, Value: value}})
}

func applyOpencodePatch(v *hujson.Value, path, op, ptr string, value any) error {
	p, err := opencodePatch(op, ptr, value)
	if err != nil {
		return err
	}
	if err := v.Patch(p); err != nil {
		return fmt.Errorf("%s: %w: %v", filepath.Base(path), ErrMalformedConfig, err)
	}
	return nil
}

// writeOpencode serialises the (comment-preserving) value back to disk through
// the same symlink-aware atomic writer the other formats use.
//
// `format` runs hujson's opinionated whole-file formatter and is passed ONLY
// for a file grafel created: patching an object hujson parsed from `{}` packs
// onto one line, which is fine for JSON but unpleasant to open. It is
// deliberately NOT applied to a file the user already owns — reflowing
// somebody's hand-formatted config to add one entry is exactly the
// non-surgical edit the rest of this package refuses to make.
func writeOpencode(path string, v hujson.Value, format bool) error {
	if format {
		v.Format()
	}
	v.UpdateOffsets()
	out := v.Pack()
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return writeRaw(path, string(out))
}

// registerOpencode adds or replaces mcp.grafel in an opencode config,
// preserving every other key, every foreign server, and the user's comments
// and trailing commas. Idempotent.
func registerOpencode(path, binPath string) error {
	v, existing, err := readOpencode(path)
	if err != nil {
		return err
	}
	if _, ok := v.Value.(*hujson.Object); !ok {
		return fmt.Errorf("%s: %w: config root is not a JSON object", filepath.Base(path), ErrMalformedConfig)
	}

	ptr := "/" + opencodeServersKey
	if found := v.Find(ptr); found == nil {
		if err := applyOpencodePatch(&v, path, "add", ptr, map[string]any{}); err != nil {
			return err
		}
	} else if _, isObj := found.Value.(*hujson.Object); !isObj {
		// Something non-object already occupies `mcp`. Replacing it would
		// destroy real user data, so refuse the way mcpServersOf does.
		return fmt.Errorf("%s: %w: %q is not an object", filepath.Base(path), ErrMalformedConfig, opencodeServersKey)
	}

	if err := applyOpencodePatch(&v, path, "add", ptr+"/"+ServerName, newOpencodeEntry(binPath)); err != nil {
		return err
	}
	return writeOpencode(path, v, !existing)
}

// unregisterOpencode removes ONLY mcp.grafel, leaving foreign servers and
// unrelated keys (and comments) intact. If grafel was the last member the now
// empty `mcp` object is dropped too, mirroring the mcpServers arm rather than
// persisting an orphan `{"mcp":{}}`. Idempotent: a missing file, a missing
// `mcp` key or a missing grafel entry are all no-ops that write nothing.
func unregisterOpencode(path string) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	v, _, err := readOpencode(path)
	if err != nil {
		return err
	}
	ptr := "/" + opencodeServersKey
	servers := opencodeObject(&v, ptr)
	if servers == nil {
		return nil
	}
	if v.Find(ptr+"/"+ServerName) == nil {
		return nil
	}
	if err := applyOpencodePatch(&v, path, "remove", ptr+"/"+ServerName, nil); err != nil {
		return err
	}
	if servers = opencodeObject(&v, ptr); servers != nil && len(servers.Members) == 0 {
		if err := applyOpencodePatch(&v, path, "remove", ptr, nil); err != nil {
			return err
		}
	}
	return writeOpencode(path, v, false)
}

// hasOpencodeGrafelEntry is HasGrafelEntry's opencode arm. Without it
// mcptools/doctor reports a false negative for a correctly-registered
// opencode, because the generic arm looks only under `mcpServers`.
func hasOpencodeGrafelEntry(path string) bool {
	v, _, err := readOpencode(path)
	if err != nil {
		return false
	}
	return v.Find("/"+opencodeServersKey+"/"+ServerName) != nil
}

// DetectOpencodePaths returns the opencode config path to register.
// Uses $XDG_CONFIG_HOME/opencode/opencode.json — ShapeBroadSettings, since
// the MCP servers are one key among many in a general settings file.
func DetectOpencodePaths() []HostTarget {
	return DetectHostPaths([]string{"$XDG_CONFIG_HOME", "opencode"}, "opencode.json", ShapeBroadSettings)
}
