import { describe, expect, it } from "vitest";
import {
  arrowDirection,
  isBackKey,
  isEditableTarget,
  isRangeInput,
  isTelevisionUserAgent
} from "./device";

describe("device helpers", () => {
  it("detects television browsers from the user agent", () => {
    expect(isTelevisionUserAgent("Mozilla/5.0 (Web0S; Linux/SmartTV)")).toBe(true);
    expect(isTelevisionUserAgent("Mozilla/5.0 (SMART-TV; Linux; Tizen 6.5)")).toBe(true);
    expect(isTelevisionUserAgent("Mozilla/5.0 (Linux; Android 11) Chrome/120.0.0.0 Mobile")).toBe(false);
  });

  it("treats Escape and TV Back key codes as back", () => {
    expect(isBackKey(new KeyboardEvent("keydown", { key: "Escape" }))).toBe(true);
    const tvBack = new KeyboardEvent("keydown", { key: "Unidentified" });
    Object.defineProperty(tvBack, "keyCode", { get: () => 461 });
    expect(isBackKey(tvBack)).toBe(true);
    expect(isBackKey(new KeyboardEvent("keydown", { key: "Enter" }))).toBe(false);
  });

  it("maps arrow keys to spatial directions", () => {
    expect(arrowDirection(new KeyboardEvent("keydown", { key: "ArrowRight" }))).toBe("right");
    expect(arrowDirection(new KeyboardEvent("keydown", { key: "Enter" }))).toBeNull();
  });

  it("identifies text fields versus range inputs", () => {
    const text = document.createElement("input");
    text.type = "text";
    const range = document.createElement("input");
    range.type = "range";
    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    expect(isEditableTarget(text)).toBe(true);
    expect(isEditableTarget(range)).toBe(false);
    expect(isEditableTarget(checkbox)).toBe(false);
    expect(isRangeInput(range)).toBe(true);
  });
});
