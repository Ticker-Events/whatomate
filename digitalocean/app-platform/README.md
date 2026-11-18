# TiQR Agent on App Platform

Backend-only deploy: **one** service runs `whatomate server -workers 1` (API + embedded workers). UI stays on Vercel.

## Live app (created)

| Field | Value |
|--------|--------|
| App ID | `d9095d9a-eadd-497b-ab6a-36b8dcc049db` |
| Region | **blr** (Bangalore) |
| Valkey | Production managed DBaaS cluster **`tiqr-agent-valkey`** in **sgp1** (attached in spec via `cluster_name`; not an App Platform dev DB) |

```bash
doctl apps get d9095d9a-eadd-497b-ab6a-36b8dcc049db --format ID,DefaultIngress,ActiveDeployment.ID
doctl apps list-deployments d9095d9a-eadd-497b-ab6a-36b8dcc049db
```

App Platform does **not** support dev Valkey/Redis components (`engine: VALKEY` / `REDIS` requires `production: true` and an existing `cluster_name`).

## Spec

- [`tiqr-agent.yaml`](tiqr-agent.yaml) — committed template (placeholder `WHATOMATE_*` envs, `cluster_name: tiqr-agent-valkey`)
- Local filled spec for create/update: `tiqr-agent.local.yaml` (gitignored)

Config is **individual** `WHATOMATE_*` env vars (section-aware mapping in `internal/config/config.go`). Redis comes from the Valkey binding. `docker/entrypoint.sh` still ensures `/tmp/whatomate-config.toml` exists so `-config` works with an empty file when only envs are set.

## First create (reference)

```bash
# 1. Build/push thin wrap of DOCR image + entrypoint (from repo root)
doctl registry login
docker pull registry.digitalocean.com/tiqr/tiqr-agent:latest
TAG=$(git rev-parse --short HEAD)
# Dockerfile: FROM registry.../tiqr-agent:latest, COPY docker/entrypoint.sh, ENTRYPOINT ["/entrypoint.sh"]
docker build -f /tmp/Dockerfile.tiqr-agent-appplatform \
  -t "registry.digitalocean.com/tiqr/tiqr-agent:$TAG" \
  -t registry.digitalocean.com/tiqr/tiqr-agent:latest .
docker push "registry.digitalocean.com/tiqr/tiqr-agent:$TAG"
docker push registry.digitalocean.com/tiqr/tiqr-agent:latest

# 2. Ensure managed Valkey exists (if missing)
doctl databases create tiqr-agent-valkey --engine valkey --region sgp1 --size db-s-1vcpu-1gb --num-nodes 1 --wait

# 3. Generate local spec from DOKS config.toml → WHATOMATE_* envs
python3 digitalocean/app-platform/generate-local-spec.py

# 4. Create or update the app
doctl apps create --spec digitalocean/app-platform/tiqr-agent.local.yaml --format ID,DefaultIngress,Created
# doctl apps update d9095d9a-eadd-497b-ab6a-36b8dcc049db --spec digitalocean/app-platform/tiqr-agent.local.yaml
```

## After deploy

1. Confirm `/health` on the default `*.ondigitalocean.app` ingress.
2. Point Vercel UI at that API origin (CORS: update `WHATOMATE_SERVER_ALLOWED_ORIGINS`).
3. Cut DNS when ready (e.g. `agent-api.tiqr.store` → App Platform); leave `agent.tiqr.store` on Vercel for the UI.
