// Transport DTOs mirror the shared Go service. Selection and planning stay in Go.
export interface Provenance {
  adapter: string; mechanism: string; dialect: string; profileVersion: string;
  source: string; line: number; pattern: string; scope: string; action: string;
}
export interface GroupDecision {
  groupId: string; priority: number; decision: string;
  provenance?: Provenance; ancestorBlocker?: string;
}
export interface Explanation {
  path: string; kind: string; decision: string; reason: string;
  composition: string; groups: GroupDecision[];
}
export interface Entry {
  path: string; name: string; kind: string; size: number; modifiedAt: string;
  decision: string; status: string; explanation: Explanation;
}
export interface Operation {
  operationId: string; type: string; relativePath: string; expectedSize: number;
  explanation: string;
}
export interface Plan {
  planId: string; planDigest: string; rulesDigest: string; createdAt: string;
  destinationIdentity: string; scanComplete: { source: boolean; destination: boolean };
  operations: Operation[]; risks: string[];
  summary: { operationCount: number; uploadBytes: number; trashCount: number };
}
export interface Capability {
  adapter: string; mechanism: string; dialect: string; profileVersion: string;
  supported: boolean; limitations: string[];
}
export interface Preview {
  projectName: string; sourceRoot: string; offline: boolean;
  entries: Entry[]; plan: Plan; capabilities: Capability[];
}
export interface DesktopBridge {
  OpenFolder(): Promise<Preview | null>;
  OpenConfiguration(): Promise<Preview | null>;
  Refresh(): Promise<Preview | null>;
  Cancel(): Promise<void>;
}
declare global { interface Window { go?: { desktop?: { App?: DesktopBridge } } } }
