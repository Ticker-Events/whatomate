# TiQR Agent on DOKS

Deploy Whatomate as **tiqr-agent** on the existing DigitalOcean Kubernetes cluster `tiqr` (blr1), on a dedicated **agent** node pool.

| Item | Value |
|------|--------|
| Hostname | `https://agent.tiqr.store` |
| Namespace | `tiqr-agent` |
| Node pool | `agent` — default `s-2vcpu-4gb` ×2 (auto-scale 2–4), label/taint `workload=agent` |
| Topology | API HPA 2–4 (`server -workers 0`), 2× worker, 1× UI (nginx) |
| Postgres | DO Managed PostgreSQL (external) |
| Redis | In-cluster `agent-redis` on the agent pool |
| Images | `registry.digitalocean.com/tiqr/tiqr-agent`, `…/tiqr-agent-ui` |

Shares the cluster with ticker-events (production / staging / observability pools) but does **not** schedule onto those pools.

## Prerequisites

- `doctl` authenticated with access to the `tiqr` cluster and `tiqr` container registry
- `kubectl`, `kustomize`, `docker`
- Existing cluster ingress-nginx + cert-manager (from ticker-events bootstrap)
- DO Managed Postgres reachable from the cluster VPC / trusted sources

## Bootstrap (once)

```bash
# 1. Add the agent node pool
bash digitalocean/scripts/01-add-agent-node-pool.sh

# 2. Namespace + registry pull secret
bash digitalocean/scripts/02-setup-registry-ns.sh

# 3. Create secrets from the example
cp digitalocean/k8s/overlays/production/secrets.example.yaml \
   digitalocean/k8s/overlays/production/secrets.yaml
# Edit secrets.yaml: Managed Postgres host/password, JWT, encryption_key, admin password
kubectl apply -f digitalocean/k8s/overlays/production/secrets.yaml

# 4. DNS: point agent.tiqr.store A/CNAME at the same ingress LoadBalancer IP
#    used by api.tiqr.events (kubectl -n ingress-nginx get svc)
```

## Build and deploy

```bash
bash digitalocean/scripts/build-and-push.sh
bash digitalocean/scripts/deploy.sh
```

Or with an explicit tag:

```bash
TAG=$(git rev-parse --short HEAD) bash digitalocean/scripts/build-and-push.sh
TAG=$TAG bash digitalocean/scripts/deploy.sh
```

Deploy order: apply manifests → wait for `agent-migrate` Job → wait for API / worker / UI / Redis rollouts.

## Layout

```
digitalocean/
  scripts/
    01-add-agent-node-pool.sh
    02-setup-registry-ns.sh
    build-and-push.sh
    deploy.sh
  k8s/
    base/           # api, worker, ui, redis, migrate
    overlays/production/
```

## Multi-API notes

- WebSocket broadcasts fan out over Redis (`whatomate:ws:broadcast`) so live chat works across both API pods.
- SLA processing uses a Redis lock so only one API replica runs escalations.
- Uploads use `emptyDir` per pod; prefer S3 for durable media in production.
- Voice/WebRTC UDP hostPorts are not configured yet.

## CI

`.github/workflows/deploy-agent.yaml` runs tests, then builds and deploys on every push to `tiqr-main` (and via workflow_dispatch). Requires secret `DIGITALOCEAN_ACCESS_TOKEN`.
