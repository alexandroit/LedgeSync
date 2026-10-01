import { expect, test } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { Preview } from '../src/types';

const repository = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
let fixture: string;
let preview: Preview;

test.beforeAll(() => {
  fixture = mkdtempSync(join(tmpdir(), 'ledgesync-ui-'));
  mkdirSync(join(fixture, 'src'));
  mkdirSync(join(fixture, 'build'));
  mkdirSync(join(fixture, 'z-pages'));
  for (let i = 0; i < 105; i++) writeFileSync(join(fixture, 'z-pages', `page${String(i).padStart(3, '0')}.txt`), 'pagination fixture');
  for (const [path, content] of Object.entries({ '.gitignore': 'build/\n*.log\n', 'README.md': 'LedgeSync test fixture', 'src/main.ts': 'export const fixture = true;', 'build/generated.js': 'excluded', 'debug.log': 'excluded log', '&copy;.txt': 'plain filename' })) {
    // An HTML entity is a valid filename on all desktop targets, displayed verbatim.
    writeFileSync(join(fixture, path), content);
  }
  preview = JSON.parse(execFileSync('go', ['run', './cmd/ledgesync', 'browse', '--root', fixture, '--json'], { cwd: repository, encoding: 'utf8', maxBuffer: 8 * 1024 * 1024 }));
});
test.afterAll(() => rmSync(fixture, { recursive: true, force: true }));
test.beforeEach(async ({ page }) => {
  await page.addInitScript(data => {
    // Stub only the Wails transport; every entry/plan/explanation came from Go.
    window.go = { desktop: { App: { OpenFolder: async () => data, OpenConfiguration: async () => data, Refresh: async () => data, Cancel: async () => {} } } };
  }, preview);
  await page.goto('/');
});

test('real core selection, excluded provenance, navigation, grid, search, and preview', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.getByRole('button', { name: 'Choose a local folder', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Files', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'debug.log, Excluded', exact: true })).toHaveCount(0);
  await page.getByLabel('Show excluded files', { exact: true }).check();
  await page.getByRole('button', { name: 'debug.log, Excluded', exact: true }).click();
  const inspector = page.getByRole('complementary', { name: 'Details and policy explanation' });
  await expect(inspector.getByText('*.log', { exact: true })).toBeVisible();
  await expect(inspector.getByText('.gitignore', { exact: true })).toBeVisible();
  await expect(inspector.getByText('gitignore', { exact: true })).toBeVisible();
  await expect(inspector.getByText('Excluded means skipped. Existing destination files remain untouched.')).toBeVisible();
  await page.getByRole('button', { name: 'Grid view', exact: true }).click();
  await expect(page.locator('.file-list.grid')).toBeVisible();
  await page.getByRole('button', { name: 'src, Local only', exact: true }).dblclick();
  await expect(page.getByRole('button', { name: 'src/main.ts, Local only', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Back', exact: true }).click();
  const folder = page.getByRole('button', { name: 'src, Local only', exact: true });
  await folder.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('button', { name: 'src/main.ts, Local only', exact: true })).toBeVisible();
  const back = page.getByRole('button', { name: 'Back', exact: true });
  await back.focus();
  await page.keyboard.press('Enter');
  await page.getByRole('searchbox', { name: 'Search this project' }).fill('main.ts');
  await expect(page.getByRole('searchbox', { name: 'Search this project' })).toBeFocused();
  await expect(page.getByRole('button', { name: 'src/main.ts, Local only', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Sync pairs', exact: true }).click();
  await expect(page.getByText('Read-only simulation using an empty test destination.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Excluded', exact: true }).click();
  await expect(page.locator('.operations')).toContainText('debug.log');
  await expect(page.locator('.operations')).not.toContainText('README.md');
  await page.getByRole('button', { name: 'Policies', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Policy capabilities' })).toBeVisible();
  await expect(page.getByText('svn-ignore', { exact: true })).toBeVisible();
  expect(errors).toEqual([]);
});

test('filenames remain text and failed refresh never presents an old valid plan', async ({ page }) => {
  await page.getByRole('button', { name: 'Choose a local folder', exact: true }).click();
  await expect(page.getByText('&copy;.txt', { exact: true })).toBeVisible();
  await expect(page.locator('.file-name b')).toHaveCount(0);
  await page.evaluate(() => { window.go!.desktop!.App!.Refresh = async () => { throw new Error('RULE_SOURCE_UNAVAILABLE: previously observed source disappeared'); }; });
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('RULE_SOURCE_UNAVAILABLE');
  await expect(page.locator('.file-list')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'See what belongs.' })).toBeVisible();
});

test('missing desktop transport is explicit and never generates fake inventory', async ({ page }) => {
  await page.evaluate(() => { delete window.go; });
  await page.getByRole('button', { name: 'Choose a local folder', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Open the LedgeSync desktop application');
  await expect(page.locator('.file-list')).toHaveCount(0);
});

test('refresh returns to the first page when the real inventory shrinks', async ({ page }) => {
  await page.getByRole('button', { name: 'Choose a local folder', exact: true }).click();
  await page.getByRole('button', { name: 'z-pages, Local only', exact: true }).dblclick();
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page.getByRole('button', { name: 'z-pages/page104.txt, Local only', exact: true })).toBeVisible();
  for (let i = 5; i < 105; i++) rmSync(join(fixture, 'z-pages', `page${String(i).padStart(3, '0')}.txt`));
  const updated = JSON.parse(execFileSync('go', ['run', './cmd/ledgesync', 'browse', '--root', fixture, '--json'], { cwd: repository, encoding: 'utf8', maxBuffer: 8 * 1024 * 1024 }));
  await page.evaluate(data => { window.go!.desktop!.App!.Refresh = async () => data; }, updated);
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('button', { name: 'z-pages/page000.txt, Local only', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Back', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Next', exact: true })).toHaveCount(0);
});
