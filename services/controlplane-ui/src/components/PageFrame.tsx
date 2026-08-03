import type React from "react";
import type { ReactNode } from "react";
import { cn } from "./cn";

export function PageFrame(props: {
  title: string;
  description?: string;
  /** Small label above the title. Pass "" to hide; set a value when context helps (e.g. Account). */
  eyebrow?: string;
  tabs: Array<{ label: string; path: string }>;
  pathname: string;
  onNavigate: (path: string) => void;
  children: ReactNode;
}): React.JSX.Element {
  const eyebrow = props.eyebrow?.trim();
  return (
    <section className="min-h-[calc(100vh-112px)] rounded-[1.75rem] border border-slate-200/90 bg-white/95 p-6 shadow-xl shadow-slate-900/[0.07]">
      <div>
        {eyebrow ? (
          <span className="mb-2 inline-block text-xs font-bold uppercase tracking-[0.14em] text-blue-950">
            {eyebrow}
          </span>
        ) : null}
        <h1 className="m-0 text-3xl font-bold tracking-tight text-slate-950 md:text-4xl">
          {props.title}
        </h1>
        {props.description ? (
          <p className="mt-2 max-w-4xl text-sm leading-6 text-slate-500 md:text-base">
            {props.description}
          </p>
        ) : null}
      </div>
      {props.tabs.length > 0 ? (
        <nav
          className="mb-6 mt-5 flex flex-wrap gap-x-6 gap-y-1 border-b border-slate-200"
          aria-label="Page sections"
        >
          {props.tabs.map((tab) => {
            const active = props.pathname === tab.path;
            return (
              <button
                key={tab.path}
                type="button"
                className={cn(
                  "-mb-px border-b-2 px-0.5 pb-3 text-sm font-semibold tracking-tight transition",
                  active
                    ? "border-blue-950 text-slate-950"
                    : "border-transparent text-slate-500 hover:text-slate-950"
                )}
                aria-current={active ? "page" : undefined}
                onClick={() => props.onNavigate(tab.path)}
              >
                {tab.label}
              </button>
            );
          })}
        </nav>
      ) : (
        <div className="mb-6 mt-5" />
      )}
      <div className="w-full">{props.children}</div>
    </section>
  );
}
