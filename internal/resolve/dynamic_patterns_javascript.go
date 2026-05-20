package resolve

import "regexp"

// jsDynamicPatterns is the per-language dynamic-dispatch pattern catalog for
// JavaScript and TypeScript. Both language tags share this slice.
// Matches here tag a stub as DispositionDynamic.
//
// See the per-language catalog overview comment in refs.go for the design
// rationale behind the language-gated approach (Refs #44).
var jsDynamicPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^Reflect\.`),      // Reflect.apply / Reflect.construct / Reflect.get
	regexp.MustCompile(`^eval$`),          // bare eval (issue #95)
	regexp.MustCompile(`^eval\(`),         // eval(src)
	regexp.MustCompile(`^Function$`),      // bare Function constructor reference
	regexp.MustCompile(`^Function\(`),     // Function(src)
	regexp.MustCompile(`^new Function\(`), // new Function(src)
	// Dynamic import / require: must NOT be a literal-string first arg —
	// `require("fs")` and `import("./mod")` are statically resolvable.
	regexp.MustCompile("^require\\([^\"'`)]"),
	regexp.MustCompile("^import\\([^\"'`)]"),
	regexp.MustCompile(`^process\.env\.`), // env-driven (JS)
	// Wave-4 (TS framework) — relative-import paths. The JS/TS
	// extractor emits IMPORTS edges with the literal module string
	// as ToID (e.g. `./cart-context`, `../fragments/cart`, `.`,
	// `..`, `../..`). Every importing file produces its own
	// SCOPE.Component import entity for the same module string, so
	// bare-name lookup is ambiguous and the edge drives to
	// bug-resolver despite the placeholder being bookkeeping rather
	// than the imported symbol's source. Mirrors the Python relative-
	// import pattern above (#432) and the
	// `scope:component:import:local:` heuristic-scope-stub branch
	// (which the cross-language imports extractor emits for the same
	// shape). Pattern matches `.`, `..`, and any leading-`./`/`../`
	// path; anchored to avoid biting bare identifiers.
	regexp.MustCompile(`^\.{1,3}$`),
	regexp.MustCompile(`^\.{1,2}/`),
	// TS path-mapped local imports — tsconfig.json `baseUrl` /
	// `paths` lets imports look like `components/grid` or
	// `lib/shopify/types`. These are intra-repo (no leading dot, no
	// leading `@scope`, no `node:` prefix, no package dot-domain
	// like `next.js`) and the extractor emits one SCOPE.Component
	// per importing file, producing the same ambig-bare-no-hint
	// disposition as relative paths. Restrict to multi-segment
	// paths whose first segment is a common TS-monorepo source root
	// (`src`, `app`, `lib`, `components`, `pages`, `hooks`,
	// `utils`, `helpers`, `services`, `store`, `styles`, `types`,
	// `config`, `constants`, `features`, `modules`, `domain`,
	// `data`, `api`, `server`, `client`, `shared`, `core`,
	// `common`, `models`, `views`, `controllers`, `middleware`,
	// `tests`, `test`, `__tests__`, `__mocks__`, `routes`). The
	// per-language gate (js/ts only) keeps these names from
	// shadowing real go/python/etc. modules with the same prefix.
	regexp.MustCompile(`^(src|app|lib|components|pages|hooks|utils|helpers|services|store|styles|types|config|constants|features|modules|domain|data|api|server|client|shared|core|common|models|views|controllers|middleware|tests|test|__tests__|__mocks__|routes)/`),
	// JS reflective `Function.prototype.{bind,apply,call}` is real, but
	// the bare `.bind(` / `.apply(` / `.call(` patterns collide with too
	// many domain methods (DB driver `bind`, `discount.apply(order)`,
	// `controller.call(...)`). Keep them out of the JS catalog; the
	// extractors tag truly reflective uses (e.g. `Reflect.apply`) which
	// the explicit `Reflect\.` pattern above already covers.

	// Wave-7 (TS/JS React frontend, #519) — React useState setter
	// destructure pattern. The JS extractor strips the receiver from
	// destructured tuples (`const [v, setV] = useState(...)`), leaving
	// bare `setV` callee names that the resolver cannot bind because
	// the symbol is component-local and the extractor doesn't know its
	// origin. The `set[A-Z]...` convention is universal in the React
	// community (RFC + React docs) and the per-language gate
	// (js/ts only) prevents collision with `setHeader` / `setCookie`
	// style helpers in non-JS code. Names are bare leaf identifiers.
	regexp.MustCompile(`^set[A-Z][A-Za-z0-9_]*$`),
	// Promise chain methods — `then`, `catch`, `finally` — bare-name
	// callees on the result of `await`-able / Promise-returning
	// functions. The extractor emits the chained method as a bare
	// identifier with the receiver stripped, and the receiver is a
	// Promise value the resolver cannot model. `then` alone is the
	// dominant residual in client-fixture-b. JS-only gate keeps these
	// out of Ruby (`then` is a real method) / Go / Python collisions.
	regexp.MustCompile(`^then$`),
	regexp.MustCompile(`^catch$`),
	regexp.MustCompile(`^finally$`),
	// Wave-11 (TS/JS React frontend, ship-gate) — React handler-prop
	// convention `onClose`, `onClick`, `onChange`, `onSubmit`,
	// `onCancel`, `onConfirm`, `onSuccess`, `onError`, `onValueChange`,
	// `onSelect`, `onFocus`, `onBlur`, etc. These are callable props
	// passed from a parent component; the actual handler implementation
	// lives in the parent and is bound at component-invocation time, so
	// the call site (inside the child component) cannot statically bind
	// it. Standard React convention (React docs + RFC). The per-language
	// gate (js/ts only) keeps it from biting Python/Go/etc. where the
	// `onX` callable-prop convention does not exist. client-fixture-b
	// top bug-extractor sample after wave-10 was `onClearAll` /
	// `onClose` confirming this is the dominant residual shape.
	regexp.MustCompile(`^on[A-Z][A-Za-z0-9]*$`),
	// Wave-13 (TS/JS React frontend, real-residue) — `handle*` and
	// `after*` callable-prop / lifecycle-hook conventions. Mirrors
	// the wave-11 `^on[A-Z]...` rule. React/JSX components routinely
	// destructure callable props named `handleClientSelection`,
	// `handleReloadData`, `handleSaveOnCell`, `afterSaveNote`,
	// `afterCreateSuccess` from parent components or pass them as
	// `useCallback` returns. The actual implementation lives in the
	// parent (or in a higher-order hook) and is bound at component-
	// invocation time, so the call site cannot statically bind it.
	// `handleX` is universal React tutorial style (React docs +
	// every state-of-the-art repo); `afterX` is form/lifecycle hook
	// convention (antd Form `afterClose`, react-hook-form `afterSubmit`).
	// Both names dominate cfb wave-12-FINAL bug-extractor + bug-resolver
	// residues (`handleClientSelection`, `handleReloadData`,
	// `afterSaveNote`, `afterSaveSuccess`, `afterCreateSuccess`).
	// Same-file preference resolver (wave-9 Chain-fix A) fires BEFORE
	// the dynamic pattern check via the hex-ID branch in
	// classifyDispositionLang, so a same-file lifted handler entity
	// still wins. Per-language gate (js/ts only) keeps these from
	// shadowing `HandleXxx` Go method conventions / Python `handle_*`
	// snake_case (covered by other patterns). Conservative scope:
	// `get*` / `set*` / `load*` / `save*` / `create*` / `update*` /
	// `delete*` / `fetch*` / `use*` / `submit*` / `cancel*` / `select*`
	// / `reset*` / `toggle*` are deliberately EXCLUDED — those verbs
	// shadow real user-defined entities (services, repos, mutators)
	// far more aggressively than `handle*`/`after*`.
	regexp.MustCompile(`^handle[A-Z][A-Za-z0-9]*$`),
	regexp.MustCompile(`^after[A-Z][A-Za-z0-9]*$`),
	// PLT #537 RN/Expo wave — React ref + responsive-design + RN
	// notification idioms that the JS extractor strips to bare leaf
	// identifiers. `current` is the universal React useRef property
	// (`ref.current`); `isTablet` / `isMobile` / `isLandscape` /
	// `isPortrait` are responsive-design flags returned by
	// react-native-device-info / useResponsive hooks and consumed as
	// bare destructured locals; `enqueue` is the RN notification
	// queue method on the NotificationContext / Toast / Snackbar
	// receivers (also antd's notification.enqueue). These names are
	// indistinguishable from real user methods to the resolver
	// after the receiver is stripped — same shape as the wave-11
	// `^on[A-Z]` rule. JS-only gate keeps them out of Go / Python /
	// Java where `current` is a real first-class symbol name.
	regexp.MustCompile(`^current$`),
	regexp.MustCompile(`^isTablet$`),
	regexp.MustCompile(`^isMobile$`),
	regexp.MustCompile(`^isLandscape$`),
	regexp.MustCompile(`^isPortrait$`),
	regexp.MustCompile(`^isDesktop$`),
	regexp.MustCompile(`^enqueue$`),
}

func init() {
	dynamicPatternsByLang["javascript"] = jsDynamicPatterns
	dynamicPatternsByLang["typescript"] = jsDynamicPatterns
}
