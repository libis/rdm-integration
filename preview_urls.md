# Preview URL Support for Globus Downloads

This document describes how preview URL users can download files from draft datasets via Globus.

## Overview

Preview URL support enables users who only have a Dataverse preview URL (not a full account) to download files from draft datasets using Globus. This is useful for:

- Reviewers evaluating datasets before publication
- Collaborators accessing draft data without Dataverse accounts
- External validators checking data before acceptance

**Status**: ✅ Implemented and tested (2026-01-08)

---

## Important Limitations

### Supported Preview URL Types

| Preview URL Type | Description | Globus Support |
|------------------|-------------|----------------|
| **General Preview URL** | Full access token, user appears as `#<datasetId>` | ✅ **Fully supported** |
| **Anonymous Preview URL** | Anonymized token for blind peer review | ❌ **Not supported** |

### Why Anonymous Preview URLs Don't Work

Anonymous (anonymized) preview URLs are intentionally restricted by Dataverse's `ApiKeyAuthMechanism.checkAnonymizedAccessToRequestPath()` to only allow access to `/access/datafile/{id}` endpoints. This is a security feature for blind peer review - the anonymized token cannot:

- Call `globusDownloadParameters` API
- Call `requestGlobusDownload` API  
- Access file listing or dataset metadata APIs

**Workaround**: Dataset owners should create a **General Preview URL** instead of an Anonymous Preview URL when Globus downloads are needed.

---

## User Flow

### For Preview URL Users

1. Open the preview URL received from the dataset owner
2. Click "Globus Download" button in Dataverse
3. A popup appears with three options:
   - **Continue as guest** — Only works for public files
   - **Continue with preview URL** — Paste your preview URL here
   - **Log in** — Redirect to institutional login
4. Paste the preview URL into the input field
5. Click "Continue with preview URL"
6. Complete Globus OAuth login (if not already authenticated)
7. Select files and destination, then start transfer

---

## Technical Architecture

### Token Types

| Token Type | Format | Purpose |
|------------|--------|---------|
| **General Preview Token** | UUID (36 chars) | Authenticate Dataverse API calls for draft access |
| **Anonymous Preview Token** | UUID (36 chars) | Limited access for blind review (NOT for Globus) |
| **Globus OAuth Token** | Opaque string | Authenticate Globus file transfers |
| **Signed URL Token** | SHA512 (128 hex) | Validate callback URL integrity (NOT for auth) |

### Data Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              USER JOURNEY                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. Dataverse                    2. Integration App                         │
│  ┌──────────────┐               ┌───────────────────────────────┐          │
│  │ Preview URL  │──callback────▶│ Parse callback                │          │
│  │ + Globus btn │   (base64)    │ Extract: datasetDbId,         │          │
│  └──────────────┘               │          downloadId            │          │
│                                 └───────────────┬───────────────┘          │
│                                                 │                           │
│  3. Guest Login Popup                           ▼                           │
│  ┌──────────────────────────────────────────────────────────────┐          │
│  │  ┌─────────────────┐  ┌─────────────────────┐  ┌──────────┐  │          │
│  │  │ Continue as     │  │ Continue with       │  │ Log in   │  │          │
│  │  │ guest           │  │ preview URL         │  │          │  │          │
│  │  └─────────────────┘  └──────────┬──────────┘  └──────────┘  │          │
│  │                                  │                            │          │
│  │  [Paste preview URL here: ______________________________ ]    │          │
│  └──────────────────────────────────┼───────────────────────────┘          │
│                                     │                                       │
│  4. Token Extraction                ▼                                       │
│  ┌──────────────────────────────────────────────────────────────┐          │
│  │ Extract token from: previewurl.xhtml?token=abc-123-def       │          │
│  │ Store in OAuth state for preservation across redirect         │          │
│  └───────────────────────────────────┬──────────────────────────┘          │
│                                      │                                      │
│  5. Globus OAuth                     ▼                                      │
│  ┌──────────────────┐       ┌────────────────┐                             │
│  │ Globus Login     │◀──────│ Redirect with  │                             │
│  │ (user auths)     │       │ state={token}  │                             │
│  └────────┬─────────┘       └────────────────┘                             │
│           │                                                                 │
│           ▼                                                                 │
│  6. Return & Download                                                       │
│  ┌──────────────────────────────────────────────────────────────┐          │
│  │ OAuth callback → Restore token from state                     │          │
│  │ Call backend with token → Get file list                       │          │
│  │ User selects files → Start Globus transfer                    │          │
│  └──────────────────────────────────────────────────────────────┘          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Key API Endpoints

