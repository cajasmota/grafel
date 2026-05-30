// Package elixir provides regex-based custom extractors for Elixir source files.
// Each extractor targets a specific framework and registers via init().
package elixir

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

var (
	// ectoBlockOpenRe matches the block-form `do` keyword (not the inline `do:`).
	// It matches `do` only when not immediately followed by a colon.
	ectoBlockOpenRe = regexp.MustCompile(`\bdo\b`)
	// ectoBlockTokenRe matches a balancing token. `do:` (inline keyword) is
	// matched as its own alternative FIRST so it is consumed without changing
	// depth; the bare `do`, `fn`, and `end` words open/close blocks.
	ectoBlockTokenRe = regexp.MustCompile(`do:|\b(?:do|fn|end)\b`)
)

// submatch1 returns the first non-empty capture group of every match of re
// against s, in source order. Used to enumerate struct fields / atom members
// from a regex with alternated capture groups.
func submatch1(re *regexp.Regexp, s string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		for _, g := range m[1:] {
			if g != "" {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

// uniqueStrings returns ss with duplicates removed, preserving first-seen order.
func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	var out []string
	for _, s := range ss {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

// ectoBlockBody returns the source of a `do ... end` block whose opening `do`
// keyword starts at or after blockStart. It balances nested do/end pairs so that
// inner constructs (e.g. an `if ... do ... end` inside a changeset) do not
// terminate the block prematurely. The returned string excludes the trailing
// `end`. If no `do` is found, an empty string is returned.
//
// Tokenisation is line/word based: only standalone `do`/`end`/`fn` words and
// the `do:` / `, do:` inline form are considered, which is sufficient for the
// well-formatted Ecto schema, query, and migration blocks this package targets.
func ectoBlockBody(source string, blockStart int) string {
	rest := source[blockStart:]
	// Find the first `do` that opens the block (word-boundary, not `do:`).
	openRe := ectoBlockOpenRe
	loc := openRe.FindStringIndex(rest)
	if loc == nil {
		return ""
	}
	bodyStart := blockStart + loc[1]
	depth := 1
	i := bodyStart
	for i < len(source) {
		tok := ectoBlockTokenRe.FindStringIndex(source[i:])
		if tok == nil {
			break
		}
		word := source[i+tok[0] : i+tok[1]]
		switch word {
		case "do":
			depth++
		case "fn":
			depth++
		case "end":
			depth--
			if depth == 0 {
				return source[bodyStart : i+tok[0]]
			}
		}
		i += tok[1]
	}
	return source[bodyStart:]
}

func makeEntity(name, kind, subtype, filePath, language string, lineNum int) types.EntityRecord {
	e := types.EntityRecord{
		Name:             name,
		Kind:             kind,
		Subtype:          subtype,
		SourceFile:       filePath,
		StartLine:        lineNum,
		EndLine:          lineNum,
		Language:         language,
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Properties: map[string]string{
			"kind":    kind,
			"subtype": subtype,
		},
	}
	e.ID = e.ComputeID()
	return e
}

func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}
