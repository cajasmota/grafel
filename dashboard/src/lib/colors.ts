/**
 * Centralized color palette for Archigraph dashboard.
 * All color values are Tailwind utility classes — dark-mode-aware via the dark: prefix.
 */

import type { HttpVerb, EntityKind, TopologyProtocol } from '@/types/api'

// ────────────────────────────────────────────────────────────────────────────
// HTTP Verb colors
// ────────────────────────────────────────────────────────────────────────────

export const VERB_COLORS: Record<HttpVerb, { bg: string; text: string; border: string }> = {
  GET:     { bg: 'bg-emerald-900/40', text: 'text-emerald-300', border: 'border-emerald-700' },
  POST:    { bg: 'bg-blue-900/40',    text: 'text-blue-300',    border: 'border-blue-700' },
  PUT:     { bg: 'bg-orange-900/40',  text: 'text-orange-300',  border: 'border-orange-700' },
  PATCH:   { bg: 'bg-yellow-900/40',  text: 'text-yellow-300',  border: 'border-yellow-700' },
  DELETE:  { bg: 'bg-red-900/40',     text: 'text-red-300',     border: 'border-red-700' },
  HEAD:    { bg: 'bg-slate-900/40',   text: 'text-slate-300',   border: 'border-slate-700' },
  OPTIONS: { bg: 'bg-slate-900/40',   text: 'text-slate-300',   border: 'border-slate-700' },
  ANY:     { bg: 'bg-slate-800/40',   text: 'text-slate-400',   border: 'border-slate-600' },
  WS:      { bg: 'bg-purple-900/40',  text: 'text-purple-300',  border: 'border-purple-700' },
}

// Light mode overrides for verb chips
export const VERB_COLORS_LIGHT: Record<HttpVerb, { bg: string; text: string; border: string }> = {
  GET:     { bg: 'bg-emerald-50',  text: 'text-emerald-700', border: 'border-emerald-300' },
  POST:    { bg: 'bg-blue-50',     text: 'text-blue-700',    border: 'border-blue-300' },
  PUT:     { bg: 'bg-orange-50',   text: 'text-orange-700',  border: 'border-orange-300' },
  PATCH:   { bg: 'bg-yellow-50',   text: 'text-yellow-700',  border: 'border-yellow-300' },
  DELETE:  { bg: 'bg-red-50',      text: 'text-red-700',     border: 'border-red-300' },
  HEAD:    { bg: 'bg-slate-50',    text: 'text-slate-600',   border: 'border-slate-300' },
  OPTIONS: { bg: 'bg-slate-50',    text: 'text-slate-600',   border: 'border-slate-300' },
  ANY:     { bg: 'bg-slate-100',   text: 'text-slate-500',   border: 'border-slate-200' },
  WS:      { bg: 'bg-purple-50',   text: 'text-purple-700',  border: 'border-purple-300' },
}

// ────────────────────────────────────────────────────────────────────────────
// Entity kind colors
// ────────────────────────────────────────────────────────────────────────────

const KIND_COLOR_MAP: Partial<Record<EntityKind, { bg: string; text: string }>> = {
  Function:     { bg: 'bg-violet-900/40', text: 'text-violet-300' },
  Class:        { bg: 'bg-blue-900/40',   text: 'text-blue-300' },
  Component:    { bg: 'bg-cyan-900/40',   text: 'text-cyan-300' },
  Schema:       { bg: 'bg-teal-900/40',   text: 'text-teal-300' },
  Route:        { bg: 'bg-emerald-900/40',text: 'text-emerald-300' },
  Endpoint:     { bg: 'bg-emerald-900/40',text: 'text-emerald-300' },
  Service:      { bg: 'bg-indigo-900/40', text: 'text-indigo-300' },
  DataAccess:   { bg: 'bg-amber-900/40',  text: 'text-amber-300' },
  Datastore:    { bg: 'bg-orange-900/40', text: 'text-orange-300' },
  Model:        { bg: 'bg-pink-900/40',   text: 'text-pink-300' },
  Queue:        { bg: 'bg-rose-900/40',   text: 'text-rose-300' },
  MessageTopic: { bg: 'bg-rose-900/40',   text: 'text-rose-300' },
  ExternalAPI:  { bg: 'bg-slate-900/40',  text: 'text-slate-300' },
  Document:     { bg: 'bg-slate-800/40',  text: 'text-slate-400' },
  Process:      { bg: 'bg-lime-900/40',   text: 'text-lime-300' },
}

