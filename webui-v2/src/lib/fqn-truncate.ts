/**
 * fqn-truncate.ts — presentational truncation for fully-qualified names.
 *
 * ALL MCP / agent queries still receive full strings; this function is
 * used only by display components.
 */

export interface TruncatedString {
  /** The display form (possibly shortened to last N segments). */
  display: string;
  /** The original full string. */
  full: string;
}

/**
 * Truncate a fully-qualified name to its last `segments` dot-separated parts.
 *
 * @param fqn      The full qualified name, e.g.
 *                 "com.clientfixturea.users.controllers.UsersController.update"
 * @param segments How many trailing segments to keep (default 2).
 *
 * @example
 *   truncateFqn("com.clientfixturea.users.controllers.UsersController.update")
 *   // { display: "UsersController.update", full: "…" }
 *
 *   truncateFqn("UsersController.update")
 *   // { display: "UsersController.update", full: "UsersController.update" }
 *
 *   // Generic types are preserved as-is in the last segment:
 *   truncateFqn("com.clientfixturea.dto.List<UserDTO>", 1)
 *   // { display: "List<UserDTO>", full: "…" }
 */
export function truncateFqn(fqn: string, segments = 2): TruncatedString {
  if (!fqn) return { display: "", full: "" };

  const parts = fqn.split(".");
  if (parts.length <= segments) {
    return { display: fqn, full: fqn };
  }

  const display = parts.slice(parts.length - segments).join(".");
  return { display, full: fqn };
}
