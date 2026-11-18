#!/usr/bin/env python3
"""Build tiqr-agent.local.yaml from the template + DOKS production config.toml.

Converts config.toml keys into individual WHATOMATE_* App Platform envs.
Redis is left to the Valkey binding in the template (not copied from DOKS).
"""

from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
TEMPLATE = Path(__file__).resolve().parent / "tiqr-agent.yaml"
SECRETS = ROOT / "digitalocean/k8s/overlays/production/secrets.yaml"
OUT = Path(__file__).resolve().parent / "tiqr-agent.local.yaml"

# Sections / keys provided by App Platform Valkey binding or fixed runtime envs.
SKIP_SECTIONS = {"redis"}
SKIP_KEYS = {
    ("app", "environment"),  # set explicitly in template
    ("server", "host"),
    ("server", "port"),
    ("cookie", "secure"),
}

# Mark as App Platform SECRET type.
SECRET_KEYS = {
    ("app", "encryption_key"),
    ("database", "password"),
    ("jwt", "secret"),
    ("whatsapp", "webhook_verify_token"),
    ("whatsapp", "app_secret"),
    ("storage", "s3_key"),
    ("storage", "s3_secret"),
    ("default_admin", "password"),
    ("aisensy", "webhook_secret"),
    ("firebase", "credentials"),
    ("ticker", "pushkey"),
}

# No Go config struct yet — omit from App Platform envs.
SKIP_SECTIONS_NO_CODE = {"msg91"}


def extract_config_toml(secrets_text: str) -> str:
    marker = "  config.toml: |"
    start = secrets_text.find(marker)
    if start < 0:
        raise SystemExit(f"config.toml block not found in {SECRETS}")
    start = secrets_text.find("\n", start) + 1
    end = secrets_text.find("\n---\n", start)
    if end < 0:
        end = len(secrets_text)
    lines: list[str] = []
    for line in secrets_text[start:end].splitlines():
        if line.startswith("    "):
            lines.append(line[4:])
        elif line.strip() == "":
            lines.append("")
        else:
            break
    return "\n".join(lines).rstrip() + "\n"


def parse_toml_flat(toml_text: str) -> list[tuple[str, str, str]]:
    """Return [(section, key, value)] for simple TOML (no arrays/tables-in-tables)."""
    section = ""
    out: list[tuple[str, str, str]] = []
    for raw in toml_text.splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("[") and line.endswith("]"):
            section = line[1:-1].strip()
            continue
        if "=" not in line or not section:
            continue
        key, _, rest = line.partition("=")
        key = key.strip()
        if key.startswith("_"):
            continue  # commented-out / legacy keys in DOKS toml
        val = rest.strip()
        if val.startswith('"') and val.endswith('"'):
            val = val[1:-1]
        elif val.startswith("'") and val.endswith("'"):
            val = val[1:-1]
        out.append((section, key, val))
    return out


def yaml_escape(value: str) -> str:
    """Quote values that need it for App Platform env YAML."""
    if value == "":
        return '""'
    if any(c in value for c in ':"\'{}[]#&*!|>%@`\n') or value.strip() != value:
        # Prefer single quotes; double any embedded single quotes.
        return "'" + value.replace("'", "''") + "'"
    if value.lower() in {"true", "false", "null", "yes", "no"} or re.fullmatch(
        r"-?\d+(\.\d+)?", value
    ):
        return f'"{value}"'
    return value


def env_entries(pairs: list[tuple[str, str, str]]) -> str:
    lines: list[str] = []
    for section, key, value in pairs:
        if section in SKIP_SECTIONS or section in SKIP_SECTIONS_NO_CODE:
            continue
        if (section, key) in SKIP_KEYS:
            continue
        env_key = f"WHATOMATE_{section}_{key}".upper()
        lines.append(f"      - key: {env_key}")
        lines.append("        scope: RUN_TIME")
        if (section, key) in SECRET_KEYS:
            lines.append("        type: SECRET")
        lines.append(f"        value: {yaml_escape(value)}")
    return "\n".join(lines)


def main() -> None:
    template = TEMPLATE.read_text()
    marker = "      # BEGIN_GENERATED_ENVS\n"
    end_marker = "      # END_GENERATED_ENVS\n"
    if marker not in template or end_marker not in template:
        raise SystemExit("template missing BEGIN_GENERATED_ENVS / END_GENERATED_ENVS markers")

    cfg = extract_config_toml(SECRETS.read_text())
    pairs = parse_toml_flat(cfg)
    generated = env_entries(pairs) + "\n"

    start = template.index(marker) + len(marker)
    end = template.index(end_marker)
    filled = template[:start] + generated + template[end:]
    OUT.write_text(filled)
    count = generated.count("\n      - key:")
    print(f"Wrote {OUT} ({count} WHATOMATE_* envs from config.toml)")


if __name__ == "__main__":
    main()
