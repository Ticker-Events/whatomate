#!/bin/sh
# App Platform: materialize optional WHATOMATE_CONFIG_TOML, or empty file for
# env-only WHATOMATE_* vars. On DOKS the config.toml Secret is volume-mounted
# and Kubernetes `command` replaces this entrypoint entirely.
#
# Migrations: for `server`, append -migrate (unless WHATOMATE_SKIP_MIGRATE=1 or
# already present). Migrate runs inside the server process AFTER ListenAndServe
# so App Platform readiness probes are not connection-refused during schema apply.
set -e
umask 077
CONFIG_FILE=/tmp/whatomate-config.toml
if [ -n "${WHATOMATE_CONFIG_TOML:-}" ]; then
  # shellcheck disable=SC2016
  printf '%s\n' "$WHATOMATE_CONFIG_TOML" > "$CONFIG_FILE"
elif [ ! -f "$CONFIG_FILE" ]; then
  : > "$CONFIG_FILE"
fi

# Default to server when no args (matches Dockerfile CMD convention).
if [ "$#" -eq 0 ]; then
  set -- server -config config.toml
fi

if [ "$1" = "server" ] && [ "${WHATOMATE_SKIP_MIGRATE:-}" != "1" ]; then
  has_migrate=0
  for arg in "$@"; do
    if [ "$arg" = "-migrate" ]; then
      has_migrate=1
      break
    fi
  done
  if [ "$has_migrate" -eq 0 ]; then
    set -- "$@" -migrate
  fi
fi

exec ./whatomate "$@"
