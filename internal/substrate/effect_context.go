// Conditional / loop effect attribution + per-function cyclomatic complexity
// (#4821, control-flow epic #4820 part (a)).
//
// The branch facet (branches.go) enumerates the response-affecting CONTROL-FLOW
// decision points of a function. This file adds the complementary, effect-side
// view the porting/audit agent needs: for each EFFECT a function performs
// (db_read/db_write/http_call/message_publish/…), under WHAT CONDITION does it
// run, and is it inside a LOOP (a fan-out / N+1 signal)?
//
// It is a second consumer of the same per-function source window the branch
// facet walks, reusing the already-registered per-language effect sniffers
// (EffectSnifferFor) to locate the sink lines, then classifying each sink's
// enclosing block context:
//
//   - conditional = true  → the sink is inside an if/else-if/else, switch/case,
//     or try/catch branch (vs unconditional/top-level).
//   - condition          → the nearest enclosing branch predicate text, so the
//     graph/MCP can answer "under what condition does this write / call?".
//   - in_loop = true     → the sink is inside a for/while/foreach over a
//     collection.
//
// Plus a cheap per-function summary: cyclomatic_complexity (decision points + 1)
// and branch_count, persisted as properties on the function/operation entity.
//
// Like the branch facet, this is OPT-IN (computed only when the effects MCP tool
// is asked for include="effect_contexts") so the default effects payload stays
// byte-for-byte unchanged and the #2828 token-reduction work is respected.
//
// Block-scope detection is language-family general: Python keys on significant
// indentation; brace languages on `{`/`}` depth. The flagship languages for the
// first increment are Python (Django/oracle stack) and JS/TS (NestJS), matching
// epic #4820's scope; other languages reuse whichever mechanism their family
// uses but are validated/expanded in the per-language follow-ups.
package substrate

import (
	"regexp"
	"strings"
)

// EffectContext is one effect occurrence inside a function, annotated with the
// control-flow context it runs under. JSON shape is the public contract the
// effects MCP tool serialises.
type EffectContext struct {
	// Effect is the lattice element (db_read / db_write / http_call / …).
	Effect string `json:"effect"`
	// Sink is the short primitive tag that matched (e.g. "fetch",
	// "requests.post", "repo.save"). Mirrors EffectMatch.Sink.
	Sink string `json:"sink"`
	// Line is the 1-indexed absolute source line of the sink.
	Line int `json:"line"`
	// Conditional is true when the sink is inside any conditional block
	// (if/else-if/else, switch/case, try/catch). False when it runs
	// unconditionally at the top level of the function body.
	Conditional bool `json:"conditional"`
	// Condition is the nearest enclosing branch predicate text (e.g.
	// "if user.is_admin", "catch (e)", "if (flag)"). Empty when the sink is
	// unconditional.
	Condition string `json:"condition,omitempty"`
	// InLoop is true when the sink is inside a for/while/foreach loop — a
	// fan-out / N+1 signal.
	InLoop bool `json:"in_loop,omitempty"`
}

// FunctionComplexity is the cheap per-function control-flow summary persisted as
// entity properties (cyclomatic_complexity, branch_count).
type FunctionComplexity struct {
	// Cyclomatic is the cyclomatic complexity: decision points + 1.
	Cyclomatic int `json:"cyclomatic_complexity"`
	// BranchCount is the raw number of decision points (Cyclomatic - 1).
	BranchCount int `json:"branch_count"`
}

// blockHeader is one enclosing control-flow header discovered while walking a
// function body, with the source span it scopes and whether it is a loop.
type blockHeader struct {
	condition string
	startLine int // 1-indexed absolute line of the header
	endLine   int // exclusive absolute line at which the block's scope ends
	isLoop    bool
	indent    int // python: header indent; brace: nesting depth at header
}

