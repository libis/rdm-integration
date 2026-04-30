# Globus Folder Selection — Problem & Solution

## Problems we have hit (in order they showed up)

### Problem 1: Tree doesn't visually refresh after expanding a node

PrimeNG `<p-tree>` does reference-equality check on its `[value]` binding.
When expanding a node, `handleOptionsResponse` mutated `node.children` in-place
and incremented a `refreshTrigger` signal. But `rootOptions` computed still
returned the **same array reference** from `_rootOptionsData()`, so PrimeNG
never detected the change — the user had to click again to see children.

### Problem 2: Default folder not shown in hierarchy

On initial load, the backend listed the **contents** of `DefaultDirectory`
(i.e. its children) and returned them as a flat list. The user saw the
subfolders but not the default folder itself, and couldn't navigate up to
parent directories.

### Problem 3: iRODS-backed mapped collections show only "~"

KU Leuven's `VSC iRODS ghum.irods.icts.kuleuven.be` (and likely any other
mapped collection where Globus stores the user-home shorthand verbatim
instead of resolving it) lists `/~/` successfully but returns:

```
{
  "absolute_path": "/~/",         // echoed unchanged
  "DATA": [ ... files only, no subdirs ... ]
}
```

instead of the resolved path `/ghum/home/u0050020/`. The previous fix used
`path.Dir(items[0].Value)` to extract the parent, which gave `/~`, and
`buildHierarchy("/~/", ...)` produced a single meaningless `~` node.
Symptoms reported by the user:

- The folder picker shows just `~` (looks like nothing was loaded).
- Selecting `~` was effectively "upload to my home", but the user couldn't
  tell — it might just as well be `/`. They had no path-level confirmation
  that this was their home, so they understandably refused to click submit.

This is a Globus-connector quirk we cannot fix server-side; the connector
varies endpoint by endpoint and ICTS reconfigures things without notice.
The fix has to be entirely defensive on our side.

## Solution

### Frontend fix (Problem 1)

In `handleOptionsResponse` for both `connect.component.ts` and
`download.component.ts`, replaced:

```typescript
this.refreshTrigger.update((n) => n + 1);
```

with:

```typescript
this._rootOptionsData.update((prev) => [...prev]);
```

This creates a **new array reference** via spread, so PrimeNG detects the
change and re-renders the tree immediately.

In `connect.component.ts`, `refreshTrigger` was removed entirely (it had no
other consumers). In `download.component.ts` it remains for the file-action
toggle / `action` computed signal, but `rootOptions` no longer depends on it.

### Backend fix (Problem 2 + Problem 3)

**`types/select_item.go`** — added two optional fields:

```go
Expanded bool         `json:"expanded,omitempty"`
Children []SelectItem `json:"children,omitempty"`
```

These are backward-compatible: other plugins (iRODS, OneDrive, SFTP, etc.)
return flat `[]SelectItem` arrays where `Children` and `Expanded` are
zero-valued and omitted from JSON.

**`globus/common.go`** — exposed the response's `absolute_path` from the
listing API via a new helper `listOnce(ctx, path, theUrl, token)` that
returns `(entries, absolutePath, err)`. The existing `listItems` /
`listDirEntries` / `getResponse` signatures are unchanged; they delegate
to new `*WithPath` variants that capture the field on the first response
page.

**`globus/options.go`** — rewrote `listFolderItems` as a defensive
multi-candidate resolver. The implementation comment in the file is
authoritative; in short:

1. Look up endpoint metadata. Tolerate any failure short of NotFound.
2. Build an ordered candidate list: resolved `DefaultDirectory` (if any),
   `/~/`, `/`. Deduplicated.
3. Try each candidate. NotFound => skip. Other errors => remember as
   `lastErr` and keep trying.
4. From a successful listing, derive the most authoritative resolved path:
   `response.absolute_path` (if non-empty) > parent of first child's `Id` >
   the queried path.
5. If the resolved path is **meaningful** (a real concrete absolute path —
   not `/`, not containing `~`, not containing `{...}`), build a nested
   hierarchy expanded down to it with the leaf marked `Selected: true`.
6. Otherwise, prefer the first attempt that returned a non-empty folder
   listing and present its items **flat**, with **no node marked
   `Selected`**. This is the safety net for the iRODS quirk: the user
   sees navigable folders but is never nudged into uploading to a path
   they cannot trust.
