#!/bin/sh
set -e

cleanup() {
  if [ -n "${NG_PID:-}" ] && kill -0 "${NG_PID}" 2>/dev/null; then
    kill "${NG_PID}" 2>/dev/null || true
  fi
  if [ -n "${APP_PID:-}" ] && kill -0 "${APP_PID}" 2>/dev/null; then
    kill "${APP_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

cd /workspace/backend || {
  echo "dev-entrypoint: backend workspace not found" >&2
  exit 1
}

if [ -n "${FRONTEND_DEV_DIR:-}" ] && [ -d "${FRONTEND_DEV_DIR}" ]; then
  FRONTEND_DEV_PORT="${FRONTEND_DEV_PORT:-4200}"
  echo "dev-entrypoint: starting Angular dev server on port ${FRONTEND_DEV_PORT}"
  (
    cd "${FRONTEND_DEV_DIR}" && \
      BROWSER=none CHOKIDAR_USEPOLLING=1 npm run start -- --host 0.0.0.0 --port "${FRONTEND_DEV_PORT}" --poll 2000
  ) &
  NG_PID=$!
else
  echo "dev-entrypoint: FRONTEND_DEV_DIR not set or missing; skipping Angular dev server" >&2
fi

echo "dev-entrypoint: building backend"
go mod download
go build -o /tmp/app ./app

echo "dev-entrypoint: starting backend"
/tmp/app "$@" &
APP_PID=$!

wait "${APP_PID}"
