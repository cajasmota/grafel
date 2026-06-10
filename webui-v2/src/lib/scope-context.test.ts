/* Unit tests for the pure scope-derivation logic (#4637). The provider /
   hook wiring is React-bound; here we cover deriveScopeOptions which is the
   load-bearing pure function feeding the selector + matcher. */

import { describe, it, expect } from "vitest";
import { deriveScopeOptions } from "./scope-context";
import type { Group } from "@/data/types";

function group(partial: Partial<Group>): Group {
  return {
    id: "g",
    name: "g",
    repos: [],
    entityCount: 0,
    fidelity: null,
    indexedAt: null,
    health: "unindexed",
    ...partial,
  };
}

describe("deriveScopeOptions", () => {
  it("returns only ALL for an undefined group", () => {
    const opts = deriveScopeOptions(undefined);
    expect(opts).toHaveLength(1);
    expect(opts[0]).toMatchObject({ kind: "all", value: "" });
  });

  it("returns only ALL for a single-repo, non-monorepo group (selector hides)", () => {
    const opts = deriveScopeOptions(group({ repos: ["api"] }));
    // ALL + the one repo = 2; the selector itself decides to hide when the
    // ONLY real choice equals the group, but derivation still lists the repo.
    expect(opts.map((o) => o.value)).toEqual(["", "repo:api"]);
  });

  it("lists every repo for a multi-repo group, ALL first, sorted", () => {
    const opts = deriveScopeOptions(group({ repos: ["web", "api"] }));
    expect(opts.map((o) => o.value)).toEqual(["", "repo:api", "repo:web"]);
    expect(opts[0].kind).toBe("all");
  });

  it("expands a monorepo into repo + per-module options", () => {
    const opts = deriveScopeOptions(
      group({
        repos: ["mono"],
        monorepos: { mono: ["packages/b", "packages/a"] },
      }),
    );
    expect(opts.map((o) => o.value)).toEqual([
      "",
      "repo:mono",
      "module:mono/packages/a",
      "module:mono/packages/b",
    ]);
    const mod = opts.find((o) => o.value === "module:mono/packages/a")!;
    expect(mod).toMatchObject({ kind: "module", repo: "mono", modulePath: "packages/a" });
  });

  it("mixes plain repos and monorepos in one group", () => {
    const opts = deriveScopeOptions(
      group({
        repos: ["api", "mono"],
        monorepos: { mono: ["m1"] },
      }),
    );
    expect(opts.map((o) => o.value)).toEqual([
      "",
      "repo:api",
      "repo:mono",
      "module:mono/m1",
    ]);
  });
});
