/**
 * Tests for the Flowchart view (#4819):
 *   - the pure shape/caption helpers (boxFor / defaultCaption), and
 *   - the api `detail` mapping (the Detail slider → backend `detail=` param).
 *
 * Run with: npx vitest run src/components/flow-dag/flowchart.test.ts
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { boxFor, defaultCaption } from "./flowchart-shapes";
import { api } from "@/lib/api";
import type { ControlFlowDetail, ControlFlowNode } from "@/data/types";

function cfgNode(partial: Partial<ControlFlowNode> & { shape: ControlFlowNode["shape"] }): ControlFlowNode {
  return { id: "n0", ...partial };
}

describe("flowchart shapes", () => {
  it("gives decisions and loops a squarer diamond box", () => {
    const dec = boxFor("decision");
    const loop = boxFor("loop");
    // Squarer than the process rectangle so the rotated inner square reads as a diamond.
    expect(dec).toEqual(loop);
    expect(dec.h).toBeGreaterThan(boxFor("process").h);
  });

  it("gives terminals a short pill box and process a wide rectangle", () => {
    expect(boxFor("start")).toEqual(boxFor("end"));
    expect(boxFor("start").h).toBeLessThan(boxFor("process").h);
    expect(boxFor("process").w).toBeGreaterThan(boxFor("start").w);
  });

  it("prefers a source label, else a shape-derived caption", () => {
    expect(defaultCaption(cfgNode({ shape: "process", label: "await repo.save(x)" }))).toBe(
      "await repo.save(x)",
    );
    expect(defaultCaption(cfgNode({ shape: "start" }))).toBe("Start");
    expect(defaultCaption(cfgNode({ shape: "throw" }))).toBe("Throw");
  });

  it("falls back to the condition text for unlabelled decisions/loops", () => {
    expect(defaultCaption(cfgNode({ shape: "decision", condition: "if !user" }))).toBe("if !user");
    expect(defaultCaption(cfgNode({ shape: "loop", condition: "for row in rows" }))).toBe(
      "for row in rows",
    );
  });
});

describe("control-flow api detail mapping", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  function mockFetch() {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        new Response(JSON.stringify({ ok: true, data: { nodes: [], edges: [] } }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);
    return fetchMock;
  }

  it("maps the detail slider value into the control-flow query param", async () => {
    for (const detail of ["outline", "decisions", "data", "full"] as ControlFlowDetail[]) {
      const fetchMock = mockFetch();
      await api.getPathControlFlow("grp", "abc123", { detail });
      const url = String(fetchMock.mock.calls[0][0]);
      expect(url).toContain("/groups/grp/paths/abc123/control-flow");
      expect(url).toContain(`detail=${detail}`);
      vi.restoreAllMocks();
    }
  });

  it("includes the verb when given and omits it otherwise", async () => {
    let fetchMock = mockFetch();
    await api.getPathControlFlow("grp", "abc123", { detail: "decisions", verb: "POST" });
    expect(String(fetchMock.mock.calls[0][0])).toContain("verb=POST");
    vi.restoreAllMocks();

    fetchMock = mockFetch();
    await api.getPathControlFlow("grp", "abc123", { detail: "decisions" });
    expect(String(fetchMock.mock.calls[0][0])).not.toContain("verb=");
  });

  it("maps the inline depth into the control-flow query param (#4883)", async () => {
    // depth=1 is the default (handler-only) → param omitted to keep the URL terse.
    let fetchMock = mockFetch();
    await api.getPathControlFlow("grp", "abc123", { detail: "decisions", depth: 1 });
    expect(String(fetchMock.mock.calls[0][0])).not.toContain("depth=");
    vi.restoreAllMocks();

    // depth>=2 (inline callee CFGs) is sent.
    fetchMock = mockFetch();
    await api.getPathControlFlow("grp", "abc123", { detail: "decisions", depth: 3 });
    expect(String(fetchMock.mock.calls[0][0])).toContain("depth=3");
  });
});
