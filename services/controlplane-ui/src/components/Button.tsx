import type React from "react";
import type { ButtonHTMLAttributes } from "react";
import { cn } from "./cn";

type ButtonVariant = "primary" | "ghost" | "icon" | "mutedIcon";

export function Button({
  className,
  variant = "primary",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant }): React.JSX.Element {
  return (
    <button
      {...props}
      className={cn(
        "relative inline-flex min-h-10 items-center justify-center gap-2 rounded-xl px-4 text-sm font-semibold transition hover:-translate-y-px disabled:cursor-not-allowed disabled:opacity-55",
        variant === "primary" && "bg-blue-950 text-white shadow-md shadow-blue-950/15 hover:bg-blue-900",
        variant === "ghost" && "bg-slate-100 text-slate-950 hover:bg-slate-200",
        variant === "icon" && "h-12 w-12 rounded-[1.15rem] p-0 text-slate-700 hover:bg-slate-100",
        variant === "mutedIcon" && "h-12 w-12 rounded-[1.15rem] bg-slate-100 p-0 text-slate-700 hover:bg-slate-200",
        className
      )}
    />
  );
}
