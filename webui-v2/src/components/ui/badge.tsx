import { cn } from "@/lib/utils";

type Tone = "neutral" | "accent" | "success" | "warning" | "danger" | "info";

const tones: Record<Tone, string> = {
  neutral: "bg-surface-2 text-text-2 border-border",
  accent: "bg-accent-soft text-accent-strong border-transparent",
  success: "bg-success-soft text-success border-transparent",
  warning: "bg-warning-soft text-warning border-transparent",
  danger: "bg-danger-soft text-danger border-transparent",
  info: "bg-info-soft text-info border-transparent",
};

export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  tone?: Tone;
  /** Optional leading dot color (any CSS color / token var). Pairs color with a label. */
  dot?: string;
}

/** Small status/label pill. Always carries a text label — never color-only. */
export function Badge({ className, tone = "neutral", dot, children, ...props }: BadgeProps) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 h-[18px] px-2 rounded-full border",
        "text-xs font-medium tabular-nums",
        tones[tone],
        className,
      )}
      {...props}
    >
      {dot ? (
        <span className="size-1.5 rounded-full" style={{ background: dot }} aria-hidden />
      ) : null}
      {children}
    </span>
  );
}
