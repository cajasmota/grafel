// Hono backend-HTTP log_extraction fixture (#2905).
// Hono ships a built-in `logger` middleware backed by console; apps also
// drop to console.* for structured request logging. The observability
// extractor attributes a console log signal.
import { Hono } from "hono";

const app = new Hono();

app.get("/health", (c) => {
  console.info("health check", { path: c.req.path });
  return c.json({ ok: true });
});
