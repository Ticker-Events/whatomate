#!/usr/bin/env bash
# Build and push tiqr-agent (API/worker) and tiqr-agent-ui images.
set -euo pipefail

REGISTRY="${REGISTRY:-registry.digitalocean.com/tiqr}"
TAG="${TAG:-$(git rev-parse --short HEAD 2>/dev/null || echo latest)}"
API_IMAGE_NAME="${API_IMAGE_NAME:-tiqr-agent}"
UI_IMAGE_NAME="${UI_IMAGE_NAME:-tiqr-agent-ui}"
API_IMAGE="${API_IMAGE:-$REGISTRY/$API_IMAGE_NAME:$TAG}"
UI_IMAGE="${UI_IMAGE:-$REGISTRY/$UI_IMAGE_NAME:$TAG}"

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

echo "Building $API_IMAGE ..."
docker build -t "$API_IMAGE" -t "$REGISTRY/$API_IMAGE_NAME:latest" .

echo "Building $UI_IMAGE ..."
UI_BUILD_ARGS=()
if [[ -n "${VITE_FIREBASE_CONFIG:-}" ]]; then
  UI_BUILD_ARGS+=(--build-arg "VITE_FIREBASE_CONFIG=$VITE_FIREBASE_CONFIG")
else
  UI_ENV_FILE="${UI_ENV_FILE:-}"
  if [[ -z "$UI_ENV_FILE" ]]; then
    if [[ -f "$ROOT/frontend/.env.production" ]]; then
      UI_ENV_FILE="$ROOT/frontend/.env.production"
    elif [[ -f "$ROOT/frontend/.env" ]]; then
      UI_ENV_FILE="$ROOT/frontend/.env"
    fi
  fi
  if [[ -n "$UI_ENV_FILE" && -f "$UI_ENV_FILE" ]]; then
    value=$(grep -E '^VITE_FIREBASE_CONFIG=' "$UI_ENV_FILE" | head -1 | cut -d= -f2- || true)
    if [[ -n "$value" ]]; then
      UI_BUILD_ARGS+=(--build-arg "VITE_FIREBASE_CONFIG=$value")
    fi
  fi
fi
if [[ ${#UI_BUILD_ARGS[@]} -gt 0 ]]; then
  echo "UI Firebase config: enabled"
else
  echo "Warning: VITE_FIREBASE_CONFIG not set — Firestore real-time chat will be disabled in the UI"
fi
docker build "${UI_BUILD_ARGS[@]}" -t "$UI_IMAGE" -t "$REGISTRY/$UI_IMAGE_NAME:latest" -f frontend/Dockerfile frontend/

if [[ "${PUSH:-1}" == "1" ]]; then
  echo "Logging in to DO registry..."
  doctl registry login

  echo "Pushing $API_IMAGE ..."
  docker push "$API_IMAGE"
  docker push "$REGISTRY/$API_IMAGE_NAME:latest"

  echo "Pushing $UI_IMAGE ..."
  docker push "$UI_IMAGE"
  docker push "$REGISTRY/$UI_IMAGE_NAME:latest"
fi

echo "Done:"
echo "  $API_IMAGE"
echo "  $UI_IMAGE"
