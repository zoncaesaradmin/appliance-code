import type { Session } from "../types";

export type AppShellState = {
  booting: boolean;
  initialized: boolean;
  profile: string;
  capabilities: string[];
  session: Session | null;
};
