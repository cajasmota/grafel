/* ============================================================
   lib/graph-colors.ts — color helpers for the cosmos.gl canvas.

   CRITICAL LESSON PORTED FROM v1 (#1392): cosmos.gl 2.6.4 reads the
   per-point / per-link color attribute as a RAW float vec4 and assigns it
   straight to the fragment color — it does NOT divide by 255. So colors
   MUST be uploaded in the 0..1 float range. Any channel > 1 clamps to 1.0
   in the shader → every node renders WHITE. We keep the human-friendly
   0–255 parse space and normalize to 0..1 only when writing the GPU buffer
   (writeNormalizedRGBA).

   Colors are sourced from tokens.css (the source-of-truth) via the
   --pastel-N / --pastel-N-ink categorical scale, resolved at runtime so the
   theme/palette switches flow through automatically.
   ============================================================ */

export type RGBA = [number, number, number, number]; // rgb 0–255, a 0–1

/** Number of pastel categorical slots in tokens.css (--pastel-1 … --pastel-10). */
export const PASTEL_SCALE_SIZE = 10;

const SLATE_500: RGBA = [100, 116, 139, 1];

/** Parse #rrggbb / #rgb / rgb()/rgba() into [r,g,b,a] (rgb 0-255, a 0-1). */
export function parseColor(c: string | null | undefined): RGBA {
  if (!c || typeof c !== "string") return SLATE_500;
  const s = c.trim();
  if (s.startsWith("#")) {
    let hex = s.slice(1);
    if (hex.length === 3) hex = hex.split("").map((ch) => ch + ch).join("");
    const r = parseInt(hex.slice(0, 2), 16);
    const g = parseInt(hex.slice(2, 4), 16);
    const b = parseInt(hex.slice(4, 6), 16);
    const a = hex.length >= 8 ? parseInt(hex.slice(6, 8), 16) / 255 : 1;
    if (Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b)) return SLATE_500;
    return [r, g, b, a];
  }
  const m = s.match(/rgba?\(([^)]+)\)/);
  if (m) {
    const parts = m[1].split(",").map((p) => parseFloat(p.trim()));
    return [parts[0] ?? 0, parts[1] ?? 0, parts[2] ?? 0, parts[3] ?? 1];
  }
  return SLATE_500;
}

/**
 * Write an RGBA (rgb 0–255, a 0–1) into a packed GPU buffer at quad index i,
 * NORMALISING rgb to the 0..1 range cosmos.gl's shaders expect. THE fix.
 */
export function writeNormalizedRGBA(out: Float32Array, i: number, rgba: RGBA): void {
  out[i * 4] = rgba[0] / 255;
  out[i * 4 + 1] = rgba[1] / 255;
  out[i * 4 + 2] = rgba[2] / 255;
  out[i * 4 + 3] = rgba[3];
}

/**
 * Resolve the pastel categorical scale (and ink variants) from tokens.css at
 * runtime. Re-read on theme change so the dark-mode desaturated pastels and the
 * warm-palette overlay both flow through. Returns 0–255 RGBA arrays.
 */
export function readPastelScale(root: HTMLElement = document.documentElement): {
  fill: RGBA[];
  ink: RGBA[];
} {
  const style = getComputedStyle(root);
  const fill: RGBA[] = [];
  const ink: RGBA[] = [];
  for (let i = 1; i <= PASTEL_SCALE_SIZE; i++) {
    fill.push(parseColor(style.getPropertyValue(`--pastel-${i}`)));
    ink.push(parseColor(style.getPropertyValue(`--pastel-${i}-ink`)));
  }
  return { fill, ink };
}

/** Pick a pastel slot (1-based color index) from a resolved scale, wrapping. */
export function pastelAt(scale: RGBA[], colorIndex: number): RGBA {
  if (scale.length === 0) return SLATE_500;
  const idx = ((colorIndex - 1) % scale.length + scale.length) % scale.length;
  return scale[idx];
}

/**
 * Degree gradient (cool indigo → violet → pink → warm yellow), ported from the
 * v1 Silk Road ramp. Used for the "degree / connections" color mode. The cool
 * floor is kept dark/saturated on purpose — cosmos.gl blends additively so a
 * dark floor accumulates toward color rather than clipping to white.
 */
const DEGREE_STOPS: RGBA[] = [
  [49, 46, 129, 1], // indigo-900
  [124, 58, 237, 1], // violet-600
  [219, 39, 119, 1], // pink-600
  [250, 204, 21, 1], // yellow-400
];

