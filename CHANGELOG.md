# Changelog

All notable changes are documented here. Entries follow
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions.
PR numbers link to https://github.com/cajasmota/grafel/pull/<N>.

---

## [Unreleased]

### Added
- **Zod `.refine()` / `.superRefine()` / `.transform()` / `.pipe()` →
  entities (#5497):** the Zod schema extractor now captures the schema-level
  custom-validator chain that follows a schema declaration, which the
  field-constraint folder did not model. `.refine(fn, msg?)` and
  `.superRefine(fn)` each emit a `SCOPE.Schema` child (subtype `zod_refinement`,
  `refinement_kind=refine|superRefine`) carrying the literal/object `message`
  attribute when one is statically present; `.transform(fn)` emits a
  `SCOPE.Schema` child (subtype `zod_transform`). Both are linked to the host
  schema via a `CONTAINS` (`member=refinement|transform`) edge with an
  order-preserving `chain_index`, so multiple chained refinements/transforms
  stay distinct and ordered. `.pipe(Other)` records a `pipe_target` attribute on
  the host schema plus a `REFERENCES` (`ref_kind=zod_pipe`) edge to the named
  target schema (an inline `z.<factory>()` pipe target records the attribute but
  no named edge). Works on object, scalar (`z.string().refine(...)`), and nested
  (#5496) schemas. Honest-partial: the inline arrow/function body is not
  deep-analyzed — only the node and its literal message are captured; a schema
  with no custom-validator chain is unchanged. Part of the framework-stack
  coverage epic (#5479).
- **Zod nested `z.object()` → nested schema tree (#5496):** the Zod schema
  extractor now recurses into nested `z.object({...})` sub-schemas instead of
  recording only the top-level scalar fields. A nested object reached directly
  (`address: z.object({...})`), inside a wrapper (`z.array(z.object({...}))`,
  `z.record(z.object({...}))`), through a chain (`z.object({...}).optional()` /
  `.nullable()` / `.default()` / `.describe()`), or as a `z.union([...])` branch
  is emitted as a child `SCOPE.Schema` (subtype `nested_schema`) named with the
  dotted field path (`Profile.address`, `Profile.address.city`), carrying its own
  scalar fields and field members, and linked to its parent via a
  `CONTAINS` (`member=nested_schema`) edge. Recursion is capped at depth 8 to
  bound pathological nesting. Honest-partial: a field with no statically
  locatable `z.object()` (dynamic/computed, or a `$ref` to another named schema)
  yields no nested child — no false nesting. Flat schemas and chain-constraint
  chips are unchanged. Part of the framework-stack coverage epic (#5479).
- **TanStack query/mutation → HTTP endpoint `USES`/`CALLS` edge (#5494):**
  Each `tanstack_query` / `tanstack_mutation` operation (#5492) is now linked to
  the backend endpoint its `queryFn`/`mutationFn` fetcher calls, making the
  frontend-query → backend-endpoint cross-stack flow queryable. An **inline**
  static fetcher (`() => fetch('/api/users')`, `() => axios.get('/api/users')`,
  `() => fetch(url, { method: 'POST' })`) emits a `USES` edge to the endpoint
  stub `http:<VERB>:<path>` — the *same* synthetic `http_endpoint` Name the
  consumer-side HTTP synthesiser emits, so the existing cross-repo HTTP linker
  binds the query straight to the server route. A **named ref**
  (`queryFn: getUsers`, `mutationFn: createUser`) emits a `CALLS` edge to the
  data function; the existing call-graph + http-client edges then carry the
  transitive query → … → endpoint path (one hop is enough here). The `queryKey`
  label is never treated as an endpoint (no regression of #3171 — only the
  fetcher body is resolved). Both the object-arg form and the older positional
  form are handled. Part of the framework-stack coverage epic (#5479).
- **TanStack/React Query hooks → query/mutation entities (#5492):**
  TanStack Query (React adapter) was previously presence-detected only — the
  `useQuery` family surfaced as generic `USES_HOOK` edges. Each call site is now
  extracted as a decorated `SCOPE.Operation`: `useQuery` / `useSuspenseQuery` /
  `useInfiniteQuery` / `useQueries` → subtype `tanstack_query`, and
  `useMutation` → subtype `tanstack_mutation` (`via=tanstack_query`,
  `framework=react`), with a `CONTAINS` edge from the enclosing component /
  custom hook. Attributes captured: `query_key` (the literal `queryKey` array
  stringified, e.g. `['user', id]` → `user,id`), `query_fn` / `mutation_fn`
  (the `queryFn`/`mutationFn` callee ref name when resolvable), `query_kind`,
  and `query_call`. Both the object-arg form (`useQuery({ queryKey, queryFn })`)
  and the older positional form (`useQuery(key, fn)`) are handled. The pass is
  import-gated on `@tanstack/react-query` (or legacy `react-query`) so a
  non-TanStack function named `useQuery` is not mis-tagged; `useQueryClient`
  still surfaces as a plain hook edge (it returns the client handle, not an
  operation). The `queryKey`→endpoint `USES` edge is a follow-up (#5494). Mirrors
  the existing Angular (`injectQuery`) and Vue/Svelte (`createQuery`) adapter
  extraction. Part of the framework-stack coverage epic (#5479).
- **Kysely query-builder data-access → db_read/db_write effects with table attribution (#5491):**
  Kysely, the type-safe SQL query builder (previously 0 coverage), is now
  credited with receiver-gated, table-bearing db effects, mirroring the Prisma
  model-bearing model (#5490). The verb is the chain ROOT method on the
  `db`/`kysely`/`trx` instance: `db.selectFrom("user")…` is a `db_read`;
  `db.insertInto("post")`, `db.updateTable(...)`, `db.deleteFrom(...)`, and
  `db.replaceInto(...)` are `db_write`. Each chain (which terminates in
  `.execute()` / `.executeTakeFirst()` / `.executeTakeFirstOrThrow()` /
  `.stream()`) emits the effect attributed to its enclosing function with a
  table-bearing sink tag (`kysely.read:user`, `kysely.write:post`), so the
  data-access flow is queryable by table. Raw `` sql`…`.execute(db) `` is
  classified by the leading SQL keyword (`SELECT`/`WITH` → read;
  `INSERT`/`UPDATE`/`DELETE`/`REPLACE` → write; undeterminable → a generic
  `db_read`), and `(db|kysely|trx).executeQuery(…)` is a generic `db_read`. The
  distinctive Kysely chain-root methods plus the `db`/`kysely`/`trx` receiver
  gate (`trx` = the transaction-callback handle) keep an unrelated `.execute()`
  from being misread. Part of the framework-stack coverage epic (#5479).
- **Prisma model-layer data-access → db_read/db_write effects with model attribution (#5490):**
  the data-access layer — model functions in `*.server.ts` (and `*.server.tsx`)
  modules that wrap the Prisma client — is now credited with a receiver-gated,
  model-bearing db effect, so the read/write data-access flow is queryable by
  model and ties to the Prisma model entity (#5489). A `(prisma|db|tx).<model>.<verb>()`
  call emits a `db_read` or `db_write` effect attributed to its enclosing
  function, carrying a model-bearing sink tag (`prisma.read:User`,
  `prisma.write:Post`). Verb → effect: `findUnique/findFirst/findMany/count/
  aggregate/groupBy` are reads; `create/createMany/update/updateMany/upsert/
  delete/deleteMany` are writes; `$queryRaw` reads and `$executeRaw` writes (as
  before). The receiver gate requires an imported Prisma client / delegate
  (`prisma`, `db`, or a `tx` interactive-transaction callback handle), so an
  unrelated `.create()`/`.update()` on some other object is not misread as a
  Prisma data-access effect, and the inner delegate calls of a
  `prisma.$transaction(async (tx) => …)` are credited per-model. The effect-
  propagation pass already unions these effects up the CALLS graph, so a caller
  of a model function such as `getUsers()` transitively shows the `db_read`
  effect. Part of the framework-stack coverage epic (#5479).
- **Prisma modular split schema resolved cross-file (#5489):** the Prisma schema
  extractor now treats a modular split schema — `prisma/schema/*.prisma`, one
  domain per file, the `prismaSchemaFolder` preview feature — as ONE logical
  schema. When a `.prisma` file lives in a folder holding multiple `.prisma`
  files, the extractor unions the model and enum symbol tables across every file
  in that schema folder before resolving relation targets. As a result a `model
  Post` in `post.prisma` carrying `author User @relation(...)` now produces a
  resolved `Post → User` relationship edge even though `model User` lives in
  `user.prisma`, and a field whose type names an `enum` declared in a sibling
  file (e.g. `status Status` → `enums.prisma`) resolves to that enum instead of
  dangling. Cross-file relation fields are no longer dropped as honest-partial.
  Models keep their real source file and location. The single-`schema.prisma`
  case is unchanged (one file = the union of one). Part of the framework-stack
  coverage epic (#5479).
- **Next.js React Server Component data-fetch edges (#5488):** an async
  App-Router Server Component (a `page.tsx`/`layout.tsx` or other server
  component with no `'use client'` directive) now emits first-class edges to the
  data sources it awaits during render, so the server-side data flow of a route
  is queryable. An awaited call to a local/imported data function — `await
  getUser(id)`, `await db.user.findMany()`, a `*.server.ts` model fn — produces a
  `CALLS` edge from the component to the callee, and a direct `await fetch(url)`
  produces a `data_fetch` site plus a `READS_FROM` edge. Both are tagged
  `rsc_data_fetch=true` (with the resolved `url`/`callee` and `rendering=server`).
  The pass is gated to server components — App-Router page/layout files with no
  module-level `'use client'` — so client components and their event handlers,
  which use the same call syntax, are never mislabelled as server data-fetches.
  Completes the Next.js routing-coverage epic (#5479).
- **Next.js Server Actions extracted as operations, including the `action()`
  wrapper pattern (#5487):** Server Actions are now recognised in three forms,
  each emitted as a `SCOPE.Operation`/`server_action` operation bound to its
  module. (1) A file-level `'use server'` directive marks every exported async
  function in the module as an action. (2) A function-level inline `'use server'`
  (`async function f(){ 'use server'; … }` or arrow const) is an action
  regardless of any module-level directive. (3) The common `action()`-wrapper
  idiom — `export const doThing = action(schema, async (input) => {…})`, where
  the callee is a known action-wrapper factory (`action`/`actionClient`,
  `createServerAction`, and custom `authAction`/`safeAction`/`adminAction`/
  `publicAction`/`protectedAction`/`createSafeActionClient`) — captures the
  wrapped handler named after the exported const, recording an optional leading
  validation-schema argument as the `validation_schema` attribute and the wrapper
  name as `action_wrapper`. The wrapper is gated to the known callee-name set so
  an ordinary `const x = foo()` is not misread as an action. Part of the Next.js
  routing-coverage epic (#5479).
- **Next.js App Router `route.ts` HTTP-method handlers extracted as endpoints
  (#5486):** each exported HTTP-method handler in an App Router Route Handler
  file (`app/.../route.{ts,js,tsx}`) is now synthesized as a per-method
  `http_endpoint_definition` keyed `http:<METHOD>:<path>`. Both export forms are
  recognised — the function declaration (`export async function GET(req) {…}`)
  and the const arrow / function-expression (`export const GET = (req) => {…}`)
  — for the full verb set (`GET`/`POST`/`PUT`/`PATCH`/`DELETE`/`HEAD`/
  `OPTIONS`). The endpoint path is derived from the file's `app/`-relative
  directory using the App Router convention: route groups `(group)` are stripped
  (invisible to routing), dynamic segments `[id]`/`[...slug]` are normalised to
  the canonical `{id}`/`{slug}` form, and Route Handlers are recognised anywhere
  under `app/`, not only under `api/` (e.g. `app/feed/route.ts` → `GET /feed`,
  `app/api/users/[id]/route.ts` → `GET /api/users/{id}`,
  `app/(admin)/api/x/route.ts` → `GET /api/x`). Detection gates on the `route.*`
  basename, so a `page.tsx` exporting a `GET` function is not mistaken for a
  Route Handler. Reuses the existing `http_endpoint_definition` model; the
  verb-named handler binds to the `SCOPE.Operation` the JS/TS extractor emits.
  Part of the Next.js routing-coverage epic (#5479).
- **Topology screen renders Inngest event→function workflows (#5485):** the
  dashboard topology surface now reflects the Inngest async-workflow graph.
  Inngest event topics (`framework=inngest`) are grouped under a dedicated
  `inngest` broker band (previously they fell into the generic `unknown`
  bucket), so the event→function→event chain — a function emitting an event that
  triggers another function, wired by the `PUBLISHES_TO`/`SUBSCRIBES_TO` edges
  from #5482/#5483 — reads as one connected workflow. The topology detail panel
  badges producers/consumers as "Inngest function" / "Inngest event" and, for
  each Inngest function, lists its durable step structure inline (#5484's
  `inngest_step` children: `run`/`sleep`/`sleepUntil`/`waitForEvent`/`invoke`,
  with the awaited event for `waitForEvent` and the invoked target for
  `invoke`). Extends the existing pub-sub/queue topology rendering — no new
  extraction. Completes the Inngest epic (#5479) headline.
- **Inngest durable functions extracted as Function entities (#5480):** the new
  `custom_js_inngest` JS/TS extractor recognises `inngest.createFunction({ id |
  name }, { event | cron }, handler)` call sites (both the modern object-config
  signature and the older positional trigger form) and emits one
  `SCOPE.Function` entity (subtype `inngest_function`) per definition, named
  after the config `id`/`name` — the consumer side of an Inngest event, modelled
  like a BullMQ Worker / serverless function. The trigger is captured as an
  attribute (`trigger_event` + `trigger_type=event`, or `trigger_cron` +
  `trigger_type=cron`); `function_id`, `receiver`, and `framework=inngest` are
  recorded too. Detection is attribution-gated on an `inngest` import or a
  receiver named `inngest`. This is Phase 1 (entities) of the Inngest coverage
  epic (#5479); the EMITS/TRIGGERS edges that wire the trigger event to
  producers and topics are follow-up tickets (#5482/#5483/#5484). New registry
  record `msg.inngest` (`message_broker` / `task_queues`).
- **Inngest event names extracted as MessageTopic entities (#5481):** the
  `custom_js_inngest` extractor now also emits one `SCOPE.MessageTopic` entity
  (subtype `inngest`, `framework=inngest`, `topic_id=event:<name>`) per
  **distinct** Inngest event name — the event analogue of a BullMQ/Kafka topic.
  Event names are harvested from `createFunction({ event: "..." })` triggers,
  `<client>.send`/`sendEvent({ name: "..." })` producer payloads, and typed
  `new EventSchemas().fromRecord<{ "name": ... }>()` / `fromUnion` schema
  definitions (the quoted keys of the balanced type-argument record). Topics are
  deduped by event name within a file and carry the first reference site as their
  source location, attribution-gated on the same `inngest` import / receiver as
  the function extractor. Phase 1 (entities) of the Inngest epic (#5479); the
  EMITS/TRIGGERS edges wiring topics to their producers/consumers are #5482/#5483.
  Records the `topic_attribution` capability on `msg.inngest`.
- **Inngest `inngest.send` → EMITS edge (emitter → event) (#5482):** the new
  `applyInngestEdges` engine pass wires the producer side of Inngest events. For
  each `<client>.send({ name: "..." })` / `sendEvent(...)` call (and in-handler
  `step.sendEvent(...)`), it emits a `PUBLISHES_TO` edge from the enclosing
  scope to the `SCOPE.MessageTopic` event entity #5481 created, resolved via the
  `SCOPE.MessageTopic:<name>` Kind:Name ToID stub. It **reuses the existing
  `PUBLISHES_TO` edge kind** (the same kind the Kafka/BullMQ/RabbitMQ producer
  passes emit), so the cross-repo topic linker and the dashboard topology/flows
  panels understand it with no new code. An array of payloads yields one edge
  per distinct event name; the enclosing function/handler/route is resolved via
  `findEnclosingNodeName`, which falls back to the synthetic `Function:module`
  scope when no enclosing function can be resolved (e.g. a module-top-level
  send) rather than dropping the edge. Attribution-gated like the entity
  extractor (`inngest` import or a receiver named/ending in `inngest`, plus the
  `step` object). Append-only. Phase 2 (edges) of the Inngest epic (#5479);
  records the `producer_extraction` capability on `msg.inngest`.
- **Inngest event → function `SUBSCRIBES_TO` trigger edge (#5483):** the
  `applyInngestEdges` engine pass now also wires the consumer/trigger side. For
  each event-triggered `<client>.createFunction({ id }, { event: "..." }, …)`
  call, it emits a `SUBSCRIBES_TO` edge from the function (`Function:<id>`, the
  consumer entity #5480 created) to the `SCOPE.MessageTopic` event entity #5481
  created, resolved via the `SCOPE.MessageTopic:<name>` Kind:Name ToID stub. It
  **reuses the existing `SUBSCRIBES_TO` consumer edge kind and direction**
  (consumer function → topic — the same as the Kafka/BullMQ/Azure consumer
  passes), so the cross-repo topic linker and the dashboard topology/flows
  panels understand it with no new code. This is the symmetric partner of
  #5482's `PUBLISHES_TO`: a function that sends event X and another triggered by
  X form an event → function → event workflow chain. **Cron**-triggered
  functions (`{ cron: "..." }`, no event) are scheduled jobs and emit no
  subscriber edge. The function identity is the config `id` (preferred) or
  `name` (fallback); attribution-gated like the entity extractor (`inngest`
  import or a receiver named/ending in `inngest`); append-only. Phase 2 (edges)
  of the Inngest epic (#5479); flips the `consumer_extraction` capability on
  `msg.inngest` to full.
- **Inngest durable step structure inside a function (#5484):** the
  `custom_js_inngest` extractor now extracts the durable **step** structure
  inside each `createFunction` handler body. Every `step.run("id", …)`,
  `step.sleep`, `step.sleepUntil`, `step.waitForEvent`, and `step.invoke` call
  becomes one `SCOPE.Operation` child entity (subtype `inngest_step`,
  `framework=inngest`) named after the step-id literal, carrying a `step_kind`
  attribute (`run` / `sleep` / `sleepUntil` / `waitForEvent` / `invoke`),
  `step_id`, `inngest_function`, and the source location. Each step is
  **`CONTAINS`-linked** from its enclosing Inngest function (`Function:<id>` →
  step), so the durable workflow's internal structure is navigable on the graph.
  `step.waitForEvent` additionally records the awaited event name as a
  `wait_event` attribute, and `step.invoke` records the invoked-function
  reference as an `invoke_target` attribute (the `SUBSCRIBES_TO` / invoke edges
  to those targets are deferred follow-ups). Steps are read from the bounded
  `createFunction` argument region — so they bind to the right enclosing function
  with no cross-function bleed — and gated by the existing `inngest` attribution
  plus a `step`-receiver gate, so a stray `.run(`/`.invoke(` on an unrelated
  receiver is not misattributed. Phase 2 (step structure) of the Inngest epic
  (#5479); records the framework-specific `step_structure` capability on
  `msg.inngest`.
- **Index-progress list sorts by status — active rows on top, done sinks (#5495):**
  on the dashboard index/group view the per-repo/module progress rows used to
  render in a static repo/module order, so with a large (e.g. 30-module) group
  you couldn't see what was actively working — completed and not-yet-started rows
  were interleaved with the live ones. The list is now sorted by a status rank:
  actively-indexing rows stay pinned at the **top**, queued (registered but
  not-yet-started) rows sit just below them, and completed/failed rows sink to
  the **bottom** as they finish. The sort is stable within each status band — the
  original repo-then-module order is preserved — so rows don't jump around as
  unrelated repos advance, and a single-repo / all-same-status list renders
  exactly as before.

### Fixed
- **Install now ships the bundled skills, not just the MCP (#5503):** on a
  released-tarball install (the common path on macOS) the `grafel` binary lands
  on its own with no `skills/` directory beside it, so `grafel install` found no
  on-disk skills source, silently skipped the skills step, and registered only
  the MCP server — leaving users with the grafel MCP tools but none of the
  `/grafel-*` slash-command skills. The skills are now **embedded in the binary**
  (single source of truth stays the repo-root `skills/` tree) and skill discovery
  falls back to materialising them into a stable `~/.grafel/skills-cache` when no
  on-disk source exists, so `install` copies the full skill set into every
  detected Claude config's `skills/` dir on macOS, Linux, and Windows alike. A
  dev/repo checkout still uses the live on-disk tree; the embedded copy is only
  the last-resort fallback. The copy is idempotent (re-install overwrites
  cleanly) and `grafel uninstall` removes the installed skills, mirroring the MCP
  deregistration. All home-derived paths honour `$HOME` consistently with the MCP
  registration.
- **Index-concurrency cap — no more all-at-once monorepo storms (#5493):** when a
  group/monorepo with many modules was registered or rebuilt, the daemon indexed
  every module up to the memory-auto-tuned rebuild cap (8 on a 16 GB host, 16 on
  32 GB) simultaneously. The per-index core cap (`GRAFEL_EXTRACT_GOMAXPROCS`)
  bounds cores *within* one index but not the *number* of concurrent indexes, so
  a 30-module group spun up ~8–16 indexers at once and pinned the machine. A new
  daemon-wide **index-concurrency gate** now bounds concurrent module/repo index
  operations to `GRAFEL_INDEX_CONCURRENCY` (**default 2**); excess indexes queue
  FIFO and run as slots free, so a 30-module group drains 2-at-a-time. One slot is
  reserved for foreground/interactive index so a background storm can't starve it
  (#5328). `grafel_index_status` now reports the gate's `concurrency` block
  (`indexing` / `queued` / `cap`) so a draining group reads "indexing 2, queued 28"
  instead of looking stalled.

---

## [0.1.4] — 2026-06-24

**Tree-sitter cutover release.** All 26 grammars are migrated off the
unmaintained `smacker/go-tree-sitter` binding onto the official
`tree-sitter/go-tree-sitter` v0.24.0 runtime (ABI 13–14); smacker is fully
removed and the build-tag split is gone — official is the only build. The
unused markdown grammar was dropped (markdown stays a supported language via its
pure-stdlib extractor). Swift `actor`/`distributed actor` are now extracted as
first-class concurrency components, and the `find_callers` MCP tool got fuzzy
input resolution plus a cross-repo caller-recovery fix. Also ships the 0.1.4
grammar-freshness infrastructure (Renovate, monthly freshness/calendar crons,
per-language parse-error canary), `grafel_index_status`, dead-ref retention-cap
GC, dashboard cold-stats, progressive graph rendering, and the deferred-from-0.1.3
group-algo CPU/overlay fixes.

### Added
- **Swift `actor` declarations as first-class concurrency components (#5417,
  C3(b)):** `actor` and `distributed actor` types are now extracted as
  `SCOPE.Component` with `subtype=actor`, mirroring how `class`/`struct`/`enum`
  are modeled (one shared `class_declaration` grammar rule, classified by the
  leading keyword). An actor's members extract identically to a class — each
  method gets a `CONTAINS` edge and each stored property a `SCOPE.Schema`/field
  — so concurrency components stop collapsing into the generic `class` bucket.
  The other four C3(b) candidates from the impact analysis were verified against
  the pinned ABI-14 grammars and **deferred as grammar-too-old**: C# extension
  members (`tree-sitter-c-sharp` v0.23.1 has no `extension` node), Kotlin context
  parameters, Python t-strings (PEP 750), and JS/TS `await using` — all
  post-date their pinned grammar tags and currently parse to `ERROR`/misshaped
  nodes, so no extractor code was added for them.
  ([`internal/extractors/swift/swift.go`](internal/extractors/swift/swift.go))
- **Official tree-sitter grammar providers — batch B2, batch 4a (vendored C)
  (#5418, B2 cutover Part B):** directly-vendorable grammar packages for
  **proto, dockerfile, kotlin** under `internal/treesitter/ts/grammars/`, behind
  the `ts_official` build tag. Unlike the go-get providers, these vendor the
  grammar's committed `src/parser.c` (+ `scanner.c` and `tree_sitter/` headers
  where present) directly into the package, with a hand-written official-style
  cgo binding that calls the exported `tree_sitter_<name>()` symbol and wraps it
  via `official.WrapLanguage` — compiled against the **official** runtime, no new
  Go module. This is the only path for these three: proto
  (`mitchellh/tree-sitter-proto`, ABI 13, MIT) has **no Go binding anywhere**;
  dockerfile (`camdencheek/tree-sitter-dockerfile` v0.2.0, ABI 14, MIT, external
  scanner) and kotlin (`fwcd/tree-sitter-kotlin` 0.3.8, ABI 14, MIT, external
  scanner) commit an ABI-≤14 `parser.c` that their module go.mod / module
  boundary makes unreachable to a `go get` of the binding (cutover plan §3/§4).
  Each package carries a SPDX/source/ref attribution note for the license-audit
  gate, a smoke-test ABI guard (top kind `source_file`), and is wired into
  `adapters_official.go`'s `migratedLanguages` + `abiProbeSource`. (sql, hcl, and
  groovy stay deferred to batch 4b — they need ABI-14 regeneration first.)
- **Official tree-sitter grammar providers — batch B2, batch 4b (regenerated +
  vendored C) (#5418, B2 cutover Part B):** vendored grammar packages for
  **sql, hcl, groovy** under `internal/treesitter/ts/grammars/`, behind the
  `ts_official` build tag. Unlike batch 4a, none of these has a usable committed
  ABI-≤14 `parser.c`: hcl (`tree-sitter-grammars/tree-sitter-hcl` v1.1.1) and
  groovy (`murtaza64/tree-sitter-groovy` HEAD) commit an **ABI-15** `parser.c`
  (which SIGSEGVs against the v0.24.0 runtime), and sql
  (`DerekStride/tree-sitter-sql` v0.3.9) `.gitignore`s its generated `parser.c`
  entirely. For each, the `parser.c` was **regenerated from the grammar's
  `grammar.js`** with **tree-sitter-cli v0.23.2** (the v0.23 line emits
  `LANGUAGE_VERSION 14`; a v0.24+ CLI would emit ABI 15), then vendored alongside
  any external `scanner.c` and the `tree_sitter/` headers, with a hand-written
  official-style cgo binding that calls the exported `tree_sitter_<name>()`
  symbol and wraps it via `official.WrapLanguage` — compiled against the
  **official** runtime, no new Go module. sql (MIT, external scanner, top kind
  `program`) and hcl (Apache-2.0, external scanner, top kind `config_file`) carry
  a vendored `scanner.c`; groovy (MIT, no external scanner, top kind
  `source_file`) does not. Each package carries a SPDX/source/ref +
  `regenerated-with tree-sitter-cli v0.23.2` attribution note for the
  license-audit gate, a smoke-test ABI guard, and is wired into
  `adapters_official.go`'s `migratedLanguages` + `abiProbeSource`.
- **Progressive graph rendering in the dashboard (#5446, increment 2):** the
  Graph screen now consumes the streaming endpoint (`GET /api/v2/graph/{group}/stream`)
  and feeds the GPU canvas (cosmos.gl) incrementally, so a large graph **builds
  up live** with a "building graph… N / total nodes" counter and a subtle
  progress bar instead of a long blank wait. The SSE consumer accumulates the
  `meta` → `chunk…` → `done` events into the same normalized payload the
  full-payload fetch produces (a pure, unit-tested reducer), so it is a drop-in
  data source. A cold group (503) shows a "warming index…" state and retries
  with a bounded, capped backoff; a mid-stream drop falls back to the
  full-payload fetch (identical shape) so the graph still loads. On `done` the
  canvas runs one canonical re-layout so the complete graph settles cleanly and
  fits the viewport. A tiny graph still streams in a single round-trip, so the
  small-graph case stays effectively instant. Frontend only this increment.
- **Streaming graph endpoint for progressive load (#5446, increment 1):** a new
  `GET /api/v2/graph/{group}/stream` SSE endpoint streams the same node/edge
  shape as `/api/v2/graph/{group}` but progressively — a `meta` event first
  (`total_nodes`/`total_edges` for a progress counter), then `chunk` events of
  ~750 nodes (plus the edges that became deliverable) ordered **important-first**
  (centrality/PageRank descending from the group-algo overlay, falling back to
  connectivity/degree when no overlay is present), then a final `{"done":true}`.
  This lets the renderer (GPU canvas) start painting the most central part of a
  large graph immediately instead of waiting for the whole payload to serialise.
  Streams from the warm cache only (never force-loads); a cold group returns the
  503 not-loaded signal. The existing full-payload endpoint is unchanged, so the
  frontend can switch with no data-model change. Backend only this increment.
- **Env-tunable dead-ref retention cap (`GRAFEL_REF_RETENTION_CAP`, #5447):** the
  ceiling on grace-protected dead-in-git ref graphs the daemon keeps per repo
  (default 8) can now be overridden via the environment, letting an operator
  shrink the dead-ref footprint on a machine with heavy transient-ref churn
  (e.g. set it to 4). Unset → default 8; a non-negative int sets the cap
  (0 keeps no grace-protected dead refs); a negative int disables the cap
  backstop; unparseable values fall back to the default. Mirrors the existing
  `GRAFEL_TIER_*`/`GRAFEL_EXTRACT_GOMAXPROCS` env pattern.
- **Official tree-sitter grammar providers — batch B2, batch 3 (source-swaps)
  (#5418, B2 cutover Part B):** ABI-14-pinned official-runtime grammar packages
  for **lua, toml, yaml** under `internal/treesitter/ts/grammars/`, mirroring the
  Phase-0 Go provider and wired into `migratedLanguages` behind the `ts_official`
  build tag (each with an ABI smoke-parse guard test asserting a non-nil,
  non-error root of the expected top kind: lua `chunk`, toml `document`, yaml
  `stream`). Unlike the version-only batches, these are **recorded source-swaps**:
  each moves off its dead, binding-less bundled repo (lua `Azganoth/…`, toml/yaml
  `ikatyang/…`, all without a Go binding) onto the maintained
  **`tree-sitter-grammars`** org, whose `bindings/go` depends on the official
  runtime — a freshness win and the one-runtime requirement (cutover plan §5).
  `grammars.lock`'s `source` is updated for all four source-swap languages.
  Pins: tree-sitter-lua `v0.3.0` (v0.4.0+ ABI 15), tree-sitter-toml `v0.7.0`
  (latest), tree-sitter-yaml `v0.7.2` (latest; still ABI 14, parser.c-verified) —
  all `LANGUAGE_VERSION 14`, inside the v0.24.0 runtime's 13–14 window.
  **hcl is intentionally deferred to the vendored-C track:** the
  `tree-sitter-grammars/tree-sitter-hcl` `bindings/go` only exists from `v1.2.0`
  (ABI 15, SIGSEGVs at `RootNode`), and the ABI-14 tags (`v1.1.0`/`v1.1.1`) ship
  **no Go binding** — so hcl cannot use this go-get-a-binding pattern (cutover
  plan §3/§4). The **default build is unchanged** (100% smacker); the
  `ts_official` path is only populated so the eventual default-flip has these
  providers ready and validated. Additive prep — not the big-bang flip.
- **Official tree-sitter grammar providers — batch B2
  (#5418, B2 cutover Part B):** ABI-≤14-pinned official-runtime grammar packages
  for **elixir, ocaml, php, scala, swift** under
  `internal/treesitter/ts/grammars/`, mirroring the Phase-0 Go provider and wired
  into `migratedLanguages` behind the `ts_official` build tag (each with an ABI
  smoke-parse guard test asserting a non-nil, non-error root of the expected top
  kind: elixir `source`, ocaml `compilation_unit`, php `program`, scala
  `compilation_unit`, swift `source_file`). Modules/versions:
  tree-sitter-elixir `v0.3.4` (via an `elixir-lang` → canonical-path replace),
  tree-sitter-ocaml `v0.23.2` (`LanguageOCaml`), tree-sitter-php `v0.23.11`
  (`LanguagePHP`), tree-sitter-scala `v0.23.4`, and alex-pinkus/tree-sitter-swift
  at the `0.7.3-with-generated-files` tag (parser.c is checked in only on the
  `-with-generated-files` tags) — all `LANGUAGE_VERSION ≤14`, inside the v0.24.0
  runtime's 13–14 window. **kotlin and sql are intentionally deferred:** their go
  bindings `#include` a generated `src/parser.c` that is unreachable from a module
  download (kotlin's lives outside the nested `bindings/go` module boundary;
  `DerekStride/tree-sitter-sql` `.gitignore`s `src/parser.c`, so it is never
  committed), so both need the vendored-C track (cutover plan §3/§4), not this
  official-binding pattern. The **default build is unchanged** (100% smacker); the
  `ts_official` path is only populated so the eventual default-flip has these
  providers ready and validated. Additive prep — not the big-bang flip.
- **Official tree-sitter grammar providers — batch B
  (#5418, B2 cutover Part B):** ABI-14-pinned official-runtime grammar packages
  for **bash, c, cpp, css, html, ruby** under `internal/treesitter/ts/grammars/`,
  mirroring the Phase-0 Go provider and wired into `migratedLanguages` behind the
  `ts_official` build tag (each with an ABI smoke-parse guard test asserting a
  non-nil, non-error root of the expected top kind). Modules/versions:
  tree-sitter-bash `v0.23.3`, tree-sitter-c `v0.23.6`, tree-sitter-cpp `v0.23.4`,
  tree-sitter-css `v0.23.2`, tree-sitter-html `v0.23.2`, tree-sitter-ruby
  `v0.23.1` — all `LANGUAGE_VERSION 14`, inside the v0.24.0 runtime's 13–14
  window (cpp/html/ruby are already upstream-latest; bash/c/css pin back from an
  ABI-15 latest). The **default build is unchanged** (100% smacker); the
  `ts_official` path is only populated so the eventual default-flip has these
  providers ready and validated. Additive prep — not the big-bang flip.
- **Official tree-sitter grammar providers for the high-value languages
  (#5418, B2 cutover Part A):** ABI-14-pinned official-runtime grammar packages
  for **python, java, csharp, typescript (+tsx), javascript, rust** under
  `internal/treesitter/ts/grammars/`, mirroring the Phase-0 Go provider and
  wired into `migratedLanguages` behind the `ts_official` build tag (each with an
  ABI smoke-parse guard test). Modules/versions:
  tree-sitter-python `v0.23.6`, tree-sitter-java `v0.23.5`,
  tree-sitter-c-sharp `v0.23.1`, tree-sitter-typescript `v0.23.2`
  (ships both typescript + tsx), tree-sitter-javascript `v0.23.1`,
  tree-sitter-rust `v0.23.2` — all `LANGUAGE_VERSION 14`, inside the v0.24.0
  runtime's 13–14 window. The **default build is unchanged** (100% smacker); the
  `ts_official` path is only populated so it can be validated before the eventual
  default-flip. Python's inline grammar provider is split into the build-tagged
  `language_smacker.go` / `language_official.go` pair like the Go extractor.
- **B2 tree-sitter cutover plan (#5418):** the one-runtime big-bang spec for
  swapping the default build off `smacker/go-tree-sitter` onto the official
  `tree-sitter/go-tree-sitter` v0.24.0 + per-language grammar modules. Pins the
  runtime (ABI range **13–14**, verified from its `api.h`) and gives the full
  **27-language version matrix** — each grammar's official-style Go module path +
  the freshest ABI-≤14 tag (7 already-latest, 14 pinned back because their latest
  is ABI 15 / the v0.25 generation that SIGSEGVs, 4 source-swaps to the
  `tree-sitter-grammars` org, 3 vendored). Resolves **proto** (vendor C + a thin
  official-runtime binding; grammar is ABI 13, no regen), the **dockerfile/groovy**
  smacker-`require` caveats (vendor the already-official-style binding; groovy
  regen to ABI 14), the **lua/toml/yaml/hcl** source-swaps, and **markdown**
  (drop the unused grammar). Defines the cutover **validation gate** (ABI
  smoke-parse every grammar, B1 fidelity re-bench all languages, #481 determinism
  re-test, 3-OS CGO release matrix, license audit of ~26 modules) and the C3
  (b)/(c) per-language coupling that blocks tagging. Includes a bounded co-link
  proof that dropping smacker dissolves the 247-duplicate-symbol blocker and
  multiple ABI-14 grammars co-build+parse on the official runtime.
  (`docs/treesitter-cutover-plan.md`)
- **C3 new-feature impact analysis (#5417):** per-language triage of language
  features released during the ~22-month grammar catch-up window (2024-08 →
  2026-06), classified (a) parse-only / (b) needs-new-extraction / (c)
  changes-existing-extraction against grafel's actual extractors. Identifies the
  (b) backfill worklist — C# 14 extension members, Swift actors, Kotlin context
  parameters, Python t-strings, JS/TS `await using` — and the (c) adaptations,
  all gated on the B1 grammar-bump / B2 smacker-decouple cutover.
  (`docs/c3-feature-impact-analysis.md`)

### Fixed
- **Test-isolation: `grafel install` fresh-install test escaped the #5443
  sandbox guard on Linux/Windows CI (#5418):**
  `TestIssue2683_FreshInstall_WritesAllSixFiles` (and its sibling rules-file
  tests in `cmd/grafel`) redirected `HOME`/`GRAFEL_HOME` into a `t.TempDir()`
  but not `XDG_CONFIG_HOME`. `registry.ConfigDir()` honors `XDG_CONFIG_HOME`
  first, which on the Linux/Windows GitHub runners points at the real
  `~/.config`, so `install.Apply` → `SaveGroupConfig` resolved the fleet config
  under the real home and tripped the write-path guard — a panic that surfaced
  on Linux/Windows but not on macOS (where `XDG_CONFIG_HOME` is unset and the
  path fell back to the redirected `HOME`). Switched the shared `applyWithStubs`
  helper to `testsupport.IsolateHome(t)`, which redirects all of
  `HOME`/`XDG_CONFIG_HOME`/`GRAFEL_HOME` into the sandbox. Audited every test
  calling a guarded writer (`SaveGroupConfig`/`AddGroup`/`install.Apply`/
  `ConfigPathFor`); this was the only un-isolated one.
  ([`cmd/grafel/issue2683_rules_files_test.go`](cmd/grafel/issue2683_rules_files_test.go))
- **B2 cutover, C3 (c) — extractors adapted to the official grammars' changed
  node shapes (#5418):** the big-bang flip (Step B) swapped all grammars to
  fresher official versions whose CST node shapes differ from the 2024-08
  smacker snapshot, breaking ~27 extractor tests across 8 packages. Fixed
  without weakening any test (the goldens stay the contract):
  - **Systemic node-identity bug (root cause of most failures).** Many
    extractors compared two `ts.Node` handles with `==` to ask "is this child
    the call callee / module name / declaration name?" The official binding
    returns a fresh wrapper struct per accessor call, so `==` is never true and
    those guards silently mis-fired — leaking phantom CALLS/REFERENCES edges,
    mis-attributing exception edges, and dropping relative-import names. Added
    `ts.SameNode(a, b)` (identity by type + byte span) and routed every such
    comparison through it across **python, golang, javascript, ruby, elixir**
    (and preventively **java, kotlin**, whose grammars didn't shift shape but
    carried the same latent `==`). Groovy's constructor-skip set, keyed by node,
    was rekeyed by byte span for the same reason.
  - **python:** relative-import dotted-name layout (`from .x import y`) and the
    `from .x import y as z` alias, plus a duplicate nested-constructor REFERENCES
    edge — all the `== modNode` / `isPyCallCallee` identity guards.
  - **lua:** rewired for the current tree-sitter-lua shapes — `function_call`
    `name` field with `dot_index_expression` / `method_index_expression` callees,
    `arguments` (was `function_arguments`), `require(...)` and `local x =
    require(...)` nested under `assignment_statement(variable_list/expression_list)`,
    and `table_constructor`.
  - **groovy:** the regenerated grammar has no enum rule and a flaky Gradle-DSL
    parse — adapted enum detection to the bare `identifier(enum)`/`identifier(Name)`
    + `closure` header and recovered valued constants from the contorted
    `juxt_function_call` / `parenthesized_expression` token stream via an ordered
    name↔value pairing; handled both `task name { }` (juxt) and `task name(...)`
    (function_call) sibling shapes.
  - **cpp:** pure-virtual `= 0` now parses as a bare `= number_literal(0)` inside
    a method `field_declaration` (no `pure_virtual_clause` node) — abstract-class
    detection accepts both.
  - **elixir / ruby:** exception-flow `def` body scoping and `ENV['KEY']`
    element-reference index were the `== body` / `== obj` identity guards above.
- **B2 cutover — full `go test ./...` suite greened after the flip (#5418):** the
  3-OS build and the per-package extractor passes were green, but the **whole**
  suite was not — the per-package runs missed cross-package failures. Fixed
  without weakening any test:
  - **Un-isolated tests caught by the #5443 registry sandbox guard.** Tests that
    only set `GRAFEL_HOME` (not the full home/XDG set) still resolved the fleet
    config path off the real config dir, so the guard correctly panicked to stop
    them clobbering the developer's live `~/.config/grafel`. Added
    `testsupport.IsolateHome(t)` to **8** tests across 3 packages:
    `TestGroupsForRepoPath` (cmd/grafel, via `writeTestGroup`),
    `TestDiffGroup_CrossRepoRankNonDecreasing` + `TestDiffGroup_Empty`,
    `TestAssembleGroupGraph_OnDisk`, `TestRunGroupAlgorithms_EmptyGroup`,
    `TestCurrentSourceMtimes_OnDisk`, the `registerTwoRepoGroup` helper (3
    recompute tests), and `TestApplyOverlay_GroupValuesAppliedAtLoad`
    (internal/mcp, via `setupApplyGroup`). The guard is unchanged.
  - **Dropped-markdown grammar counts.** `TestSupportedLanguages_ReturnsExpectedCount`
    (29 → 28) and `TestLoadLock_RealManifest` (28 → 27 grammars) updated to match
    the parser registry and `grammars.lock` after markdown was dropped in the flip.
  - **MCP tool-inventory tests** (pre-existing, surfaced by the full run): the
    `grafel_index_status` tool (#5435) was registered but absent from the test
    inventories — added it to `minimalArgs`, `wantPresent`, and `wantToolParams`
    and bumped the registered-tool counts 68 → 69
    (`TestElapsedMSCoverageAllTools`, `TestToolNameSurface`,
    `TestToolParamNamesPreserved`).

### Changed
- **B2 cutover, Step B — flipped to the official binding as the sole build;
  removed smacker entirely (#5418):** every language now resolves to its
  `internal/treesitter/ts/grammars/<lang>` provider on the official
  `tree-sitter/go-tree-sitter` runtime, with **no build tag** — the
  `ts_official` / `!ts_official` split is gone and official is the only build.
  The build-tagged adapter pair (`adapters_default.go` / `adapters_official.go`)
  was collapsed into a single un-tagged `adapters.go`, and the per-extractor
  `language_smacker.go` / smacker grammar-handle files were dropped. The legacy
  `github.com/smacker/go-tree-sitter` dependency is removed from `go.mod`/`go.sum`
  and its adapter package (`internal/treesitter/ts/smacker/`) deleted; the
  concrete smacker tree fields `ParseResult.Tree` and `FileInput.Tree` are
  retired — only the binding-agnostic façade `ParseResult.TSTree` / `TSTree`
  remains. The full tree now compiles and links all 26 grammars on the official
  runtime in one binary (the co-link blocker is gone once the second runtime is
  no longer linked). The **markdown** grammar was dropped from the parser
  registry and `grammars.lock` (its extractor is pure-stdlib; no functional
  impact). Per-language C3 (c) extractor adaptations for the newer grammars'
  changed node shapes, and the full B1 fidelity re-bench, remain gated follow-ups
  before tag (cutover plan §7).
- **B2 cutover, Step A — node-traversing extractors moved onto the
  binding-agnostic `ts.Node` façade (#5418):** the remaining extractor files
  that still typed their CST walks against the concrete smacker `*sitter.Node`
  now traverse `ts.Node` / `ts.Tree` instead, sourcing the tree from the parser
  factory's always-populated `ParseResult.TSTree`. Migrated: the Spring (Java),
  Spring (Kotlin) and Django route-composition passes, plus the custom Ktor
  nested-route extractor; two now-redundant `*sitter`-import compile shims were
  removed. Purely mechanical — same nodes extracted, no behaviour change (route
  golden tests unchanged). Runs under the **default** build, so the smacker
  removal and runtime flip remain a separate later step; the grammar-handle
  registration files (`*_smacker.go`, per-language `language.go`/`grammar.go`)
  and the smacker parser factory stay on the concrete binding by design.

### Fixed
- **`find_callers`: fuzzy input resolution + recover dropped cross-repo callers
  (#5475):** `find_callers` was the most-failed neighbor tool — a ~20-44%
  "entity not found" rate for its heaviest consumer, plus a silent caller
  under-report. Two bugs, one shared root cause (unresolved / cross-repo ids).
  (1) The shared `resolveEntityArg` path resolved `entity_id` only by exact hex
  id → exact unique Name/QualifiedName, then errored — while `find` resolved the
  same reference fuzzily (different case, a qualified-name suffix like
  `svc.Target`, a substring). It now falls through to the *same* case-insensitive
  substring match `find` uses (extracted into a shared `fuzzyMatchEntities`
  helper), preferring a unique exact-case-insensitive name and returning a
  disambiguation envelope when the probe matches many; the exact-id + exact-name
  happy path is byte-for-byte unchanged and a genuine miss still errors. (2) When
  an inbound caller edge's `FromID` was an unresolved id (the cross-repo linker
  never rewrote a raw path / placeholder to a stamped entity id), only file-ref
  and projected-semantic edges got the synthetic-caller fallback; the remaining
  real caller kinds (CALLS, TESTS, IMPLEMENTS, HANDLES, ROUTES_TO, inheritance,
  …) were silently dropped, so `find_callers` returned N-1 callers. The fallback
  now covers every id that reached the walk via the `isInboundNeighborKind`
  gate. The resolver fix is shared, so `find_callees` / `neighbors` benefit too
  (verified not regressed).
- **Graph view: streamed nodes now reach the canvas every chunk — no more
  blank-until-done (#5446):** the progressive Graph screen showed a climbing
  "building graph… N / total" counter while the WebGL canvas stayed blank, then
  the whole graph popped in at once on `done`. The canvas had a streaming-grow
  path (seed new nodes near a placed neighbour + re-heat the sim each chunk), but
  its trigger was gated on an internal post-settle "placed count" ref that is
  only populated **after** the first settle runs — and the first settle is
  deferred a frame (a cache-hit mount settles without ever setting it). When
  chunks arrived faster than that ref was set, every grown chunk fell through to
  the non-streaming branch, which — because the auto-start flag is already
  latched during a stream — uploaded the raw seed but never re-heated the
  simulation, so later nodes sat in the GPU buffers, unlaid-out and invisible,
  until the stream ended. The trigger now keys on the buffer **actually uploaded
  to the canvas** (`shouldStreamGrow`, unit-tested) rather than the placed-count
  ref, so every grown chunk takes the live-grow + re-heat path and the graph
  visibly grows from the first chunk; the already-merged sim energy (#5461) and
  camera tracker (#5459/#5463) then make the live explode actually visible.
- **Graph view: MCP-activity glow no longer leaves stale amber/blue edges + nodes
  (#5446):** the replay/activity glow restored the base point + link colours only
  at the animation's natural decay-end. When a pulse was **superseded** (rapid
  replay-all / the next epoch) or the user pressed **Reset**, the effect cleanup
  only cancelled the animation frame — the half-tinted colour/size buffers it last
  uploaded were left on the GPU, so glowed edges and nodes accumulated and
  persisted. The base point/link colours + sizes are now restored on glow
  cancel/supersede **and** on Reset, so no stale glow survives (the
  consecutive-replay dim-focus behaviour is preserved).
- **Graph view: Reset / re-explode no longer drifts the camera off-screen
  (#5462):** pressing **Reset** (or any path that triggers a fresh re-settle —
  group-by, layout change, deep-link re-explode) made the graph spread but the
  camera drifted off toward a corner instead of staying centered + framed,
  sometimes leaving the viewport entirely; the initial page load framed fine. The
  during-settle camera tracker self-terminated on "user interaction" via a
  **sticky** latch that flips true on the first canvas click/pan/zoom and never
  resets — so because the user has almost always clicked/selected a node before
  pressing Reset, the tracker bailed the instant the explode began and the spread
  ran with a frozen camera. A programmatic re-settle now ARMS an **auto-follow
  window** that keeps the camera tracking the live node bounding box through the
  *entire* explode regardless of that latch; only a **genuine** subsequent user
  pan/zoom/drag during the settle stops auto-following. Real vs. programmatic
  camera moves are told apart via cosmos.gl's `userDriven` flag (the tracker's own
  `fitView` glides report `userDriven === false`, so they don't self-cancel the
  window they serve), and the fit cadence was tightened (~120ms) so a fast
  `start(1)` explode never outruns the camera. Initial-load framing and the
  streaming live-explode are unchanged. The interaction-vs-programmatic decision is
  extracted to a pure, unit-tested helper.
- **Streaming graph now visibly spreads/explodes live instead of staying a
  clustered blob until done (#5446, #5460):** when the Graph screen loaded a
  large group progressively (chunked stream), the layout stayed a tight,
  near-invisible cluster for the whole stream and only "snapped" into a spread at
  the very end — the dramatic, energetic spread that the full-payload load/reset
  path produces was missing. The streaming data-push was re-heating the running
  force simulation with too gentle a pulse (a low alpha) and seeding new nodes in
  a tiny radius, so the accumulated structure barely moved between chunks. The
  streaming re-heat now injects energy **comparable to the fresh-settle (Reset)
  path** on every chunk, so the whole accumulated graph **visibly spreads as it
  grows**, and new trailing nodes are seeded with a visible offset around an
  already-placed neighbor (so they read as growth without flying in from the
  origin). The settle-time camera tracker (#5459) keeps the spreading graph
  framed throughout, so the load now reads as a live explode that grows with the
  stream; the on-`done` step is just a final settle/fit polish. The non-streaming
  load/reset explode is unchanged.
- **Graph MCP-activity replay now glows EVERY event, not just the last (#5457):**
  in the graph view's MCP-activity panel, "Replay all" and clicking an individual
  event entry are meant to make each step's nodes **glow** in sequence. Only the
  *last* step actually pulsed on screen. Both replay paths feed the same canvas
  glow, which **caps** the pulse to nodes currently in the viewport (a perf cap)
  and falls back to an off-screen sample when none are in view — and since the
  replay camera is **static**, each step's nodes are almost always off-screen, so
  the pulse fired **invisibly** in a far cluster. The canvas now **pans/fits the
  camera to a step's nodes** (eased, matching the comet/sweep overlay's per-step
  motion) whenever the step is fully off-screen, so every replayed event glows in
  view — replay-all *and* individual clicks. Gated so it never fires in a
  focus/ego view or during a camera restore, and never when a node is already in
  view (so a view the user is reading isn't yanked).
- **Graph force-layout stays centered during the settle instead of drifting to a
  corner then snapping (#5458):** when the graph "exploded"/settled (initial load,
  Reset, and streaming) it appeared to shrink toward the upper-right corner each
  second — sometimes leaving the viewport — then **snapped** to center on a single
  final `fitView`. The camera was static while the simulation spread the nodes.
  The canvas now **continuously tracks the camera to the spreading layout**: a
  throttled, eased fit (~every 350ms while the simulation is running) keeps the
  graph centered + framed the whole time, and the **final fit is eased/animated**
  (cosmos.gl `fitView(duration)`) so even an intentional re-fit glides rather than
  jumps. Auto-fit runs only during the initial settle / explode / stream
  cool-down — it does **not** fight the user's manual pan/zoom once they've
  interacted, and does not fit during focus/ego views.
- **Streamed graph now renders and grows LIVE from the first chunk (#5455,
  epic #5446):** the progressive Graph screen showed a climbing
  "building graph… N / total" counter while the WebGL canvas stayed **blank**,
  then the whole graph popped in at once on `done`. The force-directed canvas
  auto-settled the first chunk and then **paused** the simulation, so nodes
  added by later chunks were pushed into the GPU buffers but never laid out (the
  sim was paused) and sat invisible until the end. The canvas now has a
  **streaming mode**: each chunk keeps the simulation warm and **gently re-heats**
  it (`Graph.start(0.35)` — a low-alpha pulse, not a full reset), so the newly
  arrived nodes are laid out and rendered immediately and the graph **grows +
  jiggles live** from the first chunk. New nodes are **seeded next to an
  already-placed neighbor** (their primary edge endpoint, with a small jitter) —
  or near the centroid of the placed mass when they have no placed neighbor yet —
  so they don't all fly in from the origin. On `done` the canvas runs a short
  final cool-down + camera fit on the layout the live stream already produced (a
  polish step, not the first paint) instead of the previous destructive full
  re-layout that reshuffled the whole graph. Small graphs still stream in a
  single round-trip and look instant; the progress counter, warming state, and
  mid-stream fallback are unchanged. A partial streamed layout is never persisted
  to the layout cache. Frontend only.
- **Tests can no longer clobber a developer's real `~/.config/grafel` fleet
  config (#5443):** a group-overlay test resolved the fleet-config path
  (`registry.ConfigPathFor` → `registry.ConfigDir`) without redirecting
  `HOME`/`XDG_CONFIG_HOME`/`GRAFEL_HOME` into a TempDir. When run from a real
  home it fell back to the live `~/.config/grafel/<group>.fleet.json` and
  overwrote it, repointing the group's repos at an already-deleted `t.TempDir`
  so the group dropped to 0 entities. The offending test now isolates via
  `testsupport.IsolateHome(t)`, and — as a fail-closed guardrail — the registry
  config writers (`SaveGroupConfig`, the `registry.json` writer) now **panic
  loudly under `go test` if the write target lands inside the real user home**,
  so a future un-isolated test fails immediately instead of silently corrupting
  the developer's live config.
- **Dashboard/`grafel status` no longer show "0 entities / never indexed" for a
  cold-but-indexed group (#5442):** the WebUI group overview and `grafel status`
  derived per-repo entity counts + last-indexed time only from the
  `graph-stats.json` sidecar, which the daemon's incremental reindex path never
  wrote (it writes `graph.fb` only). A group maintained by the daemon — or any
  cold group whose sidecar was absent — therefore reported `0 / never` even
  though the MCP lazy-loaded `graph.fb` and served real results. On a sidecar
  miss both surfaces now read the persisted entity/relationship counts and index
  timestamp **cheaply from the `graph.fb` header** (mmap + vector lengths via
  `internal/graph/fbreader`, no full graph materialization), and the incremental
  reindex path now writes the sidecar so future cold reads are correct without
  the fallback. A truly-never-indexed group (no `graph.fb`) still reports
  `0 / never`.
- **Dead-ref GC now bounds grace-protected transient refs (retention cap,
  #5440):** the dead-ref sweeper (#5236) reaps refs git no longer knows about,
  but its 24h grace window had no backstop. A high-churn workload — the rewrite
  agent's `merge-NNNN` branches, created + deleted minutes apart but each indexed
  — left every deleted ref's fresh ~80MB `graph.fb` grace-protected for a full
  24h. On `core-backend-v3` this piled up **~1GB of dead-ref graphs** (12
  `merge-NNNN` dirs alongside `main` while `git for-each-ref` showed only
  `main`), mmap'd into the daemon and inflating RSS to ~1.6GB. The sweeper now
  applies a **retention cap** (`DefaultRefRetentionCap` = 8) per repo: of the
  dead-in-git refs the grace window protects, only the N most-recently-indexed
  are kept; the oldest beyond the cap are reaped immediately (mmap released via
  the existing `DropReader` path before unlink, Windows-safe). Live / primary /
  HEAD / active-worktree refs and the `_unknown` sentinel never count toward the
  cap and are never evicted by it; the fail-closed git-enumeration guard is
  unchanged. Runs on the existing reaper cadence — no new goroutine.
- **group-algo overlay now reports god-nodes' real PageRank (was 0):** the
  determinism rounding (`roundForDeterminism`) bucketed every score to a fixed
  **1e-4 absolute** quantum. On large group unions (28k+ entities) PageRank mass
  sums to 1 across all nodes, so even a top-5% **god-node**'s score is ~3–4e-5 —
  below the bucket — and `math.Round(v*1e4)/1e4` collapsed it to **0**. The
  overlay then showed a flagged god-node with `pagerank: 0`, a direct
  contradiction (a god-node is by definition among the most central). Rounding is
  now **hybrid**: scores ≥ 1e-3 keep the proven 1e-4 absolute bucket (issue #489
  byte-determinism unchanged), while scores < 1e-3 round to **4 significant
  figures** so small-but-meaningful values survive non-zero and stay ordered. The
  `is_god` flag and `pagerank` value are now consistent.
- **group-algo no longer pins the machine (CPU regression, v0.1.3):** the
  background group-scope analytics pass (Louvain + PageRank + betweenness over
  the whole group union) ran at the host's full GOMAXPROCS and re-fired on a
  30s debounce, so on a 12-core machine it sat at **500–1000% CPU for hours** and
  starved foreground work. It now runs **capped to 2 cores** (Go `GOMAXPROCS`,
  env `GRAFEL_GROUP_ALGO_CPU` to override — set `=1` for a single core; was
  effectively `NumCPU`), **niced (+10 OS priority demotion on Unix, no-op on
  Windows)** so even those cores yield to a consumer's CI/dev harness, and with a
  **3-minute debounce** (was 30s, env `GRAFEL_GROUP_ALGO_DEBOUNCE`) so a burst of
  commits coalesces into one pass instead of re-triggering on nearly every push.
  The `cap=` start-log now reports the real per-pass core cap, not the semaphore
  concurrency. Betweenness already self-samples above 8k nodes (K=512 pivots), so
  no change there; a follow-up may lower K further under the new cap.
- **Cached group re-applies the group-algo overlay when its file advances (#5403):** `State.Group()` now os.Stats the overlay on the serve path and re-stamps only when the mtime advances past the memoized value, so a recomputed overlay (scheduler or manual `group-algo --write`) takes effect without a daemon restart. Cheap (one stat/query), absence-tolerant. (Scheduler-trigger half for settled groups deferred for live validation.)
- **Settled groups proactively recompute a stale overlay (#5403, completes the fix with #5426):** the scheduler now runs a low-frequency overlay-freshness sweep (`overlaySweepLoop`, default every 10 min, `GRAFEL_OVERLAY_SWEEP_INTERVAL=0` disables) that checks each known group's on-disk overlay for staleness (`groupalgo.OverlayNeedsRecompute`: overlay EXISTS but a source repo's `graph.fb` advanced past the recorded `source_mtimes`) and re-arms the existing **debounced + CPU-capped** group-algo pass for the stale ones. Previously `scheduleGroupAlgo` only fired off a link pass — i.e. only for actively-reindexed groups — so a settled group's overlay could stay stale until its next reindex. The sweep is cheap (per-group stat-compares only), skips groups with no overlay yet (first-compute path owns those), and skips groups that already have a pending/in-flight pass so it never thrashes the debounce.
- **Daemon no longer exits on transient `Accept()` errors (#5424):**
  `acceptLoop` previously returned the serve loop on **any** `Accept()` error
  other than `net.ErrClosed`, which made `Run` unlink the unix socket and drop
  every MCP client. A transient failure under load — `EMFILE` (fd exhaustion),
  `ENOMEM`/`ENOBUFS` (memory pressure), `ECONNABORTED`, or any `net.Error` whose
  `Timeout()` is true — was therefore treated as fatal and caused an MCP outage.
  The loop now mirrors `net/http.Server.Serve`: it logs a WARN, backs off with an
  exponential delay (5ms → double → cap 1s, reset on a successful Accept), and
  keeps serving. The socket survives and only clean shutdown (`net.ErrClosed`) or
  a genuinely unrecoverable error exits.

### Added
- **`grafel_index_status` — per-repo index freshness (#5433):** a new
  **lightweight** MCP tool that reports each registered repo's index state
  (`current` \| `queued` \| `indexing` \| `dirty`) plus `indexed_ref` / `head_ref`,
  with optional `repo` (substring/exact) and `group` filters. It reads ONLY the
  scheduler's in-memory snapshot — it does **not** load or assemble the group
  graph — so agents can poll it cheaply. Fixes a head-of-line blocking footgun:
  the global `grafel_stats.is_indexing` flag is a single process-wide bool, so an
  agent that polled it to gate "is my repo ready?" was blocked by **any** repo's
  indexing, including unrelated ones (multi-agent / multi-worktree setups
  deadlock-waited). Agents should now gate on **their** repo's
  `state == "current"` (and `indexed_ref == head_ref` where both are known)
  instead of the global flag. The same `repo_index_states` array is also surfaced
  in `grafel_stats` for one-shot inspection. The data already existed in the
  scheduler (`inflight`/`pendingIndex`/`dirty`/`pendingRefs`); it is published via
  the existing `indexstate` leaf-package bridge (no new lock path, no import
  cycle). Closes #5433.
- **Binding-agnostic tree-sitter abstraction + Go on the official binding
  (#5418, B2 Phase 0, ADR 0023):** new `internal/treesitter/ts` façade (a minimal
  `Node`/`Tree`/`Parser`/`Language`/`Adapter` interface modelled on grafel's
  *actual* CST usage — `Type`/`Child`/`ChildByFieldName`/`StartByte`/`StartPoint`
  etc.; no query-engine surface) with two adapters: `ts/smacker` (wraps the
  current, unmaintained `smacker/go-tree-sitter` with no behaviour change — every
  grammar keeps running on it) and `ts/official` (wraps the maintained
  `tree-sitter/go-tree-sitter` v0.24.0). The **Go extractor is migrated end-to-end
  onto the façade** and parses via the official binding under `-tags ts_official`,
  ABI-pinned to `tree-sitter-go` v0.23.4 (the runtime-v0.24.0-compatible pair; a
  newer grammar compiles but SIGSEGVs at `RootNode` — ADR 0023 §6). Adds a startup
  **ABI guard** (smoke-parses trivial source, asserts a sane non-error root before
  any real file) and a per-grammar smoke test. **Co-link blocker found:** the
  smacker and official bundles each statically vendor the same tree-sitter C
  runtime under identical symbols, so a single binary linking both fails with
  ~247 duplicate symbols on macOS — the official path is therefore opt-in behind a
  build tag until Phase 1 resolves it; default builds link only smacker and every
  grammar (incl. Go) works exactly as before. Remaining 27 grammars stay on
  smacker; migration plan + per-language batch order in
  `docs/treesitter-binding-migration-plan.md`.
- **New-language-feature triage process (#5415, #5359 C1):**
  `docs/new-language-feature-triage.md` — the decision procedure that turns a new
  language version into scoped work. Each notable feature is classified
  **(a) parse-only** / **(b) needs-new-extraction** / **(c) changes-existing-extraction**,
  with explicit (a)-vs-(b) and (b)-vs-(c) tests and a "triage is spec-driven, not
  grammar-driven" rule (don't block triage on the stale grammar). Ships a fillable
  per-version **feature-impact-report template** (the A3 calendar cron, #5413,
  points at it when version N lands) and a **worked example classifying 8 real
  C# 13 features** (5×a · 1×b `field` keyword field-membership · 2×c `allows ref
  struct` + partial properties), each carrying the grammar-bump prerequisite.
  Wired into the cadence: A3 nudge → A2/A4 alarms fill the grammar-status row →
  (b)/(c) action items feed the C2 recipe / C3 backfill.
- **Extractor recipe for new constructs (#5416, #5359 C2):**
  `docs/extractor-recipe.md` — the repeatable, grounded build path for each
  (b)/(c) item from triage. Grounded in grafel's real architecture (pure manual
  tree-sitter `node.Type()` traversal, no native queries) citing the C# extractor
  (`internal/extractors/csharp/csharp.go`): locate the grammar node kind → add the
  `case`/`build…` emit in the right extractor → register any new Kind in
  `internal/types/kinds.go` + `All…Kinds()` and run the producer-kind guard
  (`go test ./internal/types/`) → **update `registry.json` + coverage docs in the
  SAME PR** (`go run ./tools/coverage update/gen/validate`, surgical 2-space edits)
  → `coverage fmt --check` passes → add a value-asserting fixture test with
  fixture-then-live validation. Includes a copy-into-PR pre-merge checklist.
- **B2 assessment — migrate to the official tree-sitter binding + per-language
  modules (ADR-0023, #5418, #5359 B2):** a written go/no-go assessment of moving
  off the unmaintained single `smacker/go-tree-sitter` dependency to the live
  `tree-sitter/go-tree-sitter` (v0.24.0) with per-language grammar Go modules.
  Covers the current usage surface (245 importing files / 1758 `Node` traversal
  sites / no native-query usage), the API delta (`Type()→Kind()`,
  `StartPoint()→StartPosition()`, unsigned ints, `NewLanguage(Language())`), a
  per-language module-availability table (21/28 clean · 3 source-swaps · 2
  caveats · 1 true gap — proto), CGO/release-matrix impact, an empirically
  verified ABI-pinning hazard (PoC built one grammar; a runtime/grammar version
  mismatch compiled but SIGSEGV'd at `RootNode()`), and a phased plan with the B1
  benchmark gate + rollback. Recommendation: GO, phased behind an abstraction
  layer; land Phase 0 (abstraction + 1 language) in 0.1.4, slip the rest.
- **Language-release calendar + reminder cron (#5413, #5359 A3):** a proactive
  freshness nudge that fires ahead of known release windows. `docs/language-release-calendar.md`
  documents the cadence for the predictable-cadence languages (Java Mar/Sep,
  C#/.NET Nov, Python Oct, Go Feb/Aug, TS ~quarterly, Rust ~6wk, Swift ~Sep,
  PHP Nov, Ruby Dec; irregular ones marked) plus a per-release checklist that
  feeds the C1 triage process (#5415) — for each version N, verify the new
  syntax parses (A4 canary / A2 cron) and that extractors model the new
  constructs. `.github/workflows/language-release-calendar.yml` (monthly cron +
  `workflow_dispatch`, no push/PR) opens/updates a single idempotent reminder
  issue (stable `grammar-release-watch` label) for the windows in the next
  ~8 weeks. Minimal permissions (`issues: write`, `contents: read`).
- **Renovate dependency-bump automation (#5410, #5359 A1):** `renovate.json`
  (extends `config:recommended` + dependency dashboard, monthly schedule,
  grouped Go-module PRs, `separateMajorMinor`, no auto-merge). A dedicated rule
  routes grammar-binding bumps (`smacker/go-tree-sitter`, the official
  `tree-sitter/go-tree-sitter` decouple target, and any future per-language
  `tree-sitter/tree-sitter-<lang>` modules) to a distinct `grammar-bump` +
  `needs-benchmark` label so they hit the B1 benchmark gate, never auto-merge.
  Honest framing: the pinned smacker binding is unmaintained (at its own
  upstream HEAD), so Renovate finds nothing newer on it today — it earns its
  place on the repo's other Go deps and goes grammar-live once B2 (#5418) splits
  grammars into per-language modules; **A2 (#5411) remains the real grammar
  alarm.** No Dependabot config exists (Renovate is the single bump tool). See
  `docs/grammar-freshness-audit.md` §4c–4d.
- **Per-language parse-error-node canary (#5414, #5359 A4):** the
  version-agnostic freshness alarm. During indexing, both the in-process and the
  subprocess extract paths now aggregate the existing per-parse tree-sitter
  `ErrorRatio` into a node-weighted **per-language ERROR-node rate**
  (`internal/treesitter/canary.go`, threaded through `BatchStats.ParseErrors` →
  coordinator → indexer). The rates, a baseline comparison, and a `spiked` flag
  are written to the `graph-stats.json` sidecar (`parse_error_canary` +
  top-level `parse_error_spike`), and a `WARN … SPIKE` line is logged when a
  language's rate exceeds its baseline by the absolute (`GRAFEL_CANARY_ABS_DELTA`,
  default +0.02) or relative (`GRAFEL_CANARY_REL_FACTOR`, default 2×) threshold —
  the direct symptom of unhandled new syntax. The committed baseline lives at
  `docs/grammar-canary-baseline.json` (override via `GRAFEL_CANARY_BASELINE`);
  zero/absent-language tolerant. See `docs/grammar-freshness-audit.md` §4b.
- **Grammar-freshness monthly cron + tracking issue (#5411, #5359 A2):** a
  scheduled GitHub Action (`.github/workflows/grammar-freshness.yml`, monthly
  cron + `workflow_dispatch`, no push/PR) plus a standalone Go checker
  (`tools/grammar-freshness`) that reads `grammars.lock`, queries each upstream
  `tree-sitter/tree-sitter-<lang>` repo's latest release (falling back to the
  default-branch commit date), and reports which grammars have moved ahead of
  the bundled smacker snapshot. When any are stale it creates or updates a
  single idempotent tracking issue (stable `grammar-freshness` label) listing
  them. Tracks each grammar **independently of the dependency** because the
  smacker binding is unmaintained, so Renovate-on-dep (A1) is blind; a dry run
  flags 24 of 28 grammars stale. Minimal permissions (`issues: write`,
  `contents: read`).
- **Grammar setup audit + `grammars.lock` manifest (#5359 B3):** committed a
  source-of-truth manifest mapping all 28 grammar-backed languages to their
  grammar source, the bundled `smacker/go-tree-sitter` snapshot (pinned
  `dd81d9e9be82`, 2024-08-27), and current upstream-latest, plus a
  `docs/grammar-freshness-audit.md` write-up. Key findings: no `replace`/fork is
  freshening grammars; the pinned smacker binding is at its own upstream HEAD and
  unmaintained since 2024-08-27 (so Renovate on the dep finds nothing — per-grammar
  tracking is the real alarm); `fidelity` is an IMPORTS-resolution metric, but a
  per-parse `ErrorRatio` already exists and just needs per-language aggregation for
  the A4 canary. Feeds the 0.1.4 freshness infrastructure (A2 cron) and the B2
  decoupling assessment.

### Changed
- **Migrated the `python` and `java` extractors onto the `ts.Node` abstraction
  (smacker-backed, no behavior change)** — B2 Phase 1 plumbing toward the
  one-runtime cutover (#5418, ADR 0023). Both extractors now traverse the
  binding-agnostic `internal/treesitter/ts` façade (`ts.Node`/`ts.Tree`) instead
  of the concrete `smacker/go-tree-sitter` `*sitter.Node`, and read the shared
  `FileInput.TSTree` (falling back to an inline smacker parse via the grammar
  adapter when no tree is supplied, as the Go extractor does). Default builds stay
  100% smacker-backed and link-safe; this is mechanical, behavior-preserving
  plumbing — the full python+java extractor suites pass unchanged (zero fidelity
  delta). Mirrors the Phase-0 Go-extractor migration.
- **Migrated the `javascript`+`typescript` extractors to the `ts.Node`
  abstraction (smacker-backed, no behavior change)** — B2 Phase 1 (#5418). The
  JS/TS extractor (which handles both `javascript` and `typescript`) now
  traverses the binding-agnostic `internal/treesitter/ts` façade
  (`ts.Node`/`ts.Tree`) and reads the shared `FileInput.TSTree` instead of the
  concrete `smacker/go-tree-sitter` `*sitter.Node`/`*sitter.Tree`. No inline
  parse fallback exists (the extractor early-returns when no tree is supplied),
  so no grammar provider was added; test tree-helpers build `ts.Tree` via the
  smacker adapter and stamp `TSTree`. Default builds stay 100% smacker-backed and
  link-safe; mechanical and behavior-preserving — the javascript+typescript
  extractor suite passes unchanged (zero fidelity delta).
- **Migrated the `ruby`, `php`, `csharp` and `rust` extractors to the `ts.Node`
  abstraction (smacker-backed, no behavior change)** — B2 Phase 1 (#5418). All
  four extractors now traverse the binding-agnostic `internal/treesitter/ts`
  façade (`ts.Node`/`ts.Tree`) and read the shared `FileInput.TSTree` instead of
  the concrete `smacker/go-tree-sitter` `*sitter.Node`/`*sitter.Tree`. Each
  early-returns when no tree is supplied (no inline parse fallback), so no grammar
  provider was added; test tree-helpers build `ts.Tree` via the smacker adapter
  and stamp `TSTree`. The `ts.Node` façade gains a `PrevSibling()` method (added
  to both the smacker and official adapters) that the Rust extractor's
  derive-macro scan requires. Default builds stay 100% smacker-backed and
  link-safe; mechanical and behavior-preserving — the ruby+php+csharp+rust
  extractor suites pass unchanged (zero fidelity delta).
- **Migrated the remaining extractors to the `ts.Node` abstraction — B2 Phase 1
  is complete; every extractor is now binding-agnostic** (#5418, ADR 0023). The
  final batch covers `cpp` (C/C++), `css`, `dockerfile`, `elixir`, `groovy`,
  `hcl`, `html`, `kotlin`, `lua`, `proto`, `scala`, `shell` (bash), `swift`,
  `yaml`, and the `cross/abibridge` test harness. All now traverse the
  binding-agnostic `internal/treesitter/ts` façade (`ts.Node`/`ts.Tree`) and read
  the shared `FileInput.TSTree` instead of the concrete `smacker/go-tree-sitter`
  `*sitter.Node`/`*sitter.Tree`. Extractors with an inline-parse fallback (`cpp`,
  `dockerfile`, `hcl`, `html`, `yaml`) gained an untagged smacker grammar provider
  (their `language.go`/`grammar.go`) that the fallback constructs via the ts
  adapter; the others early-return on a nil tree. The `ts.Node` façade gains a
  `FieldNameForChild(i int) string` method (added to the interface and BOTH the
  smacker and official adapters — the official adapter reconciles the `int`↔`uint32`
  index width) that the Dockerfile extractor's field lookup requires; it compiles
  under `-tags ts_official` too. With this batch **no extractor imports the root
  `github.com/smacker/go-tree-sitter` binding** any longer. Default builds stay
  100% smacker-backed and link-safe; mechanical and behavior-preserving — every
  migrated extractor suite passes unchanged (zero fidelity delta).

## [0.1.3] — 2026-06-23

### Fixed
- **`TestOverlay_NoTornRead` greens on Windows CI:** the writer now tolerates a
  transient atomic-rename failure under the artificial 4-reader stress (a failed
  swap is not a torn read — readers still see the prior complete file) and readers
  sleep 1ms between reads so the rename reliably finds a window. Asserts the real
  property (zero torn reads) plus that writes make progress. Unix unchanged.

- **Windows CI green for the group-algo overlay (no production behavior change on
  Unix):** the overlay's atomic temp+rename now **retries on the transient
  Windows sharing/access violation** (`ERROR_SHARING_VIOLATION` /
  `ERROR_ACCESS_DENIED` / `ERROR_LOCK_VIOLATION`) raised when `os.Rename`
  replaces a destination a concurrent reader still has open — a bounded backoff
  (~10 tries) that rides out the microsecond window the MCP reader holds the file
  (`TestOverlay_NoTornRead` is the stress case). On Unix it is a single
  `os.Rename` (open files are inode-referenced, so rename-over-open always
  succeeds) — unchanged. Separately, the `ApplyOverlay` MCP tests now
  `State.Close()` (unmap each repo's `graph.fb` mmap) before `t.TempDir`'s
  cleanup, because Windows cannot delete a memory-mapped file while the view is
  open — `t.Cleanup` is LIFO so registering the unmap after `t.TempDir` runs it
  first.
- **Group-algo overlay now keeps surfacing on `inspect`/`orient`/`stats`/`clusters`
  after a repo is reparsed — incl. `core-mobile` (Fixes #5400, #5401, #5397):**
  `applyGroupAlgoOverlay` memoized the per-entity stamp at the **group** level by
  the overlay FILE's mtime. But a repo's `graph.fb` can be rewritten (a reparse →
  fresh `doc.Entities` carrying the per-repo sentinel `community_id:-1`) AFTER the
  overlay was first applied. With the file-level memo, that reparsed repo was
  never re-stamped and silently reverted to `community_id:-1` — exactly the
  `core-mobile` symptom (#5401): `grafel_orient` showed `community_id:-1` for its
  entities, `grafel_inspect` surfaced no algo fields at all (#5400), and
  `grafel_stats` + `grafel_clusters repo_filter=core-mobile` reported 0
  communities (#5397) — even though the overlay placed core-mobile in community
  80. The stamp memo is now **per repo**: a repo is re-stamped whenever its
  `graph.fb` was reparsed since the last stamp (or the overlay file advanced), so
  the overlay community/pagerank/centrality survive a mid-session reparse of any
  one repo. `grafel_stats` now also derives each repo's community count from the
  overlay-stamped per-entity `community_id` (matching `grafel_clusters`) instead
  of the now-empty per-repo `Doc.Communities`. Fully absence-tolerant.

- **MCP read-side now serves the group-algo overlay instead of per-repo Louvain
  (Fixes #5396, #5397):** the group-level overlay (`~/.grafel/groups/<group>-algo.json`)
  was computed correctly and stamped onto entities by `applyGroupAlgoOverlay`,
  but the query tools never read it. `grafel_clusters`/`handleListCommunities`
  now serves the **group** communities when the overlay is applied — so a
  community can surface members spanning >1 repo (reported via a `repos` list and
  a `cross_repo` flag instead of being force-tagged to a single repo), and
  `core-mobile` entities (community 80) appear instead of a whole repo silently
  showing 0 communities (#5397). A `repo_filter` naming only one repo of a
  cross-repo community still surfaces that community. `grafel_inspect` now
  surfaces the overlay `community_id`/`pagerank`/`centrality` (and god-node /
  articulation-point flags) when requested via
  `include=community,pagerank,centrality`, not only under `verbose` — and
  `centrality` is surfaced at all for the first time. `grafel_orient` already
  reads the overlay-stamped per-entity values. Fully absence-tolerant: with no
  overlay present, every tool keeps its prior per-repo behavior unchanged.

### Changed

- **Un-deprecated `grafel_expand` / `grafel_find_callers` / `grafel_find_callees`
  — they are first-class tools again (Refs #5386):** live MCP usage metrics show
  these are actively used (`find_callers` 16 calls) and agents reach for the
  explicit names over `grafel_neighbors(direction=…)` (2 calls). The
  "Deprecated…" framing is removed from their tool descriptions and the
  handshake instructions; all tools (including `grafel_neighbors`) are kept and
  fully functional. The `node`→`entity_id` param alias on `grafel_expand`
  (#1916) is unaffected.

- **MCP `tools/list` token trim — per-connect handshake ~7592 → ~6113 tokens
  (−1479, −19.5%) (Refs #5387):** strip the blanket annotation-hint block
  (`readOnlyHint`/`destructiveHint`/`idempotentHint`/`openWorldHint`) that
  mcp-go's `NewTool` stamps on every tool by default. The four hints were
  identical across all 68 registered tools and inaccurate (read-only query
  tools like `grafel_find` were advertised as destructive), so they were pure
  duplicated boilerplate. A centralized `addTool` helper resets the annotation
  to empty when it still carries exactly the `NewTool` default, so it serializes
  as `{}` instead of the ~89-char hint block; all 68 registrations route
  through it. No change to the tool surface (names, params, types, enums,
  required-sets, handlers) or behavior — measured by `cmd/mcp-audit`. (The live
  daemon bridge `MCPToolInfo` never emitted annotations, so this is zero-change
  there and a win for the stock mcp-go stdio path / the audited budget.)

- **Graph algorithms now run once per GROUP via a debounced/capped/background
  scheduler; the per-repo algorithm pass is removed (Refs #5355, #5349):**
  communities, PageRank, betweenness, god-nodes and articulation points are now
  computed ONCE over the assembled group union — so cross-repo edges are finally
  seen — by a new `scheduleGroupAlgo` chained off the **success path of the
  cross-repo link pass**. Because the link pass already coalesces a burst of
  repo reindexes, N file saves collapse into 1 link pass and then 1 group-algo
  pass (default 30s debounce, env `GRAFEL_GROUP_ALGO_DEBOUNCE`). The pass runs in
  the background under the existing `algoSem` cap and, by default, in a
  short-lived `grafel group-algo <group> --write` child process so the heavy
  union-graph heap is isolated and reclaimed on exit (opt out with
  `GRAFEL_SUBPROCESS_INDEXER=0`); its context derives from the scheduler's
  shutdown context for clean SIGTERM cancellation. On completion it writes the
  `<group>-algo.json` overlay (A2), which the MCP apply path picks up by mtime on
  the next group load. The old per-repo algorithm pass (`scheduleAlgo` /
  `daemonSchedulerAlgo` / the `GRAFEL_EAGER_ALGO` eager path) and the per-repo
  Pass-4 computation in the index flow are removed — a single-repo group is the
  degenerate one-repo union, so single-repo groups still get algorithms via the
  group pass. The per-entity `graph.fb` algo fields are kept (vestigial, one
  release) but left at their schema sentinels rather than recomputed per-repo;
  the canonical entity/relationship sort that the pass performed is preserved for
  downstream passes. An in-flight group-algo pass is surfaced in
  `grafel_stats`' `is_indexing` (`group_algo_in_flight`). The now-dead on-demand
  lazy algo cache (`internal/mcp/algo_demand.go`, the unused `ensureAlgoResults`
  path) is deleted so there is a single algo path. **No behavior change until
  deployed.**

### Added

- **Group-algo differential validator + sampled-pivot betweenness perf guard
  (Refs #5356, #5349):** a `grafel group-algo <group> --diff` mode runs BOTH the
  OLD per-repo pass (re-derived locally — the production per-repo pass was removed
  in A3) and the NEW group pass over the union, then emits a machine-readable
  JSON report (`DiffReport`): # entities whose `community_id` changed, the top-N
  PageRank **rank churn**, the modularity delta, and a **cross-repo-rank
  non-decreasing assertion** — no entity that receives a cross-repo phantom CALLS
  edge may LOSE PageRank rank group-vs-repo (the core thesis; the process exits
  non-zero and lists the regressions if it ever does), so CI / the upvate
  baseline re-run can gate on it. Separately, `ComputeCentrality` gains a
  **sampled-pivot betweenness approximation** (deterministic seed, K random Brandes
  pivots scaled by V/K) gated by node count — exact below the threshold, sampled
  above (default 8000, env `GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD`); PageRank and
  community detection stay exact every pass. This bounds the ~O(V·E) exact
  betweenness on 28k+-entity group unions: a 28k-node synthetic pass completes in
  seconds under budget, and full-vs-sampled top-50 betweenness overlap is ≥0.9
  (the god-node tier is preserved). **No behavior change until deployed.**

- **Group-algo overlay storage + atomic swap, applied at MCP group load
  (Refs #5354, #5349):** the group-scope algorithm pass (A1) now persists its
  result as a single `~/.grafel/groups/<group>-algo.json` overlay
  (`entity_id → {community_id, pagerank, centrality, is_god_node,
  is_articulation_point}` + a community summary, stats, and per-repo
  `source_mtimes`). The overlay is written via an atomic temp-file + rename, so
  a reader observes either the whole previous overlay or the whole new one —
  never a torn read across files. At MCP group load the overlay, when present
  and not stale (every recorded repo's current `graph.fb` mtime still matches),
  is stamped onto the in-memory entities by ID and surfaced as
  `LoadedGroup.Communities`; it is memoized by mtime so a mid-session swap
  reloads only the overlay. **Absence-tolerant:** a missing or stale overlay is
  a no-op — entities keep whatever `graph.fb` carried (today's per-repo values)
  — so there is **no behavior change until an overlay file exists** (only A3's
  scheduler will produce one). The hidden `grafel group-algo` command gains a
  `--write` flag to persist the overlay; the default stays dry.

### Fixed

- **Stabilized flaky daemon timing tests (`TestIndexProgress_LivePolling` +
  SSE/poller siblings):** replaced fixed `time.Sleep` waits + tight deadlines
  with deterministic bounded-await on the actual condition, so the pre-milestone
  full 3-OS CI is reliable. `TestIndexProgress_LivePolling` now awaits the
  rebuild session becoming visible mid-flight (instead of a 20ms sleep + a 200ms
  rebuild window that scheduler jitter on a loaded `-race` runner could overrun);
  `TestSSE_MultipleSubscribers` awaits both subscribers attaching via
  `broker.Stats()` (instead of a 50ms sleep before publishing, which could race
  ahead of a not-yet-registered subscriber); and `TestSSE_DisconnectRemovesSubscriber`
  awaits the broker deregistering the subscriber (instead of asserting once after
  a fixed 200ms). Test-only changes; no production behavior change.

- **Daemon watcher re-index loop on build artifacts → memory thrash + MCP
  socket loss (Refs #5392):** the file-watcher now ignores build/output
  artifacts and gitignored paths and coalesces per-repo reindexes, so build
  churn (e.g. an Android AAB/gradle build under a watched repo) no longer
  triggers a continuous reindex loop / heap thrash. The static event-boundary
  ignore set gained the mobile build dirs/outputs (`AAB/`, `.dart_tool/`,
  `*.aab`/`*.apk`/`*.ipa`/`*.aar`) and generated-file globs
  (`*.generated.*`, `*.g.dart`, `*.pb.go`, ...); the watcher now also honours
  the repo's `.gitignore` at the event boundary (not just at directory
  subscription time) so a write under any gitignored path is dropped before it
  can arm a reindex, and exposes a `GRAFEL_WATCH_EXTRA_SKIP_DIRS` ops override.
- **Windows installer latest-version auto-resolution produced garbage → 404
  (Refs #5318):** `install.bat` extracted the release tag from the
  `/releases/latest` redirect `location:` header with `%~nx`, which treats its
  argument as a `\`-separated Windows path — but the header value is a
  `/`-separated URL, so on some Windows builds it yielded garbage (e.g.
  `LOC:=`) and built a 404 download URL. The tag is now sliced from the URL
  with a delimiter-correct substring (`!LOC:*/tag/=!`, after the CR scrub),
  with a GitHub releases-API fallback (`api.github.com/.../releases/latest`,
  `tag_name`) when the redirect parse fails, and a sanity guard that rejects any
  resolved version that does not look like a tag (must start with `v`, contain a
  digit, and contain no `/`) before attempting a download — surfacing the
  explicit `GRAFEL_VERSION` hint instead of a confusing 404. `install.ps1`
  already used proper URI/regex parsing and is unaffected.

---

## [0.1.2] — 2026-06-23

### Added

- **Group-level graph algorithms — foundation (Refs #5353, #5349):** a hidden
  `grafel group-algo <group> --dry-run` command assembles the union of a
  group's per-repo graphs (entities + relationships, including the cross-repo
  phantom CALLS edges already written into each `graph.fb` by the link pass)
  and runs the Louvain communities + PageRank/Betweenness centrality pass
  **once** at group scope, printing stats (union counts, communities and how
  many span >1 repo, modularity, top-10 PageRank with source repo). This is the
  foundation for group-level communities/centrality so cross-repo hubs rank by
  their true cross-repo importance. No behavior change to the default path yet —
  overlay storage (A2) and the debounced/capped/background scheduler (A3) land
  in follow-ups. New package `internal/graph/groupalgo`.
- **The wizard (CLI + web) now lets you choose which AI tools get the grafel
  MCP server (#5344):** instead of auto-registering the grafel MCP entry in
  every detected AI tool (Claude Code, Cursor, Windsurf, Codex, Kiro,
  Antigravity), the setup wizard adds a "Configure MCP for which tools?" step
  that lists the detected tools and lets you pick. The default is a smart
  pre-selection — a tool is checked when its MCP config was modified recently
  (within ~30 days) **or** already contains a grafel entry — and the wizard
  **remembers your last choice** (persisted to `~/.grafel/mcp-tools.json`),
  defaulting to it on subsequent runs. The step is skipped automatically when
  ≤1 tool is detected. For scripting, `grafel wizard --mcp-tools=claude,cursor`
  registers exactly those tools and `--no-mcp` registers none; without either
  flag, non-interactive runs keep today's behavior (register every detected
  tool) so existing scripts are unaffected
  ([#5344](https://github.com/cajasmota/grafel/issues/5344)).

### Fixed

- **`test.yml` round-2: OS-portable canonicalize-timeout test + deterministic
  SSE heartbeat test → green on all three OS:** two more test-only failures
  surfaced after the first batch — (1)
  `TestCanonicalizePathTimesOutAndDegrades` (`internal/daemon`) hardcoded
  Unix-style absolute paths (`/tmp/grafel-5330/...`) and asserted the degraded
  result equalled that literal string; on Windows the casing-preserving
  fallback rebuilds the path via `filepath.Join` with the active OS's volume +
  separator semantics (drive letter, `\`), so the assertion failed even though
  the timeout-degrade behavior worked. The test now builds an OS-native path
  with `t.TempDir()`/`filepath.Join` and asserts against `filepath.Clean(input)`
  so it holds on linux, darwin, AND windows; the cache test was made OS-native
  the same way. (2) `TestSSE_TerminalReassertedOnHeartbeat`
  (`internal/dashboard`) was a timing flake — it read a fixed SSE line-window
  for a bounded 4s and could miss the heartbeat-reasserted terminal+close when
  a loaded CI runner delivered them a few heartbeats late. It now polls via a
  new `readSSEUntil` helper until BOTH the terminal phase and the `close` event
  are observed, under a generous deadline, so it passes reliably under `-race`
  and repeated runs. Production behavior is unchanged.
- **`test.yml` is now green on all three OS (macOS, Linux, Windows), unblocking
  the v0.1.2 tag:** three test-only failures were fixed without touching
  production behavior — (1) a data race in the canonicalize-timeout tests
  (`internal/daemon`): the abandoned `readDirBounded` timeout goroutine kept
  reading the injected `readDirFunc` package var (and a plain `int` counter)
  after the test body had moved on, so the tests now track in-flight
  invocations with a `WaitGroup`, park blocked closures on a `stop` channel the
  helper closes before draining, and use an `atomic.Int64` counter — race-clean
  under `go test -race` and fast (no literal 10s sleeps)
  ([#5346](https://github.com/cajasmota/grafel/issues/5346)); (2) the
  Done-screen wizard-TUI tests (`internal/cli/wiztui`) hardcoded Unicode glyphs
  (`✓`/`⚠`/`·`) and failed on legacy Windows where the glyph set is ASCII —
  they now pin the Unicode set with `withGlyphs` and assert via the active
  set's symbols, so they pass under both glyph sets
  ([#5342](https://github.com/cajasmota/grafel/issues/5342) ×
  [#5345](https://github.com/cajasmota/grafel/issues/5345)); and (3)
  `TestPoller_EmitsForSourceReindex` (`internal/daemon/watch`) was timing-flaky
  on Windows — the baseline HEAD snapshot is now captured before the commit
  with a mod-time-granularity guard, and the event is awaited with a generous
  deterministic deadline instead of a tight 1s budget.
- **Graph-view rebuild toast now reports the graph's real entity/relationship
  totals instead of "0 entities, 0 relationships" (#5326):** after a background
  rebuild on the graph view, the completion toast read "rebuilt N repo(s): 0
  entities, 0 relationships" even for a fully populated graph (e.g. 3,888
  entities). `RebuildReply.TotalEntities`/`TotalRels` were left at 0 on the
  progress path because the session totals were never accumulated. The daemon
  now sums the per-repo `graph-stats.json` sidecars (the same cheap, mmap-free
  source the CLI rebuild summary uses) into the reply on rebuild completion, so
  the toast shows the real totals. When a rebuild legitimately has nothing to
  report, the toast now reads "up to date" instead of "0 entities"
  ([#5326](https://github.com/cajasmota/grafel/issues/5326)).
- **Dashboard index wizard now shows one row per repo instead of a single group
  row (Refs #5340, #5326):** the "Index a new group" dialog's Index step could
  collapse to a single row labelled with the GROUP name (e.g. "ivivo · Indexed")
  rather than one row per repo (backend + frontend). This is the web analog of
  the CLI fixes (#5343/#5348). Four changes, mirroring the Go wizard: (1) the
  group-scoped progress event (`repo_slug === group`, the cross-repo links/flows
  pass) is excluded from the per-repo rows and its phase is surfaced in the
  OVERALL header label instead of spawning a spurious group row; (2) the Index
  step seeds one pending row per expected repo from the registered repo list, so
  every repo shows up front and survives dropped early SSE events; (3) the
  progress SSE subscription is opened as soon as the from-scan POST fires —
  before the index starts — so fast indexes don't miss early per-repo events;
  (4) on successful completion every non-terminal row is finalized to Done so
  the final frame shows all repos Done. The single-repo case is unchanged
  ([#5340](https://github.com/cajasmota/grafel/issues/5340)).
- **Wizard indexing view now marks all per-repo rows Done on successful
  completion (#5340):** previously a repo could freeze on its last intermediate
  phase (e.g. "Building communities…") if its final SSE events (centrality →
  writing → done) arrived after the Rebuild RPC returned done and the forwarder
  stopped. The overall bar reached "Done · 100%" but the stuck row disagreed. On
  a successful `IndexOutcome` the view now finalizes every non-terminal row to
  Done (preserving its files/entities counts); failures leave rows as-is.
- **Daemon startup no longer deadlocks when `canonicalizePath`'s `os.ReadDir`
  blocks on a slow filesystem (#5330):** at startup the daemon canonicalizes
  each known repo path by walking its ancestors with a per-segment `os.ReadDir`,
  previously with no timeout. If a single ancestor's FS call hung (a
  `~/Documents` iCloud/Spotlight/TCC stall, a slow/unresponsive mount, or an
  observed launchd-context permission stall) the entire startup blocked forever
  and the daemon never began serving — the hang was only visible via a SIGQUIT
  goroutine dump. Each `os.ReadDir` is now bounded by a timeout (default 3s,
  overridable via `GRAFEL_CANONICALIZE_TIMEOUT_MS`); on timeout `canonicalizePath`
  degrades to preserving the input casing for that and remaining segments — the
  same fallback it already takes on a read error — and logs a WARN with the
  offending path. A new `startup: state-migration begin` log is emitted before
  the migration runs so a wedge here is diagnosable without a goroutine dump
  ([#5330](https://github.com/cajasmota/grafel/issues/5330)).

- **`grafel wizard` TUI renders correctly on legacy Windows CMD/conhost
  (#5340, #856):** the wizard's Bubble Tea TUI used Unicode glyphs (`›`, `✓`,
  `↑/↓`, `·`, `…`, `⚠`, `[✓]`, the braille spinner) with no console code-page
  setup, so outside Windows Terminal it could render as mojibake. The TUI now
  (1) switches the Windows console to the UTF-8 code page (65001) at startup and
  restores the previous code page on exit, so a modern CMD/conhost with a
  TrueType font shows the Unicode glyphs; and (2) falls back to an ASCII-safe
  glyph set (`>`, `v`, `^/v`, `-`, `...`, `!`, `[x]`, `|/-\` spinner) on legacy
  consoles. The glyph set is chosen by a single, unit-tested selector: ASCII on
  Windows unless `WT_SESSION` (Windows Terminal) or `GRAFEL_TUI_UNICODE=1` is
  set; `GRAFEL_ASCII=1` forces ASCII on any OS; non-Windows defaults to Unicode
  ([#5340](https://github.com/cajasmota/grafel/issues/5340),
  [#856](https://github.com/cajasmota/grafel/issues/856)).
- **`grafel wizard` indexing view reliably shows one row per repo (#5340):** the
  TUI indexing screen could collapse to a single row labeled with the GROUP name
  (e.g. "ivivo") reaching Done instead of one row per repo (backend, frontend).
  Two causes are fixed: (1) the wizard now establishes the broker SSE
  subscription **before** triggering the Rebuild RPC, so the early per-repo
  extraction events aren't missed when the index runs fast (previously the
  rebuild fired first and the per-repo events had already replayed by the time we
  subscribed); and (2) the group-scoped event (the cross-repo links/flows pass,
  whose `repo_slug` equals the group) is no longer folded into a spurious
  per-repo row — its phase surfaces in the overall "Indexing &lt;group&gt; — …"
  label instead ([#5340](https://github.com/cajasmota/grafel/issues/5340)).
- **Watcher install is robust to the flaky launchctl err-5 and never aborts the
  wizard (#5338):** `launchctl bootstrap` intermittently fails the first
  bootstrap of a freshly written plist with exit code 5 ("Bootstrap failed: 5:
  Input/output error"). The macOS watcher loader now retries the
  bootout→bootstrap pair (bounded, with a small backoff) **specifically on err
  5**, which clears the transient failure. And a watcher that still fails to
  activate is now a **non-fatal warning** instead of an abort: the group config
  is already persisted, so the wizard completes and the group indexes
  ("warning: watcher for X not activated: …; the group is still registered and
  will index") ([#5338](https://github.com/cajasmota/grafel/issues/5338)).
- **Deleting a group now cleans up its watcher launchd/systemd/schtasks units
  (#5338):** group delete (CLI `grafel delete`, dashboard `DELETE
  /api/v2/groups/{group}`) previously left `com.grafel.watcher.<group>.*` jobs
  loaded and plists on disk, so recreating the group fought stale state. Delete
  now Unloads (bootout) and removes the watcher unit + plist for every repo in
  the group — idempotent, tolerating "not loaded"
  ([#5338](https://github.com/cajasmota/grafel/issues/5338)).

### Changed

- **`grafel wizard` TUI polish — per-screen context, light-blue accent, and a
  captured Done summary (#5340):** each step now carries a concise explanatory
  subtitle so it's clear what's being configured (what indexing a single repo /
  group / monorepo means; "Choose which repositories to include in this group";
  the Name screen names the actual repos being grouped; the optional-docs screen
  explains shared markdown docs). The accent (header badge, step-rail pills,
  cursor `›`, selected/active highlights) moved from pink/magenta to a tasteful
  **light blue** (adaptive 256-color `117`/`75`), keeping green for done/✓. And
  the install/index output that previously scattered over the alt-screen on
  completion ("saved …", "installed N hooks/watchers/MCP", watcher warnings) is
  now **captured and rendered inside the Done screen** — a clean summary line
  plus any watcher warning as a styled non-fatal note — so nothing prints
  misaligned after the TUI tears down (the non-TTY/plain path still prints the
  normal messages) ([#5340](https://github.com/cajasmota/grafel/issues/5340)).
- **`grafel wizard` now uses a cohesive Bubble Tea TUI (#5340):** the
  interactive wizard is a single full-screen `tea.Model` state machine with
  consistent chrome on every step — a styled grafel header + a step rail
  (`Action › Select › Name › Index`) highlighting the current stage, a spacious
  body sized to the terminal (the four actions are always fully visible; long
  repo lists scroll inside a tall viewport with a position indicator), and a
  contextual footer key-hint bar on every screen (including the group-name and
  optional-docs inputs → "optional · enter to skip"). The indexing step is now a
  **per-repo view**: an overall progress bar plus **one row per repo** (name ·
  phase label · files done/total · entities · spinner while active), folding the
  broker SSE phase stream by `repo_slug` with a monotonic phase — which also
  **fixes the dropped-repo display bug** where the old single-line
  carriage-return renderer showed only one repo and dropped the rest. All
  decision logic and side effects are preserved (`ClassifyPath`, group-name
  default, `applyGroupConfig`, daemon auto-index); `ctrl-c`/`esc` cancel cleanly
  with nothing registered. Non-interactive flags
  (`--repos`/`--parent`/`--exclude`/`--no-index`) and non-TTY/CI/`$TERM=dumb`
  contexts never launch the TUI and keep the line-based flow
  ([#5340](https://github.com/cajasmota/grafel/issues/5340)).
- **`grafel wizard` auto-indexes the new group with live progress (#5338):**
  after registering a group (or adding repos to an existing one), the wizard now
  triggers an index by default and renders live CLI phase progress — the same
  broker event stream the dashboard shows — so it ends register → "Indexing…" →
  "Done". A `--no-index` flag skips it for scripting, and a down daemon is a
  warning (the group is registered and indexes later), not a failure
  ([#5338](https://github.com/cajasmota/grafel/issues/5338)).
- **Wizard group-name default is the container folder, not a child repo
  (#5338):** from `ivivo/` holding `backend/` + `frontend/`, the suggested group
  name is now `ivivo` (the common-parent container folder) instead of an
  arbitrary selected child repo's slug. A single selected repo still defaults to
  its own basename ([#5338](https://github.com/cajasmota/grafel/issues/5338)).
- **Wizard TUI gains navigation hints, more height, and `[ ]`/`[✓]` checkboxes
  (#5338):** each interactive step now shows a footer hint ("↑/↓ move · space
  select · enter confirm · / filter · esc back") so users discover that **space**
  toggles a multiselect item; the select/multiselect lists use more terminal
  height (and scroll when long) instead of a few cramped rows; and multiselect
  options now render explicit `[ ]`/`[✓]` brackets (the stock huh `ThemeCharm`
  glyphs `•`/`✓` were ambiguous — the fix sets the correct
  `SelectedPrefix`/`UnselectedPrefix` theme fields)
  ([#5338](https://github.com/cajasmota/grafel/issues/5338)).

### Changed

- **Index wizard is now action-first with smart cwd detection (#5336):** both the
  CLI (`grafel wizard`) and the dashboard scan wizard now open on an explicit
  action choice — **Index a single repository**, **Index a group of related
  repositories**, **Index a monorepo**, **Add a repository to an existing
  group** — with the cursor pre-placed on a smart default derived from the
  current directory. This fixes container folders: a parent directory holding
  multiple repos (e.g. `ivivo/` with `backend/` + `frontend/`) now resolves to
  exactly those child repos instead of scanning the cwd's PARENT for unrelated
  siblings. CLI and dashboard share a single source of truth — the new
  `detect.ClassifyPath` classifier — so they agree on what a folder is (its own
  git status, immediate child git repos, monorepo packages, sibling repos) and
  which action to suggest. The non-interactive `--repos`/`--parent`/`--exclude`
  flags are unchanged for scripting
  ([#5336](https://github.com/cajasmota/grafel/issues/5336)).
- **Granular graph-assembly progress phases in the CLI and wizard (#5334):** the
  graph-assembly tail used to collapse under one coarse "Materializing" /
  "Running algorithms" label, so the long post-extraction passes looked stuck.
  The real passes are now surfaced as live phases — **Building communities**
  (Louvain), **Computing centrality** (PageRank/Betweenness), **Computing
  flows** (process/event-flow walkers), **Writing graph**, and the group-level
  **Detecting cross-repo links** — with identical human labels in BOTH the
  `grafel index`/`rebuild`/`wizard` terminal output (live, TTY-aware) and the
  dashboard scan wizard. The coarse phases are retained as fallbacks
  ([#5334](https://github.com/cajasmota/grafel/issues/5334)).
- **Index wizard main progress bar now reflects real progress (#5332):** the
  wizard's top progress bar was driven solely by the coarse job poller, which
  barely moves during indexing — so it looked frozen near 0% even while every
  repo was at "Materializing"/"Indexed". It now derives a real aggregate from
  the per-repo feed (each repo contributes a phase-weighted fraction that
  advances as it crosses phase boundaries, including through the long
  sub-progress-less "Materializing" phase), and the header shows the current
  overall phase ("Scanning…", "Extracting AST…", …, "Materializing graph…",
  "Done") instead of a static "Indexing…". The active bar also gets a tasteful
  shimmer (respecting `prefers-reduced-motion`) so it never reads as stuck
  ([#5332](https://github.com/cajasmota/grafel/issues/5332)).
- **Windows / locale resilience (#5317):** swept the codebase for control flow
  that branched on a *localized* OS command output / error string — the bug
  class behind the Spanish-Windows `schtasks` install failure — and migrated the
  genuine occurrences to locale-invariant signals (process exit codes, on-disk
  unit-file checks, and typed `syscall.Errno` values). Migrated: the watcher
  `Unload()` for schtasks / systemctl / launchctl (`internal/install/watchers/`,
  which had the same English-text match as the original bug), the daemon
  launchd `Load()`/`Unload()` (`internal/daemon/service/launchd_darwin.go`), and
  the selftest Windows "file in use" detector (`cmd/grafel/selftest.go`, now
  errno-based). The daemon `schtasks` `Unload()` keeps its English match only as
  a documented best-effort race fallback behind the exit-code `IsLoaded()` check
  (Refs [#5317](https://github.com/cajasmota/grafel/issues/5317),
  [#856](https://github.com/cajasmota/grafel/issues/856)).
- Dashboard Paths → "Downstream flow" modal now defaults to and only shows the
  **Tree** view; the **Flowchart** view is hidden behind a `SHOW_FLOWCHART` flag
  pending a layout fix (it currently renders disconnected EXIT/ENTRY/RETURN
  fragments). The flowchart renderer is retained, just gated
  ([#5324](https://github.com/cajasmota/grafel/issues/5324)).

### Added

- **CI lint gate `lint-localematch` (#5317):** a standard-library Go AST analyzer
  (`cmd/lint-localematch`) that fails CI when new code matches command
  output/error text for control flow — `strings.Contains/HasPrefix/HasSuffix/
  Index/EqualFold` or `regexp.MatchString` whose subject is data-flow-derived
  from an `exec.Command(...).Output()/CombinedOutput()/Run()` result or an
  `err.Error()` in an exec-using file. Scoped to files that shell out (low false
  positives); justified race fallbacks opt out with `// nolint:localematch`.
  Wired into `make lint`, `pre-merge.yml`, and `test.yml`
  (Refs [#5317](https://github.com/cajasmota/grafel/issues/5317)).

### Fixed

- **Dashboard wizard showed only ONE repo of a multi-repo group** (Refs
  [#5326](https://github.com/cajasmota/grafel/issues/5326)). The feed-terminal
  fallback added for the freeze fix treated the per-repo SSE feed as terminal as
  soon as *every row seen so far* was `done`/`error`, without knowing how many
  repos to expect. Under the broker's drop policy the first repo could reach
  `done` before the second repo emitted a single event, so the feed looked
  terminal and the wizard tore the SSE subscription down — only one repo row
  ever rendered (which one was a race, hence inconsistent backend-vs-frontend),
  even though the backend correctly indexed all repos. The wizard now threads the
  EXPECTED repo count (the same repo list it registers to start indexing) into
  the feed: feed-terminal fires only once **all** expected repos have reported a
  terminal phase, and the job poller remains the primary terminal signal. The
  EventSource stays subscribed for the whole index, so every repo's rows render.
- **Dashboard wizard indexing froze mid-extraction and never showed completion;
  rebuild appeared to idle-wait ~5 min** ([#5326](https://github.com/cajasmota/grafel/issues/5326)).
  Three independent defects on the rebuild/progress path:
  - **Progress (UI freeze):** the indexer emits its terminal `done`/`error`
    progress event exactly once, but the broker's fan-out is best-effort
    (drop-on-full) — under load that single event could be dropped, leaving the
    wizard SSE stream sending only heartbeats and the UI frozen on the last
    mid-extraction frame. The broker now **retains the last terminal event per
    group** and the `/api/index-progress/{group}` SSE handler replays it on
    connect and re-asserts it on each heartbeat, so the terminal state is
    **always** rendered (then `close`), even if the live event was dropped or
    the client connected late.
  - **Goroutine leak (the "~5 min wait"):** the Rebuild RPC's dead-man heartbeat
    ran `for range ticker.C` with no exit path — `time.Ticker.Stop()` does not
    close the channel, so the goroutine blocked forever, leaking one goroutine
    per Rebuild RPC. The ticker goroutine is now torn down via a stop channel
    when the result lands. (The result itself was already delivered promptly via
    a buffered channel; the "5m0s" log line was the dead-man heartbeat, not a
    timeout the result waited on.)
  - **Diagnosability:** when the stall detector fires it now logs a bounded
    (≤1 MiB), once-per-RPC full goroutine dump (`runtime.Stack(_, true)`) so the
    next stall is root-causable from the daemon log alone. The warning interval
    is overridable via `GRAFEL_STALL_WARN_INTERVAL`.
  - **Wizard UX (frontend follow-up):** the index wizard's terminal state was a
    *separate* source of truth from the now-fixed SSE feed — it came only from
    the job poller (`/api/v2/jobs/{id}`), so a rebuild could finish (`rebuild:
    done` in the daemon log) while the poller hadn't flipped, leaving the button
    stuck on "Indexing…". The wizard now reaches **terminal ("Done")** when
    *either* the job poller flips *or* the per-repo SSE feed shows every repo
    `done`/`error` (the icon, label, progress bar and close-guard all follow this
    effective status). The progress feed now renders **one row per repo** keyed
    by repo slug — a repo-level event and its redundant module-scoped duplicate
    collapse into a single row showing the latest status (no more stale "… module"
    rows frozen mid-extraction). The wizard modal is also slightly larger
    (`max-w-lg`, capped height with internal scroll, taller feed area) so the feed
    no longer scrolls cramped.
- `grafel_find_callers` / `find_callees` / `neighbors` now resolve an entity by
  name or qualified name (not only the opaque entity_id), returning
  disambiguation candidates when ambiguous instead of a hard `entity not found`
  — fixes a ~35% error rate on name-based calls
  ([#5314](https://github.com/cajasmota/grafel/issues/5314)).
- **Windows (end-to-end, from user feedback):** the MCP bridge now dials the
  daemon via `transport.Dial`, selecting the named pipe on Windows (was a
  hardcoded AF_UNIX `net.Dial("unix", ...)`), so `/mcp` can connect; the offline
  stub also always carries a valid `inputSchema` so a connection failure surfaces
  clearly instead of as a cryptic Zod rejection (Refs [#856](https://github.com/cajasmota/grafel/issues/856)).
- **Windows (end-to-end, from user feedback):** `grafel install` no longer aborts
  on a clean install — `schtasks` Unload now checks existence via the exit-code
  based `IsLoaded()` instead of matching English-only error text (worked around a
  localized "cannot find the file" on non-English Windows) (Refs [#856](https://github.com/cajasmota/grafel/issues/856)).
- **Windows (end-to-end, from user feedback):** the task user SID is now resolved
  via the native `os/user` API instead of shelling out to `whoami /user`, which a
  PATH-shadowing MSYS/Git Bash `whoami` could break; an empty SID degrades to a
  conditional (omitted) `<UserId>` instead of invalid task XML (Refs [#856](https://github.com/cajasmota/grafel/issues/856)).
- **Windows (end-to-end, from user feedback):** the watcher PID registry now
  creates its state directory before taking the lock, fixing a non-fatal
  "system cannot find the path specified" on a fresh `%AppData%\grafel`
  (Refs [#856](https://github.com/cajasmota/grafel/issues/856)).
- **Windows (end-to-end, from user feedback):** cross-repo link passes now fall
  back to copying `graph.json`/`graph.fb` into the staging dir when `os.Symlink`
  fails for lack of `SeCreateSymbolicLinkPrivilege`, so cross-repo edges are no
  longer silently 0 without Developer Mode/admin (Refs [#856](https://github.com/cajasmota/grafel/issues/856)).

### Added

- **Windows CMD installer + manual install path.** A new `install.bat` provides
  a non-PowerShell, non-admin one-line install for `cmd.exe`
  (`curl -fL …/install.bat -o "%TEMP%\grafel-install.bat" && "%TEMP%\grafel-install.bat"`);
  it mirrors `install.ps1` exactly (same `%USERPROFILE%\.grafel\bin` prefix,
  release asset names, `checksums.txt` SHA256 verification, and user-PATH append)
  using only Windows 10 1803+ built-ins. New `docs/install-windows-manual.md`
  documents a step-by-step locked-down/air-gapped install, and the install docs
  (README, `docs/install.md`, `docs/quickstart.md`) now present all three Windows
  methods — PowerShell, CMD, and manual
  (Fixes [#5319](https://github.com/cajasmota/grafel/issues/5319), Refs
  [#5318](https://github.com/cajasmota/grafel/issues/5318),
  [#856](https://github.com/cajasmota/grafel/issues/856)).

---

## [0.1.1] — 2026-06-20

### Fixed

- **Reindex is now resource-safe by default** ([#PR](https://github.com/cajasmota/grafel/pull/5310)) —
  background reindexing no longer saturates the host on a fresh `curl|bash`
  install. Previously the good behaviour was env-var-gated, so a clean install
  ran the indexer in-process at full host cores with no per-job CPU bound,
  spiking to 300–998% CPU for 10–20 min per `git push` (reported by a
  dogfooding user on a 36k-entity NestJS repo). Now, out of the box and with no
  env vars set:
  - **Subprocess indexer is default-on**, so each reindex runs in a short-lived
    child process bounded to `GRAFEL_EXTRACT_GOMAXPROCS` (default 2) cores —
    the host stays usable during heavy reindexing.
  - **The daemon's own in-process Go parallelism is capped at ~half the host
    cores** (`GRAFEL_DAEMON_GOMAXPROCS` default), bounding GC / algorithm
    passes / the in-process index fallback.
  - **Incremental reindex is default-on** (already shipped in #5231) so a
    single-file push patches the graph (~25× faster) instead of a full
    reindex.
  - **A Go soft memory limit is applied by default** (already shipped in
    #5237: ~40% of RAM, floor 2 GB, ceiling 2.5 GB) so swap can't saturate.

  All four remain overridable: the env vars are now **opt-OUT / tuning**
  overrides rather than enablers, so the existing production plist
  (`GRAFEL_SUBPROCESS_INDEXER=1`, `GRAFEL_INCREMENTAL_REINDEX=1`,
  `GRAFEL_DAEMON_MEMLIMIT_MB=1536`) is fully back-compatible.

### Added

- **`grafel_stats` exposes live reindex state** ([#PR](https://github.com/cajasmota/grafel/pull/5310)) —
  the tool now returns `is_indexing` (and, while indexing, `indexing_in_flight`
  + `indexing_started_at`) so a coordinator can query reindex state via MCP
  instead of polling `ps aux` for hot grafel processes.

---

## [0.1.0] — 2026-05-23 (Preview Release)

grafel's first pre-release. Active development; APIs, MCP tool names,
and graph schema may change between minor versions.

### Added

- **5-tier docgen ladder** — Tier 0 single-section (#1809), Tier 1
  single-page with contracts (#1812), Tier 2 5-page slice with cross-page
  contracts (#1814), Tier 3 single-repo with repo-level contracts (#1817),
  Tier 4 full multi-repo group (#1820).
- **LLM-mode emit-and-orchestrate workflow** — schema (#1816),
  `--llm-mode=emit` for Tier 0+1 (#1819), `--llm-mode=apply` (#1821),
  section cache (#1823), `generate-docs` skill Pass 20 orchestrator (#1822),
  user-facing docs (#1824).
- **MCP handshake cwd-gate** with `grafel_status` sentinel — tools list
  goes from 2,319 tokens → ~80 bytes for sessions outside any indexed group
  (#1769, #1783).
- **`grafel_stats --breakdown=unresolved_imports`** for import taxonomy
  (#1839).
- **Windows CGO experimental build workflow** (#1791) — produces
  `grafel.exe` artifact via MinGW on GitHub Actions.
- **Python `config_module` entity** for `settings.py`/`urls.py`/etc. (#1778).
- **Go extractor** — method-value references (#1789, #1792), intra-package
  bare-function calls (#1806, #1810), struct field references via receiver
  type chain (#1840, #1843).
- **Resolver** — platform-variant merging for `_unix.go` + `_windows.go`
  pairs (#1815).
- **`grafel_whoami` `wire_version` field** — returns `"0.1.0"`; bump per
  minor release (#1845).

### Changed

- **MCP param renames with compat aliases** — `grafel_find(query=)` was
  `question`; `grafel_get_source(entity_id=)` was `node_id` (#1790).
- **`grafel_stats.fidelity` scope narrowed to IMPORTS-only** matching
  `health-history.bug_rate` — value jumped from 0.656 to 0.943 on grafel
  (prior dilution was hiding ~5,000 import rescues) (#1842, #1844).
- **MCP positioning** embedded in handshake/CLAUDE.md/skills/docs: "pair MCP
  + grep" is the canonical position (#1838).
- **Default response limits shrunk**; token_budget enforcement extended;
  per-tool field elision (#1738, #1739, #1749, #1751).
- **`grafel_find` returns TOON format** with `id` as first column (#1737,
  #1743, #1761).
- **Entity-ID interning** in MCP responses (#1740, #1750).
- **Subgraph fold** — `get_subgraph` + `summarize_subgraph` →
  `grafel_subgraph(format=raw|markdown)` (#1754, #1764, #1768).
- Posix-only test files now carry `//go:build !windows` tags for Windows
  compatibility (#1787, #1804).

### Fixed

- **`grafel_get_source` 5 s fsevents wedge on macOS** — now 48 ms via
  `O_NONBLOCK` + `lstat` + semaphore (#1773, #1780).
- **`gitignore.go` Windows build** via `_unix`/`_windows` split (#1787).
- **`filepathBase` cross-platform path handling** (#1782).
- **Tier 4 LLM emit propagation** through Tier 2/3/4 opts + integration test
  (#1825, #1828, #1832).
- **Section guidance `graph_context`** now populated with
  `qualified_name`/`repo`/`source_window` (#1827, #1831).
- **MSYS2 path resolution** on Windows GHA runners (#1805, #1808).
- **grafel daemon test deadline** 2 s → 4 s + `test.yml` `-timeout 5m`
  (#1797, #1800).
- **`mat: zero length` panic guard** in `RunAlgorithmsWithOptions` (#1795,
  #1801).
- **`TestAxumE2E` skip-on-CI guard** (#1798, #1799).
- **166-file gofmt baseline** restored (#1786).
- **Vendored go-tree-sitter** with `//go:build cgo` patch (#1796, #1802).

### Known Limitations (roadmap for v0.2.0+)

- Resolver platform-variant merging works in unit tests but does not fully
  reflect in `find_callers` end-to-end (#1818 — separate bug class).
- Token-ratio recovery: iter6 had token ratio 6.83× vs 0.7× target — quality
  is strong, token economy needs work (#1807).
- Docgen `source_window` cwd-relative path bug — affects `BuildBundle` when
  called from a cwd outside the indexed repo root (#1834).
- Tier 4 emit one-off bundle/page count mismatch: one page in N produces no
  bundle (#1835).
- Go struct field extraction v2: cross-file struct types, interfaces,
  generics, embedded type promotion (#1840 — current v1 handles same-file
  struct fields only).
- `grafel_stats.fidelity` is IMPORTS-only; CALLS and REFERENCES
  improvements are not yet surfaced as metrics.
- JS/TS/Python sibling sweeps for the receiver-chain pattern that #1843 fixed
  in Go are queued for v0.2.0.

---

## [Unreleased] — v1.0-rc (2026-05-21, overnight session)

### Dashboard — new surfaces and nav

- **Cmd+K command palette** — fuzzy search all surfaces and actions from
  anywhere in the dashboard. (#1234, #1237)
- **Nav redesign** — 9 surfaces reorganised into Explore / Operate dropdown
  menus. (#1210, #1213)
- **MCP Activity surface (Jarvis)** — live log of every MCP tool call at
  `/mcp-activity`. (#1226, #1230)
- **Graph canvas Jarvis integration** — graph nodes pulse in real time when
  returned by an MCP tool. (#1225, #1232)
- **Quality surface** — orphan audit + recall measurement + health-score
  history trend line. (#1198, #1205, #1214, #1223)
- **Patterns surface** — list, edit, delete, and export agent-learned
  patterns. (#1189, #1197)
- **Settings surface** — theme, auto-update, telemetry, MCP config, log
  level, all persisted to `~/.grafel/settings.json`. (#1206, #1211)
- **System surface** — daemon control panel with restart, stop, and live
  log tail. (#1195, #1203)
- **Update surface** — version check, apply, and refresh-rules-lite. (#1199,
  #1208)
- **Diagnostics surface** — daemon + per-group health checks. (#1187, #1193)
- **Maintenance ops** — rebuild, reset, and cleanup actions per group or
  per repo in the dashboard. (#1200, #1204)
- **Graph thumbnail** — group cards on the landing page show a preview of the
  indexed graph. (#983, #1194)
- **Pending surface tiers** — tiered enrichment queue buckets
  (Critical / High / Medium / Low). (#1133, #1185)

### Paths v2

- `/api/paths/{group}` returns endpoint definitions grouped by
  `owning_backend`. (#1218, #1227)
- Orphan-caller detection at `/api/paths/{group}/orphan-callers`. (#1225)
- Duplicate path elimination (105 dupes removed, same endpoint with and
  without prefix). (#1124, #1163)
- XPath / XML namespace strings filtered from the Paths list. (#1125, #1160)
- DRF `ANY`-verb paths deduplicated via `http_endpoint_synthesis` entries.
  (#1126, #1158)

### Topology v2

- Rich per-topic detail panel v2 at
  `/api/topology/{group}/topic/{topicId}`. (#1141, #1178)
- `broker_canonical` + `owning_service` + `broker_groups` metadata. (#1139,
  #1175)
- Orphan publisher detector at `/api/topology/{group}/orphan-publishers`.
  (#1136, #1155)
- Orphan subscriber detector at `/api/topology/{group}/orphan-subscribers`.
  (#1137, #1159)
- Broker + service grouping headers in the list view. (#1142, #1176)
- Four-tab structure: All / Orphan Publishers / Orphan Subscribers /
  Scheduled Jobs. (#1140, #1168)
- `message_topic` YAML frontmatter wired into detail endpoint. (#1143, #1182)
- `Task`/`ScheduledJob` entity kinds bucketed into the Topology queue view.
  (#1116, #1122)

### Flows v2

- Per-flow React Flow DAG detail panel. (#1150, #1177)
- `process_flow` frontmatter wired into the flow detail panel. (#1152, #1181)
- Four-tab structure for Flows v2. (#1149, #1170)
- Entry-kind grouping headers in the flow list. (#1151, #1171)
- Entry-kind grouping metadata on `/api/flows/{group}` list endpoint. (#1148,
  #1167)
- Step-kind annotation and side-effect classification. (#1147, #1166)
- Truncated flow detector at `/api/flows/{group}/truncated`. (#1146, #1161)
- Dead-end flow classifier at `/api/flows/{group}/dead-ends`. (#1145, #1156)

### Real-time indexing progress (SSE)

- In-memory pub/sub broker for indexer progress events. (#1183, #1184)
- Internal `progress` package instruments the full indexer pipeline. (#1188)
- SSE endpoint `/api/index-progress` (all groups) and
  `/api/index-progress/{group}`. (#1186, #1190)
- `rebuild` CLI subscribes to broker for real-time terminal progress. (#1196,
  #1201)
- Dashboard `useIndexProgress` hook + `IndexingProgressModal`. (#1191, #1207)

### MCP — new tools and Jarvis broker

- MCP event broker + SSE endpoint `/api/mcp-activity/stream` (Phase 1).
  (#1215, #1222)
- 3 new HTTP endpoint tools: `grafel_endpoint_definitions`,
  `grafel_endpoint_calls`, `grafel_endpoint_stats`. (#1220, #1229)
- 13 additional tools for Topology v2, Flows v2, Quality, and graph
  traversal. (#1202, #1209)

### Entity model

- **`http_endpoint_definition` + `http_endpoint_call`** — `http_endpoint`
  split into two distinct entity kinds at the extractor layer. Legacy
  `http_endpoint` remains readable via compatibility helper. (#1217, #1233)
- Confidence score (0–100) added to every enrichment `Candidate`. (#1131,
  #1179)
- Enrichment model: 1 `EnrichmentTask` per entity with N pending actions.
  (#1134, #1165)
- Rebuild summary includes per-kind breakdown + color-coded percentage. (#1132,
  #1174)
- `describe_entity` emitter switched to research-driven positive selection;
  noise kinds excluded. (#1130, #1154, #1162, #1173)

### AGENTS.md auto-injection

- After every `grafel rebuild`, an Architecture Map block is written into
  `AGENTS.md` in each indexed repo. (#1216, #1221)

### Graph rendering

- 6-band zoom LoD (expanded from 3) for smoother level-of-detail
  progression. (#1108, #1192)
- Four rendering pathologies fixed: LoD threshold, Process pile-up, sizing,
  and hash labels. (#1121, #1127)
- Galaxy tune + 3-way color mode + Jarvis hook. (#1153, #1172)

### Extractors

- Stdlib placeholder elimination extended to PHP, Elixir, Clojure, and
  Erlang. (#1085, #1224)

### Docs / skills

- `generate-docs` skill: Topology v2 + Flows v2 frontmatter schemas and Pass
  14 validation. (#1212)

### Bug fixes

- Resolve leftover conflict marker from earlier rebase (build). (#1231)
- Merge conflict markers in `daemon.go` resolved. (#1228)
- `inferEntryKind` helper rename to resolve collision. (#1169)
- `actionEntry` field name consistency fix. (standalone commit)
- Unblock `npm run build` — fix tsc errors in test files. (#1180)

---

## Earlier sessions (2026-05-19 – 2026-05-20)

Covered by the session checkpoints in `MEMORY.md`. Key highlights:

- Daemon install-and-forget architecture (ADR-0017).
- `-81%` RSS via profile-driven fix (#637).
- Patterns chain: agent-learned patterns via ADR-0018.
- Cosmograph migration + tuning.
- 25+ new language extractors.
- Custom-extractor pipeline wiring (#1086).
- Lifecycle CLI (#1090).
- Near-zero Python orphans.
- Cross-repo functional testing.
- Paths v2 shipped (#1099, #1098, #1100, #1104).
- Unified enrichment schema (#1105).
- Graph hard-stop (#1101).
- Repo-first layout (#1106, not yet landed at session end).

---

_Older history is tracked in the [GitHub releases](https://github.com/cajasmota/grafel/releases)._
