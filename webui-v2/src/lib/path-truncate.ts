/**
 * path-truncate.ts — presentational truncation helpers for file paths.
 *
 * ALL MCP / agent queries still receive full strings; these functions
 * are used only by display components (RefLine, TruncatedPath, etc.).
 *
 * Strategy for truncateFilePath:
 *   - Keep first 2 path segments (src/main/java/...)
 *   - Replace the middle with "..."
 *   - Keep last 1-2 segments + filename:line
 *   - If the path is already short enough, return it unchanged.
 */

export interface TruncatedString {
  /** The display form (possibly truncated with "..."). */
  display: string;
  /** The original, never-truncated full string. */
  full: string;
}

/**
 * Truncate a file path for display purposes.
 *
 * @param path   Full relative file path, e.g.
 *               "src/main/java/com/clientfixturea/users/controllers/UsersController.java:130"
 * @param maxLen Maximum character length for the display form (default 50).
 *               Paths shorter than or equal to maxLen are returned unchanged.
 *
 * @example
 *   truncateFilePath(
 *     "src/main/java/com/clientfixturea/users/controllers/UsersController.java:130"
 *   )
 *   // { display: "src/main/java/.../UsersController.java:130", full: "…" }
 */
export function truncateFilePath(
  path: string,
  maxLen = 50,
): TruncatedString {
  if (!path) return { display: "", full: "" };
  if (path.length <= maxLen) return { display: path, full: path };

  // Split on "/" so we can reason about segments.
  const parts = path.split("/");
  if (parts.length <= 2) {
    // Nothing useful to trim; just hard-truncate with a tail ellipsis.
    return { display: path.slice(0, maxLen - 1) + "…", full: path };
  }

  const filename = parts[parts.length - 1]; // e.g. "UsersController.java:130"
  const firstTwo = parts.slice(0, 2).join("/"); // e.g. "src/main"

  // Try: firstTwo + "/.../" + filename
  const candidate = `${firstTwo}/.../${filename}`;
  if (candidate.length <= maxLen) {
    return { display: candidate, full: path };
  }

  // Still too long — keep only the filename
  const fileOnly = `.../${filename}`;
  if (fileOnly.length <= maxLen) {
    return { display: fileOnly, full: path };
  }

  // Absolute last resort: hard-truncate the display to maxLen
  return { display: filename.slice(0, maxLen - 1) + "…", full: path };
}

/**
 * Truncate a fully-qualified name to its last N dot-separated segments.
 *
 * Moved to a separate file (fqn-truncate.ts) per the spec; this re-export
 * is provided only as a convenience barrel — import from the canonical
 * location when possible.
 */
export { truncateFqn } from "./fqn-truncate";
