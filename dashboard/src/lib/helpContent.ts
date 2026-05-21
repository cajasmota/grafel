/**
 * helpContent.ts — Static content for the /help surface.
 *
 * Sections: About, Tour, Glossary, Tips & Tricks, FAQ, Contact
 * All data is static — no API calls needed for the content itself.
 */

// ─────────────────────────────────────────────────────────────────────────────
// Tour
// ─────────────────────────────────────────────────────────────────────────────

export interface TourSlide {
  id: string
  title: string
  description: string
  route: string
  /** relative path from project root, used for <img src> */
  screenshotSrc?: string
}

export const TOUR_SLIDES: TourSlide[] = [
  {
    id: 'graph',
    title: 'Graph — dependency map',
    description:
      'The Graph surface renders every module, service, and package as a node in an interactive force-directed layout. Zoom, pan, and click any node to open the Entity Inspector with metadata, owners, and direct dependencies.',
    route: '/graph/fixture-a',
  },
  {
    id: 'flows',
    title: 'Flows — event & message paths',
    description:
      'Flows traces how messages and events propagate between producers, topics, and consumers across your codebase. Each row shows a publish→subscribe chain with annotated protocol, serialization format, and owning backend.',
    route: '/flows/fixture-a',
  },
  {
    id: 'topology',
    title: 'Topology — broker layout',
    description:
      'Topology maps the physical broker topology — queues, exchanges, and channels — overlaid with the logical producers and consumers that bind to them. Orphan publishers and subscribers are surfaced in dedicated tabs.',
    route: '/topology/fixture-a',
  },
  {
    id: 'paths',
    title: 'Paths — API explorer',
    description:
      'Paths lists every HTTP endpoint extracted from your codebase. Select a path to inspect its handler chain, request/response shapes, and the internal services it calls.',
    route: '/paths/fixture-a',
  },
  {
    id: 'docs',
    title: 'Docs — generated documentation',
    description:
      'Docs renders markdown documentation generated from code comments and module structure. Browse by directory tree or search within the full doc corpus.',
    route: '/docs/fixture-a',
  },
  {
    id: 'quality',
    title: 'Quality — health-score history',
    description:
      'Quality tracks graph health over time: orphan counts, extraction coverage, and dependency-cycle metrics. Spot regressions before they accumulate.',
    route: '/quality/fixture-a',
  },
  {
    id: 'diagnostics',
    title: 'Diagnostics — indexer health',
    description:
      'Diagnostics gives a real-time view of the indexer: last-run timestamps, per-language coverage, repair queue depth, and daemon resource usage.',
    route: '/diagnostics',
  },
  {
    id: 'system',
    title: 'System — daemon control',
    description:
      'System lets you start, stop, restart, and inspect the background daemon process. View live logs, check memory and CPU, and download diagnostic bundles.',
    route: '/system',
  },
]

// ─────────────────────────────────────────────────────────────────────────────
// Glossary
// ─────────────────────────────────────────────────────────────────────────────

export interface GlossaryTerm {
  term: string
  definition: string
  /** Optional surface to navigate to for more context */
  relatedRoute?: string
}

export const GLOSSARY_TERMS: GlossaryTerm[] = [
  {
    term: 'Orphan',
    definition:
      'A publisher or subscriber that emits or consumes messages on a topic/queue for which no matching counterpart was found in the codebase. Orphans indicate dead code, missing repositories, or extraction gaps.',
    relatedRoute: '/topology/fixture-a',
  },
  {
    term: 'owning_backend',
    definition:
      'The service or repository declared as the authoritative owner of a message topic, queue, or API path. Used to route questions and assign responsibility during incident triage.',
  },
  {
    term: 'Process flow',
    definition:
      'A directed chain of producers → broker topics → consumers that collectively implement a business process (e.g., "order placement"). Composed from individual flow edges by archigraph during indexing.',
    relatedRoute: '/flows/fixture-a',
  },
  {
    term: 'Extraction coverage',
    definition:
      'The percentage of source files that archigraph successfully parsed and converted to graph nodes and edges. Coverage below ~90 % often signals missing language extractors or parse errors.',
    relatedRoute: '/diagnostics',
  },
  {
    term: 'Repair queue',
    definition:
      'A set of extraction items that failed on first pass and are queued for a re-attempt with a slower, more thorough analysis strategy. Visible in the Pending surface.',
    relatedRoute: '/pending/fixture-a',
  },
  {
    term: 'Enrichment',
    definition:
      'A post-extraction pass that augments raw graph nodes with additional metadata — inferred ownership, semantic type, documentation strings, and dependency weights.',
  },
  {
    term: 'Community',
    definition:
      'A cluster of closely connected nodes identified by the Louvain community-detection algorithm. Communities are colour-coded in the Graph surface to help identify architectural boundaries.',
    relatedRoute: '/graph/fixture-a',
  },
  {
    term: 'Group',
    definition:
      'A named collection of repositories indexed together. Groups let you switch context between, for example, a monorepo and a set of microservices without re-indexing.',
  },
  {
    term: 'Daemon',
    definition:
      'The background Go process (archigraph daemon) that watches files, runs the indexer, and serves the REST/MCP API. The dashboard is a static SPA that talks to the daemon over HTTP.',
    relatedRoute: '/system',
  },
  {
    term: 'Health score',
    definition:
      'A composite metric (0–100) computed from orphan ratio, extraction coverage, cycle count, and repair-queue depth. Tracked over time on the Quality surface.',
    relatedRoute: '/quality/fixture-a',
  },
  {
    term: 'MCP',
    definition:
      'Model Context Protocol — a standardised API that lets AI assistants (Claude Code, Cursor, Windsurf) query archigraph graph data directly via tool calls during coding sessions.',
    relatedRoute: '/mcp-activity',
  },
  {
    term: 'Pattern',
    definition:
      'A recurring structural motif detected across the graph (e.g., fan-out queues, hub services, circular imports). Patterns are surfaced on the Patterns surface to guide refactoring.',
    relatedRoute: '/patterns/fixture-a',
  },
]

