(function () {
  "use strict";

  var USE_CASES = [
    { cat: "Navigate", tab: "End-to-end lineage",
      question: 'Where does <code>amountDue</code> on the invoice report come from — end to end?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -r amountDue .' },
        { out: '37 matches across <span class="hit">9 files</span> — a component, a controller, a SQL string' },
        { cmd: '<span class="kw">read</span> InvoiceReport.tsx, reportsController.ts, +6 more' },
        { out: 'the strings match — but nothing <span class="hit">connects</span> them, and no column is named' } ],
        verdict: 'Reads 14 files, still guessing which DB column backs it',
        miss: 'Reads 14 files and still guesses which DB column backs the field' },
      grafel: { call: '<span class="kw">grafel_trace</span> amountDue', trace: [
        { kind: "web", name: "InvoiceReport.tsx" },
        { kind: "endpoint", name: "GET /reports/invoices" },
        { kind: "guard", name: "requirePermission('billing:read')" },
        { kind: "service", name: "BillingController.getInvoiceReport" },
        { kind: "orm", name: "InvoiceRepository.computeAmountDue()" },
        { kind: "query", name: "SELECT … FROM invoice_lines JOIN invoices" },
        { kind: "table", name: "invoices.amount_due · Postgres", terminal: true } ],
        verdict: 'Full lineage: component → endpoint → guard → service → ORM → query → column',
        proof: 'Full lineage from component to column in one call, zero guessing' } },

    { cat: "Navigate", tab: "Who calls this?",
      question: 'What calls <code>PaymentService.charge</code> — across every repo?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -rn "\\.charge(" .' },
        { out: 'matches mix real callers with <span class="hit">definitions and look-alikes</span>' },
        { cmd: '<span class="kw">read</span> each hit to tell caller from noise' },
        { out: 'a background <span class="hit">worker in another repo</span> never shows up' } ],
        verdict: 'Can\'t separate callers from noise, misses cross-repo callers',
        miss: 'Can\'t separate real callers from look-alikes; misses cross-repo callers' },
      grafel: { call: '<span class="kw">grafel_related</span> PaymentService.charge --callers', trace: [
        { kind: "caller", name: "CheckoutController.pay · api-gateway" },
        { kind: "caller", name: "SubscriptionRenewer.run · billing-jobs" },
        { kind: "caller", name: "StripeWebhookHandler · billing-service" },
        { kind: "result", name: "3 real callers · 2 repos · 0 false hits", terminal: true } ],
        verdict: 'Exact callers across repos — including the job you\'d have missed',
        proof: 'Exact callers across every repo, ranked, zero false positives' } },

    { cat: "Navigate", tab: "Orient a new area",
      question: 'I just joined — what are the entry points and load-bearing files here?',
      grep: { lines: [
        { cmd: '<span class="kw">ls</span> -R  ·  <span class="kw">cat</span> README.md' },
        { out: 'a file tree tells you names, <span class="hit">not what matters</span>' },
        { cmd: '<span class="kw">grep</span> "func main" · open, wander, guess' },
        { out: 'no signal on which files everything depends on' } ],
        verdict: 'Wanders the tree with no sense of what\'s central',
        miss: 'A file tree gives names, not what\'s load-bearing or where to start' },
      grafel: { call: '<span class="kw">grafel_orient</span>', trace: [
        { kind: "entry", name: "HTTP routes · CLI commands · scheduled jobs" },
        { kind: "hotspot", name: "AuthMiddleware, DbPool, EventBus (PageRank)" },
        { kind: "modules", name: "6 communities — billing, auth, notify, …" },
        { kind: "start-here", name: "ranked reading list for the change", terminal: true } ],
        verdict: 'Entry points + PageRank hotspots + module map, in one call',
        proof: 'Entry points, PageRank hotspots, and module map in one call' } },

    { cat: "Navigate", tab: "Find where it's defined",
      question: 'Where exactly is <code>DiscountPolicy</code> defined — we have three similarly named types?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -rn "class DiscountPolicy" .' },
        { out: '3 hits — billing-service, billing-jobs, and a stale copy in archive/' },
        { cmd: '<span class="kw">read</span> all three, diff them by eye' },
        { out: 'can\'t tell which one is live without tracing imports by hand' } ],
        verdict: 'Finds 3 candidates, can\'t tell which is the real one',
        miss: 'Three same-named hits, no way to rank them or know which is dead' },
      grafel: { call: '<span class="kw">grafel_find</span> DiscountPolicy → <span class="kw">grafel_inspect</span>', trace: [
        { kind: "match", name: "3 candidates ranked by BM25 + live usage" },
        { kind: "live", name: "billing-service/pricing/DiscountPolicy.ts · 41 refs" },
        { kind: "stale", name: "archive/legacy copy · 0 references" },
        { kind: "shape", name: "fields, methods, callers via grafel_inspect", terminal: true } ],
        verdict: 'Ranked to the live definition, dead copies flagged by reference count',
        proof: 'BM25-ranked to the one live definition, with reference counts as proof' } },

    { cat: "Navigate", tab: "Show me the source",
      question: 'Just show me <code>InvoiceRepository.computeAmountDue</code> — not the whole file.',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -n computeAmountDue InvoiceRepository.ts' },
        { out: 'a line number — still opens the whole <span class="hit">640-line file</span>' },
        { cmd: '<span class="kw">open</span> file, scroll to line, guess where it ends' } ],
        verdict: 'Reads a 640-line file to see one 12-line method',
        miss: 'Opens the whole file to read one method — no symbol boundaries' },
      grafel: { call: '<span class="kw">grafel_get_source</span> InvoiceRepository.computeAmountDue', trace: [
        { kind: "source", name: "exact method body · 12 lines" },
        { kind: "signature", name: "(invoiceId: string): Money" },
        { kind: "doc", name: "inline comment: rounds to nearest cent", terminal: true } ],
        verdict: 'Exact symbol body only — no file-scrolling, no guessing bounds',
        proof: 'Exact function body, signature, and doc comment — nothing else' } },

    { cat: "Change safely", tab: "Rename blast radius",
      question: "I'm renaming <code>Invoice.amountDue</code> → <code>Invoice.balance</code>. What breaks?",
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -rw amountDue .' },
        { out: 'dozens of hits — <span class="hit">false positives</span> everywhere' },
        { out: 'web-app &amp; mobile-app read it off the <span class="hit">JSON response</span> — crosses the wire' },
        { out: 'grep can\'t follow an HTTP payload shape between repos' } ],
        verdict: 'Misses frontend + mobile — a "backend rename" that breaks two apps',
        miss: 'Dozens of false-positive hits; can\'t follow the field across the wire' },
      grafel: { call: '<span class="kw">grafel_impact_radius</span> Invoice.amountDue', trace: [
        { kind: "model", name: "billing-service · Invoice.amountDue (+3 sites)" },
        { kind: "mapping", name: "→ invoices.amount_due" },
        { kind: "http", name: "payload field crosses the wire" },
        { kind: "web", name: "web-app · InvoiceCard" },
        { kind: "mobile", name: "mobile-app · InvoiceScreen", terminal: true } ],
        verdict: 'Payload shape tracked across repos: backend + web + mobile',
        proof: 'Payload shape tracked end-to-end — backend, web, and mobile' } },

    { cat: "Change safely", tab: "PR diff impact",
      question: 'My PR edits 6 files — what downstream actually depends on them?',
      grep: { lines: [
        { cmd: '<span class="kw">git</span> diff --stat  ·  6 files changed' },
        { out: 'the diff shows what you touched, <span class="hit">not who depends on it</span>' },
        { cmd: '<span class="kw">grep</span> importing symbols, one by one' },
        { out: 'transitive + cross-wire consumers stay <span class="hit">invisible</span>' } ],
        verdict: 'Sees the edit, not the downstream fan-out',
        miss: 'Diff shows what changed, not what depends on it downstream' },
      grafel: { call: '<span class="kw">grafel_diff</span> --impact HEAD~1', trace: [
        { kind: "changed", name: "6 files · 11 entities touched" },
        { kind: "fans-to", name: "3 endpoints · 2 scheduled jobs" },
        { kind: "shared", name: "a DTO consumed by mobile-app" },
        { kind: "verdict", name: "review web + mobile before merge", terminal: true } ],
        verdict: 'Real downstream fan-out of the diff, not just the touched files',
        proof: 'Real downstream fan-out — including the DTO mobile-app consumes' } },

    { cat: "Change safely", tab: "Test risk",
      question: 'I changed <code>OrderService.total()</code> — which tests cover it, and what\'s exposed?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -r OrderService test/' },
        { out: 'some direct hits — but <span class="hit">transitive coverage</span> is unknowable' },
        { out: 'which branch is untested? grep can\'t say' } ],
        verdict: 'Finds test files by name, not what actually exercises the code',
        miss: 'Finds test files by name, can\'t tell what actually exercises the code' },
      grafel: { call: '<span class="kw">grafel_test_analysis</span> OrderService.total', trace: [
        { kind: "covered", name: "3 tests hit total() directly" },
        { kind: "gap", name: "0 tests cover the discount branch it calls" },
        { kind: "downstream", name: "2 consumers of total() are untested" },
        { kind: "verdict", name: "add a discount-path test before shipping", terminal: true } ],
        verdict: 'Direct + transitive coverage, and the exact untested branch',
        proof: 'Direct and transitive coverage, plus the exact untested branch' } },

    { cat: "Change safely", tab: "Safe to delete?",
      question: 'Can I delete <code>LegacyInvoicePdfExporter</code> — is anything still calling it?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -rn LegacyInvoicePdfExporter .' },
        { out: '2 hits — the class def and one import in a <span class="hit">barrel file</span>' },
        { out: 'the barrel is re-exported everywhere — is it used further?' },
        { cmd: '<span class="kw">chase</span> the re-export across 5 more files by hand' } ],
        verdict: 'Barrel re-exports hide the real call sites — deletion is a guess',
        miss: 'Barrel-file re-exports hide real usage; deletion is a coin flip' },
      grafel: { call: '<span class="kw">grafel_impact_radius</span> LegacyInvoicePdfExporter --unused', trace: [
        { kind: "refs", name: "0 live callers across 3 repos" },
        { kind: "export", name: "re-exported via index.ts, never invoked" },
        { kind: "verdict", name: "safe to delete — zero inbound edges confirmed", terminal: true } ],
        verdict: 'Zero inbound edges across every repo — deletion confirmed safe',
        proof: 'Traced every re-export — zero live callers, safe to delete' } },

    { cat: "Architecture & health", tab: "Modules & cycles",
      question: 'Where are the real module boundaries, and which packages cycle?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -rh "^import" . | sort | uniq' },
        { out: 'thousands of import lines — <span class="hit">no structure</span>' },
        { out: 'a dependency cycle is invisible to text search' } ],
        verdict: 'Import lines don\'t reveal boundaries or cycles',
        miss: 'Import lines don\'t reveal boundaries, and a cycle is invisible to text search' },
      grafel: { call: '<span class="kw">grafel_subgraph</span> --communities --cycles', trace: [
        { kind: "modules", name: "community detection → 6 modules" },
        { kind: "cycle", name: "billing ⇄ notifications import cycle" },
        { kind: "articulation", name: "shared/util is a single point of failure" },
        { kind: "verdict", name: "break the cycle, split the util", terminal: true } ],
        verdict: 'Recovered module map, the cycle, and the articulation point',
        proof: 'Recovered module map, the real cycle, and the articulation point' } },

    { cat: "Architecture & health", tab: "Dead code & debt",
      question: 'What\'s unused, and where are the god-classes?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -r exportedSymbol .  <span style="opacity:.55">// one at a time</span>' },
        { out: 'used or not? grep <span class="hit">can\'t see dynamic dispatch</span>' },
        { out: 'god-classes need fan-in / fan-out — not a text metric' } ],
        verdict: 'Per-symbol, slow, and blind to indirect use',
        miss: 'Per-symbol grep is slow and blind to dynamic dispatch' },
      grafel: { call: '<span class="kw">grafel_debt</span>', trace: [
        { kind: "dead", name: "14 exports with zero inbound edges" },
        { kind: "god", name: "3 classes by fan-in/out (OrderManager, …)" },
        { kind: "dup", name: "an auth check copy-pasted in 4 handlers" },
        { kind: "verdict", name: "prioritized debt list with call-graph proof", terminal: true } ],
        verdict: 'Unused code, god-classes, and duplication — from the graph',
        proof: 'Unused exports, god-classes, and duplication — proven by the call graph' } },

    { cat: "Architecture & health", tab: "Layering violations",
      question: 'Is our controller layer reaching straight into the DB, skipping the service layer?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -rl "db.query(" controllers/' },
        { out: 'hits exist, but so do <span class="hit">comments and test mocks</span>' },
        { out: 'no way to confirm a real violation vs. an approved exception' } ],
        verdict: 'Text hits without knowing which are real layering violations',
        miss: 'Can\'t tell a real layering violation from a comment or approved exception' },
      grafel: { call: '<span class="kw">grafel_patterns</span> --layering', trace: [
        { kind: "rule", name: "controller → service → repository → db" },
        { kind: "violation", name: "InvoiceController calls db.query directly (2 sites)" },
        { kind: "exception", name: "1 site tagged @allowed-legacy" },
        { kind: "verdict", name: "2 real violations, 1 approved exception", terminal: true } ],
        verdict: 'Real violations separated from approved exceptions and comments',
        proof: 'Structural rule check: 2 real violations, 1 approved exception, no noise' } },

    { cat: "Architecture & health", tab: "Complexity hotspots",
      question: 'Where should code review focus this sprint — what\'s actually risky?',
      grep: { lines: [
        { cmd: '<span class="kw">wc</span> -l **/*.ts | sort -rn | head' },
        { out: 'the longest files, not the <span class="hit">riskiest</span> ones' },
        { out: 'line count says nothing about fan-in, churn, or test coverage' } ],
        verdict: 'Ranks by file length — no signal on real risk',
        miss: 'Line count is a proxy for nothing — the riskiest files aren\'t the longest' },
      grafel: { call: '<span class="kw">grafel_findings</span> --hotspots', trace: [
        { kind: "hotspot", name: "PaymentService.charge — high fan-in, low coverage" },
        { kind: "hotspot", name: "BillingController — high churn, cyclomatic 24" },
        { kind: "safe", name: "InvoiceReport.tsx — high churn but well-tested" },
        { kind: "verdict", name: "2 files worth a closer review this sprint", terminal: true } ],
        verdict: 'Ranked by fan-in, churn, and test coverage — not line count',
        proof: 'Ranked by fan-in × churn × coverage gap — the actual risk signal' } },

    { cat: "Cross-repo & infra", tab: "Infra ↔ code",
      question: "We're renaming the <code>invoice-events</code> topic and moving the <code>reports</code> DB — what's affected?",
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -r "invoice-events" .' },
        { out: 'scattered across <span class="hit">Terraform, Helm, and code</span> — just strings' },
        { out: 'who publishes? who consumes? grep can\'t tell' } ],
        verdict: 'Finds the names, not the wiring — publishers/consumers invisible',
        miss: 'Finds the topic name in 3 places, can\'t say who publishes or consumes' },
      grafel: { call: '<span class="kw">grafel_cross_links</span> invoice-events, aws_db_instance.reports', trace: [
        { kind: "topic", name: "invoice-events (Kafka)" },
        { kind: "publish", name: "published by → billing-service" },
        { kind: "consume", name: "consumed by → notifications, analytics" },
        { kind: "terraform", name: "aws_db_instance.reports connstring" },
        { kind: "services", name: "→ flows via ConfigMap into billing + analytics", terminal: true } ],
        verdict: 'Links infra to code: real blast radius across infra ↔ services',
        proof: 'Infra linked to code — real publishers, consumers, and blast radius' } },

    { cat: "Cross-repo & infra", tab: "Cross-repo event trace",
      question: 'When a payment succeeds, what actually happens end to end across our services?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -r "payment.succeeded" .' },
        { out: 'matches in 4 repos — a publish call, three maybe-subscribers' },
        { out: 'can\'t confirm which handlers actually subscribe vs. just mention it' } ],
        verdict: 'Finds the string in 4 repos, can\'t confirm the real subscribers',
        miss: 'String matches don\'t confirm real subscription — dead handlers look alive' },
      grafel: { call: '<span class="kw">grafel_trace</span> payment.succeeded --cross-repo', trace: [
        { kind: "publish", name: "PaymentService.charge() → billing-service" },
        { kind: "topic", name: "payment.succeeded (Kafka)" },
        { kind: "subscribe", name: "InvoiceFinalizer · billing-jobs" },
        { kind: "subscribe", name: "ReceiptEmailer · notifications" },
        { kind: "dead", name: "AnalyticsSink · analytics — subscribed, never deployed", terminal: true } ],
        verdict: 'Full publish→consume chain across 3 repos, plus a dead subscriber',
        proof: 'Confirmed publish→consume chain, including a subscriber never deployed' } },

    { cat: "Cross-repo & infra", tab: "Shared DTO drift",
      question: 'Did the <code>InvoiceDTO</code> shape drift between the backend and what web-app / mobile-app expect?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -A8 "interface InvoiceDTO" billing-service/ web-app/ mobile-app/' },
        { out: 'three separate type definitions, formatted differently' },
        { out: 'eyeballing 3 field lists for a mismatch is error-prone' } ],
        verdict: 'Manual field-by-field comparison across 3 repos, easy to miss one',
        miss: 'Eyeballing 3 type definitions for drift — easy to miss one field' },
      grafel: { call: '<span class="kw">grafel_find_paths</span> InvoiceDTO --cross-repo', trace: [
        { kind: "source", name: "billing-service · InvoiceDTO (7 fields)" },
        { kind: "consumer", name: "web-app · InvoiceCard expects 7 fields — match" },
        { kind: "consumer", name: "mobile-app · InvoiceScreen expects 6 — missing dueDate", terminal: true } ],
        verdict: 'Field-level diff across repos — mobile-app is silently missing a field',
        proof: 'Field-level structural diff — caught the field mobile-app never got' } },

    { cat: "Security & data-flow", tab: "HTTP surface & leaks",
      question: 'What\'s our full HTTP surface — and is anything unauthenticated or leaking a sensitive field?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -rE "@(Get|Post)Mapping|router\\.(get|post)"' },
        { out: 'per-framework decorators — <span class="hit">easy to miss</span> some' },
        { out: 'auth &amp; serialization aren\'t a text pattern' } ],
        verdict: 'Enumerates some routes, can\'t judge auth or what they leak',
        miss: 'Enumerates some routes by decorator pattern, can\'t judge auth or leaks' },
      grafel: { call: '<span class="kw">grafel_endpoints</span> + <span class="kw">grafel_security</span>', trace: [
        { kind: "surface", name: "41 endpoints across 3 services" },
        { kind: "unauth", name: "2 endpoints have no auth guard" },
        { kind: "leak", name: "GET /users/:id serializes User.passwordHash" },
        { kind: "verdict", name: "guard the 2 routes, drop the field", terminal: true } ],
        verdict: 'Full surface + the unauthenticated routes + the PII leak',
        proof: 'Full surface, the unauthenticated routes, and the exact leaked field' } },

    { cat: "Security & data-flow", tab: "Taint to sink",
      question: 'Can a user-supplied invoice note reach a raw SQL query anywhere?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -rn note . | grep -i sql' },
        { out: 'no direct hits — the flow goes through <span class="hit">2 intermediate functions</span>' },
        { out: 'grep can\'t follow data through variables and function calls' } ],
        verdict: 'Text search can\'t follow tainted data through intermediate functions',
        miss: 'Can\'t follow tainted data through 2 hops of function calls' },
      grafel: { call: '<span class="kw">grafel_security</span> --taint note', trace: [
        { kind: "source", name: "InvoiceController.addNote(req.body.note)" },
        { kind: "pass", name: "NoteService.save() — no sanitization" },
        { kind: "sink", name: "raw SQL concatenation in NoteRepository.insert()", terminal: true } ],
        verdict: 'Taint-flow traced from user input to an unsanitized SQL sink',
        proof: 'Full taint path from request body to the unsanitized SQL sink' } },

    { cat: "Security & data-flow", tab: "Secret in config",
      question: 'Is there a hardcoded credential anywhere in our Terraform or app config?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -rEi "(api[_-]?key|secret|password)\\s*=" .' },
        { out: '60+ hits — mostly <span class="hit">variable names and env-var refs</span>' },
        { out: 'telling a real leaked value from a placeholder takes reading each one' } ],
        verdict: '60 noisy hits, no way to separate real secrets from placeholders',
        miss: '60 noisy grep hits, mostly env-var references and placeholders' },
      grafel: { call: '<span class="kw">grafel_findings</span> --secrets', trace: [
        { kind: "finding", name: "billing-service/config/staging.yaml:14" },
        { kind: "value", name: "stripe_secret_key hardcoded, not env-sourced" },
        { kind: "severity", name: "high — committed, not templated", terminal: true } ],
        verdict: 'One real hardcoded secret flagged, ranked by severity',
        proof: 'One real hardcoded secret, ranked high, with exact file and line' } },

    { cat: "Security & data-flow", tab: "Auth guard drift",
      question: 'Did we forget an auth guard on any endpoint that clones an existing one?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -B3 "@Get\\|@Post" **/*.controller.ts' },
        { out: 'shows the decorators, not whether a <span class="hit">guard sits above them</span>' },
        { out: 'comparing 41 endpoints by eye for a missing decorator is slow' } ],
        verdict: 'Manually eyeballing 41 endpoints for a missing decorator',
        miss: '41 endpoints to eyeball for one missing decorator — slow, error-prone' },
      grafel: { call: '<span class="kw">grafel_patterns</span> --auth-guards', trace: [
        { kind: "rule", name: "endpoints under /billing/* require @RequireAuth" },
        { kind: "violation", name: "POST /billing/invoices/:id/void — guard missing" },
        { kind: "verdict", name: "1 endpoint drifted from the pattern", terminal: true } ],
        verdict: 'Pattern-matched against sibling endpoints — found the one that drifted',
        proof: 'Matched against the sibling pattern — the exact endpoint that drifted' } },

    { cat: "Data & persistence", tab: "Column lineage",
      question: 'What code actually reads or writes <code>payments.status</code>?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -rn "payments.status\\|\\.status" .' },
        { out: 'hundreds of hits — <span class="hit">\'status\'</span> is used on a dozen types' },
        { out: 'no way to filter to just this one column' } ],
        verdict: '\'status\' is too common a name — hundreds of unrelated hits',
        miss: 'Common field name drowns in false positives from unrelated types' },
      grafel: { call: '<span class="kw">grafel_related</span> payments.status --readers --writers', trace: [
        { kind: "writer", name: "PaymentService.markPaid() — UPDATE payments SET status" },
        { kind: "writer", name: "PaymentWebhookHandler — UPDATE on webhook receipt" },
        { kind: "reader", name: "InvoiceReport query — SELECT … WHERE status = 'paid'", terminal: true } ],
        verdict: '2 writers, 1 reader — resolved to this exact column, not the field name',
        proof: 'Resolved to the exact column: 2 writers, 1 reader, nothing else' } },

    { cat: "Data & persistence", tab: "N+1 query sites",
      question: 'Do we have an N+1 query hiding in the invoice list endpoint?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -n for InvoiceListController.ts' },
        { out: 'loops everywhere — which one issues a <span class="hit">query per iteration</span>?' },
        { out: 'the ORM call is 2 functions away from the loop' } ],
        verdict: 'Loops are visible, the query-per-iteration isn\'t',
        miss: 'Loop and query are 2 function calls apart — invisible to a text scan' },
      grafel: { call: '<span class="kw">grafel_patterns</span> --n-plus-one', trace: [
        { kind: "loop", name: "invoices.forEach(inv => …)" },
        { kind: "call", name: "→ LineItemRepository.findByInvoice(inv.id)" },
        { kind: "query", name: "issues one SELECT per invoice — N+1 confirmed", terminal: true } ],
        verdict: 'Confirmed N+1: one query per loop iteration, 2 calls deep',
        proof: 'Traced the loop to the per-iteration query — N+1 confirmed structurally' } },

    { cat: "Data & persistence", tab: "Migration impact",
      question: 'We\'re dropping the <code>invoices.legacy_currency</code> column — what breaks?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -rn legacy_currency .' },
        { out: '5 hits in migrations and one ORM model' },
        { out: 'does any query or report actually read it? grep can\'t confirm' } ],
        verdict: 'Finds the column name, can\'t confirm what actually depends on it',
        miss: 'Can\'t confirm which readers are live versus dead migration cruft' },
      grafel: { call: '<span class="kw">grafel_impact_radius</span> invoices.legacy_currency', trace: [
        { kind: "column", name: "invoices.legacy_currency" },
        { kind: "reader", name: "CurrencyReconciliationJob — reads it weekly" },
        { kind: "reader", name: "FinanceExport CSV — includes the column", terminal: true } ],
        verdict: 'Two live consumers found — the drop would break a weekly job',
        proof: 'Two live consumers found — dropping the column breaks a scheduled job' } },

    { cat: "APIs & events", tab: "Event pub/sub map",
      question: 'What\'s our full event topology — every topic, publisher, and consumer?',
      grep: { lines: [
        { cmd: '<span class="kw">grep</span> -rn "\\.publish(\\|\\.subscribe(" .' },
        { out: 'calls found, but the topic name is a <span class="hit">runtime variable</span> in half' },
        { out: 'no aggregate map — just a scattered list per repo' } ],
        verdict: 'Topic names are runtime variables in half the call sites — grep sees nothing',
        miss: 'Runtime topic-name variables are invisible to grep — no aggregate view' },
      grafel: { call: '<span class="kw">grafel_event</span> --map', trace: [
        { kind: "topic", name: "invoice-events → 1 publisher, 2 consumers" },
        { kind: "topic", name: "payment.succeeded → 1 publisher, 3 consumers" },
        { kind: "topic", name: "user.deleted → 1 publisher, 0 consumers (orphaned)", terminal: true } ],
        verdict: 'Full topology resolved, including an orphaned topic nobody consumes',
        proof: 'Full pub/sub topology resolved, including an orphaned topic' } },

    { cat: "APIs & events", tab: "Breaking API change",
      question: 'Does this PR break the public API contract for any external consumer?',
      grep: { lines: [
        { cmd: '<span class="kw">git</span> diff -- **/*.controller.ts' },
        { out: 'shows the field diff, not <span class="hit">who consumes the response shape</span>' },
        { out: 'no signal on whether a required field just became optional' } ],
        verdict: 'Sees the field diff, not which consumer actually breaks',
        miss: 'Diff shows the field changed, not which consumer\'s code actually breaks' },
      grafel: { call: '<span class="kw">grafel_diff</span> --api-compat HEAD~1', trace: [
        { kind: "change", name: "InvoiceResponse.dueDate: string → string | null" },
        { kind: "consumer", name: "web-app renders dueDate unguarded — will throw on null" },
        { kind: "consumer", name: "mobile-app already null-checks — unaffected", terminal: true } ],
        verdict: 'One real breaking change identified, scoped to the consumer that breaks',
        proof: 'Scoped to the one consumer that breaks — the other was already defensive' } },

    { cat: "Onboarding & docs", tab: "Generate docs",
      question: 'I need onboarding docs for the billing module before the new hire starts Monday.',
      grep: { lines: [
        { cmd: '<span class="kw">read</span> every file in billing/ and take notes by hand' },
        { out: 'hours of manual reading, and it\'s <span class="hit">stale the day someone refactors</span>' },
        { out: 'no consistent structure across modules written this way' } ],
        verdict: 'Hours of manual reading, and stale the moment the code changes',
        miss: 'Hand-written notes go stale the moment someone refactors' },
      grafel: { call: '<span class="kw">grafel_docgen</span> billing --module', trace: [
        { kind: "overview", name: "module purpose, entry points, key entities" },
        { kind: "reference", name: "public API surface, generated from the graph" },
        { kind: "diagram", name: "dependency map to adjacent modules", terminal: true } ],
        verdict: 'Generated from the live graph — regenerates on the next index, not stale',
        proof: 'Generated straight from the live graph — clean after every change' } },

    { cat: "Onboarding & docs", tab: "Is the graph fresh?",
      question: 'Before I trust any of this, is the index actually caught up with HEAD?',
      grep: { lines: [
        { cmd: '<span class="kw">git</span> log -1  ·  eyeball whether tooling "looks right"' },
        { out: 'no way to confirm any tool\'s output reflects the <span class="hit">current commit</span>' },
        { out: 'silent staleness is invisible until something\'s obviously wrong' } ],
        verdict: 'No signal on whether any answer reflects the current commit',
        miss: 'Silent staleness — no way to know if an answer reflects an old commit' },
      grafel: { call: '<span class="kw">grafel_index_status</span>', trace: [
        { kind: "commit", name: "indexed HEAD 6b6f184 · 40s ago" },
        { kind: "coverage", name: "3/3 repos in the group, 0 pending" },
        { kind: "verdict", name: "graph is current — safe to trust the last answer", terminal: true } ],
        verdict: 'Explicit freshness check — confirms the graph matches HEAD before you trust it',
        proof: 'Explicit commit-level freshness check before you trust anything' } }
  ];

  // Agent narration, one grep line + one grafel line per USE_CASES entry, same index order.
  var NARR = [
    { g: "Let me trace amountDue through every layer by hand…", k: "One call and I can see the whole lineage — let me just confirm the terminal column." },
    { g: "I’ll read through these call sites to separate real callers from noise…", k: "The graph already resolved every caller — no guessing which hits are real." },
    { g: "Let me wander the tree and grep for main() to get my bearings…", k: "That’s the whole map in one shot — entry points, hotspots, and modules." },
    { g: "Three hits with the same name — let me open each and diff them by eye…", k: "Ranked to one live definition — no need to guess between look-alikes." },
    { g: "Let me open the whole file to find this one method…", k: "Exact function body, nothing else — no scrolling required." },
    { g: "Let me check whether this field crosses into the frontend too…", k: "That’s the full blast radius in one hop — backend, web, and mobile." },
    { g: "I’ll trace imports from each changed file one by one…", k: "The graph already knows what fans out from this diff — no manual import-chasing." },
    { g: "Let me check which test files even mention this class…", k: "It surfaces the untested branch directly — no coverage report needed." },
    { g: "This barrel file re-exports it — let me chase where that goes…", k: "Zero inbound edges across every repo — that’s a confident deletion." },
    { g: "Let me sort all the import lines and look for a pattern…", k: "The graph found the cycle and the articulation point directly — no import-sorting." },
    { g: "Let me grep each exported symbol one at a time to see if it’s used…", k: "Fan-in and fan-out are already computed — the debt list is right here." },
    { g: "I need to check each hit to rule out comments and test mocks…", k: "It separated the real violations from the approved exception automatically." },
    { g: "Longest files first — though that’s not the same as riskiest…", k: "Ranked by fan-in, churn, and coverage gap — that’s the real risk signal." },
    { g: "The topic name shows up in Terraform and code — but who’s actually wired to it?", k: "Infra and code in one graph — the publishers and consumers are right there." },
    { g: "Let me check each of these four repos to see who’s really subscribed…", k: "The full publish-to-consume chain, including a subscriber that was never deployed." },
    { g: "Let me line up all three field lists and compare them by eye…", k: "Field-level diff across repos — it caught the missing field immediately." },
    { g: "Let me check each decorator to see if auth is actually applied…", k: "Full surface plus the leak, in one pass — no manual route counting." },
    { g: "No direct hits — let me trace this through the intermediate functions…", k: "It followed the data through both hops straight to the sink." },
    { g: "60 hits — let me read each one to rule out placeholders…", k: "One real hardcoded secret, already ranked by severity." },
    { g: "Let me compare all 41 endpoints against the pattern by hand…", k: "Matched against the sibling pattern — it found the one that drifted." },
    { g: "‘status’ is everywhere — let me filter by hand for the right type…", k: "Resolved to the exact column — writers and readers, nothing else." },
    { g: "Let me trace this loop two functions deep to see what it calls…", k: "It confirmed the per-iteration query without me tracing the call chain by hand." },
    { g: "Found the column name — now let me check if any of these reads are still live…", k: "Two live consumers surfaced immediately — this would break a weekly job." },
    { g: "Half these topic names are runtime variables — hard to build a map from text…", k: "Full topology resolved, including a topic nobody’s actually consuming." },
    { g: "The diff shows the field changed — now which consumer actually breaks?", k: "Scoped straight to the one consumer that isn’t null-safe." },
    { g: "Let me read through the whole module and take notes by hand…", k: "Generated straight from the live graph — it won’t go stale." },
    { g: "Let me check the last commit and just hope the tooling is caught up…", k: "Confirmed against HEAD — I can trust this answer." }
  ];

  var $ = function (id) { return document.getElementById(id); };
  var explorer = $("explorer"), rail = $("rail"), qCat = $("q-cat"), qText = $("q-text"),
      consoleEl = $("console"), qualityEl = $("quality"),
      btnGrep = $("btn-grep"), btnGrafel = $("btn-grafel"), glider = $("glider");
  var active = 0, mode = "grep";
  var catHeads = {}, catLists = {}, catOfIndex = [];

  // Accordion: open exactly one category, collapse the rest.
  function openCategory(cat) {
    Object.keys(catLists).forEach(function (c) {
      var isOpen = c === cat;
      catLists[c].classList.toggle("collapsed", !isOpen);
      catHeads[c].setAttribute("aria-expanded", isOpen ? "true" : "false");
    });
  }

  function buildRail() {
    var cats = [], byCat = {};
    USE_CASES.forEach(function (uc, i) {
      if (!byCat[uc.cat]) { byCat[uc.cat] = []; cats.push(uc.cat); }
      byCat[uc.cat].push({ uc: uc, i: i });
      catOfIndex[i] = uc.cat;
    });
    cats.forEach(function (c) {
      var group = document.createElement("div"); group.className = "cat";
      var head = document.createElement("button");
      head.type = "button"; head.className = "cat-head"; head.setAttribute("aria-expanded", "false");
      head.innerHTML = '<span class="cat-label">' + c + '</span><span class="chev" aria-hidden="true">&#9662;</span>';
      var list = document.createElement("div"); list.className = "cat-list collapsed";
      head.addEventListener("click", function () { openCategory(c); });
      group.appendChild(head);
      byCat[c].forEach(function (o) {
        var b = document.createElement("button");
        b.className = "rail-item"; b.setAttribute("role", "tab");
        b.setAttribute("aria-selected", o.i === 0 ? "true" : "false");
        b.dataset.idx = o.i;
        b.innerHTML = '<span class="ic"></span> ' + o.uc.tab;
        b.addEventListener("click", function () { select(o.i); });
        list.appendChild(b);
      });
      group.appendChild(list);
      rail.appendChild(group);
      catHeads[c] = head; catLists[c] = list;
    });
  }

  function renderConsole(uc, idx) {
    var narr = NARR[idx] || { g: "", k: "" };
    if (mode === "grep") {
      var g = uc.grep, h = '<div class="console-bar"><span class="lamp" style="background:var(--warn)"></span> agent · shell — the grep way</div><div class="console-body grep">';
      if (narr.g) h += '<div class="agent-voice">' + narr.g + '</div>';
      g.lines.forEach(function (l) {
        h += l.cmd ? '<div class="ln"><span class="g">$</span><span class="cmd">' + l.cmd + '</span></div>'
                   : '<div class="ln"><span class="g">↳</span><span class="out">' + l.out + '</span></div>';
      });
      h += '<div class="verdict bad"><span class="mk">UNRESOLVED</span> ' + g.verdict + '</div></div>';
      consoleEl.innerHTML = h;
    } else {
      var gf = uc.grafel, t = '<div class="console-bar"><span class="lamp" style="background:var(--accent-2)"></span> agent · MCP — the grafel way</div><div class="console-body grafel">';
      t += '<div class="ln"><span class="g">▸</span><span class="cmd">' + gf.call + '</span></div><div class="trace">';
      gf.trace.forEach(function (n, i) {
        t += '<div class="node' + (n.terminal ? ' terminal' : '') + '"><span class="kind">' + n.kind + '</span><span class="name">' + n.name + '</span></div>';
        if (i < gf.trace.length - 1) t += '<div class="link-arrow"></div>';
      });
      t += '</div>';
      if (narr.k) t += '<div class="agent-voice">' + narr.k + '</div>';
      t += '<div class="verdict good"><span class="mk">RESOLVED</span> ' + gf.verdict + '</div></div>';
      consoleEl.innerHTML = t;
    }
  }

  function renderQuality(uc) {
    qualityEl.innerHTML =
      '<h4>Answer quality</h4>' +
      '<div class="qrow"><div class="qrow-head"><span class="qwho">grep way</span><span class="chip-verdict warn">PARTIAL · GUESSED</span></div><p class="qtext">' + uc.grep.miss + '</p></div>' +
      '<div class="qrow"><div class="qrow-head"><span class="qwho">grafel way</span><span class="chip-verdict accent">EXACT · COMPLETE</span></div><p class="qtext">' + uc.grafel.proof + '</p></div>' +
      '<p class="quality-note">Fewer tokens, too — one call instead of dozens. A nice side effect, not the goal.</p>';
  }

  function render() {
    var uc = USE_CASES[active];
    qCat.textContent = uc.cat;
    qText.innerHTML = uc.question;
    explorer.setAttribute("data-mode", mode);
    btnGrep.setAttribute("aria-pressed", mode === "grep");
    btnGrafel.setAttribute("aria-pressed", mode === "grafel");
    moveGlider();
    renderConsole(uc, active);
    renderQuality(uc);
  }
  function moveGlider() {
    var target = mode === "grep" ? btnGrep : btnGrafel;
    glider.style.width = target.offsetWidth + "px";
    glider.style.transform = "translateX(" + (target.offsetLeft - 4) + "px)";
  }
  function select(i) {
    active = i;
    Array.prototype.forEach.call(rail.querySelectorAll(".rail-item"), function (t) {
      t.setAttribute("aria-selected", (+t.dataset.idx === i) ? "true" : "false");
    });
    if (catOfIndex[i]) openCategory(catOfIndex[i]);
    render();
  }
  function setMode(m) { mode = m; render(); }

  btnGrep.addEventListener("click", function () { setMode("grep"); });
  btnGrafel.addEventListener("click", function () { setMode("grafel"); });
  window.addEventListener("resize", moveGlider);

  buildRail();
  select(0);
  setTimeout(moveGlider, 60);
})();