| Endpoint | Auth Required | Works for Preview URL | Notes |
|----------|---------------|----------------------|-------|
| `GET /api/datasets/{id}/globusDownloadParameters` | **No** (guest allowed) | ✅ Yes | Returns DOI, endpoint, file IDs. Guest users get unsigned URLs. |
| `POST /api/datasets/{id}/requestGlobusDownload` | **Yes** | ✅ General only | Anonymous tokens blocked by `ApiKeyAuthMechanism` |
| `GET /api/datasets/{id}/versions/:draft/files` | **Yes** | ✅ General only | File listing for draft versions |
| `GET /api/users/:me` | **Yes** | ✅ General only | Returns `#<datasetId>` for preview users |

### Why This Implementation Works

The standard `dataverse-globus` app relies on Dataverse's **signed URL** mechanism for authentication. However, signed URLs require:

1. An `AuthenticatedUser` in the database (to get `getUserIdentifier()`)
2. An `ApiToken` object (to get `getTokenString()`)

**Preview URL users have neither** - they are virtual `PrivateUrlUser` objects that don't exist in the `authenticateduser` table. When Dataverse generates signed URLs for preview users, `apiToken` is `null`, so URLs are returned **unsigned** (guest mode), which fails for draft datasets.

**Our solution bypasses signed URLs entirely:**

1. Extract the preview token from the URL
2. Pass it to the rdm-integration backend
3. Backend calls Dataverse APIs directly with `X-Dataverse-key: {previewToken}`

This works because the preview token IS a valid API key for draft access - it just can't be used for URL signing.

### How DOI Retrieval Works

The `globusDownloadParameters` endpoint has special exception handling:

```java
// Datasets.java line 4319-4323
AuthenticatedUser authUser = null;
try {
    authUser = getRequestAuthenticatedUserOrDie(crc);
} catch (WrappedResponse e) {
    logger.fine("guest user globus download");  // Continues as guest!
}
```

This allows guest users (and preview URL users) to call the API and retrieve:
- `datasetPid` (DOI)
- `endpoint` (Globus endpoint ID)
- `files` (map of file IDs to paths)

The DOI is in the response regardless of authentication status, which is why our frontend can fetch it before the user provides their preview token.

### OAuth State Preservation

The preview token must survive the Globus OAuth redirect. This is achieved by including it in the OAuth state parameter:

```typescript
interface LoginState {
  datasetId: DatasetId;
  downloadId: string;
  accessMode: 'guest' | 'preview' | 'user';
  dataverseToken?: string;  // Preview token preserved here
  // ... other fields
}
```

The state is base64-encoded and passed through the OAuth flow, then decoded on callback.

---

## Configuration

### `storeDvToken` Setting

Controls whether the Dataverse token is persisted in localStorage:

| Value | Behavior |
|-------|----------|
| `true` | Token saved to localStorage + passed in OAuth state |
| `false` | Token ONLY passed in OAuth state (no localStorage) |

