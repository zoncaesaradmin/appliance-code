import type React from "react";
import type { ButtonHTMLAttributes, ReactNode, SVGProps } from "react";

export function cn(...values: Array<string | false | null | undefined>): string {
  return values.filter(Boolean).join(" ");
}

export type IconName =
  | "admin"
  | "alerts"
  | "analyze"
  | "artifacts"
  | "builder"
  | "catalog"
  | "dns"
  | "help"
  | "home"
  | "key"
  | "license"
  | "manage"
  | "profiles"
  | "search"
  | "session"
  | "status"
  | "user"
  | "workflows";

export function Icon(props: { name: IconName; className?: string }): React.JSX.Element {
  const common: SVGProps<SVGSVGElement> = {
    className: cn(props.className ?? "h-5 w-5", "shrink-0"),
    viewBox: "0 0 24 24",
    fill: "none",
    stroke: "currentColor",
    strokeLinecap: "round",
    strokeLinejoin: "round",
    strokeWidth: 1.9,
    "aria-hidden": true
  };

  switch (props.name) {
    case "home":
      return (
        <svg {...common}>
          <path d="m4 11 8-7 8 7" />
          <path d="M6 10.5V20h12v-9.5" />
          <path d="M10 20v-6h4v6" />
        </svg>
      );
    case "manage":
      return (
        <svg {...common}>
          <path d="M4 7h16" />
          <path d="M7 4v6" />
          <path d="M4 17h16" />
          <path d="M17 14v6" />
        </svg>
      );
    case "analyze":
      return (
        <svg {...common}>
          <path d="M4 19V5" />
          <path d="M4 19h16" />
          <path d="m7 15 4-4 3 3 5-7" />
        </svg>
      );
    case "admin":
      return (
        <svg {...common}>
          <path d="M12 3 5 6v5c0 4.2 2.8 7.7 7 9 4.2-1.3 7-4.8 7-9V6l-7-3Z" />
          <path d="m9.5 12 1.8 1.8 3.4-4.1" />
        </svg>
      );
    case "search":
      return (
        <svg {...common}>
          <circle cx="10.5" cy="10.5" r="5.5" />
          <path d="m15 15 5 5" />
        </svg>
      );
    case "alerts":
      return (
        <svg {...common}>
          <path d="M18 9a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9" />
          <path d="M10 21h4" />
        </svg>
      );
    case "help":
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="9" />
          <path d="M9.8 9a2.4 2.4 0 1 1 4 1.8c-1.2.8-1.8 1.4-1.8 2.7" />
          <path d="M12 17h.01" />
        </svg>
      );
    case "builder":
      return (
        <svg {...common}>
          <path d="m14 4 6 6-6 6" />
          <path d="m10 20-6-6 6-6" />
        </svg>
      );
    case "dns":
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="8" />
          <path d="M4 12h16" />
          <path d="M12 4a12 12 0 0 1 0 16" />
          <path d="M12 4a12 12 0 0 0 0 16" />
        </svg>
      );
    case "artifacts":
      return (
        <svg {...common}>
          <path d="m12 3 8 4.5v9L12 21l-8-4.5v-9L12 3Z" />
          <path d="M4.5 8 12 12.2 19.5 8" />
          <path d="M12 21v-8.8" />
        </svg>
      );
    case "workflows":
      return (
        <svg {...common}>
          <path d="M6 7h4" />
          <path d="M14 7h4" />
          <path d="M6 17h4" />
          <path d="M10 7c2 0 2 10 4 10h4" />
        </svg>
      );
    case "status":
      return (
        <svg {...common}>
          <path d="M4 13h4l2-6 4 10 2-4h4" />
        </svg>
      );
    case "profiles":
      return (
        <svg {...common}>
          <path d="M8 7a4 4 0 1 0 8 0 4 4 0 0 0-8 0Z" />
          <path d="M4 21a8 8 0 0 1 16 0" />
        </svg>
      );
    case "user":
      return (
        <svg {...common}>
          <circle cx="12" cy="8" r="4" />
          <path d="M5 21a7 7 0 0 1 14 0" />
        </svg>
      );
    case "license":
      return (
        <svg {...common}>
          <path d="M7 3h8l4 4v14H7z" />
          <path d="M15 3v5h5" />
          <path d="m9.5 15 1.8 1.8 3.7-4.3" />
        </svg>
      );
    case "key":
      return (
        <svg {...common}>
          <circle cx="8" cy="15" r="3" />
          <path d="m10.5 12.5 7-7" />
          <path d="m15 5 4 4" />
          <path d="m13 8 3 3" />
        </svg>
      );
    case "session":
      return (
        <svg {...common}>
          <path d="M4 5h16v14H4z" />
          <path d="M8 9h8" />
          <path d="M8 13h5" />
        </svg>
      );
    case "catalog":
      return (
        <svg {...common}>
          <path d="M5 5h14" />
          <path d="M5 12h14" />
          <path d="M5 19h14" />
          <path d="M8 5v14" />
        </svg>
      );
  }
}

