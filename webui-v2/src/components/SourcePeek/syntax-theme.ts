/* ============================================================
   SourcePeek/syntax-theme.ts — token → CSS style map for the Prism
   highlighter used by <SourcePeek> (#4499).

   We don't ship one of react-syntax-highlighter's prebuilt themes (they
   hardcode a background + palette that clashes with the app's themeable
   surfaces). Instead this minimal map is transparent-backed and uses the
   app's own CSS custom properties with sane hex fallbacks, so highlighting
   stays coherent across dark / light / warm palettes with near-zero added
   weight.
   ============================================================ */

import type { CSSProperties } from "react";

type PrismStyle = Record<string, CSSProperties>;

// Color tokens fall back to readable defaults when a var is undefined.
const c = {
  base: "var(--text, #cdd6f4)",
  comment: "var(--text-4, #6c7086)",
  keyword: "var(--accent, #89b4fa)",
  string: "var(--success, #a6e3a1)",
  number: "var(--warning, #fab387)",
  func: "var(--info, #89dceb)",
  punctuation: "var(--text-3, #9399b2)",
  tag: "var(--accent, #f38ba8)",
};

export const sourcePeekPrismTheme: PrismStyle = {
  'code[class*="language-"]': {
    color: c.base,
    background: "none",
    fontFamily: "var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace)",
    fontSize: "12px",
    lineHeight: "1.5",
    whiteSpace: "pre",
    tabSize: 2,
  },
  'pre[class*="language-"]': {
    color: c.base,
    background: "none",
    margin: 0,
    padding: 0,
    overflow: "visible",
  },
  comment: { color: c.comment, fontStyle: "italic" },
  prolog: { color: c.comment },
  doctype: { color: c.comment },
  cdata: { color: c.comment },
  punctuation: { color: c.punctuation },
  property: { color: c.tag },
  tag: { color: c.tag },
  boolean: { color: c.number },
  number: { color: c.number },
  constant: { color: c.number },
  symbol: { color: c.number },
  deleted: { color: c.tag },
  selector: { color: c.string },
  "attr-name": { color: c.number },
  string: { color: c.string },
  char: { color: c.string },
  builtin: { color: c.func },
  inserted: { color: c.string },
  operator: { color: c.punctuation },
  entity: { color: c.base },
  url: { color: c.func },
  variable: { color: c.base },
  atrule: { color: c.keyword },
  "attr-value": { color: c.string },
  function: { color: c.func },
  "class-name": { color: c.func },
  keyword: { color: c.keyword },
  regex: { color: c.number },
  important: { color: c.tag, fontWeight: "bold" },
  bold: { fontWeight: "bold" },
  italic: { fontStyle: "italic" },
};
