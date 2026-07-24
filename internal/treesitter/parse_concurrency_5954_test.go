package treesitter_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter"
)

// repHdr builds a source blob: an optional file header followed by n repeated
// blocks. Blocks are indexed so every declaration is distinct, which keeps the
// parse from collapsing onto a trivially-cached shape.
func repHdr(header string, n int, f func(i int) string) []byte {
	var b strings.Builder
	b.WriteString(header)
	for i := 0; i < n; i++ {
		b.WriteString(f(i))
	}
	return []byte(b.String())
}

// concurrencyCorpus is the multi-language input set for the #5954 race gate.
// One entry per grammar family that has an external scanner with per-parser
// state (bash, python, ruby, css, html, typescript/tsx, rust, scala, php,
// yaml, toml) plus the scanner-less generated grammars (go, java, csharp, c,
// cpp, proto, sql, kotlin) — those are the two shapes whose concurrency
// behaviour differs, so both must be exercised. Sources are deliberately
// non-trivial (repeated blocks) so a parse is long enough for goroutines to
// genuinely overlap inside ts_parser_parse rather than finishing serially by
// luck.
func concurrencyCorpus() []struct {
	lang string
	src  []byte
} {
	rep := func(n int, f func(i int) string) []byte { return repHdr("", n, f) }
	return []struct {
		lang string
		src  []byte
	}{
		{"go", repHdr("package p\n", 120, func(i int) string {
			return fmt.Sprintf("func F%d(a int) int { if a > %d { return a }\n\treturn F%d(a + 1) }\n", i, i, i)
		})},
		{"python", rep(120, func(i int) string {
			return fmt.Sprintf("class C%d(Base):\n    def m%d(self, x):\n        return [y for y in range(x) if y %% 2]\n", i, i)
		})},
		{"typescript", rep(120, func(i int) string {
			return fmt.Sprintf("export function f%d<T>(x: T[]): T | undefined { return x[%d]; }\n", i, i)
		})},
		{"tsx", rep(120, func(i int) string {
			return fmt.Sprintf("const C%d = () => <div className=\"c%d\"><span>{%d}</span></div>;\n", i, i, i)
		})},
		{"javascript", rep(120, func(i int) string {
			return fmt.Sprintf("async function g%d() { const r = await fetch('/a/%d'); return r.json(); }\n", i, i)
		})},
		{"java", rep(120, func(i int) string {
			return fmt.Sprintf("class K%d { int m() { return %d; } }\n", i, i)
		})},
		{"ruby", rep(120, func(i int) string {
			return fmt.Sprintf("class R%d\n  def m%d(a)\n    a.map { |x| x + %d }\n  end\nend\n", i, i, i)
		})},
		{"bash", rep(120, func(i int) string {
			return fmt.Sprintf("f%d() {\n  local v=$(echo \"hi %d\")\n  echo \"${v}\"\n}\n", i, i)
		})},
		{"rust", rep(120, func(i int) string {
			return fmt.Sprintf("pub fn f%d(x: i32) -> i32 { let s = r#\"raw %d\"#; s.len() as i32 + x }\n", i, i)
		})},
		{"css", rep(120, func(i int) string {
			return fmt.Sprintf(".c%d { color: #%03d; margin: %dpx; }\n", i, i%1000, i)
		})},
		{"html", rep(120, func(i int) string {
			return fmt.Sprintf("<div id=\"d%d\"><p>row %d</p><script>var x=%d;</script></div>\n", i, i, i)
		})},
		{"yaml", rep(120, func(i int) string {
			return fmt.Sprintf("key%d:\n  nested: value%d\n  list:\n    - a%d\n", i, i, i)
		})},
		{"toml", rep(120, func(i int) string {
			return fmt.Sprintf("[t%d]\nkey = \"value%d\"\nnum = %d\n", i, i, i)
		})},
		{"php", rep(120, func(i int) string {
			return fmt.Sprintf("<?php\nfunction f%d() { $h = <<<EOT\nheredoc %d\nEOT;\n return $h; }\n", i, i)
		})},
		{"scala", rep(80, func(i int) string {
			return fmt.Sprintf("object O%d { def f(x: Int): Int = x + %d }\n", i, i)
		})},
		{"csharp", rep(120, func(i int) string {
			return fmt.Sprintf("class C%d { int M() => %d; }\n", i, i)
		})},
		{"c", rep(120, func(i int) string {
			return fmt.Sprintf("int f%d(int a) { return a + %d; }\n", i, i)
		})},
		{"cpp", rep(120, func(i int) string {
			return fmt.Sprintf("template <typename T> struct S%d { T f() { return T(%d); } };\n", i, i)
		})},
		{"proto", repHdr("syntax = \"proto3\";\n", 60, func(i int) string {
			return fmt.Sprintf("message M%d { int32 id = 1; string name = 2; }\n", i)
		})},
		{"sql", rep(120, func(i int) string {
			return fmt.Sprintf("SELECT a%d, b FROM t%d WHERE id = %d;\n", i, i, i)
		})},
		{"kotlin", rep(120, func(i int) string {
			return fmt.Sprintf("fun f%d(x: Int): Int { return x + %d }\n", i, i)
		})},
		{"lua", rep(120, func(i int) string {
			return fmt.Sprintf("local function f%d(a) return a + %d end\n", i, i)
		})},
		{"elixir", rep(80, func(i int) string {
			return fmt.Sprintf("defmodule M%d do\n  def f(x), do: x + %d\nend\n", i, i)
		})},
	}
}

