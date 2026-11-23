export interface ApplicationSessionStatus {
  authenticated: boolean;
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body) headers.set("Content-Type", "application/json");
  const response = await fetch(path, { ...init, headers, credentials: "include" });
  if (!response.ok) {
    let message = response.statusText || "Application sign-in failed.";
    try {
      const body = (await response.json()) as { message?: string };
      message = body.message || message;
    } catch {
      // Keep the status text when the API did not return JSON.
    }
    throw new Error(message);
  }
  return (await response.json()) as T;
}

export const applicationAuth = {
  session(): Promise<ApplicationSessionStatus> {
    return request<ApplicationSessionStatus>("/api/shared/v1/auth/session");
  },
  challenge(phone: string): Promise<{ challenge_id: string }> {
    return request<{ challenge_id: string }>("/api/shared/v1/auth/otp/challenges", {
      method: "POST",
      headers: { Origin: window.location.origin },
      body: JSON.stringify({ phone })
    });
  },
  verify(challengeId: string, code: string): Promise<void> {
    return request<void>("/api/shared/v1/auth/otp/verify", {
      method: "POST",
      headers: { Origin: window.location.origin },
      body: JSON.stringify({ challenge_id: challengeId, code })
    });
  },
  logout(): Promise<void> {
    return request<void>("/api/shared/v1/auth/logout", {
      method: "POST",
      headers: { Origin: window.location.origin },
      body: "{}"
    }).then(() => undefined);
  }
};
