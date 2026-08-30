import { useEffect, useRef, useState, type MutableRefObject } from "react";
import type { ApplianceFileEntry } from "../types";
import { loadVideoPoster, readCachedPoster } from "./videoPoster";

export function useVideoPoster(entry: ApplianceFileEntry): {
  poster: string | null;
  posterRef: MutableRefObject<HTMLDivElement | null>;
} {
  const posterRef = useRef<HTMLDivElement | null>(null);
  const [poster, setPoster] = useState(() => readCachedPoster(entry.path, entry.modifiedAt));
  const path = entry.path;
  const modifiedAt = entry.modifiedAt;

  useEffect(() => {
    setPoster(readCachedPoster(path, modifiedAt));
  }, [path, modifiedAt]);

  useEffect(() => {
    if (poster) {
      return;
    }
    const node = posterRef.current;
    let cancelled = false;

    function startCapture() {
      void loadVideoPoster({ path, modifiedAt }).then((dataUrl) => {
        if (!cancelled && dataUrl) {
          setPoster(dataUrl);
        }
      });
    }

    if (!node || typeof IntersectionObserver !== "function") {
      startCapture();
      return () => {
        cancelled = true;
      };
    }

    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((item) => item.isIntersecting)) {
          return;
        }
        observer.disconnect();
        startCapture();
      },
      { rootMargin: "180px", threshold: 0.01 }
    );
    observer.observe(node);
    return () => {
      cancelled = true;
      observer.disconnect();
    };
  }, [path, modifiedAt, poster]);

  return { poster, posterRef };
}
