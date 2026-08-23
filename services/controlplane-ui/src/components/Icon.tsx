import type React from "react";
import type { SVGProps } from "react";
import { cn } from "./cn";

export type IconName =
  | "admin"
  | "alerts"
  | "analyze"
  | "artifacts"
  | "builder"
  | "catalog"
  | "dns"
  | "files"
  | "help"
  | "home"
  | "key"
  | "license"
  | "manage"
  | "more"
  | "profiles"
  | "search"
  | "session"
  | "status"
  | "connectivity"
  | "user"
  | "video"
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
    case "more":
      return (
        <svg {...common}>
          <circle cx="12" cy="5" r="1.6" fill="currentColor" stroke="none" />
          <circle cx="12" cy="12" r="1.6" fill="currentColor" stroke="none" />
          <circle cx="12" cy="19" r="1.6" fill="currentColor" stroke="none" />
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
    case "files":
      return (
        <svg {...common}>
          <path d="M4 6.5A2.5 2.5 0 0 1 6.5 4H12l2 2h3.5A2.5 2.5 0 0 1 20 8.5v9A2.5 2.5 0 0 1 17.5 20h-11A2.5 2.5 0 0 1 4 17.5v-11Z" />
          <path d="M8 12h8" />
          <path d="M8 15h5" />
        </svg>
      );
    case "video":
      return (
        <svg {...common}>
          <rect x="3" y="6" width="14" height="12" rx="2" />
          <path d="m17 10 4-2v8l-4-2" />
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
    case "connectivity":
      return (
        <svg {...common}>
          <circle cx="6" cy="6" r="2.5" />
          <circle cx="18" cy="6" r="2.5" />
          <circle cx="12" cy="18" r="2.5" />
          <path d="M8 7.5 10.5 15" />
          <path d="m16 7.5-2.5 7.5" />
          <path d="M8.5 6h7" />
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
