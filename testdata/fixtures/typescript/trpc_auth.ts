// Proving fixture for tRPC middleware / protectedProcedure auth detection
// (#4041, epic #3872). The synthesizer (#2693) emits one
// http_endpoint_definition per leaf procedure; the auth pass
// (internal/engine/http_endpoint_trpc_auth.go) re-walks the routers and stamps
// auth_required / auth_method=trpc_middleware / auth_middleware on each
// procedure built from an auth-enforcing middleware or protectedProcedure
// builder, keyed on the dotted procedure path.
//
// Auth-enforcing middleware: throws TRPCError UNAUTHORIZED when ctx.user is
// absent. The protectedProcedure builder composes it via `.use(isAuthed)`.
import { initTRPC, TRPCError } from '@trpc/server';
import { z } from 'zod';

const t = initTRPC.context().create();

// Auth-enforcing: throws TRPCError UNAUTHORIZED on a missing principal.
const isAuthed = t.middleware(({ ctx, next }) => {
  if (!ctx.user) {
    throw new TRPCError({ code: 'UNAUTHORIZED' });
  }
  return next({ ctx: { user: ctx.user } });
});

// NOT auth — pure logging middleware (no throw, no principal check). A
// procedure built from this alone must NOT be credited with auth.
const logger = t.middleware(({ path, next }) => {
  console.log('call', path);
  return next();
});

const publicProcedure = t.procedure;
const loggedProcedure = t.procedure.use(logger);
const protectedProcedure = t.procedure.use(isAuthed);

export const appRouter = t.router({
  // AUTH: built from protectedProcedure (→ isAuthed → TRPCError UNAUTHORIZED).
  getUser: protectedProcedure
    .input(z.object({ id: z.string() }))
    .query(({ ctx }) => findUser(ctx.user.id)),

  // PUBLIC: built from publicProcedure — no auth.
  listUsers: publicProcedure.query(() => listUsers()),

  // PUBLIC: built from a non-auth (logging) middleware — no auth.
  ping: loggedProcedure.query(() => 'pong'),

  // AUTH (inline): an inline `.use(...)` auth arrow guarding ctx.session,
  // throwing on absence. Credited without a named protected builder.
  deleteAll: t.procedure
    .use(({ ctx, next }) => {
      if (!ctx.session) {
        throw new TRPCError({ code: 'FORBIDDEN' });
      }
      return next();
    })
    .mutation(() => deleteEverything()),
});
