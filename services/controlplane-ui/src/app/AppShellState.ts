import type { Session } from "../types";

export type AppShellState = {
  booting: boolean;
  initialized: boolean;
  capabilities: string[];
  session: Session | null;
};
