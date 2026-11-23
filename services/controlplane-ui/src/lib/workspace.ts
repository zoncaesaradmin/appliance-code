import type { Session } from "../types";

export type Workspace = "app" | "console";

const INTERNAL_PATH = /^(?:\/(?:app|console|home|manage|analyze|admin|account)(?:\/|$)|\/$)/;

export function isApplicationPath(pathname: string): boolean {
  return pathname === "/app" || pathname.startsWith("/app/");
}

/**
 * Application sessions are intentionally a separate identity contract from
 * the appliance's local operator sessions. The application OTP service will
 * issue this domain/method pair when it is integrated.
 */
export function isApplicationSession(session: Pick<Session, "domain" | "authMethod"> | null): boolean {
  return session?.domain === "application" && session.authMethod === "mobile_otp";
}

export function workspaceForPath(pathname: string): Workspace {
  if (pathname === "/" || isApplicationPath(pathname)) {
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
  if (
    value.includes("\\") ||
    value.startsWith("/api/") ||
    value === "/login" ||
    value === "/app/login" ||
    value === "/setup"
  ) {
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
  if (isApplicationPath(pathname)) {
    if (pathname === "/app/login") {
      return "/app";
    }
    return pathname;
  }
  // The public origin is the application entry. The explicit operator login
  // remains an appliance destination, while /app/login owns app sign-in.
  return pathname === "/" ? "/app" : "/home";
}

