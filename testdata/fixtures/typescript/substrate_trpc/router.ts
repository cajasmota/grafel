// tRPC router fixture — proves substrate sniffers for the tRPC framework
// (issue #3057).  Hand-written; no node_modules.
//
// tRPC procedures receive validated `input` (zod-typed) — the primary
// user-input channel — plus a typed `ctx` object that carries the HTTP
// request.  The jsts substrate sniffers recognise ctx.request.body /
// ctx.req.body shapes via jstsSourceReqRe; the `input` parameter itself
// is NOT currently detected (Type-B gap: input is post-validation, not a
// raw taint source in the jsts sniffer model).
//
// This fixture proves the capabilities that DO fire via ctx-shaped access.
import { initTRPC } from '@trpc/server';
import { z } from 'zod';
import DOMPurify from 'dompurify';
import { db } from '../lib/db';

const API_URL = 'https://trpc.example.com';
const SECRET = process.env.TRPC_SECRET ?? 'dev-only';

type Context = {
  req: { body: unknown; query: Record<string, string>; params: Record<string, string> };
};

const t = initTRPC.context<Context>().create();

export const appRouter = t.router({
  getUser: t.procedure
    .input(z.object({ id: z.string() }))
    .query(async ({ input, ctx }) => {
      // Source: ctx.req carries the raw HTTP request (jstsSourceReqRe fires).
      const body = ctx.req.body;
      const q = ctx.req.query;
      const userId = ctx.req.params.id;

      // Sink: raw SQL with template-string interpolation.
      const row = db.query(`SELECT * FROM users WHERE id = ${userId}`);

      // Sanitizer: DOMPurify.sanitize.
      const safeInput = DOMPurify.sanitize(String(q.name ?? ''));

      return row;
    }),

  createUser: t.procedure
    .input(z.object({ name: z.string(), script: z.string().optional() }))
    .mutation(async ({ input, ctx }) => {
      const request = ctx.req;
      const body = request.body;

      // Sink: eval — dynamic execution.
      eval(String(body));

      return { ok: true };
    }),
});

export type AppRouter = typeof appRouter;
