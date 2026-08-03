import type React from "react";
import { cn } from "./cn";

export function StatCard(props: {
  label: string;
  value: string;
  tone?: "success" | "neutral";
}): React.JSX.Element {
  return (
    <div
      className={cn(
        "rounded-2xl border p-4",
        props.tone === "success"
          ? "border-emerald-700/20 bg-emerald-50 text-emerald-700"
          : "border-slate-200/90 bg-white/90 text-slate-950"
      )}
    >
      <span className="mb-3 block text-sm text-slate-500">{props.label}</span>
      <strong className="text-2xl font-bold">{props.value}</strong>
    </div>
  );
}
