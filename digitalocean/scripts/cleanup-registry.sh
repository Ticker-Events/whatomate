#!/usr/bin/env bash
# Periodically prune stale DigitalOcean Container Registry tags.
#
# Keeps:
#   - protected tags (default: latest)
#   - the newest KEEP_COUNT version tags (by UpdatedAt)
#   - any tag younger than MAX_AGE_DAYS
#
# Deletes tags that are older than MAX_AGE_DAYS AND outside the newest KEEP_COUNT.
# After deletions, starts garbage collection to reclaim blob storage.
#
# Usage:
#   bash digitalocean/scripts/cleanup-registry.sh              # loop every INTERVAL
#   ONCE=1 bash digitalocean/scripts/cleanup-registry.sh       # single pass
#   DRY_RUN=1 bash digitalocean/scripts/cleanup-registry.sh    # print only
#
# Requires: doctl (authenticated), jq, date
set -euo pipefail

REGISTRY_NAME="${REGISTRY_NAME:-tiqr}"
# Comma-separated repository names (without registry host/prefix)
REPOSITORIES="${REPOSITORIES:-tiqr-agent,tiqr-agent-ui}"
MAX_AGE_DAYS="${MAX_AGE_DAYS:-3}"
KEEP_COUNT="${KEEP_COUNT:-2}"
# Comma-separated tags that are never deleted
PROTECTED_TAGS="${PROTECTED_TAGS:-latest}"
INTERVAL="${INTERVAL:-24h}"
ONCE="${ONCE:-0}"
DRY_RUN="${DRY_RUN:-0}"
RUN_GC="${RUN_GC:-1}"

if ! command -v doctl >/dev/null 2>&1; then
  echo "error: doctl is required" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "error: jq is required" >&2
  exit 1
fi

# Convert INTERVAL like 30s / 15m / 24h / 1d / 86400 into seconds (macOS sleep needs seconds).
interval_to_seconds() {
  local raw="$1"
  if [[ "$raw" =~ ^([0-9]+)([smhd])$ ]]; then
    local n="${BASH_REMATCH[1]}"
    case "${BASH_REMATCH[2]}" in
      s) echo "$n" ;;
      m) echo $((n * 60)) ;;
      h) echo $((n * 3600)) ;;
      d) echo $((n * 86400)) ;;
    esac
    return 0
  fi
  if [[ "$raw" =~ ^[0-9]+$ ]]; then
    echo "$raw"
    return 0
  fi
  echo "error: invalid INTERVAL='${raw}' (use seconds or Ns/Nm/Nh/Nd)" >&2
  return 1
}

INTERVAL_SECS="$(interval_to_seconds "$INTERVAL")"

is_protected() {
  local tag="$1"
  local p
  IFS=',' read -ra _protected <<<"${PROTECTED_TAGS}"
  for p in "${_protected[@]}"; do
    p="${p// /}"
    [[ -n "$p" && "$tag" == "$p" ]] && return 0
  done
  return 1
}

# Portable epoch seconds from an RFC3339 / ISO8601 timestamp.
to_epoch() {
  local ts="$1"
  if date -u -d "$ts" +%s >/dev/null 2>&1; then
    date -u -d "$ts" +%s
  else
    # macOS BSD date
    date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "${ts%.*}Z" +%s 2>/dev/null \
      || date -u -j -f "%Y-%m-%dT%H:%M:%S%z" "$(echo "$ts" | sed -E 's/(\.[0-9]+)?([+-][0-9]{2}):([0-9]{2})$/\2\3/; s/Z$/+0000/')" +%s
  fi
}

