# gws moxin

Generic API passthrough for [gws](https://github.com/googleworkspace/cli)
(Google Workspace CLI). Product-specific tools have been split into dedicated
moxins:

| Moxin      | Product         | Tools |
|------------|-----------------|-------|
| `piers`    | Google Docs     | 12    |
| `car`      | Google Drive    | 4     |
| `slip`     | Google Slides   | —     |
| `prison`   | Google Sheets   | 1     |
| `gmail`    | Gmail           | 2     |
| `calendar` | Google Calendar | 1     |

## Tools

| Tool | Description |
|------|-------------|
| `api` | Raw gws API call (debugging) |

## Authentication

All gws-based moxins rely on Application Default Credentials (ADC) at
`~/.config/gcloud/application_default_credentials.json`. No env var overrides
are needed — gws discovers ADC automatically when no keyring credentials exist.

### Setup

1. Login with the required scopes:
   ```
   gcloud auth application-default login \
     --scopes="openid,https://www.googleapis.com/auth/userinfo.email,https://www.googleapis.com/auth/cloud-platform,https://www.googleapis.com/auth/documents,https://www.googleapis.com/auth/drive.file,https://www.googleapis.com/auth/drive.readonly,https://www.googleapis.com/auth/calendar.readonly,https://www.googleapis.com/auth/gmail.readonly,https://www.googleapis.com/auth/spreadsheets.readonly,https://www.googleapis.com/auth/presentations.readonly"
   ```

2. Set the quota project:
   ```
   gcloud auth application-default set-quota-project etsy-codemosaic-sandbox
   ```

### Known issues with gws auth

**gws `+` helper commands don't forward the quota project header with ADC.**

When using ADC (not service-account credentials), gws helper commands like
`drive +search` and `drive +tree` fail with 403 "Request had insufficient
authentication scopes". This happens because the helper commands construct their
own HTTP requests and don't include the `x-goog-user-project` header that the
low-level `drive files list` path adds automatically.

**Workaround:** Use the low-level API directly (`drive files list --params '...'`)
instead of the helper commands. All moxin tools already do this.

### Auth priority

gws checks credentials in this order:

0. `GOOGLE_WORKSPACE_CLI_TOKEN` env var (raw access token)
1. `GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE` env var (plaintext JSON)
2. `~/.config/gws/credentials.enc` (encrypted)
3. `~/.config/gws/credentials.json` (plaintext)
4. ADC (`GOOGLE_APPLICATION_CREDENTIALS` or `~/.config/gcloud/application_default_credentials.json`)

Position 2 (`credentials.enc`) takes priority over ADC. If gws was previously
authenticated via `gws auth login`, its keyring credentials will be used instead
of ADC. Run `gws auth logout` and delete `token_cache.json` to clear them.
