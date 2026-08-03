import type React from "react";
import { Button } from "./Button";
import { Icon, type IconName } from "./Icon";

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
