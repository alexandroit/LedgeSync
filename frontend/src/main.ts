import './style.css';
import { describe, guidanceFor, parseError } from './errors';
import type { AutomationView, RestoreProgress, DesktopBridge, DriveConnectionStatus, DriveDestination, DriveFolder, DriveTransferStatus, DriveUploadPlan, Entry, Explanation, Operation, PolicyGroup, Preview, Project, ProjectPolicy, RunSummary, Settings, SyncActivity, SyncState, SyncStatus, TransferState } from './types';

type View = 'sync' | 'files' | 'preview' | 'policies' | 'connections' | 'activity' | 'history' | 'settings';
const version = '0.1.0-alpha.7';
type DriveAction = 'status' | 'connect' | 'check' | 'disconnect' | 'revoke';
const state = {
  preview: null as Preview | null, view: 'files' as View, path: '', query: '',
  showExcluded: false, grid: false, selected: '', busy: false, error: '',
  filter: 'all', page: 0, history: [''], historyIndex: 0,
  drive: null as DriveConnectionStatus | null,
  driveBusy: '' as DriveAction | '', driveError: '', driveCancelling: false,
  driveLoaded: false,
  driveRevision: 0,
  driveCancelIntent: false,
  destination: null as DriveDestination | null,
  uploadPlan: null as DriveUploadPlan | null,
  transfer: null as DriveTransferStatus | null,
  uploadBusy: '' as '' | 'destination' | 'preview' | 'start' | 'cancel',
  uploadError: '',
  uploadRevision: 0,
  transferPoll: 0,
  uploadPage: 0,
  destinationCancelling: false,
  destinationCancelIntent: false,
  unconfirmedUploadDigest: '',
  transferRevision: 0,
  projects: [] as Project[],
  projectsLoaded: false,
  currentProject: null as Project | null,
  projectError: '',
  projectBusy: false,
  runs: [] as RunSummary[],
  historyProject: '',
  historyError: '',
  settings: null as Settings | null,
  settingsError: '',
  settingsNotice: '',
  automation: null as AutomationView | null,
  automationPoll: 0,
  policyDraft: null as ProjectPolicy | null,
  policyError: '',
  restore: null as RestoreProgress | null,
  restoreError: '',
  restorePoll: 0,
  automationTrigger: 'interval' as 'interval' | 'watch',
  automationInterval: 900,
  syncs: [] as SyncStatus[],
  syncLoaded: false,
  syncError: '',
  syncBusy: '',
  syncActivity: [] as SyncActivity[],
  syncParent: { id: 'root', name: 'My Drive' },
  folderBrowser: { open: false, stack: [] as { id: string; name: string }[], folders: [] as DriveFolder[], loading: false, error: '' },
};
// Optional bridge methods are absent from older builds and browser previews.
function optional<K extends keyof DesktopBridge>(name: K): NonNullable<DesktopBridge[K]> | null {
  const api = window.go?.desktop?.App;
  const method = api?.[name] as unknown;
  return typeof method === 'function' ? ((...args: unknown[]) => (method as (...a: unknown[]) => unknown).apply(api, args)) as NonNullable<DesktopBridge[K]> : null;
}
const pageSize = 100;
const root = document.querySelector<HTMLDivElement>('#app')!;
let renderedLocation = '';

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
  if (state.busy || uploadLocked()) return;
  invalidateUploadPlan(); state.transfer = null;
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
    const parsed = parseError(error);
    state.error = parsed ? `${parsed.code}: ${describe(error, parsed.message)}` : error instanceof Error ? error.message : String(error);
    state.preview = null; state.selected = ''; state.path = '';
  } finally { state.busy = false; render(); void loadCurrentProject(); }
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
  if (view === 'preview' || view === 'activity' || view === 'history') void loadProjects();
  if (view === 'activity' || view === 'history') void loadHistory(view === 'history' ? state.historyProject : '');
  if (view === 'settings') void loadSettings();
  if (view === 'policies') void loadCurrentProject();
  if (view === 'sync') { void loadSyncs(); void loadSyncActivity(); }
}
async function driveAction(action: DriveAction, confirmed = false, expectedAccountReference = '') {
  if (action === 'revoke' && (!confirmed || !expectedAccountReference)) return;
  if (state.driveBusy || state.driveCancelling || uploadLocked() || state.drive?.state === 'connecting' && action !== 'status') return;
  if (action === 'connect') state.driveCancelIntent = false;
  state.driveBusy = action; state.driveError = ''; render();
  try {
    const methods = { status: 'GoogleDriveStatus', connect: 'ConnectGoogleDrive', check: 'CheckGoogleDrive', disconnect: 'DisconnectGoogleDrive' } as const;
    const result = action === 'revoke' ? await bridge().RevokeGoogleDrive(expectedAccountReference, true) : await bridge()[methods[action]]();
    setDriveStatus(result); state.driveError = '';
  } catch {
    // Transport exceptions are not a safe display surface for credentials or provider responses.
    const messages: Record<DriveAction, string> = {
      status: 'Could not read the Google Drive connection. Open the current LedgeSync desktop application and try again.',
      connect: 'Google Drive authorization could not be completed. Try connecting again from the desktop application.',
      check: 'Could not check the Google Drive connection. Check your network connection and try again.',
      disconnect: 'Could not remove the local Google Drive credentials. Check that your system credential vault is available and try again.',
      revoke: 'Could not confirm revocation and local cleanup. Google may already have removed access. Review the connection status before retrying.',
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
    if (state.drive?.state === 'connected' && !state.destination) void loadDriveDestination();
  }
}
function canRevokeDrive() {
  return Boolean(state.drive?.account && state.drive.clientConfigured &&
    (state.drive.state === 'connected' || state.drive.state === 'reconnect_required') &&
    !state.driveBusy && !state.driveCancelling && !uploadLocked());
}
function confirmDriveRevocation() {
  if (!canRevokeDrive() || document.getElementById('drive-revoke-dialog')) return;
  const reference = state.drive!.account!.reference;
  const revision = state.driveRevision;
  const dialog = el('dialog', 'revoke-dialog'); dialog.id = 'drive-revoke-dialog';
  dialog.setAttribute('aria-labelledby', 'drive-revoke-title');
  dialog.setAttribute('aria-describedby', 'drive-revoke-impact drive-revoke-alternative');
  const title = el('h2', '', 'Revoke access on Google?'); title.id = 'drive-revoke-title';
  const account = el('p', 'revoke-account', displayPath(state.drive!.account!.email || state.drive!.account!.displayName || 'This Google account'));
  const impact = el('p', '', 'This removes this account’s authorization on Google. Other apps whose OAuth clients share the same Google Cloud project may also lose this account’s authorization. Those apps may need you to sign in again.');
  impact.id = 'drive-revoke-impact';
  const alternative = el('p', '', 'To remove credentials only from this computer, cancel and choose “Disconnect from this device”. Revocation does not delete Drive files.');
  alternative.id = 'drive-revoke-alternative';
  const close = () => {
    // Remove the closed dialog synchronously before a fast backend result can
    // rerender the account. No stale account text or controls remain in the DOM.
    dialog.close(); dialog.remove();
    const trigger = document.getElementById('drive-revoke') as HTMLButtonElement | null;
    if (trigger && !trigger.disabled) trigger.focus();
  };
  const cancel = button('Cancel', close, 'button subtle'); cancel.autofocus = true;
  const confirm = button('Revoke access on Google', () => {
    // Confirmation applies only to the account/status the user reviewed.
    const unchanged = revision === state.driveRevision && reference === state.drive?.account?.reference;
    close();
    if (unchanged && canRevokeDrive()) void driveAction('revoke', true, reference);
  }, 'button danger');
  const actions = el('div', 'revoke-actions'); actions.append(cancel, confirm);
  dialog.append(title, account, impact, alternative, actions);
  dialog.addEventListener('cancel', event => { event.preventDefault(); close(); });
  document.body.append(dialog); dialog.showModal(); cancel.focus();
}
function setDriveStatus(status: DriveConnectionStatus | null) {
  if (status?.state === 'disconnected' || status?.state === 'connected' && state.drive?.account?.reference !== status.account?.reference) state.transfer = null;
  if (status?.state !== 'connected' || state.drive?.account?.reference !== status.account?.reference) {
    invalidateUploadPlan(); state.destination = null;
  }
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

function activeTransfer() { return Boolean(state.unconfirmedUploadDigest) || state.transfer?.state === 'uploading' || state.transfer?.state === 'verifying' || state.transfer?.state === 'planning'; }
function uploadLocked() { return Boolean(state.uploadBusy || activeTransfer()); }
function uploadAvailable() {
  return state.drive?.state === 'connected' && Boolean(state.drive.account) && !state.driveBusy && !state.driveCancelling && !state.busy;
}
function invalidateUploadPlan() {
  state.uploadPlan = null; state.uploadRevision++; state.uploadPage = 0; state.uploadError = '';
}
function matchingDestination(destination: DriveDestination) {
  return state.drive?.state === 'connected' && destination.accountReference === state.drive.account?.reference;
}
async function loadDriveDestination() {
  const api = window.go?.desktop?.App;
  if (typeof api?.CurrentDriveDestination !== 'function') return;
  const revision = state.uploadRevision;
  try {
    const destination = await api.CurrentDriveDestination();
    if (revision !== state.uploadRevision || uploadLocked()) return;
    state.destination = destination && matchingDestination(destination) ? destination : null;
    render();
  } catch {
    if (revision !== state.uploadRevision) return;
    state.uploadError = 'Could not read the saved destination. Choose a destination again before preparing an upload.'; render();
  }
}
async function chooseDestination(method: 'ChooseDriveDestination' | 'UseMyDrive') {
  if (!uploadAvailable() || uploadLocked()) return;
  // Even a canceled destination dialog requires a new preview before mutation.
  invalidateUploadPlan(); state.uploadBusy = 'destination'; state.destinationCancelIntent = false; render();
  const revision = state.uploadRevision;
  try {
    const destination = await bridge()[method]();
    if (revision !== state.uploadRevision) return;
    if (destination && !matchingDestination(destination)) throw new Error('Account changed');
    if (destination) { state.destination = destination; state.transfer = null; }
  } catch (error) {
    state.uploadError = state.destinationCancelIntent ? '' : describe(error, 'Could not select this Drive destination. Check your connection and authorize the folder in the browser, then try again.');
    if (!await reconcileDriveStatus()) state.uploadError = 'The saved connection status could not be read. Open Connections before choosing a destination again.';
  } finally { state.uploadBusy = ''; state.destinationCancelIntent = false; render(); void loadCurrentProject(); }
}
async function cancelDestination() {
  if (state.uploadBusy !== 'destination' || state.destinationCancelling) return;
  state.destinationCancelIntent = true; state.destinationCancelling = true; render();
  try { await bridge().CancelGoogleDrive(); }
  catch { state.destinationCancelIntent = false; state.uploadError = 'Could not cancel folder selection. Close the browser page; the pending request will time out.'; }
  finally { state.destinationCancelling = false; render(); }
}
function validUploadPlan(plan: DriveUploadPlan) {
  return Boolean(plan.planDigest && state.preview && state.destination && matchingDestination(state.destination) &&
    plan.accountReference === state.destination.accountReference && plan.destinationId === state.destination.id &&
    Number.isFinite(Date.parse(plan.expiresAt)) && Date.parse(plan.expiresAt) > Date.now());
}
async function previewDriveUpload() {
  if (!state.preview || !state.destination || !uploadAvailable() || uploadLocked()) return;
  invalidateUploadPlan(); state.transfer = null; state.uploadBusy = 'preview'; render();
  const revision = state.uploadRevision;
  try {
    const plan = await bridge().PreviewDriveUpload();
    if (revision !== state.uploadRevision) return;
    if (!validUploadPlan(plan)) throw new Error('Stale plan');
    state.uploadPlan = plan; state.view = 'preview'; state.query = ''; state.page = 0;
  } catch (error) {
    state.uploadPlan = null;
    state.uploadError = `${describe(error, 'Could not prepare a current upload plan. Check the selected folder, Drive destination and connection, then preview again.')} Nothing has been approved.`;
    await reconcileDriveStatus();
  } finally { state.uploadBusy = ''; render(); }
}
function applyTransferStatus(status: DriveTransferStatus, expectedDigest?: string) {
  if (state.unconfirmedUploadDigest && status.state === 'idle') { state.unconfirmedUploadDigest = ''; state.transfer = null; state.transferRevision++; return true; }
  if (expectedDigest && status.planDigest !== expectedDigest) return false;
  state.transferRevision++;
  state.unconfirmedUploadDigest = '';
  state.transfer = status;
  if (['succeeded', 'partial', 'failed', 'cancelled', 'needs_review'].includes(status.state)) { state.uploadPlan = null; void loadProjects(); }
  return true;
}
function scheduleTransferPoll() {
  window.clearTimeout(state.transferPoll);
  if (activeTransfer()) state.transferPoll = window.setTimeout(() => void pollDriveTransfer(), 1000);
}
async function pollDriveTransfer() {
  const digest = state.unconfirmedUploadDigest || state.transfer?.planDigest;
  const revision = state.transferRevision;
  try {
    const status = await bridge().DriveTransferStatus();
    if (revision !== state.transferRevision) return;
    if (!applyTransferStatus(status, digest)) throw new Error('Transfer changed');
    state.uploadError = '';
    if (status.state === 'needs_review' || status.state === 'failed') await reconcileDriveStatus();
  } catch {
    state.uploadError = 'Could not read transfer progress. Completion is unconfirmed; do not start another upload. Check your connection or cancel the transfer.';
  } finally { render(); scheduleTransferPoll(); }
}
async function startDriveUpload() {
  if (!state.uploadPlan || !uploadAvailable() || uploadLocked()) return;
  const plan = state.uploadPlan;
  if (!validUploadPlan(plan)) { invalidateUploadPlan(); state.uploadError = 'This plan has expired or its destination changed. Preview the folder again before uploading.'; render(); return; }
  state.uploadBusy = 'start'; state.uploadError = ''; render();
  try {
    const status = await bridge().StartDriveUpload(plan.planDigest);
    if (!applyTransferStatus(status, plan.planDigest)) throw new Error('Transfer changed');
  } catch (error) {
    state.uploadPlan = null;
    const startProblem = parseError(error);
    // A rejected Wails call can discard a status DTO. Only this exact digest
    // may reconcile the attempt; an older successful run is not evidence.
    let reconciled = false;
    try { reconciled = applyTransferStatus(await bridge().DriveTransferStatus(), plan.planDigest); }
    catch { state.unconfirmedUploadDigest = plan.planDigest; }
    if (!reconciled) state.transfer = null;
    state.uploadError = 'The approved upload could not be confirmed. Review the transfer status and preview again before retrying; existing Drive files are never overwritten.' + (startProblem ? ` ${describe(error, '')}` : '');
    await reconcileDriveStatus();
  } finally { state.uploadBusy = ''; render(); scheduleTransferPoll(); }
}
async function cancelDriveUpload() {
  if (!activeTransfer() || state.uploadBusy) return;
  state.uploadBusy = 'cancel'; state.uploadError = ''; render();
  try {
    await bridge().CancelDriveUpload();
    const revision = state.transferRevision;
    const status = await bridge().DriveTransferStatus();
    if (revision !== state.transferRevision) return;
    if (!applyTransferStatus(status, state.unconfirmedUploadDigest || state.transfer?.planDigest)) throw new Error('Transfer changed');
  } catch {
    state.uploadError = 'Cancellation is not confirmed. The transfer may still be running. Check its status before starting another upload.';
  } finally { state.uploadBusy = ''; render(); scheduleTransferPoll(); }
}
async function openUploadedDriveFolder() {
  if (state.transfer?.state !== 'succeeded' && state.transfer?.state !== 'partial') return;
  try { await bridge().OpenUploadedDriveFolder(); }
  catch { state.uploadError = 'Could not open the verified destination in your browser. Your completed upload is unchanged.'; render(); }
}
function uploadButton(label: string, action: () => void, id: string, primary = false) {
  const control = button(label, action, `button ${primary ? 'primary' : 'subtle'}`);
  control.id = `upload-${id}`; control.disabled = !uploadAvailable() || uploadLocked(); return control;
}
function renderDriveDestination(container: HTMLElement) {
  const section = el('section', 'drive-destination'); section.setAttribute('aria-labelledby', 'upload-heading');
  const title = el('h2', '', 'Upload this folder to Google Drive'); title.id = 'upload-heading'; title.tabIndex = -1;
  section.append(title);
  if (state.drive?.state !== 'connected') {
    section.append(el('p', '', 'This is a local preview. Connect Google Drive, then choose where to upload the complete folder.'));
    section.append(button('Open Connections', () => changeView('connections'), 'button subtle', 'cloud'));
  } else {
    const destination = state.destination;
    const target = el('p', 'destination-name');
    target.append(el('span', '', 'Destination: '), el('strong', '', destination ? displayPath(destination.name) : 'Not selected'));
    section.append(target);
    section.append(el('p', 'muted', destination ? `Your folder structure is preserved inside a LedgeSync-managed folder in ${displayPath(destination.name)}. Later uploads copy new files, verify existing copies and keep both versions of changed files.` : 'Choose My Drive or select an existing folder in your browser. Choosing a folder does not upload anything.'));
    if (state.currentProject) section.append(el('p', 'pair-note', `Saved sync pair: ${displayPath(state.currentProject.name)}`));
    const actions = el('div', 'upload-actions');
    actions.append(uploadButton('Choose existing Drive folder', () => void chooseDestination('ChooseDriveDestination'), 'destination'), uploadButton('Use My Drive', () => void chooseDestination('UseMyDrive'), 'root'));
    const preview = uploadButton('Preview folder upload', () => void previewDriveUpload(), 'preview', true);
    preview.disabled ||= !state.preview || !destination;
    actions.append(preview); section.append(actions);
    if (state.uploadBusy === 'destination' || state.uploadBusy === 'preview') {
      const pending = el('p', 'upload-pending', state.uploadBusy === 'destination' ? 'Complete the folder selection in your browser, then return here.' : 'Reading source files and checking the Drive destination…');
      pending.setAttribute('role', 'status'); section.append(pending);
      if (state.uploadBusy === 'destination') {
        const cancel = button(state.destinationCancelling ? 'Cancelling selection…' : 'Cancel folder selection', () => void cancelDestination(), 'button subtle'); cancel.id = 'upload-cancel-destination'; cancel.disabled = state.destinationCancelling; section.append(cancel);
      }
    }
  }
  if (state.uploadError) { const error = el('div', 'error', state.uploadError); error.setAttribute('role', 'alert'); section.append(error); }
  container.append(section);
  renderDriveTransfer(container);
}
function renderDriveTransfer(container: HTMLElement) {
  const transfer = state.transfer;
  if (state.unconfirmedUploadDigest) {
    const pending = el('section', 'transfer-card'); pending.setAttribute('aria-label', 'Folder transfer');
    const status = el('p', '', 'Upload status is unavailable. The approved request may still be running. Checking its status before allowing another upload…'); status.setAttribute('role', 'status'); pending.append(status);
    const cancel = button(state.uploadBusy === 'cancel' ? 'Cancelling upload…' : 'Cancel upload', () => void cancelDriveUpload(), 'button subtle'); cancel.id = 'upload-cancel'; cancel.disabled = Boolean(state.uploadBusy); pending.append(cancel); container.append(pending); return;
  }
  if (!transfer || transfer.state === 'idle' || transfer.state === 'awaiting_approval') return;
  renderTransferCard(container, transfer, activeTransfer(), {
    cancel: () => void cancelDriveUpload(), cancelLabel: state.uploadBusy === 'cancel' ? 'Cancelling upload…' : 'Cancel upload', cancelDisabled: Boolean(state.uploadBusy),
    open: () => void openUploadedDriveFolder(),
  });
}
const transferLabels: Record<TransferState, string> = { idle: 'No transfer', planning: 'Preparing upload', awaiting_approval: 'Waiting for approval', uploading: 'Uploading folder', verifying: 'Verifying uploaded files', succeeded: 'Folder upload verified', partial: 'Upload verified with skipped files', failed: 'Upload failed', cancelled: 'Upload cancelled', needs_review: 'Upload needs review' };
function renderTransferCard(container: HTMLElement, transfer: DriveTransferStatus, active: boolean, actions: { cancel?: () => void; cancelLabel?: string; cancelDisabled?: boolean; open?: () => void; label?: string }) {
  const card = el('section', `transfer-card transfer-${transfer.state}`); card.setAttribute('aria-label', actions.label ?? 'Folder transfer');
  const status = el('div', 'transfer-announcement'); status.setAttribute('role', 'status'); status.setAttribute('aria-live', 'polite');
  status.append(el('h2', '', transferLabels[transfer.state] ?? transfer.state), el('p', '', displayPath(transfer.message))); card.append(status);
  if (transfer.errorCode && ['failed', 'needs_review'].includes(transfer.state)) { const hint = guidanceFor(transfer.errorCode); if (hint) card.append(el('p', 'transfer-hint', hint)); }
  const progress = el('progress'); progress.max = Math.max(transfer.totalBytes, 1); progress.value = Math.min(Math.max(transfer.uploadedBytes, 0), progress.max); progress.setAttribute('aria-label', 'Verified content bytes'); card.append(progress);
  card.append(el('p', 'transfer-counts', `${transfer.completedFiles} of ${transfer.totalFiles} files verified · ${bytes(transfer.uploadedBytes)} of ${bytes(transfer.totalBytes)} verified`));
  if (active && (transfer.transferBytes ?? 0) > 0) card.append(el('p', 'transfer-sent', `Sending ${bytes(Math.min(transfer.sentBytes ?? 0, transfer.transferBytes ?? 0))} of ${bytes(transfer.transferBytes ?? 0)} to Google Drive`));
  card.append(el('p', 'muted', 'Verified totals include unchanged files already on Drive. They are not uploaded again.'));
  if (transfer.currentPath) card.append(el('p', 'transfer-path', displayPath(transfer.currentPath)));
  const issues = transfer.issues ?? [];
  if (issues.length) {
    const list = el('details', 'transfer-issues'); list.open = transfer.state === 'partial';
    list.append(el('summary', '', `${transfer.skippedFiles ?? issues.length} item${(transfer.skippedFiles ?? issues.length) === 1 ? '' : 's'} not copied in this run`));
    const ul = el('ul'); for (const issue of issues.slice(0, 100)) { const li = el('li'); li.append(el('code', '', displayPath(issue.path)), el('span', '', ` — ${issue.message}`)); ul.append(li); } list.append(ul); card.append(list);
  }
  if ((transfer.pausedFiles ?? 0) > 0) card.append(el('p', 'muted', `${transfer.pausedFiles} changed file(s) were paused by the conflict policy and not copied.`));
  if (active) {
    if (actions.cancel) { const cancel = button(actions.cancelLabel ?? 'Cancel upload', actions.cancel, 'button subtle'); cancel.id = 'upload-cancel'; cancel.disabled = Boolean(actions.cancelDisabled); card.append(cancel); }
    card.append(el('p', 'muted', 'Keep LedgeSync open until the transfer finishes. Cancellation leaves any files already created on Drive; it does not delete them.'));
  } else if (transfer.state !== 'succeeded') {
    card.append(el('p', 'muted', 'Files already created on Drive are preserved. Create a fresh preview to reconcile existing copies and continue safely.'));
  }
  if ((transfer.state === 'succeeded' || transfer.state === 'partial') && transfer.remoteFolderId && /^[A-Za-z0-9_-]{1,200}$/.test(transfer.remoteFolderId) && actions.open) {
    const open = button('Open destination folder on Google Drive', actions.open, 'button subtle'); open.id = 'upload-open'; card.append(open);
  }
  container.append(card);
}
function renderDriveUploadPlan(container: HTMLElement) {
  const plan = state.uploadPlan!;
  const approval = el('section', 'upload-approval'); approval.setAttribute('aria-label', 'Review folder upload');
  const rootAction = plan.entries.find(entry => entry.relativePath === '' || entry.relativePath === plan.sourceName)?.action;
  approval.append(el('h2', '', 'Review and upload'), el('p', '', rootAction === 'skip' ? `Update the approved copy of ${displayPath(plan.sourceName)} in ${displayPath(plan.destinationName)}. Verified copies are reused; changed files keep both versions.` : `Copy ${displayPath(plan.sourceName)} into a LedgeSync-managed folder in ${displayPath(plan.destinationName)}.`));
  const stats = el('div', 'stats');
  const counters: [string, string][] = [[String(plan.fileCount), 'files in plan'], [String(plan.folderCount), 'folders in plan'], [bytes(plan.totalBytes), 'content to verify'], [String(plan.excludedCount), 'excluded items']];
  if (plan.transferBytes !== undefined) counters.push([bytes(plan.transferBytes), 'to upload now']);
  for (const [value, label] of counters) {
    const stat = el('div'); stat.append(el('strong', '', value), el('span', '', label)); stats.append(stat);
  }
  approval.append(stats, el('p', 'upload-policy', 'Folder structure and included empty folders are preserved. Active ignore rules still apply; excluded items are skipped. Verified copies are reused. Changed files get a .ledgesync- suffix with a stable identifier; both versions remain. No existing file is overwritten or deleted.'));
  const summary = [[plan.newFiles, 'new'], [plan.changedFiles, 'changed'], [plan.unchangedFiles, 'unchanged'], [plan.recreatedItems, 'copied again'], [plan.pausedFiles, 'paused'], [plan.unsupportedCount, 'links not copied']].filter(([count]) => typeof count === 'number' && count > 0).map(([count, label]) => `${count} ${label}`);
  if (summary.length) approval.append(el('p', 'plan-summary', `Files: ${summary.join(' · ')}`));
  for (const warning of plan.warnings ?? []) approval.append(el('p', 'preserved-note', displayPath(warning)));
  const identity = el('dl', 'details upload-identity');
  field(identity, 'Google account', displayPath(state.drive?.account?.email || plan.accountReference)); field(identity, 'Destination ID', displayPath(plan.destinationId)); field(identity, 'Plan expires', new Date(plan.expiresAt).toLocaleString()); approval.append(identity);
  const submit = uploadButton(state.uploadBusy === 'start' ? 'Starting upload…' : 'Upload folder', () => void startDriveUpload(), 'start', true); submit.disabled ||= !validUploadPlan(plan); approval.append(submit);
  const entries = plan.entries.filter(entry => !state.query || entry.relativePath.toLocaleLowerCase().includes(state.query.toLocaleLowerCase()));
  const list = el('div', 'operations upload-entries'); list.setAttribute('aria-label', 'Approved folder contents');
  for (const entry of entries.slice(state.uploadPage * pageSize, (state.uploadPage + 1) * pageSize)) {
    const actionLabels: Record<string, string> = { skip: 'Verify existing copy', resume: 'Resume reserved copy', 'keep-both': 'Keep both versions', create: 'Create folder', upload: 'Copy new file', recreate: 'Copy again (earlier copy missing)', paused: 'Paused: changed file', unsupported: 'Not copied: link' };
    const row = el('div', `operation action-${entry.action || 'upload'}`); row.append(el('span', 'operation-path', displayPath(entry.relativePath || plan.sourceName)), el('span', 'operation-type', actionLabels[entry.action || ''] || (entry.kind === 'directory' || entry.kind === 'folder' ? 'Create folder' : 'Copy new file')), el('span', 'muted', entry.kind === 'directory' || entry.kind === 'folder' ? '—' : bytes(entry.size)));
    if (entry.note) { row.title = entry.note; row.append(el('span', 'operation-note', displayPath(entry.note))); }
    list.append(row);
  }
  approval.append(list);
  if (entries.length > pageSize) {
    const pagination = el('div', 'pagination'); pagination.append(el('span', 'muted', `${entries.length} items · page ${state.uploadPage + 1} of ${Math.ceil(entries.length / pageSize)}`));
    const previous = button('Previous contents', () => { state.uploadPage--; render(); }, 'button subtle'); previous.disabled = state.uploadPage === 0;
    const next = button('Next contents', () => { state.uploadPage++; render(); }, 'button subtle'); next.disabled = (state.uploadPage + 1) * pageSize >= entries.length; pagination.append(previous, next); approval.append(pagination);
  }
  const detail = el('details', 'plan-details'); detail.append(el('summary', '', 'Approved plan identity'), el('p', 'transfer-path', plan.planDigest)); approval.append(detail);
  container.append(approval);
  renderAutomationOffer(container, plan);
}

function render() {
  const location = `${state.view}:${state.preview?.sourceRoot || ''}:${state.path}`;
  const contentScroll = renderedLocation === location ? document.querySelector('.content')?.scrollTop || 0 : 0;
  const inspectorScroll = renderedLocation === location ? document.querySelector('.inspector')?.scrollTop || 0 : 0;
  renderedLocation = location;
  const focused = document.activeElement as HTMLInputElement | null;
  const restoreSearch = focused?.id === 'search';
  const restoreDrive = focused?.id.startsWith('drive-') || focused?.id.startsWith('upload-') ? focused.id : '';
  const cursor = focused?.selectionStart ?? 0;
  root.replaceChildren();
  const shell = el('div', 'shell');
  const sidebar = el('aside', 'sidebar');
  const brand = el('div', 'brand'); brand.append(icon('check', 'brand-mark'), el('span', '', 'LedgeSync'));
  sidebar.append(brand);
  const syncing = syncAvailable();
  if (syncing) {
    const add = button('Sync a folder', () => void addSync(), 'button primary choose-folder', 'refresh');
    add.disabled = Boolean(state.syncBusy) || state.drive?.state !== 'connected'; sidebar.append(add);
  } else {
    const choose = button('Choose folder', () => void scan('OpenFolder'), 'button primary choose-folder', 'folder');
    choose.disabled = state.busy || uploadLocked(); sidebar.append(choose);
  }
  const nav = el('nav', 'nav'); nav.setAttribute('aria-label', 'Main navigation');
  const mainViews: [View, string, string][] = [['files', 'Files', 'folder'], ['preview', syncing ? 'One-time copies' : 'Sync pairs', 'arrow'], ['policies', 'Policies', 'shield'], ['connections', 'Connections', 'cloud']];
  if (syncing) mainViews.unshift(['sync', 'Sync', 'refresh']);
  for (const [view, title, symbol] of mainViews) {
    const item = button(title, () => changeView(view), `nav-item ${state.view === view ? 'active' : ''}`, symbol);
    if (state.view === view) item.setAttribute('aria-current', 'page');
    nav.append(item);
  }
  for (const [view, title, symbol] of [['activity', 'Activity', 'activity'], ['history', 'History & Recovery', 'history'], ['settings', 'Settings', 'settings']] as const) {
    const item = button(title, () => changeView(view), `nav-item ${state.view === view ? 'active' : ''}`, symbol);
    if (state.view === view) item.setAttribute('aria-current', 'page');
    nav.append(item);
  }
  sidebar.append(nav);
  const automaticCount = state.projects.filter(p => p.automation.enabled && !p.automation.paused).length;
  if (state.automation?.available && (automaticCount || state.automation.running)) {
    const auto = el('p', 'sidebar-automation', state.automation.paused ? 'Automatic copies paused' : state.automation.running ? 'Automatic copy running…' : `Automatic copies on for ${automaticCount} pair${automaticCount === 1 ? '' : 's'}`);
    auto.setAttribute('role', 'status'); sidebar.append(auto);
  }
  const offline = el('div', 'sidebar-note'); offline.append(icon('shield'), el('strong', '', syncing ? syncSummary() : 'Preview before transfer'), el('p', '', `Developer alpha · ${version}`), el('p', '', state.driveBusy === 'revoke' || state.driveBusy === 'disconnect' ? 'Updating Google Drive access…' : state.drive?.state === 'revoked_local_cleanup_required' ? 'Google access revoked. Local credential cleanup is required.' : state.drive?.state === 'connected' ? 'Google Drive connected. Choose a destination and preview your folder upload.' : state.drive?.state === 'reconnect_required' ? 'Google Drive needs reconnection.' : state.drive?.state === 'client_changed' ? 'Google Drive needs a new authorization. Open Connections.' : 'Connect Google Drive in Connections.'));
  sidebar.append(offline); shell.append(sidebar);

  const workspace = el('main', 'workspace');
  const top = el('header', 'topbar');
  const search = el('label', 'search'); search.append(icon('search'));
  const input = el('input'); input.id = 'search'; input.type = 'search'; input.placeholder = 'Search this project'; input.setAttribute('aria-label', 'Search this project'); input.value = state.query;
  input.disabled = !state.preview || state.busy || ['sync', 'policies', 'connections', 'activity', 'history', 'settings'].includes(state.view);
  input.addEventListener('input', () => { state.query = input.value; state.page = 0; state.uploadPage = 0; render(); });
  search.append(input); top.append(search);
  top.append(badge(activeTransfer() ? 'Drive upload in progress' : state.destination ? 'Drive destination selected' : 'Local file preview', state.destination ? 'connected' : 'offline'));
  const loadConfig = button('Open configuration', () => void scan('OpenConfiguration'), 'button subtle'); loadConfig.disabled = state.busy || uploadLocked(); top.append(loadConfig);
  workspace.append(top);
  if (state.error) { const error = el('div', 'error', state.error); error.setAttribute('role', 'alert'); workspace.append(error); }
  if (state.busy) {
    const progress = el('div', 'progress'); progress.setAttribute('role', 'status');
    progress.append(el('span', 'spinner'), el('span', '', 'Reading local files and policy sources…'));
    progress.append(button('Cancel scan', () => { void bridge().Cancel().catch(error => { state.error = String(error); render(); }); }, 'button subtle'));
    workspace.append(progress);
  }
  const body = el('div', `body ${state.preview ? 'has-preview' : ''}`);
  const content = el('section', 'content'); content.setAttribute('aria-label', state.view === 'connections' ? 'Account connections' : ['activity', 'history', 'settings'].includes(state.view) ? 'Activity and settings' : 'File workspace');
  if (state.view === 'sync') renderSync(content);
  else if (state.view === 'connections') renderConnections(content);
  else if (state.view === 'activity') renderActivity(content);
  else if (state.view === 'history') renderHistory(content);
  else if (state.view === 'settings') renderSettings(content);
  else if (state.view === 'preview' && !state.preview) renderPairs(content);
  else if (!state.preview) renderEmpty(content);
  else {
    const heading = el('div', 'heading');
    const title = el('div'); title.append(el('p', 'eyebrow', 'LOCAL WORKSPACE'), el('h1', '', state.view === 'files' ? 'Files' : state.view === 'preview' ? 'Preview your sync pair' : 'Policy capabilities'));
    heading.append(title);
    const refresh = button('Refresh', () => void scan('Refresh'), 'button subtle', 'refresh'); refresh.disabled = state.busy || uploadLocked(); heading.append(refresh); content.append(heading);
    if (state.view === 'files') renderFiles(content);
    else if (state.view === 'preview') renderPreview(content);
    else renderPolicies(content);
  }
  body.append(content);
  if (state.preview && (state.view === 'files' || state.view === 'preview' && !state.destination)) { const inspector = el('aside', 'inspector'); inspector.id = 'inspector'; inspector.setAttribute('aria-label', 'Details and policy explanation'); renderInspector(inspector); body.append(inspector); }
  workspace.append(body); shell.append(workspace); root.append(shell);
  content.scrollTop = contentScroll;
  const inspector = document.getElementById('inspector'); if (inspector) inspector.scrollTop = inspectorScroll;
  if (restoreSearch) { const input = document.querySelector<HTMLInputElement>('#search')!; input.focus(); input.setSelectionRange(cursor, cursor); }
  else if (restoreDrive) {
    const target = document.getElementById(restoreDrive) as HTMLButtonElement | null;
    if (target && !target.disabled) target.focus();
    else document.querySelector<HTMLElement>(restoreDrive.startsWith('upload-') ? '#upload-heading' : '#drive-heading')?.focus();
  }
}

function driveButton(label: string, action: () => void, id: string, primary = false) {
  const control = button(label, action, `button ${primary ? 'primary' : 'subtle'}`);
  control.id = `drive-${id}`;
  control.disabled = Boolean(state.driveBusy || state.driveCancelling || uploadLocked());
  return control;
}
function renderConnections(container: HTMLElement) {
  container.classList.add('connections');
  const heading = el('div', 'heading');
  const title = el('div');
  const h1 = el('h1', '', 'Connections'); h1.id = 'drive-heading'; h1.tabIndex = -1;
  title.append(el('p', 'eyebrow', 'ACCOUNT ACCESS'), h1); heading.append(title); container.append(heading);
  container.append(el('p', 'section-description', 'Connect your Google account, authorize LedgeSync in your browser, and return here. Your access is saved in this computer’s credential vault.'));
  const limits = el('div', 'notice');
  limits.append(icon('info'), el('p', '', uploadLocked() ? 'Finish or cancel the folder upload in Files or Sync pairs before changing account access.' : 'In Files, choose a local folder and a Drive destination, preview the included contents, then approve the upload. Selecting a source or connecting an account does not start a transfer.'));
  container.append(limits);
  if (state.driveError) { const error = el('div', 'error', state.driveError); error.setAttribute('role', 'alert'); container.append(error); }

  const card = el('article', 'connection-card'); card.setAttribute('aria-label', 'Google Drive connection');
  const cardHeading = el('div', 'connection-heading');
  const provider = el('div', 'connection-provider'); provider.append(icon('cloud'), el('h2', '', 'Google Drive'));
  const labels: Record<DriveConnectionStatus['state'], string> = { setup_required: 'Connection unavailable', disconnected: 'Not connected', connecting: 'Waiting for authorization', connected: 'Connected', reconnect_required: 'Reconnect required', client_changed: 'New authorization required', storage_unavailable: 'Credential vault unavailable', busy: 'Connection in use', revoked_local_cleanup_required: 'Google access revoked; local cleanup required' };
  cardHeading.append(provider, badge(state.driveBusy === 'connect' ? labels.connecting : state.driveBusy === 'revoke' ? 'Revoking Google access' : state.driveBusy === 'disconnect' ? 'Removing local access' : state.drive ? labels[state.drive.state] : 'Not checked', state.drive?.state === 'connected' && !state.driveBusy ? 'connected' : ''));
  card.append(cardHeading);
  const status = el('div', 'connection-status'); status.setAttribute('role', 'status'); status.setAttribute('aria-live', 'polite'); status.setAttribute('aria-atomic', 'true');
  const pending = state.driveBusy === 'connect' || state.drive?.state === 'connecting';
  const pendingLabels: Record<DriveAction, string> = { status: 'Reading the saved connection…', connect: 'Complete authorization in your browser, then return to LedgeSync. You can cancel this request here.', check: 'Checking account authorization with Google…', disconnect: 'Removing local account credentials…', revoke: 'Revoking this account’s access on Google, then removing local credentials…' };
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
  } else if (!state.drive || state.drive.state === 'storage_unavailable' || state.drive.state === 'busy') {
    actions.append(driveButton('Retry connection status', () => void driveAction('status'), 'status'));
  } else if (state.drive.state === 'connected') {
    actions.append(driveButton('Check connection', () => void driveAction('check'), 'check'), driveButton('Disconnect from this device', () => void driveAction('disconnect'), 'disconnect'));
  } else if (state.drive.state === 'revoked_local_cleanup_required') {
    card.append(el('p', 'connection-guidance', 'Google confirmed revocation, but this computer’s credentials could not be removed. Unlock the system credential vault, then disconnect from this device to retry local cleanup. Connecting, checking and another revocation are blocked in this session. Finish local cleanup before closing LedgeSync; this warning cannot be saved while the vault is unavailable.'));
    actions.append(driveButton('Disconnect from this device', () => void driveAction('disconnect'), 'disconnect'));
  } else if (state.drive.state === 'client_changed') {
    card.append(el('p', 'connection-guidance', 'This computer has authorization from an earlier LedgeSync configuration. Disconnect it first, then connect again to authorize this version.'));
    card.append(el('p', 'connection-guidance', 'This version cannot revoke a grant from a different OAuth client. Review that earlier authorization in your Google Account connections settings if you also want to remove its access on Google.'));
    actions.append(driveButton('Disconnect from this device', () => void driveAction('disconnect'), 'disconnect'));
  } else if (state.drive.state === 'setup_required') {
    card.append(el('p', 'connection-guidance', 'Google sign-in is not configured in this build. Install the official LedgeSync application from ledgesync.com to connect your account.'));
  } else {
    if (state.drive.clientConfigured) actions.append(driveButton(state.drive.state === 'reconnect_required' ? 'Reconnect Google Drive' : 'Connect Google Drive', () => void driveAction('connect'), 'connect', true));
    if (state.drive.account) actions.append(driveButton('Disconnect from this device', () => void driveAction('disconnect'), 'disconnect'));
  }
  if (state.drive?.account && state.drive.clientConfigured && (state.drive.state === 'connected' || state.drive.state === 'reconnect_required')) {
    actions.append(driveButton('Revoke access on Google', confirmDriveRevocation, 'revoke'));
  }
  card.append(actions);
  const access = el('div', 'connection-access');
  access.append(el('h3', '', 'Access you authorize'), el('p', '', 'The drive.file permission is limited to files created by, or explicitly opened with, LedgeSync. It does not grant access to every existing file or folder in your Drive. Selecting a folder does not automatically grant access to its existing contents.'));
  access.append(el('code', 'connection-scope', state.drive?.scope || 'https://www.googleapis.com/auth/drive.file'));
  access.append(el('p', '', 'One account is supported in this build. Disconnect before connecting another account.'));
  if (state.drive?.account) access.append(el('p', 'disconnect-help', 'Disconnect from this device removes this account’s tokens only from this computer. It does not delete Drive files or revoke the Google permission grant. Revoke access on Google is a separate action that requires confirmation because other apps in the same Google Cloud project may also lose this account’s authorization.'));
  card.append(access); container.append(card);

  container.append(el('p', 'muted', 'Sign in and approve access only on Google’s page in your browser. LedgeSync never asks for your Google password or a pasted access token.'));
}

function renderEmpty(container: HTMLElement) {
  const empty = el('div', 'welcome'); empty.append(el('div', 'welcome-mark')); empty.firstElementChild!.append(icon('folder'));
  empty.append(el('p', 'eyebrow', 'MEET YOUR NEXT CLEAN SYNC'), el('h1', '', 'See what belongs.'), el('p', 'welcome-description', 'Explore a local folder, see which files your rules exclude, and review every planned copy before anything moves.'));
  const choose = button('Choose a local folder', () => void scan('OpenFolder'), 'button primary large', 'folder'); choose.disabled = state.busy || uploadLocked(); empty.append(choose);
  empty.append(el('p', 'welcome-hint', 'Starts with recursive .gitignore rules. Read-only, with no account needed.'));
  const features = el('div', 'welcome-features');
  for (const [symbol, title, description] of [['folder', 'Your files, in view', 'Browse a real local inventory.'], ['shield', 'Every rule explained', 'See why each file is included or excluded.'], ['arrow', 'Preview first', 'Choose a Drive destination and approve the folder upload.']]) {
    const card = el('div'); card.append(icon(symbol), el('strong', '', title), el('p', '', description)); features.append(card);
  }
  empty.append(features); container.append(empty);
}
function renderFiles(container: HTMLElement) {
  const preview = state.preview!;
  renderDriveDestination(container);
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
    const details = el('dl', 'details'); field(details, 'Project', preview.projectName); field(details, 'Source', preview.sourceRoot); field(details, 'Destination', state.destination ? displayPath(state.destination.name) : 'No Drive destination selected'); field(details, 'Scan', preview.plan.scanComplete.source ? 'Complete' : 'Incomplete'); container.append(details); return;
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
  renderPairList(container, true);
  renderDriveDestination(container);
  if (state.uploadPlan) { renderDriveUploadPlan(container); return; }
  if (state.destination) {
    if (!activeTransfer()) container.append(el('p', 'section-description', 'Preview folder upload to read the current local files, validate this destination and review the exact files and folders before approving.'));
    renderAutomationPanel(container);
    return;
  }
  const banner = el('div', 'notice'); banner.append(icon('info'), el('p', '', 'Local selection only. Google Drive was not contacted and no Drive destination is selected: this list shows what the current rules select. Choose a destination, then preview the folder upload to see the real plan. Nothing here can be applied.')); container.append(banner);
  const pair = el('div', 'pair'); const local = el('div'); local.append(icon('folder'), el('strong', '', preview.projectName), el('small', '', 'Local folder')); const remote = el('div'); remote.append(icon('cloud'), el('strong', '', 'No Drive destination selected'), el('small', '', 'Drive not contacted · nothing uploaded')); pair.append(local, icon('arrow'), remote); container.append(pair);
  const stats = el('div', 'stats');
  for (const [value, label] of [[String(plan.summary.operationCount), 'local selection items'], [bytes(plan.summary.uploadBytes), 'selected file size'], [String(preview.entries.filter(e => e.decision === 'exclude').length), 'excluded items'], [String(plan.summary.trashCount), 'deletions']]) { const stat = el('div'); stat.append(el('strong', '', value), el('span', '', label)); stats.append(stat); }
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
  const digest = el('details', 'plan-details'); digest.append(el('summary', '', 'Selection identity and limitations'));
  const details = el('dl', 'details'); field(details, 'Selection digest', plan.planDigest); field(details, 'Rules digest', plan.rulesDigest); digest.append(details);
  container.append(digest);
}
function renderPolicies(container: HTMLElement) {
  const preview = state.preview!;
  container.append(el('p', 'section-description', 'Capabilities are reported by the same engine that scans your files. Unsupported adapters block a configuration that requires them.'));
  renderPolicyEditor(container);
  const source = el('div', 'notice'); source.append(icon('shield'), el('p', '', 'Choose folder uses optional recursive .gitignore with conservative composition. Edit a saved sync pair’s policy above, or open a configuration file, to use custom filenames, multiple sources or rclone filter profiles.')); container.append(source);
  if (preview.pruned?.length) container.append(el('p', 'muted', `${preview.pruned.length} excluded folder${preview.pruned.length === 1 ? ' was' : 's were'} not read because the ignore rules exclude everything inside them.`));
  const list = el('div', 'capabilities');
  for (const capability of preview.capabilities) {
    const card = el('article', 'capability'); const title = el('div', 'capability-title'); title.append(el('h3', '', capability.dialect), badge(capability.supported ? 'Available profile' : 'Not implemented', capability.supported ? 'offline' : '')); card.append(title);
    card.append(el('p', 'muted', `${capability.adapter} · ${capability.mechanism} · ${capability.profileVersion}`));
    for (const limitation of capability.limitations ?? []) card.append(el('p', 'capability-limit', limitation)); list.append(card);
  }
  container.append(list);
}

// ---- Saved sync pairs ------------------------------------------------------

async function loadProjects() {
  const list = optional('ListProjects');
  if (!list) return;
  try { state.projects = await list() ?? []; state.projectError = ''; }
  catch (error) { state.projectError = describe(error, 'Saved sync pairs could not be read.'); }
  finally { state.projectsLoaded = true; render(); }
  void loadAutomation();
}
async function loadCurrentProject() {
  const current = optional('CurrentProject');
  if (!current) return;
  try {
    const project = await current();
    const changed = project?.id !== state.currentProject?.id || project?.updatedAt !== state.currentProject?.updatedAt;
    state.currentProject = project ?? null;
    if (changed) state.policyDraft = null;
    render();
  } catch { /* Saved pairs are optional; the open folder remains usable. */ }
}
async function loadAutomation() {
  const status = optional('AutomationStatus');
  if (!status) return;
  window.clearTimeout(state.automationPoll);
  try { state.automation = await status(); } catch { state.automation = null; }
  render();
  if (state.automation?.running || state.projects.some(p => p.automation.enabled && !p.automation.paused)) state.automationPoll = window.setTimeout(() => void loadAutomation(), state.automation?.running ? 1500 : 15000);
}
async function projectAction(action: () => Promise<unknown>, fallback: string) {
  if (state.projectBusy) return;
  state.projectBusy = true; state.projectError = ''; render();
  try { await action(); }
  catch (error) { state.projectError = describe(error, fallback); }
  finally { state.projectBusy = false; await loadProjects(); await loadCurrentProject(); }
}
async function openProject(project: Project) {
  const open = optional('OpenProject');
  if (!open || state.busy || uploadLocked()) return;
  invalidateUploadPlan(); state.transfer = null; state.busy = true; state.error = ''; render();
  try {
    const session = await open(project.id);
    state.preview = session.preview; state.path = ''; state.history = ['']; state.historyIndex = 0; state.selected = ''; state.query = ''; state.page = 0;
    state.currentProject = session.project; state.policyDraft = null;
    state.destination = session.destination && matchingDestination(session.destination) ? session.destination : null;
    state.uploadError = session.destinationError ? `${session.destinationError.message} ${guidanceFor(session.destinationError.code)}`.trim() : '';
    state.view = 'preview';
  } catch (error) {
    state.error = describe(error, 'This sync pair could not be opened.');
  } finally { state.busy = false; render(); }
}
function pairState(project: Project) {
  const run = project.lastRun;
  if (!run) return 'Not copied yet';
  const when = date(run.finishedAt);
  return `${transferLabels[run.state as TransferState] ?? run.state} · ${when}${run.trigger === 'automatic' ? ' · automatic' : ''}`;
}
function automationLabel(project: Project) {
  const a = project.automation;
  if (!a.enabled) return 'Automatic copies off';
  if (a.paused) return 'Automatic copies paused';
  if (a.waiting) return 'Automatic copies waiting';
  return a.trigger === 'watch' ? `Checks for changes every ${minutes(a.intervalSeconds)}` : `Copies every ${minutes(a.intervalSeconds)}`;
}
function minutes(seconds: number) { return seconds % 3600 === 0 ? `${seconds / 3600} h` : `${Math.round(seconds / 60)} min`; }
function renderPairList(container: HTMLElement, compact = false) {
  if (!optional('ListProjects')) return;
  const section = el('section', 'pairs'); section.setAttribute('aria-labelledby', 'pairs-heading');
  const heading = el('h2', '', 'Saved sync pairs'); heading.id = 'pairs-heading'; section.append(heading);
  if (state.projectError) { const error = el('div', 'error', state.projectError); error.setAttribute('role', 'alert'); section.append(error); }
  if (!state.projects.length) {
    section.append(el('p', 'muted', state.projectsLoaded ? 'No saved pairs yet. Choose a local folder and a Drive destination; an approved upload saves the pair here so you can reopen it later.' : 'Loading saved pairs…'));
    container.append(section); return;
  }
  const list = el('div', 'pair-list'); list.setAttribute('role', 'list');
  for (const project of compact ? state.projects.slice(0, 6) : state.projects) {
    const card = el('article', `pair-card ${state.currentProject?.id === project.id ? 'is-open' : ''}`); card.setAttribute('role', 'listitem'); card.setAttribute('aria-label', `Sync pair ${project.name}`);
    const title = el('div', 'pair-title'); title.append(icon('folder'), el('strong', '', displayPath(project.name)), icon('arrow'), icon('cloud'), el('span', '', displayPath(project.destination.name)));
    card.append(title, el('p', 'muted pair-source', displayPath(project.sourceRoot)), el('p', 'pair-state', pairState(project)), el('p', `pair-automation ${project.automation.paused ? 'paused' : ''}`, automationLabel(project)));
    if (project.automation.paused && project.automation.pauseReason) card.append(el('p', 'pair-pause', displayPath(project.automation.pauseReason)));
    const actions = el('div', 'pair-actions');
    const open = button(state.currentProject?.id === project.id ? 'Reopen' : 'Open', () => void openProject(project), 'button subtle'); open.disabled = state.busy || uploadLocked() || state.projectBusy; actions.append(open);
    if (!compact) {
      const rename = button('Rename', () => {
        const name = window.prompt('Name this sync pair', project.name); const fn = optional('RenameProject');
        if (name && name.trim() && fn) void projectAction(() => fn(project.id, name.trim()), 'The sync pair could not be renamed.');
      }, 'button subtle'); rename.disabled = state.projectBusy; actions.append(rename);
      const forget = button('Forget on this computer', () => {
        const fn = optional('ForgetProject');
        if (fn && window.confirm(`Forget “${project.name}” on this computer? Drive copies, the local folder and LedgeSync’s transfer journal are not changed.`)) void projectAction(() => fn(project.id), 'The sync pair could not be removed.');
      }, 'button subtle'); forget.disabled = state.projectBusy || state.automation?.activeProjectId === project.id; actions.append(forget);
    }
    card.append(actions); list.append(card);
  }
  section.append(list);
  if (compact && state.projects.length > 6) section.append(el('p', 'muted', `${state.projects.length - 6} more in History & Recovery.`));
  container.append(section);
}
function renderPairs(container: HTMLElement) {
  const heading = el('div', 'heading'); const title = el('div'); title.append(el('p', 'eyebrow', 'SYNC PAIRS'), el('h1', '', 'Sync pairs')); heading.append(title); container.append(heading);
  container.append(el('p', 'section-description', 'A sync pair is a local folder copied into a LedgeSync-managed folder in Google Drive. Open a pair to preview new and changed files, or choose a new local folder.'));
  renderPairList(container);
}

// ---- Automatic copies ------------------------------------------------------

function renderAutomationPanel(container: HTMLElement) {
  const project = state.currentProject;
  if (!project || !optional('AuthorizeAutomation') || !state.automation?.available) return;
  if (project.destination.id !== state.destination?.id) return;
  const a = project.automation;
  const section = el('section', 'automation'); section.setAttribute('aria-labelledby', 'automation-heading');
  const heading = el('h2', '', 'Automatic copies'); heading.id = 'automation-heading'; section.append(heading);
  section.append(el('p', 'section-description', automationLabel(project)));
  if (a.enabled && a.authorization) section.append(el('p', 'muted', `Authorized ${date(a.authorization.approvedAt)} for this account, destination, folder, configuration and ignore rules. Any change pauses automatic copies until you review a new preview. Runs only while LedgeSync is open.`));
  if (a.waiting) section.append(el('p', 'automation-waiting', displayPath(a.waiting)));
  if (a.paused) { const pause = el('div', 'error', `${displayPath(a.pauseReason ?? 'Paused.')} ${guidanceFor(a.pauseCode ?? '')}`.trim()); pause.setAttribute('role', 'alert'); section.append(pause); }
  const actions = el('div', 'upload-actions');
  if (a.enabled) {
    const check = button('Check now', () => { const fn = optional('CheckProjectNow'); if (fn) void projectAction(() => fn(project.id), 'The automatic check could not start.'); }, 'button subtle'); check.disabled = a.paused || state.projectBusy;
    const resume = button('Resume', () => { const fn = optional('ResumeAutomation'); if (fn) void projectAction(() => fn(project.id), 'Automatic copies could not resume.'); }, 'button subtle'); resume.disabled = !a.paused || state.projectBusy || a.pauseCode === 'AUTOMATION_REVIEW_REQUIRED';
    const disable = button('Turn off automatic copies', () => { const fn = optional('DisableAutomation'); if (fn) void projectAction(() => fn(project.id), 'Automatic copies could not be turned off.'); }, 'button subtle'); disable.disabled = state.projectBusy;
    actions.append(check, resume, disable);
  } else {
    actions.append(el('p', 'muted', 'Preview this folder upload, then enable automatic copies from the review.'));
  }
  section.append(actions);
  if (state.automation?.running && state.automation.activeProjectId === project.id && state.automation.transfer) {
    renderTransferCard(section, state.automation.transfer, true, { label: 'Automatic copy', cancel: () => { const fn = optional('CancelAutomaticCopy'); if (fn) void fn().then(() => loadAutomation()); }, cancelLabel: 'Stop automatic copy' });
  }
  container.append(section);
}
function renderAutomationOffer(container: HTMLElement, plan: DriveUploadPlan) {
  if (!optional('AuthorizeAutomation') || !state.automation?.available || (plan.recreatedItems ?? 0) > 0) return;
  const section = el('section', 'automation-offer'); section.setAttribute('aria-labelledby', 'automation-offer-heading');
  const heading = el('h3', '', 'Keep this pair up to date automatically'); heading.id = 'automation-offer-heading'; section.append(heading);
  section.append(el('p', 'muted', 'Optional. While LedgeSync is open, it can copy new and changed files of this exact pair without asking each time. It never deletes or overwrites Drive files, and it pauses for your review if the account, destination, folder, configuration or ignore rules change.'));
  const form = el('div', 'automation-form');
  const trigger = el('select'); trigger.id = 'automation-trigger'; trigger.setAttribute('aria-label', 'Automatic copy schedule');
  for (const [value, label] of [['interval', 'Copy on a schedule'], ['watch', 'Check for local changes on a schedule']] as const) { const o = el('option', '', label); o.value = value; o.selected = state.automationTrigger === value; trigger.append(o); }
  trigger.addEventListener('change', () => { state.automationTrigger = trigger.value as 'interval' | 'watch'; });
  const interval = el('select'); interval.id = 'automation-interval'; interval.setAttribute('aria-label', 'How often');
  for (const [value, label] of [[300, 'Every 5 minutes'], [900, 'Every 15 minutes'], [3600, 'Every hour'], [21600, 'Every 6 hours'], [86400, 'Every day']] as const) { const o = el('option', '', label); o.value = String(value); o.selected = state.automationInterval === value; interval.append(o); }
  interval.addEventListener('change', () => { state.automationInterval = Number(interval.value); });
  const enable = button('Allow automatic copies…', () => {
    const fn = optional('AuthorizeAutomation');
    if (!fn || !validUploadPlan(plan)) return;
    if (!window.confirm(`Allow LedgeSync to copy new and changed files from “${plan.sourceName}” to “${plan.destinationName}” automatically while it is open? Existing Drive files are never overwritten or deleted. You can pause or turn this off at any time.`)) return;
    void projectAction(async () => { state.currentProject = await fn(state.currentProject?.id ?? '', state.automationTrigger, state.automationInterval, plan.planDigest); }, 'Automatic copies could not be enabled.');
  }, 'button subtle'); enable.id = 'automation-enable'; enable.disabled = state.projectBusy || !validUploadPlan(plan) || uploadLocked();
  form.append(trigger, interval, enable); section.append(form);
  if (state.projectError) { const error = el('div', 'error', state.projectError); error.setAttribute('role', 'alert'); section.append(error); }
  container.append(section);
}

// ---- Activity, history and settings ----------------------------------------

async function loadHistory(id: string) {
  const history = optional('ProjectHistory');
  if (!history) return;
  try { state.runs = await history(id) ?? []; state.historyError = ''; }
  catch (error) { state.historyError = describe(error, 'Run history could not be read.'); }
  render();
}
function renderRun(container: HTMLElement, run: RunSummary) {
  const item = el('article', `run run-${run.state}`); item.setAttribute('aria-label', `${run.projectName} ${run.state}`);
  const head = el('div', 'run-head'); head.append(el('strong', '', displayPath(run.projectName)), badge(transferLabels[run.state as TransferState] ?? run.state, run.state === 'succeeded' ? 'connected' : ''), el('span', 'muted', `${run.trigger === 'automatic' ? 'Automatic' : 'Manual'} · ${run.finishedAt ? new Date(run.finishedAt).toLocaleString() : '—'}`));
  item.append(head, el('p', '', displayPath(run.message)));
  item.append(el('p', 'muted', `${run.completedFiles} of ${run.totalFiles} files verified · ${bytes(run.sentBytes)} sent · destination ${displayPath(run.destination)}`));
  if (run.errorCode && guidanceFor(run.errorCode)) item.append(el('p', 'transfer-hint', guidanceFor(run.errorCode)));
  if (run.issues?.length) {
    const d = el('details'); d.append(el('summary', '', `${run.issues.length} item${run.issues.length === 1 ? '' : 's'} not copied`));
    const ul = el('ul'); for (const issue of run.issues.slice(0, 100)) { const li = el('li'); li.append(el('code', '', displayPath(issue.path)), el('span', '', ` — ${issue.message}`)); ul.append(li); } d.append(ul); item.append(d);
  }
  container.append(item);
}
function renderActivity(container: HTMLElement) {
  const heading = el('div', 'heading'); const title = el('div'); title.append(el('p', 'eyebrow', 'ACTIVITY'), el('h1', '', 'Activity')); heading.append(title);
  const refresh = button('Refresh', () => { void loadProjects(); void loadHistory(''); }, 'button subtle', 'refresh'); heading.append(refresh); container.append(heading);
  if (state.transfer && state.transfer.state !== 'idle' && state.transfer.state !== 'awaiting_approval') renderDriveTransfer(container);
  if (state.automation?.running && state.automation.transfer) renderTransferCard(container, state.automation.transfer, true, { label: 'Automatic copy', cancel: () => { const fn = optional('CancelAutomaticCopy'); if (fn) void fn().then(() => loadAutomation()); }, cancelLabel: 'Stop automatic copy' });
  if (state.automation?.paused) container.append(el('p', 'notice-inline', 'Automatic copies are paused for all sync pairs in Settings.'));
  const waiting = state.projects.filter(p => p.automation.enabled && (p.automation.paused || p.automation.waiting));
  for (const p of waiting) container.append(el('p', p.automation.paused ? 'error' : 'muted', `${p.name}: ${p.automation.paused ? p.automation.pauseReason ?? 'Paused.' : p.automation.waiting}`));
  container.append(el('h2', '', 'Recent runs'));
  if (state.historyError) { const error = el('div', 'error', state.historyError); error.setAttribute('role', 'alert'); container.append(error); }
  if (!state.runs.length) container.append(el('p', 'muted', 'No runs yet. Approved uploads and automatic copies appear here.'));
  for (const run of state.runs.slice(0, 20)) renderRun(container, run);
}
function renderHistory(container: HTMLElement) {
  const heading = el('div', 'heading'); const title = el('div'); title.append(el('p', 'eyebrow', 'HISTORY & RECOVERY'), el('h1', '', 'History & Recovery')); heading.append(title); container.append(heading);
  const guide = el('div', 'notice'); guide.append(icon('history'), el('p', '', 'LedgeSync never deletes or overwrites Drive files. To recover after an interruption, open the sync pair and preview again: reserved copies are reconciled by identity, finished files are skipped, and items that are missing, trashed, renamed or moved in Drive are copied again without changing the existing Drive items. Changed files keep their earlier copies.')); container.append(guide);
  const picker = el('select'); picker.setAttribute('aria-label', 'Sync pair history');
  const all = el('option', '', 'All sync pairs'); all.value = ''; picker.append(all);
  for (const p of state.projects) { const o = el('option', '', p.name); o.value = p.id; o.selected = state.historyProject === p.id; picker.append(o); }
  picker.addEventListener('change', () => { state.historyProject = picker.value; void loadHistory(picker.value); });
  container.append(picker);
  const project = state.projects.find(p => p.id === state.historyProject);
  if (project?.lastRun && (project.lastRun.state === 'succeeded' || project.lastRun.state === 'partial') && optional('OpenProjectDriveFolder')) {
    container.append(button('Open copy in Google Drive', () => { const fn = optional('OpenProjectDriveFolder'); if (fn) void fn(project.id).catch(error => { state.historyError = describe(error, 'The Drive folder could not be opened.'); render(); }); }, 'button subtle', 'cloud'));
  }
  if (project?.sourceIdentity && optional('RestoreProjectCopy')) {
    const restore = button('Restore this copy to a new folder…', () => void startRestore(project.id), 'button subtle', 'history');
    restore.disabled = state.restore?.state === 'restoring'; container.append(restore);
    container.append(el('p', 'muted', 'Downloads the verified copy from Drive into an empty folder you choose, checking every file’s checksum. Your source folder is never changed.'));
  }
  renderRestore(container);
  if (state.historyError) { const error = el('div', 'error', state.historyError); error.setAttribute('role', 'alert'); container.append(error); }
  if (!state.runs.length) container.append(el('p', 'muted', 'No runs recorded for this selection.'));
  for (const run of state.runs) renderRun(container, run);
  renderPairList(container);
}
async function startRestore(id: string) {
  const start = optional('RestoreProjectCopy');
  if (!start) return;
  state.restoreError = '';
  try { const progress = await start(id); if (progress) state.restore = progress; }
  catch (error) { state.restoreError = describe(error, 'The copy could not be restored.'); }
  render(); scheduleRestorePoll();
}
function scheduleRestorePoll() {
  window.clearTimeout(state.restorePoll);
  if (state.restore?.state !== 'restoring') return;
  state.restorePoll = window.setTimeout(async () => {
    const status = optional('RestoreStatus');
    if (status) { try { state.restore = await status() ?? state.restore; } catch { /* keep the last known progress */ } }
    render(); scheduleRestorePoll();
  }, 1000);
}
function renderRestore(container: HTMLElement) {
  if (state.restoreError) { const error = el('div', 'error', state.restoreError); error.setAttribute('role', 'alert'); container.append(error); }
  const r = state.restore;
  if (!r) return;
  const labels: Record<RestoreProgress['state'], string> = { restoring: 'Restoring copy', succeeded: 'Copy restored and verified', partial: 'Copy restored with missing items', failed: 'Restore failed', cancelled: 'Restore cancelled' };
  const card = el('section', `transfer-card transfer-${r.state === 'restoring' ? 'uploading' : r.state}`); card.setAttribute('aria-label', 'Restore');
  const status = el('div', 'transfer-announcement'); status.setAttribute('role', 'status'); status.append(el('h2', '', labels[r.state]), el('p', '', displayPath(r.message))); card.append(status);
  const progress = el('progress'); progress.max = Math.max(r.totalBytes, 1); progress.value = Math.min(r.bytes, progress.max); progress.setAttribute('aria-label', 'Restored bytes'); card.append(progress);
  card.append(el('p', 'transfer-counts', `${r.files} of ${r.totalFiles} files restored · ${bytes(r.bytes)} of ${bytes(r.totalBytes)}`), el('p', 'transfer-path', `Restore folder: ${displayPath(r.target)}`));
  if (r.errorCode && guidanceFor(r.errorCode)) card.append(el('p', 'transfer-hint', guidanceFor(r.errorCode)));
  if (r.issues?.length) { const d = el('details', 'transfer-issues'); d.open = true; d.append(el('summary', '', `${r.issues.length} item${r.issues.length === 1 ? '' : 's'} not restored`)); const ul = el('ul'); for (const issue of r.issues.slice(0, 100)) { const li = el('li'); li.append(el('code', '', displayPath(issue.path)), el('span', '', ` — ${issue.message}`)); ul.append(li); } d.append(ul); card.append(d); }
  if (r.state === 'restoring') card.append(button('Cancel restore', () => { const fn = optional('CancelRestore'); if (fn) void fn().then(async () => { const status = optional('RestoreStatus'); if (status) state.restore = await status(); render(); }); }, 'button subtle'));
  container.append(card);
}
async function loadSettings() {
  const get = optional('GetSettings');
  if (!get) return;
  try { state.settings = await get(); state.settingsError = ''; } catch (error) { state.settingsError = describe(error, 'Settings could not be read.'); }
  render();
}
async function saveSettings(next: Settings, notice: string) {
  const save = optional('SaveSettings');
  if (!save) return;
  try { state.settings = await save(next); state.settingsError = ''; state.settingsNotice = notice; }
  catch (error) { state.settingsError = describe(error, 'Settings could not be saved.'); }
  render(); void loadAutomation();
}
function renderSettings(container: HTMLElement) {
  const heading = el('div', 'heading'); const title = el('div'); title.append(el('p', 'eyebrow', 'SETTINGS'), el('h1', '', 'Settings')); heading.append(title); container.append(heading);
  const settings = state.settings;
  if (state.settingsError) { const error = el('div', 'error', state.settingsError); error.setAttribute('role', 'alert'); container.append(error); }
  if (state.settingsNotice) { const ok = el('p', 'settings-notice', state.settingsNotice); ok.setAttribute('role', 'status'); container.append(ok); }
  if (!settings) { container.append(el('p', 'muted', optional('GetSettings') ? 'Loading settings…' : 'Settings are available in the LedgeSync desktop application.')); return; }
  const form = el('section', 'settings'); form.setAttribute('aria-label', 'Defaults for new sync pairs');
  form.append(el('h2', '', 'New sync pairs'));
  const conflict = el('select'); conflict.id = 'settings-conflict'; conflict.setAttribute('aria-label', 'When a copied file changes');
  for (const [value, label] of [['keep-both', 'Copy the new version and keep the earlier copy'], ['pause', 'Pause changed files for review']] as const) { const o = el('option', '', label); o.value = value; o.selected = settings.defaultConflictPolicy === value; conflict.append(o); }
  const retries = el('input'); retries.id = 'settings-retries'; retries.type = 'number'; retries.min = '0'; retries.max = '20'; retries.value = String(settings.defaultMaxRetries); retries.setAttribute('aria-label', 'Retries for temporary network errors');
  const labelled = (text: string, control: HTMLElement) => { const l = el('label', 'field'); l.append(el('span', '', text), control); return l; };
  form.append(labelled('When a copied file changes', conflict), labelled('Retries for temporary network errors', retries));
  form.append(button('Save defaults', () => void saveSettings({ ...settings, defaultConflictPolicy: conflict.value as Settings['defaultConflictPolicy'], defaultMaxRetries: Number(retries.value) }, 'Defaults saved. They apply to folders chosen from now on.'), 'button primary'));
  container.append(form);
  const auto = el('section', 'settings'); auto.setAttribute('aria-label', 'Automatic copies');
  auto.append(el('h2', '', 'Automatic copies'), el('p', 'muted', 'Automatic copies run only for sync pairs you authorized, and only while LedgeSync is open. Installing or updating LedgeSync never enables them.'));
  auto.append(button(settings.automationPaused ? 'Resume automatic copies' : 'Pause all automatic copies', () => void saveSettings({ ...settings, automationPaused: !settings.automationPaused }, settings.automationPaused ? 'Automatic copies resumed.' : 'Automatic copies paused.'), 'button subtle'));
  container.append(auto);
  const data = el('section', 'settings'); data.setAttribute('aria-label', 'Local data');
  data.append(el('h2', '', 'Local data and privacy'), el('p', 'muted', 'LedgeSync keeps its transfer journal, saved pairs and run history in a private folder for your user account. Google authorization stays in your system credential vault. There is no telemetry.'));
  data.append(button('Clear run history on this computer…', () => {
    const fn = optional('ClearHistory');
    if (fn && window.confirm('Clear run history on this computer? Drive files, saved pairs and the transfer journal are not changed.')) void fn().then(() => { state.settingsNotice = 'Run history cleared.'; state.runs = []; render(); }).catch(error => { state.settingsError = describe(error, 'History could not be cleared.'); render(); });
  }, 'button subtle'));
  data.append(el('p', 'muted', `LedgeSync ${version}`));
  container.append(data);
}

// ---- Policy editor -----------------------------------------------------------

const editableDialects = ['gitignore', 'rclone-filter', 'rclone-include', 'rclone-exclude'];
function clonePolicy(policy: ProjectPolicy): ProjectPolicy { return JSON.parse(JSON.stringify(policy)) as ProjectPolicy; }
function renderPolicyEditor(container: HTMLElement) {
  const project = state.currentProject;
  const section = el('section', 'policy-editor'); section.setAttribute('aria-labelledby', 'policy-heading');
  const heading = el('h2', '', 'Selection policy'); heading.id = 'policy-heading'; section.append(heading);
  if (!project || !optional('UpdateProjectPolicy')) {
    section.append(el('p', 'muted', 'Open a saved sync pair to edit its rule files, composition and conflict policy. A pair is saved when you approve its first upload.'));
    container.append(section); return;
  }
  if (project.configPath) { section.append(el('p', 'muted', `This pair uses the configuration file ${displayPath(project.configPath)}. Edit that file to change its policy.`)); container.append(section); return; }
  const draft = state.policyDraft ?? (state.policyDraft = clonePolicy(project.policy));
  const changed = () => render();
  const select = (label: string, value: string, options: [string, string][], onChange: (v: string) => void) => {
    const s = el('select'); s.setAttribute('aria-label', label); for (const [v, text] of options) { const o = el('option', '', text); o.value = v; o.selected = v === value; s.append(o); }
    s.addEventListener('change', () => { onChange(s.value); changed(); }); const l = el('label', 'field'); l.append(el('span', '', label), s); return l;
  };
  section.append(select('Composition', draft.composition, [['conservative', 'Conservative: any rule group can exclude'], ['ordered', 'Ordered: the highest-priority decisive group wins']], v => { draft.composition = v as ProjectPolicy['composition']; }));
  section.append(select('When a copied file changes', draft.conflictPolicy, [['keep-both', 'Copy the new version and keep the earlier copy'], ['pause', 'Pause changed files for review']], v => { draft.conflictPolicy = v as ProjectPolicy['conflictPolicy']; }));
  const retries = el('input'); retries.type = 'number'; retries.min = '0'; retries.max = '20'; retries.value = String(draft.maxRetries); retries.setAttribute('aria-label', 'Retries for temporary network errors');
  retries.addEventListener('change', () => { draft.maxRetries = Number(retries.value); });
  const retryLabel = el('label', 'field'); retryLabel.append(el('span', '', 'Retries for temporary network errors'), retries); section.append(retryLabel);
  draft.groups.forEach((group, index) => section.append(renderGroupEditor(draft, group, index)));
  const add = button('Add rule group', () => {
    const used = new Set(draft.groups.map(g => g.id)); let n = draft.groups.length + 1; while (used.has(`rules-${n}`)) n++;
    const priorities = new Set(draft.groups.map(g => g.priority)); let priority = 90; while (priorities.has(priority)) priority--;
    draft.groups.push({ id: `rules-${n}`, priority, enabled: true, dialect: 'gitignore', scope: 'project', sources: [{ type: 'recursive-basename', value: '.ignore', required: false }] }); changed();
  }, 'button subtle'); section.append(add);
  if (state.policyError) { const error = el('div', 'error', state.policyError); error.setAttribute('role', 'alert'); section.append(error); }
  const actions = el('div', 'upload-actions');
  const save = button('Save policy and rescan', () => {
    const fn = optional('UpdateProjectPolicy'); if (!fn || state.busy || uploadLocked()) return;
    state.policyError = ''; state.busy = true; render();
    void fn(project.id, draft).then(session => {
      state.currentProject = session.project; state.policyDraft = null; invalidateUploadPlan();
      if (session.preview) state.preview = session.preview;
      if (session.destinationError) state.uploadError = session.destinationError.message;
    }).catch(error => { state.policyError = describe(error, 'The policy could not be saved.'); }).finally(() => { state.busy = false; render(); void loadProjects(); });
  }, 'button primary'); save.disabled = state.busy || uploadLocked();
  const reset = button('Discard changes', () => { state.policyDraft = null; state.policyError = ''; render(); }, 'button subtle');
  actions.append(save, reset); section.append(actions);
  section.append(el('p', 'muted', 'Saving a policy invalidates any preview and pauses automatic copies until you review and authorize them again. Unsupported adapters are listed below and cannot be selected.'));
  container.append(section);
}
function renderGroupEditor(draft: ProjectPolicy, group: PolicyGroup, index: number) {
  const box = el('fieldset', 'policy-group'); box.append(el('legend', '', `Rule group ${group.id}`));
  const enabled = el('input'); enabled.type = 'checkbox'; enabled.checked = group.enabled; enabled.setAttribute('aria-label', `Enable ${group.id}`); enabled.addEventListener('change', () => { group.enabled = enabled.checked; });
  const dialect = el('select'); dialect.setAttribute('aria-label', `Dialect of ${group.id}`);
  for (const d of editableDialects) { const o = el('option', '', d); o.value = d; o.selected = group.dialect === d; dialect.append(o); }
  dialect.addEventListener('change', () => { group.dialect = dialect.value; if (dialect.value !== 'gitignore') for (const s of group.sources) s.type = 'root-file'; render(); });
  const priority = el('input'); priority.type = 'number'; priority.min = '0'; priority.max = '1000000'; priority.value = String(group.priority); priority.setAttribute('aria-label', `Priority of ${group.id}`); priority.addEventListener('change', () => { group.priority = Number(priority.value); });
  const row = el('div', 'policy-row'); const e = el('label'); e.append(enabled, el('span', '', 'Enabled')); row.append(e, dialect, priority); box.append(row);
  group.sources.forEach((source, i) => {
    const line = el('div', 'policy-source');
    const type = el('select'); type.setAttribute('aria-label', `Source type ${i + 1} of ${group.id}`);
    const types: [string, string][] = group.dialect === 'gitignore' ? [['recursive-basename', 'File name in every folder'], ['root-file', 'One file, path from the folder root']] : [['root-file', 'One file, path from the folder root']];
    for (const [v, t] of types) { const o = el('option', '', t); o.value = v; o.selected = source.type === v; type.append(o); }
    type.addEventListener('change', () => { source.type = type.value as typeof source.type; });
    const value = el('input'); value.type = 'text'; value.value = source.value; value.setAttribute('aria-label', `Source ${i + 1} of ${group.id}`); value.addEventListener('change', () => { source.value = value.value.trim(); });
    const required = el('input'); required.type = 'checkbox'; required.checked = source.required; required.setAttribute('aria-label', `Source ${i + 1} required`); required.addEventListener('change', () => { source.required = required.checked; });
    const req = el('label'); req.append(required, el('span', '', 'Required'));
    const remove = button('Remove', () => { group.sources.splice(i, 1); render(); }, 'button subtle'); remove.disabled = group.sources.length === 1;
    line.append(type, value, req, remove); box.append(line);
  });
  const actions = el('div', 'policy-row');
  actions.append(button('Add rule file', () => { group.sources.push({ type: group.dialect === 'gitignore' ? 'recursive-basename' : 'root-file', value: group.dialect === 'gitignore' ? '.ignore' : 'filters.txt', required: false }); render(); }, 'button subtle'));
  const remove = button('Remove group', () => { draft.groups.splice(index, 1); render(); }, 'button subtle'); remove.disabled = draft.groups.length === 1; actions.append(remove);
  box.append(actions);
  return box;
}
// ---- Two-way sync ----------------------------------------------------------

const syncStateLabels: Record<SyncState, string> = { starting: 'Starting', syncing: 'Syncing', synced: 'Up to date', paused: 'Paused', waiting: 'Waiting', confirm_deletes: 'Needs your confirmation', error: 'Needs attention' };
const activityLabels: Record<string, string> = {
  upload: 'Uploaded to Drive', download: 'Downloaded from Drive', update_up: 'Updated in Drive', update_down: 'Updated on this computer',
  delete_up: 'Moved to the Google Drive trash', delete_down: 'Moved to the LedgeSync trash on this computer', conflict: 'Changed on both sides — both versions kept',
  folder_up: 'Folder created in Drive', folder_down: 'Folder created on this computer', sync_added: 'Started syncing', restore_deletes: 'Restoring deleted files',
};
let syncReload = 0;
let activityLoadedAt = 0;
function syncAvailable() { return Boolean(optional('SyncList')); }
function scheduleSyncReload() {
  if (syncReload) return;
  syncReload = window.setTimeout(() => { syncReload = 0; void loadSyncs(); }, 250);
}
async function loadSyncs() {
  const fn = optional('SyncList'); if (!fn) return;
  try { state.syncs = await fn(); state.syncLoaded = true; }
  catch (error) { state.syncError = describe(error, 'The sync status could not be read.'); }
  if (state.view === 'sync' && Date.now() - activityLoadedAt > 2000) void loadSyncActivity();
  render();
}
async function loadSyncActivity() {
  const fn = optional('SyncActivity'); if (!fn) return;
  activityLoadedAt = Date.now();
  try { state.syncActivity = await fn(''); } catch { /* activity is informative only */ }
  render();
}
async function syncAction(action: () => Promise<unknown>, fallback: string, busy: string) {
  if (state.syncBusy) return;
  state.syncBusy = busy; state.syncError = ''; render();
  try { await action(); } catch (error) { state.syncError = describe(error, fallback); }
  finally { state.syncBusy = ''; }
  await loadSyncs();
}
async function addSync() {
  const fn = optional('SyncChooseFolder'); if (!fn) return;
  state.view = 'sync';
  await syncAction(() => fn(state.syncParent.id, state.syncParent.name), 'This folder could not be synced. Check that Google Drive is connected, then try again.', 'add');
}
function syncSummary() {
  if (!state.syncs.length) return 'Two-way sync';
  if (state.syncs.some(s => s.state === 'confirm_deletes' || s.state === 'error')) return 'A synced folder needs attention';
  if (state.syncs.some(s => s.state === 'syncing' || s.state === 'starting')) return 'Syncing…';
  if (state.syncs.every(s => s.state === 'synced')) return 'All folders up to date';
  return `${state.syncs.length} synced folder${state.syncs.length === 1 ? '' : 's'}`;
}
function renderSync(container: HTMLElement) {
  const heading = el('div', 'heading');
  const title = el('div'); title.append(el('p', 'eyebrow', 'TWO-WAY SYNC'), el('h1', '', 'Synced folders'));
  heading.append(title);
  const add = button('Sync a folder', () => void addSync(), 'button primary', 'folder'); add.id = 'sync-add';
  add.disabled = Boolean(state.syncBusy) || state.drive?.state !== 'connected';
  heading.append(add); container.append(heading);
  container.append(el('p', 'section-description', 'Like Google Drive for desktop: choose a folder and LedgeSync keeps it and its Google Drive copy the same, in both directions, while LedgeSync is open. Changes on either side reach the other, deleted files go to the trash on the other side, and when a file changes on both sides both versions are kept.'));
  if (state.drive && state.drive.state !== 'connected') {
    const notice = el('div', 'notice sync-connect');
    notice.append(icon('cloud'), el('p', '', state.drive.state === 'reconnect_required' || state.drive.state === 'client_changed' ? 'Reconnect Google Drive to grant the full Drive access that sync needs.' : 'Connect Google Drive to start syncing.'));
    notice.append(button('Open Connections', () => changeView('connections'), 'button subtle'));
    container.append(notice);
  }
  const location = el('div', 'sync-location');
  location.append(icon('cloud'), el('span', '', 'New folders sync into'), el('strong', '', displayPath(state.syncParent.name)));
  const change = button('Change', () => void openFolderBrowser(), 'button subtle'); change.id = 'sync-location-change';
  change.disabled = state.drive?.state !== 'connected' || Boolean(state.syncBusy) || !optional('DriveFolders');
  location.append(change); container.append(location);
  if (state.syncError) { const error = el('div', 'error', state.syncError); error.setAttribute('role', 'alert'); container.append(error); }
  if (state.folderBrowser.open) renderFolderBrowser(container);
  const list = el('div', 'sync-list'); list.setAttribute('role', 'list');
  if (!state.syncs.length) list.append(el('p', 'empty-list', state.syncLoaded ? 'No folders are synced yet. Choose “Sync a folder” to start.' : 'Loading…'));
  for (const s of state.syncs) list.append(renderSyncCard(s));
  container.append(list);
  renderSyncActivityList(container);
}
function renderSyncCard(s: SyncStatus): HTMLElement {
  const card = el('article', `sync-card sync-${s.state}`); card.setAttribute('role', 'listitem'); card.setAttribute('aria-label', `Synced folder ${s.pair.name}`);
  const head = el('div', 'sync-head');
  head.append(icon('folder'), el('strong', '', displayPath(s.pair.name)), badge(syncStateLabels[s.state] ?? s.state, `sync-badge ${s.state}`));
  card.append(head, el('p', 'muted sync-path', displayPath(s.pair.localRoot)), el('p', 'sync-remote', `Google Drive: ${displayPath(s.pair.parent?.name || 'My Drive')} › ${displayPath(s.pair.name)}`));
  const line = s.state === 'synced' && s.lastSyncAt ? `${s.message} · checked ${date(s.lastSyncAt)}` : s.message;
  const status = el('p', 'sync-message', displayPath(line)); status.setAttribute('role', 'status'); card.append(status);
  if (s.state === 'syncing' && s.total > 0) {
    const progress = el('progress'); progress.max = s.total; progress.value = s.done; card.append(progress);
    card.append(el('p', 'sync-counts', `${s.done} of ${s.total} · ↑ ${s.uploads} to Drive · ↓ ${s.downloads} to this computer`));
    if (s.currentPath) card.append(el('p', 'transfer-path', displayPath(s.currentPath)));
  }
  if ((s.state === 'error' || s.state === 'waiting') && s.errorCode) { const hint = guidanceFor(s.errorCode); if (hint) card.append(el('p', 'transfer-hint', hint)); }
  if (s.state === 'confirm_deletes') {
    const box = el('div', 'sync-confirm'); box.setAttribute('role', 'alert');
    const parts: string[] = [];
    if (s.remoteDeletes) parts.push(`${s.remoteDeletes} file${s.remoteDeletes === 1 ? '' : 's'} deleted on this computer would be moved to the Google Drive trash`);
    if (s.localDeletes) parts.push(`${s.localDeletes} file${s.localDeletes === 1 ? '' : 's'} deleted in Google Drive would be moved to the LedgeSync trash on this computer`);
    box.append(el('p', '', `${parts.join('; ')}. So many at once can mean a disconnected disk or a mistake, so LedgeSync waits for you.`));
    const actions = el('div', 'pair-actions');
    const confirm = button('Delete on the other side too', () => { const fn = optional('SyncConfirmDeletes'); if (fn) void syncAction(() => fn(s.pair.id), 'The deletions could not be confirmed.', s.pair.id); }, 'button primary'); confirm.id = `sync-confirm-${s.pair.id}`;
    const restore = button('Restore the files instead', () => { const fn = optional('SyncRestoreDeletes'); if (fn) void syncAction(() => fn(s.pair.id), 'The files could not be restored.', s.pair.id); }, 'button subtle'); restore.id = `sync-restore-${s.pair.id}`;
    confirm.disabled = restore.disabled = Boolean(state.syncBusy);
    actions.append(confirm, restore); box.append(actions); card.append(box);
  }
  if (s.issues.length) {
    const details = el('details', 'transfer-issues'); details.append(el('summary', '', `${s.issues.length} item${s.issues.length === 1 ? '' : 's'} not synced`));
    const list = el('ul'); for (const issue of s.issues.slice(0, 50)) list.append(el('li', '', `${displayPath(issue.path)} — ${displayPath(issue.message)}`));
    details.append(list); card.append(details);
  }
  const actions = el('div', 'pair-actions');
  const paused = s.pair.paused;
  const pause = button(paused ? 'Resume' : 'Pause', () => { const fn = optional(paused ? 'SyncResume' : 'SyncPause'); if (fn) void syncAction(() => fn(s.pair.id), 'The sync could not be changed.', s.pair.id); }, 'button subtle'); pause.id = `sync-pause-${s.pair.id}`;
  const now = button('Sync now', () => { const fn = optional('SyncNow'); if (fn) void syncAction(() => fn(s.pair.id), 'The sync could not start.', s.pair.id); }, 'button subtle'); now.id = `sync-now-${s.pair.id}`;
  const openFailed = (error: unknown) => { state.syncError = describe(error, 'The folder could not be opened.'); render(); };
  const openLocal = button('Open folder', () => { const fn = optional('SyncOpenLocal'); if (fn) void fn(s.pair.id).catch(openFailed); }, 'button subtle');
  const openDrive = button('Open in Drive', () => { const fn = optional('SyncOpenDrive'); if (fn) void fn(s.pair.id).catch(openFailed); }, 'button subtle');
  const remove = button('Stop syncing', () => { const fn = optional('SyncRemove'); if (fn && window.confirm(`Stop syncing “${s.pair.name}”? Files stay on this computer and in Google Drive.`)) void syncAction(() => fn(s.pair.id), 'The sync could not be removed.', s.pair.id); }, 'button subtle'); remove.id = `sync-remove-${s.pair.id}`;
  now.disabled = paused;
  for (const control of [pause, now, remove]) control.disabled = control.disabled || Boolean(state.syncBusy);
  actions.append(pause, now, openLocal, openDrive, remove); card.append(actions);
  return card;
}
function renderSyncActivityList(container: HTMLElement) {
  if (!optional('SyncActivity')) return;
  const section = el('section', 'sync-activity'); section.setAttribute('aria-labelledby', 'sync-activity-heading');
  const heading = el('h2', '', 'Recent activity'); heading.id = 'sync-activity-heading'; section.append(heading);
  if (!state.syncActivity.length) { section.append(el('p', 'muted', 'Changes appear here as they sync.')); container.append(section); return; }
  const list = el('ul');
  for (const a of state.syncActivity.slice(0, 40)) {
    const item = el('li');
    item.append(el('span', 'activity-kind', activityLabels[a.kind] ?? a.kind), el('span', 'activity-path', displayPath(a.path || a.detail || '')), el('span', 'muted', date(a.at)));
    if (a.kind === 'conflict' && a.detail) item.append(el('span', 'muted activity-detail', `Your version was kept as ${displayPath(a.detail)}`));
    list.append(item);
  }
  section.append(list); container.append(section);
}
async function openFolderBrowser() {
  state.folderBrowser = { open: true, stack: [{ id: 'root', name: 'My Drive' }], folders: [], loading: true, error: '' };
  render(); await loadFolders();
}
async function loadFolders() {
  const fn = optional('DriveFolders'); if (!fn) return;
  const fb = state.folderBrowser;
  const current = fb.stack[fb.stack.length - 1];
  fb.loading = true; fb.error = ''; render();
  try { fb.folders = await fn(current.id); }
  catch (error) { fb.error = describe(error, 'Drive folders could not be listed.'); fb.folders = []; }
  finally { fb.loading = false; render(); }
}
function renderFolderBrowser(container: HTMLElement) {
  const fb = state.folderBrowser;
  const box = el('section', 'folder-browser'); box.setAttribute('aria-label', 'Choose a Google Drive folder');
  const crumbs = el('div', 'breadcrumbs');
  fb.stack.forEach((entry, index) => {
    if (index) crumbs.append(el('span', 'separator', '›'));
    crumbs.append(button(entry.name, () => { fb.stack = fb.stack.slice(0, index + 1); void loadFolders(); }, 'crumb'));
  });
  box.append(crumbs);
  if (fb.error) box.append(el('div', 'error', fb.error));
  const list = el('div', 'folder-list');
  if (fb.loading) list.append(el('p', 'muted', 'Loading folders…'));
  else if (!fb.folders.length) list.append(el('p', 'muted', 'No folders here.'));
  for (const f of fb.folders) {
    const row = button(f.name, () => { fb.stack.push({ id: f.id, name: f.name }); void loadFolders(); }, 'folder-row', 'folder');
    row.disabled = fb.loading; list.append(row);
  }
  box.append(list);
  const current = fb.stack[fb.stack.length - 1];
  const actions = el('div', 'pair-actions');
  const use = button(`Use “${current.name}”`, () => {
    state.syncParent = { id: current.id, name: current.name }; fb.open = false;
    try { localStorage.setItem('ledgesync.syncParent', JSON.stringify(state.syncParent)); } catch { /* per-viewer convenience only */ }
    render();
  }, 'button primary'); use.id = 'folder-use';
  const cancel = button('Cancel', () => { fb.open = false; render(); }, 'button subtle');
  actions.append(use, cancel); box.append(actions); container.append(box);
}
try {
  const saved = JSON.parse(localStorage.getItem('ledgesync.syncParent') ?? 'null') as { id?: unknown; name?: unknown } | null;
  if (saved && typeof saved.id === 'string' && typeof saved.name === 'string') state.syncParent = { id: saved.id, name: saved.name };
} catch { /* per-viewer convenience only */ }
if (syncAvailable()) state.view = 'sync';

render();
// The desktop bridge may arrive after web assets initialize. Startup reads only
// saved connection metadata; destination selection and upload remain explicit.
if (typeof window.go?.desktop?.App?.GoogleDriveStatus === 'function') void driveAction('status');
if (optional('ListProjects')) void loadProjects();
window.runtime?.EventsOn?.('ledgesync:automation', () => { void loadAutomation(); void loadProjects(); });
if (syncAvailable()) { void loadSyncs(); void loadSyncActivity(); }
window.runtime?.EventsOn?.('ledgesync:sync', scheduleSyncReload);
