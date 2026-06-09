/**
 * Contrast / accessibility tests for repo-color.tsx
 *
 * Verifies the repo-label chip foreground meets WCAG AA (~4.5:1) against its
 * background for EVERY repo hue in BOTH light and dark themes, covering:
 *   - the curated pastel-N / pastel-N-ink token pairs (chip bg = pastel @28%
 *     composited over the theme surface), and
 *   - the hash-derived hslDerived() path used for groups with >8 repos.
 *
 * Run with: npx vitest run src/lib/repo-color.test.ts
 */
import { describe, it, expect } from "vitest";
import { getRepoColor } from "./repo-color";

// --- color math (sRGB relative luminance per WCAG 2.x) --------------------

type RGB = [number, number, number];

function hexToRgb(h: string): RGB {
  const s = h.replace("#", "");
  return [
    parseInt(s.slice(0, 2), 16),
    parseInt(s.slice(2, 4), 16),
    parseInt(s.slice(4, 6), 16),
  ];
}

function lin(c: number): number {
  const x = c / 255;
  return x <= 0.03928 ? x / 12.92 : Math.pow((x + 0.055) / 1.055, 2.4);
}

function luminance([r, g, b]: RGB): number {
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

/** Alpha-composite `top` (opaque rgb) at `alpha` over opaque `bottom`. */
function composite(top: RGB, bottom: RGB, alpha: number): RGB {
  return [0, 1, 2].map((i) => top[i] * alpha + bottom[i] * (1 - alpha)) as RGB;
}

function contrast(a: RGB, b: RGB): number {
  const la = luminance(a);
  const lb = luminance(b);
  const hi = Math.max(la, lb);
  const lo = Math.min(la, lb);
  return (hi + 0.05) / (lo + 0.05);
}

/** Parse `hsl(h, s%, l%[, a])` to opaque RGB (alpha ignored — handled separately). */
function hslStrToRgb(str: string): { rgb: RGB; alpha: number } {
  const m = str.match(/hsl\(\s*([\d.]+)\s*,\s*([\d.]+)%\s*,\s*([\d.]+)%\s*(?:,\s*([\d.]+)\s*)?\)/);
  if (!m) throw new Error("not an hsl() string: " + str);
  const h = parseFloat(m[1]);
  const s = parseFloat(m[2]) / 100;
  const l = parseFloat(m[3]) / 100;
  const alpha = m[4] !== undefined ? parseFloat(m[4]) : 1;
  const k = (n: number) => (n + h / 30) % 12;
  const a = s * Math.min(l, 1 - l);
  const f = (n: number) => l - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)));
  return { rgb: [f(0) * 255, f(8) * 255, f(4) * 255] as RGB, alpha };
}

const AA = 4.5;

// Theme surfaces the chip sits on (from tokens.css).
const SURFACE_LIGHT: RGB = [255, 255, 255]; // --surface
const SURFACE_DARK: RGB = [22, 25, 31]; //   approx dark card surface

// --- curated token table (mirrors tokens.css) -----------------------------
// Keep in sync with src/styles/tokens.css :root / [data-theme="dark"].

const CURATED_LIGHT = {
  pastel: [
    "#a5cde3", "#b3dec3", "#fac8a0", "#f0b1bd", "#cdb9e5",
    "#efd99a", "#b0d3b3", "#f4bfca", "#b8c0e0", "#f7caa3",
  ],
  ink: [
    "#366f8f", "#40765b", "#965d2c", "#9b5266", "#735ba7",
    "#816928", "#4b734f", "#96586b", "#59659e", "#8c632f",
  ],
};

const CURATED_DARK = {
  pastel: [
    "#6f9bb3", "#7da689", "#c4956d", "#b8838f", "#9686b3",
    "#b8a565", "#82a384", "#c08699", "#8a91b3", "#c4986f",
  ],
  ink: [
    "#95c1d9", "#a4cfb0", "#e8b893", "#dcaab5", "#bcabd9",
    "#dcc88b", "#a9caaa", "#e1aabb", "#b1b8d9", "#e8be93",
  ],
};

// Chip background = pastel @28% over surface (see RepoChip / CURATED).
const BG_MIX_ALPHA = 0.28;

describe("repo chip contrast — curated palette", () => {
  for (const [theme, table, surface] of [
    ["light", CURATED_LIGHT, SURFACE_LIGHT],
    ["dark", CURATED_DARK, SURFACE_DARK],
  ] as const) {
    table.pastel.forEach((p, i) => {
      it(`pastel-${i + 1} ink clears AA in ${theme}`, () => {
        const bg = composite(hexToRgb(p), surface, BG_MIX_ALPHA);
        const fg = hexToRgb(table.ink[i]);
        const ratio = contrast(bg, fg);
        expect(ratio).toBeGreaterThanOrEqual(AA);
      });
    });
  }
});

describe("repo chip contrast — hash-derived (>8 repos) path", () => {
  // hslDerived returns an OPAQUE background, so its contrast is theme-
  // independent. Sample many distinct slugs to cover the golden-ratio wheel.
  const slugs = Array.from({ length: 400 }, (_, n) => `repo-${n}-svc`);

  it("every sampled hash-derived slug clears AA over both surfaces", () => {
    let worst = Infinity;
    for (const slug of slugs) {
      const { background, foreground } = getRepoColor(slug);
      // Only assert the derived (hsl) path here; curated CSS-vars are covered above.
      if (!background.startsWith("hsl")) continue;
      const bgParsed = hslStrToRgb(background);
      const fg = hslStrToRgb(foreground).rgb;
      for (const surface of [SURFACE_LIGHT, SURFACE_DARK]) {
        const bg = composite(bgParsed.rgb, surface, bgParsed.alpha);
        worst = Math.min(worst, contrast(bg, fg));
      }
    }
    expect(worst).toBeGreaterThanOrEqual(AA);
  });
});
