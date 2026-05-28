// tRPC over both HTTP and WebSocket (#2906). The common production setup:
// queries/mutations over the standalone HTTP adapter, subscriptions over the
// WebSocket adapter, sharing the same router and HTTP server instance.
import { createHTTPServer } from '@trpc/server/adapters/standalone';
import { applyWSSHandler } from '@trpc/server/adapters/ws';
import { initTRPC } from '@trpc/server';
import { WebSocketServer } from 'ws';

const t = initTRPC.create();

export const appRouter = t.router({
  list: t.procedure.query(() => []),
  create: t.procedure.mutation(() => ({})),
  onTick: t.procedure.subscription(() => null),
});

const { server } = createHTTPServer({ router: appRouter });
const wss = new WebSocketServer({ server });
applyWSSHandler({ wss, router: appRouter });
server.listen(3000);
