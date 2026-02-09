# Globus Folder Selection — Problem & Solution

## Problems

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

### Backend fix (Problem 2)

**`types/select_item.go`** — added two optional fields:

```go
Expanded bool         `json:"expanded,omitempty"`
Children []SelectItem `json:"children,omitempty"`
```

These are backward-compatible: other plugins (iRODS, OneDrive, SFTP, etc.)
return flat `[]SelectItem` arrays where `Children` and `Expanded` are
zero-valued and omitted from JSON.

**`globus/options.go`** — rewrote `listFolderItems`:

1. On initial load (no `option`), resolve `DefaultDirectory` via the Globus
   endpoint API. Template variables like `{server_default}` map to `/~/`.
2. List the default directory's contents to get its children **and** the
   resolved absolute path (from Globus API's `absolute_path` field).
3. Call `buildHierarchy(resolvedDir, children)` which constructs a nested
   tree from `/` down to the default directory:

   ```
   home/          (expanded)
     └── user/    (expanded)
           └── data/  (expanded, selected, children populated)
                 ├── subfolder1/
                 └── subfolder2/
   ```

4. On subsequent expand (user clicks a node), return flat children as before.

The frontend's existing `convertToTreeNodes` already handles recursive
`children`, `expanded`, and `selected` fields — no frontend model changes
were needed.

### How it handles all endpoint types

| Endpoint type | DefaultDirectory | Resolved path | Hierarchy shown |
|---------------|------------------|---------------|-----------------|
| Linux server with template | `{server_default}` | `/~/` → `/home/user/` | `/home/ > user/ > ...` |
| Linux personal | `/home/user/data/` | `/home/user/data/` | `/home/ > user/ > data/` |
| Windows personal | `C:/Users/me/` | `C:/Users/me/` | `C:/ > Users/ > me/` |
| Root only | `/` | `/` | flat children of `/` |

## Files changed

### Backend (`rdm-integration`)
- `image/app/plugin/types/select_item.go` — added `Expanded`, `Children` fields
- `image/app/plugin/impl/globus/options.go` — rewrote `listFolderItems`, added `buildHierarchy`

### Frontend (`rdm-integration-frontend`)
- `src/app/download/download.component.ts` — `_rootOptionsData.update(prev => [...prev])` instead of `refreshTrigger`
- `src/app/connect/connect.component.ts` — same fix, removed unused `refreshTrigger`
