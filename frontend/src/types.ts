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
  ruleSources?: string[]; unsupported?: number; pruned?: string[];
}
export interface DriveConnectionStatus {
  state: 'setup_required' | 'disconnected' | 'connecting' | 'connected' | 'reconnect_required' | 'client_changed' | 'storage_unavailable' | 'busy' | 'revoked_local_cleanup_required';
  clientConfigured: boolean;
  account?: { reference: string; displayName: string; email: string };
  message: string;
  scope: string;
}
export interface DriveDestination {
  id: string; name: string; accountReference: string; parents?: string[]; myDrive?: boolean;
}
export type UploadAction = 'create' | 'upload' | 'keep-both' | 'skip' | 'resume' | 'recreate' | 'paused' | 'unsupported';
export interface DriveUploadPlan {
  planDigest: string; sourceName: string; destinationName: string; destinationId: string; accountReference: string;
  fileCount: number; folderCount: number; totalBytes: number; excludedCount: number;
  transferBytes?: number; newFiles?: number; changedFiles?: number; unchangedFiles?: number; recreatedItems?: number;
  pausedFiles?: number; unsupportedCount?: number; conflictPolicy?: string;
  sourceIdentity?: string; configDigest?: string; rulesDigest?: string; sourceDigest?: string;
  entries: { relativePath: string; kind: string; size: number; action?: UploadAction | string; note?: string }[];
  expiresAt: string; warnings: string[];
}
export interface TransferIssue { path: string; code: string; message: string }
export type TransferState = 'idle' | 'planning' | 'awaiting_approval' | 'uploading' | 'verifying' | 'succeeded' | 'partial' | 'failed' | 'cancelled' | 'needs_review';
export interface DriveTransferStatus {
  state: TransferState;
  planDigest?: string; runId?: string; totalFiles: number; completedFiles: number; skippedFiles?: number; pausedFiles?: number;
  totalBytes: number; uploadedBytes: number; transferBytes?: number; sentBytes?: number;
  currentPath?: string; message: string; errorCode?: string; remoteFolderId?: string; issues?: TransferIssue[];
  startedAt?: string; finishedAt?: string;
}
export interface RuleSource { type: 'recursive-basename' | 'root-file' | 'recursive-vcs-property'; value: string; required: boolean }
export interface PolicyGroup { id: string; priority: number; enabled: boolean; dialect: string; scope: string; sources: RuleSource[] }
export interface ProjectPolicy { composition: 'conservative' | 'ordered'; conflictPolicy: 'keep-both' | 'pause'; maxRetries: number; groups: PolicyGroup[] }
export interface AutomationAuthorization { accountReference: string; destinationId: string; sourceIdentity: string; configDigest: string; rulesDigest: string; conflictPolicy: string; planDigest: string; approvedAt: string }
export interface ProjectAutomation {
  enabled: boolean; trigger: 'interval' | 'watch' | ''; intervalSeconds: number; authorization?: AutomationAuthorization;
  paused: boolean; pauseCode?: string; pauseReason?: string; lastCheckedAt?: string; nextRunAt?: string; waiting?: string;
}
export interface RunSummary {
  runId: string; projectId: string; projectName: string; destination: string; trigger: 'manual' | 'automatic' | string;
  state: TransferState | string; errorCode?: string; message: string; startedAt: string; finishedAt: string;
  totalFiles: number; completedFiles: number; skippedFiles: number; pausedFiles: number;
  totalBytes: number; uploadedBytes: number; sentBytes: number; remoteFolderId?: string; issues?: TransferIssue[];
}
export interface Project {
  id: string; name: string; sourceRoot: string; configPath?: string; policy: ProjectPolicy;
  destination: DriveDestination; automation: ProjectAutomation; sourceIdentity?: string; createdAt: string; updatedAt: string; lastRun?: RunSummary;
}
export interface ErrorInfo { code: string; message: string }
export interface ProjectSession { project: Project; preview: Preview | null; destination?: DriveDestination; destinationError?: ErrorInfo }
export interface AutomationView { available: boolean; paused: boolean; activeProjectId?: string; running: boolean; transfer?: DriveTransferStatus }
export interface RestoreProgress {
  state: 'restoring' | 'succeeded' | 'partial' | 'failed' | 'cancelled';
  target: string; totalFiles: number; files: number; folders: number; totalBytes: number; bytes: number;
  currentPath?: string; errorCode?: string; message: string; issues?: TransferIssue[];
}
export interface Settings { defaultConflictPolicy: 'keep-both' | 'pause'; defaultMaxRetries: number; automationPaused: boolean }
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
  ListProjects?(): Promise<Project[]>;
  CurrentProject?(): Promise<Project | null>;
  OpenProject?(id: string): Promise<ProjectSession>;
  SaveCurrentProject?(name: string): Promise<Project>;
  RenameProject?(id: string, name: string): Promise<Project>;
  ForgetProject?(id: string): Promise<void>;
  DefaultPolicy?(): Promise<ProjectPolicy>;
  UpdateProjectPolicy?(id: string, policy: ProjectPolicy): Promise<ProjectSession>;
  ProjectHistory?(id: string): Promise<RunSummary[]>;
  ClearHistory?(): Promise<void>;
  GetSettings?(): Promise<Settings>;
  SaveSettings?(settings: Settings): Promise<Settings>;
  OpenProjectDriveFolder?(id: string): Promise<void>;
  AuthorizeAutomation?(id: string, trigger: string, intervalSeconds: number, planDigest: string): Promise<Project>;
  DisableAutomation?(id: string): Promise<Project>;
  ResumeAutomation?(id: string): Promise<Project>;
  CheckProjectNow?(id: string): Promise<void>;
  AutomationStatus?(): Promise<AutomationView>;
  CancelAutomaticCopy?(): Promise<void>;
  RestoreProjectCopy?(id: string): Promise<RestoreProgress | null>;
  RestoreStatus?(): Promise<RestoreProgress | null>;
  CancelRestore?(): Promise<void>;
}
declare global { interface Window { go?: { desktop?: { App?: DesktopBridge } }; runtime?: { EventsOn?(name: string, callback: () => void): () => void } } }
