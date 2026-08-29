export function isTelevisionUserAgent(userAgent = typeof navigator === "undefined" ? "" : navigator.userAgent): boolean {
  return /TV|Tizen|Web0S|WebOS|BRAVIA|VIDAA|Viera|SmartTV|AppleTV|HbbTV|NetCast|CrKey\/|AFT[A-Z]|Silk.+TV/i.test(
    userAgent
  );
}

export function isBackKey(event: KeyboardEvent): boolean {
  if (event.key === "Escape" || event.key === "GoBack" || event.key === "BrowserBack") {
    return true;
  }
  return event.keyCode === 461 || event.keyCode === 10009;
}

export type SpatialDirection = "up" | "down" | "left" | "right";

export function arrowDirection(event: KeyboardEvent): SpatialDirection | null {
  switch (event.key) {
    case "ArrowUp":
      return "up";
    case "ArrowDown":
      return "down";
    case "ArrowLeft":
      return "left";
    case "ArrowRight":
      return "right";
    default:
      return null;
  }
}

export function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  if (target.isContentEditable) {
    return true;
  }
  if (target instanceof HTMLTextAreaElement) {
    return true;
  }
  if (!(target instanceof HTMLInputElement)) {
    return false;
  }
  return target.type !== "checkbox" && target.type !== "radio" && target.type !== "button" && target.type !== "file" && target.type !== "range";
}

export function isRangeInput(target: EventTarget | null): boolean {
  return target instanceof HTMLInputElement && target.type === "range";
}
