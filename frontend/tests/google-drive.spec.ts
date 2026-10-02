import { expect, test, type Page } from '@playwright/test';
import type { DriveConnectionStatus, Preview } from '../src/types';

const scope = 'https://www.googleapis.com/auth/drive.file';
const setup: DriveConnectionStatus = { state: 'setup_required', clientConfigured: false, message: 'Google sign-in is not configured in this build.', scope };
const disconnected: DriveConnectionStatus = { state: 'disconnected', clientConfigured: true, message: 'Ready to authorize Google Drive.', scope };
const connected: DriveConnectionStatus = { state: 'connected', clientConfigured: true, message: 'Google Drive authorization is saved in the system credential vault.', scope, account: { reference: 'google-drive:fixture-account', displayName: 'Fixture Account', email: 'fixture@example.invalid' } };

interface Harness {
  calls: string[];
  complete(result: DriveConnectionStatus): void;
  fail(): void;
  setStatus(result: DriveConnectionStatus): void;
  setCheck(result: DriveConnectionStatus): void;
}
declare global { interface Window { driveHarness: Harness; injected?: boolean } }

async function start(page: Page, initial: DriveConnectionStatus) {
  await page.addInitScript(({ initial, disconnected }) => {
    // Replace the native transport only. The actual application renders and owns the flow.
    let current = initial;
    let checked: DriveConnectionStatus | null = null;
    let resolveConnect: ((value: DriveConnectionStatus) => void) | undefined;
    let rejectConnect: ((reason: Error) => void) | undefined;
    const calls: string[] = [];
    window.driveHarness = {
      calls,
      complete(result) { current = result; resolveConnect?.(result); },
      fail() { rejectConnect?.(new Error('sensitive-transport-value <script>window.injected=true</script>')); },
      setStatus(result) { current = result; },
      setCheck(result) { checked = result; },
    };
    window.go = { desktop: { App: {
      OpenFolder: async () => null, OpenConfiguration: async () => null, Refresh: async () => null, Cancel: async () => {},
      GoogleDriveStatus: async () => { calls.push('status'); return current; },
      ConnectGoogleDrive: async () => { calls.push('connect'); return new Promise<DriveConnectionStatus>((resolve, reject) => { resolveConnect = resolve; rejectConnect = reject; }); },
      CheckGoogleDrive: async () => { calls.push('check'); if (checked) current = checked; return current; },
      DisconnectGoogleDrive: async () => { calls.push('disconnect'); current = disconnected; return current; },
      CancelGoogleDrive: async () => { calls.push('cancel'); current = { ...disconnected, message: 'Authorization cancelled.' }; rejectConnect?.(new Error('Google Drive authorization was canceled.')); },
    } } };
  }, { initial, disconnected });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connections', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Connections', exact: true })).toBeVisible();
}

test('a fresh official build is ready for one-click authorization without client setup', async ({ page }) => {
  await start(page, disconnected);
  const card = page.getByRole('article', { name: 'Google Drive connection' });
  await expect(card.getByRole('button')).toHaveCount(1);
  await expect(card.getByRole('button', { name: 'Connect Google Drive', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: /OAuth|JSON|Cloud setup/ })).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText('Google Cloud');
  await expect(page.locator('input:not([type="search"]), textarea')).toHaveCount(0);
  await expect(page.getByRole('searchbox')).toBeDisabled();
  await page.getByRole('button', { name: 'Files', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'See what belongs.' })).toBeVisible();
  await page.getByRole('button', { name: 'Connections', exact: true }).click();
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status']);
  await card.getByRole('button', { name: 'Connect Google Drive', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Complete authorization in your browser');
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'connect']);
  await page.evaluate(result => window.driveHarness.complete(result), connected);
  await expect(card.getByText('Connected', { exact: true })).toBeVisible();
});

test('an unconfigured build directs users to the official app without exposing developer setup', async ({ page }) => {
  await start(page, setup);
  await expect(page.getByText('Connection unavailable', { exact: true })).toBeVisible();
  await expect(page.getByText('Install the official LedgeSync application from ledgesync.com', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: /Connect Google Drive|OAuth|JSON|Cloud setup/ })).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText('Google Cloud');
  await expect(page.locator('input:not([type="search"]), textarea')).toHaveCount(0);
  await expect(page.getByText('Cloud browsing and file transfers are not implemented yet.', { exact: false })).toBeVisible();
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status']);
});