// parseFingerprint is the observable output of one Parse, reduced to something
// comparable. The root S-expression is included deliberately: it is the exact
// artefact issue #481 reported as varying between runs, so comparing it (not
// merely the node count) is what makes this a real nondeterminism gate.
type parseFingerprint struct {
	nodeCount  int
	errorRatio float64
	sexp       string
	errText    string
}

func fingerprint(t *testing.T, f *treesitter.ParserFactory, src []byte, lang string) parseFingerprint {
	t.Helper()
	res, err := f.Parse(context.Background(), src, lang)
	fp := parseFingerprint{}
	if err != nil {
		fp.errText = err.Error()
	}
	if res != nil {
		fp.nodeCount = res.NodeCount
		fp.errorRatio = res.ErrorRatio
		if res.TSTree != nil {
			if root := res.TSTree.RootNode(); root != nil {
				fp.sexp = root.String()
			}
			res.TSTree.Close()
		}
	}
	return fp
}

// TestParse_ConcurrentMultiLanguage_NoRace is the #5954 safety gate for the
// removal of the global parseMu.
//
// It exercises exactly the path parseMu used to serialise —
// ParserFactory.Parse -> parseOfficial -> ts.Parser.Parse (ts_parser_parse) —
// from many goroutines at once, across every grammar shape in the bundle
// (external-scanner grammars and scanner-less ones), and asserts that each
// concurrent parse produces a result byte-identical to the serial baseline
// for the same input.
//
// Run under -race this covers the Go side (the factory's abiGuardOnce
// sync.Map, the indexstate parse gate, the binding's go-pointer payload
// registry). The C side cannot be instrumented by the Go race detector, so
// the C-level argument is structural and is documented on the ParserFactory
// godoc: a private TSParser per call, an immutable shared TSLanguage, and no
// mutable file-scope state in any bundled external scanner. This test is the
// empirical half — a shared-grammar race of the #481 kind would surface here
// as a fingerprint mismatch (garbled tree) or a crash.
func TestParse_ConcurrentMultiLanguage_NoRace(t *testing.T) {
	corpus := concurrencyCorpus()
	f := treesitter.NewParserFactory(nil)

	// Serial baseline first: one parse per language, no concurrency.
	baseline := make([]parseFingerprint, len(corpus))
	for i, c := range corpus {
		baseline[i] = fingerprint(t, f, c.src, c.lang)
		// Every corpus entry must yield a real, non-trivial tree — otherwise
		// the concurrent half below would be comparing empty fingerprints and
		// silently proving nothing.
		if baseline[i].errText != "" {
			t.Fatalf("baseline parse of %s failed: %s", c.lang, baseline[i].errText)
		}
		if baseline[i].sexp == "" || baseline[i].nodeCount < 500 {
			t.Fatalf("baseline parse of %s produced a degenerate tree (nodes=%d, sexp_len=%d)",
				c.lang, baseline[i].nodeCount, len(baseline[i].sexp))
		}
	}

	// Then hammer the same inputs concurrently. 8 goroutines × 4 rounds over
	// 23 languages = 736 concurrent parses; goroutines are released together
	// so the overlap inside ts_parser_parse is real rather than incidental.
	const (
		goroutines = 8
		rounds     = 4
	)
	var (
		start = make(chan struct{})
		wg    sync.WaitGroup
		mu    sync.Mutex
		fails []string
	)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			<-start
			for r := 0; r < rounds; r++ {
				// Stagger the start index per goroutine so different
				// languages (hence different grammars) are in flight
				// simultaneously, not the same one in lockstep.
				for k := 0; k < len(corpus); k++ {
					i := (k + g) % len(corpus)
					got := fingerprint(t, f, corpus[i].src, corpus[i].lang)
					if got != baseline[i] {
						mu.Lock()
						fails = append(fails, fmt.Sprintf(
							"lang=%s g=%d r=%d: concurrent parse diverged from serial baseline "+
								"(nodes %d vs %d, ratio %g vs %g, err %q vs %q, sexp_equal=%v)",
							corpus[i].lang, g, r,
							got.nodeCount, baseline[i].nodeCount,
							got.errorRatio, baseline[i].errorRatio,
							got.errText, baseline[i].errText,
							got.sexp == baseline[i].sexp))
						mu.Unlock()
					}
				}
			}
		}(g)
	}
	close(start)
	wg.Wait()

	if len(fails) > 0 {
		max := len(fails)
		if max > 10 {
			max = 10
		}
		t.Fatalf("%d concurrent parses diverged from the serial baseline:\n%s",
			len(fails), strings.Join(fails[:max], "\n"))
	}
}

