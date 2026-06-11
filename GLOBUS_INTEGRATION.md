# Globus Integration Details

**Navigation:** [← Back to README](README.md#high-performance-globus-transfers)

## Table of Contents

- [Overview](#overview)
- [Feature Comparison](#feature-comparison)
- [Preview URL Support](#preview-url-support)
- [Scoped Institutional Login](#scoped-institutional-login)
- [Transfer Progress Monitoring](#transfer-progress-monitoring)
- [How It Works](#how-it-works)
- [Authentication Flow](#authentication-flow)
- [Limitations](#limitations)
- [Configuration](#configuration)

[↑ Back to Top](#globus-integration-details)

---

## Overview

This document provides a technical overview of the Globus integration, including capabilities, limitations, and how it differs from the official `dataverse-globus` app.

The rdm-integration Globus plugin supports both uploads and downloads via managed Globus transfers for S3-backed storage. A dedicated download component provides a streamlined flow for large file retrieval and can be wired as a separate Dataverse external tool.

[↑ Back to Top](#globus-integration-details) | [→ Feature Comparison](#feature-comparison)

---

## Feature Comparison

| Feature | rdm-integration | dataverse-globus app |
|---------|-----------------|----------------------|
| **Authentication** | | |
| Globus uploads to S3 storage | ✅ | ✅ |
| Globus downloads from S3 storage | ✅ | ✅ |
| Preview URL support (General) | ✅ **Unique** | ❌ |
| Preview URL support (Anonymous) | ❌ (Dataverse limitation) | ❌ |
| Guest download (public datasets) | ✅ | ✅ |
| Authenticated user download | ✅ | ✅ |
| Globus OAuth | Redirect with client secret | PKCE (v2 branch) |
| Dataverse auth | Shibboleth / OIDC | Signed URLs |
| **Scoped Institutional Login** | | |
| `session_required_single_domain` support | ✅ | ❌ |
| Access to institutional endpoints (e.g., HPC) | ✅ (for logged-in users) | ❌ |
| Scope removal for guest/preview access | ✅ (automatic) | N/A |
| **Transfer Monitoring** | | |
| Real-time transfer progress polling | ✅ | ❌ |
| Progress percentage display | ✅ | ❌ |
| Transfer status in UI | ✅ (ACTIVE/SUCCEEDED/FAILED) | ❌ |
| Link to Globus web app for monitoring | ✅ | ✅ |
| **User Interface** | | |
| Hierarchical tree view for files | ✅ (PrimeNG TreeTable) | Flat list per folder¹ |
| Color-coded file selection | ✅ (CSS variables for light/dark) | ❌ |
| Folder selection (select all children) | ✅ (recursive) | ❌² |
| Toggle all files at once | ✅ | ✅ (checkbox) |
| Destination folder tree navigation | ✅ (expandable tree) | ✅ (list navigation) |
| Multiple endpoint search tabs | ❌ | ✅ (Personal/Recent/Search) |
| DOI dropdown with search | ✅ | ❌ (passed via callback) |
| **Performance** | | |
| Endpoint file listing strategy | Recursive upfront (full tree built before compare view) | On-demand per folder (navigate into folder to list) |
| Initial load time for large endpoints | ⚠️ Slow (all folders listed upfront via Globus API) | ✅ Fast (only lists current folder) |
| **Maintenance** | | |
| Active development | ✅ | ⚠️ (v2 branch merged ~mid-2025) |
| Latest Angular version | ✅ (Angular 21) | Angular 17 (v2 branch) |
| Regular security updates | ✅ | ❌ |

¹ Uses `mat-selection-list` with double-click navigation into subdirectories
² Has "Select All" checkbox but only for visible items in current folder, not recursive

[↑ Back to Top](#globus-integration-details) | [→ Preview URL Support](#preview-url-support)

---

## Preview URL Support

This integration supports **General Preview URLs** for Globus downloads from draft datasets. This enables:
- Reviewers to download draft dataset files via Globus
- Collaborators without Dataverse accounts to access data
- External validators to retrieve files before publication

**Important**: **Anonymous Preview URLs are NOT supported** due to Dataverse's `ApiKeyAuthMechanism` which blocks anonymized tokens from accessing Globus APIs. This is a Dataverse security feature for blind peer review.

**Detailed documentation:** [preview_urls.md](preview_urls.md)

[↑ Back to Top](#globus-integration-details) | [→ Scoped Institutional Login](#scoped-institutional-login)

---

## Scoped Institutional Login

When configured with `session_required_single_domain` (e.g., `kuleuven.be`), logged-in users are required to authenticate with their institutional identity at Globus. This enables access to institutional Globus endpoints such as:

- HPC storage endpoints
- Research group shared storage
- Institutional data repositories

**For guest and preview URL users**, the scope restriction is automatically removed, allowing them to use any Globus identity (personal or institutional) to complete the transfer.

**Example:**
```
Globus OAuth URL for logged-in users:
https://auth.globus.org/v2/oauth2/authorize?scope=...&session_required_single_domain=kuleuven.be

Globus OAuth URL for guest/preview users (scope stripped):
https://auth.globus.org/v2/oauth2/authorize?scope=...
```

[↑ Back to Top](#globus-integration-details) | [→ Transfer Progress Monitoring](#transfer-progress-monitoring)

---

## Transfer Progress Monitoring

The download component includes real-time transfer progress monitoring:

- **Automatic polling**: Status checked every 5 seconds while transfer is active
- **Progress bar**: Shows percentage complete based on bytes transferred
- **Status display**: ACTIVE → SUCCEEDED/FAILED/CANCELED
- **External link**: Direct link to Globus web app for detailed monitoring
- **Completion callback**: UI updates automatically when transfer finishes

[↑ Back to Top](#globus-integration-details) | [→ How It Works](#how-it-works)

---

## How It Works

Unlike the official `dataverse-globus` app which relies on Dataverse's signed URL mechanism, this integration:

1. **Extracts** the preview token from the URL provided by the user
2. **Passes** the token to the backend as a Dataverse API key
3. **Calls** Dataverse APIs directly with `X-Dataverse-key: {previewToken}`

This bypasses the signed URL limitation where preview users (who are virtual `PrivateUrlUser` objects not stored in the database) cannot have signed URLs generated for them.

[↑ Back to Top](#globus-integration-details) | [→ Authentication Flow](#authentication-flow)

---

## Authentication Flow

```
┌──────────────────────────────────────────────────────────────────────┐
│  1. User clicks "Globus Download" in Dataverse                       │
│     └─→ Callback URL contains datasetDbId and downloadId             │
│                                                                      │
│  2. User sees login options popup:                                   │
│     ├─ "Continue as guest" (public files only)                       │
│     ├─ "Continue with preview URL" ← pastes General Preview URL      │
│     └─ "Log in" (institutional SSO)                                  │
│                                                                      │
│  3. Token extracted from preview URL and preserved in OAuth state    │
│                                                                      │
│  4. User authenticates with Globus (OAuth redirect)                  │
│                                                                      │
│  5. Backend receives: Globus token + Dataverse preview token         │
│     └─→ Calls Dataverse APIs with preview token                      │
│     └─→ Initiates Globus transfer with Globus token                  │
└──────────────────────────────────────────────────────────────────────┘
```

[↑ Back to Top](#globus-integration-details) | [→ Limitations](#limitations)

---

## Limitations

| Limitation | Reason | Workaround |
|------------|--------|------------|
| Anonymous Preview URLs don't work | Dataverse blocks anonymized tokens for Globus APIs | Use General Preview URL |
| Preview users can't use signed URLs | `PrivateUrlUser` not in database, no `ApiToken` | Direct API calls with token |
| Requires Globus app registration | OAuth flow needs client ID and secret | Register at auth.globus.org |
| Slow initial load for large endpoints | The hierarchical compare view requires recursively listing all files and folders in the pre-selected Globus endpoint path via the Globus Transfer API. Endpoints with thousands of files or deeply nested directories cause noticeable delays. | Advise users to pre-select a narrow source path; the Globus Transfer API itself is the bottleneck |

[↑ Back to Top](#globus-integration-details) | [→ Configuration](#configuration)

---

## Configuration

`rdm-integration` uses a server-side OAuth token exchange for Globus. Configure
it with a Globus **Confidential Client**:

1. Open `https://app.globus.org`.
2. Go to **Settings** -> **Developers**.
3. Add or select a project.
4. In that project, open **Apps** and add an app.
5. Add a **Confidential Client** to that app.

Use the Confidential Client ID as `tokenGetter.oauth_client_id`. The matching
client secret is stored in the backend OAuth secrets file, not in
`frontend_config.json`.

This is not the native/PKCE client flow used by some Globus integrations. It is
closer to a portal/science-gateway setup because the backend exchanges the
authorization code with the client secret.

### Frontend plugin configuration

The Globus plugin entry belongs in `frontend_config.json`, under `plugins`.
Use the `tokenGetter.URL` field for the Globus authorization URL:

```json
{
  "plugins": [
    {
      "id": "globus",
      "name": "Globus",
      "plugin": "globus",
      "pluginName": "Globus",
      "sourceUrlFieldValue": "https://transfer.api.globusonline.org/v0.10",
      "optionFieldName": "Folder",
      "optionFieldPlaceholder": "Select folder",
      "optionFieldInteractive": true,
      "repoNameFieldName": "Endpoint",
      "repoNameFieldPlaceholder": "Select endpoint",
      "repoNameFieldHasSearch": true,
      "tokenGetter": {
        "URL": "https://auth.globus.org/v2/oauth2/authorize?scope=urn%3Aglobus%3Aauth%3Ascope%3Atransfer.api.globus.org%3Aall+openid+email+profile",
        "oauth_client_id": "YOUR_GLOBUS_CLIENT_ID"
      }
    }
  ]
}
```

For institution-scoped login, add the Globus `session_required_single_domain`
parameter to the authorization URL, for example:

```text
&session_required_single_domain=kuleuven.be
```

Preview and guest download flows remove this restriction automatically so
external reviewers can use a non-institutional Globus identity.

### Backend OAuth secrets

`backend_config.json` should point to an OAuth secrets file:

```json
{
  "options": {
    "pathToOauthSecrets": "/dsdata/oauth/secrets.json"
  }
}
```

The secrets file maps the Globus Confidential Client ID to the token endpoint
and client secret:

```json
{
  "YOUR_GLOBUS_CLIENT_ID": {
    "postURL": "https://auth.globus.org/v2/oauth2/token",
    "clientSecret": "YOUR_GLOBUS_CLIENT_SECRET"
  }
}
```

The repository includes general local examples in `conf/backend_config.json`,
`conf/frontend_config.json`, and `conf/example_oauth_secrets.json`. These are
copied into `docker-volumes/integration/...` by `make init`, which is run
automatically by `make up` when the local volumes have not been initialized.
Those example files show the configuration structure, but currently do not
include a complete Globus example.

### Backend Globus and storage settings

For Globus uploads/downloads backed by Dataverse S3 storage, set the Dataverse
Globus endpoint in `backend_config.json`:

```json
{
  "globusEndpoint": "YOUR_GLOBUS_ENDPOINT_UUID",
  "options": {
    "defaultDriver": "s3",
    "pathToOauthSecrets": "/dsdata/oauth/secrets.json",
    "s3Config": {
      "awsEndpoint": "https://s3.example.org",
      "awsRegion": "us-east-1",
      "awsPathstyle": true,
      "awsBucket": "dataverse-bucket"
    }
  }
}
```

### Dataverse settings

Dataverse must also be configured for Globus. In the Dataverse source, the
Globus upload and download UI checks are still gated by `:UploadMethods` and
`:DownloadMethods` containing `globus`, respectively. Configure these with the
standard Dataverse settings API, for example:

```bash
curl -X PUT -d 'native/http,dvwebloader,globus' \
  http://localhost:8080/api/admin/settings/:UploadMethods

curl -X PUT -d 'native/http,globus' \
  http://localhost:8080/api/admin/settings/:DownloadMethods

curl -X PUT -d 'https://your-rdm-integration-host/connect' \
  http://localhost:8080/api/admin/settings/:GlobusAppUrl

curl -X PUT -d 50 \
  http://localhost:8080/api/admin/settings/:GlobusPollingInterval

curl -X PUT -d false \
  http://localhost:8080/api/admin/settings/:GlobusSingleFileTransfer
```

The Dataverse public guides currently document `:UploadMethods`,
`:GlobusAppUrl`, `:GlobusPollingInterval`, `:GlobusSingleFileTransfer`, and
`:GlobusBatchLookupSize`. The `:DownloadMethods` documentation has moved around
and may be incomplete in current guides, but the Dataverse code still uses it to
enable Globus dataset/file download actions. Check your Dataverse version if
you are not running a recent 6.x build.

See the Dataverse installation guide for the current settings API examples,
Globus settings, and the Dataverse Globus API documentation for the
upload/download transfer flow:

- [Dataverse configuration settings](https://guides.dataverse.org/en/latest/installation/config.html)
- [Dataverse Globus Transfer API](https://guides.dataverse.org/en/latest/developers/globus-api.html)

The Dataverse storage configuration must also align with the Globus endpoint.
In deployments this is typically set through JVM or MicroProfile properties,
including:

```text
-Ddataverse.files.s3.download-redirect=true
-Ddataverse.files.s3.upload-redirect=true
-Ddataverse.files.s3.managed=true
-Ddataverse.files.s3.transfer-endpoint-with-basepath=YOUR_GLOBUS_ENDPOINT_UUID
-Ddataverse.files.s3.globus-token=BASE64_CLIENT_ID_COLON_CLIENT_SECRET
-Ddataverse.files.s3.upload-out-of-band=true
```

`dataverse.files.s3.globus-token` is Dataverse's storage-side Globus credential
and is separate from the user OAuth client configured for `rdm-integration`.
It is the base64 encoding of the Globus client ID and secret in the form
`client_id:client_secret`.

**Backend configuration guide:** [README.md#backend-configuration](README.md#backend-configuration)
**Frontend configuration guide:** [README.md#frontend-configuration](README.md#frontend-configuration)

[↑ Back to Top](#globus-integration-details)

---

## Related Documentation

- [Preview URL Support](preview_urls.md) — Detailed technical documentation on preview URL authentication
- [README.md](README.md) — Main project documentation

[↑ Back to Top](#globus-integration-details) | [← Back to README](README.md)

---

## License

This project is licensed under the Apache License, Version 2.0. See [LICENSE.txt](LICENSE.txt) for details.
