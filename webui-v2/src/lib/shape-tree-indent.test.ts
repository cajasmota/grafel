import { describe, it, expect } from "vitest";
import {
  SHAPE_INDENT_STEP,
  indentForDepth,
  fieldTypeLabel,
  fieldOptionality,
} from "./shape-tree-indent";

describe("indentForDepth", () => {
  it("returns 0 for the top level", () => {
    expect(indentForDepth(0)).toBe(0);
  });

  it("indents one step per depth level", () => {
    expect(indentForDepth(1)).toBe(SHAPE_INDENT_STEP);
    expect(indentForDepth(2)).toBe(SHAPE_INDENT_STEP * 2);
    expect(indentForDepth(3)).toBe(SHAPE_INDENT_STEP * 3);
  });

  it("clamps negative depths to 0", () => {
    expect(indentForDepth(-5)).toBe(0);
  });
});

describe("fieldTypeLabel", () => {
  it("returns the bare type when required", () => {
    expect(fieldTypeLabel("string", false)).toBe("string");
    expect(fieldTypeLabel("string", undefined)).toBe("string");
  });

  it("annotates nullable types with | null", () => {
    expect(fieldTypeLabel("number", true)).toBe("number | null");
  });

  it("does not double-annotate types that already advertise null", () => {
    expect(fieldTypeLabel("string | null", true)).toBe("string | null");
    expect(fieldTypeLabel("string?", true)).toBe("string?");
  });

  it("falls back to unknown when type is missing", () => {
    expect(fieldTypeLabel(undefined, false)).toBe("unknown");
    expect(fieldTypeLabel("  ", false)).toBe("unknown");
  });
});

describe("fieldOptionality", () => {
  it("maps nullable to optional/required", () => {
    expect(fieldOptionality(true)).toBe("optional");
    expect(fieldOptionality(false)).toBe("required");
    expect(fieldOptionality(undefined)).toBe("required");
  });
});
