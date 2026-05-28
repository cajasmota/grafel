// Restify backend-HTTP log_extraction fixture (#2905).
// Restify embeds bunyan as its server logger (`server.log` is a bunyan
// instance) and apps construct one explicitly. The observability extractor
// attributes a bunyan log signal.
import restify from "restify";
import bunyan from "bunyan";

const logger = bunyan.createLogger({ name: "api", level: "info" });
const server = restify.createServer({ log: logger });

server.get("/health", (req, res, next) => {
  logger.info({ path: req.path() }, "health check");
  res.send({ ok: true });
  next();
});
