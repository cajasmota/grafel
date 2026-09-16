package main

import (
	"go/ast"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// registerFuncs are the two entry points an extractor package can register
// itself through. internal/extractors.Register is a thin delegate to
// internal/extractor.Register; both are in use.
var registerFuncs = map[string]bool{
	"github.com/cajasmota/grafel/internal/extractor.Register":  true,
	"github.com/cajasmota/grafel/internal/extractors.Register": true,
}

// extraGrammarKeys records grammar keys a package receives through a route
// other than its own Register call. There is exactly one, and it is not
// derivable from the registry: internal/extractors/incremental.go (and the
// full-index path it mirrors) re-points the PARSE language to "tsx" for a
// .tsx/.jsx file whose classified language is typescript/javascript, so the
// javascript package traverses TSX trees it never registered for.
//
// Anything added here is a claim that must be traceable to a call site. Keep it
// at one entry if at all possible: every entry is a hand-maintained fact, which
// is the category of thing this gate exists to distrust.
var extraGrammarKeys = map[string][]string{
	"internal/extractors/javascript": {"tsx"},
}

// Registration is one extractor.Register call found in the tree.
type Registration struct {
	Dir  string // module-relative package directory
	Key  string // the language key, "" when the argument is not a constant
	File string
	Line int
}

// findRegistrations derives package → language-key from the Register call
// sites. A non-constant key is recorded with Key == "" so it is visible rather
// than silently absent.
func (s *scanner) findRegistrations() []Registration {
	var out []Registration
	for _, p := range s.pkgs {
		info := p.TypesInfo
		if info == nil {
			continue
		}
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				fn := calleeFunc(info, call)
				if fn == nil || fn.Pkg() == nil {
					return true
				}
				if !registerFuncs[fn.Pkg().Path()+"."+fn.Name()] {
					return true
				}
				tp := s.fset.Position(call.Pos())
				file := tp.Filename
				if rel, err := filepath.Rel(s.modRoot, file); err == nil && !strings.HasPrefix(rel, "..") {
					file = filepath.ToSlash(rel)
				}
				r := Registration{
					Dir:  filepath.ToSlash(filepath.Dir(file)),
					File: file,
					Line: tp.Line,
				}
				if tv, ok := info.Types[call.Args[0]]; ok && tv.Value != nil {
					r.Key = constStringOf(tv)
				}
				out = append(out, r)
				return true
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir < out[j].Dir
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Grammar is one grammar's symbol table, read from the same ts.Language handle
// the daemon parses with.
type Grammar struct {
	Key   string
	Kinds map[string]bool
}

// loadGrammars dumps the node-kind symbol table of every grammar in the parser
// factory's registry.
func loadGrammars() (map[string]*Grammar, error) {
	out := map[string]*Grammar{}
	for key, lang := range treesitter.GrammarLanguages() {
		raw, ok := official.Unwrap(lang)
		if !ok || raw == nil {
			return nil, &grammarErr{key: key}
		}
		kinds := map[string]bool{}
		n := raw.NodeKindCount()
		for id := uint32(0); id < n; id++ {
			name := raw.NodeKindForId(uint16(id))
			if name == "" {
				continue
			}
			kinds[name] = true
		}
		out[key] = &Grammar{Key: key, Kinds: kinds}
	}
	return out, nil
}

type grammarErr struct{ key string }

func (e *grammarErr) Error() string {
	return "node-type-gate: grammar " + e.key + " is not an official-adapter language (cannot read its symbol table)"
}

// grammarKeysFor returns the grammar keys a package's node-type literals may be
// resolved against: every language it registers for that has a grammar, plus
// the documented extra routes.
func grammarKeysFor(dir string, regs []Registration, grammars map[string]*Grammar) []string {
	seen := map[string]bool{}
	for _, r := range regs {
		if r.Dir != dir || r.Key == "" {
			continue
		}
		if _, ok := grammars[r.Key]; ok {
			seen[r.Key] = true
		}
	}
	if len(seen) > 0 {
		for _, k := range extraGrammarKeys[dir] {
			if _, ok := grammars[k]; ok {
				seen[k] = true
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// pkgDirs lists every distinct package directory that produced a site.
func pkgDirs(sites []Site) []string {
	seen := map[string]bool{}
	for _, s := range sites {
		seen[s.Dir] = true
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
