# gws moxin

Generic API passthrough for [gws](https://github.com/googleworkspace/cli)
(Google Workspace CLI). Product-specific tools have been split into dedicated
moxins:

| Moxin      | Product         | Tools |
|------------|-----------------|-------|
| `piers`    | Google Docs     | 16    |
| `car`      | Google Drive    | 5     |
| `slip`     | Google Slides   | —     |
| `prison`   | Google Sheets   | 1     |
| `gmail`    | Gmail           | 2     |
| `calendar` | Google Calendar | 1     |

## Tools

| Tool | Description |
|------|-------------|
| `api` | Raw gws API call (debugging) |

## Availability

These moxins are built but not shipped in the bundle baked into moxy. To use
them, make the `gws-moxins` flake output's `share/moxy/moxins/<name>`
directories discoverable — link them into `~/.config/moxy/moxins/`, or put the
bundle's `share/moxy/moxins` on `MOXIN_PATH` (`moxy-moxins-all` carries them
too, along with every other built moxin) — and make sure no moxyfile in the hierarchy lists them under
`disable-moxins` (that key merges additively, so a project-local moxyfile
cannot re-enable what a parent disables).

## Authentication

Every gws-based moxin wraps a pinned `gws` binary and reads the credential
that `gws auth login` stores. Authentication is a one-time browser consent
against an OAuth client that you own; no `gcloud` install is needed.

`just run-gws <args>` runs the same pinned binary the moxins wrap, so a
credential written through it is in the format they read back.

### Setup

1. In a Google Cloud project you own, enable the APIs you want to use: Google
   Drive, Google Docs, Google Sheets, Gmail, Google Calendar. A call to an API
   that is not enabled fails with a 403 whose message carries the enable URL.

2. Configure the project's OAuth consent screen (Google Auth Platform):

   - Audience: **External**, unless the project belongs to a Google Workspace
     organization, in which case **Internal** avoids the two caveats below.
   - Publishing status: **In production**. An External client left in
     **Testing** is issued refresh tokens that expire after 7 days, and only
     accounts listed under Test users can log in.

3. Create an OAuth client of type **Desktop app**, download its JSON, and save
   it as `~/.config/gws/client_secret.json`.

4. Log in, limiting the scope picker to the services you need:
   ```
   just run-gws auth login -s drive,docs,sheets,gmail,calendar
   ```
   An unverified External client shows a "Google hasn't verified this app"
   screen; continue past it. The default picker grants read/write scopes —
   pass `--readonly`, or `--scopes <comma-separated list>`, to narrow them.
   The `piers` edit and comment tools need write access to Docs.

5. Verify:
   ```
   just run-gws auth status
   just run-gws drive files list --params '{"pageSize": 1}'
   ```

### Smoke-testing the moxins

```
# one built moxin script, directly (bypasses moxy)
just debug-gws-moxin-smoke car get <fileId>

# one tool through the moxy proxy, bypassing any ambient disable-moxins
just debug-gws-moxy-call piers.outline '{"document_id":"<docId>"}'
```

### Credential storage

`gws auth login` writes `~/.config/gws/credentials.enc`, encrypted with a key
held in the OS keyring. On a host with no keyring service, set
`GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file` to keep the key in
`~/.config/gws/.encryption_key` instead. `GOOGLE_WORKSPACE_CLI_CONFIG_DIR`
relocates the whole directory.

### Auth priority

gws checks credentials in this order:

1. `GOOGLE_WORKSPACE_CLI_TOKEN` env var (raw access token)
2. `GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE` env var (plaintext JSON; user or
   service-account credentials)
3. `~/.config/gws/credentials.enc` (encrypted, from `gws auth login`)
4. `~/.config/gws/credentials.json` (plaintext)

### Sharing with other users

Each user either repeats the setup with their own project, or one client is
shared. A shared unverified External client is capped at 100 users and keeps
the warning screen; lifting both requires Google's OAuth verification, and the
broad Drive scopes (`drive`, `drive.readonly`) are in Google's *restricted*
tier. A Google Workspace organization can sidestep verification with an
Internal client.

### Known issues

**gws `+` helper commands did not carry the quota project under gcloud ADC.**
Observed with the earlier ADC-based setup (not re-tested with `gws auth
login`): helper commands such as `drive +search` build their own HTTP requests
and omitted the `x-goog-user-project` header the low-level path adds, failing
with a 403. All moxin tools use the low-level API form
(`drive files list --params '...'`) rather than the helpers.