cleanup_repo() {
  local repo="$1"
  local now cutoff deleted=0 kept=0 skipped_protected=0
  DELETES_THIS_PASS=0
  now="$(date -u +%s)"
  cutoff=$((now - MAX_AGE_DAYS * 86400))

  echo "--- repository: ${repo} ---"

  local tags_json
  if ! tags_json="$(doctl registry repository list-tags "${repo}" -o json 2>/dev/null)"; then
    echo "  warning: failed to list tags for ${repo}; skipping"
    return 0
  fi

  if [[ -z "$tags_json" || "$tags_json" == "null" || "$tags_json" == "[]" ]]; then
    echo "  no tags"
    return 0
  fi

  # Sort all tags by UpdatedAt descending (newest first).
  local sorted
  sorted="$(jq -c 'sort_by(.updated_at) | reverse | .[]' <<<"$tags_json")"

  # Collect newest KEEP_COUNT non-protected tags to retain as "latest versions".
  local -a keep_tags=()
  local line tag updated_at digest epoch
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    tag="$(jq -r '.tag // empty' <<<"$line")"
    [[ -z "$tag" ]] && continue
    if is_protected "$tag"; then
      continue
    fi
    if ((${#keep_tags[@]} < KEEP_COUNT)); then
      keep_tags+=("$tag")
    fi
  done <<<"$sorted"

  echo "  keep latest ${KEEP_COUNT} versions: ${keep_tags[*]:-(none)}"
  echo "  protected tags: ${PROTECTED_TAGS}"
  echo "  max age: ${MAX_AGE_DAYS}d (cutoff epoch ${cutoff})"

  local -a to_delete=()
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    tag="$(jq -r '.tag // empty' <<<"$line")"
    updated_at="$(jq -r '.updated_at // empty' <<<"$line")"
    digest="$(jq -r '.manifest_digest // empty' <<<"$line")"
    [[ -z "$tag" || -z "$updated_at" ]] && continue

    if is_protected "$tag"; then
      echo "  keep  ${tag}  (protected)  updated=${updated_at}"
      skipped_protected=$((skipped_protected + 1))
      kept=$((kept + 1))
      continue
    fi

    local in_keep=0
    local k
    for k in "${keep_tags[@]+"${keep_tags[@]}"}"; do
      if [[ "$tag" == "$k" ]]; then
        in_keep=1
        break
      fi
    done
    if ((in_keep)); then
      echo "  keep  ${tag}  (latest ${KEEP_COUNT})  updated=${updated_at}"
      kept=$((kept + 1))
      continue
    fi

    if ! epoch="$(to_epoch "$updated_at")"; then
      echo "  skip  ${tag}  (unparseable UpdatedAt: ${updated_at})"
      continue
    fi

    if ((epoch >= cutoff)); then
      echo "  keep  ${tag}  (younger than ${MAX_AGE_DAYS}d)  updated=${updated_at}"
      kept=$((kept + 1))
      continue
    fi

    echo "  delete ${tag}  updated=${updated_at}  digest=${digest}"
    to_delete+=("$tag")
  done <<<"$sorted"

  if ((${#to_delete[@]} == 0)); then
    echo "  nothing to delete (kept=${kept}, protected=${skipped_protected})"
    return 0
  fi

  if [[ "$DRY_RUN" == "1" ]]; then
    echo "  DRY_RUN=1 — would delete ${#to_delete[@]} tag(s): ${to_delete[*]}"
    DELETES_THIS_PASS=${#to_delete[@]}
    return 0
  fi

  doctl registry repository delete-tag "${repo}" "${to_delete[@]}" --force
  deleted=${#to_delete[@]}
  DELETES_THIS_PASS=$deleted
  echo "  deleted ${deleted} tag(s); kept ${kept}"
}

run_garbage_collection() {
  if [[ "$RUN_GC" != "1" ]]; then
    echo "Skipping garbage collection (RUN_GC=${RUN_GC})"
    return 0
  fi
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "DRY_RUN=1 — would start garbage collection on registry '${REGISTRY_NAME}'"
    return 0
  fi

  echo "Starting garbage collection on registry '${REGISTRY_NAME}'..."
  # Include untagged manifests left behind after tag deletes.
  if doctl registry garbage-collection start "${REGISTRY_NAME}" \
    --include-untagged-manifests \
    --force; then
    echo "Garbage collection started."
  else
    echo "warning: failed to start garbage collection (another GC may be active)" >&2
  fi
}

cleanup_once() {
  echo "=== DO registry cleanup @ $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="
  echo "registry=${REGISTRY_NAME} repos=${REPOSITORIES} max_age=${MAX_AGE_DAYS}d keep=${KEEP_COUNT} dry_run=${DRY_RUN}"

  local repo
  local any_deleted=0
  IFS=',' read -ra _repos <<<"${REPOSITORIES}"
  for repo in "${_repos[@]}"; do
    repo="${repo// /}"
    [[ -z "$repo" ]] && continue
    if cleanup_repo "$repo"; then
      :
    fi
    # cleanup_repo prints "deleted N"; detect via a return code would be cleaner —
    # use a side channel: DELETES_THIS_PASS set by cleanup_repo.
    if [[ "${DELETES_THIS_PASS:-0}" -gt 0 ]]; then
      any_deleted=1
    fi
  done

  if [[ "$any_deleted" == "1" ]]; then
    run_garbage_collection
  else
    echo "No tags deleted; skipping garbage collection."
  fi
  echo "=== cleanup pass complete ==="
}

# Exit the sleep/loop promptly on Ctrl-C / SIGTERM.
trap 'echo "received signal, exiting"; exit 0' INT TERM

if [[ "$ONCE" == "1" ]]; then
  cleanup_once
  exit 0
fi

echo "Running periodic cleanup every ${INTERVAL} (${INTERVAL_SECS}s). Set ONCE=1 for a single pass."
while true; do
  cleanup_once || echo "warning: cleanup pass failed; will retry after ${INTERVAL}" >&2
  echo "Sleeping ${INTERVAL} (${INTERVAL_SECS}s)..."
  sleep "${INTERVAL_SECS}"
done
