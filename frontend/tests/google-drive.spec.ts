import { expect, test, type Page } from '@playwright/test';
import type { DriveConnectionStatus, Preview } from '../src/types';

const scope = 'https://www.googleapis.com/auth/drive.file';
const setup: DriveConnectionStatus = { state: 'setup_required', clientConfigured: false, message: 'Google sign-in is not configured in this build.', scope };
const disconnected: DriveConnectionStatus = { state: 'disconnected', clientConfigured: true, message: 'Ready to authorize Google Drive.', scope };
const connected: DriveConnectionStatus = { state: 'connected', clientConfigured: true, message: 'Google Drive authorization is saved in the system credential vault.', scope, account: { reference: 'google-drive:fixture-account', displayName: 'Fixture Account', email: 'fixture@example.invalid' } };

interface Harness {
  calls: string[];
  revocations: { accountReference: string; confirmed: boolean }[];
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
      revocations: [],
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
      RevokeGoogleDrive: async (accountReference: string, confirmed: boolean) => { calls.push('revoke'); window.driveHarness.revocations.push({ accountReference, confirmed }); if (!confirmed || accountReference !== current.account?.reference) throw new Error('Confirmation does not match the saved account.'); current = { ...disconnected, message: 'Google access revoked and local credentials removed.' }; return current; },
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
  await expect(page.getByText('In Files, choose a local folder and a Drive destination', { exact: false })).toBeVisible();
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
  await expect(card.getByRole('button', { name: 'Disconnect from this device', exact: true })).toBeEnabled();
  await expect(card.getByRole('button', { name: 'Revoke access on Google', exact: true })).toHaveCount(0);
  await expect(card.getByText('This version cannot revoke a grant from a different OAuth client.', { exact: false })).toBeVisible();
  await expect(page.getByText('Google Drive connected. Choose a destination and preview your folder upload.', { exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status']);
  await card.getByRole('button', { name: 'Disconnect from this device', exact: true }).click();
  await expect(card.getByText('fixture@example.invalid', { exact: true })).toHaveCount(0);
  await card.getByRole('button', { name: 'Connect Google Drive', exact: true }).click();
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'disconnect', 'connect']);
  await page.evaluate(result => window.driveHarness.complete(result), connected);
  await expect(card.getByText('Connected', { exact: true })).toBeVisible();
});

