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
export interface DriveConnectionStatus {
  state: 'setup_required' | 'disconnected' | 'connecting' | 'connected' | 'reconnect_required' | 'client_changed' | 'storage_unavailable' | 'busy' | 'revoked_local_cleanup_required';
  clientConfigured: boolean;
  account?: { reference: string; displayName: string; email: string };
  message: string;
  scope: string;
}
export interface DriveDestination {
  id: string; name: string; accountReference: string; parents?: string[];
}
export interface DriveUploadPlan {
  planDigest: string; sourceName: string; destinationName: string; destinationId: string; accountReference: string;
  fileCount: number; folderCount: number; totalBytes: number; excludedCount: number;
  entries: { relativePath: string; kind: string; size: number; action?: string }[];
  expiresAt: string; warnings: string[];
}
export interface DriveTransferStatus {
  state: 'idle' | 'planning' | 'awaiting_approval' | 'uploading' | 'verifying' | 'succeeded' | 'failed' | 'cancelled' | 'needs_review';
  planDigest?: string; totalFiles: number; completedFiles: number; totalBytes: number; uploadedBytes: number;
  currentPath?: string; message: string; remoteFolderId?: string;
}
export interface DesktopBridge {
  OpenFolder(): Promise<Preview | null>;
  OpenConfiguration(): Promise<Preview | null>;
  Refresh(): Promise<Preview | null>;
  Cancel(): Promise<void>;
  GoogleDriveStatus(): Promise<DriveConnectionStatus>;
  ConnectGoogleDrive(): Promise<DriveConnectionStatus>;
  CheckGoogleDrive(): Promise<DriveConnectionStatus>;
  DisconnectGoogleDrive(): Promise<DriveConnectionStatus>;
  RevokeGoogleDrive(expectedAccountReference: string, confirmed: boolean): Promise<DriveConnectionStatus>;
  CancelGoogleDrive(): Promise<void>;
  ChooseDriveDestination(): Promise<DriveDestination | null>;
  UseMyDrive(): Promise<DriveDestination>;
  CurrentDriveDestination(): Promise<DriveDestination | null>;
  PreviewDriveUpload(): Promise<DriveUploadPlan>;
  StartDriveUpload(planDigest: string): Promise<DriveTransferStatus>;
  DriveTransferStatus(): Promise<DriveTransferStatus>;
  CancelDriveUpload(): Promise<void>;
  OpenUploadedDriveFolder(): Promise<void>;
}
declare global { interface Window { go?: { desktop?: { App?: DesktopBridge } } } }
