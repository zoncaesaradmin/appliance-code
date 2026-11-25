export interface ApplicationSessionStatus {
  authenticated: boolean;
}

export interface NearbyZone {
  id: string;
  name: string;
  latitude: number;
  longitude: number;
  distance_km: number;
}

export interface ActiveMembership {
	zone_id: string;
	name: string;
}

// Sentences the identity service writes for the person using the app.
// Transport failures, HTTP reason phrases, and operator diagnostics stay off the page.
const memberMessages = new Set([
  "Enter a valid mobile number with country code.",
  "Too many attempts. Try again later.",
  "Could not send a code. Try again.",
  "The code is invalid or expired.",
  "Could not verify the code. Try again.",
  "Sign-in is unavailable.",
  "Too many requests. Try again later.",
  "Send a valid location within the search radius.",
  "This link has expired. Start again from your channel.",
  "Nearby zone discovery is unavailable.",
  "Sign in first.",
  "Memberships are unavailable.",
  "Choose a valid zone and share your location.",
  "You are outside this Zonity's join radius.",
  "Could not submit the zone request.",
  "Enter a zone name between 2 and 100 characters.",
  "Sign in before requesting a new Zonity.",
  "Could not submit the Zonity request.",
  "Onboarding is unavailable."
]);

export class ApplicationAuthError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApplicationAuthError";
    this.status = status;
  }
}

export function presentedAuthError(reason: unknown, fallback: string): string {
  return reason instanceof ApplicationAuthError ? reason.message : fallback;
}

export function requiresSignIn(reason: unknown): boolean {
  return reason instanceof ApplicationAuthError && reason.status === 401;
}

function memberMessage(candidate: string, fallback: string): string {
  const message = candidate.trim();
  return memberMessages.has(message) ? message : fallback;
}

async function request<T>(path: string, fallback: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body) headers.set("Content-Type", "application/json");
  let response: Response;
  try {
    response = await fetch(path, { ...init, headers, credentials: "include" });
  } catch {
    throw new ApplicationAuthError(0, fallback);
  }
  if (!response.ok) {
    let candidate = "";
    try {
      const body = (await response.json()) as { message?: unknown };
      if (typeof body.message === "string") {
        candidate = body.message;
      }
    } catch {
      candidate = "";
    }
    throw new ApplicationAuthError(response.status, memberMessage(candidate, fallback));
  }
  try {
    return (await response.json()) as T;
  } catch {
    throw new ApplicationAuthError(response.status, fallback);
  }
}

export const applicationAuth = {
  session(): Promise<ApplicationSessionStatus> {
    return request<ApplicationSessionStatus>("/api/shared/v1/auth/session", "Sign-in is unavailable.");
  },
  memberships(): Promise<{ memberships: ActiveMembership[] }> {
    return request<{ memberships: ActiveMembership[] }>("/api/shared/v1/onboarding/memberships", "Could not load your Zonity memberships.");
  },
  challenge(phone: string): Promise<{ challenge_id: string }> {
    return request<{ challenge_id: string }>("/api/shared/v1/auth/otp/challenges", "Could not send a code. Try again.", {
      method: "POST",
      headers: { Origin: window.location.origin },
      body: JSON.stringify({ phone })
    });
  },
  verify(challengeId: string, code: string): Promise<void> {
    return request<void>("/api/shared/v1/auth/otp/verify", "Could not verify the code. Try again.", {
      method: "POST",
      headers: { Origin: window.location.origin },
      body: JSON.stringify({ challenge_id: challengeId, code })
    });
  },
  logout(): Promise<void> {
    return request<void>("/api/shared/v1/auth/logout", "Could not sign out. Try again.", {
      method: "POST",
      headers: { Origin: window.location.origin },
      body: "{}"
    }).then(() => undefined);
  },
  nearby(latitude: number, longitude: number, discoveryToken = ""): Promise<{ zones: NearbyZone[] }> {
    return request<{ zones: NearbyZone[] }>("/api/shared/v1/onboarding/nearby", "Could not find nearby Zonities.", {
      method: "POST",
      headers: { Origin: window.location.origin },
      body: JSON.stringify({ latitude, longitude, radius_km: 10, ...(discoveryToken ? { discovery_token: discoveryToken } : {}) })
    });
  },
  join(zoneID: string, latitude: number, longitude: number): Promise<{ zone_id: string; status: string; approval_mode: string }> {
    return request<{ zone_id: string; status: string; approval_mode: string }>("/api/shared/v1/onboarding/join", "Could not submit the join request.", {
      method: "POST",
      headers: { Origin: window.location.origin },
      body: JSON.stringify({ zone_id: zoneID, latitude, longitude })
    });
  },
  requestZone(name: string): Promise<{ name: string; status: string }> {
    return request<{ name: string; status: string }>("/api/shared/v1/onboarding/zone-requests", "Could not submit the Zonity request.", {
      method: "POST",
      headers: { Origin: window.location.origin },
      body: JSON.stringify({ name })
    });
  }
};
