import type { IconName } from "./ui";
import type { Session } from "./types";

export type Mode = {
  id: string;
  label: string;
  shortLabel: string;
  icon: IconName;
  defaultPath: string;
  features: Array<{ label: string; path: string; description: string; icon: IconName }>;
  visibleWhen: (context: NavigationContext) => boolean;
};

export type NavigationContext = {
  session: Pick<Session, "permissions" | "username">;
};

const ADMIN_USERNAMES = new Set(["admin", "administrator"]);
const ADMIN_PERMISSION_MARKERS = new Set([
  "audit.export",
  "audit.read",
  "roles.create",
  "roles.delete",
  "system.operate",
  "users.create",
  "users.disable"
]);

function alwaysVisible(): boolean {
  return true;
}

export function isSystemAdministrator(session: NavigationContext["session"]): boolean {
  const username = session.username.trim().toLowerCase();
  if (ADMIN_USERNAMES.has(username)) {
    return true;
  }
  return session.permissions.some((permission) =>
    ADMIN_PERMISSION_MARKERS.has(permission.trim().toLowerCase())
  );
}

export const MODES: Mode[] = [
  {
    id: "home",
    label: "Home",
    shortLabel: "HM",
    icon: "home",
    defaultPath: "/home",
    features: [],
    visibleWhen: alwaysVisible
  },
  {
    id: "manage",
    label: "Manage",
    shortLabel: "MG",
    icon: "manage",
    defaultPath: "/manage/builder",
    features: [
      {
        label: "Builder",
        path: "/manage/builder",
        description: "Workspaces, Git access, and build submission",
        icon: "builder"
      },
      {
        label: "DNS",
        path: "/manage/dns",
        description: "Manage LAN DNS records for the appliance zone",
        icon: "dns"
      },
      {
        label: "Artifacts",
        path: "/manage/artifacts",
        description: "Review artifact catalog and registry grants",
        icon: "artifacts"
      }
    ],
    visibleWhen: alwaysVisible
  },
  {
    id: "analyze",
    label: "Analyze",
    shortLabel: "AZ",
    icon: "analyze",
    defaultPath: "/analyze/workflows",
    features: [
      {
        label: "Workflows",
        path: "/analyze/workflows",
        description: "Operational view of current builder workflow activity",
        icon: "workflows"
      }
    ],
    visibleWhen: alwaysVisible
  },
  {
    id: "admin",
    label: "Admin",
    shortLabel: "AD",
    icon: "admin",
    defaultPath: "/admin/system-status",
    features: [
      {
        label: "System Status",
        path: "/admin/system-status",
        description: "Version, readiness, and appliance identity",
        icon: "status"
      },
      {
        label: "Profiles",
        path: "/admin/profiles",
        description: "Profile management entry points for future rollout",
        icon: "profiles"
      },
      {
        label: "Licensing",
        path: "/admin/licensing",
        description: "License and entitlement placeholder",
        icon: "license"
      }
    ],
    visibleWhen: (context) => isSystemAdministrator(context.session)
  }
];

export function visibleModes(context: NavigationContext): Mode[] {
  return MODES.filter((mode) => mode.visibleWhen(context));
}

export function currentMode(pathname: string, modes: Mode[] = MODES): Mode {
  return (
    modes.find((mode) => pathname === mode.defaultPath || pathname.startsWith(`/${mode.id}/`)) ??
    modes[0] ??
    MODES[0]
  );
}

export function modeUsesFeatureSelector(mode: Mode): boolean {
  return mode.features.length > 0;
}
