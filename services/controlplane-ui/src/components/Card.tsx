import type React from "react";
import type { ReactNode } from "react";

export function Card(props: {
  title: string;
  subtitle: string;
  children: ReactNode;
  className?: string;
}): React.JSX.Element {
  return (
    <section className={`rounded-3xl border border-slate-200/90 bg-white/95 p-5 shadow-xl shadow-slate-900/[0.07] ${props.className || ""}`}>
      <div className="mb-5">
        <h3 className="m-0 text-base font-bold text-slate-950">{props.title}</h3>
        <p className="mt-1 text-sm leading-6 text-slate-500">{props.subtitle}</p>
      </div>
      <div>{props.children}</div>
    </section>
  );
}
