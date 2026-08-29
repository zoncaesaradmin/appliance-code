import type { LoginResponse } from "./types";

const AUTH_STORAGE_KEY = "appliance.controlplane-ui.auth";
const AUTH_PERSIST_KEY = "appliance.controlplane-ui.auth.persist";

function readStore(store: Storage | undefined, key: string): string | null {
  try {
    return store?.getItem(key) ?? null;
  } catch {
    return null;
  }
}

function writeStore(store: Storage | undefined, key: string, value: string): void {
  try {
    store?.setItem(key, value);
  } catch {
    // Private-mode or missing storage must not crash sign-in.
  }
}

function removeStore(store: Storage | undefined, key: string): void {
  try {
    store?.removeItem(key);
  } catch {
    // Ignore storage access failures.
  }
}

export function isAuthPersisted(): boolean {
  return readStore(window.localStorage, AUTH_PERSIST_KEY) === "1";
}

export function setAuthPersist(persist: boolean): void {
  if (persist) {
    writeStore(window.localStorage, AUTH_PERSIST_KEY, "1");
    return;
  }
  removeStore(window.localStorage, AUTH_PERSIST_KEY);
}

function readAuthRaw(): string | null {
  return readStore(window.localStorage, AUTH_STORAGE_KEY) || readStore(window.sessionStorage, AUTH_STORAGE_KEY);
}

export function loadAuth(): LoginResponse | null {
  const raw = readAuthRaw();
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
  const serialized = JSON.stringify(tokens);
  removeStore(window.sessionStorage, AUTH_STORAGE_KEY);
  removeStore(window.localStorage, AUTH_STORAGE_KEY);
  writeStore(isAuthPersisted() ? window.localStorage : window.sessionStorage, AUTH_STORAGE_KEY, serialized);
}

export function clearAuth(): void {
  removeStore(window.sessionStorage, AUTH_STORAGE_KEY);
  removeStore(window.localStorage, AUTH_STORAGE_KEY);
}
