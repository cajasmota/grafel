/* ============================================================
   lib/scope-context.tsx — active repo/module SCOPE for the dashboard (#4637).

   Multi-repo and monorepo groups previously dumped every repo's entities
   together with no way to focus on one. This module provides:

     • ScopeProvider     — wraps the in-group shell, derives the available
                           scope options from the loaded Group metadata
                           (Group.repos + Group.monorepos) and holds the
                           active selection.
     • useScope()        — read the active scope + matcher inside any page.

   PERSISTENCE
   -----------
   The active scope lives in the `?scope=` URL search param (so it survives
   navigation between in-group screens and is shareable) AND is mirrored to
   localStorage per-group (so it survives a hard refresh that lands on a URL
   without the param). On mount, when the URL has no `?scope=` but
   localStorage remembers one for this group, we restore it into the URL.

   ENCODING (the `?scope=` value)
   ------------------------------
     • absent / ""            → ALL (no scoping)
     • "repo:<slug>"          → a single repo
     • "module:<repo>/<path>" → a single monorepo module within <repo>

   A scope value that no longer matches any available option (stale URL,
   repo removed) is treated as ALL so a bad param never blanks every page.

   APPLYING SCOPE
   --------------
   Pages read `matchesScope(repo, modulePath?)` from useScope() and filter
   their rows/records by it. For records that only carry a repo slug, pass
   just the repo; a module scope then degrades to repo-level (it matches any
   record in its parent repo) with a TODO to plumb true module attribution
   where the underlying data exposes a module path.
   ============================================================ */

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  type ReactNode,
} from "react";
import { useParams, useSearchParams } from "react-router-dom";
import { useGroups } from "@/hooks/use-groups";
import type { Group } from "@/data/types";

// ---------------------------------------------------------------------------
// § Scope option model
// ---------------------------------------------------------------------------

export type ScopeKind = "all" | "repo" | "module";

export interface ScopeOption {
  /** Stable encoded value used in the URL + localStorage. "" === ALL. */
  value: string;
  kind: ScopeKind;
  /** Human label for the selector ("All", repo slug, or module sub-path). */
  label: string;
  /** Parent repo slug. Empty for the ALL option. */
  repo: string;
  /**
   * Module sub-path within `repo` for module-kind options; undefined for
   * repo/all options.
   */
  modulePath?: string;
}

/** The implicit "show everything" option (value === ""). */
const ALL_OPTION: ScopeOption = { value: "", kind: "all", label: "All", repo: "" };

/**
 * Derive the selectable scope options from a group's metadata.
 *
 * Source of truth (no extra network call): the GET /api/v2/groups payload
 * already exposes `Group.repos` (top-level repo slugs) and `Group.monorepos`
 * (parent-repo → module sub-paths). We surface:
 *   • one "repo:<slug>" option per repo, and
 *   • one "module:<repo>/<path>" option per declared monorepo module.
 *
 * The ALL option is always first. Options are stable-sorted so the selector
 * order doesn't jump between renders.
 */
export function deriveScopeOptions(group: Group | undefined): ScopeOption[] {
  if (!group) return [ALL_OPTION];

  const repos = [...(group.repos ?? [])].sort((a, b) => a.localeCompare(b));
  const monorepos = group.monorepos ?? {};

  const opts: ScopeOption[] = [ALL_OPTION];

  for (const repo of repos) {
    const modules = monorepos[repo];
    if (modules && modules.length > 0) {
      // A monorepo: offer the whole repo PLUS each declared module.
      opts.push({ value: `repo:${repo}`, kind: "repo", label: repo, repo });
      for (const m of [...modules].sort((a, b) => a.localeCompare(b))) {
        opts.push({
          value: `module:${repo}/${m}`,
          kind: "module",
          // Show just the module tail when it's unambiguous within the repo;
          // the parent repo is conveyed by grouping/the repo chip.
          label: m,
          repo,
          modulePath: m,
        });
      }
    } else {
      opts.push({ value: `repo:${repo}`, kind: "repo", label: repo, repo });
    }
  }

  return opts;
}

// ---------------------------------------------------------------------------
// § Context value
// ---------------------------------------------------------------------------