export function degreeColor(t: number): RGBA {
  const x = Math.max(0, Math.min(1, t));
  const seg = x * (DEGREE_STOPS.length - 1);
  const i = Math.min(Math.floor(seg), DEGREE_STOPS.length - 2);
  const f = seg - i;
  const a = DEGREE_STOPS[i];
  const b = DEGREE_STOPS[i + 1];
  return [
    a[0] + (b[0] - a[0]) * f,
    a[1] + (b[1] - a[1]) * f,
    a[2] + (b[2] - a[2]) * f,
    1,
  ];
}

/** Cross-repo "bridge" edge color — bright sky so integration points pop.
 *  Distinct + bright in BOTH themes (#1564-1). */
export const CROSS_REPO_EDGE: RGBA = [56, 189, 248, 1];

/**
 * Fix #1564-1 / #1564-2: theme-aware link palette. The legacy SAME_REPO_EDGE
 * slate (71,85,105) was tuned for the LIGHT background; on the near-black dark
 * bg (#020617) it vanished. Link colors must adapt to the active theme AND
 * encode the cross-vs-intra structure so inter-module/inter-repo wiring stands
 * out (#1564-2). We classify every edge into one of three buckets:
 *
 *   • cross-repo  — brightest, a distinct sky/cyan in BOTH themes (bridges).
 *   • cross-module (same repo, different module) — bright + emphasized so the
 *     inter-module structure reads as wiring, not islands.
 *   • intra-module — faded into the background so the structure above pops.
 *
 * On DARK we use LIGHTER colors (links sit on a dark bg); on LIGHT we use
 * DARKER colors (links sit on a light bg). Re-read on theme change by the
 * caller (re-packs link colors), so the toggle flows through live.
 */
export interface LinkPalette {
  crossRepo: RGBA;
  crossModule: RGBA;
  intraModule: RGBA;
}

export function linkPalette(isDark: boolean): LinkPalette {
  // Fix #1566: #1565 made cross edges thick + bright violet — "violet
  // spaghetti" that dominated the canvas. Dial the hue WAY back: cross edges
  // are now only a SLIGHTLY-distinct, more muted tone than intra (a soft
  // sky / lavender-ish slate) so the user can TRACE them on inspection rather
  // than be overwhelmed. Still theme-aware (#1564) + dark-visible.
  // Fix #1599: with real cross-repo edges now present (and rare — 376 of 37k on
  // upvate), the bridge color is the KEY signal and can be a vivid, fully-
  // saturated cyan in both themes without becoming spaghetti. The intra tiers are
  // pushed quieter (lower contrast vs the bg) so the bright bridges clearly own
  // the foreground. This is the chromatic half of the multi-repo emphasis (the
  // opacity/width gaps live in graph-canvas).
  if (isDark) {
    return {
      // vivid sky/cyan — the bridge signal, pops on the near-black bg.
      crossRepo: [56, 211, 255, 1],
      // soft periwinkle — only slightly distinct from intra slate.
      crossModule: [148, 163, 220, 1],
      // dim slate — quiet, recedes well into the dark bg.
      intraModule: [120, 134, 156, 1],
    };
  }
  return {
    // saturated sky/cyan — the bridge signal, pops on the light bg.
    crossRepo: [14, 144, 210, 1],
    // muted indigo — only slightly distinct from the slate intra edges.
    crossModule: [110, 116, 170, 1],
    // light slate — quiet, recedes into the light bg.
    intraModule: [120, 134, 156, 1],
  };
}

/** Same-repo edge color — slate, lifted brighter (#1532-2). Retained for
 *  back-compat; the live canvas now uses linkPalette() (#1564). */
export const SAME_REPO_EDGE: RGBA = [71, 85, 105, 1];
/** Highlighted (focused-neighbor) edge color — amber. */
export const HIGHLIGHT_EDGE: RGBA = [251, 146, 60, 1];

/**
 * Jarvis MCP-query glow color (#1157). A warm amber that pops in BOTH themes
 * against the repo/community pastels and the slate edge mesh, so a node/edge the
 * MCP agent just touched visibly lights up before fading. Same hue as
 * HIGHLIGHT_EDGE for consistency, lifted slightly for the additive glow blend.
 */
export const JARVIS_GLOW: RGBA = [255, 176, 59, 1];

/**
 * Linear-interpolate between two RGBA colors. t in [0,1]. Alpha lerps too.
 * Used by the canvas glow loop to blend a node's base color toward the Jarvis
 * amber at the current pulse intensity.
 */
export function lerpRGBA(a: RGBA, b: RGBA, t: number): RGBA {
  const x = Math.max(0, Math.min(1, t));
  return [
    a[0] + (b[0] - a[0]) * x,
    a[1] + (b[1] - a[1]) * x,
    a[2] + (b[2] - a[2]) * x,
    a[3] + (b[3] - a[3]) * x,
  ];
}
