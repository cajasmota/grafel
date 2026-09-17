import type {
  RepositoryChannel,
  RepositoryEvidence,
  RepositoryTopologyFilters,
} from "@/data/types";
import { Input, SearchInput } from "@/components/ui/input";

const channels: RepositoryChannel[] = ["dubbo", "http", "kafka", "rabbitmq", "other"];
const evidence: RepositoryEvidence[] = ["confirmed", "inferred", "dangling", "ambiguous", "external"];

function ToggleList<T extends string>({ values, selected, onChange }: { values: T[]; selected: T[]; onChange: (values: T[]) => void }) {
  return <div className="flex flex-wrap gap-1">{values.map((value) => <label key={value} className="flex items-center gap-1 rounded border border-border px-1.5 py-1 text-[11px] text-text-2"><input type="checkbox" checked={selected.includes(value)} onChange={() => onChange(selected.includes(value) ? selected.filter((item) => item !== value) : [...selected, value])} />{value}</label>)}</div>;
}

export function RepositoryTopologyFilters({ filters, repositories, onChange }: {
  filters: RepositoryTopologyFilters;
  repositories: string[];
  onChange: (patch: Partial<RepositoryTopologyFilters>) => void;
}) {
  const pathMode = Boolean(filters.source || filters.target);
  const focusMode = Boolean(filters.focus);
  return (
    <aside className="w-72 shrink-0 overflow-y-auto border-r border-border bg-surface px-3 py-3">
      <div className="space-y-4">
        <label className="block text-xs font-medium text-text-2">Search<SearchInput className="mt-1" value={filters.search} onChange={(event) => onChange({ search: event.target.value })} placeholder="Contract, service, repository" /></label>
        <section><h3 className="mb-1 text-xs font-medium text-text-2">Channels</h3><ToggleList values={channels} selected={filters.channels} onChange={(next) => onChange({ channels: next })} /></section>
        <section><h3 className="mb-1 text-xs font-medium text-text-2">Evidence</h3><ToggleList values={evidence} selected={filters.evidence} onChange={(next) => onChange({ evidence: next })} /></section>
        <label className="block text-xs font-medium text-text-2">Repositories<select multiple className="mt-1 h-28 w-full rounded border border-border bg-surface-1 p-1 text-xs" value={filters.repos} onChange={(event) => onChange({ repos: Array.from(event.target.selectedOptions, (option) => option.value) })}>{repositories.map((repository) => <option key={repository}>{repository}</option>)}</select></label>
        <label className="block text-xs font-medium text-text-2">Focus repository<select disabled={pathMode} className="mt-1 h-8 w-full rounded border border-border bg-surface-1 px-2 text-xs disabled:opacity-50" value={filters.focus} onChange={(event) => onChange({ focus: event.target.value })}><option value="">All connected repositories</option>{repositories.map((repository) => <option key={repository}>{repository}</option>)}</select></label>
        <div className="grid grid-cols-2 gap-2"><label className="text-xs font-medium text-text-2">Direction<select disabled={!focusMode} className="mt-1 h-8 w-full rounded border border-border bg-surface-1 px-1 text-xs disabled:opacity-50" value={filters.direction} onChange={(event) => onChange({ direction: event.target.value as RepositoryTopologyFilters["direction"] })}><option value="both">Both</option><option value="inbound">Inbound</option><option value="outbound">Outbound</option></select></label><label className="text-xs font-medium text-text-2">Depth<select disabled={!focusMode} className="mt-1 h-8 w-full rounded border border-border bg-surface-1 px-1 text-xs disabled:opacity-50" value={filters.depth} onChange={(event) => onChange({ depth: Number(event.target.value) as 1 | 2 | 3 })}><option value="1">1 hop</option><option value="2">2 hops</option><option value="3">3 hops</option></select></label></div>
        <div className="grid grid-cols-2 gap-2"><label className="text-xs font-medium text-text-2">Path source<select disabled={focusMode} className="mt-1 h-8 w-full rounded border border-border bg-surface-1 px-1 text-xs disabled:opacity-50" value={filters.source} onChange={(event) => onChange({ source: event.target.value })}><option value="">Source</option>{repositories.map((repository) => <option key={repository}>{repository}</option>)}</select></label><label className="text-xs font-medium text-text-2">Path target<select disabled={focusMode} className="mt-1 h-8 w-full rounded border border-border bg-surface-1 px-1 text-xs disabled:opacity-50" value={filters.target} onChange={(event) => onChange({ target: event.target.value })}><option value="">Target</option>{repositories.map((repository) => <option key={repository}>{repository}</option>)}</select></label></div>
        <label className="block text-xs font-medium text-text-2">Minimum relationships<Input className="mt-1" type="number" min={1} value={filters.minCount} onChange={(event) => onChange({ minCount: Math.max(1, Number(event.target.value) || 1) })} /></label>
      </div>
    </aside>
  );
}
