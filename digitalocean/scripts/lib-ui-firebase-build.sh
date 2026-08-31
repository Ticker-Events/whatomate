#!/usr/bin/env bash
# Resolve VITE_FIREBASE_CONFIG and append a base64 Docker build-arg (avoids JSON shell quoting bugs).
append_ui_firebase_build_args() {
  local -n _out=$1
  local root="${2:-.}"
  local config="${VITE_FIREBASE_CONFIG:-}"

  if [[ -z "$config" ]]; then
    local ui_env_file="${UI_ENV_FILE:-}"
    if [[ -z "$ui_env_file" ]]; then
      if [[ -f "$root/frontend/.env.production" ]]; then
        ui_env_file="$root/frontend/.env.production"
      elif [[ -f "$root/frontend/.env" ]]; then
        ui_env_file="$root/frontend/.env"
      fi
    fi
    if [[ -n "$ui_env_file" && -f "$ui_env_file" ]]; then
      config=$(grep -E '^VITE_FIREBASE_CONFIG=' "$ui_env_file" | head -1 | cut -d= -f2- || true)
    fi
  fi

  if [[ -z "$config" ]]; then
    return 1
  fi

  local b64
  b64=$(printf '%s' "$config" | base64 | tr -d '\n')
  _out+=(--build-arg "VITE_FIREBASE_CONFIG_B64=$b64")
  return 0
}