// EffectContextsFor computes the conditional/loop attribution for every effect
// the registered sniffer detects in funcSource, plus the per-function
// complexity summary. startLine is the 1-indexed absolute file line of the
// function's first line so emitted Line values are absolute. Returns nil
// contexts (but a valid complexity) when the language has no effect sniffer or
// no sinks are present. Pure + deterministic.
func EffectContextsFor(lang, funcSource string, startLine int) ([]EffectContext, FunctionComplexity) {
	complexity := ComputeFunctionComplexity(funcSource)
	if strings.TrimSpace(funcSource) == "" {
		return nil, complexity
	}
	sniffer := EffectSnifferFor(lang)
	if sniffer == nil {
		return nil, complexity
	}
	// Clamp to the target function's own body so sink lines from trailing
	// sibling defs (when the window was EndLine-padded) are not mis-attributed.
	clamped := ClampToFunctionBody(funcSource, lang)
	matches := sniffer(clamped)
	if len(matches) == 0 {
		return nil, complexity
	}
	blocks := enclosingBlocks(clamped, lang, startLine)
	out := make([]EffectContext, 0, len(matches))
	for _, m := range matches {
		if m.Line <= 0 {
			continue
		}
		absLine := startLine + m.Line - 1 // EffectMatch.Line is 1-indexed within the window
		ec := EffectContext{
			Effect: string(m.Effect),
			Sink:   m.Sink,
			Line:   absLine,
		}
		if cond, loop, ok := innermostEnclosing(blocks, absLine); ok {
			ec.Conditional = true
			ec.Condition = cond
			ec.InLoop = loop
		}
		out = append(out, ec)
	}
	return out, complexity
}

// innermostEnclosing returns the condition text + loop flag of the INNERMOST
// block that encloses absLine, walking the (source-ordered) block list. The
// loop flag is sticky: if ANY enclosing block (not just the innermost) is a
// loop, in_loop is true — a sink nested under `for { if x { write } }` is still
// a fan-out. The surfaced condition is the nearest enclosing predicate.
func innermostEnclosing(blocks []blockHeader, absLine int) (cond string, inLoop bool, ok bool) {
	bestSpan := int(^uint(0) >> 1) // max int
	for _, b := range blocks {
		if absLine <= b.startLine || absLine >= b.endLine {
			continue
		}
		ok = true
		if b.isLoop {
			inLoop = true
		}
		if span := b.endLine - b.startLine; span < bestSpan {
			bestSpan = span
			cond = b.condition
		}
	}
	return cond, inLoop, ok
}

// --- block-scope discovery ------------------------------------------------

// enclosingBlocks enumerates every conditional/loop block header in a function
// body with the absolute source span it scopes. Dispatches on language family:
// Python uses indentation; brace languages use `{`/`}` depth.
func enclosingBlocks(src, lang string, startLine int) []blockHeader {
	switch {
	case lang == "python":
		return pythonBlocks(src, startLine)
	case lang == "ruby":
		return rubyBlocks(src, startLine)
	case braceLangs[lang] || braceCFGLangs[lang]:
		return braceBlocks(src, lang, startLine)
	default:
		// No block detector for this family yet — effects are still reported,
		// just without conditional/loop attribution (honest-partial).
		return nil
	}
}

// braceCFGLangs is the set of brace-delimited languages that are NOT in the
// ClampToFunctionBody braceLangs set (so their function body is not source-level
// clamped) but DO have a validated control-flow / effect-context block detector.
// Swift's `{}`-delimited bodies are classified the same way as the braceLangs
// family for control-flow purposes (#4830).
var braceCFGLangs = map[string]bool{
	"swift": true,
}

var (
	pyCondHeaderRe = regexp.MustCompile(`^(\s*)((?:el)?if\b.*?|else|except\b[^:]*|elif\b.*?|with\b.*?|try)\s*:\s*(?:#.*)?$`)
	pyLoopHeaderRe = regexp.MustCompile(`^(\s*)((?:async\s+)?for\b.*?|while\b.*?)\s*:\s*(?:#.*)?$`)
)

// pythonBlocks scopes if/elif/else/except/try/with (conditional) and for/while
// (loop) blocks by indentation: a block runs from its header to the first
// following non-blank line indented at or below the header.
func pythonBlocks(src string, startLine int) []blockHeader {
	lines := strings.Split(src, "\n")
	var out []blockHeader
	for i, raw := range lines {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var cond string
		var isLoop bool
		var indent int
		if m := pyLoopHeaderRe.FindStringSubmatch(raw); m != nil {
			indent = len(m[1])
			cond = strings.TrimSpace(m[2])
			isLoop = true
		} else if m := pyCondHeaderRe.FindStringSubmatch(raw); m != nil {
			indent = len(m[1])
			cond = strings.TrimSpace(m[2])
		} else {
			continue
		}
		end := i + 1
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			if leadingWS(lines[j]) <= indent {
				break
			}
			end = j + 1
		}
		out = append(out, blockHeader{
			condition: cond,
			startLine: startLine + i,
			endLine:   startLine + end,
			isLoop:    isLoop,
			indent:    indent,
		})
	}
	return out
}

