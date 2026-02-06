# Globus Options Path Problem

## Problem Statement

When selecting a folder from a Globus endpoint, we need to:
1. Show the **full absolute path** to start from (not `/~/` or `/{server_default}/`)
2. Allow the frontend to construct a tree by splitting on `/`
3. Pre-select the default folder
4. Work in ALL cases:
   - Windows personal endpoints (drive letters like `C:/`)
   - Linux personal endpoints (`/~/` → `/home/user/`)
   - Servers with `{server_default}` template variable

## UI Behavior

1. **Initial state**: Root shows "Expand and select" (placeholder)
2. **First expansion**: Shows the absolute path hierarchy to the pre-selected default folder
   - Example: If default is `/home/u0050020/data/`, show tree:
     ```
     /home/
       └── u0050020/
             └── data/  ← pre-selected
     ```
3. **Subsequent expansions**: Load siblings of the expanded folder
4. **Collapse/expand**: Refreshes and loads siblings

## Frontend Tree Refresh Pattern

Use **trigger input pattern** (NOT deep cloning) for consistency with other components:
- `datafile.component.ts` - uses `trigger` input to force computed signal re-evaluation
- `downloadablefile.component.ts` - same pattern
- `metadatafield.component.ts` - same pattern

The pattern:
```typescript
readonly trigger = input(0);
readonly someComputed = computed(() => {
  this.trigger(); // Track trigger to update on changes
  return /* computed value */;
});
```

When tree nodes change, increment the trigger to force UI refresh.

## Current State Analysis

### Backend: `options.go`
- `listFolderItems()` tries DefaultDirectory, then `/`, then `/~/`
- When DefaultDirectory contains `{...}`, it substitutes with `/~/`
- Returns folder items with their `Value` field set to the path

### Backend: `common.go`
- `listItems()` calls Globus API with the path
- Globus API returns `absolute_path` in the response which resolves `/~/` → `/home/user/`
- Each item's path should use this resolved `absolute_path`

### The Core Issue
When querying `/~/` or `/{server_default}/`:
1. Globus API resolves it to the actual path (e.g., `/home/u0050020/`)
2. The response's `absolute_path` field contains this resolved path
3. We need to use this resolved path for constructing folder IDs

## Solution Plan

### Step 1: Verify Globus API behavior
Make an initial `/ls` call with the DefaultDirectory (or `/~/` or `/`) and check the `absolute_path` in the response.

### Step 2: Backend uses resolved paths from Globus API
The Globus API returns an `absolute_path` field in each response item. When we query `/~/` or `/{server_default}/`, the API resolves it to the actual path (e.g., `/home/u0050020/`).

In `listItems()`, we use `v.AbsolutePath` from each response item instead of constructing paths from the queried path.

### Step 3: Existing tree logic handles hierarchy
The existing code already builds the tree structure correctly - no changes needed there. The frontend splits paths on `/` to show the hierarchy.

**On collapse + expand**: Load ALL siblings at that level (normal folder listing).

This is efficient - initial load shows just the path (3-4 items), not thousands of folders.

### Step 4: Handle all three cases

| Case | DefaultDirectory | Query | API absolute_path | Result |
|------|------------------|-------|-------------------|--------|
| Linux server with template | `{server_default}` | `/~/` | `/home/user/` | Use `/home/user/` |
| Linux personal | `/home/user/` | `/home/user/` | `/home/user/` | Use `/home/user/` |
| Windows personal | `/` | `/` | `/` | Items: `C`, `D` → `/C/`, `/D/` |

### Step 5: Frontend handles tree construction
Frontend receives items with full absolute paths. It can:
- Split on `/` to show hierarchy
- Expand/collapse to load siblings
- Default folder is pre-selected

## Implementation

### `common.go` - listItems()
```go
func listItems(...) {
    response, err := getResponse(...)
    for _, v := range response {
        // Use resolved absolute_path from API
        basePath := v.AbsolutePath
        if basePath == "" {
            basePath = path  // fallback to queried path
        }
        if !strings.HasSuffix(basePath, "/") {
            basePath = basePath + "/"
        }
        id := basePath + v.Name + "/"
        // ...
    }
}
```

### `options.go` - listFolderItems()
```go
func listFolderItems(...) {
    // For initial load, try to get resolved default directory
    if params.Option == "" {
        endpoint, _ := getEndpoint(...)
        defaultDir := endpoint.DefaultDirectory
        
        // Determine what path to query
        queryPath := "/"
        if defaultDir != "" {
            if strings.Contains(defaultDir, "{") {
                queryPath = "/~/"  // Template variable → use /~/
            } else {
                queryPath = defaultDir
            }
        }
        
        // Query and get resolved path from response
        params.Option = queryPath
        items, err := doListFolderItems(...)
        // Items now have full absolute paths in their Value field
        return items, err
    }
    
    // Expanding existing node - just list that folder
    return doListFolderItems(params, "")
}
```

## Execution Checklist

### Backend
- [x] 1. `common.go`: Use `v.AbsolutePath` from Globus API response (the API resolves `/~/` etc. for us)
- [x] 2. `options.go`: When `DefaultDirectory` contains `{...}`, query `/~/` instead (API will resolve it)
- [x] 3. Keep existing tree logic (already works)

### Frontend Migration: Remove deep cloning, use trigger pattern
- [x] 5. `connect.component.ts`: Add `refreshTrigger` signal, remove `deepCloneTree` usage
- [x] 6. `download.component.ts`: Already has `refreshTrigger`, remove `deepCloneTree` usage
- [x] 7. `tree-utils.ts`: Remove `deepCloneTree` function if no longer needed
- [x] 8. Increment trigger after `node.children = ...` to force refresh

### Testing
- [ ] 9. Test case: Linux server with `{server_default}`
- [ ] 10. Test case: Linux personal endpoint
- [ ] 11. Test case: Windows personal endpoint
- [ ] 12. Frontend displays full paths correctly
- [ ] 13. Expanding folders works (loads siblings)
- [ ] 14. Default folder pre-selection works
