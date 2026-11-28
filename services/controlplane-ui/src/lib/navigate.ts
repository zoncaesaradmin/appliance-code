export function navigate(path: string, replace = false): void {
  // Geo is a separate HTML entry point, so history-only navigation would
  // leave the console application mounted and its auth guard would redirect.
  if (requiresDocumentNavigation(path)) {
    if (replace) {
      window.location.replace(path);
    } else {
      window.location.assign(path);
    }
    return;
  }
  if (replace) {
    window.history.replaceState({}, "", path);
  } else {
    window.history.pushState({}, "", path);
  }
  window.dispatchEvent(new PopStateEvent("popstate"));
}

export function requiresDocumentNavigation(path: string): boolean {
  return /^\/geo(?:[/?#]|$)/.test(path);
}
