import { expect, test, type Page } from '@playwright/test';
import type { DriveConnectionStatus, DriveFolder, SyncActivity, SyncStatus } from '../src/types';

const account = 'google-drive:sync-fixture';
const connected: DriveConnectionStatus = { state: 'connected', clientConfigured: true, account: { reference: account, displayName: 'Fixture', email: 'sync@example.invalid' }, message: 'Saved connection.', scope: 'https://www.googleapis.com/auth/drive' };
const disconnected: DriveConnectionStatus = { state: 'disconnected', clientConfigured: true, message: 'Ready.', scope: 'https://www.googleapis.com/auth/drive' };
const pair = (id: string, name: string) => ({ id, name, localRoot: `/synthetic/${name}`, accountReference: account, parent: { folderId: 'root', name: 'My Drive' }, remoteRootId: `remote-${id}`, paused: false, createdAt: '2026-10-02T10:00:00Z' });
const synced: SyncStatus = { pair: pair('p1', 'Projects'), state: 'synced', message: 'Up to date', uploads: 0, downloads: 0, done: 0, total: 0, issues: [{ path: 'Budget', code: 'GOOGLE_FILE_SKIPPED', message: 'Google Docs, Sheets, Slides and shortcuts stay in Drive and are not downloaded.' }], lastSyncAt: '2026-10-02T10:05:00Z', driveUrl: 'https://drive.google.com/drive/folders/remote-p1' };
const syncing: SyncStatus = { pair: pair('p2', 'Photos'), state: 'syncing', message: 'Syncing…', uploads: 3, downloads: 2, done: 2, total: 5, currentPath: 'trip/beach.jpg', issues: [] };
const confirm: SyncStatus = { pair: pair('p3', 'Archive'), state: 'confirm_deletes', message: 'Many files were deleted on one side.', errorCode: 'SYNC_DELETE_CONFIRMATION', uploads: 0, downloads: 0, done: 0, total: 0, remoteDeletes: 42, issues: [] };
const activity: SyncActivity[] = [
  { at: '2026-10-02T10:04:00Z', kind: 'download', path: 'from web.pdf' },
  { at: '2026-10-02T10:03:00Z', kind: 'conflict', path: 'report.txt', detail: 'report (conflict 2026-10-02 100300).txt' },
  { at: '2026-10-02T10:02:00Z', kind: 'delete_up', path: 'old.txt', detail: 'Moved to the Google Drive trash' },
];
const folders: Record<string, DriveFolder[]> = { root: [{ id: 'work-id', name: 'Work', parents: ['root'], canAddChildren: true }], 'work-id': [{ id: 'clients-id', name: 'Clients', parents: ['work-id'], canAddChildren: true }] };

interface SyncHarness { calls: string[]; args: unknown[][] }
declare global { interface Window { syncHarness: SyncHarness } }

async function start(page: Page, drive: DriveConnectionStatus = connected, list: SyncStatus[] = [synced, syncing, confirm]) {
  page.on('dialog', dialog => void dialog.accept());
  await page.addInitScript(({ drive, list, activity, folders }) => {
    const calls: string[] = [];
    const args: unknown[][] = [];
    let current = list;
    const record = (name: string, ...values: unknown[]) => { calls.push(name); args.push(values); };
    window.syncHarness = { calls, args };
    const find = (id: string) => current.find(s => s.pair.id === id)!;
    window.go = { desktop: { App: {
      OpenFolder: async () => null, OpenConfiguration: async () => null, Refresh: async () => null, Cancel: async () => {},
      GoogleDriveStatus: async () => drive, ConnectGoogleDrive: async () => drive, CheckGoogleDrive: async () => drive, DisconnectGoogleDrive: async () => drive, RevokeGoogleDrive: async () => drive, CancelGoogleDrive: async () => {},
      ChooseDriveDestination: async () => null, UseMyDrive: async () => null, CurrentDriveDestination: async () => null,
      PreviewDriveUpload: async () => { throw new Error('unused'); }, StartDriveUpload: async () => { throw new Error('unused'); }, DriveTransferStatus: async () => ({ state: 'idle', totalFiles: 0, completedFiles: 0, totalBytes: 0, uploadedBytes: 0, message: '' }), CancelDriveUpload: async () => {}, OpenUploadedDriveFolder: async () => {},
      SyncList: async () => { record('list'); return current; },
      SyncChooseFolder: async (parentID: string, parentName: string) => {
        record('choose', parentID, parentName);
        const added = { pair: { id: 'p4', name: 'New Folder', localRoot: '/synthetic/New Folder', accountReference: 'a', parent: { folderId: parentID, name: parentName }, remoteRootId: 'r4', paused: false, createdAt: '2026-10-02T10:06:00Z' }, state: 'starting', message: 'Starting…', uploads: 0, downloads: 0, done: 0, total: 0, issues: [] } as SyncStatus;
        current = [...current, added]; return added;
      },
      SyncPause: async (id: string) => { record('pause', id); current = current.map(s => s.pair.id === id ? { ...s, state: 'paused', message: 'Sync is paused.', pair: { ...s.pair, paused: true } } : s); return find(id); },
      SyncResume: async (id: string) => { record('resume', id); current = current.map(s => s.pair.id === id ? { ...s, state: 'starting', message: 'Starting…', pair: { ...s.pair, paused: false } } : s); return find(id); },
      SyncNow: async (id: string) => { record('now', id); return find(id); },
      SyncConfirmDeletes: async (id: string) => { record('confirm', id); current = current.map(s => s.pair.id === id ? { ...s, state: 'syncing', message: 'Syncing…' } : s); return find(id); },
      SyncRestoreDeletes: async (id: string) => { record('restore', id); current = current.map(s => s.pair.id === id ? { ...s, state: 'syncing', message: 'Syncing…' } : s); return find(id); },
      SyncRemove: async (id: string) => { record('remove', id); current = current.filter(s => s.pair.id !== id); },
      SyncActivity: async () => activity,
      SyncOpenLocal: async (id: string) => { record('open-local', id); },
      SyncOpenDrive: async (id: string) => { record('open-drive', id); },
      DriveFolders: async (parent: string) => { record('folders', parent); return folders[parent] ?? []; },
    } } } as never;
  }, { drive, list, activity, folders });
  await page.goto('/');
}

