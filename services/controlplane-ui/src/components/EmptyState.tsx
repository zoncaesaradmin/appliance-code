import type React from "react";

export function EmptyState(props: { message: string }): React.JSX.Element {
  return (
    <div className="rounded-2xl border border-dashed border-slate-300 bg-slate-50 p-5 text-sm leading-6 text-slate-500">
      {props.message}
    </div>
  );
}
