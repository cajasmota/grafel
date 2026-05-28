// Fastify backend-HTTP log_extraction fixture (#2905).
// Fastify ships pino as its first-class logger; apps enable it via the
// `logger` option and call `request.log`/`app.log`. The observability
// extractor attributes a pino log signal.
import Fastify from "fastify";
import pino from "pino";

const app = Fastify({ logger: pino({ level: "info" }) });

app.get("/health", async (request, reply) => {
  request.log.info({ path: request.url }, "health check");
  return { ok: true };
});
