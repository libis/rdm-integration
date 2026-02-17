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
| Guest download (public datasets) | ✅ | ❌ |
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

[↑ Back to Top](#globus-integration-details) | [→ Configuration](#configuration)

---

## Configuration

Globus plugin configuration in `backend_config.json`:

```json
{
  "plugins": [
    {
      "id": "globus",
      "plugin": "globus",
      "tokenGetter": {
        "authURL": "https://auth.globus.org/v2/oauth2/authorize",
        "oauth_client_id": "YOUR_GLOBUS_CLIENT_ID"
      }
    }
  ]
}
```

The client secret is stored separately in the OAuth secrets file (see `pathToOauthSecrets` in backend configuration).

**Backend configuration guide:** [README.md#backend-configuration](README.md#backend-configuration)

[↑ Back to Top](#globus-integration-details)

---

## Related Documentation

- [Preview URL Support](preview_urls.md) — Detailed technical documentation on preview URL authentication
- [README.md](README.md) — Main project documentation

[↑ Back to Top](#globus-integration-details) | [← Back to README](README.md)

---

## License

This project is licensed under the Apache License, Version 2.0. See [LICENSE.txt](LICENSE.txt) for details.
