# Google verification for full Drive access

Since 0.1.0-alpha.7, LedgeSync requests `https://www.googleapis.com/auth/drive`.
Two-way sync needs it to receive files that are added to a synced folder by the
Drive website, phones or other apps ([ADR-034](12_ADR_DECISIONS.md)).

## Why "Google hasn't verified this app" appears

Google classifies full Drive access as a **restricted scope**. Until Google
verifies the app for that scope:
- consent shows the unverified-app screen, and users continue with
  **Advanced → Go to LedgeSync**;
- at most 100 Google accounts can authorize the app.

Personal use by the owner, or by a few people the owner knows, is an accepted
exception. The warning appears only while connecting, not during sync.

## What verification requires

Google requires a third-party security assessment (CASA) for apps that "have
the ability to access data from or through a third-party server". LedgeSync has
no server: it talks to Google Drive only from the user's device, and keeps
tokens in the operating system's credential vault. So only the verification
review should apply. Google makes the final determination
([Google: restricted scope verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/restricted-scope-verification)).

The owner completes these steps in the Google Cloud project of the bundled
OAuth client:
1. **Domain.** Verify `ledgesync.com` in [Google Search Console](https://search.google.com/search-console)
   with the account that owns the project. Use a DNS TXT record at Cloudflare.
   An HTML verification file served at the site root also works, and the
   maintainers can publish one.
2. **Branding** (Google Auth Platform → Branding):

   | Field | Value |
   |---|---|
   | App name | LedgeSync |
   | User support email | the owner's support address |
   | App logo | 120×120 PNG made from the app icon (`sips -z 120 120 build/appicon.png --out logo.png` after a desktop build) |
   | App home page | https://ledgesync.com/ |
   | Privacy policy | https://ledgesync.com/privacy-policy/ |
   | Terms of service | https://ledgesync.com/public-term/ |
   | Authorized domain | ledgesync.com |
   | Developer contact | the owner's address |

3. **Data access.** Add `https://www.googleapis.com/auth/drive` and paste the
   justification below.
4. **Demo video.** Upload an unlisted YouTube video that follows the script
   below.
5. **Verification Center.** Submit, then answer Google's e-mails.

## Scope justification

> LedgeSync is an open-source desktop and command-line application that keeps
> a folder on the user's computer and a folder in the user's Google Drive in
> two-way sync, like Google Drive for desktop. The user explicitly chooses each
> folder to sync. LedgeSync must list, download, upload, update and move to the
> trash the files inside that Drive folder, including files that the user adds
> to it through the Drive website, mobile apps or other applications.
>
> The drive.file scope cannot see files created by other apps, so changes made
> outside LedgeSync would never sync. drive.readonly cannot upload, and
> drive.appdata is not visible to users.
>
> LedgeSync accesses Google Drive only from the user's device. It has no
> server, stores tokens only in the operating system's credential vault, does
> not transfer user data to third parties and does not use it for advertising.
> Access is limited to the folders the user chooses to sync or copy, and to
> listing folders when the user picks where a synced folder lives.

## Demo video script (2–3 minutes)

1. Show https://ledgesync.com/ and open the installed app. The version is shown
   in the sidebar.
2. In **Connections**, click **Connect Google Drive**. On the browser consent
   screen, show the address bar with the OAuth `client_id`, and the permission
   "See, edit, create, and delete all of your Google Drive files". Approve.
3. In **Synced folders**, click **Sync a folder** and choose a local folder.
   Show the folder and its files appearing in Google Drive.
4. In drive.google.com, upload a file into that Drive folder and show it
   arriving in the local folder.
5. Edit a local file and show the new revision under **Manage versions** in
   Drive. Delete a local file and show it in the Drive trash.
6. Show **Stop syncing** and **Connections → Disconnect**, and point to the
   privacy policy.
