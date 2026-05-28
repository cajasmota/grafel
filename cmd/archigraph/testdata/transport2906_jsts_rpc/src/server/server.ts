// tRPC standalone HTTP + WebSocket server. Both adapters are wired in this
// module against the same router, so queries/mutations are reachable over
// HTTP and the subscription over WS — the procedures synthesised here are
// stamped transport=http+ws.
import { createHTTPServer } from "@trpc/server/adapters/standalone";
import { applyWSSHandler } from "@trpc/server/adapters/ws";
import { initTRPC } from "@trpc/server";
import { WebSocketServer } from "ws";
import { z } from "zod";

const t = initTRPC.create();

const appRouter = t.router({
  health: t.procedure.query(() => ({ ok: true })),
  createPost: t.procedure
    .input(z.object({ title: z.string() }))
    .mutation(({ input }) => ({ id: 1, title: input.title })),
  onPost: t.procedure.subscription(() => null),
});

const { server, listen } = createHTTPServer({ router: appRouter });
const wss = new WebSocketServer({ server });
applyWSSHandler({ wss, router: appRouter });
listen(3000);