test('opens on synced folders with live status, progress and items not synced', async ({ page }) => {
  await start(page);
  await expect(page.getByRole('heading', { name: 'Synced folders' })).toBeVisible();
  const projects = page.getByRole('listitem', { name: 'Synced folder Projects' });
  await expect(projects.getByText('Up to date', { exact: true }).first()).toBeVisible();
  await expect(projects.getByText('Google Drive: My Drive › Projects')).toBeVisible();
  await projects.getByText('1 item not synced').click();
  await expect(projects.getByText(/Budget — Google Docs, Sheets, Slides/)).toBeVisible();
  const photos = page.getByRole('listitem', { name: 'Synced folder Photos' });
  await expect(photos.getByText('2 of 5 · ↑ 3 to Drive · ↓ 2 to this computer')).toBeVisible();
  await expect(photos.getByText('trip/beach.jpg')).toBeVisible();
  await expect(page.getByText('Downloaded from Drive')).toBeVisible();
  await expect(page.getByText('Your version was kept as report (conflict 2026-10-02 100300).txt')).toBeVisible();
  await expect(page.getByText('Moved to the Google Drive trash').first()).toBeVisible();
});

test('choosing a folder starts syncing it into the selected Drive location', async ({ page }) => {
  await start(page);
  await page.locator('#sync-add').click();
  await expect(page.getByRole('listitem', { name: 'Synced folder New Folder' })).toBeVisible();
  let harness = await page.evaluate(() => window.syncHarness);
  expect(harness.args[harness.calls.indexOf('choose')]).toEqual(['root', 'My Drive']);
  await page.locator('#sync-location-change').click();
  await page.getByRole('button', { name: 'Work' }).click();
  await expect(page.getByRole('button', { name: 'Clients' })).toBeVisible();
  await page.locator('#folder-use').click();
  await expect(page.locator('.sync-location strong')).toHaveText('Work');
  await page.getByRole('button', { name: 'Sync a folder' }).first().click();
  harness = await page.evaluate(() => window.syncHarness);
  expect(harness.args[harness.calls.lastIndexOf('choose')]).toEqual(['work-id', 'Work']);
  expect(harness.calls).toContain('folders');
});

test('many deletions wait for a decision: delete on the other side or restore', async ({ page }) => {
  await start(page);
  const archive = page.getByRole('listitem', { name: 'Synced folder Archive' });
  await expect(archive.getByText(/42 files deleted on this computer would be moved to the Google Drive trash/)).toBeVisible();
  await archive.locator('#sync-restore-p3').click();
  let harness = await page.evaluate(() => window.syncHarness);
  expect(harness.args[harness.calls.indexOf('restore')]).toEqual(['p3']);
  await start(page);
  await page.locator('#sync-confirm-p3').click();
  harness = await page.evaluate(() => window.syncHarness);
  expect(harness.args[harness.calls.indexOf('confirm')]).toEqual(['p3']);
});

test('pause, resume, sync now, open and stop syncing call the sync service', async ({ page }) => {
  await start(page);
  await page.locator('#sync-pause-p1').click();
  await expect(page.locator('#sync-pause-p1')).toHaveText('Resume');
  await page.locator('#sync-pause-p1').click();
  await page.locator('#sync-now-p2').click();
  const projects = page.getByRole('listitem', { name: 'Synced folder Projects' });
  await projects.getByRole('button', { name: 'Open folder' }).click();
  await projects.getByRole('button', { name: 'Open in Drive' }).click();
  await page.locator('#sync-remove-p2').click();
  await expect(page.getByRole('listitem', { name: 'Synced folder Photos' })).toHaveCount(0);
  const harness = await page.evaluate(() => window.syncHarness);
  for (const call of ['pause', 'resume', 'now', 'open-local', 'open-drive', 'remove']) expect(harness.calls).toContain(call);
});

test('without a Google connection the page explains how to start', async ({ page }) => {
  await start(page, disconnected, []);
  await expect(page.getByText('Connect Google Drive to start syncing.')).toBeVisible();
  await expect(page.locator('#sync-add')).toBeDisabled();
  await expect(page.getByText('No folders are synced yet. Choose “Sync a folder” to start.')).toBeVisible();
});
