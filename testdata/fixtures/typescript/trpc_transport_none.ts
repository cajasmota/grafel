// tRPC router defined in a standalone module with NO adapter (#2906).
// This is the idiomatic split where the router type is exported for the
// client and the transport is wired elsewhere. The transport binding is not
// visible here, so the synthesizer leaves `transport` unset (honest "unknown
// in this module" rather than a guessed default).
import { initTRPC } from '@trpc/server';

const t = initTRPC.create();

export const appRouter = t.router({
  list: t.procedure.query(() => []),
});

export type AppRouter = typeof appRouter;
