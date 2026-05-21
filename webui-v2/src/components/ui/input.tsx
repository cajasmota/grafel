import { forwardRef } from "react";
import { Search } from "lucide-react";
import { cn } from "@/lib/utils";
import { Kbd } from "./kbd";

export const Input = forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...props }, ref) => (
    <input
      ref={ref}
      className={cn(
        "h-8 w-full rounded-md bg-surface border border-border px-3 text-md text-text",
        "placeholder:text-text-4",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)] focus-visible:border-accent",
        className,
      )}
      {...props}
    />
  ),
);
Input.displayName = "Input";

export interface SearchInputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  /** Optional keyboard hint rendered on the right (e.g. "/"). */
  shortcut?: string;
}

/** Search field with a leading icon + optional trailing kbd hint. */
export const SearchInput = forwardRef<HTMLInputElement, SearchInputProps>(
  ({ className, shortcut, ...props }, ref) => (
    <div className="relative flex items-center">
      <Search size={14} className="absolute left-2.5 text-text-3 pointer-events-none" />
      <input
        ref={ref}
        type="text"
        className={cn(
          "h-8 w-full rounded-md bg-surface border border-border pl-8 pr-9 text-md text-text",
          "placeholder:text-text-4",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)] focus-visible:border-accent",
          className,
        )}
        {...props}
      />
      {shortcut ? <Kbd className="absolute right-2.5">{shortcut}</Kbd> : null}
    </div>
  ),
);
SearchInput.displayName = "SearchInput";