export function BrandMark(): React.JSX.Element {
  return (
    <div className="grid h-11 w-11 place-items-center rounded-2xl bg-gradient-to-br from-slate-950 to-blue-800 text-base font-black text-white shadow-lg shadow-slate-900/15">
      Z
    </div>
  );
}

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

export function IconButton(props: {
  icon: IconName;
  label: string;
  badge?: number;
  muted?: boolean;
  onClick?: () => void;
  disabled?: boolean;
  title?: string;
}): React.JSX.Element {
  return (
    <Button
      aria-label={props.label}
      disabled={props.disabled}
      onClick={props.onClick}
      title={props.title || props.label}
      variant={props.muted ? "mutedIcon" : "icon"}
    >
      <Icon name={props.icon} className="h-6 w-6" />
      {props.badge && props.badge > 0 ? (
        <span className="absolute -right-1 -top-1 grid h-5 min-w-5 place-items-center rounded-full bg-emerald-600 px-1 text-[0.68rem] font-bold text-white">
          {props.badge}
        </span>
      ) : null}
    </Button>
  );
}

export function Card(props: {
  title: string;
  subtitle: string;
  children: ReactNode;
}): React.JSX.Element {
  return (
    <section className="rounded-3xl border border-slate-200/90 bg-white/95 p-5 shadow-xl shadow-slate-900/[0.07]">
      <div className="mb-5">
        <h3 className="m-0 text-base font-bold text-slate-950">{props.title}</h3>
        <p className="mt-1 text-sm leading-6 text-slate-500">{props.subtitle}</p>
      </div>
      <div>{props.children}</div>
    </section>
  );
}

export function PageFrame(props: {
  title: string;
  description: string;
  tabs: Array<{ label: string; path: string; icon?: IconName }>;
  pathname: string;
  onNavigate: (path: string) => void;
  children: ReactNode;
}): React.JSX.Element {
  return (
    <section className="min-h-[calc(100vh-112px)] rounded-[1.75rem] border border-slate-200/90 bg-white/95 p-6 shadow-xl shadow-slate-900/[0.07]">
      <div>
        <span className="mb-2 inline-block text-xs font-bold uppercase tracking-[0.14em] text-blue-950">
          Appliance workspace
        </span>
        <h1 className="m-0 text-3xl font-bold tracking-tight text-slate-950 md:text-4xl">
          {props.title}
        </h1>
        <p className="mt-2 max-w-4xl text-sm leading-6 text-slate-500 md:text-base">
          {props.description}
        </p>
      </div>
      {props.tabs.length > 0 ? (
        <div className="mb-6 mt-5 flex flex-wrap gap-2 border-b border-slate-200 pb-4">
          {props.tabs.map((tab) => {
            const active = props.pathname === tab.path;
            return (
              <button
                key={tab.path}
                className={cn(
                  "inline-flex min-h-10 items-center gap-2 rounded-full px-4 text-sm font-semibold transition",
                  active
                    ? "bg-blue-100 text-blue-950"
                    : "text-slate-500 hover:bg-slate-100 hover:text-slate-950"
                )}
                onClick={() => props.onNavigate(tab.path)}
              >
                {tab.icon ? <Icon name={tab.icon} className="h-4 w-4" /> : null}
                {tab.label}
              </button>
            );
          })}
        </div>
      ) : (
        <div className="mb-6 mt-5" />
      )}
      <div className="w-full">{props.children}</div>
    </section>
  );
}

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

export function EmptyState(props: { message: string }): React.JSX.Element {
  return (
    <div className="rounded-2xl border border-dashed border-slate-300 bg-slate-50 p-5 text-sm leading-6 text-slate-500">
      {props.message}
    </div>
  );
}

export const fieldClass = "grid gap-2";
export const fieldLabelClass = "text-sm font-medium text-slate-500";
export const inputClass =
  "min-h-11 rounded-2xl border border-slate-200 bg-white px-3 text-sm text-slate-950 outline-none transition focus:border-blue-800 focus:ring-4 focus:ring-blue-950/10 disabled:bg-slate-100 disabled:text-slate-500";
export const stackClass = "grid gap-4";
export const twoColumnClass = "grid gap-5 lg:grid-cols-2";
export const statsGridClass = "grid gap-5 sm:grid-cols-2 xl:grid-cols-4";
export const detailListClass = "grid gap-0";
export const detailRowClass =
  "flex items-center justify-between gap-4 border-b border-slate-200 py-3 last:border-b-0";
export const mutedTextClass = "text-sm text-slate-500";
export const tableListClass = "grid gap-0";
export const tableRowClass =
  "flex items-center justify-between gap-4 border-b border-slate-200 py-3 last:border-b-0";
export const badgeRowClass = "flex flex-wrap gap-2";
export const pillClass = "inline-flex min-h-8 items-center rounded-full bg-slate-100 px-3 text-sm font-semibold text-slate-950";
export const navyPillClass = "bg-blue-100 text-blue-950";
