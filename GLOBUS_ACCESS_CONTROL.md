# Dataset Download Filtering

## Overview

This document describes how the download UI filters datasets based on user permissions. We only show datasets where the user can download all files, providing a cleaner user experience.

---

## How It Works

### Dataset Search Flow

1. **Get user's datasets** via `/api/mydata/retrieve` (includes own drafts, unpublished)
2. **Search public datasets** via `/api/search`
3. **Filter by downloadability** - only show datasets where user can download all files

### Why Filter?

When a dataset contains restricted or embargoed files that a user cannot access, showing it in the download list would lead to failed transfers. By filtering these datasets out, we:

- Provide a cleaner UI showing only actionable items
- Avoid confusing error messages during transfer attempts
- Give users a clear picture of what they can actually download

---

## Permission Check Logic

### `CanUserDownloadAllFiles` Function

This function determines if a user can download all files in a dataset:

```go
func CanUserDownloadAllFiles(ctx context.Context, persistentId, token, user string, 
    hasRestricted, hasEmbargoed bool) (bool, error)
```

Returns `true` if:
1. **Dataset has no restrictions** - no restricted or embargoed files, OR
2. **User has EditDataset permission** - dataset owners/curators can access all files

### Dataverse API Used

`GET /api/datasets/:persistentId/userPermissions?persistentId={pid}`

Returns:
```json
{
    "status": "OK",
    "data": {
        "canViewUnpublishedDataset": true,
        "canEditDataset": true,
        "canPublishDataset": true,
        "canManageDatasetPermissions": true,
        "canDeleteDatasetDraft": true
    }
}
```

The `canEditDataset` permission indicates the user is an owner or curator with full access to all files.

---

## Performance Optimization

The permission check is only made when necessary:

| Scenario | API Calls |
|----------|-----------|
| Public dataset (no restrictions) | 0 extra calls |
| Restricted dataset | 1 call to userPermissions |

This ensures minimal overhead for most datasets while still providing accurate filtering.

---

## User Experience

| Scenario | Dataset Shown in Download List? |
|----------|--------------------------------|
| Public dataset | ✅ Yes |
| Restricted dataset, user is owner | ✅ Yes |
| Restricted dataset, user is NOT owner | ❌ No (cannot download all files) |
| Embargoed dataset, user is owner | ✅ Yes |
| Embargoed dataset, user is NOT owner | ❌ No (cannot download all files) |

Users who cannot download all files in a dataset will simply not see that dataset in the download interface, avoiding any confusion or failed transfer attempts.

---

## Code References

### Go Files
- `image/app/common/downloadable.go` - Main download handler with filtering
- `image/app/dataverse/dataverse_read.go` - `CanUserDownloadAllFiles` and `GetDatasetUserPermissions`
- `image/app/dataverse/embargo.go` - `GetDatasetNodesWithAccessInfo` checks for restrictions

### Key Functions
- `CanUserDownloadAllFiles()` - Determines if user can download all files
- `GetDatasetUserPermissions()` - Fetches user's permissions on a dataset
- `GetDatasetNodesWithAccessInfo()` - Returns file tree with restriction flags

### Additional Notes

1. **Users with granted access to specific files but not EditDataset** will still be filtered out
   - These users can download via direct HTTP instead of Globus transfer
   - This provides a consistent user experience where Globus shows only fully downloadable datasets

2. **Fine-grained access not currently supported**
   - We use EditDataset as a proxy for "can access all files"
   - A more precise check would require per-file permission checks (expensive)
   - For most use cases, dataset owners are the primary users of bulk Globus downloads
