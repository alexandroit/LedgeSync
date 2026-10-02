import { expect, test, type Page } from '@playwright/test';
import type { DriveConnectionStatus, DriveDestination, DriveTransferStatus, DriveUploadPlan, Preview, Project, RunSummary, Settings } from '../src/types';

const account = 'google-drive:pairs-fixture';
const connected: DriveConnectionStatus = { state: 'connected', clientConfigured: true, account: { reference: account, displayName: 'Fixture', email: 'pairs@example.invalid' }, message: 'Saved connection.', scope: 'https://www.googleapis.com/auth/drive.file' };
const destination: DriveDestination = { id: 'fixture-parent', name: 'Backups', accountReference: account };
const preview: Preview = { projectName: 'Local preview', sourceRoot: '/synthetic/Client Work', offline: true, entries: [], capabilities: [], plan: { planId: 'p', planDigest: 'local', rulesDigest: 'rules', createdAt: '2026-10-02T00:00:00Z', destinationIdentity: 'none', scanComplete: { source: true, destination: true }, operations: [], risks: [], summary: { operationCount: 0, uploadBytes: 0, trashCount: 0 } } };
const plan: DriveUploadPlan = { planDigest: 'pairs-plan', sourceName: 'Client Work', destinationName: 'Backups', destinationId: destination.id, accountReference: account, fileCount: 3, folderCount: 2, totalBytes: 3000, transferBytes: 1000, newFiles: 1, changedFiles: 1, unchangedFiles: 1, excludedCount: 2, expiresAt: '2099-01-01T00:00:00Z', warnings: [], entries: [{ relativePath: '', kind: 'directory', size: 0, action: 'skip' }, { relativePath: 'a.txt', kind: 'file', size: 1000, action: 'skip' }, { relativePath: 'b.txt', kind: 'file', size: 1000, action: 'upload' }, { relativePath: 'c.txt', kind: 'file', size: 1000, action: 'keep-both', note: 'Changed since the last copy; the earlier copy remains.' }] };
const run: RunSummary = { runId: 'run-1', projectId: 'pair-1', projectName: 'Client Work', destination: 'Backups', trigger: 'automatic', state: 'partial', message: 'Copied and verified the approved items that were unchanged.', startedAt: '2026-10-02T10:00:00Z', finishedAt: '2026-10-02T10:01:00Z', totalFiles: 3, completedFiles: 2, skippedFiles: 1, pausedFiles: 0, totalBytes: 3000, uploadedBytes: 2000, sentBytes: 1000, remoteFolderId: 'managed-folder', issues: [{ path: 'notes/draft.md', code: 'SOURCE_CHANGED', message: 'This file\'s content changed after the preview: notes/draft.md' }] };
const project: Project = { id: 'pair-1', name: 'Client Work', sourceRoot: '/synthetic/Client Work', policy: { composition: 'conservative', conflictPolicy: 'keep-both', maxRetries: 6, groups: [{ id: 'git', priority: 100, enabled: true, dialect: 'gitignore', scope: 'project', sources: [{ type: 'recursive-basename', value: '.gitignore', required: false }] }] }, destination, automation: { enabled: false, trigger: '', intervalSeconds: 0, paused: false }, sourceIdentity: 'source-identity', createdAt: '2026-10-02T09:00:00Z', updatedAt: '2026-10-02T10:01:00Z', lastRun: run };
const settings: Settings = { defaultConflictPolicy: 'keep-both', defaultMaxRetries: 6, automationPaused: false };

interface PairsHarness { calls: string[]; args: unknown[][]; setProject(value: Project): void; failPreview(message: string): void; setStatus(value: DriveTransferStatus): void }
declare global { interface Window { pairsHarness: PairsHarness } }

