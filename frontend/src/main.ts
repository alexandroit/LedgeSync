import './style.css';
import type { DesktopBridge, DriveConnectionStatus, Entry, Explanation, Operation, Preview } from './types';

type View = 'files' | 'preview' | 'policies' | 'connections';
type DriveAction = 'status' | 'import' | 'connect' | 'check' | 'disconnect';
const state = {
  preview: null as Preview | null, view: 'files' as View, path: '', query: '',
  showExcluded: false, grid: false, selected: '', busy: false, error: '',
  filter: 'all', page: 0, history: [''], historyIndex: 0,
  drive: null as DriveConnectionStatus | null,
  driveBusy: '' as DriveAction | '', driveError: '', driveCancelling: false,
  driveLoaded: false, driveOpeningSetup: false,
  driveSetupExpanded: null as boolean | null,
  driveRevision: 0,
  driveCancelIntent: false,
};
const pageSize = 100;
const root = document.querySelector<HTMLDivElement>('#app')!;

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className = '', text?: string): HTMLElementTagNameMap[K] {
  const element = document.createElement(tag);
  element.className = className;
  if (text !== undefined) element.textContent = text;
  return element;
}
function icon(name: string, className = ''): HTMLElement {
  const span = el('span', `icon ${className}`);
  span.setAttribute('aria-hidden', 'true');
  const paths: Record<string, string> = {
    folder: '<path d="M3 6a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v10H3Z"/>',
    file: '<path d="M6 3h8l4 4v14H6Z"/><path d="M14 3v5h4M9 13h6M9 17h6"/>',
    search: '<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>',
    list: '<path d="M8 6h13M8 12h13M8 18h13M3 6h.1M3 12h.1M3 18h.1"/>',
    grid: '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>',
    refresh: '<path d="M20 10a8 8 0 1 0-2 8M20 3v7h-7"/>',
    arrow: '<path d="M4 12h16m-6-6 6 6-6 6"/>',
    back: '<path d="m14 6-6 6 6 6"/>',
    check: '<path d="m5 12 4 4L19 6"/>',
    shield: '<path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6Z"/><path d="m8 12 3 3 5-6"/>',
    cloud: '<path d="M7 18a5 5 0 1 1 0-10 6 6 0 0 1 11 2 4 4 0 0 1 0 8Z"/>',
    history: '<path d="M3 11a9 9 0 1 1 2 7M3 4v7h7M12 7v6l4 2"/>',
    settings: '<path d="M4 7h16M4 17h16"/><circle cx="8" cy="7" r="3"/><circle cx="16" cy="17" r="3"/>',
    activity: '<path d="M2 12h5l3-8 4 16 3-8h5"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/>',
  };
  // Only fixed application-owned SVG paths enter innerHTML; filenames use textContent.
  span.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">${paths[name] ?? paths.file}</svg>`;
  return span;
}
function button(label: string, action: () => void, className = '', symbol?: string) {
  const b = el('button', className);
  b.type = 'button';
  if (symbol) b.append(icon(symbol));
  b.append(el('span', '', label));
  b.addEventListener('click', action);
  return b;
}
function bridge(): DesktopBridge {
  const api = window.go?.desktop?.App;
  if (!api) throw new Error('Open the LedgeSync desktop application to choose a local folder. This browser preview cannot access your files.');
  return api;
}
async function scan(method: 'OpenFolder' | 'OpenConfiguration' | 'Refresh') {
  if (state.busy) return;
  state.busy = true; state.error = ''; render();
  try {
    const preview = await bridge()[method]();
    if (preview) {
      state.preview = preview;
      state.page = 0;
      if (method !== 'Refresh') {
        state.path = ''; state.history = ['']; state.historyIndex = 0;
        state.selected = ''; state.query = ''; state.page = 0; state.view = 'files';
      }
      if (state.path && !preview.entries.some(e => e.path === state.path && e.kind === 'directory')) state.path = '';
      if (method === 'Refresh') { state.history = [state.path]; state.historyIndex = 0; }
      if (!preview.entries.some(e => e.path === state.selected)) state.selected = '';
    }
  } catch (error) {
    state.error = error instanceof Error ? error.message : String(error);
    state.preview = null; state.selected = ''; state.path = '';
  } finally { state.busy = false; render(); }
}
function navigate(path: string, addHistory = true) {
  state.path = path; state.query = ''; state.selected = ''; state.page = 0;
  if (addHistory) {
    state.history = state.history.slice(0, state.historyIndex + 1);
    state.history.push(path); state.historyIndex++;
  }
  render();
}
function historyMove(amount: number) {
  state.historyIndex += amount;
  navigate(state.history[state.historyIndex], false);
}
function changeView(view: View) {
  state.view = view; state.query = ''; state.page = 0; render();
  if (view === 'connections' && !state.driveLoaded && !state.driveBusy) void driveAction('status');
}
async function driveAction(action: DriveAction) {
  if (state.driveBusy || state.driveCancelling || state.drive?.state === 'connecting' && action !== 'status') return;
  if (action === 'connect') state.driveCancelIntent = false;
  state.driveBusy = action; state.driveError = ''; render();
  try {
    const methods = { status: 'GoogleDriveStatus', import: 'ImportGoogleOAuthClient', connect: 'ConnectGoogleDrive', check: 'CheckGoogleDrive', disconnect: 'DisconnectGoogleDrive' } as const;
    const result = await bridge()[methods[action]]();
    // The native file picker returns null when dismissed; keep the previous state.
    if (result) { setDriveStatus(result); state.driveError = ''; }
  } catch {
    // Transport exceptions are not a safe display surface for credentials or provider responses.
    const messages: Record<DriveAction, string> = {
      status: 'Could not read the Google Drive connection. Open the current LedgeSync desktop application and try again.',
      import: 'Could not import the OAuth client. Choose the JSON downloaded for a Google Cloud Desktop app client and try again.',
      connect: 'Google Drive authorization could not be completed. Try connecting again from the desktop application.',
      check: 'Could not check the Google Drive connection. Check your network connection and try again.',
      disconnect: 'Could not remove the local Google Drive credentials. Check that your system credential vault is available and try again.',
    };
    state.driveError = messages[action];
    // Wails rejects a Go (status, error) result and drops its status value.
    // Re-read once after this explicit action so a revoked account or vault failure
    // cannot leave a stale Connected badge. Never infer state from error strings.
    if (action === 'status') setDriveStatus(null);
    else if (!await reconcileDriveStatus()) state.driveError += ' The saved connection status could not be read. Retry connection status.';
    else if (action === 'connect' && state.driveCancelIntent) state.driveError = '';
  } finally {
    if (action === 'connect') state.driveCancelIntent = false;
    state.driveLoaded = true; state.driveBusy = ''; render();
  }
}
function setDriveStatus(status: DriveConnectionStatus | null) {
  state.drive = status; state.driveRevision++;
}
async function reconcileDriveStatus(): Promise<boolean> {
  const revision = state.driveRevision;
  try {
    const status = await bridge().GoogleDriveStatus();
    // An authorization may finish while a cancellation's status read is in flight.
    if (revision === state.driveRevision) setDriveStatus(status);
    return true;
  } catch {
    if (revision === state.driveRevision) setDriveStatus(null);
    return false;
  }
}
async function cancelDrive() {
  if (state.driveCancelling) return;
  const pendingConnect = state.driveBusy === 'connect';
  if (pendingConnect) state.driveCancelIntent = true;
  state.driveCancelling = true; state.driveError = ''; render();
  try {
    await bridge().CancelGoogleDrive();
    // An authorization started elsewhere can also be cancelled from this screen.
    if (!pendingConnect && !await reconcileDriveStatus()) state.driveError = 'The saved connection status could not be read. Retry connection status.';
  } catch {
    state.driveCancelIntent = false;
    state.driveError = 'Could not cancel authorization. You can close the Google authorization page; the pending request will time out.';
  } finally { state.driveCancelling = false; render(); }
}
async function openGoogleSetup() {
  if (state.driveOpeningSetup) return;
  state.driveOpeningSetup = true; state.driveError = ''; render();
  try { await bridge().OpenGoogleOAuthSetup(); }
  catch { state.driveError = 'Could not open your browser. Open console.cloud.google.com in your browser to configure a Desktop app OAuth client.'; }
  finally { state.driveOpeningSetup = false; render(); }
}
function bytes(size: number): string {
  if (size < 1024) return `${size} B`;
  const units = ['KiB', 'MiB', 'GiB', 'TiB'];
  let unit = -1;
  do { size /= 1024; unit++; } while (size >= 1024 && unit < units.length - 1);
  return `${size.toFixed(size < 10 ? 1 : 0)} ${units[unit]}`;
}
function date(value: string) {
  const parsed = new Date(value);
  return Number.isNaN(parsed.valueOf()) ? '—' : parsed.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}
