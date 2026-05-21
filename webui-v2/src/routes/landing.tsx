/* ============================================================
   Landing — group selector + create-group wizard + empty state.
   The entry point; renders WITHOUT the in-group chrome.
   Scaffold only — real group cards + wizard land in the landing ticket.
   ============================================================ */

import { Link } from "react-router-dom";
import { Card, CardBody, Button, Badge } from "@/components/ui";

const DEMO_GROUPS = [
  { id: "demo", name: "demo", repos: 3, entities: 20717, fidelity: 88 },
];

export default function Landing() {
  return (
    <div className="h-full ag-scroll bg-bg">
      <div className="mx-auto max-w-4xl px-6 py-12">
        <h1 className="text-3xl font-semibold text-text">Your groups</h1>
        <p className="mt-1 text-md text-text-3">Pick a group to explore its knowledge graph, or create a new one.</p>

        <div className="mt-8 grid grid-cols-1 sm:grid-cols-2 gap-4">
          {DEMO_GROUPS.map((g) => (
            <Link key={g.id} to={`/g/${g.id}/graph`} className="block">
              <Card className="hover:shadow-[var(--shadow-2)] transition-shadow">
                <CardBody>
                  <div className="flex items-center justify-between">
                    <span className="font-mono text-md text-text">{g.name}</span>
                    <Badge tone={g.fidelity >= 80 ? "success" : g.fidelity >= 50 ? "warning" : "danger"}>
                      {g.fidelity}% fidelity
                    </Badge>
                  </div>
                  <div className="mt-3 flex gap-4 text-sm text-text-3 tabular-nums">
                    <span>{g.repos} repos</span>
                    <span>{g.entities.toLocaleString()} entities</span>
                  </div>
                </CardBody>
              </Card>
            </Link>
          ))}

          <Card className="border-dashed grid place-items-center min-h-[120px]">
            <Button variant="secondary">+ Create group</Button>
          </Card>
        </div>
      </div>
    </div>
  );
}
