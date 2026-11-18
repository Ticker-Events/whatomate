#!/bin/sh
# App Platform: materialize optional WHATOMATE_CONFIG_TOML, or empty file for
# env-only WHATOMATE_* vars. On DOKS the config.toml Secret is volume-mounted
# and Kubernetes `command` replaces this entrypoint entirely.
set -e
umask 077
CONFIG_FILE=/tmp/whatomate-config.toml
if [ -n "${WHATOMATE_CONFIG_TOML:-}" ]; then
  # shellcheck disable=SC2016
  printf '%s\n' "$WHATOMATE_CONFIG_TOML" > "$CONFIG_FILE"
elif [ ! -f "$CONFIG_FILE" ]; then
  : > "$CONFIG_FILE"
fi

# Use -config from args when present so migrate and server share the same file.
MIGRATE_CONFIG="$CONFIG_FILE"
prev=""
for arg in "$@"; do
  if [ "$prev" = "-config" ]; then
    MIGRATE_CONFIG="$arg"
    break
  fi
  prev="$arg"
done

./whatomate migrate -config "$MIGRATE_CONFIG"
exec ./whatomate "$@"