async function start(page: Page) {
  page.on('dialog', dialog => void dialog.accept());
  await page.addInitScript(({ connected, destination, preview, plan, run, project, settings }) => {
    const calls: string[] = [];
    const args: unknown[][] = [];
    let current = project;
    let previewFailure = '';
    let status: DriveTransferStatus = { state: 'idle', totalFiles: 0, completedFiles: 0, totalBytes: 0, uploadedBytes: 0, message: '' };
    let opened: Project | null = null;
    let config = settings;
    const record = (name: string, ...values: unknown[]) => { calls.push(name); args.push(values); };
    window.pairsHarness = { calls, args, setProject(value) { current = value; }, failPreview(message) { previewFailure = message; }, setStatus(value) { status = value; } };
    window.go = { desktop: { App: {
      OpenFolder: async () => preview, OpenConfiguration: async () => preview, Refresh: async () => preview, Cancel: async () => {},
      GoogleDriveStatus: async () => connected, ConnectGoogleDrive: async () => connected, CheckGoogleDrive: async () => connected, DisconnectGoogleDrive: async () => connected, RevokeGoogleDrive: async () => connected, CancelGoogleDrive: async () => {},
      ChooseDriveDestination: async () => destination, UseMyDrive: async () => destination, CurrentDriveDestination: async () => opened ? destination : null,
      PreviewDriveUpload: async () => { record('preview'); if (previewFailure) throw new Error(previewFailure); return plan; },
      StartDriveUpload: async () => { record('start'); return status; }, DriveTransferStatus: async () => status, CancelDriveUpload: async () => {}, OpenUploadedDriveFolder: async () => {},
      ListProjects: async () => { record('list'); return [current]; },
      CurrentProject: async () => opened,
      OpenProject: async (id: string) => { record('open', id); opened = current; return { project: current, preview, destination }; },
      RenameProject: async (id: string, name: string) => { record('rename', id, name); current = { ...current, name }; return current; },
      ForgetProject: async (id: string) => { record('forget', id); },
      UpdateProjectPolicy: async (id: string, policy: Project['policy']) => { record('policy', id, policy); if (policy.groups.length > 1 && policy.groups[1].sources[0].value === '') throw new Error('CONFIG_INVALID: unsafe rule source path'); current = { ...current, policy }; opened = current; return { project: current, preview, destination }; },
      ProjectHistory: async (id: string) => { record('history', id); return [run]; },
      ClearHistory: async () => { record('clear'); },
      GetSettings: async () => config,
      SaveSettings: async (next: Settings) => { record('settings', next); config = next; return config; },
      OpenProjectDriveFolder: async (id: string) => { record('open-drive', id); },
      AuthorizeAutomation: async (id: string, trigger: string, interval: number, digest: string) => { record('authorize', id, trigger, interval, digest); current = { ...current, automation: { enabled: true, trigger: trigger as 'interval', intervalSeconds: interval, paused: false, authorization: { accountReference: connected.account!.reference, destinationId: destination.id, sourceIdentity: 's', configDigest: 'c', rulesDigest: 'r', conflictPolicy: 'keep-both', planDigest: digest, approvedAt: '2026-10-02T10:05:00Z' } } }; opened = current; return current; },
      DisableAutomation: async (id: string) => { record('disable', id); current = { ...current, automation: { enabled: false, trigger: '', intervalSeconds: 0, paused: false } }; opened = current; return current; },
      ResumeAutomation: async (id: string) => { record('resume', id); return current; },
      CheckProjectNow: async (id: string) => { record('check-now', id); },
      AutomationStatus: async () => ({ available: true, paused: config.automationPaused, running: false }),
      CancelAutomaticCopy: async () => {},
      RestoreProjectCopy: async (id: string) => { record('restore', id); return { state: 'restoring', target: '/synthetic/Restored', totalFiles: 2, files: 0, folders: 0, totalBytes: 2000, bytes: 0, message: 'Reading the recorded copy.' }; },
      RestoreStatus: async () => ({ state: 'partial', target: '/synthetic/Restored', totalFiles: 2, files: 1, folders: 1, totalBytes: 2000, bytes: 1000, message: 'Restored the available verified files.', issues: [{ path: 'a.txt', code: 'REMOTE_CHANGED', message: 'This item was trashed, moved, renamed or replaced in Drive.' }] }),
      CancelRestore: async () => {},
    } } } as never;
  }, { connected, destination, preview, plan, run, project, settings });
  await page.goto('/');
}

