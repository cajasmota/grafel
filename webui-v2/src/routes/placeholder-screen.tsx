/* ============================================================
   PlaceholderScreen — the slot every screen ticket fills in.

   Renders the screen title + a short description + a "next steps"
   note pointing at the handoff doc. Screen-building agents replace
   the body with the real surface; the route + shell wiring already
   exists, so they only build the screen content.
   ============================================================ */

import { Card, CardBody } from "@/components/ui";

export interface PlaceholderScreenProps {
  title: string;
  description: string;
  /** Handoff doc filename that specs this screen. */
  doc: string;
}

export function PlaceholderScreen({ title, description, doc }: PlaceholderScreenProps) {
  return (
    <div className="p-6 max-w-3xl">
      <h1 className="text-2xl font-semibold text-text">{title}</h1>
      <p className="mt-1 text-md text-text-3">{description}</p>
      <Card className="mt-5">
        <CardBody>
          <p className="text-md text-text-2">
            This screen is scaffolded but not yet implemented. Build it per{" "}
            <span className="font-mono text-text">docs/screens/{doc}</span> and its matching prototype, consuming
            primitives from <span className="font-mono text-text">@/components/ui</span> and the chrome from{" "}
            <span className="font-mono text-text">@/layouts/app-shell</span>.
          </p>
        </CardBody>
      </Card>
    </div>
  );
}