// --- ruby (`end`-delimited) block discovery -------------------------------

var (
	// rubyCondBlockRe — a leading-keyword conditional block: `if`, `unless`,
	// `case`, `begin`. (Trailing-modifier `x if y` is NOT a block; it has code
	// before the keyword and is excluded by isRubyModifierLine.)
	rubyCondBlockRe = regexp.MustCompile(`^\s*(if|unless|case|begin)\b(.*?)\s*(?:then)?\s*$`)
	// rubyLoopBlockRe — keyword loop forms: `while`/`until`/`for … in …`.
	rubyLoopBlockRe = regexp.MustCompile(`^\s*(while|until|for)\b(.*?)\s*(?:do)?\s*$`)
	// rubyDoBlockRe — an iterator block tail `…each do |x|` / `…do` opening a
	// `do…end` block (a loop / fan-out over a collection).
	rubyDoBlockRe = regexp.MustCompile(`\bdo\b(\s*\|[^|]*\|)?\s*$`)
	// rubyElsifRe — `elsif`/`else`/`rescue`/`ensure`/`when` continuation clauses
	// (do not open a new `end`-block).
	rubyElsifRe    = regexp.MustCompile(`^\s*(elsif|else|rescue|ensure|when)\b`)
	rubyDefBlockRe = regexp.MustCompile(`^\s*(def|class|module)\b`)
)

// rubyBlocks scopes Ruby `end`-delimited conditional/loop blocks by keyword
// depth: an opener (`if`/`unless`/`case`/`begin`/`while`/`until`/`for`, or a
// trailing `do`) increments depth, an `end` decrements it; the block runs from
// its header to the matching `end`. Iterator `do…end` (and `.each do`) blocks
// are treated as loops (a fan-out / N+1 signal), mirroring the brace-family
// `.forEach`. Modifier guards (`return x if y`) are not blocks and are skipped.
func rubyBlocks(src string, startLine int) []blockHeader {
	lines := strings.Split(src, "\n")
	var out []blockHeader
	for i, raw := range lines {
		scan := strings.TrimSpace(raw)
		if scan == "" {
			continue
		}
		var cond string
		var isLoop bool
		switch {
		case rubyLoopBlockRe.MatchString(raw):
			m := rubyLoopBlockRe.FindStringSubmatch(raw)
			cond = strings.TrimSpace(m[1] + " " + strings.TrimSpace(m[2]))
			isLoop = true
		case rubyDoBlockRe.MatchString(raw) && !rubyCondBlockRe.MatchString(raw):
			// `collection.each do |x|` — iterator block (loop).
			cond = strings.TrimSpace(rubyDoBlockRe.ReplaceAllString(scan, ""))
			if cond == "" {
				cond = "do"
			}
			isLoop = true
		case rubyCondBlockRe.MatchString(raw) && !isRubyModifierLine(raw):
			m := rubyCondBlockRe.FindStringSubmatch(raw)
			cond = strings.TrimSpace(m[1] + " " + strings.TrimSpace(m[2]))
		default:
			continue
		}
		end := rubyBlockSpanEnd(lines, i)
		out = append(out, blockHeader{
			condition: cond,
			startLine: startLine + i,
			endLine:   startLine + end,
			isLoop:    isLoop,
		})
	}
	return out
}

// isRubyModifierLine reports whether a line is a trailing-modifier guard
// (`return x if cond`) rather than a block opener — i.e. there is code before
// the leading conditional keyword.
func isRubyModifierLine(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	for _, kw := range []string{"if", "unless", "case", "begin"} {
		if strings.HasPrefix(trimmed, kw+" ") || trimmed == kw {
			return false
		}
	}
	return true
}

