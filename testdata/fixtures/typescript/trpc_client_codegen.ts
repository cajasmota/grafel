// Proving fixture for tRPC client_codegen (#2865). A tRPC client is not
// hand-written — it is INFERRED from the server's AppRouter type, so the
// typed proxy factory IS the generated client. The shipped trpc.yaml rule
// captures these factories + the inferRouterInputs/Outputs type helpers as
// Operation entities.
import { createTRPCClient, httpBatchLink } from '@trpc/client';
import { createTRPCProxyClient } from '@trpc/client';
import { createTRPCReact } from '@trpc/react-query';
import type { inferRouterInputs, inferRouterOutputs } from '@trpc/server';
import type { AppRouter } from '../server/router';

export const client = createTRPCClient<AppRouter>({
  links: [httpBatchLink({ url: '/api/trpc' })],
});

export const proxy = createTRPCProxyClient<AppRouter>({
  links: [httpBatchLink({ url: '/api/trpc' })],
});

export const trpc = createTRPCReact<AppRouter>();

export type RouterInputs = inferRouterInputs<AppRouter>;
export type RouterOutputs = inferRouterOutputs<AppRouter>;