7. If nothing worked, propagate the last non-NotFound error, or return an
   empty list.

Every step logs to `logging.Logger` with the candidate, response
`absolute_path`, derived resolved path, and meaningfulness flag. When a
new endpoint variant misbehaves, those logs are the first place to look.

### How it handles all endpoint types

| Endpoint type | DefaultDirectory | response.absolute_path | Outcome |
|---------------|------------------|------------------------|---------|
| GCS personal Linux | `/~/` or empty | `/home/user/` | hierarchy `home/ > user/` selected |
| GCS personal macOS | `/~/` or empty | `/Users/user/` | hierarchy `Users/ > user/` selected |
| GCS personal Windows | `/~/` or `/C:/Users/me/` | `/C:/Users/me/` | hierarchy `C:/ > Users/ > me/` selected |
| Linux server with template | `/{server_default}/` | resolved `/home/user/` | hierarchy down to `/home/user/` selected |
| Mapped collection (POSIX-backed) explicit dir | `/home/me/data/` | `/home/me/data/` | hierarchy down to `/home/me/data/` selected |
| Mapped collection (iRODS-backed, well-behaved) | `/{server_default}/` | `/ghum/home/u0050020/` | hierarchy down to `/ghum/home/u0050020/` selected |
| **Mapped collection (iRODS-backed, echoes shorthand)** | `/~/` or empty | `/~/` (unchanged) | flat folders from `/~/`, or fall through to `/` if empty |
| Public endpoint with no home | empty | n/a (NotFound on `/~/`) | flat root listing, no preselect |
| ACL-restricted endpoint | any | PermissionDenied | error surfaced; user re-auths or contacts admin |

In all "flat fallback" cases the user has to click through to a real folder
before the submit button accepts the selection. This is intentional: we
explicitly do not want to auto-select `/`, `/~/`, or any other unresolved
path that could end up writing to a place the user does not own.

### Why we don't try harder to guess the home path

For iRODS endpoints we technically know `params.User` (e.g. `u0050020`)
and could guess `/<zone>/home/<user>/`, but:

- The zone (`ghum`, `vsc`, …) is endpoint-specific and not exposed in any
  field we get back from the Transfer API.
- Other connectors (Box, S3, …) use entirely different conventions.
- Guessing wrong and then preselecting the guess would silently lead the
  user into uploading to a path they do not have write access to — worse
  than today's behavior.

If ICTS later starts populating `default_directory` properly on the iRODS
collection, the multi-candidate resolver will pick that up automatically
and switch to the full hierarchy — no further code changes needed.

## Files changed

### Backend (`rdm-integration`)

- `image/app/plugin/types/select_item.go` — added `Expanded`, `Children`
- `image/app/plugin/impl/globus/common.go` — added `listOnce` and the
  `*WithPath` helpers; existing `listItems`/`listDirEntries`/`getResponse`
  delegate to them, so all other callers (`query.go`, tests) are unchanged
- `image/app/plugin/impl/globus/options.go` — replaced `listFolderItems`
  with `resolveAndBuildInitialTree`, added candidate-path / meaningfulness
  helpers, hardened `buildHierarchy` against placeholder inputs
- `image/app/plugin/impl/globus/options_test.go` — added coverage for the
  resolved-home, iRODS-echo, root-fallback, empty-home-falls-through,
  explicit-default-dir, placeholder-rejection, and meaningfulness cases

### Frontend (`rdm-integration-frontend`)

- `src/app/download/download.component.ts` — `_rootOptionsData.update(prev => [...prev])` instead of `refreshTrigger`
- `src/app/connect/connect.component.ts` — same fix, removed unused `refreshTrigger`

## Investigating future regressions

When the picker misbehaves on a new endpoint:

1. Reproduce, then grep server logs for `globus options:` — every
   resolution decision is logged.
2. Note the values: `defaultDirectory`, `responseAbsolutePath`, `resolvedDir`,
   `meaningful`. The combination tells you which branch fired.
3. Most fixes will be either:
   - Adding another candidate path to `buildCandidatePaths` (rare).
   - Tightening `isMeaningfulHierarchyPath` to reject a new placeholder
     pattern Globus invents (more likely).
4. **Do not** add endpoint-name heuristics or hard-coded zone guesses.
   We pay that cost forever; defensive resolution does not.
