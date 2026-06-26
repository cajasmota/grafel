package typescript

// ABI-15 POC validation (#5473 Phase 2). Proves a TypeScript grammar regenerated
// from grammar.js (v0.23.2) with tree-sitter CLI >=0.25 to LANGUAGE_VERSION 15
// loads and parses real TS/TSX cleanly under the go-tree-sitter v0.25.0 runtime,
// WITHOUT the unbounded parse-error-recovery loop the mixed-ABI pairing produced.
//
// What this asserts, end to end:
//   1. AbiVersion() == 15 on both the TS and TSX languages (the regen actually
//      took effect through the go.mod replace / vendored binding).
//   2. Each real-world sample parses through the official adapter (the same
//      watchdog-bounded Parse path the daemon uses) and COMPLETES well within a
//      wall-clock bound -- i.e. no loop. The watchdog deadline would turn a loop
//      into a bounded error; we assert we never even approach it.
//   3. The tree is sane: root kind "program", non-error root, children present.
//   4. error_ratio (ERROR/MISSING nodes over total nodes) is low -- the grammar
//      genuinely understands modern TS, it is not error-recovering its way through.
import (
	"testing"
	"time"
	"unsafe"

	tsofficial "github.com/tree-sitter/go-tree-sitter"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// wallClockBound is the per-parse ceiling for these small real-world samples. A
// healthy parse finishes in microseconds-to-milliseconds; the daemon watchdog
// (official.defaultParseTimeout) only fires at 20s. A sample taking anywhere near
// this bound would itself be the loop signal this POC is built to detect.
const wallClockBound = 2 * time.Second

// maxErrorRatio is the ceiling on ERROR/MISSING node share. Clean parses of valid
// TS sit at 0; we allow a hair of slack so the assertion is about "not error-
// recovering through the file", not exact zero.
const maxErrorRatio = 0.02

// realTSSamples exercise the constructs called out in the POC brief: functions,
// classes, generics, decorators, async, plus const/interface/enum/type surface.
var realTSSamples = map[string]string{
	"generics_and_interface": `
interface Repository<T extends { id: string }> {
  find(id: string): Promise<T | undefined>;
  all(): readonly T[];
}

function pluck<T, K extends keyof T>(items: T[], key: K): T[K][] {
  return items.map((it) => it[key]);
}
`,
	"class_decorators_async": `
function sealed(constructor: Function) {
  Object.seal(constructor);
}

@sealed
class Greeter<TName extends string = "world"> {
  private readonly name: TName;
  constructor(name: TName) {
    this.name = name;
  }
  async greet(): Promise<string> {
    const who = await Promise.resolve(this.name);
    return ` + "`hello ${who}`" + `;
  }
}
`,
	"enums_unions_guards": `
enum Color { Red, Green, Blue }

type Shape =
  | { kind: "circle"; radius: number }
  | { kind: "square"; side: number };

function area(s: Shape): number {
  switch (s.kind) {
    case "circle":
      return Math.PI * s.radius ** 2;
    case "square":
      return s.side * s.side;
    default:
      const _exhaustive: never = s;
      return _exhaustive;
  }
}
`,
}

// realTSXSamples exercise the JSX/TSX superset path (LanguageTSX).
var realTSXSamples = map[string]string{
	"function_component_generics": `
import * as React from "react";

type Props<T> = { items: T[]; render: (t: T) => React.ReactNode };

export function List<T>({ items, render }: Props<T>): JSX.Element {
  return (
    <ul className="list">
      {items.map((it, i) => (
        <li key={i}>{render(it)}</li>
      ))}
    </ul>
  );
}
`,
	"async_handler_jsx": `
const Button = ({ onClick }: { onClick: () => Promise<void> }) => {
  const handle = async () => {
    await onClick();
  };
  return <button onClick={handle}>Go</button>;
};
`,
}

func TestABI15_LanguageVersionIs15(t *testing.T) {
	cases := map[string]func() unsafe.Pointer{
		"typescript": tstypescript.LanguageTypescript,
		"tsx":        tstypescript.LanguageTSX,
	}
	for name, accessor := range cases {
		lang := tsofficial.NewLanguage(accessor())
		if lang == nil {
			t.Fatalf("%s: NewLanguage returned nil", name)
		}
		if got := lang.AbiVersion(); got != 15 {
			t.Errorf("%s: AbiVersion() = %d, want 15 (regen did not take effect)", name, got)
		} else {
			t.Logf("%s: AbiVersion() = 15 (ABI-15 confirmed under runtime LANGUAGE_VERSION=%d, MIN_COMPATIBLE=%d)",
				name, tsofficial.LANGUAGE_VERSION, tsofficial.MIN_COMPATIBLE_LANGUAGE_VERSION)
		}
	}
}

func TestABI15_ParsesRealTypeScriptCleanly(t *testing.T) {
	runSamples(t, Language(), realTSSamples)
}

func TestABI15_ParsesRealTSXCleanly(t *testing.T) {
	runSamples(t, LanguageTSX(), realTSXSamples)
}

func runSamples(t *testing.T, lang ts.Language, samples map[string]string) {
	t.Helper()
	adapter := official.New()
	parser, err := adapter.NewParser(lang)
	if err != nil {
		t.Fatalf("NewParser failed (ABI mismatch / SetLanguage rejected ABI-15?): %v", err)
	}
	defer parser.Close()

	for name, src := range samples {
		name, src := name, src
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			tree, err := parser.Parse([]byte(src))
			elapsed := time.Since(start)
			if err != nil {
				// A watchdog kill (ErrParseDeadlineExceeded) lands here -> the loop
				// would have manifested as a bounded error, which is itself the proof
				// the un-regenerated ABI-14 grammar looped. For ABI-15 we expect none.
				t.Fatalf("Parse returned error after %s: %v", elapsed, err)
			}
			if tree == nil {
				t.Fatalf("Parse returned nil tree after %s", elapsed)
			}
			defer tree.Close()

			if elapsed > wallClockBound {
				t.Fatalf("parse took %s (> %s bound) -- possible parse loop", elapsed, wallClockBound)
			}

			root := tree.RootNode()
			if root == nil {
				t.Fatal("RootNode is nil (ABI mismatch crash site)")
			}
			if got := root.Type(); got != "program" {
				t.Fatalf("root kind = %q, want program", got)
			}
			if root.IsError() {
				t.Fatal("root is an ERROR node")
			}
			if root.ChildCount() == 0 {
				t.Fatal("root has no children")
			}

			total, bad := countNodes(root)
			ratio := float64(bad) / float64(total)
			if ratio > maxErrorRatio {
				t.Fatalf("error_ratio = %.4f (%d/%d ERROR nodes) > %.4f -- grammar is error-recovering, not parsing",
					ratio, bad, total, maxErrorRatio)
			}
			t.Logf("OK: %s parsed in %s | nodes=%d error_nodes=%d error_ratio=%.4f",
				name, elapsed.Round(time.Microsecond), total, bad, ratio)
		})
	}
}

// countNodes walks the whole tree counting total nodes and ERROR/MISSING nodes.
func countNodes(n ts.Node) (total, bad int) {
	total = 1
	if n.IsError() {
		bad = 1
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if c == nil {
			continue
		}
		ct, cb := countNodes(c)
		total += ct
		bad += cb
	}
	return total, bad
}
