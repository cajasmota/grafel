import { describe, it, expect } from "vitest";
import { categoryStyle } from "./categoryStyle";

// Mirrors the backend catalog (#4885): the richer provider-agnostic categories
// must each resolve to a distinct, on-theme style; unknowns fall back to "other".
describe("categoryStyle (#4885 richer catalog)", () => {
  it("resolves the new Security/Identity category", () => {
    const s = categoryStyle("security");
    expect(s.key).toBe("security");
    expect(s.label).toBe("Security");
    expect(s.color).toContain("var(");
  });

  it("resolves the new Observability category", () => {
    const s = categoryStyle("observability");
    expect(s.key).toBe("observability");
    expect(s.label).toBe("Observability");
    expect(s.color).toContain("var(");
  });

  it("is case-insensitive and trims empties to other", () => {
    expect(categoryStyle("SECURITY").key).toBe("security");
    expect(categoryStyle("").key).toBe("other");
    expect(categoryStyle(undefined).key).toBe("other");
  });

  it("falls back to Other for an unknown category", () => {
    const s = categoryStyle("totally_unknown");
    expect(s.key).toBe("totally_unknown");
    expect(s.label).toBe("Other");
  });

  it("keeps the established categories intact", () => {
    expect(categoryStyle("datastore").label).toBe("Datastore");
    expect(categoryStyle("compute").label).toBe("Compute");
    expect(categoryStyle("messaging").label).toBe("Other"); // not a backend category name
  });
});
