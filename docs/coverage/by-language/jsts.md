<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# JS/TS

**Frameworks**: 32 · **Tools**: 21 · **ORMs**: 18 · **Other**: 4

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
| [AdonisJS](../detail/lang.jsts.framework.adonisjs.md) | 🟡 3/5 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/24 | 🟡 6/11 | |
| [Express](../detail/lang.jsts.framework.express.md) | ✅ 5/5 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 23/24 | 🟡 9/12 | |
| [Fastify](../detail/lang.jsts.framework.fastify.md) | 🟡 3/5 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 23/24 | 🟡 7/12 | |
| [Feathers](../detail/lang.jsts.framework.feathers.md) | 🟡 3/5 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Hapi](../detail/lang.jsts.framework.hapi.md) | 🟡 3/5 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Hono](../detail/lang.jsts.framework.hono.md) | 🟡 3/5 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 23/24 | 🟡 7/12 | |
| [Koa](../detail/lang.jsts.framework.koa.md) | 🟡 3/5 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 23/24 | 🟡 9/12 | |
| [Marble.js](../detail/lang.jsts.framework.marblejs.md) | 🟡 3/5 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [NestJS](../detail/lang.jsts.framework.nestjs.md) | 🟡 3/5 | ✅ 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 22/24 | 🟡 10/12 | |
| [Polka](../detail/lang.jsts.framework.polka.md) | 🟡 3/5 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Pothos (GraphQL)](../detail/lang.jsts.framework.pothos.md) | 🟡 2/4 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 1/24 | 🔴 0/12 | |
| [Restify](../detail/lang.jsts.framework.restify.md) | 🟡 3/5 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Sails](../detail/lang.jsts.framework.sails.md) | 🟡 3/5 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/24 | 🟡 8/13 | |
| [TypeGraphQL (GraphQL)](../detail/lang.jsts.framework.type-graphql.md) | 🟡 3/5 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 1/24 | 🔴 0/12 | |


### UI Frontend

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Angular](../detail/lang.jsts.framework.angular.md) | ✅ 3/3 | ✅ 1/1 | 🟡 23/24 | ✅ 17/17 | |
| [React](../detail/lang.jsts.framework.react.md) | ✅ 3/3 | ✅ 1/1 | 🟡 23/24 | ✅ 21/21 | |
| [Svelte](../detail/lang.jsts.framework.svelte.md) | ✅ 3/3 | ✅ 1/1 | 🟡 23/24 | 🟢 17/17 | |
| [Vue](../detail/lang.jsts.framework.vue.md) | ✅ 3/3 | ✅ 1/1 | 🟡 23/24 | 🟢 19/19 | |


### Meta Framework

| Name | Routing | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|
| [Astro](../detail/lang.jsts.framework.astro.md) | ✅ 2/2 | ✅ 3/3 | ✅ 1/1 | 🟡 23/24 | ✅ 7/7 | |
| [Gatsby](../detail/lang.jsts.framework.gatsby.md) | ✅ 2/2 | ✅ 3/3 | ✅ 1/1 | 🟡 23/24 | ✅ 8/8 | |
| [Next.js API Routes / App Router](../detail/lang.jsts.framework.next-api.md) | ✅ 2/2 | ✅ 3/3 | ✅ 1/1 | 🟢 24/24 | ✅ 11/11 | |
| [Nuxt](../detail/lang.jsts.framework.nuxt.md) | ✅ 2/2 | ✅ 3/3 | ✅ 1/1 | 🟡 23/24 | ✅ 8/8 | |
| [Remix](../detail/lang.jsts.framework.remix.md) | ✅ 2/2 | ✅ 3/3 | ✅ 1/1 | 🟡 23/24 | ✅ 8/8 | |
| [SvelteKit](../detail/lang.jsts.framework.sveltekit.md) | ✅ 2/2 | ✅ 3/3 | ✅ 1/1 | 🟡 23/24 | ✅ 8/8 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Expo](../detail/lang.jsts.framework.expo.md) | ✅ 3/3 | ✅ 1/1 | 🟡 22/23 | ✅ 14/14 | |
| [Ionic](../detail/lang.jsts.framework.ionic.md) | ✅ 3/3 | ✅ 1/1 | 🟡 22/23 | ✅ 10/10 | |
| [NativeScript](../detail/lang.jsts.framework.nativescript.md) | ✅ 3/3 | ✅ 1/1 | 🟡 22/23 | ✅ 10/10 | |
| [React Native](../detail/lang.jsts.framework.react-native.md) | ✅ 3/3 | ✅ 1/1 | 🟡 22/23 | ✅ 20/20 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Electron](../detail/lang.jsts.framework.electron.md) | 🟡 12/13 | ✅ 3/3 | |


### RPC Framework

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [GraphQL Resolvers (Apollo Server / GraphQL Yoga / etc.)](../detail/lang.jsts.framework.graphql-resolvers.md) | 🟡 23/24 | 🟢 6/6 | |
| [tRPC](../detail/lang.jsts.framework.trpc.md) | 🟡 23/24 | ✅ 4/4 | |


