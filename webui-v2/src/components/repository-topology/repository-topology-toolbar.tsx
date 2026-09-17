import { ArrowDown, ArrowRight, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { RepositoryTopologyPreset } from "@/lib/repository-topology-filters";
import type { RepositoryTopologyDirection } from "@/lib/repository-topology-layout";

const presets: Array<{ id: RepositoryTopologyPreset; label: string }> = [
  { id: "overview", label: "Overview" },
  { id: "dubbo", label: "Dubbo" },
  { id: "message-flow", label: "Messages" },
  { id: "http", label: "HTTP" },
  { id: "requirement-impact", label: "Impact" },
  { id: "incident", label: "Incident" },
  { id: "graph-health", label: "Graph health" },
  { id: "repository-path", label: "Path" },
];

export function RepositoryTopologyToolbar({
  rankdir,
  onRankdirChange,
  onPreset,
  onReset,
}: {
  rankdir: RepositoryTopologyDirection;
  onRankdirChange: (direction: RepositoryTopologyDirection) => void;
  onPreset: (preset: RepositoryTopologyPreset) => void;
  onReset: () => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
      <span className="mr-1 text-xs font-medium text-text-3">Presets</span>
      {presets.map((preset) => (
        <Button key={preset.id} size="sm" variant="ghost" onClick={() => onPreset(preset.id)}>
          {preset.label}
        </Button>
      ))}
      <div className="ml-auto flex items-center gap-1">
        <Button size="sm" variant={rankdir === "LR" ? "secondary" : "ghost"} onClick={() => onRankdirChange("LR")} title="Left to right">
          <ArrowRight size={14} />
        </Button>
        <Button size="sm" variant={rankdir === "TB" ? "secondary" : "ghost"} onClick={() => onRankdirChange("TB")} title="Top to bottom">
          <ArrowDown size={14} />
        </Button>
        <Button size="sm" variant="ghost" onClick={onReset}><RotateCcw size={14} /> Reset</Button>
      </div>
    </div>
  );
}
