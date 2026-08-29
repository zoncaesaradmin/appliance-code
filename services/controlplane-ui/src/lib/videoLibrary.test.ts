import { describe, expect, it } from "vitest";
import type { ApplianceFileEntry } from "../types";
import {
  buildVideoShelves,
  canManageVideoLibrary,
  displayTitle,
  formatClock,
  isPlayableVideo,
  playableVideos,
  posterHue
} from "./videoLibrary";

function video(path: string, name = path.split("/").pop() || path): ApplianceFileEntry {
  return {
    name,
    path,
    type: "file",
    sizeBytes: 1024,
    modifiedAt: "2026-08-29T12:00:00Z"
  };
}

describe("video library helpers", () => {
  it("accepts only MP4 files as playable", () => {
    expect(isPlayableVideo("intro.mp4")).toBe(true);
    expect(isPlayableVideo("notes.txt")).toBe(false);
  });

  it("groups videos into library and directory shelves", () => {
    const shelves = buildVideoShelves([
      video("welcome.mp4"),
      video("training/intro.mp4"),
      video("training/wrap.mp4"),
      video("labs/day-one.mp4")
    ]);
    expect(shelves.map((shelf) => shelf.id)).toEqual(["library", "labs", "training"]);
    expect(shelves[0]?.videos.map((item) => item.name)).toEqual(["welcome.mp4"]);
    expect(shelves[2]?.videos.map((item) => item.name)).toEqual(["intro.mp4", "wrap.mp4"]);
  });

  it("filters playable files from a mixed tree", () => {
    const playable = playableVideos([
      { entry: { name: "training", path: "training", type: "directory", sizeBytes: 0, modifiedAt: "" }, depth: 0 },
      { entry: video("training/intro.mp4", "intro.mp4"), depth: 1 },
      { entry: video("readme.txt", "readme.txt"), depth: 0 }
    ]);
    expect(playable.map((item) => item.path)).toEqual(["training/intro.mp4"]);
  });

  it("formats clock values and display titles", () => {
    expect(formatClock(75)).toBe("1:15");
    expect(formatClock(3661)).toBe("1:01:01");
    expect(displayTitle(video("clips/Intro Lesson.mp4", "Intro Lesson.mp4"))).toBe("Intro Lesson");
  });

  it("gates library management on write permission", () => {
    expect(canManageVideoLibrary({ permissions: ["video.play"] })).toBe(false);
    expect(canManageVideoLibrary({ permissions: ["video.library.write"] })).toBe(true);
  });

  it("keeps poster hues stable for a name", () => {
    expect(posterHue("intro.mp4")).toBe(posterHue("intro.mp4"));
    expect(posterHue("intro.mp4")).not.toBe(posterHue("wrap.mp4"));
  });
});
