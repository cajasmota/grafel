/* ============================================================
   markdown.tsx — Minimal, dependency-free markdown renderer.

   webui-v2 ships no markdown library (keeps the bundle small + offline).
   This renderer covers the subset the `generate-docs` skill emits:
   headings, paragraphs, fenced + inline code, lists (ordered/unordered),
   blockquotes, links, bold/italic, hr, and tables. It intentionally does
   NOT render raw HTML — text is escaped, so doc content cannot inject markup.

   Block-level parsing is line-based; inline parsing tokenizes code spans
   first (so emphasis/links inside backticks are left literal), then handles
   links, bold, and italic.
   ============================================================ */

import { Fragment, type ReactNode } from "react";

// ── Inline parsing ─────────────────────────────────────────────────────────

function renderInline(text: string, keyPrefix: string): ReactNode[] {
  const out: ReactNode[] = [];
  // Split on inline code spans first; odd indices are code.
  const codeParts = text.split(/(`[^`]+`)/g);
  codeParts.forEach((part, ci) => {
    if (part.startsWith("`") && part.endsWith("`") && part.length >= 2) {
      out.push(
        <code
          key={`${keyPrefix}-c${ci}`}
          className="px-1 py-0.5 rounded bg-surface-2 text-[var(--accent)] font-mono text-[0.85em]"
        >
          {part.slice(1, -1)}
        </code>,
      );
      return;
    }
    out.push(...renderEmphasisAndLinks(part, `${keyPrefix}-t${ci}`));
  });
  return out;
}

function renderEmphasisAndLinks(text: string, keyPrefix: string): ReactNode[] {
  const out: ReactNode[] = [];
  // Links: [label](href)
  const linkRe = /\[([^\]]+)\]\(([^)]+)\)/g;
  let last = 0;
  let m: RegExpExecArray | null;
  let i = 0;
  while ((m = linkRe.exec(text)) !== null) {
    if (m.index > last) {
      out.push(...renderEmphasis(text.slice(last, m.index), `${keyPrefix}-pre${i}`));
    }
    const href = m[2];
    const isExternal = /^https?:\/\//.test(href);
    out.push(
      <a
        key={`${keyPrefix}-l${i}`}
        href={href}
        target={isExternal ? "_blank" : undefined}
        rel={isExternal ? "noreferrer noopener" : undefined}
        className="text-[var(--accent)] hover:underline"
      >
        {m[1]}
      </a>,
    );
    last = m.index + m[0].length;
    i++;
  }
  if (last < text.length) {
    out.push(...renderEmphasis(text.slice(last), `${keyPrefix}-end`));
  }
  return out;
}

function renderEmphasis(text: string, keyPrefix: string): ReactNode[] {
  // Bold (**x**) then italic (*x* / _x_). Process bold first.
  const parts = text.split(/(\*\*[^*]+\*\*)/g);
  return parts.map((part, pi) => {
    if (part.startsWith("**") && part.endsWith("**") && part.length >= 4) {
      return (
        <strong key={`${keyPrefix}-b${pi}`} className="font-semibold text-text">
          {part.slice(2, -2)}
        </strong>
      );
    }
    // italic within the non-bold chunk
    const itParts = part.split(/(\*[^*]+\*|_[^_]+_)/g);
    return (
      <Fragment key={`${keyPrefix}-f${pi}`}>
        {itParts.map((ip, ii) => {
          if (
            (ip.startsWith("*") && ip.endsWith("*") && ip.length >= 2) ||
            (ip.startsWith("_") && ip.endsWith("_") && ip.length >= 2)
          ) {
            return (
              <em key={`${keyPrefix}-i${pi}-${ii}`} className="italic">
                {ip.slice(1, -1)}
              </em>
            );
          }
          return <Fragment key={`${keyPrefix}-x${pi}-${ii}`}>{ip}</Fragment>;
        })}
      </Fragment>
    );
  });
}

// ── Block parsing ────────────────────────────────────────────────────────────

export function Markdown({ source }: { source: string }) {
  const lines = source.replace(/\r\n/g, "\n").split("\n");
  const blocks: ReactNode[] = [];
  let i = 0;
  let key = 0;
  const nextKey = () => `b${key++}`;

  while (i < lines.length) {
    const line = lines[i];

    // Fenced code block
    if (/^```/.test(line.trim())) {
      const lang = line.trim().slice(3).trim();
      const buf: string[] = [];
      i++;
      while (i < lines.length && !/^```/.test(lines[i].trim())) {
        buf.push(lines[i]);
        i++;
      }
      i++; // skip closing fence
      blocks.push(
        <pre
          key={nextKey()}
          className="my-3 p-3 rounded-md bg-surface-2 border border-border overflow-x-auto text-[0.8rem] leading-relaxed"
        >
          <code className="font-mono text-text-2" data-lang={lang}>
            {buf.join("\n")}
          </code>
        </pre>,
      );
      continue;
    }

    // Blank line
    if (line.trim() === "") {
      i++;
      continue;
    }

    // Horizontal rule
    if (/^(---|\*\*\*|___)\s*$/.test(line.trim())) {
      blocks.push(<hr key={nextKey()} className="my-4 border-border" />);
      i++;
      continue;
    }

    // Heading
    const h = /^(#{1,6})\s+(.*)$/.exec(line);
    if (h) {
      const level = h[1].length;
      const content = renderInline(h[2].trim(), nextKey());
      const cls = [
        "font-semibold text-text scroll-mt-4",
        level === 1 ? "text-2xl mt-1 mb-4 pb-2 border-b border-border" : "",
        level === 2 ? "text-xl mt-6 mb-3" : "",
        level === 3 ? "text-lg mt-5 mb-2" : "",
        level >= 4 ? "text-base mt-4 mb-2 text-text-2" : "",
      ].join(" ");
      const Tag = (`h${level}` as keyof JSX.IntrinsicElements);
      blocks.push(
        <Tag key={nextKey()} className={cls}>
          {content}
        </Tag>,
      );
      i++;
      continue;
    }

    // Table (header row + separator row of dashes)
    if (line.includes("|") && i + 1 < lines.length && /^\s*\|?[\s:|-]+\|?\s*$/.test(lines[i + 1]) && lines[i + 1].includes("-")) {
      const splitRow = (r: string) =>
        r.replace(/^\s*\|/, "").replace(/\|\s*$/, "").split("|").map((c) => c.trim());
      const headers = splitRow(line);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].includes("|") && lines[i].trim() !== "") {
        rows.push(splitRow(lines[i]));
        i++;
      }
      blocks.push(
        <div key={nextKey()} className="my-3 overflow-x-auto">
          <table className="w-full text-sm border-collapse">
            <thead>
              <tr className="border-b border-border">
                {headers.map((hd, hi) => (
                  <th key={hi} className="text-left font-semibold text-text px-2 py-1.5">
                    {renderInline(hd, `th${hi}`)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row, ri) => (
                <tr key={ri} className="border-b border-border/50">
                  {row.map((cell, cidx) => (
                    <td key={cidx} className="px-2 py-1.5 text-text-2 align-top">
                      {renderInline(cell, `td${ri}-${cidx}`)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>,
      );
      continue;
    }

    // Blockquote
    if (/^>\s?/.test(line)) {
      const buf: string[] = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) {
        buf.push(lines[i].replace(/^>\s?/, ""));
        i++;
      }
      blocks.push(
        <blockquote
          key={nextKey()}
          className="my-3 pl-3 border-l-2 border-[var(--accent)] text-text-3 italic"
        >
          {renderInline(buf.join(" "), nextKey())}
        </blockquote>,
      );
      continue;
    }

    // Unordered list
    if (/^\s*[-*+]\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\s*[-*+]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*[-*+]\s+/, ""));
        i++;
      }
      blocks.push(
        <ul key={nextKey()} className="my-3 ml-5 list-disc space-y-1 text-text-2">
          {items.map((it, ii) => (
            <li key={ii}>{renderInline(it, `ul${ii}`)}</li>
          ))}
        </ul>,
      );
      continue;
    }

    // Ordered list
    if (/^\s*\d+\.\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*\d+\.\s+/, ""));
        i++;
      }
      blocks.push(
        <ol key={nextKey()} className="my-3 ml-5 list-decimal space-y-1 text-text-2">
          {items.map((it, ii) => (
            <li key={ii}>{renderInline(it, `ol${ii}`)}</li>
          ))}
        </ol>,
      );
      continue;
    }

    // Paragraph — gather consecutive non-blank, non-block lines.
    const buf: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !/^```/.test(lines[i].trim()) &&
      !/^(#{1,6})\s+/.test(lines[i]) &&
      !/^>\s?/.test(lines[i]) &&
      !/^\s*[-*+]\s+/.test(lines[i]) &&
      !/^\s*\d+\.\s+/.test(lines[i]) &&
      !/^(---|\*\*\*|___)\s*$/.test(lines[i].trim())
    ) {
      buf.push(lines[i]);
      i++;
    }
    blocks.push(
      <p key={nextKey()} className="my-3 leading-relaxed text-text-2">
        {renderInline(buf.join(" "), nextKey())}
      </p>,
    );
  }

  return <div className="text-sm">{blocks}</div>;
}
