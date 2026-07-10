#!/usr/bin/env bash
# Deploy tiqr-agent to the tiqr DOKS cluster (migrate first, then apps).
set -euo pipefail

OVERLAY="${OVERLAY:-digitalocean/k8s/overlays/production}"
NAMESPACE="${NAMESPACE:-tiqr-agent}"
CLUSTER_NAME="${CLUSTER_NAME:-tiqr}"
REGISTRY="${REGISTRY:-registry.digitalocean.com/tiqr}"
TAG="${TAG:-$(git rev-parse --short HEAD 2>/dev/null || echo latest)}"
API_IMAGE="${API_IMAGE:-$REGISTRY/tiqr-agent:$TAG}"
UI_IMAGE="${UI_IMAGE:-$REGISTRY/tiqr-agent-ui:$TAG}"

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OVERLAY_PATH="$ROOT/$OVERLAY"

echo "Cluster: $CLUSTER_NAME  Namespace: $NAMESPACE"
echo "API image: $API_IMAGE"
echo "UI image:  $UI_IMAGE"

doctl kubernetes cluster kubeconfig save "$CLUSTER_NAME"

cd "$OVERLAY_PATH"
kustomize edit set image \
  "registry.digitalocean.com/tiqr/tiqr-agent=$API_IMAGE" \
  "registry.digitalocean.com/tiqr/tiqr-agent-ui=$UI_IMAGE"

echo "Applying manifests from $OVERLAY ..."
# Job pod templates are immutable — delete any previous migrate job before apply.
kubectl delete job agent-migrate -n "$NAMESPACE" --ignore-not-found --wait=true
kubectl apply -k .

echo "Running migrations ..."
kubectl wait --for=condition=complete "job/agent-migrate" -n "$NAMESPACE" --timeout=300s

echo "Waiting for app rollouts ..."
kubectl rollout status deployment/agent-api -n "$NAMESPACE" --timeout=600s
kubectl rollout status deployment/agent-worker -n "$NAMESPACE" --timeout=600s
kubectl rollout status deployment/agent-ui -n "$NAMESPACE" --timeout=300s
kubectl rollout status deployment/agent-redis -n "$NAMESPACE" --timeout=300s

echo "Deploy complete."
