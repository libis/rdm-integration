# Fast Redeploy (FRD) Workflow - Development Guide

## Status: ⚠️ BLOCKED - Base Image Issues

**Date:** November 5, 2025  
**Issue:** The `gdcc/base:6.8-noble` Docker image has a critical bug that prevents ANY deployment (both regular WARs and exploded WARs).

### Root Cause
The base image disables file logging with:
```bash
set-log-attributes com.sun.enterprise.server.logging.GFFileHandler.logtoFile=false
```

However, Payara 6.2025.3's `GFFileHandler` still attempts to access `this.absoluteFile.exists()` during deployment, causing:
```
java.lang.NullPointerException: Cannot invoke "java.io.File.exists()" because "this.absoluteFile" is null
```

This bug affects:
- PREBOOT_COMMANDS deployments
- Post-boot deployments via asadmin
- Both exploded WARs and regular WAR files

### Options to Proceed

1. **Wait for upstream fix** - Report to GDCC/Dataverse team
2. **Use older base image** - Try `gdcc/base:6.7` or earlier versions
3. **Build custom base** - Fork and fix the base image ourselves
4. **Use standard Payara** - Build from `payara/server-full:6.2025.3-jdk17` directly

---

## Goal: Fast Redeploy Workflow

Enable rapid iteration during development by:
- **Backend**: Incremental Java compilation + rsync to exploded WAR (~5-15 seconds)
- **Frontend**: Angular `ng serve` with hot reload (instant updates)

**Target**: 4-6x faster than full rebuild (70-140s → 15-25s for backend, instant for frontend)

---

## Architecture Overview

### Development Stack
```
┌─────────────────────────────────────────────┐
│ docker-compose.yml (base)                   │
│ + docker-compose.dev.yml (dev overrides)    │
└─────────────────────────────────────────────┘
                    ↓
        ┌───────────────────────┐
        │   Dev Environment     │
        ├───────────────────────┤
        │ ┌──────────────────┐  │
        │ │ Dataverse        │  │  ← Exploded WAR + dynamic reload
        │ │ (Payara)         │  │  ← Volume mount from host
        │ └──────────────────┘  │
        │ ┌──────────────────┐  │
        │ │ RDM Integration  │  │  ← ng serve proxy
        │ │ (Go + Angular)   │  │  ← Volume mount frontend source
        │ └──────────────────┘  │
        └───────────────────────┘
```

### Dataverse Backend FRD

#### Concept
1. **Initial Setup**: Build and explode WAR into volume-mounted directory
2. **Development Loop**: 
   - Edit Java source files
   - `mvn compile` (incremental, ~5-15s)
   - `rsync` compiled classes to exploded WAR's WEB-INF/classes
   - Touch `.reload` file to trigger Payara hot reload

#### Configuration Required
- Payara dynamic reload enabled: `dynamic-reload-enabled=true`
- Polling interval: `dynamic-reload-poll-interval-in-seconds=2`
- Exploded WAR mounted at `/opt/payara/deployments/dataverse-6.8`

#### Makefile Targets (Planned)
```makefile
dev_up:
	# 1. Build WAR
	cd ../dataverse && mvn package -DskipTests -q
	# 2. Explode into volume
	unzip -o dataverse-$(DATAVERSE_VERSION).war -d ./docker-volumes/dataverse/data/applications/dataverse-$(DATAVERSE_VERSION)
	# 3. Start containers with dev overrides
	docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
	# 4. Wait for ready
	./wait-for-dataverse.sh

frd-backend:
	# Compile Java sources incrementally
	cd ../dataverse && mvn compile -DskipTests -q
	# Sync compiled classes to exploded WAR
	rsync -a --delete ../dataverse/target/classes/ ./docker-volumes/dataverse/data/applications/dataverse-6.8/WEB-INF/classes/
	# Trigger reload
	touch ./docker-volumes/dataverse/data/applications/dataverse-6.8/.reload
```

### Frontend FRD

#### Concept
1. **Dev Mode Detection**: Check `FRONTEND_DEV_MODE=true` environment variable
2. **ng serve**: Run Angular dev server on port 4200 with hot reload
3. **Go Proxy**: Proxy frontend requests from Go app to `localhost:4200`
4. **Instant Updates**: Angular hot reload provides instant browser updates

#### Implementation

**Dockerfile.dev (rdm-integration)**:
```dockerfile
FROM node:20-alpine AS dev

# Install Angular CLI
RUN npm install -g @angular/cli

# Install Go
RUN apk add --no-cache go

WORKDIR /app

# Dev entrypoint script
COPY dev-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/dev-entrypoint.sh

# Volume mount: ../rdm-integration-frontend:/app/frontend

ENTRYPOINT ["/usr/local/bin/dev-entrypoint.sh"]
CMD ["app"]
```

**dev-entrypoint.sh**:
```bash
#!/bin/sh
set -e

if [ -d "/app/frontend/src" ]; then
    echo "🔥 Frontend dev mode enabled"
    cd /app/frontend
    npm install --prefer-offline
    ng serve --host 0.0.0.0 --port 4200 --disable-host-check --poll 2000 &
    export FRONTEND_DEV_MODE=true
    export FRONTEND_DEV_URL=http://localhost:4200
fi

# Start main app
exec "$@"
```

