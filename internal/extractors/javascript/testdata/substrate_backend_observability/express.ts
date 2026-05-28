// Express backend-HTTP log_extraction fixture (#2905).
// Hand-written, dependency-manifest-free. Express apps idiomatically wire
// the `morgan` HTTP-access logger as middleware plus a `winston` app logger;
// the observability extractor attributes a log signal to each.
import express from "express";
import morgan from "morgan";
import winston from "winston";

const logger = winston.createLogger({ level: "info" });
const app = express();
app.use(morgan("combined"));

app.get("/health", (req, res) => {
  logger.info("health check", { path: req.path });
  res.json({ ok: true });
});
