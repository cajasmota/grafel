import { cn } from "@/lib/utils";

/** Keyboard key hint. Mirrors the prototype `.kbd` token styling. */
export function Kbd({ className, children, ...props }: React.HTMLAttributes<HTMLElement>) {
  return (
    <kbd
      className={cn(
        "inline-flex items-center justify-center min-w-[18px] h-[18px] px-[5px]",
        "font-mono text-[10px] text-text-3 bg-surface",
        "border border-border border-b-[1.5px] rounded-xs",
        className,
      )}
      {...props}
    >
      {children}
    </kbd>
  );
}
