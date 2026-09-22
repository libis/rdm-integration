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
   listing, with **no node marked `Selected`**. Put a resolved root listing
   under `/`. Keep an unresolved `/~/` listing under a separate `~` node,
   beside a collapsed `/` that can be expanded to browse elsewhere.
   Use the resolved directory, so a `/~/` request resolving to `/` is
   displayed under `/`, not under `~`.
7. If nothing worked, propagate the last non-NotFound error, or return an
   empty list.

Every step logs to `logging.Logger` with the candidate, response
`absolute_path`, derived resolved path, and meaningfulness flag. When a
new endpoint variant misbehaves, those logs are the first place to look.

### How it handles all endpoint types

| Endpoint type | DefaultDirectory | response.absolute_path | Outcome |
|---------------|------------------|------------------------|---------|
| GCP Linux | `/~/` or empty | `/home/user/` | hierarchy `/ > home/ > user/` selected at user |
| GCP macOS | `/~/` or empty | `/Users/user/` | hierarchy `/ > Users/ > user/` selected at user |
| GCP Windows | `/~/` or `/C/Users/me/` | `/C/Users/me/` | hierarchy `/ > C/ > Users/ > me/` selected at me |
| Linux server with template | `/{server_default}/` | resolved `/home/user/` | hierarchy down to `/home/user/` selected |
| Mapped collection (POSIX-backed) explicit dir | `/home/me/data/` | `/home/me/data/` | hierarchy down to `/home/me/data/` selected |
| Mapped collection (iRODS-backed, well-behaved) | `/{server_default}/` | `/ghum/home/u0050020/` | hierarchy down to `/ghum/home/u0050020/` selected |
| **Mapped collection (iRODS-backed, echoes shorthand)** | `/~/` or empty | `/~/` (unchanged) | folders under `~` beside collapsed `/`, or fall through to `/` if empty |
| Public endpoint with no home | empty | n/a (NotFound on `/~/`) | folders under `/`, no preselect |
| ACL-restricted endpoint | any | PermissionDenied | error surfaced; user re-auths or contacts admin |

Every successful initial listing includes a selectable `/`. Resolved home
hierarchies sit beneath it. Both the connect and download pickers replace
their initial placeholder with this tree and show the selected path.
Fallback nodes, including `/` and `/~/`, require an explicit selection.
Expanding `/` subsequently requests its children without another root wrapper.

Windows drive paths follow the documented `/drive_letter/path` convention,
including mapped network drives. See the
[Globus Connect Personal Windows guide](https://docs.globus.org/globus-connect-personal/install/windows/).
Guest collection paths are relative to the collection's virtual root, as
described in the [Globus file operations API](https://docs.globus.org/api/transfer/file_operations/).

### Transfer path preservation

Upload source paths add a separator only when the selected directory lacks
one. Do not use `path.Join` here: it removes `..` locally, which can change
the path's meaning when an earlier component is an endpoint symlink.
File directory labels and storage identifiers must stay paired with the
same transfer item even when multiple files are iterated from a Go map.

### Regression coverage

`paths_test.go` covers personal Linux, macOS and Windows paths, mapped
network drives, POSIX and iRODS collections, object-storage-shaped paths,
guest roots, empty directories, missing absolute paths, and permission
fallbacks. It also exercises expanding `/` and navigating to another drive.
`query_test.go` runs recursive mock HTTP listings through query conversion
and transfer manifest construction, checking relative paths and metadata.
Connect and download browser tests cover initial root replacement, selected
home preservation, explicit root selection, and loading root children.

These tests simulate the API responses; they do not exercise live personal
endpoints or certify every server connector and access-policy configuration.

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

Root selection (September 2026):

- `image/app/plugin/impl/globus/options.go` — `withRoot` puts every initial
  listing under a selectable, never preselected `/` node
- `image/app/plugin/impl/globus/streams.go` — `transferItems` builds the
  transfer manifest; the source path adds only the missing separator
- `image/app/plugin/impl/globus/paths_test.go`, `query_test.go`,
  `streams_test.go`, `common_test.go` — per-endpoint regression tables

### Frontend (`rdm-integration-frontend`)

- `src/app/download/download.component.ts` — `_rootOptionsData.update(prev => [...prev])` instead of `refreshTrigger`
- `src/app/connect/connect.component.ts` — same fix, removed unused `refreshTrigger`

Root selection (September 2026):

- `connect.component.ts`, `download.component.ts` — `isRootListing` replaces
  the "Expand and select" placeholder with the backend's `/` tree
- `connect.component.html`, `download.component.html` — selected path shown
  as plain text under the tree

## Globus uploads and the file status afterwards

Globus gives only a `last_modified` timestamp as the remote hash, and that can
never be recomputed from the stored file. Once the transfer task is accepted
and Dataverse has registered the files (`doTransfer` in the globus plugin),
a record per file is written to Redis under `globus transfer <pid> -> <file id>`
for `LockMaxDuration`: the storage identifier Dataverse handed out for the
transfer plus the source timestamp (`types.GlobusTransfer`). The rehash job
(`globusTransferLastModified` in core) uses the timestamp only when the file
listed by Dataverse is backed by that very storage object, and keeps the
record, so a wiped hash cache can be rebuilt. A transfer that fails before
Dataverse accepts the files leaves no record, a file replaced through the UI
has another object and stays "updated", and records written before the object
binding (a bare timestamp) are ignored.

Persisting a Globus job drops the cached rehash of every file it transfers,
copies included: a file deleted outside the integration and copied again with
identical content keeps its Dataverse checksum, so the old cache entry would
still match and the job that reads the transfer record would never be queued.

A polled compare (`api/common/compare`) queues the rehash job itself when no
job holds the dataset lock, after refreshing the destination side from the
listing: the page echoes the "?" it was shown, and a job must never hash an
echoed "?" as if it were a checksum. Before, only the connect page's compare did, and a
page that arrived at the compare view through polling alone spun on Updating
with nothing in the log.

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