test('saved pairs reopen without approval and list last outcome', async ({ page }) => {
  await start(page);
  await page.getByRole('button', { name: 'Sync pairs', exact: true }).click();
  const card = page.getByRole('listitem', { name: 'Sync pair Client Work' });
  await expect(card).toContainText('Upload verified with skipped files');
  await expect(card).toContainText('automatic');
  await card.getByRole('button', { name: 'Open', exact: true }).click();
  await expect(page.locator('.destination-name')).toHaveText('Destination: Backups');
  await expect(page.getByText('Saved sync pair: Client Work', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => window.pairsHarness.calls)).not.toContain('start');
});

test('typed backend errors show the redacted reason and guidance; nothing is approved', async ({ page }) => {
  await start(page);
  await page.getByRole('button', { name: 'Choose a local folder', exact: true }).click();
  await page.getByRole('button', { name: 'Choose existing Drive folder', exact: true }).click();
  await page.evaluate(() => window.pairsHarness.failPreview('DRIVE_STORAGE_FULL: Google Drive storage is full.'));
  await page.getByRole('button', { name: 'Preview folder upload', exact: true }).click();
  const alert = page.getByRole('alert');
  await expect(alert).toContainText('Google Drive storage is full.');
  await expect(alert).toContainText('Free up Google Drive storage');
  await expect(alert).toContainText('Nothing has been approved.');
  await expect(page.getByRole('button', { name: 'Upload folder', exact: true })).toHaveCount(0);
});

test('plan review summarizes new, changed and unchanged files and enables automation only after confirmation', async ({ page }) => {
  await start(page);
  await page.getByRole('button', { name: 'Sync pairs', exact: true }).click();
  await page.getByRole('listitem', { name: 'Sync pair Client Work' }).getByRole('button', { name: 'Open', exact: true }).click();
  await page.getByRole('button', { name: 'Preview folder upload', exact: true }).click();
  const review = page.getByRole('region', { name: 'Review folder upload' });
  await expect(review).toContainText('Files: 1 new · 1 changed · 1 unchanged');
  await expect(review).toContainText('Keep both versions');
  await expect(review).toContainText('Changed since the last copy; the earlier copy remains.');
  await page.getByLabel('Automatic copy schedule').selectOption('watch');
  await page.getByLabel('How often').selectOption('3600');
  await page.getByRole('button', { name: 'Allow automatic copies…', exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.pairsHarness.calls)).toContain('authorize');
  const args = await page.evaluate(() => window.pairsHarness.args[window.pairsHarness.calls.indexOf('authorize')]);
  expect(args).toEqual(['pair-1', 'watch', 3600, 'pairs-plan']);
  expect(await page.evaluate(() => window.pairsHarness.calls)).not.toContain('start');
});

test('partial runs list the files that were not copied', async ({ page }) => {
  await start(page);
  await page.getByRole('button', { name: 'Choose a local folder', exact: true }).click();
  await page.getByRole('button', { name: 'Choose existing Drive folder', exact: true }).click();
  await page.evaluate(() => window.pairsHarness.setStatus({ state: 'uploading', planDigest: 'pairs-plan', totalFiles: 3, completedFiles: 0, totalBytes: 3000, uploadedBytes: 0, transferBytes: 1000, sentBytes: 0, message: 'Uploading.' }));
  await page.getByRole('button', { name: 'Preview folder upload', exact: true }).click();
  await page.getByRole('button', { name: 'Upload folder', exact: true }).click();
  await page.evaluate(() => window.pairsHarness.setStatus({ state: 'partial', planDigest: 'pairs-plan', totalFiles: 3, completedFiles: 2, skippedFiles: 1, totalBytes: 3000, uploadedBytes: 2000, message: 'Copied and verified the approved items that were unchanged.', remoteFolderId: 'managed-folder', issues: [{ path: 'notes/draft.md', code: 'SOURCE_CHANGED', message: 'This file changed after the preview.' }] }));
  const transfer = page.getByRole('region', { name: 'Folder transfer' });
  await expect(transfer.getByRole('heading', { name: 'Upload verified with skipped files', exact: true })).toBeVisible();
  await expect(transfer).toContainText('1 item not copied in this run');
  await expect(transfer).toContainText('notes/draft.md');
  await expect(transfer.getByRole('button', { name: 'Open destination folder on Google Drive', exact: true })).toBeVisible();
});

