// Koa backend-HTTP log_extraction fixture (#2905).
// Koa apps wire structured logging via the `koa-pino-logger` middleware
// (pino under the hood). The observability extractor attributes a pino log
// signal from the import.
import Koa from "koa";
import pino from "pino";

const logger = pino({ level: "info" });
const app = new Koa();

app.use(async (ctx, next) => {
  logger.info({ path: ctx.path }, "request");
  await next();
});
