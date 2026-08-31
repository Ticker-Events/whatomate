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

# shellcheck source=lib-ui-firebase-build.sh
source "$ROOT/digitalocean/scripts/lib-ui-firebase-build.sh"

echo "Building $API_IMAGE ..."
docker build -t "$API_IMAGE" -t "$REGISTRY/$API_IMAGE_NAME:latest" .

echo "Building $UI_IMAGE ..."
UI_BUILD_ARGS=()
if append_ui_firebase_build_args UI_BUILD_ARGS "$ROOT"; then
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
