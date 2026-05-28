// Marble.js backend-HTTP log_extraction fixture (#2905).
// Marble.js effects log through pino (its documented logger middleware).
// The observability extractor attributes a pino log signal.
import { r } from "@marblejs/http";
import pino from "pino";
import { mapTo } from "rxjs/operators";

const logger = pino({ level: "info" });

export const health$ = r.pipe(
  r.matchPath("/health"),
  r.matchType("GET"),
  r.useEffect((req$) =>
    req$.pipe(
      mapTo(() => {
        logger.info("health check");
        return { body: { ok: true } };
      })
    )
  )
);