test('legacy authorization retains identity until explicit disconnect then offers the new one-click flow', async ({ page }) => {
  const legacy: DriveConnectionStatus = { ...connected, state: 'client_changed', message: 'The saved authorization belongs to a different app configuration.' };
  await start(page, legacy);
  const card = page.getByRole('article', { name: 'Google Drive connection' });
  await expect(card.getByText('New authorization required', { exact: true })).toBeVisible();
  await expect(card.getByText('fixture@example.invalid', { exact: true })).toBeVisible();
  await expect(card.getByText('Disconnect it first, then connect again to authorize this version.', { exact: false })).toBeVisible();
  await expect(card.getByRole('button')).toHaveCount(1);
  await expect(card.getByRole('button', { name: 'Disconnect account', exact: true })).toBeEnabled();
  await expect(page.getByText('Google Drive connected. Cloud transfers are not available yet.', { exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status']);
  await card.getByRole('button', { name: 'Disconnect account', exact: true }).click();
  await expect(card.getByText('fixture@example.invalid', { exact: true })).toHaveCount(0);
  await card.getByRole('button', { name: 'Connect Google Drive', exact: true }).click();
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'disconnect', 'connect']);
  await page.evaluate(result => window.driveHarness.complete(result), connected);
  await expect(card.getByText('Connected', { exact: true })).toBeVisible();
});

test('failed legacy disconnection preserves identity and never enables authorization with the new client', async ({ page }) => {
  await start(page, { ...connected, state: 'client_changed', message: 'Disconnect the previous app authorization.' });
  await page.evaluate(() => { window.go!.desktop!.App!.DisconnectGoogleDrive = async () => { throw new Error('sensitive-vault-response'); }; });
  await page.getByRole('button', { name: 'Disconnect account', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Could not remove the local Google Drive credentials.');
  await expect(page.getByText('fixture@example.invalid', { exact: true })).toBeVisible();
  await expect(page.getByText('New authorization required', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: /Connect Google Drive|Reconnect Google Drive|Check connection/ })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Disconnect account', exact: true })).toBeEnabled();
  await expect(page.locator('body')).not.toContainText('sensitive-vault-response');
});

test('browser authorization shows pending cancellation then connected identity as literal text', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await start(page, disconnected);
  await page.getByRole('button', { name: 'Connect Google Drive', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Complete authorization in your browser');
  await expect(page.getByRole('button', { name: 'Cancel authorization', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Replace OAuth client JSON', exact: true })).toHaveCount(0);
  const hostile = { ...connected, account: { ...connected.account!, displayName: '<img src=x onerror="window.injected=true"> &copy;' } };
  await page.evaluate(result => window.driveHarness.complete(result), hostile);
  const card = page.getByRole('article', { name: 'Google Drive connection' });
  await expect(card.getByText('Connected', { exact: true })).toBeVisible();
  await expect(card.getByText(hostile.account.displayName, { exact: true })).toBeVisible();
  await expect(card.getByText('fixture@example.invalid', { exact: true })).toBeVisible();
  await expect(card.getByText('google-drive:fixture-account', { exact: true })).toBeVisible();
  await expect(card.locator('img, script')).toHaveCount(0);
  expect(await page.evaluate(() => window.injected)).toBeUndefined();
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0]);
  await expect(page.getByRole('button', { name: 'Check connection', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Disconnect account', exact: true })).toBeEnabled();
  await expect(page.getByText('Google Drive connected. Cloud transfers are not available yet.', { exact: true })).toBeVisible();
  expect(errors).toEqual([]);
});

test('cancel and failed authorization allow a fresh attempt without showing transport details', async ({ page }) => {
  await start(page, disconnected);
  await page.getByRole('button', { name: 'Connect Google Drive', exact: true }).click();
  await page.getByRole('button', { name: 'Cancel authorization', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Authorization cancelled.');
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Connect Google Drive', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: 'Connect Google Drive', exact: true }).click();
  await page.evaluate(() => window.driveHarness.fail());
  await expect(page.getByRole('alert')).toContainText('Google Drive authorization could not be completed.');
  await expect(page.locator('body')).not.toContainText('sensitive-transport-value');
  await expect(page.getByRole('button', { name: 'Connect Google Drive', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: 'Connect Google Drive', exact: true }).click();
  await page.evaluate(result => window.driveHarness.complete(result), connected);
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(page.getByText('Connected', { exact: true })).toBeVisible();
});

test('cancellation does not hide a failed persisted-status read', async ({ page }) => {
  await start(page, disconnected);
  await page.getByRole('button', { name: 'Connect Google Drive', exact: true }).click();
  await page.evaluate(() => { window.go!.desktop!.App!.GoogleDriveStatus = async () => { throw new Error('sensitive-status-failure'); }; });
  await page.getByRole('button', { name: 'Cancel authorization', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('The saved connection status could not be read.');
  await expect(page.getByRole('button', { name: 'Retry connection status', exact: true })).toBeEnabled();
  await expect(page.getByText('Not checked', { exact: true })).toBeVisible();
  await expect(page.locator('body')).not.toContainText('sensitive-status-failure');
});

test('a connected account does not turn the local simulated plan into a Drive plan', async ({ page }) => {
  await start(page, connected);
  // A minimal transport DTO is sufficient here: this regression checks the
  // connection-dependent banner, while explorer.spec exercises real Go plans.
  const preview: Preview = {
    projectName: 'Preview fixture', sourceRoot: '/synthetic-fixture', offline: true, entries: [], capabilities: [],
    plan: { planId: 'synthetic', planDigest: 'fixture-plan', rulesDigest: 'fixture-rules', createdAt: '2026-10-02T00:00:00Z', destinationIdentity: 'synthetic-empty', scanComplete: { source: true, destination: true }, operations: [], risks: [], summary: { operationCount: 0, uploadBytes: 0, trashCount: 0 } },
  };
  await page.evaluate(data => { window.go!.desktop!.App!.OpenFolder = async () => data; }, preview);
  await page.getByRole('button', { name: 'Choose folder', exact: true }).click();
  await page.getByRole('button', { name: 'Sync pairs', exact: true }).click();
  await expect(page.getByText('This preview uses a simulated destination and cannot be applied', { exact: false })).toBeVisible();
  await expect(page.locator('body')).not.toContainText('Google Drive is not connected');
  await expect(page.getByText('Google Drive connected. Cloud transfers are not available yet.', { exact: true })).toBeVisible();
  await expect(page.getByText('Developer alpha · 0.1.0-alpha.3', { exact: true })).toBeVisible();
});

test('account check reports reconnection and disconnect removes displayed account without cloud deletion claims', async ({ page }) => {
  await start(page, connected);
  const expired: DriveConnectionStatus = { ...connected, state: 'reconnect_required', message: 'Google authorization has expired or was revoked. Reconnect this account.' };
  await page.evaluate(result => window.driveHarness.setCheck(result), expired);
  await page.getByRole('button', { name: 'Check connection', exact: true }).click();
  await expect(page.getByText('Reconnect required', { exact: true })).toBeVisible();
  await expect(page.getByText('fixture@example.invalid', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reconnect Google Drive', exact: true })).toBeEnabled();
  await expect(page.getByText('It does not delete Drive files or revoke the Google permission grant.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Disconnect account', exact: true }).click();
  await expect(page.getByText('Not connected', { exact: true })).toBeVisible();
  await expect(page.getByText('fixture@example.invalid', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Connect Google Drive', exact: true })).toBeEnabled();
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'check', 'disconnect']);
});

test('a rejected check reconciles the persisted reconnect state discarded by Wails', async ({ page }) => {
  await start(page, connected);
  const expired: DriveConnectionStatus = { ...connected, state: 'reconnect_required', message: 'Reconnect the saved Google account.' };
  await page.evaluate(result => {
    window.go!.desktop!.App!.CheckGoogleDrive = async () => {
      window.driveHarness.calls.push('check');
      window.driveHarness.setStatus(result);
      // A Go (status, error) result rejects in Wails; no DTO reaches this promise.
      throw 'sensitive-provider-rejection';
    };
  }, expired);
  await page.getByRole('button', { name: 'Check connection', exact: true }).click();
  await expect(page.getByText('Reconnect required', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reconnect Google Drive', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Check connection', exact: true })).toHaveCount(0);
  await expect(page.getByRole('alert')).toContainText('Could not check the Google Drive connection.');
  await expect(page.locator('body')).not.toContainText('sensitive-provider-rejection');
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'check', 'status']);
});

for (const operation of ['connect', 'disconnect'] as const) {
  test(`a rejected ${operation} reconciles the vault-unavailable state`, async ({ page }) => {
    const initial = operation === 'disconnect' ? connected : disconnected;
    await start(page, initial);
    const unavailable: DriveConnectionStatus = { state: 'storage_unavailable', clientConfigured: false, scope, message: 'Unlock the operating system credential vault and try again.' };
    await page.evaluate(({ operation, unavailable }) => {
      const methods = { connect: 'ConnectGoogleDrive', disconnect: 'DisconnectGoogleDrive' } as const;
      window.go!.desktop!.App![methods[operation]] = async () => {
        window.driveHarness.calls.push(operation);
        window.driveHarness.setStatus(unavailable);
        throw 'sensitive-vault-error';
      };
    }, { operation, unavailable });
    const labels = { connect: 'Connect Google Drive', disconnect: 'Disconnect account' };
    await page.getByRole('button', { name: labels[operation], exact: true }).click();
    await expect(page.getByText('Credential vault unavailable', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Retry connection status', exact: true })).toBeEnabled();
    await expect(page.getByText('Connected', { exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Connect Google Drive', exact: true })).toHaveCount(0);
    await expect(page.locator('body')).not.toContainText('sensitive-vault-error');
    expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', operation, 'status']);
  });
}

test('failed status reconciliation removes stale connection claims and requires an explicit retry', async ({ page }) => {
  await start(page, connected);
  await page.evaluate(() => {
    window.go!.desktop!.App!.CheckGoogleDrive = async () => { throw 'sensitive-check-failure'; };
    window.go!.desktop!.App!.GoogleDriveStatus = async () => { throw 'sensitive-status-failure'; };
  });
  await page.getByRole('button', { name: 'Check connection', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('The saved connection status could not be read.');
  await expect(page.getByText('Not checked', { exact: true })).toBeVisible();
  await expect(page.getByText('Connected', { exact: true })).toHaveCount(0);
  await expect(page.getByText('fixture@example.invalid', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Retry connection status', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Disconnect account', exact: true })).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText('sensitive-status-failure');
  await page.evaluate(result => { window.go!.desktop!.App!.GoogleDriveStatus = async () => result; }, connected);
  await page.getByRole('button', { name: 'Retry connection status', exact: true }).click();
  await expect(page.getByText('Connected', { exact: true })).toBeVisible();
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('unavailable vault requires explicit retry and never offers plaintext credential entry', async ({ page }) => {
  await start(page, { ...setup, state: 'storage_unavailable', message: 'Unlock your system credential vault and retry.' });
  await expect(page.getByText('Credential vault unavailable', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Import OAuth client JSON', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Connect Google Drive', exact: true })).toHaveCount(0);
  await expect(page.locator('textarea, input:not([type="search"])')).toHaveCount(0);
  await page.evaluate(result => window.driveHarness.setStatus(result), disconnected);
  await page.getByRole('button', { name: 'Retry connection status', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Connect Google Drive', exact: true })).toBeEnabled();
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'status']);
});

test('failed account checks and disconnects preserve account identity and allow retry', async ({ page }) => {
  await start(page, connected);
  await page.evaluate(() => { window.go!.desktop!.App!.CheckGoogleDrive = async () => { throw new Error('sensitive-check-response'); }; });
  await page.getByRole('button', { name: 'Check connection', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Could not check the Google Drive connection.');
  await expect(page.getByRole('button', { name: 'Check connection', exact: true })).toBeEnabled();
  await page.evaluate(() => { window.go!.desktop!.App!.DisconnectGoogleDrive = async () => { throw new Error('sensitive-vault-response'); }; });
  await page.getByRole('button', { name: 'Disconnect account', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Could not remove the local Google Drive credentials.');
  await expect(page.getByRole('button', { name: 'Disconnect account', exact: true })).toBeEnabled();
  await expect(page.getByText('fixture@example.invalid', { exact: true })).toBeVisible();
  await expect(page.locator('body')).not.toContainText('sensitive-check-response');
  await expect(page.locator('body')).not.toContainText('sensitive-vault-response');
});

test('reconnection retains known account identity while awaiting authorization', async ({ page }) => {
  await start(page, { ...connected, state: 'reconnect_required', message: 'Reconnect the saved Google account.' });
  await page.getByRole('button', { name: 'Reconnect Google Drive', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Cancel authorization', exact: true })).toBeEnabled();
  await expect(page.getByText('fixture@example.invalid', { exact: true })).toBeVisible();
  await page.evaluate(result => window.driveHarness.complete(result), connected);
  await expect(page.getByText('Connected', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Check connection', exact: true })).toBeEnabled();
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'connect']);
});

test('failed cancellation remains cancellable and never reports a disconnected account', async ({ page }) => {
  await start(page, disconnected);
  await page.evaluate(() => { window.go!.desktop!.App!.CancelGoogleDrive = async () => { throw new Error('sensitive-cancel-response'); }; });
  await page.getByRole('button', { name: 'Connect Google Drive', exact: true }).click();
  await page.getByRole('button', { name: 'Cancel authorization', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Could not cancel authorization.');
  await expect(page.getByRole('button', { name: 'Cancel authorization', exact: true })).toBeEnabled();
  await expect(page.getByText('Waiting for authorization', { exact: true })).toBeVisible();
  await expect(page.locator('body')).not.toContainText('sensitive-cancel-response');
  await page.evaluate(result => window.driveHarness.complete(result), connected);
  await expect(page.getByText('Connected', { exact: true })).toBeVisible();
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('navigation during browser authorization preserves the request without polling', async ({ page }) => {
  await start(page, disconnected);
  await page.getByRole('button', { name: 'Connect Google Drive', exact: true }).click();
  await page.getByRole('button', { name: 'Files', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'See what belongs.' })).toBeVisible();
  await page.getByRole('button', { name: 'Connections', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Cancel authorization', exact: true })).toBeEnabled();
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'connect']);
  await page.getByRole('button', { name: 'Cancel authorization', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Connect Google Drive', exact: true })).toBeEnabled();
});
