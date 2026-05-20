/**
 * Grouping logic for Surface 4 — Paths grouped view.
 *
 * Priority:
 *  1. path.controller — explicit controller class / module name from extractor
 *  2. URL prefix segment — second segment of the path e.g. /api/users/... → "users"
 *  3. "(uncategorized)" — fallback for paths with no useful grouping signal
 */

import type { PathRow, HttpVerb } from '@/types/api'
import { sortVerbs } from '@/lib/pathUtils'

export interface PathGroup {
  name: string
  paths: PathRow[]
  /** Counts per HTTP verb, sorted by canonical verb order */
  verbCounts: { verb: HttpVerb; count: number }[]
  totalEndpoints: number
}

const VERB_ORDER: HttpVerb[] = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS', 'ANY', 'WS']

/** Derive a group key from a PathRow */
function groupKey(path: PathRow): string {
  if (path.controller) return path.controller

  // Try to extract a meaningful prefix segment
  // e.g. "/api/v1/users/..." → "users" (skip "api", "v1", "v2" etc.)
  const segments = path.path.replace(/^\//, '').split('/').filter(Boolean)
  const VERSION_RE = /^v\d+$/i
  const GENERIC = new Set(['api', 'rest', 'graphql'])

  for (const seg of segments) {
    if (!VERSION_RE.test(seg) && !GENERIC.has(seg.toLowerCase()) && !seg.startsWith('{')) {
      return seg
    }
  }

  return '(uncategorized)'
}

/** Sort paths within a group: by method order, then alphabetically by path */
function sortPathRows(rows: PathRow[]): PathRow[] {
  return [...rows].sort((a, b) => {
    const aVerb = sortVerbs(a.verbs)[0] ?? 'ANY'
    const bVerb = sortVerbs(b.verbs)[0] ?? 'ANY'
    const aIdx = VERB_ORDER.indexOf(aVerb)
    const bIdx = VERB_ORDER.indexOf(bVerb)
    if (aIdx !== bIdx) return aIdx - bIdx
    return a.path.localeCompare(b.path)
  })
}

/** Build verb distribution from a set of PathRows */
function buildVerbCounts(rows: PathRow[]): { verb: HttpVerb; count: number }[] {
  const counts = new Map<HttpVerb, number>()
  for (const row of rows) {
    for (const verb of row.verbs) {
      counts.set(verb, (counts.get(verb) ?? 0) + 1)
    }
  }
  return VERB_ORDER
    .filter((v) => counts.has(v))
    .map((v) => ({ verb: v, count: counts.get(v)! }))
}

/**
 * Group a flat array of PathRows into named PathGroup objects.
 * Uncategorized paths always appear last.
 */
export function groupPaths(paths: PathRow[]): PathGroup[] {
  const map = new Map<string, PathRow[]>()

  for (const path of paths) {
    const key = groupKey(path)
    const bucket = map.get(key) ?? []
    bucket.push(path)
    map.set(key, bucket)
  }

  const groups: PathGroup[] = []
  const uncategorized: PathGroup[] = []

  for (const [name, rows] of map.entries()) {
    const sorted = sortPathRows(rows)
    const group: PathGroup = {
      name,
      paths: sorted,
      verbCounts: buildVerbCounts(sorted),
      totalEndpoints: sorted.reduce((s, r) => s + r.multiplicity, 0),
    }
    if (name === '(uncategorized)') {
      uncategorized.push(group)
    } else {
      groups.push(group)
    }
  }

  // Sort named groups alphabetically, uncategorized always last
  groups.sort((a, b) => a.name.localeCompare(b.name))
  return [...groups, ...uncategorized]
}
