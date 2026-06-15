# Generic Node.js convention

Required reading: `_graph-searchability.md`.

Use for Node.js repos that do not match a more specific convention (`react.md`, `vite.md`). Examples: an Express/Koa/Fastify backend, a CLI built with Commander/Yargs, a library, a Lambda handler bundle.

## Public surface

1. **Package exports** — `package.json` `exports` (or `main`/`module`/`types` for legacy packages).
2. **HTTP routes** — every route registration on the server framework.
3. **CLI entry points** — `package.json` `bin` field.
4. **Lambda / FaaS handlers** — referenced by handler-string in IaC.

## Module shape

```
src/
  <feature>/
    index.ts
    <module>.ts
  index.ts          # barrel for the package
package.json
tsconfig.json
```

## Entry points (Pass 3)

- `package.json` `main` / `module` / `bin`.
- The server framework root (e.g., `src/server.ts` instantiating `express()`).
- `tsconfig.json` — module resolution + path aliases.

## Dynamic edges (Pass 4)

- **Middleware registration** — `app.use(...)` / `fastify.register(...)` is order-sensitive.
- **Event emitters** — `EventEmitter`-based pub/sub couples emitter and listener by string event name.
- **Dynamic imports** — `await import('./x')` is a runtime edge.
- **Module-augmentation** — TypeScript module augmentation adds methods invisibly. Note any third-party types extended.

## Deployment signals (Pass 5)

- `Dockerfile` (multi-stage with `node:<v>-alpine` final image is common).
- `package.json` `engines.node`.
- CI build / publish targets.

## Manifest files

`package.json` plus exactly one of `pnpm-lock.yaml`, `package-lock.json`, `yarn.lock`. Note the package manager explicitly.

## Cross-cutting pitfalls

- **CommonJS vs ESM** — interop is fraught. Document the choice and any `.cjs` / `.mjs` exceptions.
- **Process signals and graceful shutdown** — production servers must handle `SIGTERM`. Document the shutdown path.
- **`unhandledRejection`** — default behavior changed in recent Node versions; document the policy.

## Cross-repo signals

Outbound HTTP via `node-fetch` / `undici` / `axios`; outbound message bus via cloud SDKs. Library consumers pin a version range; that's a cross-repo edge but a weak one.