const KIND_DEFAULT = { bg: 'bg-slate-800/40', text: 'text-slate-400' }

export function kindColors(kind: EntityKind): { bg: string; text: string } {
  return KIND_COLOR_MAP[kind] ?? KIND_DEFAULT
}

// ────────────────────────────────────────────────────────────────────────────
// Repo island colors — distinct hues for multi-repo graph clusters (#1000)
// ────────────────────────────────────────────────────────────────────────────

/**
 * Returns a stable hex color for a repo slug.
 * Uses a fixed palette of 8 distinct hues; repos beyond 8 wrap around.
 * The palette avoids red/green (reserved for error/success UI) and grey
 * (reserved for unknown/filtered nodes).
 */
const REPO_COLOR_PALETTE: string[] = [
  '#38bdf8', // sky-400    — repo 0
  '#a78bfa', // violet-400 — repo 1
  '#34d399', // emerald-400 — repo 2
  '#fbbf24', // amber-400  — repo 3
  '#f472b6', // pink-400   — repo 4
  '#60a5fa', // blue-400   — repo 5
  '#fb923c', // orange-400 — repo 6
  '#4ade80', // green-400  — repo 7
]

const repoColorCache = new Map<string, string>()
const repoOrder: string[] = []

export function repoColor(slug: string): string {
  if (repoColorCache.has(slug)) return repoColorCache.get(slug)!
  if (!repoOrder.includes(slug)) repoOrder.push(slug)
  const idx = repoOrder.indexOf(slug)
  const color = REPO_COLOR_PALETTE[idx % REPO_COLOR_PALETTE.length]
  repoColorCache.set(slug, color)
  return color
}

// ────────────────────────────────────────────────────────────────────────────
// Repo Tailwind palette — stable Tailwind color per slug (for UI chips)
// ────────────────────────────────────────────────────────────────────────────

const REPO_TAILWIND_PALETTE = [
  { bg: 'bg-sky-900/40',     text: 'text-sky-300',     dot: 'bg-sky-400' },
  { bg: 'bg-fuchsia-900/40', text: 'text-fuchsia-300', dot: 'bg-fuchsia-400' },
  { bg: 'bg-lime-900/40',    text: 'text-lime-300',    dot: 'bg-lime-400' },
  { bg: 'bg-amber-900/40',   text: 'text-amber-300',   dot: 'bg-amber-400' },
  { bg: 'bg-rose-900/40',    text: 'text-rose-300',    dot: 'bg-rose-400' },
  { bg: 'bg-teal-900/40',    text: 'text-teal-300',    dot: 'bg-teal-400' },
  { bg: 'bg-indigo-900/40',  text: 'text-indigo-300',  dot: 'bg-indigo-400' },
  { bg: 'bg-orange-900/40',  text: 'text-orange-300',  dot: 'bg-orange-400' },
]

const repoTwCache = new Map<string, (typeof REPO_TAILWIND_PALETTE)[number]>()

function hashStr(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) {
    h = (Math.imul(31, h) + s.charCodeAt(i)) | 0
  }
  return Math.abs(h)
}

/** Returns Tailwind color classes for a repo slug — for use in UI chip components. */
export function repoTailwindColor(slug: string): (typeof REPO_TAILWIND_PALETTE)[number] {
  if (!repoTwCache.has(slug)) {
    repoTwCache.set(slug, REPO_TAILWIND_PALETTE[hashStr(slug) % REPO_TAILWIND_PALETTE.length])
  }
  return repoTwCache.get(slug)!
}

