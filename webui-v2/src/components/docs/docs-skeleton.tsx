/* ============================================================
   docs-skeleton.tsx — Loading skeleton for the entity article.
   Rendered while useDocsEntity is pending.
   ============================================================ */

import { Skeleton } from "@/components/ui/skeleton";

export function DocsEntitySkeleton() {
  return (
    <article className="max-w-[760px] mx-auto px-8 py-8 flex flex-col gap-8">
      {/* Head */}
      <div className="flex flex-col gap-3">
        <div className="flex items-center gap-2">
          <Skeleton w="w-14" />
          <Skeleton w="w-60" />
        </div>
        <Skeleton w="w-80" />
        <div className="flex gap-2">
          <Skeleton w="w-18" />
          <Skeleton w="w-18" />
        </div>
      </div>
      {/* Signature block */}
      <div className="flex flex-col gap-2">
        <Skeleton w="w-20" />
        <Skeleton h="h-20" className="rounded-md" />
      </div>
      {/* Description */}
      <div className="flex flex-col gap-2">
        <Skeleton w="w-20" />
        <Skeleton w="w-full" />
        <Skeleton w="w-[90%]" />
        <Skeleton w="w-[60%]" />
      </div>
    </article>
  );
}
