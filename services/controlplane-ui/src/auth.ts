import type { LoginResponse } from "./types";

const AUTH_STORAGE_KEY = "appliance.controlplane-ui.auth";

export function loadAuth(): LoginResponse | null {
  const raw = window.sessionStorage.getItem(AUTH_STORAGE_KEY);
  if (!raw) {
    return null;
  }
  try {
    return JSON.parse(raw) as LoginResponse;
  } catch {
    clearAuth();
    return null;
  }
}

export function saveAuth(tokens: LoginResponse | null): void {
  if (!tokens) {
    clearAuth();
    return;
  }
  window.sessionStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(tokens));
}

export function clearAuth(): void {
  window.sessionStorage.removeItem(AUTH_STORAGE_KEY);
}
