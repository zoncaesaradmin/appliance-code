export interface ApplicationSessionStatus {
  authenticated: boolean;
}

export interface NearbyZone {
  id: string;
  name: string;
  distance_km: number;
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
  },
  nearby(latitude: number, longitude: number, discoveryToken = ""): Promise<{ zones: NearbyZone[] }> {
    return request<{ zones: NearbyZone[] }>("/api/shared/v1/onboarding/nearby", {
      method: "POST",
      headers: { Origin: window.location.origin },
      body: JSON.stringify({ latitude, longitude, radius_km: 10, ...(discoveryToken ? { discovery_token: discoveryToken } : {}) })
    });
  },
  join(zoneID: string, discoveryToken = ""): Promise<{ zone_id: string; status: string }> {
    return request<{ zone_id: string; status: string }>("/api/shared/v1/onboarding/join", {
      method: "POST",
      headers: { Origin: window.location.origin },
      body: JSON.stringify({ zone_id: zoneID, ...(discoveryToken ? { discovery_token: discoveryToken } : {}) })
    });
  },
  requestZone(name: string): Promise<{ name: string; status: string }> {
    return request<{ name: string; status: string }>("/api/shared/v1/onboarding/zone-requests", {
      method: "POST",
      headers: { Origin: window.location.origin },
      body: JSON.stringify({ name })
    });
  }
};
