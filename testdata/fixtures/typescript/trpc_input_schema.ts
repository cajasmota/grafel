// Proving fixture for tRPC schema_extraction (#2865). The synthesizer emits
// one http_endpoint_definition per leaf procedure; the schema pass
// (internal/engine/http_endpoint_trpc_schema.go) recovers each procedure's
// `.input(z.object({…}))` validator and stamps input_schema /
// input_schema_lib / has_input_schema on the matching endpoint, keyed on the
// dotted procedure path.
import { initTRPC } from '@trpc/server';
import { z } from 'zod';

const t = initTRPC.create();

export const appRouter = t.router({
  getUser: t.procedure
    .input(z.object({ id: z.string().uuid() }))
    .query(({ input }) => findUser(input.id)),

  createUser: t.procedure
    .input(z.object({ name: z.string(), email: z.string().email() }))
    .mutation(({ input }) => createUser(input)),

  // No input — left unstamped (procedure takes no validated input).
  listUsers: t.procedure.query(() => listUsers()),
});
