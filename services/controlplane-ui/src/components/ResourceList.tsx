import React from "react";
import { RowActionsMenu, type RowAction } from "./RowActionsMenu";

export type ResourceListColumn = {
  key: string;
  label?: string;
  value: React.ReactNode;
};

export function ResourceList(props: {
  children: React.ReactNode;
  className?: string;
}): React.JSX.Element {
  return <div className={props.className ? `resource-list ${props.className}` : "resource-list"}>{props.children}</div>;
}

export function ResourceListRow(props: {
  columns: ResourceListColumn[];
  actions?: RowAction[];
  actionsLabel?: string;
  onClick?: () => void;
  ariaLabel?: string;
}): React.JSX.Element {
  const columnCount = Math.max(props.columns.length, 1);
  const clickable = typeof props.onClick === "function";
  const className = clickable ? "resource-list__row resource-list__row--clickable" : "resource-list__row";

  function handleActivate() {
    props.onClick?.();
  }

  return (
    <div
      className={className}
      style={{
        gridTemplateColumns: `repeat(${columnCount}, minmax(0, 1fr)) auto`
      }}
      role={clickable ? "button" : undefined}
      tabIndex={clickable ? 0 : undefined}
      aria-label={props.ariaLabel}
      onClick={clickable ? handleActivate : undefined}
      onKeyDown={
        clickable
          ? (event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                handleActivate();
              }
            }
          : undefined
      }
    >
      {props.columns.map((column) => (
        <div className="resource-list__cell" key={column.key}>
          {column.label ? <span className="resource-list__label">{column.label}</span> : null}
          <strong className="resource-list__value">{column.value}</strong>
        </div>
      ))}
      <div
        className="resource-list__actions"
        onClick={(event) => event.stopPropagation()}
        onKeyDown={(event) => event.stopPropagation()}
      >
        {props.actions && props.actions.length > 0 ? (
          <RowActionsMenu label={props.actionsLabel || "Row actions"} actions={props.actions} />
        ) : null}
      </div>
    </div>
  );
}

export type { RowAction };
