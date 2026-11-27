import type { Session } from "../types";

export type Workspace = "app" | "console" | "geo";

type LandingPolicy = {
  loginPath: "/app/login" | "/console/login" | "/geo/login";
  authenticatedPath: "/app" | "/home" | "/geo";
};

const defaultLandingPolicy: LandingPolicy = {
  loginPath: "/console/login",
  authenticatedPath: "/home"
};

// Profile-specific entry behavior belongs in code, not in install-time
// configuration. Add a profile here when it needs a different public entry;
// every unlisted profile safely uses the operator console default.
const profileLandingPolicies: Record<string, LandingPolicy> = {
  "application-core": {
    loginPath: "/app/login",
    authenticatedPath: "/app"
  },
  "geo-zone": {
    loginPath: "/geo/login",
    authenticatedPath: "/geo"
  }
};

const INTERNAL_PATH = /^(?:\/(?:app|console|geo|home|manage|analyze|admin|account)(?:\/|$)|\/$)/;

export function isApplicationPath(pathname: string): boolean {
  return pathname === "/app" || pathname.startsWith("/app/");
}

export function isGeoPath(pathname: string): boolean {
  return pathname === "/geo" || pathname.startsWith("/geo/");
}

export function landingPolicyForProfile(profile: string | null | undefined): LandingPolicy {
  const normalized = (profile ?? "").trim().toLowerCase();
  return profileLandingPolicies[normalized] ?? defaultLandingPolicy;
}

export function defaultLoginPath(profile: string | null | undefined): LandingPolicy["loginPath"] {
  return landingPolicyForProfile(profile).loginPath;
}

export function defaultAuthenticatedPath(profile: string | null | undefined): LandingPolicy["authenticatedPath"] {
  return landingPolicyForProfile(profile).authenticatedPath;
}

export function isApplicationEntryPath(pathname: string, profile: string | null | undefined): boolean {
  return pathname === "/" && defaultLoginPath(profile) === "/app/login";
}

export function isGeoEntryPath(pathname: string, profile: string | null | undefined): boolean {
  return pathname === "/" && defaultLoginPath(profile) === "/geo/login";
}

/**
 * Application sessions are intentionally a separate identity contract from
 * the appliance's local operator sessions. The application OTP service will
 * issue this domain/method pair when it is integrated.
 */
export function isApplicationSession(session: Pick<Session, "domain" | "authMethod"> | null): boolean {
  return session?.domain === "application" && session.authMethod === "mobile_otp";
}

export function workspaceForPath(pathname: string, profile?: string | null): Workspace {
  if (isApplicationPath(pathname) || isApplicationEntryPath(pathname, profile)) {
    return "app";
  }
  if (isGeoPath(pathname) || isGeoEntryPath(pathname, profile)) {
    return "geo";
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
    value === "/console/login" ||
    value === "/app/login" ||
    value === "/geo/login" ||
    value === "/setup"
  ) {
    return fallback;
  }
  return INTERNAL_PATH.test(value) ? value : fallback;
}

export function loginDestination(pathname: string, search: string, profile?: string | null): string {
  const requested = new URLSearchParams(search).get("next");
  if (requested) {
    return safeNextPath(requested);
  }
  if (pathname === "/console/login") {
    return "/home";
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
  if (isGeoPath(pathname)) {
    return pathname === "/geo/login" ? "/geo" : pathname;
  }
  return pathname === "/" ? defaultAuthenticatedPath(profile) : "/home";
}

