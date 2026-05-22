/* ============================================================
   docs-reader.tsx — Right pane: rendered markdown document.

   Renders one generated MD document (fetched by path key) plus a small
   header strip with the document title and its repo/category path. The
   markdown body is rendered by the dependency-free <Markdown> component.
   ============================================================ */

import { FileText } from "lucide-react";
import type { DocPage } from "@/data/types";
import { Markdown } from "./markdown";

export function DocsReader({ page }: { page: DocPage }) {
  return (
    <article className="mx-auto max-w-3xl px-8 py-6">
      <div className="flex items-center gap-2 mb-1 text-xs text-text-4 font-mono">
        <FileText size={12} />
        <span className="truncate">{page.path}</span>
      </div>
      <Markdown source={page.markdown} />
    </article>
  );
}
