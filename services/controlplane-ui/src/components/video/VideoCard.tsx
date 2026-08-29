import React from "react";
import type { ApplianceFileEntry } from "../../types";
import { displayTitle, formatBytes, formatModifiedAt, posterHue } from "../../lib/videoLibrary";

export function VideoCard(props: {
  entry: ApplianceFileEntry;
  canManage: boolean;
  onPlay: () => void;
  onRequestDelete: () => void;
}): React.JSX.Element {
  const title = displayTitle(props.entry);
  const hue = posterHue(props.entry.path);
  const letter = (title.trim().charAt(0) || "V").toUpperCase();

  return (
    <article className="video-card">
      <button
        type="button"
        className="video-card__hit"
        data-nav
        data-nav-id={`play:${props.entry.path}`}
        data-path={props.entry.path}
        aria-label={`Play ${title}`}
        onClick={props.onPlay}
      >
        <div
          className="video-card__poster"
          style={{ background: `linear-gradient(160deg, hsl(${hue} 42% 28%), hsl(${(hue + 40) % 360} 38% 14%))` }}
        >
          <span className="video-card__letter" aria-hidden="true">
            {letter}
          </span>
          <span className="video-card__play" aria-hidden="true">
            ▶
          </span>
        </div>
        <div className="video-card__meta">
          <strong className="video-card__title">{title}</strong>
          <span className="video-card__detail">
            {formatBytes(props.entry.sizeBytes)} · {formatModifiedAt(props.entry.modifiedAt)}
          </span>
        </div>
      </button>
      {props.canManage ? (
        <button
          type="button"
          className="video-card__more"
          data-nav
          data-nav-id={`more:${props.entry.path}`}
          aria-label={`More actions for ${title}`}
          onClick={props.onRequestDelete}
        >
          ⋮
        </button>
      ) : null}
    </article>
  );
}
