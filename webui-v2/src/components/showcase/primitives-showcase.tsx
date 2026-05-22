/* ============================================================
   PrimitivesShowcase — living catalog of the component library.

   Renders every primitive in the Lego layer so the design quality
   bar is visible and verifiable. Also exercises the theme/palette/
   density knobs to prove tokens drive the whole tree.
   Route: /showcase
   ============================================================ */

import {
  Button,
  SearchInput,
  Kbd,
  Badge,
  Card,
  CardHeader,
  CardTitle,
  CardBody,
  Pill,
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  InfoLabel,
  Popover,
  PopoverTrigger,
  PopoverContent,
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogTitle,
  DialogDescription,
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@/components/ui";
import { useAppStore } from "@/store/use-app-store";
import { CommandPalette } from "@/components/chrome/command-palette";

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-semibold text-text">{title}</h2>
      <div className="flex flex-wrap items-center gap-3">{children}</div>
    </section>
  );
}

export default function PrimitivesShowcase() {
  const { theme, palette, density, toggleTheme, setPalette, setDensity, setCommandOpen } = useAppStore();

  return (
    <div className="h-full ag-scroll bg-bg">
      <div className="mx-auto max-w-4xl px-6 py-10 space-y-8">
        <header>
          <h1 className="text-3xl font-semibold text-text">Component library</h1>
          <p className="mt-1 text-md text-text-3">
            The Lego primitives. Every value below comes from{" "}
            <span className="font-mono text-text">tokens.css</span> — flip the knobs to prove it.
          </p>
        </header>

        <Section title="Appearance knobs (token-driven)">
          <Button variant="secondary" onClick={toggleTheme}>
            Theme: {theme}
          </Button>
          <Button variant="secondary" onClick={() => setPalette(palette === "cool" ? "warm" : "cool")}>
            Palette: {palette}
          </Button>
          <Button
            variant="secondary"
            onClick={() => setDensity(density === "comfortable" ? "compact" : "comfortable")}
          >
            Density: {density}
          </Button>
        </Section>

        <Section title="Button">
          <Button variant="primary">Primary</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="danger">Danger</Button>
          <Button size="sm">Small</Button>
          <Button disabled>Disabled</Button>
        </Section>

        <Section title="Pill">
          <Pill>Inactive</Pill>
          <Pill active>Active</Pill>
          <Pill count={12}>Filters</Pill>
        </Section>

        <Section title="Badge (color + label, never color-only)">
          <Badge>neutral</Badge>
          <Badge tone="accent">accent</Badge>
          <Badge tone="success">success</Badge>
          <Badge tone="warning">warning</Badge>
          <Badge tone="danger">danger</Badge>
          <Badge tone="info">info</Badge>
          <Badge dot="var(--pastel-3)">TypeScript</Badge>
        </Section>

        <Section title="Input">
          <SearchInput placeholder="Search nodes, paths, flows…" shortcut="/" className="max-w-xs" />
        </Section>

        <Section title="Kbd">
          <Kbd>⌘K</Kbd>
          <Kbd>G</Kbd>
          <span className="text-md text-text-2">
            Press <Kbd>/</Kbd> to search
          </span>
        </Section>

        <Section title="Tooltip / InfoLabel">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="secondary">Hover me</Button>
            </TooltipTrigger>
            <TooltipContent>A token-styled Radix tooltip.</TooltipContent>
          </Tooltip>
          <span className="text-md text-text-2">
            <InfoLabel label="Fidelity" hint="Group health, 0–100. High = good. Replaces legacy bug-rate." />
          </span>
        </Section>

        <Section title="Popover">
          <Popover>
            <PopoverTrigger asChild>
              <Button variant="secondary">Open popover</Button>
            </PopoverTrigger>
            <PopoverContent align="start">
              <p className="text-md font-medium text-text">Communities</p>
              <p className="mt-1 text-sm text-text-3">Floating Radix popover, restyled to tokens.</p>
            </PopoverContent>
          </Popover>
        </Section>

        <Section title="Dialog">
          <Dialog>
            <DialogTrigger asChild>
              <Button variant="secondary">Open dialog</Button>
            </DialogTrigger>
            <DialogContent>
              <DialogTitle>Confirm action</DialogTitle>
              <DialogDescription>A centered modal with focus trap and scrim, all from tokens.</DialogDescription>
            </DialogContent>
          </Dialog>
        </Section>

        <Section title="Tabs">
          <Tabs defaultValue="a" className="w-full">
            <TabsList>
              <TabsTrigger value="a">Routes</TabsTrigger>
              <TabsTrigger value="b">Schemas</TabsTrigger>
              <TabsTrigger value="c">Orphans</TabsTrigger>
            </TabsList>
            <TabsContent value="a" className="pt-3 text-md text-text-2">
              Routes content.
            </TabsContent>
            <TabsContent value="b" className="pt-3 text-md text-text-2">
              Schemas content.
            </TabsContent>
            <TabsContent value="c" className="pt-3 text-md text-text-2">
              Orphans content.
            </TabsContent>
          </Tabs>
        </Section>

        <Section title="Card">
          <Card className="w-full max-w-sm">
            <CardHeader>
              <CardTitle>Card title</CardTitle>
            </CardHeader>
            <CardBody>
              <p className="text-md text-text-2">Surface, border, radius, and shadow all bound to tokens.</p>
            </CardBody>
          </Card>
        </Section>

        <Section title="Command palette">
          <Button variant="secondary" onClick={() => setCommandOpen(true)}>
            Open ⌘K palette
          </Button>
          <CommandPalette />
        </Section>
      </div>
    </div>
  );
}
