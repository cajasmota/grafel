// Fastify handlers — referenced by routes.ts.

import type { FastifyReply, FastifyRequest } from "fastify";

export async function listProducts(
  req: FastifyRequest,
  reply: FastifyReply,
): Promise<void> {
  reply.send([{ sku: "abc" }]);
}

export async function createProduct(
  req: FastifyRequest,
  reply: FastifyReply,
): Promise<void> {
  reply.code(201).send({ ok: true });
}