// ────────────────────────────────────────────────────────────────────────────
// Protocol colors — each broker/channel protocol gets a distinct hue + shape
// ────────────────────────────────────────────────────────────────────────────

export interface ProtocolColorSpec {
  /** Tailwind bg class */
  bg: string
  /** Tailwind text class */
  text: string
  /** Tailwind border class */
  border: string
  /** CSS hex for canvas rendering (SVG / canvas cannot use Tailwind classes) */
  hex: string
  /** Accessible shape name for protocol nodes */
  shape: 'square' | 'circle' | 'hexagon' | 'diamond' | 'triangle' | 'star' | 'pentagon' | 'cross' | 'chevron' | 'bolt' | 'clock' | 'ring'
  /** Human-readable label */
  label: string
}

export const PROTOCOL_COLORS: Record<TopologyProtocol, ProtocolColorSpec> = {
  // ── existing ──────────────────────────────────────────────────────────────
  kafka:                { bg: 'bg-cyan-900/40',    text: 'text-cyan-300',    border: 'border-cyan-700',    hex: '#22d3ee', shape: 'square',   label: 'Kafka' },
  rabbitmq:             { bg: 'bg-amber-900/40',   text: 'text-amber-300',   border: 'border-amber-700',   hex: '#fbbf24', shape: 'circle',   label: 'RabbitMQ' },
  sqs:                  { bg: 'bg-orange-900/40',  text: 'text-orange-300',  border: 'border-orange-700',  hex: '#fb923c', shape: 'hexagon',  label: 'SQS' },
  pubsub:               { bg: 'bg-blue-900/40',    text: 'text-blue-300',    border: 'border-blue-700',    hex: '#60a5fa', shape: 'diamond',  label: 'Pub/Sub' },
  nats:                 { bg: 'bg-fuchsia-900/40', text: 'text-fuchsia-300', border: 'border-fuchsia-700', hex: '#e879f9', shape: 'pentagon', label: 'NATS' },
  websocket:            { bg: 'bg-teal-900/40',    text: 'text-teal-300',    border: 'border-teal-700',    hex: '#2dd4bf', shape: 'triangle', label: 'WebSocket' },
  sse:                  { bg: 'bg-indigo-900/40',  text: 'text-indigo-300',  border: 'border-indigo-700',  hex: '#818cf8', shape: 'star',     label: 'SSE' },
  graphql_subscription: { bg: 'bg-pink-900/40',    text: 'text-pink-300',    border: 'border-pink-700',    hex: '#f472b6', shape: 'cross',    label: 'GraphQL Sub' },
  // ── new runtime entities (#946) ───────────────────────────────────────────
  /** Redis pub/sub channels (channel:redis-pubsub:*) */
  redis_pubsub:         { bg: 'bg-red-900/40',     text: 'text-red-300',     border: 'border-red-700',     hex: '#f87171', shape: 'ring',     label: 'Redis P/S' },
  /** Redis Streams (stream:redis:*) */
  'redis-stream':       { bg: 'bg-rose-900/40',    text: 'text-rose-300',    border: 'border-rose-700',    hex: '#fb7185', shape: 'chevron',  label: 'Redis Stream' },
  /** Top-level redis broker protocol (folded view) */
  redis:                { bg: 'bg-red-900/40',     text: 'text-red-300',     border: 'border-red-700',     hex: '#f87171', shape: 'ring',     label: 'Redis' },
  /** Async task queues (dramatiq / RQ / Hangfire / Quartz) */
  'task-queue':         { bg: 'bg-lime-900/40',    text: 'text-lime-300',    border: 'border-lime-700',    hex: '#a3e635', shape: 'clock',    label: 'Task Queue' },
  /** Serverless functions (AWS Lambda / GCF / Azure) */
  serverless:           { bg: 'bg-yellow-900/40',  text: 'text-yellow-300',  border: 'border-yellow-700',  hex: '#fde047', shape: 'bolt',     label: 'Serverless' },
}

export function protocolColor(protocol: TopologyProtocol): ProtocolColorSpec {
  return PROTOCOL_COLORS[protocol]
}