export interface ScopeContextValue {
  /** All selectable options including ALL (first). */
  options: ScopeOption[];
  /** The resolved active option (ALL when none/stale). */
  active: ScopeOption;
  /** True when there is more than one real scope (renders the selector). */
  hasMultiple: boolean;
  /** Select a scope by its encoded value ("" clears to ALL). */
  setScope: (value: string) => void;
  /**
   * Predicate a page uses to keep a record. Pass the record's repo slug and,
   * when available, its module sub-path. With ALL active everything matches.
   *
   * Module attribution is best-effort: when a `modulePath` is supplied we
   * require it to be inside the active module's path; when it's omitted, a
   * module scope falls back to matching the parent repo (TODO: tighten once
   * per-record module paths are plumbed everywhere).
   */
  matchesScope: (repo: string | undefined | null, modulePath?: string | null) => boolean;
}

const ScopeContext = createContext<ScopeContextValue | null>(null);

const SCOPE_PARAM = "scope";
const storageKey = (groupId: string) => `ag.scope.${groupId}`;

function readStored(groupId: string): string {
  if (typeof localStorage === "undefined") return "";
  try {
    return localStorage.getItem(storageKey(groupId)) ?? "";
  } catch {
    return "";
  }
}

function writeStored(groupId: string, value: string): void {
  if (typeof localStorage === "undefined") return;
  try {
    if (value) localStorage.setItem(storageKey(groupId), value);
    else localStorage.removeItem(storageKey(groupId));
  } catch {
    /* ignore quota / disabled storage */
  }
}

// ---------------------------------------------------------------------------
// § Provider
// ---------------------------------------------------------------------------

export function ScopeProvider({ children }: { children: ReactNode }) {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const { data: groups = [] } = useGroups();

  const group = groups.find((g) => g.id === groupId);
  const options = useMemo(() => deriveScopeOptions(group), [group]);

  const rawParam = searchParams.get(SCOPE_PARAM) ?? "";

  // Resolve the active option: a param wins, but only if it still matches a
  // real option (otherwise a stale/removed scope silently falls back to ALL).
  const active = useMemo(
    () => options.find((o) => o.value === rawParam) ?? ALL_OPTION,
    [options, rawParam],
  );

  const setScope = useCallback(
    (value: string) => {
      writeStored(groupId, value);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (!value) next.delete(SCOPE_PARAM);
          else next.set(SCOPE_PARAM, value);
          return next;
        },
        { replace: true },
      );
    },
    [groupId, setSearchParams],
  );

  // Restore a remembered scope on first mount of a group when the URL has none.
  // Only restore if it still matches an available option for the loaded group.
  useEffect(() => {
    if (!groupId) return;
    if (rawParam) {
      // URL already carries a scope — keep localStorage in sync with it.
      writeStored(groupId, options.some((o) => o.value === rawParam) ? rawParam : "");
      return;
    }
    const stored = readStored(groupId);
    if (stored && options.some((o) => o.value === stored)) {
      setScope(stored);
    }
    // Intentionally depends on options so restore waits until the group (and
    // therefore its option set) has loaded.
  }, [groupId, rawParam, options, setScope]);

  const matchesScope = useCallback<ScopeContextValue["matchesScope"]>(
    (repo, modulePath) => {
      if (active.kind === "all") return true;
      if (!repo) return false;
      if (repo !== active.repo) return false;
      if (active.kind === "repo") return true;
      // Module scope: tighten when a record exposes a module path, else fall
      // back to repo-level (TODO: per-record module attribution).
      if (modulePath == null) return true;
      const want = active.modulePath ?? "";
      return modulePath === want || modulePath.startsWith(`${want}/`);
    },
    [active],
  );

  const value = useMemo<ScopeContextValue>(
    () => ({
      options,
      active,
      hasMultiple: options.length > 1,
      setScope,
      matchesScope,
    }),
    [options, active, setScope, matchesScope],
  );

  return <ScopeContext.Provider value={value}>{children}</ScopeContext.Provider>;
}

// ---------------------------------------------------------------------------
// § Consumer hook
// ---------------------------------------------------------------------------

/**
 * Read the active scope, its options, and the matcher. Safe outside a
 * provider (returns an inert ALL scope) so a stray consumer never crashes.
 */
export function useScope(): ScopeContextValue {
  const ctx = useContext(ScopeContext);
  if (!ctx) {
    return {
      options: [ALL_OPTION],
      active: ALL_OPTION,
      hasMultiple: false,
      setScope: () => {},
      matchesScope: () => true,
    };
  }
  return ctx;
}
