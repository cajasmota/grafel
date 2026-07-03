import { describe, it, expect } from "vitest";

import { defaultActionFor, groupCandidatesFor, reposForAction } from "./wizard-action";
import type { ScanInspectReply } from "@/data/types";

function reply(partial: Partial<ScanInspectReply>): ScanInspectReply {
  return {
    valid: true,
    absPath: "/code/x",
    suggestedGroup: "x",
    suggestedSlug: "x",
    stack: "go",
    monorepo: "",
    packages: [],
    childGitRepos: [],
    childrenKind: "",
    siblingGitRepos: [],
    isGitRepo: false,
    suggestedAction: "",
    hasAgentsMd: false,
    ...partial,
  };
}

describe("defaultActionFor", () => {
  it("maps suggestedAction through", () => {
    expect(defaultActionFor(reply({ suggestedAction: "group" }))).toBe("group");
    expect(defaultActionFor(reply({ suggestedAction: "monorepo" }))).toBe("monorepo");
    expect(defaultActionFor(reply({ suggestedAction: "single" }))).toBe("single");
  });
  it("falls back to single for empty/null", () => {
    expect(defaultActionFor(reply({ suggestedAction: "" }))).toBe("single");
    expect(defaultActionFor(null)).toBe("single");
  });
});

describe("groupCandidatesFor", () => {
  it("returns child git repos (the ivivo case)", () => {
    const scan = reply({
      absPath: "/code/ivivo",
      isGitRepo: false,
      childGitRepos: ["frontend", "backend"],
      childrenKind: "git-repos",
      suggestedAction: "group",
    });
    expect(groupCandidatesFor(scan)).toEqual(["backend", "frontend"]);
  });

  it("returns self + sibling basenames when the path is a repo with siblings", () => {
    const scan = reply({
      absPath: "/code/service-a",
      isGitRepo: true,
      siblingGitRepos: ["/code/service-b", "/code/service-c"],
      suggestedAction: "group",
    });
    expect(groupCandidatesFor(scan)).toEqual(["service-a", "service-b", "service-c"]);
  });

  it("handles Windows sibling paths", () => {
    const scan = reply({
      absPath: "D:\\code\\service-a",
      isGitRepo: true,
      siblingGitRepos: ["D:\\code\\service-b"],
      suggestedAction: "group",
    });
    expect(groupCandidatesFor(scan)).toEqual(["service-a", "service-b"]);
  });

  it("returns empty for an invalid or non-repo folder", () => {
    expect(groupCandidatesFor(reply({ valid: false }))).toEqual([]);
    expect(groupCandidatesFor(reply({ isGitRepo: false, childGitRepos: [] }))).toEqual([]);
    expect(groupCandidatesFor(null)).toEqual([]);
  });
});

describe("reposForAction", () => {
  it("keeps single repository authoritative even when packages are detected", () => {
    const scan = reply({
      absPath: "/code/assessment",
      suggestedSlug: "assessment",
      packages: ["apps/admin", "apps/field", "src"],
      monorepo: "pnpm",
      suggestedAction: "monorepo",
    });

    expect(reposForAction(scan, "single", new Set(), new Set(scan.packages))).toEqual([
      { path: "/code/assessment", slug: "assessment" },
    ]);
  });

  it("indexes selected packages for the monorepo action", () => {
    const scan = reply({
      absPath: "/code/assessment",
      suggestedSlug: "assessment",
      packages: ["apps/admin", "src"],
      monorepo: "pnpm",
    });

    expect(reposForAction(scan, "monorepo", new Set(), new Set(["src"]))).toEqual([
      { path: "/code/assessment/src", slug: "assessment-src", modules: ["src"] },
    ]);
  });

  it("indexes selected child repos for the group action", () => {
    const scan = reply({
      absPath: "/code/system",
      childGitRepos: ["api", "web"],
      childrenKind: "git-repos",
    });

    expect(reposForAction(scan, "group", new Set(["web"]), new Set())).toEqual([
      { path: "/code/system/web", slug: "web" },
    ]);
  });
});
