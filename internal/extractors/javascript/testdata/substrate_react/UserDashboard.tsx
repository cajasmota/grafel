// React fixture for substrate recording sweep (#2849).
// Proves: http_effect, db_effect, fs_effect, mutation_effect,
//         taint_source_detection, taint_sink_detection, sanitizer_recognition,
//         vulnerability_finding, def_use_chain_extraction, pure_function_tagging,
//         template_pattern_catalog.
// A sibling file (cyclic_dep.tsx) creates the import cycle needed for
// module_cycle_detection; both import each other.
// import_resolution_quality is proven by the cross-file import below.
import React, { useState, useEffect } from "react";
import { formatUserName } from "./utils";
import { useSharedState } from "./cyclic_dep";

// ── pure helper (no side effects) ────────────────────────────────────────────
// Proves pure_function_tagging: formatDisplayName has no db/http/fs/mutation
// effects; the substrate pure-function pass should tag it pure.
export function formatDisplayName(first: string, last: string): string {
  return `${first} ${last}`.trim();
}

// ── component with http_effect ────────────────────────────────────────────────
// Proves http_effect: fetchUsers calls the Fetch API — sniffEffectsJSTS picks
// up the call-site; effect_propagation tags fetchUsers with http_out.
async function fetchUsers(): Promise<void> {
  const res = await fetch("https://api.example.com/users");
  const data = await res.json();
  return data;
}

// ── component with db_effect ─────────────────────────────────────────────────
// Proves db_effect: loadUserById calls .findOne() — jstsDBReadRe matches;
// saveUser calls .create() — jstsDBWriteRe matches.
async function loadUserById(id: string) {
  return await db.findOne({ where: { id } });
}

async function saveUser(user: object) {
  return await db.create(user);
}

// ── component with fs_effect ──────────────────────────────────────────────────
// Proves fs_effect: readUserAvatar uses fs.readFile; writeAuditLog uses
// fs.writeFile. Both are matched by jstsFSReadRe / jstsFSWriteRe.
async function readUserAvatar(path: string) {
  return await fs.readFile(path, "utf8");
}

async function writeAuditLog(entry: string) {
  await fs.writeFile("/var/log/audit.log", entry);
}

// ── mutation_effect ───────────────────────────────────────────────────────────
// Proves mutation_effect: setCache assigns this.cache; jstsMutationRe matches
// `this.<field> =`.
class UserStore {
  cache: object | null = null;

  setCache(data: object) {
    this.cache = data;
  }
}

// ── taint source + sink + sanitizer + vulnerability_finding ──────────────────
// Proves taint_source_detection: req.body.userId is matched by jstsSourceReqRe.
// Proves taint_sink_detection: dangerouslySetInnerHTML matches jstsSinkXSSRe.
// Proves sanitizer_recognition: DOMPurify.sanitize matches jstsSanitizerHTMLRe.
// Proves vulnerability_finding: the unsanitised innerHTML path constitutes a
//   finding once the taint_flow pass connects source → sink without sanitizer.
function renderUserBio(req: any) {
  const userId = req.body.userId;
  const rawBio = req.body.bio;

  // Safe path: sanitize before render.
  const cleanBio = DOMPurify.sanitize(rawBio);
  const safeElement = { __html: cleanBio };

  // Unsafe path: unsanitised dangerouslySetInnerHTML (XSS vector).
  const unsafeElement = { dangerouslySetInnerHTML: { __html: rawBio } };

  return { safe: safeElement, unsafe: unsafeElement };
}

// ── def-use chain extraction ──────────────────────────────────────────────────
// Proves def_use_chain_extraction: userId is defined via const, used in the
// fetch call; result is defined from fetch, then used in setCache.
async function loadAndCache(userId: string) {
  const apiUrl = `https://api.example.com/users/${userId}`;
  const result = await fetch(apiUrl);
  const parsed = await result.json();
  const store = new UserStore();
  store.setCache(parsed);
  return parsed;
}

// ── template_pattern_catalog ─────────────────────────────────────────────────
// Proves template_pattern_catalog: t("dashboard.title") matches jstsI18nRe;
// console.error matches jstsLogRe; SELECT literal matches jstsSQLRe.
function DashboardTitle() {
  const title = t("dashboard.title");
  console.error("Failed to load dashboard: {}");
  const query = "SELECT id, name FROM users WHERE active = 1";
  return title;
}

// ── cross-file import (import_resolution_quality) ────────────────────────────
// Proves import_resolution_quality: the import of formatUserName from ./utils
// and useSharedState from ./cyclic_dep are cross-file imports; the jsts
// substrate sniffer captures named-import bindings from relative paths.
export function UserCard({ userId }: { userId: string }) {
  const [name, setName] = useState<string>("");
  const shared = useSharedState();

  useEffect(() => {
    loadUserById(userId).then((user: any) => {
      setName(formatUserName(user.first, user.last));
    });
  }, [userId]);

  return name;
}
