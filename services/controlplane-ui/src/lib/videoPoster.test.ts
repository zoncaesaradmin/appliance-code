import { beforeEach, describe, expect, it } from "vitest";
import { posterStorageKey, readCachedPoster, writeCachedPoster } from "./videoPoster";
import { resetDomStorage } from "../test/domStorage";

describe("video poster cache", () => {
  beforeEach(() => {
    resetDomStorage();
  });

  it("reads a stored JPEG data URL for the same path and modified time", () => {
    writeCachedPoster("training/intro.mp4", "2026-08-29T12:00:00Z", "data:image/jpeg;base64,abc");

    expect(readCachedPoster("training/intro.mp4", "2026-08-29T12:00:00Z")).toBe("data:image/jpeg;base64,abc");
    expect(readCachedPoster("training/intro.mp4", "2026-08-30T12:00:00Z")).toBeNull();
    expect(window.sessionStorage.getItem(posterStorageKey("training/intro.mp4", "2026-08-29T12:00:00Z"))).toContain(
      "data:image/jpeg"
    );
  });

  it("ignores non-image values in storage", () => {
    window.sessionStorage.setItem(posterStorageKey("clip.mp4", "t"), "not-an-image");
    expect(readCachedPoster("clip.mp4", "t")).toBeNull();
  });
});
