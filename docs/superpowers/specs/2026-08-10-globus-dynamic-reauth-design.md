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
- **Config shape:** structured `tokenGetter` fields (`scopes`,
  `session_required_single_domain`) with the legacy URL-with-params form still
  supported via frontend normalization. No forced config migration.

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
`"Code: Message"` text form. This format is load-bearing: the folder-landing
candidate loop in `options.go` (`resolveAndBuildInitialTree`) classifies
errors by string — `isNotFoundError` matches `ClientError.NotFound` /
`EndpointNotFound` to advance to the next candidate path, and a reauth error
must survive as `lastErr` so it propagates when every candidate fails. The
folder-resolution logic itself (candidate order, `meaningful`-path detection,
per-endpoint-type quirks for Windows/Linux/macOS personal endpoints, VSC,
iRODS) is intentionally untouched by this design.

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

### 4. Config: structured token-getter settings

Today the entire authorize URL — including `scope=` and
`session_required_single_domain=kuleuven.be` — is baked into `tokenGetter.URL`
as one string, forcing regex/substring surgery in the frontend. The config
gains explicit optional fields, with the legacy form still supported:

```json
"tokenGetter": {
    "URL": "https://auth.globus.org/v2/oauth2/authorize",
    "oauth_client_id": "33992ba4-...",
    "scopes": ["urn:globus:auth:scope:transfer.api.globus.org:all", "openid", "email", "profile"],
    "session_required_single_domain": ["kuleuven.be"]
}
```

- Go `config.TokenGetter` (`image/app/config/frontend_config.go`) gains
  `Scopes []string` (`json:"scopes,omitempty"`) and
  `SessionRequiredSingleDomain []string`
  (`json:"session_required_single_domain,omitempty"`), so the fields pass
  through `/api/frontend/config` to the frontend. `omitempty` keeps served
  config byte-identical for installs that don't set them.
- Frontend `plugin.service` normalizes every token getter at load into one
  canonical model: `{ authorizeUrl, oauth_client_id, baseScopes[], baseDomains[] }`.
  Structured fields are preferred; **fallback:** when absent, `scope` and
  `session_required_single_domain` query params are parsed out of `URL`
  (scope split on spaces, domain split on commas) and removed from the base
  URL; any other query params in the URL are preserved. Existing production
  configs keep working unmigrated.
- The domain value is a list in config and comma-joined in the URL (Globus
  semantics: an identity from *one* of the listed domains satisfies it).
- Migration of the production config in `rdm-deployment` is a follow-up, not
  part of this change.

### 5. Frontend: authorize URL construction

Shared `buildAuthorizeUrl(base, { scopes, domains, guestMode })` — where
`base` is the normalized model from section 4 — used by both components'
`getRepoToken`:

- **Scope merge (not replace).** Start from `baseScopes`. Each required scope
  replaces the base entry whose base it extends (match by prefix before `[`);
  unmatched required scopes are appended; all other base entries are
  preserved. Result example:
  `transfer:all[*https://auth.globus.org/scopes/<id>/data_access] openid email profile`.
  No accumulation across successive errors is needed: Globus consents persist
  server-side per user+client (which is why one user reproduces a consent
  error and another does not).
- **Domains.** When `domains` present (from a reauth error):
  `session_required_single_domain=<domains comma-joined>` **replaces**
  `baseDomains` (the endpoint demands those domains; the configured
  `kuleuven.be` cannot satisfy it), and `prompt=login` is appended so Globus
  forces fresh authentication with the required identity instead of silently
  reusing the SSO session.
- **Guest/preview mode** omits `baseDomains` (replacing today's regex
  stripping), but error-demanded `domains` are still applied even for guests.
- All parameters (`scope`, `session_required_single_domain`, `client_id`,
  `redirect_uri`, `response_type`, `state`, `prompt`) are appended
  programmatically — no substring surgery on configured URLs.
- The download component's `getRepoToken()` gains the optional reauth
  parameter; state preservation across the redirect via `LoginState` is
  already in place in both components.

### 6. Testing

- **Go:** interpreter unit tests with real payload fixtures (the Sydney 530
  GridFTP text, a `ConsentRequired` body, an `authorization_parameters` body,
  plain non-reauth errors); marker round-trip (`Error()` ↔
  `ParseReauthError`); `WriteError` behavior for typed, marker-embedded, and
  plain errors; options-handler propagation test. Regression guard for the
  folder-landing fallback: an HTTP 404 body must still produce a
  `ClientError.NotFound...` error string (so `isNotFoundError` keeps advancing
  candidates), and a ReauthError raised on every candidate must propagate out
  of `resolveAndBuildInitialTree` as the returned error.
- **Angular:** `extractReauth` (structured 401, legacy marker, non-reauth
  errors); token-getter normalization (structured fields preferred, legacy
  URL-with-params parsed, other query params preserved); `buildAuthorizeUrl`
  (scope merge, domain replace + `prompt=login`, guest-mode interplay);
  regression spec: download `getOptions` receiving the 401 reauth payload
  triggers re-login instead of showing "Branch lookup failed".
- **Go config:** `TokenGetter` round-trip test — structured fields survive
  unmarshal/marshal through `/api/frontend/config`, and configs without them
  serialize unchanged.

### 7. Documentation

Update `GLOBUS_INTEGRATION.md`: describe dynamic consent/domain recovery,
the 401 reauth contract, the structured `tokenGetter` fields (with the legacy
URL form documented as still supported), and the revised meaning of the
configured `session_required_single_domain` (initial default for logged-in
users, no longer a hard limit).

## Out of scope

- Detecting failures of already-submitted transfer tasks via task events, and
  any cancel/re-submit flow.
- Migrating the production config in `rdm-deployment` to the structured
  fields (follow-up; the legacy URL form keeps working). `kuleuven.be` stays
  the configured initial default for logged-in users.
- Anonymous preview URL limitations (Dataverse-side).
- Folder/landing-directory resolution (`resolveAndBuildInitialTree` candidate
  logic and endpoint-type quirks). Believed fixed and orthogonal: the
  user-to-user difference observed on ManGO GHUM (one user hits the consent
  error, another does not) is explained by durable per-user Globus consents,
  not by home-directory differences.
