# Globus Dynamic Re-authentication — Design

**Date:** 2026-08-10
**Repos:** `rdm-integration` (Go backend), `rdm-integration-frontend` (Angular)
**Status:** Approved

## Problem

The Globus plugin's login is static: the authorize URL is pinned in config with a
single `session_required_single_domain=kuleuven.be` and a fixed scope list. Two
classes of failures observed in production:

1. **Missing per-collection `data_access` consent** (VSC HPC, ManGO GHUM).
   Globus GCS v5 mapped collections return `ConsentRequired` +
   `required_scopes`. The backend wraps this in a `*scopes*...*scopes*` marker
   inside a plain-text 500. The connect component parses the marker and
   re-authorizes; the **download component does not** — users see
   `Branch lookup failed: 500 - *scopes*urn:globus:auth:scope:...` raw.

2. **Identity-domain policy on foreign endpoints** (sydney.edu.au).
   Domain-restricted destination endpoints fail with `LOGIN_DENIED` /
   `530-Login incorrect` carrying
   `authorization_parameters.session_required_single_domain: ["sydney.edu.au"]`.
   Nothing detects this anywhere; users only see a cryptic error in the Globus
   activity monitor.

3. **Latent bug:** when the connect component re-authorizes with required
   scopes, it *replaces* the entire `scope=` parameter, dropping
   `openid email profile`. The token cache then lacks an auth.globus.org token,
   so `getPrincipal` (userinfo call, `streams.go`) fails until the user logs in
   fresh. Scopes must be merged, not replaced.

## Decisions (made during brainstorming)

- **Recovery surface:** browse/submit-time only. Detect errors on every Globus
  API call the backend makes (ls, endpoint lookup, search, submission_id,
  transfer submission, userinfo). Task-event monitoring of already-submitted
  transfers is out of scope.
- **Error channel:** structured HTTP error responses (not string markers in the
  frontend contract). Backend returns `401` + JSON; string markers survive only
  as an internal serialization detail and a legacy fallback.

## Design

### 1. Backend: unified Globus error interpretation

`DoGlobusRequest` (`image/app/plugin/impl/globus/common.go`) currently ignores
HTTP status codes. Change: on status >= 400, parse the error body:

```json
{
  "code": "...",
  "message": "...",
  "required_scopes": ["..."],
  "authorization_parameters": {
    "session_required_single_domain": ["..."],
    "session_message": "..."
  }
}
```

Detection rules, in one shared interpreter:

- `code == "ConsentRequired"` → `RequiredScopes` from `required_scopes`.
- Top-level `authorization_parameters.session_required_single_domain` →
  `RequiredDomains` (GCS v5 session policies).
- GridFTP 530 text embedding: scan `message` for `GridFTP-JSON-Result: {...}`
  (or a bare `"session_required_single_domain": [...]` fragment) and parse the
  embedded JSON → `RequiredDomains`. Covers `ExternalError.DirListingFailed.*`
  from GridFTP-backed endpoints (the Sydney case).

On match, return a typed error:

```go
type ReauthError struct {
    RequiredScopes  []string
    RequiredDomains []string
    Message         string // session_message when present
}
```

`ReauthError.Error()` serializes as `*reauth*{json}*reauth*` so the value
survives string round-trips through the Redis job plumbing (async compare/store
paths) and can be re-parsed at the HTTP boundary. A `ParseReauthError(string)`
helper does the reverse.

The existing `ConsentRequired` → `*scopes*` special case in
`getPartialResponse` collapses into this. Non-reauth errors keep today's
`"Code: Message"` text form.

### 2. Backend: HTTP boundary writer

One shared helper:

```go
// WriteError writes err to w. ReauthError (typed via errors.As, or embedded
// as a *reauth* marker in the error string) becomes HTTP 401 with a JSON
// body; anything else keeps the existing "500 - %v" plain-text behavior.
func WriteError(w http.ResponseWriter, err error)
```

Reauth response contract:

```
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{"reauth": {"required_scopes": [...], "required_domains": [...], "message": "..."}}
```

Applied in every handler where Globus errors can surface:

- `plugin/funcs/options` (folder browse — both flows)
- `plugin/funcs/search` (endpoint search)
- `plugin/funcs/compare`
- `common/compare.go` — `GetCachedResponse` (parses the marker out of the
  cached `ErrorMessage` string) and `Compare`
- `common/download.go` — `Download`, `GlobusTransferStatus`
- `common/store.go`

### 3. Frontend: shared reauth extraction

A shared utility (replacing per-component string matching):

```ts
interface ReauthRequest { scopes?: string[]; domains?: string[]; message?: string }
function extractReauth(err: unknown): ReauthRequest | undefined
```

- Primary: `err.status === 401 && err.error?.reauth` → map fields.
- Legacy fallback: string body containing `*scopes*...*scopes*` (covers
  version skew between separately deployed backend/frontend).

Called **first** in every relevant error handler:

- **connect component:** `getOptions`, repo search, compare polling —
  replaces `handleScopesError`.
- **download component:** `getOptions`, repo search, download submission,
  polling — currently has no handling at all (the reported bug).

When extraction succeeds, the component calls its `getRepoToken(reauth)`.

### 4. Frontend: authorize URL construction

Shared `buildAuthorizeUrl(baseUrl, { scopes, domains, guestMode })` used by both
components' `getRepoToken`:

- **Scope merge (not replace).** Parse the configured scope list
  (`transfer:all openid email profile`). Each required scope replaces the
  configured entry whose base it extends (match by prefix before `[`);
  unmatched required scopes are appended; all other configured entries are
  preserved. Result example:
  `transfer:all[*https://auth.globus.org/scopes/<id>/data_access] openid email profile`.
  No accumulation across successive errors is needed: Globus consents persist
  server-side per user+client (which is why one user reproduces a consent
  error and another does not).
- **Domains.** When `domains` present:
  `session_required_single_domain=<domains comma-joined>` **replaces** any
  configured value (the endpoint demands those domains; the configured
  `kuleuven.be` cannot satisfy it), and `prompt=login` is appended so Globus
  forces fresh authentication with the required identity instead of silently
  reusing the SSO session.
- **Guest/preview stripping** of `session_required_single_domain` remains, but
  only when no explicit `domains` were demanded by an error.
- The download component's `getRepoToken()` gains the optional reauth
  parameter; state preservation across the redirect via `LoginState` is
  already in place in both components.

### 5. Testing

- **Go:** interpreter unit tests with real payload fixtures (the Sydney 530
  GridFTP text, a `ConsentRequired` body, an `authorization_parameters` body,
  plain non-reauth errors); marker round-trip (`Error()` ↔
  `ParseReauthError`); `WriteError` behavior for typed, marker-embedded, and
  plain errors; options-handler propagation test.
- **Angular:** `extractReauth` (structured 401, legacy marker, non-reauth
  errors); `buildAuthorizeUrl` (scope merge, domain replace + `prompt=login`,
  guest-strip interplay); regression spec: download `getOptions` receiving the
  401 reauth payload triggers re-login instead of showing
  "Branch lookup failed".

### 6. Documentation

Update `GLOBUS_INTEGRATION.md`: describe dynamic consent/domain recovery,
the 401 reauth contract, and the revised meaning of the configured
`session_required_single_domain` (initial default for logged-in users, no
longer a hard limit).

## Out of scope

- Detecting failures of already-submitted transfer tasks via task events, and
  any cancel/re-submit flow.
- Config format changes; `kuleuven.be` stays the configured initial default.
- Anonymous preview URL limitations (Dataverse-side).