test('activity, history and settings use saved data and confirm destructive local actions', async ({ page }) => {
  await start(page);
  await page.getByRole('button', { name: 'Activity', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Activity', exact: true })).toBeVisible();
  await expect(page.getByRole('article', { name: 'Client Work partial' })).toContainText('notes/draft.md');
  await page.getByRole('button', { name: 'History & Recovery', exact: true }).click();
  await expect(page.getByText('LedgeSync never deletes or overwrites Drive files.', { exact: false })).toBeVisible();
  await page.getByLabel('Sync pair history').selectOption('pair-1');
  await page.getByRole('button', { name: 'Open copy in Google Drive', exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.pairsHarness.calls)).toContain('open-drive');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByLabel('When a copied file changes').selectOption('pause');
  await page.getByRole('button', { name: 'Save defaults', exact: true }).click();
  await expect(page.getByText('Defaults saved.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Pause all automatic copies', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Resume automatic copies', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Clear run history on this computer…', exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.pairsHarness.calls)).toContain('clear');
  const saved = await page.evaluate(() => window.pairsHarness.args.filter((_, i) => window.pairsHarness.calls[i] === 'settings'));
  expect(saved[0]).toEqual([{ defaultConflictPolicy: 'pause', defaultMaxRetries: 6, automationPaused: false }]);
});

test('policy editor saves validated groups and shows typed validation errors', async ({ page }) => {
  await start(page);
  await page.getByRole('button', { name: 'Sync pairs', exact: true }).click();
  await page.getByRole('listitem', { name: 'Sync pair Client Work' }).getByRole('button', { name: 'Open', exact: true }).click();
  await page.getByRole('button', { name: 'Policies', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Selection policy', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Add rule group', exact: true }).click();
  await page.getByLabel('Source 1 of rules-2').fill('');
  await page.getByLabel('Source 1 of rules-2').dispatchEvent('change');
  await page.getByRole('button', { name: 'Save policy and rescan', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('unsafe rule source path');
  await page.getByLabel('Source 1 of rules-2').fill('team-upload-rules.txt');
  await page.getByLabel('Source 1 of rules-2').dispatchEvent('change');
  await page.getByLabel('Dialect of rules-2').selectOption('rclone-filter');
  await page.getByRole('button', { name: 'Save policy and rescan', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveCount(0);
  const policies = await page.evaluate(() => window.pairsHarness.args.filter((_, i) => window.pairsHarness.calls[i] === 'policy'));
  const last = policies[policies.length - 1] as [string, Project['policy']];
  expect(last[1].groups[1]).toEqual({ id: 'rules-2', priority: 90, enabled: true, dialect: 'rclone-filter', scope: 'project', sources: [{ type: 'root-file', value: 'team-upload-rules.txt', required: false }] });
});

test('restore downloads the verified copy into a new folder and reports missing items', async ({ page }) => {
  await start(page);
  await page.getByRole('button', { name: 'History & Recovery', exact: true }).click();
  await page.getByLabel('Sync pair history').selectOption('pair-1');
  await page.getByRole('button', { name: 'Restore this copy to a new folder…', exact: true }).click();
  const card = page.getByRole('region', { name: 'Restore' });
  await expect(card.getByRole('heading', { name: 'Copy restored with missing items', exact: true })).toBeVisible();
  await expect(card).toContainText('1 of 2 files restored');
  await expect(card).toContainText('a.txt');
  expect(await page.evaluate(() => window.pairsHarness.calls)).toContain('restore');
});
