#!/usr/bin/env bash
# Create the tiqr-agent namespace and apply the DO registry pull secret.
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-tiqr}"
NAMESPACE="${NAMESPACE:-tiqr-agent}"
REGISTRY_NAME="${REGISTRY_NAME:-tiqr}"

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

echo "Saving kubeconfig for cluster '${CLUSTER_NAME}'..."
doctl kubernetes cluster kubeconfig save "${CLUSTER_NAME}"

echo "Ensuring namespace '${NAMESPACE}' exists..."
kubectl apply -f "$ROOT/digitalocean/k8s/overlays/production/namespace.yaml"

echo "Granting cluster pull access to registry '${REGISTRY_NAME}'..."
doctl kubernetes cluster registry add "${CLUSTER_NAME}" 2>/dev/null || true

echo "Applying imagePullSecret 'do-registry' in namespace '${NAMESPACE}'..."
doctl registry kubernetes-manifest --name do-registry --namespace "${NAMESPACE}" | kubectl apply -f -

echo ""
echo "=== Registry + namespace ready ==="
kubectl get namespace "${NAMESPACE}"
kubectl get secret do-registry -n "${NAMESPACE}"
