// Fastify route registration — before #2678 these endpoints were NOT
// emitted at all (the Express synthesizer's receiver allowlist excludes
// "fastify"). After the fix:
//   - the endpoints are emitted by synthesizeFastify,
//   - and the resolve pass rewrites source_file to handlers.ts.

import Fastify from "fastify";
import { listProducts, createProduct } from "./handlers";

const fastify = Fastify();

fastify.get("/products", listProducts);
fastify.post("/products", createProduct);

export default fastify;
