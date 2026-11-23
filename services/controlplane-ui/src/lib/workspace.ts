export type Workspace = "app" | "console";

const INTERNAL_PATH = /^(?:\/(?:app|console|home|manage|analyze|admin|account)(?:\/|$)|\/$)/;

/**
 * The current release still serves the appliance shell at the root. The
 * application workspace becomes the root destination when its signed UI
 * artifact is installed; until then /app remains an explicit, reserved
 * namespace.
 */
export function workspaceForPath(pathname: string): Workspace {
  if (pathname === "/app" || pathname.startsWith("/app/")) {
    return "app";
  }
  return "console";
}

/** Convert the new console entry point to the existing shell routes. */
export function consolePath(pathname: string): string {
  if (pathname === "/console" || pathname === "/console/") {
    return "/home";
  }
  if (pathname.startsWith("/console/")) {
    const suffix = pathname.slice("/console".length);
    if (
      suffix === "/home" ||
      suffix.startsWith("/home/") ||
      suffix === "/manage" ||
      suffix.startsWith("/manage/") ||
      suffix === "/analyze" ||
      suffix.startsWith("/analyze/") ||
      suffix === "/admin" ||
      suffix.startsWith("/admin/") ||
      suffix === "/account" ||
      suffix.startsWith("/account/")
    ) {
      return suffix;
    }
  }
  return pathname;
}

/** Accept only an internal UI path as a post-login destination. */
export function safeNextPath(raw: string | null | undefined, fallback = "/home"): string {
  const value = (raw ?? "").trim();
  if (!value || value.length > 512 || !value.startsWith("/") || value.startsWith("//")) {
    return fallback;
  }
  if (value.includes("\\") || value.startsWith("/api/") || value === "/login" || value === "/setup") {
    return fallback;
  }
  return INTERNAL_PATH.test(value) ? value : fallback;
}

export function loginDestination(pathname: string, search: string): string {
  const requested = new URLSearchParams(search).get("next");
  if (requested) {
    return safeNextPath(requested);
  }
  if (pathname === "/console" || pathname.startsWith("/console/")) {
    return consolePath(pathname);
  }
  if (pathname === "/app" || pathname.startsWith("/app/")) {
    return pathname;
  }
  // Preserve the shipped root behavior until the application UI artifact and
  // its API services are present in the signed bundle.
  return "/home";
}

