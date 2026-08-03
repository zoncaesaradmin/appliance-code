import type React from "react";
import { cn } from "./cn";

export function BrandMark(props: { size?: "sm" | "lg" }): React.JSX.Element {
  const large = props.size === "lg";
  return (
    <div
      className={cn(
        "grid place-items-center rounded-[1.35rem] bg-gradient-to-br from-slate-950 to-blue-800 font-black text-white shadow-lg shadow-slate-900/15",
        large ? "h-16 w-16 text-2xl" : "h-11 w-11 text-base"
      )}
      aria-hidden="true"
    >
      <svg viewBox="0 0 32 32" className={large ? "h-9 w-9" : "h-6 w-6"} fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          d="M7 8.5h18l-11 7.2H25L7 23.5V20l11-6.8H7V8.5Z"
          fill="currentColor"
        />
      </svg>
    </div>
  );
}