**frontend.go modifications**:
```go
var devProxy *httputil.ReverseProxy

func init() {
    if os.Getenv("FRONTEND_DEV_MODE") == "true" {
        devURL := os.Getenv("FRONTEND_DEV_URL")
        if devURL == "" {
            devURL = "http://localhost:4200"
        }
        target, _ := url.Parse(devURL)
        devProxy = httputil.NewSingleHostReverseProxy(target)
        log.Printf("✅ Frontend dev mode - proxying to %s", devURL)
    }
}

func Frontend(w http.ResponseWriter, r *http.Request) {
    // Handle redirects...
    
    if devProxy != nil {
        devProxy.ServeHTTP(w, r)  // Proxy to ng serve
    } else {
        // Serve embedded files
        r.URL.Path = "/dist/datasync" + r.URL.Path
        fs.ServeHTTP(w, r)
    }
}
```

**docker-compose.dev.yml**:
```yaml
services:
  integration:
    build:
      context: ../rdm-integration
      dockerfile: image/Dockerfile.dev
    volumes:
      - ../rdm-integration-frontend:/app/frontend:ro
    ports:
      - "4200:4200"  # Expose ng serve
    environment:
      - FRONTEND_DEV_MODE=true
      - FRONTEND_DEV_URL=http://localhost:4200
```

---

## Files Modified/Created

### Attempted (Blocked by Base Image)

1. **dataverse/Dockerfile.dev** - Dev image with dynamic reload
2. **docker-compose.dev.yml** - Dev overrides with volume mounts
3. **Makefile** - FRD targets (dev_up, frd-backend, frd-frontend)
4. **image/Dockerfile.dev** - Frontend dev image with ng serve
5. **image/app/frontend/frontend.go** - Dev mode proxy logic

### Version Management
All versions read from `.env`:
```bash
DATAVERSE_VERSION=6.8
BASE_VERSION=6.8-noble
# ... other versions
```

---

## Troubleshooting

### Base Image Issues
**Symptom**: `NullPointerException: Cannot invoke "java.io.File.exists()"`  
**Cause**: GFFileHandler bug with disabled file logging  
**Solution**: Use different base image or wait for upstream fix

### Exploded WAR Not Deploying
- Check volume mount path matches deployment directory
- Verify permissions (user/group IDs match)
- Check Payara logs for deployment errors
- Ensure `dynamic-reload-enabled=true`

### Frontend Not Hot Reloading
- Verify `FRONTEND_DEV_MODE=true` is set
- Check port 4200 is accessible
- Verify frontend source is mounted at `/app/frontend`
- Check ng serve is running: `docker exec integration ps aux | grep ng`

### Rsync Performance
- Use `--delete` to remove old classes
- Exclude test classes if not needed
- Consider `--checksum` for large projects

---

## Performance Targets

### Current (Full Rebuild)
- Backend: 70-140 seconds (clean build + container rebuild)
- Frontend: 60-90 seconds (npm build + container rebuild)

### Target (FRD)
- Backend: 5-15 seconds (mvn compile + rsync)
- Frontend: Instant (ng serve hot reload)

### Expected Improvement
- **4-6x faster iteration** for backend changes
- **Instant feedback** for frontend changes
- **No container rebuilds** during development

---

## Next Steps

1. **Resolve base image issue**:
   - Try `gdcc/base:6.7` or earlier versions
   - Or build custom base from `payara/server-full:6.2025.3-jdk17`
   - Or report bug to GDCC team

2. **Test exploded WAR deployment**:
   ```bash
   # Manual test without PREBOOT_COMMANDS
   docker exec dataverse asadmin deploy --force /opt/payara/deployments/dataverse-6.8
   ```

3. **Implement frontend dev mode**:
   - Complete Dockerfile.dev for rdm-integration
   - Add proxy logic to frontend.go
   - Test ng serve with hot reload

4. **Optimize backend FRD**:
   - Fine-tune rsync filters
   - Test reload trigger mechanisms
   - Measure actual compile times

5. **Document workflow**:
   - Create step-by-step developer guide
   - Add troubleshooting section
   - Document known limitations

---

## References

- [Payara Dynamic Reload Documentation](https://docs.payara.fish/)
- [Dataverse Container Guide](https://guides.dataverse.org/en/latest/container/)
- [Angular CLI ng serve](https://angular.io/cli/serve)
- [Go httputil.ReverseProxy](https://pkg.go.dev/net/http/httputil#ReverseProxy)

---

## Notes

### What Worked
✅ Version management from .env  
✅ Separate dev Dockerfiles approach  
✅ docker-compose.dev.yml override pattern  
✅ Frontend dev mode architecture (ng serve + proxy)  

### What's Blocked
❌ Dataverse base image deployment (NullPointerException bug)  
❌ Exploded WAR testing (can't deploy anything)  
❌ Backend FRD workflow (needs working deployment)  

### Lessons Learned
- Base image quality is critical - always test deployment first
- PREBOOT_COMMANDS are fragile - prefer post-boot deployment
- File logging issues can block deployments silently
- Standard Payara images may be more reliable than custom builds
