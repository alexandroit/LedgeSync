// Desktop errors arrive as "CODE: message". Only the code selects guidance;
// the message is the backend's redacted text. Anything else is not displayed.
export interface PublicError { code: string; message: string }

const pattern = /^([A-Z][A-Z0-9_]{1,63}): ([^\u0000]{1,2000})$/s;

export function parseError(error: unknown): PublicError | null {
  const text = typeof error === 'string' ? error : error instanceof Error ? error.message : '';
  const match = pattern.exec(text.trim());
  return match ? { code: match[1], message: match[2] } : null;
}

const guidance: Record<string, string> = {
  AUTH_REQUIRED: 'Open Connections and reconnect Google Drive.',
  AUTH_SCOPE_REQUIRED: 'Open Connections and reconnect Google Drive.',
  AUTH_IDENTITY_CHANGED: 'Open Connections and check which account is connected.',
  ACCOUNT_CHANGED: 'Open Connections and check which account is connected.',
  AUTH_STORAGE_UNAVAILABLE: 'Unlock your system credential vault (Keychain, Credential Manager or GNOME Keyring), then try again.',
  AUTH_BUSY: 'Another Google Drive authorization is running. Wait for it to finish.',
  TRANSFER_BUSY: 'Another Drive operation is running. Wait for it to finish or cancel it.',
  SCAN_BUSY: 'A folder scan is running. Wait for it to finish or cancel it.',
  DESTINATION_REQUIRED: 'Choose My Drive or an existing Drive folder first.',
  DESTINATION_CHANGED: 'Choose the Drive destination again, then preview.',
  DESTINATION_UNAVAILABLE: 'Choose the Drive destination again, then preview.',
  DESTINATION_READ_ONLY: 'Choose a folder you can edit.',
  DRIVE_NOT_FOUND: 'Choose the folder again in the Google Picker. LedgeSync can only use folders you select or that it created.',
  DRIVE_NOT_FOLDER: 'Choose a folder rather than a file.',
  DRIVE_PERMISSION_DENIED: 'Choose a folder you own or can edit.',
  DRIVE_UNSUPPORTED: 'Shared drives are not supported yet. Choose a folder in My Drive.',
  DRIVE_STORAGE_FULL: 'Free up Google Drive storage, then preview again.',
  DRIVE_QUOTA: 'Google Drive limited this application. Try again later.',
  DRIVE_NETWORK: 'Check your internet connection, then preview again. Completed copies are kept.',
  DRIVE_UNAVAILABLE: 'Google Drive is temporarily unavailable. Preview again later; completed copies are kept.',
  DRIVE_RATE_LIMIT: 'Google Drive asked LedgeSync to slow down. Preview again in a few minutes.',
  TIMEOUT: 'Check your internet connection, then try again.',
  RULES_CHANGED: 'Ignore rules changed. Review them, then preview again.',
  RULE_SOURCE_UNAVAILABLE: 'A rule file that was used before is missing. Restore it, or change the sync pair’s policy explicitly.',
  RULE_PARSE_ERROR: 'Fix the reported rule file, then refresh.',
  CONFIG_CHANGED: 'The configuration changed. Preview again.',
  CONFIG_INVALID: 'Correct the highlighted settings.',
  CAPABILITY_UNSUPPORTED: 'This policy uses an adapter that is not available in this version.',
  SOURCE_UNAVAILABLE: 'Make sure the folder exists and its drive is connected.',
  SOURCE_REPLACED: 'The folder was replaced or moved to another drive. Choose it again.',
  SCAN_INCOMPLETE: 'Some items could not be read. Check folder permissions, then refresh.',
  STATE_UNAVAILABLE: 'LedgeSync’s private journal is unavailable. Close other LedgeSync windows or commands and try again.',
  STATE_INVALID: 'The saved transfer history does not match this folder. Choose the folder again.',
  PLAN_REQUIRED: 'Preview again before uploading.',
  PLAN_EXPIRED: 'The preview expired. Preview again before uploading.',
  PLAN_STALE: 'The folder or Drive changed after the preview. Preview again.',
  REMOTE_CHANGED: 'Preview again. LedgeSync reconciles earlier copies and never overwrites them.',
  UNKNOWN_REMOTE_RESULT: 'Preview again. LedgeSync checks the reserved copy before continuing, so nothing is duplicated.',
  AUTOMATION_REVIEW_REQUIRED: 'Preview the sync pair, review it and authorize automatic copies again.',
  OAUTH_UNAVAILABLE: 'Install the official LedgeSync application from ledgesync.com.',
};

export function guidanceFor(code: string): string { return guidance[code] ?? ''; }

// describe returns user-facing text: the redacted message plus guidance, or
// the fallback when the error is not a typed backend error.
export function describe(error: unknown, fallback: string): string {
  const parsed = parseError(error);
  if (!parsed) return fallback;
  const hint = guidanceFor(parsed.code);
  return hint && !parsed.message.includes(hint) ? `${parsed.message} ${hint}` : parsed.message;
}