// ─────────────────────────────────────────────────────────────────────────────
// Tips & Tricks
// ─────────────────────────────────────────────────────────────────────────────

export interface Tip {
  id: string
  title: string
  description: string
  shortcut?: string
}

export const TIPS: Tip[] = [
  {
    id: 'cmd-k',
    title: 'Command Palette',
    description:
      'Open the command palette to jump to any surface, search nodes, toggle the theme, or trigger a graph refresh — all from the keyboard.',
    shortcut: '⌘K',
  },
  {
    id: 'go-home',
    title: 'Go home',
    description:
      'Press g then h anywhere outside an input field to navigate back to the home / landing page.',
    shortcut: 'g h',
  },
  {
    id: 'help-shortcut',
    title: 'Open Help',
    description: 'Press ⌘? (Cmd+Shift+/) to open this Help surface from anywhere in the app.',
    shortcut: '⌘?',
  },
  {
    id: 'group-switcher',
    title: 'Switch groups quickly',
    description:
      'Use the group switcher in the top bar to move between indexed repository groups without leaving the current surface.',
  },
  {
    id: 'entity-inspector',
    title: 'Entity Inspector',
    description:
      'Click any node in the Graph surface to open the Entity Inspector panel. It shows owners, dependencies, and outgoing edges.',
    shortcut: 'click node',
  },
  {
    id: 'edge-filters',
    title: 'Filter edge kinds',
    description:
      'In the Graph toolbar, use the edge-kind filter chips to show only the dependency types you care about (imports, calls, publishes, subscribes).',
  },
  {
    id: 'dark-mode',
    title: 'Theme toggle',
    description:
      'Toggle between light and dark mode using the sun/moon icon in the top bar, or via the Command Palette ("Toggle theme").',
  },
  {
    id: 'mcp-setup',
    title: 'Connect your AI assistant',
    description:
      'Go to Settings → MCP Configuration to get the config block for Claude Code, Cursor, or Windsurf. Paste it into your editor to unlock graph-aware AI assistance.',
    shortcut: '/settings',
  },
]

// ─────────────────────────────────────────────────────────────────────────────
// FAQ
// ─────────────────────────────────────────────────────────────────────────────

export interface FaqItem {
  id: string
  question: string
  answer: string
}

export const FAQ_ITEMS: FaqItem[] = [
  {
    id: 'indexing-slow',
    question: 'Why does indexing take so long on the first run?',
    answer:
      'On the first index archigraph parses every source file to build the graph from scratch. Depending on repo size this can take 30 seconds to several minutes. Subsequent runs are incremental — only changed files are re-processed, so they complete in seconds.',
  },
  {
    id: 'add-repo',
    question: 'How do I add a repository to archigraph?',
    answer:
      'Edit ~/.archigraph/config.yaml and add the repository path under the appropriate group. Then run archigraph index <group> or trigger a refresh from the System panel. The daemon picks up config changes on the next index.',
  },
  {
    id: 'graph-empty',
    question: 'Why is my graph empty?',
    answer:
      'A few common causes: (1) the daemon has not finished indexing yet — check the System panel for progress; (2) no repositories are configured for the selected group; (3) the selected group name does not match what is in config.yaml. The Diagnostics surface shows extraction coverage per language.',
  },
  {
    id: 'orphans',
    question: 'Why do I have so many orphan publishers / subscribers?',
    answer:
      'Orphans appear when archigraph can find one side of a message exchange (e.g., a Kafka producer) but not the consumer, or vice versa. Common causes are: repos not yet indexed, dynamically generated topic names, or consumers written in a language without an extractor.',
  },
  {
    id: 'mcp-not-working',
    question: 'My AI assistant cannot find the archigraph MCP tools.',
    answer:
      'Make sure the daemon is running (archigraph daemon start) and that the MCP config block in Settings → MCP Configuration is pasted into the correct editor config file. The daemon must be reachable on the configured port (default 47274).',
  },
  {
    id: 'health-score-low',
    question: 'What does a low health score mean?',
    answer:
      'A low health score (below 70) usually means high orphan ratio, low extraction coverage, or many repair-queue items pending. Check the Quality surface trend and the Diagnostics surface to pinpoint the biggest contributor.',
  },
  {
    id: 'languages',
    question: 'Which languages does archigraph support?',
    answer:
      'Go, TypeScript/JavaScript, Python, Java, Kotlin, Rust, Ruby, PHP, C#, Swift, Dart, Elixir, Scala, Haskell, Clojure, and more. See the Diagnostics surface for extraction coverage per language in your current group.',
  },
  {
    id: 'data-privacy',
    question: 'Does archigraph send my source code anywhere?',
    answer:
      'No. archigraph runs entirely locally. The daemon reads your source files to build a graph, but neither the code nor the graph is sent to any external server. Optional anonymous telemetry (off by default) only sends aggregate counts — never code or file paths.',
  },
]
