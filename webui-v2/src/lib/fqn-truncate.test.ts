/**
 * Unit tests for fqn-truncate.ts
 *
 * Run with: npx vitest run src/lib/fqn-truncate.test.ts
 */
import { describe, it, expect } from "vitest";
import { truncateFqn } from "./fqn-truncate";

describe("truncateFqn", () => {
  it("keeps last 2 segments of a deep FQN", () => {
    const fqn =
      "com.clientfixturea.users.controllers.UsersController.update";
    const result = truncateFqn(fqn);
    expect(result.display).toBe("UsersController.update");
    expect(result.full).toBe(fqn);
  });

  it("returns unchanged when already short (1 segment)", () => {
    const fqn = "update";
    const result = truncateFqn(fqn);
    expect(result.display).toBe("update");
    expect(result.full).toBe("update");
  });

  it("returns unchanged when already short (2 segments)", () => {
    const fqn = "UsersController.update";
    const result = truncateFqn(fqn);
    expect(result.display).toBe("UsersController.update");
    expect(result.full).toBe("UsersController.update");
  });

  it("handles generic type in last segment", () => {
    const fqn = "com.clientfixturea.dto.List<UserDTO>";
    // There's only 1 segment when split on "." beyond the generic — but
    // the generic bracket shouldn't confuse the splitter.
    // "com", "clientfixturea", "dto", "List<UserDTO>" → 4 parts → last 2
    const result = truncateFqn(fqn);
    expect(result.display).toBe("dto.List<UserDTO>");
    expect(result.full).toBe(fqn);
  });

  it("handles no dots (single segment)", () => {
    const fqn = "handleRequest";
    const result = truncateFqn(fqn);
    expect(result.display).toBe("handleRequest");
  });

  it("handles empty string", () => {
    const result = truncateFqn("");
    expect(result.display).toBe("");
    expect(result.full).toBe("");
  });

  it("respects custom segments count of 3", () => {
    const fqn = "com.example.service.UserService.findById";
    const result = truncateFqn(fqn, 3);
    expect(result.display).toBe("service.UserService.findById");
  });

  it("respects custom segments count of 1", () => {
    const fqn = "com.example.service.UserService.findById";
    const result = truncateFqn(fqn, 1);
    expect(result.display).toBe("findById");
  });
});
