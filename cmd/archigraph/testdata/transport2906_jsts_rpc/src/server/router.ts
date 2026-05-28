// Domain router — transport-agnostic, exported for both the HTTP and the
// WebSocket entry points. This is the idiomatic split: the router knows
// nothing about how it is served.
import { initTRPC } from "@trpc/server";
import { z } from "zod";

const t = initTRPC.create();

export const appRouter = t.router({
  health: t.procedure.query(() => ({ ok: true })),
  createPost: t.procedure
    .input(z.object({ title: z.string() }))
    .mutation(({ input }) => ({ id: 1, title: input.title })),
  onPost: t.procedure.subscription(() => null),
});

export type AppRouter = typeof appRouter;
