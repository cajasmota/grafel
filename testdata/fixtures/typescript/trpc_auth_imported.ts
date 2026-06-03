// Proving fixture for the cross-file protectedProcedure case (#4041). The
// auth-enforcing builder is defined in another module (`../trpc`) and merely
// IMPORTED here. The binding is name-based, so the procedure is credited at
// MEDIUM confidence — honest about the heuristic.
import { router, publicProcedure, protectedProcedure } from '../trpc';
import { z } from 'zod';

export const userRouter = router({
  // AUTH (medium): imported protectedProcedure, definition not in this file.
  me: protectedProcedure.query(({ ctx }) => ctx.user),

  // PUBLIC: imported publicProcedure.
  signup: publicProcedure
    .input(z.object({ email: z.string() }))
    .mutation(({ input }) => createUser(input)),
});
