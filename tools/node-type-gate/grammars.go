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

// parseLangArg maps a parse entry point to the index of its language
// argument. A package that calls it with a CONSTANT language is bound to that
// grammar as directly as an extractor is bound by its Register call — it is
// literally the key the parser factory dispatches on — so it is derived the
// same way rather than hand-listed.
//
// This is what maps internal/custom/kotlin: the custom lane registers under
// synthetic keys ("custom_kotlin_ktor_routes") that are not grammar names and
// parses for itself, so Register-derivation alone leaves it with no grammar and
// its 11 kotlin literals unchecked (#7076 review, finding 1).
var parseLangArg = map[string]int{
	"github.com/cajasmota/grafel/internal/treesitter.Parse": 2,
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

// ParseBinding is one call to a parse entry point, recording which grammar the
// calling package's trees come from.
type ParseBinding struct {
	Dir  string
	Key  string // "" when the language argument is not a constant
	File string
	Line int
}

// findParseBindings derives package → grammar from the parse call sites. A
// non-constant language is recorded with Key == "": that package's trees can be
// ANY grammar, so it cannot be soundly mapped, and saying so is the point.
func (s *scanner) findParseBindings() []ParseBinding {
	var out []ParseBinding
	for _, p := range s.pkgs {
		info := p.TypesInfo
		if info == nil {
			continue
		}
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				fn := calleeFunc(info, call)
				if fn == nil || fn.Pkg() == nil {
					return true
				}
				idx, ok := parseLangArg[fn.Pkg().Path()+"."+fn.Name()]
				if !ok || idx >= len(call.Args) {
					return true
				}
				tp := s.fset.Position(call.Pos())
				file := tp.Filename
				if rel, err := filepath.Rel(s.modRoot, file); err == nil && !strings.HasPrefix(rel, "..") {
					file = filepath.ToSlash(rel)
				}
				b := ParseBinding{
					Dir:  filepath.ToSlash(filepath.Dir(file)),
					File: file,
					Line: tp.Line,
				}
				if tv, ok := info.Types[call.Args[idx]]; ok && tv.Value != nil {
					b.Key = constStringOf(tv)
				}
				out = append(out, b)
				return true
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir < out[j].Dir
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// dirsWithDynamicParse is the set of package directories that parse under a
// language they compute at runtime. Such a package can receive ANY grammar, so
// resolving its literals against the subset it happens to name as constants
// would report a live literal as dead. They are unmappable BY CONSTRUCTION, not
// by omission, and the gate names them rather than hiding them.
func dirsWithDynamicParse(bs []ParseBinding) map[string][]ParseBinding {
	out := map[string][]ParseBinding{}
	for _, b := range bs {
		if b.Key == "" {
			out[b.Dir] = append(out[b.Dir], b)
		}
	}
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
// resolved against: every language it registers for, every language it parses
// with as a constant, and the documented extra routes.
//
// Returns nil for a package with a dynamic parse language — see
// dirsWithDynamicParse. Mapping such a package to the subset of grammars it
// names as constants would be worse than not mapping it: a literal that is live
// under a grammar reached through the runtime path would be reported dead.
func grammarKeysFor(dir string, regs []Registration, binds []ParseBinding, grammars map[string]*Grammar) []string {
	if len(dirsWithDynamicParse(binds)[dir]) > 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, r := range regs {
		if r.Dir != dir || r.Key == "" {
			continue
		}
		if _, ok := grammars[r.Key]; ok {
			seen[r.Key] = true
		}
	}
	for _, b := range binds {
		if b.Dir != dir || b.Key == "" {
			continue
		}
		if _, ok := grammars[b.Key]; ok {
			seen[b.Key] = true
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
