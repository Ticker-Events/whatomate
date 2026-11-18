#!/bin/sh
# App Platform: materialize optional WHATOMATE_CONFIG_TOML, or empty file for
# env-only WHATOMATE_* vars. On DOKS the config.toml Secret is volume-mounted.
set -e
umask 077
CONFIG_FILE=/tmp/whatomate-config.toml
if [ -n "${WHATOMATE_CONFIG_TOML:-}" ]; then
  # shellcheck disable=SC2016
  printf '%s\n' "$WHATOMATE_CONFIG_TOML" > "$CONFIG_FILE"
elif [ ! -f "$CONFIG_FILE" ]; then
  : > "$CONFIG_FILE"
fi
exec ./whatomate "$@"
