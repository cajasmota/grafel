/* ============================================================
   ErrorConstellation — broken-constellation illustration for the
   Errors screen. Each error variant gets a distinct tint + overlay.
   Mirrors the `ConstellationArt` component from the design prototype.

   Pure presentational; no business logic; tokens only.
   ============================================================ */

export type ErrorVariant =
  | "notFound"
  | "groupGone"
  | "daemonDown"
  | "indexerFailed"
  | "upgrading"
  | "offline";

interface DotSpec {
  x: number;
  y: number;
  r?: number;
  color: string;
  opacity?: number;
}

interface ArtConfig {
  edgeColor: string;
  dashed: boolean;
  dots: DotSpec[];
  edges: [number, number][];
  overlay?: React.ReactNode;
}

const ART_CONFIG: Record<ErrorVariant, ArtConfig> = {
  notFound: {
    edgeColor: "var(--border-strong)",
    dashed: true,
    dots: [
      { x: 60, y: 38, r: 3.0, color: "var(--pastel-1)" },
      { x: 110, y: 28, r: 3.4, color: "var(--pastel-5)" },
      { x: 160, y: 42, r: 3.0, color: "var(--pastel-2)" },
      { x: 80, y: 80, r: 3.2, color: "var(--pastel-3)" },
      { x: 140, y: 84, r: 3.0, color: "var(--pastel-9)" },
    ],
    edges: [
      [0, 1],
      [1, 2],
      [3, 4],
      [1, 3],
    ],
  },
  groupGone: {
    edgeColor: "var(--text-4)",
    dashed: true,
    dots: [
      { x: 70, y: 50, r: 2.4, color: "var(--text-4)", opacity: 0.4 },
      { x: 110, y: 32, r: 2.8, color: "var(--text-4)", opacity: 0.4 },
      { x: 150, y: 50, r: 2.4, color: "var(--text-4)", opacity: 0.4 },
      { x: 90, y: 82, r: 2.4, color: "var(--text-4)", opacity: 0.4 },
      { x: 130, y: 82, r: 2.4, color: "var(--text-4)", opacity: 0.4 },
    ],
    edges: [
      [0, 1],
      [1, 2],
      [0, 3],
      [2, 4],
      [3, 4],
    ],
  },
  daemonDown: {
    edgeColor: "color-mix(in srgb, var(--danger) 35%, transparent)",
    dashed: true,
    dots: [
      { x: 60, y: 38, r: 3.0, color: "var(--pastel-1)" },
      { x: 110, y: 28, r: 3.4, color: "var(--danger)" },
      { x: 160, y: 42, r: 3.0, color: "var(--pastel-2)" },
      { x: 80, y: 80, r: 3.0, color: "var(--pastel-3)" },
      { x: 140, y: 84, r: 3.0, color: "var(--pastel-9)" },
    ],
    edges: [
      [0, 1],
      [1, 2],
      [3, 4],
    ],
    overlay: (
      <circle cx="110" cy="28" r="9" fill="none" stroke="var(--danger)" strokeWidth="0.6" opacity="0.4">
        <animate attributeName="r" from="6" to="14" dur="1.6s" repeatCount="indefinite" />
        <animate attributeName="opacity" from="0.45" to="0" dur="1.6s" repeatCount="indefinite" />
      </circle>
    ),
  },
  indexerFailed: {
    edgeColor: "var(--border)",
    dashed: false,
    dots: [
      { x: 60, y: 38, r: 3.0, color: "var(--pastel-1)" },
      { x: 110, y: 28, r: 3.4, color: "var(--pastel-5)" },
      { x: 160, y: 42, r: 3.0, color: "var(--pastel-2)" },
      { x: 80, y: 80, r: 3.0, color: "var(--pastel-3)" },
      { x: 140, y: 84, r: 3.0, color: "var(--danger)" },
    ],
    edges: [
      [0, 1],
      [1, 2],
      [0, 3],
    ],
    overlay: (
      <g transform="translate(132,76)">
        <circle cx="0" cy="0" r="10" fill="var(--bg)" stroke="var(--danger)" strokeWidth="1.2" />
        <path
          d="M-4 -4 L 4 4 M 4 -4 L -4 4"
          stroke="var(--danger)"
          strokeWidth="1.4"
          strokeLinecap="round"
        />
      </g>
    ),
  },
  upgrading: {
    edgeColor: "var(--border)",
    dashed: false,
    dots: [
      { x: 60, y: 38, r: 2.8, color: "var(--pastel-1)", opacity: 0.5 },
      { x: 110, y: 28, r: 3.4, color: "var(--accent)" },
      { x: 160, y: 42, r: 2.8, color: "var(--pastel-2)", opacity: 0.5 },
      { x: 80, y: 80, r: 2.8, color: "var(--pastel-3)", opacity: 0.5 },
      { x: 140, y: 84, r: 2.8, color: "var(--pastel-9)", opacity: 0.5 },
    ],
    edges: [
      [0, 1],
      [1, 2],
      [1, 3],
      [1, 4],
    ],
    overlay: (
      <circle r="10" cx="110" cy="28" fill="none" stroke="var(--accent)" strokeWidth="1" strokeDasharray="32" strokeLinecap="round">
        <animateTransform
          attributeName="transform"
          type="rotate"
          from="0 110 28"
          to="360 110 28"
          dur="1.4s"
          repeatCount="indefinite"
        />
      </circle>
    ),
  },
  offline: {
    edgeColor: "var(--text-4)",
    dashed: true,
    dots: [
      { x: 60, y: 38, r: 2.6, color: "var(--text-4)", opacity: 0.45 },
      { x: 110, y: 28, r: 3.0, color: "var(--text-4)", opacity: 0.45 },
      { x: 160, y: 42, r: 2.6, color: "var(--text-4)", opacity: 0.45 },
      { x: 80, y: 80, r: 2.6, color: "var(--text-4)", opacity: 0.45 },
      { x: 140, y: 84, r: 2.6, color: "var(--text-4)", opacity: 0.45 },
    ],
    edges: [
      [0, 1],
      [1, 2],
      [3, 4],
      [0, 3],
      [2, 4],
    ],
  },
};

export interface ErrorConstellationProps {
  variant: ErrorVariant;
  className?: string;
}

export function ErrorConstellation({ variant, className }: ErrorConstellationProps) {
  const config = ART_CONFIG[variant] ?? ART_CONFIG.notFound;

  return (
    <svg
      viewBox="0 0 220 120"
      width="220"
      height="120"
      className={className}
      aria-hidden="true"
    >
      <defs>
        <linearGradient id="er-grad" x1="0" x2="1" y1="0" y2="1">
          <stop offset="0" stopColor="var(--accent)" stopOpacity="0.6" />
          <stop offset="1" stopColor="var(--accent-strong)" />
        </linearGradient>
      </defs>

      {/* edges */}
      <g
        stroke={config.edgeColor}
        strokeWidth="0.8"
        fill="none"
        strokeDasharray={config.dashed ? "3 3" : undefined}
      >
        {config.edges.map(([a, b], i) => (
          <line
            key={i}
            x1={config.dots[a].x}
            y1={config.dots[a].y}
            x2={config.dots[b].x}
            y2={config.dots[b].y}
          />
        ))}
      </g>

      {/* dots */}
      <g>
        {config.dots.map((d, i) => (
          <circle
            key={i}
            cx={d.x}
            cy={d.y}
            r={d.r ?? 3.2}
            fill={d.color}
            opacity={d.opacity ?? 1}
          />
        ))}
      </g>

      {/* variant-specific overlay */}
      {config.overlay}
    </svg>
  );
}
