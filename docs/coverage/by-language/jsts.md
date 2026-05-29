<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# JS/TS

**Frameworks**: 30 · **Tools**: 21 · **ORMs**: 18 · **Other**: 2

Back to [summary](../summary.md).

### Legend

Each group column shows `glyph covered/applicable` — **covered** = capabilities with extraction, **applicable** = covered + missing (not-applicable capabilities are excluded from both). The glyph is the group's **support level**:

| Glyph | Level | Meaning |
|---|---|---|
| ✅ | **Comprehensive** | every applicable capability is `full` — fixture-proven, resolves the general case |
| 🟢 | **Supported** | every applicable capability is extracted; some only *heuristically* (detected by pattern, not full AST/data-flow resolution) |
| 🟡 | **Partial** | some capabilities extracted, some still missing |
| 🔴 | **Not extracted** | nothing extracted yet |
| — | **N/A** | capability does not apply to this framework |

Examples: `🟢 20/20` = fully supported, some capabilities heuristic · `🟡 12/20` = 8 not yet extracted. Detail pages use the same palette **per cell** (✅ full · 🟢 heuristic/partial · 🔴 missing · — n/a).

## Frameworks


### Backend HTTP

| Name | Routing | Auth | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|---|
| [AdonisJS](../detail/lang.jsts.framework.adonisjs.md) | 🟢 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟢 20/20 | 🟢 6/6 | |
| [Express](../detail/lang.jsts.framework.express.md) | ✅ 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Fastify](../detail/lang.jsts.framework.fastify.md) | ✅ 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Feathers](../detail/lang.jsts.framework.feathers.md) | 🟢 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Hapi](../detail/lang.jsts.framework.hapi.md) | 🟢 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Hono](../detail/lang.jsts.framework.hono.md) | 🟢 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Koa](../detail/lang.jsts.framework.koa.md) | 🟢 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Marble.js](../detail/lang.jsts.framework.marblejs.md) | 🟢 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [NestJS](../detail/lang.jsts.framework.nestjs.md) | 🟢 3/3 | ✅ 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Polka](../detail/lang.jsts.framework.polka.md) | 🟢 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Restify](../detail/lang.jsts.framework.restify.md) | 🟢 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Sails](../detail/lang.jsts.framework.sails.md) | 🟢 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟢 20/20 | 🟢 8/8 | |


### UI Frontend

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Angular](../detail/lang.jsts.framework.angular.md) | ✅ 3/3 | ✅ 1/1 | 🟢 21/21 | ✅ 17/17 | |
| [React](../detail/lang.jsts.framework.react.md) | ✅ 3/3 | ✅ 1/1 | 🟢 21/21 | ✅ 21/21 | |
| [Svelte](../detail/lang.jsts.framework.svelte.md) | ✅ 3/3 | ✅ 1/1 | 🟢 21/21 | 🟢 17/17 | |
| [Vue](../detail/lang.jsts.framework.vue.md) | ✅ 3/3 | ✅ 1/1 | 🟢 21/21 | 🟢 19/19 | |


### Meta Framework

| Name | Routing | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|
| [Astro](../detail/lang.jsts.framework.astro.md) | ✅ 2/2 | ✅ 3/3 | ✅ 1/1 | 🟢 21/21 | ✅ 7/7 | |
| [Gatsby](../detail/lang.jsts.framework.gatsby.md) | ✅ 2/2 | ✅ 3/3 | ✅ 1/1 | 🟢 21/21 | ✅ 8/8 | |
| [Next.js API Routes / App Router](../detail/lang.jsts.framework.next-api.md) | ✅ 2/2 | ✅ 3/3 | ✅ 1/1 | 🟢 21/21 | ✅ 11/11 | |
| [Nuxt](../detail/lang.jsts.framework.nuxt.md) | ✅ 2/2 | ✅ 3/3 | ✅ 1/1 | 🟢 21/21 | ✅ 8/8 | |
| [Remix](../detail/lang.jsts.framework.remix.md) | ✅ 2/2 | ✅ 3/3 | ✅ 1/1 | 🟢 21/21 | ✅ 8/8 | |
| [SvelteKit](../detail/lang.jsts.framework.sveltekit.md) | ✅ 2/2 | ✅ 3/3 | ✅ 1/1 | 🟢 21/21 | ✅ 8/8 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Expo](../detail/lang.jsts.framework.expo.md) | ✅ 3/3 | ✅ 1/1 | 🟢 20/20 | ✅ 13/13 | |
| [Ionic](../detail/lang.jsts.framework.ionic.md) | ✅ 3/3 | ✅ 1/1 | 🟢 20/20 | ✅ 10/10 | |
| [NativeScript](../detail/lang.jsts.framework.nativescript.md) | ✅ 3/3 | ✅ 1/1 | 🟢 20/20 | ✅ 10/10 | |
| [React Native](../detail/lang.jsts.framework.react-native.md) | ✅ 3/3 | ✅ 1/1 | 🟢 20/20 | ✅ 19/19 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Electron](../detail/lang.jsts.framework.electron.md) | 🟢 10/10 | ✅ 3/3 | |


