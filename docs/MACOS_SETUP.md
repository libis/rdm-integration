# macOS Setup Guide

## Common Issues and Solutions

### GID 20 Conflict

On macOS, GID 20 is reserved for the `staff` group. This is the default group for all regular users on macOS. When running `make up` on macOS, the build process automatically detects your user's GID (20) and tries to use it in the Docker containers.

**Symptom:**
```
addgroup: gid '20' in use
```

**Solution:**
The Dockerfiles have been updated to handle this case gracefully. The containers will:
1. Check if the requested GID already exists
2. If it's GID 20 (macOS staff group), use the existing group instead of trying to create a new one
3. Ensure the application user is added to the appropriate group

### UID 501 on macOS

macOS typically assigns UID 501 to the first user account. The Docker setup will use this UID for the application users inside containers to ensure proper file permissions when mounting volumes.

## Testing the Setup

After making the changes, test with:

```bash
make clean
make up
```

The build should complete successfully without the "gid in use" error.

## Volume Permissions

The use of matching UIDs/GIDs ensures that:
- Files created inside containers are owned by your user outside
- Files mounted from your host system are accessible inside containers
- No permission issues when editing files or accessing volumes

## Troubleshooting

If you still encounter permission issues:

1. Check your actual UID and GID:
   ```bash
   id -u  # Should show 501 on macOS
   id -g  # Should show 20 on macOS
   ```

2. Clean up Docker volumes and rebuild:
   ```bash
   make clean
   docker system prune -a --volumes
   make up
   ```

3. If problems persist, you can override the USER_ID and GROUP_ID:
   ```bash
   USER_ID=1000 GROUP_ID=1000 make up
   ```