// rubyBlockSpanEnd returns the exclusive line index of the matching `end` for
// the Ruby block whose header is lines[headerIdx], tracking keyword-open /
// `end`-close depth. Inline modifier guards do not open a block.
func rubyBlockSpanEnd(lines []string, headerIdx int) int {
	depth := 0
	for j := headerIdx; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "" {
			continue
		}
		if j == headerIdx {
			depth++
		} else if rubyElsifRe.MatchString(lines[j]) {
			// continuation clause — no depth change
		} else if (rubyCondBlockRe.MatchString(lines[j]) && !isRubyModifierLine(lines[j])) ||
			rubyLoopBlockRe.MatchString(lines[j]) ||
			rubyDefBlockRe.MatchString(lines[j]) ||
			rubyDoBlockRe.MatchString(lines[j]) {
			depth++
		}
		if rubyEndRe.MatchString(lines[j]) {
			depth--
			if depth <= 0 {
				return j + 1
			}
		}
	}
	return len(lines)
}

var (
	// Paren-form headers — the C/Java family always parenthesises conditions.
	braceIfRe      = regexp.MustCompile(`^\s*(?:\}\s*else\s+)?if\s*\(`)
	braceElseRe    = regexp.MustCompile(`^\s*\}?\s*else\b`)
	braceElseIfRe  = regexp.MustCompile(`^\s*\}?\s*else\s*if\b`)
	braceCatchRe   = regexp.MustCompile(`^\s*\}?\s*catch\b`)
	braceTryRe     = regexp.MustCompile(`^\s*try\b`)
	braceSwitchRe  = regexp.MustCompile(`^\s*switch\s*\(`)
	braceForRe     = regexp.MustCompile(`^\s*for\s*[(\s]`)
	braceWhileRe   = regexp.MustCompile(`^\s*}?\s*while\s*\(`)
	braceForEachRe = regexp.MustCompile(`\.\s*for[Ee]ach\s*\(`)
	// foreachStmtRe — the statement-form `foreach (…)` loop header used by C#
	// and PHP (distinct from the `.forEach(` method-call form above).
	foreachStmtRe = regexp.MustCompile(`^\s*foreach\s*\(`)

	// PHP-specific keyword forms (`elseif`, `foreach`).
	phpElseIfRe  = regexp.MustCompile(`^\s*\}?\s*else\s*if\b`)
	phpForEachRe = regexp.MustCompile(`^\s*foreach\s*\(`)

	// No-paren / keyword-condition forms used by Go, Rust, Kotlin, Scala and
	// Swift, where a header reads `if cond {`, `for x := range … {`, `for {`,
	// `match x {`, `when {`, `loop {`, `guard … else {`. The trailing `{`
	// anchors them so a plain `if (…)` paren form is still preferred.
	noParenIfRe     = regexp.MustCompile(`^\s*(?:\}\s*)?if\b`)
	noParenElseIfRe = regexp.MustCompile(`^\s*\}?\s*else\s+if\b`)
	noParenForRe    = regexp.MustCompile(`^\s*for\b`)
	noParenWhileRe  = regexp.MustCompile(`^\s*\}?\s*while\b`)
	goSwitchRe      = regexp.MustCompile(`^\s*(?:\w+\s*:?=\s*)?switch\b`)
	goSelectRe      = regexp.MustCompile(`^\s*select\s*\{`)
	rustMatchRe     = regexp.MustCompile(`^\s*(?:\w+\s*=\s*)?match\b`)
	rustLoopRe      = regexp.MustCompile(`^\s*(?:'\w+:\s*)?loop\b`)
	kotlinWhenRe    = regexp.MustCompile(`^\s*(?:\w+\s*=\s*)?when\b`)
	scalaMatchRe    = regexp.MustCompile(`\bmatch\s*\{`) // scala infix `expr match {`
	swiftGuardRe    = regexp.MustCompile(`^\s*guard\b`)
	swiftSwitchRe   = regexp.MustCompile(`^\s*switch\b`) // swift no-paren `switch x {`
)

// braceDialect captures the per-language header forms that differ from the
// C/Java baseline (no-paren conditions, keyword loops/matches, etc.).
type braceDialect struct {
	noParen bool // accept `if cond {`, `for cond {`, `while cond {` (Go/Rust/Kotlin/Scala/Swift)
	php     bool // accept `foreach`, `elseif`
	goExtra bool // accept `switch`/`select` (Go)
	rust    bool // accept `match`, `loop`
	kotlin  bool // accept `when`
	scala   bool // accept `match`
	swift   bool // accept `guard`, `switch` (no-paren)
}

