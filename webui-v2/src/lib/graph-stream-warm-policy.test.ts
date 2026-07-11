/* ============================================================
   lib/graph-stream-warm-policy.test.ts — unit tests for the pure warm
   retry/give-up policy + SSE error-event parsing (#5722).

   Extracted as a pure seam (mirrors graph-stream-reducer) so the
   retry-ceiling and error-parsing logic used by hooks/use-graph-stream is
   unit-testable without a live EventSource.
   ============================================================ */

import { describe, expect, it } from "vitest";
import {
  MAX_WARM_ATTEMPTS,
  WARM_BACKOFF_MS,
  decideWarmRetry,
  parseWarmErrorEvent,
} from "./graph-stream-warm-policy";

describe("decideWarmRetry", () => {
  it("retries with the backoff schedule while under the ceiling", () => {
    for (let attempt = 0; attempt < WARM_BACKOFF_MS.length; attempt++) {
      const decision = decideWarmRetry(attempt);
      expect(decision.kind).toBe("retry");
      if (decision.kind === "retry") {
        expect(decision.delayMs).toBe(WARM_BACKOFF_MS[attempt]);
      }
    }
  });

  it("clamps to the last backoff step once past the schedule but under the ceiling", () => {
    const decision = decideWarmRetry(WARM_BACKOFF_MS.length + 1);
    expect(decision.kind).toBe("retry");
    if (decision.kind === "retry") {
      expect(decision.delayMs).toBe(WARM_BACKOFF_MS[WARM_BACKOFF_MS.length - 1]);
    }
  });

  it("gives up once the retry ceiling is reached, with a helpful message", () => {
    const decision = decideWarmRetry(MAX_WARM_ATTEMPTS);
    expect(decision.kind).toBe("giveUp");
    if (decision.kind === "giveUp") {
      expect(decision.message.length).toBeGreaterThan(0);
    }
  });

  it("never retries indefinitely — the ceiling is reached in finite attempts", () => {
    let attempt = 0;
    let decision = decideWarmRetry(attempt);
    while (decision.kind === "retry" && attempt < 1000) {
      attempt++;
      decision = decideWarmRetry(attempt);
    }
    expect(decision.kind).toBe("giveUp");
    expect(attempt).toBeLessThan(1000);
  });
});

describe("parseWarmErrorEvent", () => {
  it("parses a well-formed backend error payload", () => {
    const parsed = parseWarmErrorEvent(
      JSON.stringify({ code: "load_failed", message: "group testgrp: config file does not exist" }),
    );
    expect(parsed.code).toBe("load_failed");
    expect(parsed.message).toBe("group testgrp: config file does not exist");
  });

  it("falls back to a generic message on malformed JSON", () => {
    const parsed = parseWarmErrorEvent("not json");
    expect(parsed.code).toBe("unknown");
    expect(parsed.message.length).toBeGreaterThan(0);
  });

  it("falls back to a generic message when fields are missing", () => {
    const parsed = parseWarmErrorEvent(JSON.stringify({}));
    expect(parsed.code).toBe("unknown");
    expect(parsed.message.length).toBeGreaterThan(0);
  });
});