test('failed legacy disconnection preserves identity and never enables authorization with the new client', async ({ page }) => {
  await start(page, { ...connected, state: 'client_changed', message: 'Disconnect the previous app authorization.' });
  await page.evaluate(() => { window.go!.desktop!.App!.DisconnectGoogleDrive = async () => { throw new Error('sensitive-vault-response'); }; });
  await page.getByRole('button', { name: 'Disconnect from this device', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Could not remove the local Google Drive credentials.');
  await expect(page.getByText('fixture@example.invalid', { exact: true })).toBeVisible();
  await expect(page.getByText('New authorization required', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: /Connect Google Drive|Reconnect Google Drive|Check connection/ })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Disconnect from this device', exact: true })).toBeEnabled();
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
  await expect(page.getByRole('button', { name: 'Disconnect from this device', exact: true })).toBeEnabled();
  await expect(page.getByText('Google Drive connected. Choose a destination and preview your folder upload.', { exact: true })).toBeVisible();
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
  await expect(page.getByText('Google Drive connected. Choose a destination and preview your folder upload.', { exact: true })).toBeVisible();
  await expect(page.getByText('Developer alpha · 0.1.0-alpha.4', { exact: true })).toBeVisible();
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
  await page.getByRole('button', { name: 'Disconnect from this device', exact: true }).click();
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

for (const operation of ['connect', 'disconnect', 'revoke'] as const) {
  test(`a rejected ${operation} reconciles the vault-unavailable state`, async ({ page }) => {
    const initial = operation === 'connect' ? disconnected : connected;
    await start(page, initial);
    const unavailable: DriveConnectionStatus = { state: 'storage_unavailable', clientConfigured: false, scope, message: 'Unlock the operating system credential vault and try again.' };
    await page.evaluate(({ operation, unavailable }) => {
      const methods = { connect: 'ConnectGoogleDrive', disconnect: 'DisconnectGoogleDrive', revoke: 'RevokeGoogleDrive' } as const;
      window.go!.desktop!.App![methods[operation]] = async () => {
        window.driveHarness.calls.push(operation);
        window.driveHarness.setStatus(unavailable);
        throw 'sensitive-vault-error';
      };
    }, { operation, unavailable });
    const labels = { connect: 'Connect Google Drive', disconnect: 'Disconnect from this device', revoke: 'Revoke access on Google' };
    await page.getByRole('button', { name: labels[operation], exact: true }).click();
    if (operation === 'revoke') await page.getByRole('dialog').getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
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
  await expect(page.getByRole('button', { name: 'Disconnect from this device', exact: true })).toHaveCount(0);
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
  await page.getByRole('button', { name: 'Disconnect from this device', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Could not remove the local Google Drive credentials.');
  await expect(page.getByRole('button', { name: 'Disconnect from this device', exact: true })).toBeEnabled();
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

test('remote revocation defaults to cancel and cancel or Escape never invokes the backend', async ({ page }) => {
  await start(page, connected);
  const trigger = page.getByRole('button', { name: 'Revoke access on Google', exact: true });
  for (const method of ['button', 'Escape', 'Enter']) {
    await trigger.click();
    const dialog = page.getByRole('dialog', { name: 'Revoke access on Google?' });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeFocused();
    await expect(dialog).toContainText('Other apps whose OAuth clients share the same Google Cloud project may also lose this account’s authorization.');
    await expect(dialog).toContainText('fixture@example.invalid');
    if (method === 'button') await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    else await page.keyboard.press(method);
    await expect(dialog).toHaveCount(0);
    await expect(trigger).toBeFocused();
    await expect(page.getByText('Connected', { exact: true })).toBeVisible();
    expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status']);
    expect(await page.evaluate(() => window.driveHarness.revocations)).toEqual([]);
  }
});

test('remote revocation sends the explicitly confirmed account and clears identity only after success', async ({ page }) => {
  await start(page, connected);
  await page.getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Revoke access on Google?' });
  await expect(dialog).toContainText('To remove credentials only from this computer, cancel and choose “Disconnect from this device”.');
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status']);
  await dialog.getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText('Not connected', { exact: true })).toBeVisible();
  await expect(page.getByRole('status')).toHaveText('Google access revoked and local credentials removed.');
  await expect(page.getByText('fixture@example.invalid', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Connect Google Drive', exact: true })).toBeEnabled();
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'revoke']);
  expect(await page.evaluate(() => window.driveHarness.revocations)).toEqual([{ accountReference: connected.account!.reference, confirmed: true }]);
});

test('revocation in flight disables competing account operations without reporting Connected', async ({ page }) => {
  await start(page, connected);
  await page.evaluate(() => {
    window.go!.desktop!.App!.RevokeGoogleDrive = async (accountReference, confirmed) => {
      window.driveHarness.calls.push('revoke');
      window.driveHarness.revocations.push({ accountReference, confirmed });
      return new Promise<DriveConnectionStatus>(resolve => { window.driveHarness.complete = result => { window.driveHarness.setStatus(result); resolve(result); }; });
    };
  });
  await page.getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  await expect(page.getByText('Revoking Google access', { exact: true })).toBeVisible();
  for (const name of ['Check connection', 'Disconnect from this device', 'Revoke access on Google']) {
    await expect(page.getByRole('button', { name, exact: true })).toBeDisabled();
  }
  await expect(page.getByText('Connected', { exact: true })).toHaveCount(0);
  await expect(page.getByText('Google Drive connected. Choose a destination and preview your folder upload.', { exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'revoke']);
  await page.evaluate(result => window.driveHarness.complete(result), disconnected);
  await expect(page.getByText('Not connected', { exact: true })).toBeVisible();
});

test('failed revocation reconciles account status without exposing transport details or retrying remotely', async ({ page }) => {
  const consoleMessages: string[] = [];
  page.on('console', message => consoleMessages.push(message.text()));
  await start(page, connected);
  await page.evaluate(() => {
    window.go!.desktop!.App!.RevokeGoogleDrive = async () => { window.driveHarness.calls.push('revoke'); throw new Error('synthetic-refresh-token-revoke-error <script>window.injected=true</script>'); };
  });
  await page.getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Could not confirm revocation and local cleanup.');
  await expect(page.getByText('fixture@example.invalid', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Revoke access on Google', exact: true })).toBeEnabled();
  await expect(page.locator('body')).not.toContainText('synthetic-refresh-token-revoke-error');
  expect(consoleMessages.join('\n')).not.toContain('synthetic-refresh-token-revoke-error');
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length, window.injected])).toEqual([0, 0, undefined]);
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'revoke', 'status']);
  await page.getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'revoke', 'status']);
});

test('Google success with failed vault cleanup permits only local cleanup and never repeats revocation', async ({ page }) => {
  await start(page, connected);
  const cleanup: DriveConnectionStatus = { ...connected, state: 'revoked_local_cleanup_required', message: 'Google confirmed revocation. Local credential cleanup still needs an unlocked vault.' };
  await page.evaluate(result => {
    window.go!.desktop!.App!.RevokeGoogleDrive = async () => {
      window.driveHarness.calls.push('revoke'); window.driveHarness.setStatus(result);
      throw new Error('synthetic-private-cleanup-error');
    };
  }, cleanup);
  await page.getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  await expect(page.getByText('Google access revoked; local cleanup required', { exact: true })).toBeVisible();
  await expect(page.getByRole('status')).toContainText('Google confirmed revocation.');
  await expect(page.getByText(/Finish local cleanup before closing LedgeSync/)).toBeVisible();
  await expect(page.getByText(/this warning cannot be saved while the vault is unavailable/)).toBeVisible();
  for (const name of ['Check connection', 'Connect Google Drive', 'Reconnect Google Drive', 'Revoke access on Google']) {
    await expect(page.getByRole('button', { name, exact: true })).toHaveCount(0);
  }
  await expect(page.getByText('Google Drive connected. Choose a destination and preview your folder upload.', { exact: true })).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText('synthetic-private-cleanup-error');
  await page.getByRole('button', { name: 'Disconnect from this device', exact: true }).click();
  await expect(page.getByText('Not connected', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Connect Google Drive', exact: true })).toBeEnabled();
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'revoke', 'status', 'disconnect']);
});

test('an account changed outside the UI cannot retarget an open revocation confirmation', async ({ page }) => {
  await start(page, connected);
  await page.getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('fixture@example.invalid');
  const replacement: DriveConnectionStatus = { ...connected, account: { reference: 'google-drive:replacement-account', displayName: 'Replacement Account', email: 'replacement@example.invalid' } };
  // A different process replaced the persisted grant; the open dialog still
  // represents the original account. The service rejects that expected reference.
  await page.evaluate(result => window.driveHarness.setStatus(result), replacement);
  await dialog.getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Could not confirm revocation and local cleanup.');
  await expect(page.getByText('replacement@example.invalid', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => window.driveHarness.revocations)).toEqual([{ accountReference: connected.account!.reference, confirmed: true }]);
  expect(await page.evaluate(() => window.driveHarness.calls)).toEqual(['status', 'revoke', 'status']);
  await page.getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('replacement@example.invalid');
  expect(await page.evaluate(() => window.driveHarness.revocations)).toHaveLength(1);
});

test('failed revocation status reconciliation removes stale identity and all account actions', async ({ page }) => {
  await start(page, connected);
  await page.evaluate(() => {
    window.go!.desktop!.App!.RevokeGoogleDrive = async () => { throw new Error('synthetic-revocation-response'); };
    window.go!.desktop!.App!.GoogleDriveStatus = async () => { throw new Error('synthetic-vault-response'); };
  });
  await page.getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Revoke access on Google', exact: true }).click();
  await expect(page.getByText('Not checked', { exact: true })).toBeVisible();
  await expect(page.getByRole('alert')).toContainText('The saved connection status could not be read.');
  await expect(page.getByRole('article', { name: 'Google Drive connection' }).getByRole('button')).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Retry connection status', exact: true })).toBeEnabled();
  await expect(page.getByText('fixture@example.invalid', { exact: true })).toHaveCount(0);
  await expect(page.getByText('Connected', { exact: true })).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText('synthetic-revocation-response');
  await expect(page.locator('body')).not.toContainText('synthetic-vault-response');
});