**Recommendation**: Use `false` for better security (token doesn't persist in browser).

---

## Implementation Details

### Frontend Changes (`rdm-integration-frontend`)

1. **`app.component.ts`** — Parse callback, extract `datasetDbId` and `downloadId`
2. **`download.component.ts`** — Handle preview URL input, preserve token in OAuth state
3. **`data.service.ts`** — Pass token to backend API calls

### Backend Changes (`rdm-integration`)

1. **`download.go`** — Accept `dataverseKey` from request, pass to `StreamParams.DVToken`
2. **`streams.go`** — Use `DVToken` when calling Dataverse's `requestGlobusDownload` API

---

## Troubleshooting

### Common Issues

| Symptom | Cause | Solution |
|---------|-------|----------|
| "Bad API key" error | Using Anonymous Preview URL | Create a **General Preview URL** instead |
| "User doesn't have permission" error | Token not passed to backend | Ensure `dataverseKey` is in request body |
| File list is empty | Token invalid or expired | Generate new preview URL |
| DOI shows as "?" | DOI fetch failed | Check `globusDownloadParameters` API call |
| OAuth loop (keeps redirecting) | DOI not fetched before redirect | Ensure DOI is fetched first |

### How to Identify Preview URL Type

| URL Pattern | Type |
|-------------|------|
| `/privateurl.xhtml?token=...` | General Preview URL ✅ |
| `/previewurl.xhtml?token=...` | Could be either - test with `/api/users/:me` |

Test with curl:
```bash
# General Preview URL - returns user info
curl -H "X-Dataverse-key: YOUR_TOKEN" "https://dataverse.example.org/api/users/:me"
# Returns: {"data":{"identifier":"#12345",...}}

# Anonymous Preview URL - returns "Bad API key"
# Returns: {"status":"ERROR","message":"Bad API key"}
```

### Debug Logging

Enable console debug logging by checking browser console for:

```
[DownloadComponent] continueWithPreviewUrl called
[DownloadComponent] Token set: abc-123-def...
[DownloadComponent] getRepoToken loginState: { dataverseToken: "abc-..." }
```

---

## Comparison with dataverse-globus App

The official `dataverse-globus` app (https://github.com/scholarsportal/dataverse-globus) does **not** support Preview URL downloads. Here's why:

| Aspect | dataverse-globus | rdm-integration |
|--------|------------------|-----------------|
| **Auth for Dataverse** | Signed URLs from callback | Direct API calls with token |
| **Preview URL support** | ❌ No | ✅ General Preview URLs |
| **Anonymous Preview URL** | ❌ No | ❌ No (Dataverse limitation) |
| **Auth for Globus** | PKCE (v2 branch) or API token (main) | OAuth redirect with app secret |

The `dataverse-globus` v2 branch uses PKCE for Globus authentication (no client secret needed), while rdm-integration uses a standard OAuth redirect flow with client ID and secret registered at Globus.

---

## Security Considerations

1. **Token in URL**: The preview token is passed in the OAuth state (URL parameter). This is acceptable because:
   - HTTPS encrypts the URL in transit
   - Preview tokens are short-lived
   - OAuth state is single-use

2. **No localStorage**: When `storeDvToken: false`, tokens are never persisted, reducing exposure.

3. **Signed URL Token**: The `token` parameter in Dataverse callback URLs is a SHA512 signature, NOT an API token. Never use it for authentication.

---

## Future Improvements

- [ ] Auto-detect preview URL in clipboard
- [ ] Better error messages for invalid/expired tokens
- [ ] Token format validation (UUID check)
- [ ] E2E tests with Playwright
- [ ] User documentation

---

## Related Files

**Frontend** (`rdm-integration-frontend`):
- `src/app/app.component.ts` — Callback parsing
- `src/app/download/download.component.ts` — Download flow
- `src/app/services/data.service.ts` — API calls
- `src/app/models/plugin.ts` — `storeDvToken` config

**Backend** (`rdm-integration`):
- `image/app/common/download.go` — Request handling
- `image/app/plugin/impl/globus/streams.go` — Globus transfer logic


