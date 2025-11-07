# Fast redeploy workflow

The development stack (`make dev_up`) now supports targeted rebuilds so you can iterate on the backend or Dataverse without tearing everything down. This document describes the flow and the safeguards baked into the `frd-*` (fast redeploy) Make targets.

## Prerequisites

### Workspace layout

Clone the repositories so they sit side by side:

```bash
git clone https://github.com/libis/rdm-integration.git
cd rdm-integration
git clone https://github.com/IQSS/dataverse.git ../dataverse
git clone https://github.com/libis/rdm-integration-frontend.git ../rdm-integration-frontend
```

The dev compose override expects those exact sibling directories:

- `../dataverse` — the Dataverse source tree used for rebuilding the WAR
- `../rdm-integration-frontend` — the Angular workspace served by the dev container

### Runtime setup

1. Run `make dev_up` once per session. This boots the dev stack, mounts the local sources, and exposes an unproxied backend port (`http://localhost:7788/`). As a side effect the command creates `.dev_up_active`, a sentinel file used by the fast-redeploy targets.
2. Keep the dev stack running while you iterate. Stopping it (e.g. via `make down`) removes the sentinel file, so the fast-redeploy targets will refuse to run until you start the stack again.

The `frd-*` targets check for `.dev_up_active` before executing. If the file is missing they abort with a reminder to run `make dev_up` first.

While the dev stack is running, the integration container launches `ng serve` against the mounted `../rdm-integration-frontend` workspace. Saving frontend files triggers live recompiles that flow through the OAuth proxy on `http://localhost:4180/`, so most UI tweaks show up immediately without rerunning `make frd-integration`.

## Commands

### `make frd-integration`

- Restarts only the `integration` container using the dev compose override (`docker-compose.yml.dev`).
- Waits until the backend’s port (`http://localhost:7788/`) responds with HTTP 200, ensuring the Go app is up and its Angular reverse proxy is serving traffic.
- Use this after editing Go code, frontend assets, or configuration consumed by the integration service.

### `make frd-dataverse`

- Compiles Dataverse incrementally with Maven (`mvn -T 1C … compile`).
- Syncs the freshly built classes into `WEB-INF/classes/` and mirrors `src/main/webapp` (preserving Payara-provided libs), keeping the exploded WAR aligned without a full package. Run a full `mvn package` when dependencies or other packaging outputs change.
- Forces Payara to redeploy the updated application via `asadmin deploy --force --upload=false …`.
- Waits for Payara to accept admin commands before redeploying and blocks until `http://localhost:8080/` returns HTTP 200, so the application is ready when the command finishes.
- Like `frd-integration`, it refuses to run unless the dev stack is already up.

## Typical workflow

```shell
make dev_up          # start the development stack once
# edit Go/frontend or Dataverse sources…
make frd-integration # quick backend/frontend redeploy
make frd-dataverse   # when Dataverse code/resources change
```

If the stack is stopped (`make down`) you will need to rerun `make dev_up` before invoking the fast redeploy targets again.