func braceDialectFor(lang string) braceDialect {
	switch lang {
	case "go":
		return braceDialect{noParen: true, goExtra: true}
	case "rust":
		return braceDialect{noParen: true, rust: true}
	case "kotlin":
		return braceDialect{noParen: true, kotlin: true}
	case "scala":
		return braceDialect{noParen: true, scala: true}
	case "swift":
		return braceDialect{noParen: true, swift: true}
	case "php":
		return braceDialect{php: true}
	default: // jsts, java, csharp — C/Java baseline (paren conditions)
		return braceDialect{}
	}
}

// braceBlocks scopes brace-delimited conditional/loop blocks. For each header
// line it finds the `{` that opens the block (K&R same-line or Allman next
// line) and tracks depth to the matching `}`, recording the absolute span. The
// dialect for `lang` decides which header forms are recognised (no-paren
// conditions for Go/Rust/Kotlin/Scala/Swift, `foreach`/`elseif` for PHP, etc.).
func braceBlocks(src, lang string, startLine int) []blockHeader {
	dia := braceDialectFor(lang)
	lines := strings.Split(src, "\n")
	var out []blockHeader
	for i, raw := range lines {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		cond, isLoop, matched := classifyBraceHeader(raw, dia)
		if !matched {
			continue
		}
		// Find the block span by brace depth from this header.
		end := braceSpanEnd(lines, i)
		out = append(out, blockHeader{
			condition: cond,
			startLine: startLine + i,
			endLine:   startLine + end,
			isLoop:    isLoop,
		})
	}
	return out
}

// classifyBraceHeader reports whether a line opens a conditional/loop block and
// returns its condition text + loop flag, applying the language dialect.
func classifyBraceHeader(raw string, dia braceDialect) (cond string, isLoop bool, matched bool) {
	scan := stripBraceNoise(raw)
	// --- PHP keyword forms first (foreach/elseif) ------------------------
	if dia.php {
		switch {
		case phpForEachRe.MatchString(scan):
			return trimBraceCond(scan), true, true
		case phpElseIfRe.MatchString(scan):
			return trimBraceCond(scan), false, true
		}
	}
	// --- paren baseline (C/Java/jsts/csharp/php) -------------------------
	switch {
	case foreachStmtRe.MatchString(scan): // C# / PHP `foreach (…)`
		return trimBraceCond(scan), true, true
	case braceForRe.MatchString(scan):
		return trimBraceCond(scan), true, true
	case braceWhileRe.MatchString(scan):
		return trimBraceCond(scan), true, true
	case braceForEachRe.MatchString(scan):
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(scan), "{")), true, true
	case braceIfRe.MatchString(scan):
		return trimBraceCond(scan), false, true
	case braceSwitchRe.MatchString(scan):
		return trimBraceCond(scan), false, true
	case braceCatchRe.MatchString(scan):
		return trimBraceCond(scan), false, true
	case braceTryRe.MatchString(scan):
		return "try", false, true
	}
	// --- no-paren / keyword dialects (Go/Rust/Kotlin/Scala/Swift) --------
	if dia.swift && (swiftGuardRe.MatchString(scan) || swiftSwitchRe.MatchString(scan)) {
		return trimNoParenCond(scan), false, true
	}
	if dia.goExtra && (goSwitchRe.MatchString(scan) || goSelectRe.MatchString(scan)) {
		return trimNoParenCond(scan), false, true
	}
	if dia.rust && (rustMatchRe.MatchString(scan) || rustLoopRe.MatchString(scan)) {
		isLoop := rustLoopRe.MatchString(scan)
		return trimNoParenCond(scan), isLoop, true
	}
	if dia.kotlin && kotlinWhenRe.MatchString(scan) {
		return trimNoParenCond(scan), false, true
	}
	if dia.scala && scalaMatchRe.MatchString(scan) { // scala infix `x match {`
		return trimNoParenCond(scan), false, true
	}
	if dia.noParen {
		switch {
		case noParenForRe.MatchString(scan): // `for x := range … {`, `for x in … {`, `for {`
			return trimNoParenCond(scan), true, true
		case noParenWhileRe.MatchString(scan):
			return trimNoParenCond(scan), true, true
		case noParenElseIfRe.MatchString(scan):
			return trimNoParenCond(scan), false, true
		case noParenIfRe.MatchString(scan): // `if cond {`, `if let … {`
			return trimNoParenCond(scan), false, true
		}
	}
	// --- else / fallthrough (all brace dialects) -------------------------
	switch {
	case braceElseIfRe.MatchString(scan):
		return trimBraceCond(scan), false, true
	case braceElseRe.MatchString(scan):
		return "else", false, true
	}
	return "", false, false
}