### RPC Framework

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [GraphQL Resolvers (Apollo Server / GraphQL Yoga / etc.)](../detail/lang.jsts.framework.graphql-resolvers.md) | 🟢 21/21 | ✅ 3/3 | |
| [tRPC](../detail/lang.jsts.framework.trpc.md) | 🟢 21/21 | ✅ 4/4 | |


### AI Integration

| Name | Other capabilities | Notes |
|---|---|---|
| [LangChain.js](../detail/lang.jsts.framework.langchain.md) | 🟢 4/4 | |


## Tools

| Name | Dependency graph | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|
| [AVA](../detail/test.ava.md) | ✅ | — | — | ✅ | |
| [Bun (runtime + manager)](../detail/build.bun.md) | ✅ | — | — | ✅ | |
| [Cypress](../detail/test.cypress.md) | ✅ | — | — | ✅ | |
| [Jasmine](../detail/test.jasmine.md) | ✅ | — | — | ✅ | |
| [Jest](../detail/test.jest.md) | ✅ | — | — | ✅ | |
| [Lerna](../detail/build.lerna.md) | ✅ | — | — | ✅ | |
| [Mocha](../detail/test.mocha.md) | ✅ | — | — | ✅ | |
| [Nx (monorepo)](../detail/build.nx.md) | ✅ | — | — | ✅ | |
| [Parcel](../detail/build.parcel.md) | ✅ | — | — | ✅ | |
| [Playwright](../detail/test.playwright.md) | ✅ | — | — | ✅ | |
| [Rollup](../detail/build.rollup.md) | ✅ | — | — | ✅ | |
| [Turborepo](../detail/build.turborepo.md) | ✅ | — | — | ✅ | |
| [Vite](../detail/build.vite.md) | ✅ | — | — | ✅ | |
| [Vitest](../detail/test.vitest.md) | ✅ | — | — | ✅ | |
| [Webpack](../detail/build.webpack.md) | ✅ | — | — | ✅ | |
| [Yarn](../detail/build.yarn.md) | ✅ | — | — | ✅ | |
| [esbuild](../detail/build.esbuild.md) | ✅ | — | — | ✅ | |
| [npm](../detail/build.npm.md) | ✅ | — | — | ✅ | |
| [package.json (npm/yarn/pnpm)](../detail/pkg.npm.md) | — | ✅ | ✅ | — | |
| [pnpm](../detail/build.pnpm.md) | ✅ | — | — | ✅ | |
| [tap / node:test](../detail/test.tap.md) | ✅ | — | — | ✅ | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [@elastic/elasticsearch](../detail/lang.jsts.driver.elastic.md) | ✅ 1/1 | |
| [AWS SDK DynamoDB (JS)](../detail/lang.jsts.driver.dynamodb.md) | ✅ 1/1 | |
| [Drizzle](../detail/lang.jsts.orm.drizzle.md) | ✅ 7/7 | |
| [Knex (query builder)](../detail/lang.jsts.orm.knex.md) | ✅ 6/6 | |
| [MikroORM](../detail/lang.jsts.orm.mikro-orm.md) | 🟢 8/8 | |
| [MongoDB Node.js driver](../detail/lang.jsts.driver.mongodb.md) | ✅ 1/1 | |
| [Mongoose](../detail/lang.jsts.orm.mongoose.md) | 🟢 5/5 | |
| [Objection.js](../detail/lang.jsts.orm.objection.md) | 🟢 8/8 | |
| [Prisma](../detail/lang.jsts.orm.prisma.md) | ✅ 7/7 | |
| [Sequelize](../detail/lang.jsts.orm.sequelize.md) | 🟢 8/8 | |
| [TypeORM](../detail/lang.jsts.orm.typeorm.md) | 🟢 8/8 | |
| [better-sqlite3 / sqlite3](../detail/lang.jsts.driver.sqlite.md) | ✅ 1/1 | |
| [cassandra-driver (JS)](../detail/lang.jsts.driver.cassandra.md) | ✅ 1/1 | |
| [ioredis / node-redis](../detail/lang.jsts.driver.redis.md) | ✅ 1/1 | |
| [mysql / mysql2](../detail/lang.jsts.driver.mysql.md) | ✅ 1/1 | |
| [neo4j-driver (JS)](../detail/lang.jsts.driver.neo4j.md) | ✅ 1/1 | |
| [node-postgres / pg](../detail/lang.jsts.driver.postgres.md) | ✅ 1/1 | |
| [supabase-js](../detail/lang.jsts.driver.supabase.md) | ✅ 1/1 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [BullMQ / bull (Node task queue)](../detail/msg.bullmq.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [tsconfig.json](../detail/config.tsconfig.md) | [platform](../by-category/platform.md) | ✅ | |
