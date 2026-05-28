// tRPC over HTTP — standalone request/response adapter (#2906).
// The router is transport-agnostic; createHTTPServer from
// @trpc/server/adapters/standalone binds it to an HTTP transport.
import { createHTTPServer } from '@trpc/server/adapters/standalone';
import { initTRPC } from '@trpc/server';

const t = initTRPC.create();

export const appRouter = t.router({
  list: t.procedure.query(() => [{ id: 1 }]),
  create: t.procedure.mutation(({ input }) => ({ ok: true, input })),
});

createHTTPServer({ router: appRouter }).listen(3000);
