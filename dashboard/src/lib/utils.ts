import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

/**
 * Merge Tailwind CSS class names with conditional logic support.
 * Wraps clsx + twMerge so conflicting Tailwind classes are resolved correctly.
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Human-readable label for a graph node. Returns the node's `label` when it is
 * a non-empty / non-whitespace string, otherwise falls back to its raw id.
 *
 * The `label ?? id` pattern is NOT sufficient because some payload nodes carry
 * an empty-string label (which `??` does not catch), making the hover tooltip
 * render the raw id (e.g. `upvate_core::proc:8288cd4a88`) instead of a real
 * name like `AttachmentViewer → split`.
 */
export function nodeDisplayLabel(
  node: { label?: string | null; id: string },
): string {
  const label = node.label
  if (typeof label === 'string' && label.trim().length > 0) return label
  return node.id
}
