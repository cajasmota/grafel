// tRPC over WebSocket — applyWSSHandler from @trpc/server/adapters/ws (#2906).
// Subscriptions are served over a WebSocket transport; no HTTP adapter is
// wired in this module.
import { applyWSSHandler } from '@trpc/server/adapters/ws';
import { initTRPC } from '@trpc/server';
import { WebSocketServer } from 'ws';

const t = initTRPC.create();

export const appRouter = t.router({
  onTick: t.procedure.subscription(() => null),
});

const wss = new WebSocketServer({ port: 3001 });
applyWSSHandler({ wss, router: appRouter });