// TestParse_ConcurrentSameLanguage_StableTree narrows the gate to the single
// riskiest shape: many goroutines parsing DIFFERENT sources through the SAME
// shared ts.Language handle at the same time. That is precisely the
// shared-grammar-state configuration ADR-0023:73 blames for #481 under the
// (now removed) smacker binding, so it gets its own explicit assertion rather
// than being folded into the round-robin above.
func TestParse_ConcurrentSameLanguage_StableTree(t *testing.T) {
	f := treesitter.NewParserFactory(nil)

	const variants = 24
	srcs := make([][]byte, variants)
	for i := range srcs {
		var b strings.Builder
		b.WriteString("package p\n")
		for j := 0; j < 40; j++ {
			fmt.Fprintf(&b, "func F%d_%d(a int) int { m := map[string]int{\"k%d\": %d}; return m[\"k%d\"] + a }\n", i, j, j, j, j)
		}
		srcs[i] = []byte(b.String())
	}

	want := make([]parseFingerprint, variants)
	for i := range srcs {
		want[i] = fingerprint(t, f, srcs[i], "go")
	}

	var (
		start = make(chan struct{})
		wg    sync.WaitGroup
		mu    sync.Mutex
		bad   int
	)
	for g := 0; g < variants; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			<-start
			for r := 0; r < 6; r++ {
				i := (g + r) % variants
				if fingerprint(t, f, srcs[i], "go") != want[i] {
					mu.Lock()
					bad++
					mu.Unlock()
				}
			}
		}(g)
	}
	close(start)
	wg.Wait()

	if bad > 0 {
		t.Fatalf("%d of %d concurrent same-language parses produced a tree differing from the serial baseline "+
			"(this is the #481 symptom — do NOT re-land parseMu without recording it against ADR-0023)",
			bad, variants*6)
	}
}