// trimNoParenCond returns the header text up to (excluding) the opening `{` for
// a no-paren / keyword-condition header (`if force {`, `for row in rows {`,
// `match x {`, `when {`, `guard … else {`). It keeps the keyword + predicate so
// the surfaced condition reads naturally.
func trimNoParenCond(scan string) string {
	s := strings.TrimSpace(stripLeadingCloser(scan))
	if idx := strings.Index(s, "{"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

// trimBraceCond returns the header text up to and including its closing `)` (or
// the trimmed header for paren-less headers like `else`/`try`), dropping a
// trailing `{` and any leading `} ` closer.
func trimBraceCond(scan string) string {
	s := strings.TrimSpace(stripLeadingCloser(scan))
	if idx := strings.LastIndex(s, ")"); idx >= 0 {
		return strings.TrimSpace(s[:idx+1])
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "{")
	return strings.TrimSpace(s)
}

// braceSpanEnd returns the exclusive line index at which the brace block whose
// header is lines[headerIdx] closes. It locates the first `{` at/after the
// header (skipping a leading closer), then tracks depth to the matching `}`.
// When no opening brace is found within a short lookahead (brace-less
// single-statement body) the block is treated as spanning just the header +
// next line.
func braceSpanEnd(lines []string, headerIdx int) int {
	depth := 0
	opened := false
	for j := headerIdx; j < len(lines); j++ {
		scan := stripBraceNoise(lines[j])
		if j == headerIdx {
			scan = stripLeadingCloser(scan)
		}
		for _, r := range scan {
			switch r {
			case '{':
				depth++
				opened = true
			case '}':
				depth--
			}
		}
		if opened && depth <= 0 {
			return j + 1
		}
		if !opened && j-headerIdx > 2 {
			// Brace-less single-statement body (`if (x) doWrite();`).
			return j + 2
		}
	}
	return len(lines)
}

// --- cyclomatic complexity ------------------------------------------------

// decisionPointRe counts the control-flow decision points for cyclomatic
// complexity: if/elif/else-if, case, catch/except/rescue, ternary, &&/||,
// for/while/foreach. Mirrors enrichers.ComputeCyclomaticComplexity's keyword set
// but lives here so the substrate facet has a single, self-contained source of
// truth for the branch count surfaced alongside effect contexts. Language-
// neutral keyword set; comments/strings are not stripped (cheap, and the
// over-count from the rare keyword-in-string is negligible and conservative).
var decisionPointRe = []*regexp.Regexp{
	regexp.MustCompile(`\bif\b`),
	regexp.MustCompile(`\belif\b`),
	regexp.MustCompile(`\bfor\b`),
	regexp.MustCompile(`\bwhile\b`),
	regexp.MustCompile(`\bcase\b`),
	regexp.MustCompile(`\bcatch\b`),
	regexp.MustCompile(`\bexcept\b`),
	regexp.MustCompile(`\brescue\b`),
	regexp.MustCompile(`\.\s*for[Ee]ach\s*\(`),
	regexp.MustCompile(`\bdo\s*\|[^|]*\|`), // ruby/crystal iterator block `each do |x|`
	regexp.MustCompile(`\?[^.:?]`),         // ternary `?` (not `?.` optional chain, not `??`)
	regexp.MustCompile(`&&`),
	regexp.MustCompile(`\|\|`),
}

// ComputeFunctionComplexity returns the cyclomatic complexity (decision points
// + 1) and branch count (decision points) for a function source window.
// `else`/`else if` is counted via its `if`; a bare `else` adds no new path so
// it is intentionally NOT counted, matching the standard McCabe definition.
func ComputeFunctionComplexity(src string) FunctionComplexity {
	branches := 0
	for _, re := range decisionPointRe {
		branches += len(re.FindAllString(src, -1))
	}
	return FunctionComplexity{Cyclomatic: branches + 1, BranchCount: branches}
}
