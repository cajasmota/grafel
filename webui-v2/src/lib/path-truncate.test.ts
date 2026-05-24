/**
 * Unit tests for path-truncate.ts
 *
 * These tests run with: npx vitest run src/lib/path-truncate.test.ts
 * (vitest is added as a dev dependency for this feature).
 */
import { describe, it, expect } from "vitest";
import { truncateFilePath } from "./path-truncate";

describe("truncateFilePath", () => {
  it("returns unchanged string when short enough", () => {
    const result = truncateFilePath("src/main/Foo.java:10");
    expect(result.display).toBe("src/main/Foo.java:10");
    expect(result.full).toBe("src/main/Foo.java:10");
  });

  it("truncates a deep Java path to first 2 segments + filename", () => {
    const path =
      "src/main/java/com/clientfixturea/users/controllers/UsersController.java:130";
    const result = truncateFilePath(path);
    expect(result.display).toBe("src/main/.../UsersController.java:130");
    expect(result.full).toBe(path);
  });

  it("preserves full string on result.full regardless of truncation", () => {
    const path =
      "src/main/java/com/clientfixturea/services/auth/impl/JWTValidatorServiceImpl.java:42";
    const result = truncateFilePath(path);
    expect(result.full).toBe(path);
    expect(result.display.length).toBeLessThanOrEqual(50);
  });

  it("handles path with no slashes (just a filename)", () => {
    const path = "UsersController.java:5";
    const result = truncateFilePath(path);
    expect(result.display).toBe(path);
    expect(result.full).toBe(path);
  });

  it("handles path with no line number (no colon)", () => {
    const path = "src/main/java/com/clientfixturea/controllers/UsersController.java";
    const result = truncateFilePath(path, 50);
    expect(result.full).toBe(path);
    expect(result.display.length).toBeLessThanOrEqual(50);
  });

  it("handles empty string", () => {
    const result = truncateFilePath("");
    expect(result.display).toBe("");
    expect(result.full).toBe("");
  });

  it("uses custom maxLen", () => {
    const path =
      "src/main/java/com/clientfixturea/users/controllers/UsersController.java:130";
    const result = truncateFilePath(path, 80);
    // Path is 75 chars, within 80 — should not be truncated
    expect(result.display).toBe(path);
  });

  it("handles 2-segment path that is too long", () => {
    const path = "verylongsegmentone/verylongfilename_that_exceeds_max_length.java:200";
    const result = truncateFilePath(path, 30);
    expect(result.display.length).toBeLessThanOrEqual(30);
    expect(result.full).toBe(path);
  });
});
