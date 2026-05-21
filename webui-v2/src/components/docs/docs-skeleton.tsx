/* ============================================================
   docs-skeleton.tsx — Loading skeleton for the entity article.
   Rendered while useDocsEntity is pending.
   ============================================================ */

function SkeletonLine({ w = "100%" }: { w?: string }) {
  return (
    <div
      className="h-3 rounded bg-surface-2 animate-pulse"
      style={{ width: w }}
    />
  );
}

export function DocsEntitySkeleton() {
  return (
    <article className="max-w-[760px] mx-auto px-8 py-8 flex flex-col gap-8">
      {/* Head */}
      <div className="flex flex-col gap-3">
        <div className="flex items-center gap-2">
          <SkeletonLine w="56px" />
          <SkeletonLine w="240px" />
        </div>
        <SkeletonLine w="320px" />
        <div className="flex gap-2">
          <SkeletonLine w="72px" />
          <SkeletonLine w="72px" />
        </div>
      </div>
      {/* Signature block */}
      <div className="flex flex-col gap-2">
        <SkeletonLine w="80px" />
        <div className="h-20 rounded-md bg-surface-2 animate-pulse" />
      </div>
      {/* Description */}
      <div className="flex flex-col gap-2">
        <SkeletonLine w="80px" />
        <SkeletonLine w="100%" />
        <SkeletonLine w="90%" />
        <SkeletonLine w="60%" />
      </div>
    </article>
  );
}
