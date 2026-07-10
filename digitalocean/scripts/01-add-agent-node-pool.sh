#!/usr/bin/env bash
# Add a dedicated "agent" node pool to the existing tiqr DOKS cluster.
# Requires: doctl authenticated (doctl auth init)
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-tiqr}"
POOL_NAME="${POOL_NAME:-agent-s}"
# s-2vcpu-4gb is enough for 2–4 API + 2 worker + UI + Redis (scale nodes with load).
AGENT_SIZE="${AGENT_SIZE:-s-2vcpu-4gb}"
AGENT_COUNT="${AGENT_COUNT:-2}"
AGENT_MIN="${AGENT_MIN:-2}"
AGENT_MAX="${AGENT_MAX:-4}"

echo "Adding node pool '${POOL_NAME}' to cluster '${CLUSTER_NAME}'..."
echo "  size: ${AGENT_SIZE}"
echo "  count: ${AGENT_COUNT} (auto-scale ${AGENT_MIN}–${AGENT_MAX})"
echo "  label: tiqr.events/workload=agent"
echo "  taint: workload=agent:NoSchedule"

# Skip if the pool already exists.
if doctl kubernetes cluster node-pool list "${CLUSTER_NAME}" --format Name --no-header 2>/dev/null | grep -qx "${POOL_NAME}"; then
  echo "Node pool '${POOL_NAME}' already exists — skipping create."
else
  doctl kubernetes cluster node-pool create "${CLUSTER_NAME}" \
    --name "${POOL_NAME}" \
    --size "${AGENT_SIZE}" \
    --count "${AGENT_COUNT}" \
    --auto-scale \
    --min-nodes "${AGENT_MIN}" \
    --max-nodes "${AGENT_MAX}" \
    --label tiqr.events/workload=agent \
    --taint workload=agent:NoSchedule
fi

echo ""
echo "Saving kubeconfig..."
doctl kubernetes cluster kubeconfig save "${CLUSTER_NAME}"

echo ""
echo "=== Agent node pool ready ==="
kubectl get nodes -L tiqr.events/workload
echo ""
echo "Next steps:"
echo "  1. bash digitalocean/scripts/02-setup-registry-ns.sh"
echo "  2. Create Managed Postgres and fill secrets (see digitalocean/README.md)"
echo "  3. bash digitalocean/scripts/build-and-push.sh && bash digitalocean/scripts/deploy.sh"
