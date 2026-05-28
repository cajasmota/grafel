// hapi backend-HTTP log_extraction fixture (#2905).
// hapi commonly wires the `hapi-pino` plugin for request logging (pino
// under the hood). The observability extractor attributes a pino log signal.
import Hapi from "@hapi/hapi";
import pino from "pino";

const logger = pino({ level: "info" });
const server = Hapi.server({ port: 3000 });

server.route({
  method: "GET",
  path: "/health",
  handler: (request, h) => {
    logger.info({ path: request.path }, "health check");
    return { ok: true };
  },
});