function displayPath(path: string) { return path.replace(/[\u0000-\u001f\u007f\u202a-\u202e\u2066-\u2069]/g, c => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`); }
function badge(text: string, kind = '') { return el('span', `badge ${kind}`, text); }

function render() {
  const focused = document.activeElement as HTMLInputElement | null;
  const restoreSearch = focused?.id === 'search';
  const restoreDrive = focused?.id.startsWith('drive-') ? focused.id : '';
  const cursor = focused?.selectionStart ?? 0;
  root.replaceChildren();
  const shell = el('div', 'shell');
  const sidebar = el('aside', 'sidebar');
  const brand = el('div', 'brand'); brand.append(icon('check', 'brand-mark'), el('span', '', 'LedgeSync'));
  sidebar.append(brand);
  const choose = button('Choose folder', () => void scan('OpenFolder'), 'button primary choose-folder', 'folder');
  choose.disabled = state.busy; sidebar.append(choose);
  const nav = el('nav', 'nav'); nav.setAttribute('aria-label', 'Main navigation');
  for (const [view, title, symbol] of [['files', 'Files', 'folder'], ['preview', 'Sync pairs', 'arrow'], ['policies', 'Policies', 'shield'], ['connections', 'Connections', 'cloud']] as const) {
    const item = button(title, () => changeView(view), `nav-item ${state.view === view ? 'active' : ''}`, symbol);
    if (state.view === view) item.setAttribute('aria-current', 'page');
    nav.append(item);
  }
  for (const [title, symbol] of [['Activity', 'activity'], ['History & Recovery', 'history'], ['Settings', 'settings']]) {
    const item = button(title, () => {}, 'nav-item planned', symbol);
    item.disabled = true; item.title = 'Planned for a later release'; item.append(el('small', '', 'Later')); nav.append(item);
  }
  sidebar.append(nav);
  const offline = el('div', 'sidebar-note'); offline.append(icon('shield'), el('strong', '', 'Preview before transfer'), el('p', '', 'Developer alpha · 0.1.0-alpha.2'), el('p', '', state.drive?.state === 'connected' ? 'Google Drive connected. Cloud transfers are not available yet.' : state.drive?.state === 'reconnect_required' ? 'Google Drive needs reconnection.' : 'Connect Google Drive in Connections.'));
  sidebar.append(offline); shell.append(sidebar);

  const workspace = el('main', 'workspace');
  const top = el('header', 'topbar');
  const search = el('label', 'search'); search.append(icon('search'));
  const input = el('input'); input.id = 'search'; input.type = 'search'; input.placeholder = 'Search this project'; input.setAttribute('aria-label', 'Search this project'); input.value = state.query;
  input.disabled = !state.preview || state.busy || state.view === 'policies' || state.view === 'connections';
  input.addEventListener('input', () => { state.query = input.value; state.page = 0; render(); });
  search.append(input); top.append(search);
  top.append(badge('Local file preview', 'offline'));
  const loadConfig = button('Open configuration', () => void scan('OpenConfiguration'), 'button subtle'); loadConfig.disabled = state.busy; top.append(loadConfig);
  workspace.append(top);
  if (state.error) { const error = el('div', 'error', state.error); error.setAttribute('role', 'alert'); workspace.append(error); }
  if (state.busy) {
    const progress = el('div', 'progress'); progress.setAttribute('role', 'status');
    progress.append(el('span', 'spinner'), el('span', '', 'Reading local files and policy sources…'));
    progress.append(button('Cancel scan', () => { void bridge().Cancel().catch(error => { state.error = String(error); render(); }); }, 'button subtle'));
    workspace.append(progress);
  }
  const body = el('div', `body ${state.preview ? 'has-preview' : ''}`);
  const content = el('section', 'content'); content.setAttribute('aria-label', state.view === 'connections' ? 'Account connections' : 'File workspace');
  if (state.view === 'connections') renderConnections(content);
  else if (!state.preview) renderEmpty(content);
  else {
    const heading = el('div', 'heading');
    const title = el('div'); title.append(el('p', 'eyebrow', 'LOCAL WORKSPACE'), el('h1', '', state.view === 'files' ? 'Files' : state.view === 'preview' ? 'Preview your sync pair' : 'Policy capabilities'));
    heading.append(title);
    const refresh = button('Refresh', () => void scan('Refresh'), 'button subtle', 'refresh'); refresh.disabled = state.busy; heading.append(refresh); content.append(heading);
    if (state.view === 'files') renderFiles(content);
    else if (state.view === 'preview') renderPreview(content);
    else renderPolicies(content);
  }
  body.append(content);
  if (state.preview && state.view !== 'policies' && state.view !== 'connections') { const inspector = el('aside', 'inspector'); inspector.id = 'inspector'; inspector.setAttribute('aria-label', 'Details and policy explanation'); renderInspector(inspector); body.append(inspector); }
  workspace.append(body); shell.append(workspace); root.append(shell);
  if (restoreSearch) { const input = document.querySelector<HTMLInputElement>('#search')!; input.focus(); input.setSelectionRange(cursor, cursor); }
  else if (restoreDrive) {
    const target = document.getElementById(restoreDrive) as HTMLButtonElement | null;
    if (target && !target.disabled) target.focus();
    else document.querySelector<HTMLElement>('#drive-heading')?.focus();
  }
}

function driveButton(label: string, action: () => void, id: string, primary = false) {
  const control = button(label, action, `button ${primary ? 'primary' : 'subtle'}`);
  control.id = `drive-${id}`;
  control.disabled = Boolean(state.driveBusy || state.driveCancelling);
  return control;
}
function renderConnections(container: HTMLElement) {
  container.classList.add('connections');
  const heading = el('div', 'heading');
  const title = el('div');
  const h1 = el('h1', '', 'Connections'); h1.id = 'drive-heading'; h1.tabIndex = -1;
  title.append(el('p', 'eyebrow', 'ACCOUNT ACCESS'), h1); heading.append(title); container.append(heading);
  container.append(el('p', 'section-description', 'Authorize Google Drive in your system browser. LedgeSync keeps account tokens in your operating system credential vault.'));
  const limits = el('div', 'notice');
  limits.append(icon('info'), el('p', '', 'Google Drive authorization is available in this build. Cloud browsing and file transfers are not implemented yet. Files and Sync pairs still show local files and a simulated destination.'));
  container.append(limits);
  if (state.driveError) { const error = el('div', 'error', state.driveError); error.setAttribute('role', 'alert'); container.append(error); }

  const card = el('article', 'connection-card'); card.setAttribute('aria-label', 'Google Drive connection');
  const cardHeading = el('div', 'connection-heading');
  const provider = el('div', 'connection-provider'); provider.append(icon('cloud'), el('h2', '', 'Google Drive'));
  const labels: Record<DriveConnectionStatus['state'], string> = { setup_required: 'Setup required', disconnected: 'Not connected', connecting: 'Waiting for authorization', connected: 'Connected', reconnect_required: 'Reconnect required', storage_unavailable: 'Credential vault unavailable' };
  cardHeading.append(provider, badge(state.driveBusy === 'connect' ? labels.connecting : state.drive ? labels[state.drive.state] : 'Not checked', state.drive?.state === 'connected' ? 'connected' : ''));
  card.append(cardHeading);
  const status = el('div', 'connection-status'); status.setAttribute('role', 'status'); status.setAttribute('aria-live', 'polite'); status.setAttribute('aria-atomic', 'true');
  const pending = state.driveBusy === 'connect' || state.drive?.state === 'connecting';
  const pendingLabels: Record<DriveAction, string> = { status: 'Reading the saved connection…', import: 'Choose the Desktop app OAuth client JSON in the file picker…', connect: 'Complete authorization in your browser, then return to LedgeSync. You can cancel this request here.', check: 'Checking account authorization with Google…', disconnect: 'Removing local account credentials…' };
  if (state.driveBusy || pending) status.append(el('span', 'spinner'), el('p', '', state.driveCancelling ? 'Cancelling authorization…' : pendingLabels[state.driveBusy || 'connect']));
  else status.append(el('p', '', state.drive?.message || 'Read the saved connection to get started.'));
  card.append(status);

  if (state.drive?.account) {
    const account = el('dl', 'details connection-account');
    field(account, 'Account', displayPath(state.drive.account.displayName || 'Google account'));
    field(account, 'Email', displayPath(state.drive.account.email));
    field(account, 'Account reference', displayPath(state.drive.account.reference));
    card.append(account);
  }
  const actions = el('div', 'connection-actions');
  if (pending) {
    const cancel = driveButton(state.driveCancelling ? 'Cancelling…' : 'Cancel authorization', () => void cancelDrive(), 'cancel');
    cancel.disabled = state.driveCancelling; actions.append(cancel);
  } else if (!state.drive || state.drive.state === 'storage_unavailable') {
    actions.append(driveButton('Retry connection status', () => void driveAction('status'), 'status'));
  } else if (state.drive.state === 'connected') {
    actions.append(driveButton('Check connection', () => void driveAction('check'), 'check'), driveButton('Disconnect account', () => void driveAction('disconnect'), 'disconnect'));
  } else {
    if (state.drive.clientConfigured) actions.append(driveButton(state.drive.state === 'reconnect_required' ? 'Reconnect Google Drive' : 'Connect Google Drive', () => void driveAction('connect'), 'connect', true));
    if (!state.drive.account) actions.append(driveButton(state.drive.clientConfigured ? 'Replace OAuth client JSON' : 'Import OAuth client JSON', () => void driveAction('import'), 'import', !state.drive.clientConfigured));
    if (state.drive.account) actions.append(driveButton('Disconnect account', () => void driveAction('disconnect'), 'disconnect'));
  }
  card.append(actions);
  const access = el('div', 'connection-access');
  access.append(el('h3', '', 'Access you authorize'), el('p', '', 'The drive.file permission is limited to files created by, or explicitly opened with, LedgeSync. It does not grant access to every existing file or folder in your Drive. Selecting a folder does not automatically grant access to its existing contents.'));
  access.append(el('code', 'connection-scope', state.drive?.scope || 'https://www.googleapis.com/auth/drive.file'));
  access.append(el('p', '', 'One account is supported in this build. Disconnect before connecting another account.'));
  if (state.drive?.account) access.append(el('p', 'disconnect-help', 'Disconnect removes this account’s tokens from this computer. It does not delete Drive files or revoke the Google permission grant. You can revoke the grant separately in your Google Account connections settings.'));
  card.append(access); container.append(card);

  const setup = el('details', 'connection-setup'); setup.open = state.driveSetupExpanded ?? !state.drive?.clientConfigured;
  setup.addEventListener('toggle', () => { if (setup.isConnected) state.driveSetupExpanded = setup.open; });
  setup.append(el('summary', '', 'Set up your Google Cloud OAuth client'));
  setup.append(el('p', '', 'This build requires your own Desktop app OAuth client. You only need to import its configuration once on this computer.'));
  const steps = el('ol');
  for (const text of [
    'Create or choose a project in Google Cloud Console, then enable the Google Drive API.',
    'Open Google Auth platform. Configure Branding and Audience. For personal testing, choose External and add your Google account under Test users.',
    'In Data Access, add https://www.googleapis.com/auth/drive.file. This is the permission LedgeSync requests.',
    'Open Clients, choose Create client, select Desktop app, and download the client JSON. Import that file above, then choose Connect Google Drive.',
  ]) steps.append(el('li', '', text));
  setup.append(steps);
  const open = driveButton(state.driveOpeningSetup ? 'Opening browser…' : 'Open Google Cloud setup', () => void openGoogleSetup(), 'setup'); open.disabled = state.driveOpeningSetup; setup.append(open);
  setup.append(el('p', 'muted', 'Sign in and approve access only on Google’s page in your browser. Do not paste passwords, authorization codes, or tokens into LedgeSync. Keep the downloaded client JSON out of shared projects and source control.'));
  container.append(setup);
}

function renderEmpty(container: HTMLElement) {
  const empty = el('div', 'welcome'); empty.append(el('div', 'welcome-mark')); empty.firstElementChild!.append(icon('folder'));
  empty.append(el('p', 'eyebrow', 'MEET YOUR NEXT CLEAN SYNC'), el('h1', '', 'See what belongs.'), el('p', 'welcome-description', 'Explore a local folder, see which files your rules exclude, and review every planned copy before anything moves.'));
  const choose = button('Choose a local folder', () => void scan('OpenFolder'), 'button primary large', 'folder'); choose.disabled = state.busy; empty.append(choose);
  empty.append(el('p', 'welcome-hint', 'Starts with recursive .gitignore rules. Read-only, with no account needed.'));
  const features = el('div', 'welcome-features');
  for (const [symbol, title, description] of [['folder', 'Your files, in view', 'Browse a real local inventory.'], ['shield', 'Every rule explained', 'See why each file is included or excluded.'], ['arrow', 'Preview first', 'Compare against an empty test destination.']]) {
    const card = el('div'); card.append(icon(symbol), el('strong', '', title), el('p', '', description)); features.append(card);
  }
  empty.append(features); container.append(empty);
}
function renderFiles(container: HTMLElement) {
  const preview = state.preview!;
  const breadcrumbs = el('nav', 'breadcrumbs'); breadcrumbs.setAttribute('aria-label', 'Folder path');
  const back = button('Back', () => historyMove(-1), 'icon-button', 'back'); back.disabled = state.historyIndex === 0; back.setAttribute('aria-label', 'Back');
  const forward = button('Forward', () => historyMove(1), 'icon-button forward', 'back'); forward.disabled = state.historyIndex === state.history.length - 1; forward.setAttribute('aria-label', 'Forward');
  breadcrumbs.append(back, forward, button(preview.projectName || 'Local folder', () => navigate(''), 'crumb', 'folder'));
  let path = '';
  for (const part of state.path.split('/').filter(Boolean)) {
    path = path ? `${path}/${part}` : part; const target = path;
    breadcrumbs.append(el('span', 'separator', '/'), button(displayPath(part), () => navigate(target), 'crumb'));
  }
  container.append(breadcrumbs);
  const toolbar = el('div', 'file-toolbar');
  const toggle = el('label', 'excluded-toggle');
  const checkbox = el('input'); checkbox.type = 'checkbox'; checkbox.checked = state.showExcluded;
  checkbox.addEventListener('change', () => { state.showExcluded = checkbox.checked; state.page = 0; render(); });
  toggle.append(checkbox, el('span', '', 'Show excluded files')); toolbar.append(toggle);
  const excluded = preview.entries.filter(entry => entry.decision === 'exclude').length;
  toolbar.append(el('span', 'muted excluded-total', `${excluded} excluded in project`));
  const modes = el('div', 'view-modes');
  for (const grid of [false, true]) {
    const mode = button(grid ? 'Grid view' : 'List view', () => { state.grid = grid; render(); }, `icon-button ${state.grid === grid ? 'selected' : ''}`, grid ? 'grid' : 'list');
    mode.setAttribute('aria-pressed', String(state.grid === grid)); mode.title = grid ? 'Grid view' : 'List view'; modes.append(mode);
  }
  toolbar.append(modes); container.append(toolbar);
  const query = state.query.toLocaleLowerCase();
  const entries = preview.entries.filter(entry => (state.showExcluded || entry.decision !== 'exclude') && (query ? entry.path.toLocaleLowerCase().includes(query) : parentPath(entry.path) === state.path)).sort((a, b) => (a.kind === 'directory' ? 0 : 1) - (b.kind === 'directory' ? 0 : 1) || a.name.localeCompare(b.name));
  if (query) container.append(el('p', 'result-note', `Search results across the project for “${state.query}”`));
  const list = el('div', state.grid ? 'file-list grid' : 'file-list'); list.setAttribute('aria-label', 'Files'); list.setAttribute('role', 'list');
  if (!state.grid) { const columns = el('div', 'file-columns'); columns.append(el('span', '', 'Name'), el('span', '', 'Status'), el('span', '', 'Size'), el('span', '', 'Modified')); list.append(columns); }
  for (const entry of entries.slice(state.page * pageSize, (state.page + 1) * pageSize)) {
    const item = el('div', 'file-item'); item.setAttribute('role', 'listitem');
    const row = button('', () => select(entry.path), `file-row ${entry.decision === 'exclude' ? 'excluded' : ''} ${state.selected === entry.path ? 'is-selected' : ''}`);
    row.replaceChildren(); row.dataset.path = entry.path; row.setAttribute('aria-label', `${displayPath(entry.path)}, ${entry.status}`); row.setAttribute('aria-pressed', String(state.selected === entry.path));
    const name = el('span', 'file-name'); name.append(icon(entry.kind === 'directory' ? 'folder' : 'file', entry.kind === 'directory' ? 'folder-icon' : 'file-icon'));
    const nameText = el('span'); nameText.append(el('strong', '', displayPath(entry.name))); if (query) nameText.append(el('small', '', displayPath(parentPath(entry.path)) || 'Project root')); name.append(nameText);
    row.append(name, badge(entry.status, entry.decision === 'exclude' ? 'excluded-badge' : ''), el('span', 'file-size', entry.kind === 'directory' ? '—' : bytes(entry.size)), el('span', 'file-date', date(entry.modifiedAt)));
    if (entry.kind === 'directory') {
      row.title = 'Double-click or press Enter to open this folder';
      row.addEventListener('dblclick', () => navigate(entry.path));
      row.addEventListener('keydown', event => { if (event.key === 'Enter') { event.preventDefault(); navigate(entry.path); } });
    }
    item.append(row); list.append(item);
  }
  container.append(list);
  if (!entries.length) container.append(el('div', 'empty-list', query ? 'No matching files. Try another search or show excluded files.' : 'No visible files in this folder. Excluded items may be hidden.'));
  renderPagination(container, entries.length);
  container.append(el('p', 'footnote', 'Folder selection uses case-sensitive Gitignore patterns for every file, including tracked files. Open a configuration for other supported rule sources.'));
}
function parentPath(path: string) { const index = path.lastIndexOf('/'); return index < 0 ? '' : path.slice(0, index); }
function renderPagination(container: HTMLElement, count: number) {
  const pagination = el('div', 'pagination'); pagination.append(el('span', 'muted', `${count} item${count === 1 ? '' : 's'}${count > pageSize ? ` · page ${state.page + 1} of ${Math.ceil(count / pageSize)}` : ''}`));
  if (count > pageSize) {
    const previous = button('Previous', () => { state.page--; render(); }, 'button subtle'); previous.disabled = state.page === 0;
    const next = button('Next', () => { state.page++; render(); }, 'button subtle'); next.disabled = (state.page + 1) * pageSize >= count;
    pagination.append(previous, next);
  }
  container.append(pagination);
}
function select(path: string) {
  state.selected = path;
  for (const row of document.querySelectorAll<HTMLElement>('[data-path]')) {
    row.classList.toggle('is-selected', row.dataset.path === path); row.setAttribute('aria-pressed', String(row.dataset.path === path));
  }
  const inspector = document.getElementById('inspector'); if (inspector) renderInspector(inspector);
}
function field(container: HTMLElement, name: string, value: string) {
  container.append(el('dt', '', name), el('dd', '', value));
}
function explain(container: HTMLElement, explanation: Explanation) {
  container.append(el('h3', '', 'Selection explanation'), el('p', 'reason', explanation.reason));
  const summary = el('dl', 'details'); field(summary, 'Decision', explanation.decision); field(summary, 'Composition', explanation.composition); container.append(summary);
  for (const group of explanation.groups ?? []) {
    const block = el('section', 'rule-card'); const head = el('div', 'rule-heading'); head.append(el('strong', '', group.groupId), badge(group.decision)); block.append(head);
    const details = el('dl', 'details'); field(details, 'Priority', String(group.priority));
    if (group.ancestorBlocker) field(details, 'Ancestor blocker', displayPath(group.ancestorBlocker));
    const p = group.provenance;
    if (p) {
      field(details, 'Adapter', p.adapter); field(details, 'Mechanism', p.mechanism); field(details, 'Dialect', p.dialect);
      field(details, 'Profile version', p.profileVersion); field(details, 'Source', displayPath(p.source));
      if (p.line) field(details, 'Line', String(p.line));
      field(details, 'Scope', displayPath(p.scope) || 'Project root'); field(details, 'Action', p.action);
      block.append(details, el('p', 'rule-label', 'Matching pattern'), el('code', 'pattern', displayPath(p.pattern)));
    } else { block.append(details, el('p', 'muted', 'No matching explicit rule in this group.')); }
    container.append(block);
  }
}
function renderInspector(container: HTMLElement) {
  container.replaceChildren(); const preview = state.preview!;
  const head = el('div', 'inspector-title'); head.append(el('h2', '', 'Details'), icon('info')); container.append(head);
  const entry = preview.entries.find(item => item.path === state.selected);
  if (!entry) {
    container.append(el('div', 'inspector-placeholder')); container.lastElementChild!.append(icon('file'));
    container.append(el('h3', '', 'Every file has a reason'), el('p', 'muted', 'Select a file or folder to inspect its metadata and the exact policy behind its selection.'));
    const details = el('dl', 'details'); field(details, 'Project', preview.projectName); field(details, 'Source', preview.sourceRoot); field(details, 'Destination', 'Empty test destination'); field(details, 'Scan', preview.plan.scanComplete.source ? 'Complete' : 'Incomplete'); container.append(details); return;
  }
  const entryTitle = el('div', 'selected-title'); entryTitle.append(icon(entry.kind === 'directory' ? 'folder' : 'file'), el('h3', '', displayPath(entry.name))); container.append(entryTitle, badge(entry.status, entry.decision === 'exclude' ? 'excluded-badge' : ''));
  const details = el('dl', 'details'); field(details, 'Path', displayPath(entry.path)); field(details, 'Kind', entry.kind); if (entry.kind !== 'directory') field(details, 'Size', bytes(entry.size)); field(details, 'Modified', date(entry.modifiedAt)); container.append(details);
  if (entry.kind === 'directory') container.append(button('Open folder', () => { state.view = 'files'; navigate(entry.path); }, 'button subtle', 'folder'));
  explain(container, entry.explanation);
  if (entry.decision === 'exclude') container.append(el('p', 'preserved-note', 'Excluded means skipped. Existing destination files remain untouched.'));
}
function operationLabel(operation: Operation) {
  const names: Record<string, string> = { 'upload-new': '→ Copy new file', 'create-directory': '→ Create folder', skip: '× Skip', conflict: '! Conflict', 'skip-verified': '= Verified' };
  return names[operation.type] ?? operation.type;
}
function renderPreview(container: HTMLElement) {
  const preview = state.preview!; const plan = preview.plan;
  const banner = el('div', 'notice'); banner.append(icon('info'), el('p', '', 'Read-only simulation using an empty test destination. This preview uses a simulated destination and cannot be applied, regardless of your Google Drive connection.')); container.append(banner);
  const pair = el('div', 'pair'); const local = el('div'); local.append(icon('folder'), el('strong', '', preview.projectName), el('small', '', 'Local folder')); const remote = el('div'); remote.append(icon('cloud'), el('strong', '', 'Test destination'), el('small', '', 'Empty · offline')); pair.append(local, icon('arrow'), remote); container.append(pair);
  const stats = el('div', 'stats');
  for (const [value, label] of [[String(plan.summary.operationCount), 'planned operations'], [bytes(plan.summary.uploadBytes), 'planned copy size'], [String(preview.entries.filter(e => e.decision === 'exclude').length), 'excluded items'], [String(plan.summary.trashCount), 'deletions']]) { const stat = el('div'); stat.append(el('strong', '', value), el('span', '', label)); stats.append(stat); }
  container.append(stats);
  const filters = el('div', 'filters'); filters.setAttribute('aria-label', 'Preview filters');
  for (const filter of ['all', 'changes', 'excluded', 'conflicts']) { const b = button(filter[0].toUpperCase() + filter.slice(1), () => { state.filter = filter; state.page = 0; render(); }, `filter ${state.filter === filter ? 'active' : ''}`); b.setAttribute('aria-pressed', String(state.filter === filter)); filters.append(b); }
  container.append(filters);
  const excludedPaths = new Set(preview.entries.filter(entry => entry.decision === 'exclude').map(entry => entry.path));
  const operations = plan.operations.filter(op => (!state.query || op.relativePath.toLocaleLowerCase().includes(state.query.toLocaleLowerCase())) && (state.filter === 'all' || state.filter === 'changes' && ['upload-new', 'create-directory'].includes(op.type) || state.filter === 'excluded' && excludedPaths.has(op.relativePath) || state.filter === 'conflicts' && op.type === 'conflict'));
  const list = el('div', 'operations');
  for (const op of operations.slice(state.page * pageSize, (state.page + 1) * pageSize)) {
    const row = button('', () => select(op.relativePath), `operation ${state.selected === op.relativePath ? 'is-selected' : ''}`); row.replaceChildren(); row.dataset.path = op.relativePath;
    row.append(el('span', 'operation-path', displayPath(op.relativePath)), el('span', 'operation-type', operationLabel(op)), el('span', 'muted', op.expectedSize ? bytes(op.expectedSize) : '—')); list.append(row);
  }
  container.append(list); if (!operations.length) container.append(el('p', 'empty-list', 'No operations in this view.')); renderPagination(container, operations.length);
  const digest = el('details', 'plan-details'); digest.append(el('summary', '', 'Plan identity and limitations'));
  const details = el('dl', 'details'); field(details, 'Plan digest', plan.planDigest); field(details, 'Rules digest', plan.rulesDigest); field(details, 'Destination identity', plan.destinationIdentity); digest.append(details);
  for (const risk of plan.risks ?? []) digest.append(el('p', 'muted', risk)); container.append(digest);
}
function renderPolicies(container: HTMLElement) {
  const preview = state.preview!;
  container.append(el('p', 'section-description', 'Capabilities are reported by the same engine that scans your files. Unsupported adapters block a configuration that requires them.'));
  const source = el('div', 'notice'); source.append(icon('shield'), el('p', '', 'Choose folder uses optional recursive .gitignore with conservative composition. Open a configuration to use custom filenames, multiple sources, or rclone filter profiles.')); container.append(source);
  const list = el('div', 'capabilities');
  for (const capability of preview.capabilities) {
    const card = el('article', 'capability'); const title = el('div', 'capability-title'); title.append(el('h3', '', capability.dialect), badge(capability.supported ? 'Available profile' : 'Not implemented', capability.supported ? 'offline' : '')); card.append(title);
    card.append(el('p', 'muted', `${capability.adapter} · ${capability.mechanism} · ${capability.profileVersion}`));
    for (const limitation of capability.limitations ?? []) card.append(el('p', 'capability-limit', limitation)); list.append(card);
  }
  container.append(list);
}
render();