### AI Integration

| Name | Other capabilities | Notes |
|---|---|---|
| [LangChain.js](../detail/lang.jsts.framework.langchain.md) | 🟢 4/4 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [AVA](../detail/test.ava.md) | ✅ | — | — | — | ✅ | |
| [Bun (runtime + manager)](../detail/build.bun.md) | ✅ | — | — | — | ✅ | |
| [Cypress](../detail/test.cypress.md) | ✅ | — | — | — | ✅ | |
| [Jasmine](../detail/test.jasmine.md) | ✅ | — | — | — | ✅ | |
| [Jest](../detail/test.jest.md) | ✅ | — | — | — | ✅ | |
| [Lerna](../detail/build.lerna.md) | ✅ | — | — | — | ✅ | |
| [Mocha](../detail/test.mocha.md) | ✅ | — | — | — | ✅ | |
| [Nx (monorepo)](../detail/build.nx.md) | ✅ | — | — | — | ✅ | |
| [Parcel](../detail/build.parcel.md) | ✅ | — | — | — | ✅ | |
| [Playwright](../detail/test.playwright.md) | ✅ | — | — | — | ✅ | |
| [Rollup](../detail/build.rollup.md) | ✅ | — | — | — | ✅ | |
| [Turborepo](../detail/build.turborepo.md) | ✅ | — | — | — | ✅ | |
| [Vite](../detail/build.vite.md) | ✅ | — | — | — | ✅ | |
| [Vitest](../detail/test.vitest.md) | ✅ | — | — | — | ✅ | |
| [Webpack](../detail/build.webpack.md) | ✅ | — | — | — | ✅ | |
| [Yarn](../detail/build.yarn.md) | ✅ | — | — | — | ✅ | |
| [esbuild](../detail/build.esbuild.md) | ✅ | — | — | — | ✅ | |
| [npm](../detail/build.npm.md) | ✅ | — | — | — | ✅ | |
| [package.json (npm/yarn/pnpm)](../detail/pkg.npm.md) | — | — | ✅ | ✅ | — | |
| [pnpm](../detail/build.pnpm.md) | ✅ | — | — | — | ✅ | |
| [tap / node:test](../detail/test.tap.md) | ✅ | — | — | — | ✅ | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [@elastic/elasticsearch](../detail/lang.jsts.driver.elastic.md) | 🟡 1/4 | |
| [AWS SDK DynamoDB (JS)](../detail/lang.jsts.driver.dynamodb.md) | 🟡 1/4 | |
| [Drizzle](../detail/lang.jsts.orm.drizzle.md) | 🟡 7/10 | |
| [Knex (query builder)](../detail/lang.jsts.orm.knex.md) | 🟡 8/9 | |
| [MikroORM](../detail/lang.jsts.orm.mikro-orm.md) | 🟡 8/11 | |
| [MongoDB Node.js driver](../detail/lang.jsts.driver.mongodb.md) | 🟡 1/4 | |
| [Mongoose](../detail/lang.jsts.orm.mongoose.md) | 🟡 5/8 | |
| [Objection.js](../detail/lang.jsts.orm.objection.md) | 🟡 8/11 | |
| [Prisma](../detail/lang.jsts.orm.prisma.md) | 🟡 8/10 | |
| [Sequelize](../detail/lang.jsts.orm.sequelize.md) | 🟢 11/11 | |
| [TypeORM](../detail/lang.jsts.orm.typeorm.md) | 🟢 11/11 | |
| [better-sqlite3 / sqlite3](../detail/lang.jsts.driver.sqlite.md) | 🟡 1/4 | |
| [cassandra-driver (JS)](../detail/lang.jsts.driver.cassandra.md) | 🟡 1/4 | |
| [ioredis / node-redis](../detail/lang.jsts.driver.redis.md) | 🟡 1/4 | |
| [mysql / mysql2](../detail/lang.jsts.driver.mysql.md) | 🟡 1/4 | |
| [neo4j-driver (JS) / neogma OGM](../detail/lang.jsts.driver.neo4j.md) | 🟡 4/7 | |
| [node-postgres / pg](../detail/lang.jsts.driver.postgres.md) | 🟡 1/4 | |
| [supabase-js](../detail/lang.jsts.driver.supabase.md) | 🟡 1/4 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [BullMQ / bull (Node task queue)](../detail/msg.bullmq.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [ORM model lifecycle-hook → handler TRIGGERS (TypeORM, Sequelize, Mongoose)](../detail/msg.orm-lifecycle-hooks-jsts.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [node-schedule (Node scheduled jobs)](../detail/msg.node-schedule.md) | [message_broker](../by-category/message_broker.md) | 🟢 | |
| [tsconfig.json](../detail/config.tsconfig.md) | [platform](../by-category/platform.md) | ✅ | |
