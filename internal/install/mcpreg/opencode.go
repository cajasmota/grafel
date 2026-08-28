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
	"bytes"
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
// UnregisterPath is driven by the uninstall loop over RECORDED PATHS with no
// tool identity in scope (internal/install/uninstall.go, internal/cli/
// uninstall.go). Extension cannot separate opencode from Cursor/Kiro — all
// three are `.json` — so the filename is the next-narrowest discriminator that
// preserves that property. `opencode.jsonc` is accepted too because opencode
// documents comments in its config and users name the file accordingly.
//
// (HasGrafelEntry's only production caller, mcptools.go, DOES have the adapter
// in scope and could pass a discriminator. UnregisterPath alone is what forces
// path-derived dispatch; HasGrafelEntry follows it for consistency rather than
// out of necessity.)
//
// KNOWN LIMITATION — this is too NARROW, never too broad. opencode honours an
// OPENCODE_CONFIG env var pointing at an arbitrary filename. A config living
// at such a path does not match here and would fall to the generic JSON arm,
// which writes `mcpServers` — the exact silent never-loads failure this file
// exists to prevent. It is a COVERAGE gap rather than a corruption risk:
// grafel only ever writes its own canonical SettingsPath, so it cannot reach
// such a file on its own. Following OPENCODE_CONFIG is deliberately NOT done —
// one canonical global path per tool is how grafel treats every other host
// (ADR-0004).
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

// indentInsertedMember gives a freshly inserted object member the same leading
// indentation as the sibling above it.
//
// hujson's patch inserts with no leading extra at all, so a multi-line config
// gets `},"grafel":{…}` jammed onto the previous member's closing line. Valid
// JSON, but not something to hand back to a user who formatted the file.
//
// Only the whitespace run AFTER the sibling's last newline is copied, never the
// sibling's whole BeforeExtra: that extra can contain the user's comment (`//
// a server I already had`), and copying it would DUPLICATE that comment onto
// grafel's entry. This is a formatting fix; it must not invent content.
//
// It is deliberately a no-op unless the object is already multi-line and the
// member we inserted is the last one — there is nothing to match otherwise, and
// guessing would be the reflow this package refuses to do.
func indentInsertedMember(v *hujson.Value, ptr, name string) {
	found := v.Find(ptr)
	if found == nil {
		return
	}
	obj, ok := found.Value.(*hujson.Object)
	if !ok || len(obj.Members) < 2 {
		return
	}
	last := len(obj.Members) - 1
	lit, ok := obj.Members[last].Name.Value.(hujson.Literal)
	if !ok || lit.Kind() != '"' || lit.String() != name {
		return
	}
	prev := obj.Members[last-1].Name.BeforeExtra
	nl := bytes.LastIndexByte(prev, '\n')
	if nl < 0 {
		return // single-line object: leave it single-line
	}
	indent := make([]byte, 0, 1+len(prev)-nl-1)
	indent = append(indent, '\n')
	indent = append(indent, prev[nl+1:]...)
	obj.Members[last].Name.BeforeExtra = indent
}

// isJSONNull reports whether a resolved value is the literal `null`.
func isJSONNull(v *hujson.Value) bool {
	lit, ok := v.Value.(hujson.Literal)
	return ok && lit.Kind() == 'n'
}

// isJSONObject reports whether a resolved value is a JSON object.
func isJSONObject(v *hujson.Value) bool {
	_, ok := v.Value.(*hujson.Object)
	return ok
}

// opencodeObject returns the object at ptr, or nil when the pointer resolves
// to something that is not an object (including a JSON null, which is what a
// user who commented their whole `mcp` block out leaves behind — on the
// UNREGISTER side there is nothing to remove from a null, so nil here makes it
// a no-op; registerOpencode replaces it, see there).
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
	found := v.Find(ptr)
	switch {
	case found == nil:
		if err := applyOpencodePatch(&v, path, "add", ptr, map[string]any{}); err != nil {
			return err
		}
	case isJSONNull(found):
		// `"mcp": null` is treated as ABSENT and replaced with an object,
		// exactly as mcpServersOf treats `"mcpServers": null` (`!present ||
		// raw == nil` → empty map). This is not a hypothetical shape: it is
		// what a user who commented out the whole `mcp` block, or who left a
		// key behind while disabling every server, has on disk. A null carries
		// no data, so replacing it destroys nothing — which is precisely why
		// the JSON arm tolerates it, and refusing here would have given
		// opencode a stricter contract than every other host for no reason.
		if err := applyOpencodePatch(&v, path, "replace", ptr, map[string]any{}); err != nil {
			return err
		}
	case !isJSONObject(found):
		// Any OTHER non-object (array, string, number, bool) is real user data
		// whose replacement would destroy something. Refuse the way
		// mcpServersOf does.
		return fmt.Errorf("%s: %w: %q is not an object", filepath.Base(path), ErrMalformedConfig, opencodeServersKey)
	}

	if err := applyOpencodePatch(&v, path, "add", ptr+"/"+ServerName, newOpencodeEntry(binPath)); err != nil {
		return err
	}
	indentInsertedMember(&v, ptr, ServerName)
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
