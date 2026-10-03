import { expect, test, type Page } from '@playwright/test';
import type { DriveConnectionStatus, DriveDestination, DriveTransferStatus, DriveUploadPlan, Preview } from '../src/types';

const account = 'google-drive:upload-fixture';
const connected: DriveConnectionStatus = { state: 'connected', clientConfigured: true, account: { reference: account, displayName: 'Fixture', email: 'upload@example.invalid' }, message: 'Saved connection.', scope: 'https://www.googleapis.com/auth/drive.file' };
const destination: DriveDestination = { id: 'fixture-parent', name: 'Existing projects', accountReference: account };
const rootDestination: DriveDestination = { ...destination, id: 'fixture-root', name: 'My Drive' };
const preview: Preview = { projectName: 'Local project', sourceRoot: '/synthetic/local-project', offline: true, entries: [], capabilities: [], plan: { planId: 'local-preview', planDigest: 'offline-only', rulesDigest: 'fixture-rules', createdAt: '2026-10-01T00:00:00Z', destinationIdentity: 'synthetic-empty', scanComplete: { source: true, destination: true }, operations: [], risks: [], summary: { operationCount: 0, uploadBytes: 0, trashCount: 0 } } };
const plan: DriveUploadPlan = { planDigest: 'approved-drive-plan', sourceName: 'Local project', destinationName: destination.name, destinationId: destination.id, accountReference: account, fileCount: 2, folderCount: 3, totalBytes: 4096, excludedCount: 1, expiresAt: '2099-01-01T00:00:00Z', warnings: ['Ignored files remain excluded.'], entries: [{ relativePath: 'Local project', kind: 'directory', size: 0 }, { relativePath: 'src', kind: 'directory', size: 0 }, { relativePath: 'empty', kind: 'directory', size: 0 }, { relativePath: 'src/a.txt', kind: 'file', size: 1024 }, { relativePath: 'b.txt', kind: 'file', size: 3072 }] };
const idle: DriveTransferStatus = { state: 'idle', totalFiles: 0, completedFiles: 0, totalBytes: 0, uploadedBytes: 0, message: '' };
const uploading: DriveTransferStatus = { state: 'uploading', planDigest: plan.planDigest, totalFiles: 2, completedFiles: 0, totalBytes: 4096, uploadedBytes: 0, currentPath: 'src/a.txt', message: 'Uploading approved files.' };
interface UploadHarness {
  calls: string[];
  starts: string[];
  setPlan(value: DriveUploadPlan): void;
  setStatus(value: DriveTransferStatus): void;
  setDestination(value: DriveDestination): void;
  deferSelection(): void;
  resolveSelection(value: DriveDestination | null): void;
  failStart(): void;
  failStatus(value: boolean): void;
  deferCancel(): void;
  resolveCancel(): void;
  deferStatusOnce(): void;
  resolveLateStatus(): void;
}
declare global { interface Window { uploadHarness: UploadHarness } }
async function start(page: Page, inApp = false) {
  await page.addInitScript(({ connected, destination, rootDestination, preview, plan, idle, uploading, inApp }) => {
    let chosen: DriveDestination | null = null;
    let selected = destination;
    let prepared = plan;
    let current = idle;
    let selectionPending = false;
    let resolveSelection: ((value: DriveDestination | null) => void) | undefined;
    let rejectSelection: ((value: Error) => void) | undefined;
    let startFailure = false;
    let statusFailure = false;
    let cancelPending = false;
    let resolveCancel: (() => void) | undefined;
    let statusPending = false;
    let resolveLateStatus: (() => void) | undefined;
    const calls: string[] = [];
    window.uploadHarness = {
      calls, starts: [],
      setPlan(value) { prepared = value; }, setStatus(value) { current = value; }, setDestination(value) { selected = value; },
      deferSelection() { selectionPending = true; }, resolveSelection(value) { if (value) chosen = value; resolveSelection?.(value); },
      failStart() { startFailure = true; }, failStatus(value) { statusFailure = value; },
      deferCancel() { cancelPending = true; }, resolveCancel() { current = { ...current, state: 'cancelled', message: 'Cancellation confirmed. Previously created files are preserved.' }; resolveCancel?.(); },
      deferStatusOnce() { statusPending = true; }, resolveLateStatus() { resolveLateStatus?.(); },
    };
    window.go = { desktop: { App: {
      OpenFolder: async () => { calls.push('source'); return preview; }, OpenConfiguration: async () => preview, Refresh: async () => { calls.push('refresh'); return preview; }, Cancel: async () => {},
      GoogleDriveStatus: async () => connected, ConnectGoogleDrive: async () => connected, CheckGoogleDrive: async () => connected, DisconnectGoogleDrive: async () => ({ ...connected, state: 'disconnected' }), RevokeGoogleDrive: async () => ({ ...connected, state: 'disconnected' }),
      CancelGoogleDrive: async () => { calls.push('cancel-selection'); rejectSelection?.(new Error('sensitive-picker-cancel')); },
      CurrentDriveDestination: async () => chosen,
      ChooseDriveDestination: async () => { calls.push('destination'); if (selectionPending) return new Promise((resolve, reject) => { resolveSelection = resolve; rejectSelection = reject; }); chosen = selected; return chosen; },
      UseMyDrive: async () => { calls.push('my-drive'); chosen = rootDestination; prepared = { ...prepared, destinationId: chosen.id, destinationName: chosen.name }; return chosen; },
      PreviewDriveUpload: async () => { calls.push('preview'); return prepared; },
      StartDriveUpload: async digest => { calls.push('start'); window.uploadHarness.starts.push(digest); if (startFailure) throw new Error('sensitive-start-failure'); current = uploading; return current; },
      DriveTransferStatus: async () => { calls.push('status'); if (statusFailure) throw new Error('sensitive-status-failure'); if (statusPending) { statusPending = false; const snapshot = current; return new Promise(resolve => { resolveLateStatus = () => resolve(snapshot); }); } return current; },
      CancelDriveUpload: async () => { calls.push('cancel'); if (cancelPending) return new Promise(resolve => { resolveCancel = resolve; }); current = { ...current, state: 'cancelled', message: 'Cancellation confirmed. Previously created files are preserved.' }; },
      OpenUploadedDriveFolder: async () => { calls.push('open-verified-folder'); },
      ...(inApp ? {
        DriveFolders: async (parent: string) => { calls.push(`folders:${parent}`); return parent === 'root' ? [{ id: destination.id, name: destination.name, parents: ['root'], canAddChildren: true }] : []; },
        UseDriveFolder: async (id: string) => { calls.push(`use-folder:${id}`); chosen = { ...destination, id }; return chosen; },
      } : {}),
    } } };
  }, { connected, destination, rootDestination, preview, plan, idle, uploading, inApp });
  await page.goto('/');
  await page.getByRole('button', { name: 'Choose a local folder', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Upload this folder to Google Drive', exact: true })).toBeVisible();
}
async function approvePreview(page: Page) {
  await page.getByRole('button', { name: 'Choose existing Drive folder', exact: true }).click();
  await page.getByRole('button', { name: 'Preview folder upload', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Review and upload', exact: true })).toBeVisible();
}

test('select destination, preview full folder, approve exact digest, and show verified completion', async ({ page }) => {
  await start(page);
  await expect(page.getByRole('button', { name: 'Preview folder upload', exact: true })).toBeDisabled();
  expect(await page.evaluate(() => window.uploadHarness.starts)).toEqual([]);
  await approvePreview(page);
  const approval = page.getByRole('region', { name: 'Review folder upload' });
  await expect(approval).toContainText('empty');
  await expect(approval).toContainText('Folder structure and included empty folders are preserved');
  await expect(approval).toContainText('excluded items');
  await expect(approval).toContainText('upload@example.invalid');
  await expect(approval).toContainText('fixture-parent');
  expect(await page.evaluate(() => window.uploadHarness.starts)).toEqual([]);
  await page.getByRole('button', { name: 'Upload folder', exact: true }).click();
  expect(await page.evaluate(() => window.uploadHarness.starts)).toEqual([plan.planDigest]);
  const transfer = page.getByRole('region', { name: 'Folder transfer' });
  await expect(transfer).toContainText('0 of 2 files verified · 0 B of 4.0 KiB verified');
  await expect(page.getByRole('button', { name: 'Choose folder', exact: true })).toBeDisabled();
  await page.evaluate(status => window.uploadHarness.setStatus(status), { ...uploading, state: 'verifying', completedFiles: 1, uploadedBytes: 4096, message: 'Checking hashes.' });
  await expect(transfer.getByRole('heading', { name: 'Verifying uploaded files', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Folder upload verified', exact: true })).toHaveCount(0);
  await page.evaluate(status => window.uploadHarness.setStatus(status), { ...uploading, state: 'succeeded', completedFiles: 2, uploadedBytes: 4096, remoteFolderId: 'new-root_123', message: 'All approved files were verified.' });
  await expect(transfer.getByRole('heading', { name: 'Folder upload verified', exact: true })).toBeVisible();
  await transfer.getByRole('button', { name: 'Open destination folder on Google Drive', exact: true }).click();
  expect(await page.evaluate(() => window.uploadHarness.calls)).toContain('open-verified-folder');
  await expect(page.getByRole('button', { name: 'Upload folder', exact: true })).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText('Empty · offline');
});

test('My Drive uses the validated root and never uploads on destination choice', async ({ page }) => {
  await start(page);
  await page.getByRole('button', { name: 'Use My Drive', exact: true }).click();
  await expect(page.locator('.destination-name')).toHaveText('Destination: My Drive');
  expect(await page.evaluate(() => window.uploadHarness.calls)).toEqual(['source', 'my-drive']);
  await page.getByRole('button', { name: 'Preview folder upload', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Review folder upload' })).toContainText('fixture-root');
  expect(await page.evaluate(() => window.uploadHarness.starts)).toEqual([]);
});

test('with full Drive access an existing folder is chosen inside the app, without the browser', async ({ page }) => {
  await start(page, true);
  await expect(page.locator('.drive-destination')).toContainText('Choose My Drive or an existing Drive folder.');
  await page.getByRole('button', { name: 'Choose existing Drive folder', exact: true }).click();
  const browser = page.getByRole('region', { name: 'Choose a Google Drive folder' });
  await browser.getByRole('button', { name: destination.name, exact: true }).click();
  await browser.getByRole('button', { name: `Use “${destination.name}”`, exact: true }).click();
  await expect(page.locator('.destination-name')).toHaveText(`Destination: ${destination.name}`);
  await expect(browser).toHaveCount(0);
  expect(await page.evaluate(() => window.uploadHarness.calls)).toEqual(['source', 'folders:root', `folders:${destination.id}`, `use-folder:${destination.id}`]);
  await page.getByRole('button', { name: 'Preview folder upload', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Review folder upload' })).toContainText(destination.id);
  expect(await page.evaluate(() => window.uploadHarness.starts)).toEqual([]);
});

test('refresh invalidates approval and a new destination requires a new preview', async ({ page }) => {
  await start(page); await approvePreview(page);
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Upload folder', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Preview folder upload', exact: true }).click();
  await page.getByRole('button', { name: 'Use My Drive', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Upload folder', exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => window.uploadHarness.starts)).toEqual([]);
});

test('expired and account-mismatched plans cannot reach approval', async ({ page }) => {
  await start(page);
  await page.getByRole('button', { name: 'Choose existing Drive folder', exact: true }).click();
  for (const invalid of [{ ...plan, expiresAt: '2000-01-01T00:00:00Z' }, { ...plan, accountReference: 'google-drive:other' }, { ...plan, destinationId: 'other-parent' }]) {
    await page.evaluate(value => window.uploadHarness.setPlan(value), invalid);
    await page.getByRole('button', { name: 'Preview folder upload', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('Nothing has been approved.');
    await expect(page.getByRole('button', { name: 'Upload folder', exact: true })).toHaveCount(0);
  }
  expect(await page.evaluate(() => window.uploadHarness.starts)).toEqual([]);
});

test('backend rejection cannot reuse an old successful transfer or expose its error', async ({ page }) => {
  await start(page); await approvePreview(page);
  await page.evaluate(old => { window.uploadHarness.setStatus(old); window.uploadHarness.failStart(); }, { ...uploading, planDigest: 'older-plan', state: 'succeeded', message: 'An older upload succeeded.' });
  await page.getByRole('button', { name: 'Upload folder', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('The approved upload could not be confirmed.');
  await expect(page.getByRole('heading', { name: 'Folder upload verified', exact: true })).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText('sensitive-start-failure');
  await expect(page.getByRole('button', { name: 'Upload folder', exact: true })).toHaveCount(0);
});

test('unreadable start result blocks a second upload until status is reconciled', async ({ page }) => {
  await start(page); await approvePreview(page);
  await page.evaluate(() => { window.uploadHarness.failStart(); window.uploadHarness.failStatus(true); });
  await page.getByRole('button', { name: 'Upload folder', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Folder transfer' })).toContainText('Upload status is unavailable.');
  await expect(page.getByRole('button', { name: 'Preview folder upload', exact: true })).toBeDisabled();
  await expect(page.locator('body')).not.toContainText('sensitive-status-failure');
  await page.evaluate(() => window.uploadHarness.failStatus(false));
  await expect(page.getByRole('button', { name: 'Preview folder upload', exact: true })).toBeEnabled();
  await expect(page.getByRole('heading', { name: 'Folder upload verified', exact: true })).toHaveCount(0);
});

test('cancellation is not declared until backend confirmation and preserves created files', async ({ page }) => {
  await start(page); await approvePreview(page);
  await page.getByRole('button', { name: 'Upload folder', exact: true }).click();
  await page.evaluate(() => window.uploadHarness.deferCancel());
  await page.getByRole('button', { name: 'Cancel upload', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Cancelling upload…', exact: true })).toBeDisabled();
  await expect(page.getByRole('heading', { name: 'Upload cancelled', exact: true })).toHaveCount(0);
  await page.evaluate(() => window.uploadHarness.resolveCancel());
  await expect(page.getByRole('heading', { name: 'Upload cancelled', exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Folder transfer' })).toContainText('Files already created on Drive are preserved.');
  await expect(page.getByRole('heading', { name: 'Folder upload verified', exact: true })).toHaveCount(0);
});

test('active transfer prevents disconnect or revocation until it is stopped', async ({ page }) => {
  await start(page); await approvePreview(page);
  await page.getByRole('button', { name: 'Upload folder', exact: true }).click();
  await page.getByRole('button', { name: 'Connections', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Disconnect from this device', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Revoke access on Google', exact: true })).toBeDisabled();
  await expect(page.getByText('Finish or cancel the folder upload in Files or Sync pairs before changing account access.', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Sync pairs', exact: true }).click();
  await page.getByRole('button', { name: 'Cancel upload', exact: true }).click();
  await page.getByRole('button', { name: 'Connections', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Disconnect from this device', exact: true })).toBeEnabled();
});

test('a delayed progress response cannot resurrect an already cancelled upload', async ({ page }) => {
  await start(page); await approvePreview(page);
  await page.evaluate(() => window.uploadHarness.deferStatusOnce());
  await page.getByRole('button', { name: 'Upload folder', exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.uploadHarness.calls.filter(call => call === 'status').length)).toBe(1);
  await page.getByRole('button', { name: 'Cancel upload', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Upload cancelled', exact: true })).toBeVisible();
  await page.evaluate(() => window.uploadHarness.resolveLateStatus());
  await expect(page.getByRole('button', { name: 'Preview folder upload', exact: true })).toBeEnabled();
  await expect(page.getByRole('heading', { name: 'Upload cancelled', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Cancel upload', exact: true })).toHaveCount(0);
});

test('a progress network failure never invents completion and keeps cancellation available', async ({ page }) => {
  await start(page); await approvePreview(page);
  await page.getByRole('button', { name: 'Upload folder', exact: true }).click();
  await page.evaluate(() => window.uploadHarness.failStatus(true));
  await expect(page.getByRole('alert')).toContainText('Completion is unconfirmed');
  await expect(page.getByRole('button', { name: 'Cancel upload', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Preview folder upload', exact: true })).toBeDisabled();
  await expect(page.getByRole('heading', { name: 'Folder upload verified', exact: true })).toHaveCount(0);
  await page.evaluate(() => window.uploadHarness.failStatus(false));
  await page.getByRole('button', { name: 'Cancel upload', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Upload cancelled', exact: true })).toBeVisible();
});

test('browser picker cancellation preserves destination while discarding old approval', async ({ page }) => {
  await start(page); await approvePreview(page);
  await page.evaluate(() => window.uploadHarness.deferSelection());
  await page.getByRole('button', { name: 'Choose existing Drive folder', exact: true }).click();
  await page.getByRole('button', { name: 'Cancel folder selection', exact: true }).click();
  await expect(page.locator('.destination-name')).toHaveText('Destination: Existing projects');
  await expect(page.getByRole('button', { name: 'Preview folder upload', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Upload folder', exact: true })).toHaveCount(0);
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText('sensitive-picker-cancel');
});

test('remote names and messages remain literal text; provider IDs cannot become arbitrary links', async ({ page }) => {
  await start(page);
  const hostileName = '<img src=x onerror="window.injected=true"> &copy;';
  await page.evaluate(value => window.uploadHarness.setDestination(value), { ...destination, name: hostileName });
  await page.evaluate(value => window.uploadHarness.setPlan(value), { ...plan, destinationName: hostileName, entries: [{ relativePath: hostileName, kind: 'file', size: 4 }], warnings: [hostileName] });
  await approvePreview(page);
  await expect(page.locator('.destination-name')).toContainText(hostileName);
  await expect(page.locator('.upload-entries')).toContainText(hostileName);
  await expect(page.locator('.content img, .content script')).toHaveCount(0);
  await page.getByRole('button', { name: 'Upload folder', exact: true }).click();
  await page.evaluate(status => window.uploadHarness.setStatus(status), { ...uploading, state: 'failed', remoteFolderId: 'https://evil.invalid/?token=fixture', message: hostileName });
  await expect(page.getByRole('heading', { name: 'Upload failed', exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Folder transfer' })).toContainText(hostileName);
  await expect(page.getByRole('region', { name: 'Folder transfer' }).getByRole('link')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Open destination folder on Google Drive', exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => [window.injected, localStorage.length, sessionStorage.length])).toEqual([undefined, 0, 0]);
});

test('repeat preview distinguishes verified copies, resumed intent and keep-both before approval', async ({ page }) => {
  await start(page);
  await page.evaluate(value => window.uploadHarness.setPlan(value), { ...plan, entries: [{ relativePath: '', kind: 'directory', size: 0, action: 'skip' }, { relativePath: 'unchanged.txt', kind: 'file', size: 1, action: 'skip' }, { relativePath: 'changed.txt', kind: 'file', size: 1, action: 'keep-both' }, { relativePath: 'interrupted.txt', kind: 'file', size: 1, action: 'resume' }] });
  await approvePreview(page);
  const approval = page.getByRole('region', { name: 'Review folder upload' });
  await expect(approval).toContainText('Verified copies are reused; changed files keep both versions.');
  await expect(approval.getByText('Keep both versions', { exact: true })).toBeVisible();
  await expect(approval.getByText('Resume reserved copy', { exact: true })).toBeVisible();
  await expect(approval.getByText('Verify existing copy', { exact: true })).toHaveCount(2);
  await expect(approval).toContainText('.ledgesync- suffix with a stable identifier');
  expect(await page.evaluate(() => window.uploadHarness.starts)).toEqual([]);
});

test('a Drive authorization loss is a review state, never a successful upload', async ({ page }) => {
  await start(page); await approvePreview(page);
  await page.getByRole('button', { name: 'Upload folder', exact: true }).click();
  await page.evaluate(status => {
    window.uploadHarness.setStatus(status);
    window.go!.desktop!.App!.GoogleDriveStatus = async () => ({ state: 'reconnect_required', clientConfigured: true, account: { reference: 'google-drive:upload-fixture', displayName: 'Fixture', email: 'upload@example.invalid' }, message: 'Reconnect required.', scope: 'https://www.googleapis.com/auth/drive.file' });
  }, { ...uploading, state: 'needs_review', message: 'Drive authorization was lost. Reconnect before reviewing another plan.' });
  await expect(page.getByRole('heading', { name: 'Upload needs review', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Open Connections', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Upload folder', exact: true })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Folder upload verified', exact: true })).toHaveCount(0);
});

test('approval and cancellation remain usable at the minimum desktop window size', async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 620 });
  await start(page); await approvePreview(page);
  const upload = page.getByRole('button', { name: 'Upload folder', exact: true });
  await upload.scrollIntoViewIfNeeded();
  await page.screenshot({ path: test.info().outputPath('folder-approval-900.png') });
  await upload.focus(); await page.keyboard.press('Enter');
  const cancel = page.getByRole('button', { name: 'Cancel upload', exact: true });
  await cancel.scrollIntoViewIfNeeded();
  await page.screenshot({ path: test.info().outputPath('folder-progress-900.png') });
  await expect(cancel).toBeVisible(); await cancel.focus(); await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { name: 'Upload cancelled', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
