#!/usr/bin/env bash
#
# One-click Sub2API -> LightBridge migration orchestrator.
#
# This script wraps the repository's production data migrator
# (backend/cmd/sub2api-migrate) with service/config discovery, filesystem and
# database backup, OpenAI Provider module setup, Claude/Gemini compatibility
# verification, and rollback support.

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

GITHUB_REPO="${GITHUB_REPO:-WilliamWang1721/LightBridge}"
MODULE_RELEASE_TAG="${MODULE_RELEASE_TAG:-module-migration-20260606}"
MODULE_REGISTRY_URL="${MODULE_REGISTRY_URL:-https://github.com/${GITHUB_REPO}/releases/download/${MODULE_RELEASE_TAG}/registry.json}"
MODULE_PUBLIC_KEY_URL="${MODULE_PUBLIC_KEY_URL:-https://github.com/${GITHUB_REPO}/releases/download/${MODULE_RELEASE_TAG}/ed25519.pub}"

LIGHTBRIDGE_SERVICE="${LIGHTBRIDGE_SERVICE:-LightBridge}"
SUB2API_SERVICE="${SUB2API_SERVICE:-sub2api}"
LIGHTBRIDGE_USER="${LIGHTBRIDGE_USER:-LightBridge}"
SUB2API_USER="${SUB2API_USER:-sub2api}"

LIGHTBRIDGE_DIR="${LIGHTBRIDGE_DIR:-/opt/LightBridge}"
SUB2API_DIR="${SUB2API_DIR:-/opt/sub2api}"
LIGHTBRIDGE_CONFIG_DIR="${LIGHTBRIDGE_CONFIG_DIR:-/etc/LightBridge}"
SUB2API_CONFIG_DIR="${SUB2API_CONFIG_DIR:-/etc/sub2api}"
BACKUP_ROOT="${BACKUP_ROOT:-/opt/LightBridge-migration-backups}"

SOURCE_DRIVER="${SOURCE_DRIVER:-postgres}"
TARGET_DRIVER="${TARGET_DRIVER:-postgres}"
SOURCE_DSN="${SOURCE_DSN:-}"
TARGET_DSN="${TARGET_DSN:-}"
SOURCE_CONFIG="${SOURCE_CONFIG:-}"
TARGET_CONFIG="${TARGET_CONFIG:-}"
MIGRATOR_BIN="${MIGRATOR_BIN:-}"
LIGHTBRIDGE_BIN="${LIGHTBRIDGE_BIN:-$LIGHTBRIDGE_DIR/LightBridge}"
OPENAI_MODULE_PACKAGE="${OPENAI_MODULE_PACKAGE:-}"
OPENAI_MODULE_PUBLIC_KEY="${OPENAI_MODULE_PUBLIC_KEY:-}"
MODULE_DATA_DIR="${MODULE_DATA_DIR:-$LIGHTBRIDGE_DIR/data}"
SERVER_HOST="${SERVER_HOST:-0.0.0.0}"
SERVER_PORT="${SERVER_PORT:-8080}"
TARGET_VERSION="${TARGET_VERSION:-}"

DRY_RUN=false
NO_SERVICE=false
SKIP_OPENAI_PROVIDER=false
NO_AUTO_ROLLBACK=false
ASSUME_YES=false
COMMAND="migrate"
ROLLBACK_DIR=""

RED=$'\033[0;31m'
GREEN=$'\033[0;32m'
YELLOW=$'\033[1;33m'
BLUE=$'\033[0;34m'
NC=$'\033[0m'

log() { printf '%s[INFO]%s %s\n' "$BLUE" "$NC" "$*"; }
ok() { printf '%s[OK]%s %s\n' "$GREEN" "$NC" "$*"; }
warn() { printf '%s[WARN]%s %s\n' "$YELLOW" "$NC" "$*"; }
die() { printf '%s[ERROR]%s %s\n' "$RED" "$NC" "$*" >&2; exit 1; }

usage() {
  cat <<EOF
Usage:
  sudo $0 migrate [options]
  sudo $0 rollback --backup-dir <dir> [--target-dsn <dsn>]

Migration options:
  --source-dsn <dsn>              Legacy Sub2API database DSN
  --target-dsn <dsn>              Target LightBridge database DSN
  --source-config <path>          Legacy config.yaml path
  --target-config <path>          LightBridge config.yaml path
  --source-driver <driver>        postgres or sqlite (default: postgres)
  --target-driver <driver>        postgres or sqlite (default: postgres)
  --migrator-bin <path>           Existing sub2api-migrate binary
  --lightbridge-bin <path>        Installed LightBridge binary
  --version <vX.Y.Z>              Install/upgrade LightBridge release first
  --openai-module-package <path>  Local OpenAI Provider module package
  --openai-module-public-key <p>  Local OpenAI Provider signing public key
  --module-registry-url <url>     Module registry URL
  --module-data-dir <dir>         LightBridge module data dir
  --skip-openai-provider          Migrate accounts without installing provider
  --dry-run                       Scan only; do not write target DB or services
  --no-service                    Do not stop/start systemd services
  --no-auto-rollback              Do not auto-rollback service/files on failure
  -y, --yes                       Non-interactive confirmation

Rollback options:
  --backup-dir <dir>              Backup directory printed by migrate
  --target-dsn <dsn>              Optional DB restore target; defaults to manifest

Environment variables with the same uppercase names can also be used.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    migrate|rollback)
      COMMAND="$1"
      shift
      ;;
    --source-dsn)
      SOURCE_DSN="${2:-}"; shift 2
      ;;
    --target-dsn)
      TARGET_DSN="${2:-}"; shift 2
      ;;
    --source-config)
      SOURCE_CONFIG="${2:-}"; shift 2
      ;;
    --target-config)
      TARGET_CONFIG="${2:-}"; shift 2
      ;;
    --source-driver)
      SOURCE_DRIVER="${2:-}"; shift 2
      ;;
    --target-driver)
      TARGET_DRIVER="${2:-}"; shift 2
      ;;
    --migrator-bin)
      MIGRATOR_BIN="${2:-}"; shift 2
      ;;
    --lightbridge-bin)
      LIGHTBRIDGE_BIN="${2:-}"; shift 2
      ;;
    --version|-v)
      TARGET_VERSION="${2:-}"; shift 2
      ;;
    --openai-module-package)
      OPENAI_MODULE_PACKAGE="${2:-}"; shift 2
      ;;
    --openai-module-public-key)
      OPENAI_MODULE_PUBLIC_KEY="${2:-}"; shift 2
      ;;
    --module-registry-url)
      MODULE_REGISTRY_URL="${2:-}"; shift 2
      ;;
    --module-data-dir)
      MODULE_DATA_DIR="${2:-}"; shift 2
      ;;
    --backup-root)
      BACKUP_ROOT="${2:-}"; shift 2
      ;;
    --backup-dir)
      ROLLBACK_DIR="${2:-}"; shift 2
      ;;
    --skip-openai-provider)
      SKIP_OPENAI_PROVIDER=true; shift
      ;;
    --dry-run)
      DRY_RUN=true; shift
      ;;
    --no-service)
      NO_SERVICE=true; shift
      ;;
    --no-auto-rollback)
      NO_AUTO_ROLLBACK=true; shift
      ;;
    -y|--yes)
      ASSUME_YES=true; shift
      ;;
    -h|--help)
      usage; exit 0
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
done

require_root() {
  [[ "$(id -u)" -eq 0 ]] || die "Please run as root: sudo $0 $COMMAND ..."
}

have() {
  command -v "$1" >/dev/null 2>&1
}

confirm() {
  local prompt="$1"
  if [[ "$ASSUME_YES" == true || "$DRY_RUN" == true ]]; then
    return 0
  fi
  read -r -p "$prompt [y/N]: " answer
  [[ "$answer" == "y" || "$answer" == "Y" || "$answer" == "yes" || "$answer" == "YES" ]]
}

service_exists() {
  local name="$1"
  have systemctl || return 1
  local fragment
  fragment="$(systemctl show -p FragmentPath --value "$name" 2>/dev/null | head -1 || true)"
  [[ -n "$fragment" && "$fragment" != "n/a" ]]
}

service_fragment() {
  systemctl show -p FragmentPath --value "$1" 2>/dev/null | head -1 || true
}

service_exec() {
  systemctl show -p ExecStart --value "$1" 2>/dev/null |
    tr ' ' '\n' |
    sed -n 's/^path=//p' |
    head -1 || true
}

service_user() {
  systemctl show -p User --value "$1" 2>/dev/null | head -1 || true
}

service_env_value() {
  local service="$1"
  local key="$2"
  systemctl show -p Environment --value "$service" 2>/dev/null |
    tr ' ' '\n' |
    sed -n "s/^${key}=//p" |
    head -1 || true
}

find_first_file() {
  local candidate
  for candidate in "$@"; do
    [[ -f "$candidate" ]] && { printf '%s\n' "$candidate"; return 0; }
  done
  return 1
}

detect_configs() {
  if [[ -z "$SOURCE_CONFIG" ]]; then
    SOURCE_CONFIG="$(find_first_file \
      "$SUB2API_CONFIG_DIR/config.yaml" \
      "$SUB2API_DIR/config.yaml" \
      "$SUB2API_DIR/config.yml" \
      "$SUB2API_CONFIG_DIR/config.yml" || true)"
  fi
  if [[ -z "$TARGET_CONFIG" ]]; then
    TARGET_CONFIG="$(find_first_file \
      "$LIGHTBRIDGE_CONFIG_DIR/config.yaml" \
      "$LIGHTBRIDGE_DIR/config.yaml" \
      "$LIGHTBRIDGE_CONFIG_DIR/config.yml" \
      "$LIGHTBRIDGE_DIR/config.yml" || true)"
  fi
}

yaml_scalar() {
  local file="$1"
  local section="$2"
  local key="$3"
  [[ -f "$file" ]] || return 1
  awk -v section="$section" -v key="$key" '
    BEGIN { in_section = 0 }
    /^[[:space:]]*#/ { next }
    /^[[:alnum:]_]+:/ {
      in_section = ($1 == section ":")
      next
    }
    in_section && $0 ~ "^[[:space:]]+" key ":" {
      sub("^[[:space:]]+" key ":[[:space:]]*", "", $0)
      sub(/[[:space:]]+#.*$/, "", $0)
      gsub(/^"/, "", $0); gsub(/"$/, "", $0)
      gsub(/^'\''/, "", $0); gsub(/'\''$/, "", $0)
      print $0
      exit
    }
  ' "$file"
}

dsn_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\'/\\\'}"
  printf "'%s'" "$value"
}

dsn_from_config() {
  local file="$1"
  local host port user password dbname sslmode
  host="$(yaml_scalar "$file" database host || true)"
  port="$(yaml_scalar "$file" database port || true)"
  user="$(yaml_scalar "$file" database user || true)"
  password="$(yaml_scalar "$file" database password || true)"
  dbname="$(yaml_scalar "$file" database dbname || true)"
  sslmode="$(yaml_scalar "$file" database sslmode || true)"

  [[ -n "$host" && -n "$port" && -n "$user" && -n "$dbname" ]] || return 1
  [[ -n "$sslmode" ]] || sslmode="prefer"

  if [[ -n "$password" ]]; then
    printf 'host=%s port=%s user=%s password=%s dbname=%s sslmode=%s' \
      "$(dsn_escape "$host")" "$port" "$(dsn_escape "$user")" "$(dsn_escape "$password")" "$(dsn_escape "$dbname")" "$sslmode"
  else
    printf 'host=%s port=%s user=%s dbname=%s sslmode=%s' \
      "$(dsn_escape "$host")" "$port" "$(dsn_escape "$user")" "$(dsn_escape "$dbname")" "$sslmode"
  fi
}

detect_dsns() {
  detect_configs
  if [[ -z "$SOURCE_DSN" && -n "$SOURCE_CONFIG" ]]; then
    SOURCE_DSN="$(dsn_from_config "$SOURCE_CONFIG" || true)"
  fi
  if [[ -z "$TARGET_DSN" && -n "$TARGET_CONFIG" ]]; then
    TARGET_DSN="$(dsn_from_config "$TARGET_CONFIG" || true)"
  fi
  if [[ -z "$TARGET_DSN" && -n "$SOURCE_DSN" ]]; then
    TARGET_DSN="$SOURCE_DSN"
    TARGET_CONFIG="$SOURCE_CONFIG"
    warn "Target DSN not found; using legacy DSN as in-place target."
  fi
}

copy_path_if_exists() {
  local src="$1"
  local dst="$2"
  [[ -e "$src" ]] || return 0
  mkdir -p "$(dirname "$dst")"
  cp -a "$src" "$dst"
}

copy_dir_contents() {
  local src="$1"
  local dst="$2"
  [[ -d "$src" ]] || return 0
  mkdir -p "$dst"
  cp -a "$src"/. "$dst"/
}

backup_database() {
  local label="$1"
  local driver="$2"
  local dsn="$3"
  local out_dir="$4"
  [[ -n "$dsn" ]] || return 0

  mkdir -p "$out_dir"
  if [[ "$driver" == "postgres" ]]; then
    if have pg_dump; then
      log "Backing up $label PostgreSQL database..."
      if pg_dump "$dsn" --format=custom --file="$out_dir/${label}.dump"; then
        ok "$label database backup: $out_dir/${label}.dump"
      else
        warn "pg_dump failed for $label; continuing with filesystem backup only."
      fi
    else
      warn "pg_dump not found; $label database backup skipped."
    fi
  elif [[ "$driver" == "sqlite" && -f "$dsn" ]]; then
    copy_path_if_exists "$dsn" "$out_dir/${label}.sqlite"
  fi
}

create_backup() {
  local timestamp
  timestamp="$(date +%Y%m%d-%H%M%S)"
  BACKUP_DIR="$BACKUP_ROOT/$timestamp"
  mkdir -p "$BACKUP_DIR/files" "$BACKUP_DIR/db"

  log "Creating migration backup: $BACKUP_DIR"
  copy_path_if_exists "$SUB2API_DIR" "$BACKUP_DIR/files/opt-sub2api"
  copy_path_if_exists "$SUB2API_CONFIG_DIR" "$BACKUP_DIR/files/etc-sub2api"
  copy_path_if_exists "$LIGHTBRIDGE_DIR" "$BACKUP_DIR/files/opt-LightBridge"
  copy_path_if_exists "$LIGHTBRIDGE_CONFIG_DIR" "$BACKUP_DIR/files/etc-LightBridge"

  if service_exists "$SUB2API_SERVICE"; then
    copy_path_if_exists "$(service_fragment "$SUB2API_SERVICE")" "$BACKUP_DIR/files/$(basename "$(service_fragment "$SUB2API_SERVICE")")"
  fi
  if service_exists "$LIGHTBRIDGE_SERVICE"; then
    copy_path_if_exists "$(service_fragment "$LIGHTBRIDGE_SERVICE")" "$BACKUP_DIR/files/$(basename "$(service_fragment "$LIGHTBRIDGE_SERVICE")")"
  fi

  backup_database "source" "$SOURCE_DRIVER" "$SOURCE_DSN" "$BACKUP_DIR/db"
  backup_database "target" "$TARGET_DRIVER" "$TARGET_DSN" "$BACKUP_DIR/db"

  {
    printf 'CREATED_AT=%q\n' "$timestamp"
    printf 'SOURCE_DRIVER=%q\n' "$SOURCE_DRIVER"
    printf 'TARGET_DRIVER=%q\n' "$TARGET_DRIVER"
    printf 'SOURCE_DSN=%q\n' "$SOURCE_DSN"
    printf 'TARGET_DSN=%q\n' "$TARGET_DSN"
    printf 'SOURCE_CONFIG=%q\n' "$SOURCE_CONFIG"
    printf 'TARGET_CONFIG=%q\n' "$TARGET_CONFIG"
    printf 'LIGHTBRIDGE_DIR=%q\n' "$LIGHTBRIDGE_DIR"
    printf 'SUB2API_DIR=%q\n' "$SUB2API_DIR"
    printf 'LIGHTBRIDGE_CONFIG_DIR=%q\n' "$LIGHTBRIDGE_CONFIG_DIR"
    printf 'SUB2API_CONFIG_DIR=%q\n' "$SUB2API_CONFIG_DIR"
    printf 'LIGHTBRIDGE_SERVICE=%q\n' "$LIGHTBRIDGE_SERVICE"
    printf 'SUB2API_SERVICE=%q\n' "$SUB2API_SERVICE"
  } > "$BACKUP_DIR/manifest.env"
  ok "Backup created: $BACKUP_DIR"
}

ensure_user() {
  local user="$1"
  local home="$2"
  if id "$user" >/dev/null 2>&1; then
    return 0
  fi
  useradd -r -s /bin/sh -d "$home" "$user"
}

install_lightbridge_service() {
  mkdir -p /etc/systemd/system "$LIGHTBRIDGE_DIR" "$LIGHTBRIDGE_DIR/data" "$LIGHTBRIDGE_CONFIG_DIR"
  ensure_user "$LIGHTBRIDGE_USER" "$LIGHTBRIDGE_DIR"

  cat > "/etc/systemd/system/${LIGHTBRIDGE_SERVICE}.service" <<EOF
[Unit]
Description=LightBridge - AI API Gateway Platform
Documentation=https://github.com/${GITHUB_REPO}
After=network.target postgresql.service redis.service
Wants=postgresql.service redis.service

[Service]
Type=simple
User=${LIGHTBRIDGE_USER}
Group=${LIGHTBRIDGE_USER}
WorkingDirectory=${LIGHTBRIDGE_DIR}
ExecStart=${LIGHTBRIDGE_BIN}
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${LIGHTBRIDGE_SERVICE}
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=${LIGHTBRIDGE_DIR} ${LIGHTBRIDGE_CONFIG_DIR}
Environment=GIN_MODE=release
Environment=DATA_DIR=${LIGHTBRIDGE_DIR}
Environment=SERVER_HOST=${SERVER_HOST}
Environment=SERVER_PORT=${SERVER_PORT}

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable "$LIGHTBRIDGE_SERVICE" >/dev/null 2>&1 || true
}

detect_lightbridge_binary() {
  if [[ -f "$LIGHTBRIDGE_BIN" ]]; then
    return 0
  fi
  if service_exists "$LIGHTBRIDGE_SERVICE"; then
    local from_service
    from_service="$(service_exec "$LIGHTBRIDGE_SERVICE")"
    if [[ -n "$from_service" && -f "$from_service" ]]; then
      LIGHTBRIDGE_BIN="$from_service"
      return 0
    fi
  fi
  return 1
}

download_lightbridge_release() {
  local version="$1"
  [[ -n "$version" ]] || return 0
  have curl || die "curl is required to download LightBridge release."
  have tar || die "tar is required to extract LightBridge release."

  local os arch archive version_num tmp
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$arch" in
    x86_64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) die "Unsupported architecture: $arch" ;;
  esac
  [[ "$version" == v* ]] || version="v$version"
  version_num="${version#v}"
  archive="LightBridge_${version_num}_${os}_${arch}.tar.gz"
  tmp="$(mktemp -d)"

  log "Downloading LightBridge $version..."
  curl -fsSL "https://github.com/${GITHUB_REPO}/releases/download/${version}/${archive}" -o "$tmp/$archive"
  tar -xzf "$tmp/$archive" -C "$tmp"
  mkdir -p "$(dirname "$LIGHTBRIDGE_BIN")"
  cp "$tmp/LightBridge" "$LIGHTBRIDGE_BIN"
  chmod +x "$LIGHTBRIDGE_BIN"
  rm -rf "$tmp"
  ok "Installed LightBridge binary: $LIGHTBRIDGE_BIN"
}

build_migrator() {
  if [[ -n "$MIGRATOR_BIN" && -x "$MIGRATOR_BIN" ]]; then
    return 0
  fi
  if [[ -x "$LIGHTBRIDGE_DIR/sub2api-migrate" ]]; then
    MIGRATOR_BIN="$LIGHTBRIDGE_DIR/sub2api-migrate"
    return 0
  fi
  [[ -f "$REPO_ROOT/backend/cmd/sub2api-migrate/main.go" ]] || die "Cannot find backend/cmd/sub2api-migrate in $REPO_ROOT"
  have go || die "go is required to build backend/cmd/sub2api-migrate. Or pass --migrator-bin."

  mkdir -p "$LIGHTBRIDGE_DIR/bin"
  log "Building sub2api-migrate..."
  (cd "$REPO_ROOT/backend" && go build -o "$LIGHTBRIDGE_DIR/bin/sub2api-migrate" ./cmd/sub2api-migrate)
  MIGRATOR_BIN="$LIGHTBRIDGE_DIR/bin/sub2api-migrate"
  ok "Migrator built: $MIGRATOR_BIN"
}

configure_module_release() {
  local config_file="$TARGET_CONFIG"
  local key_path="$MODULE_DATA_DIR/modules/ed25519.pub"
  mkdir -p "$(dirname "$key_path")" "$LIGHTBRIDGE_CONFIG_DIR"

  if [[ -z "$config_file" ]]; then
    config_file="$LIGHTBRIDGE_CONFIG_DIR/config.yaml"
    TARGET_CONFIG="$config_file"
  fi
  touch "$config_file"

  if [[ -z "$OPENAI_MODULE_PUBLIC_KEY" ]]; then
    if have curl && curl -fsSL "$MODULE_PUBLIC_KEY_URL" -o "$key_path" 2>/dev/null; then
      OPENAI_MODULE_PUBLIC_KEY="$key_path"
      ok "Downloaded OpenAI Provider signing key: $key_path"
    else
      warn "Could not download module signing public key; provider install will continue without signature key unless --openai-module-public-key is passed."
    fi
  fi

  if grep -q '^modules:' "$config_file"; then
    grep -q 'marketplace_registry_url:' "$config_file" || sed -i '/^modules:/a\  marketplace_registry_url: "'"$MODULE_REGISTRY_URL"'"' "$config_file"
    if [[ -n "$OPENAI_MODULE_PUBLIC_KEY" ]]; then
      grep -q 'signature_public_key_path:' "$config_file" || sed -i '/^modules:/a\  signature_public_key_path: "'"$OPENAI_MODULE_PUBLIC_KEY"'"' "$config_file"
    fi
    grep -q 'data_dir:' "$config_file" || sed -i '/^modules:/a\  data_dir: "'"$MODULE_DATA_DIR"'"' "$config_file"
  else
    cat >> "$config_file" <<EOF

modules:
  data_dir: "$MODULE_DATA_DIR"
  marketplace_registry_url: "$MODULE_REGISTRY_URL"
  signature_public_key_path: "$OPENAI_MODULE_PUBLIC_KEY"
  marketplace_timeout_seconds: 20
EOF
  fi
}

copy_legacy_runtime() {
  mkdir -p "$LIGHTBRIDGE_DIR" "$LIGHTBRIDGE_CONFIG_DIR" "$MODULE_DATA_DIR"
  copy_dir_contents "$SUB2API_CONFIG_DIR" "$LIGHTBRIDGE_CONFIG_DIR"
  copy_dir_contents "$SUB2API_DIR/data" "$LIGHTBRIDGE_DIR/data"
  copy_path_if_exists "$SUB2API_DIR/config.yaml" "$LIGHTBRIDGE_CONFIG_DIR/config.yaml"
  copy_path_if_exists "$SUB2API_CONFIG_DIR/config.yaml" "$LIGHTBRIDGE_CONFIG_DIR/config.yaml"
  copy_path_if_exists "$SUB2API_DIR/.installed" "$LIGHTBRIDGE_DIR/.installed"
  copy_path_if_exists "$SUB2API_CONFIG_DIR/.installed" "$LIGHTBRIDGE_DIR/.installed"
  copy_path_if_exists "$SUB2API_CONFIG_DIR/.installed" "$LIGHTBRIDGE_CONFIG_DIR/.installed"

  if [[ -z "$TARGET_CONFIG" ]]; then
    TARGET_CONFIG="$(find_first_file "$LIGHTBRIDGE_CONFIG_DIR/config.yaml" "$LIGHTBRIDGE_DIR/config.yaml" || true)"
  fi
}

stop_services_for_migration() {
  [[ "$NO_SERVICE" == true ]] && return 0
  if service_exists "$SUB2API_SERVICE"; then
    systemctl stop "$SUB2API_SERVICE" 2>/dev/null || true
  fi
  if service_exists "$LIGHTBRIDGE_SERVICE"; then
    systemctl stop "$LIGHTBRIDGE_SERVICE" 2>/dev/null || true
  fi
}

disable_legacy_service() {
  [[ "$NO_SERVICE" == true ]] && return 0
  if service_exists "$SUB2API_SERVICE"; then
    systemctl disable "$SUB2API_SERVICE" 2>/dev/null || true
    local fragment
    fragment="$(service_fragment "$SUB2API_SERVICE")"
    if [[ -n "$fragment" && "$fragment" == /etc/systemd/system/* && -f "$fragment" ]]; then
      mv "$fragment" "${fragment}.migrated-to-LightBridge" 2>/dev/null || true
    fi
    systemctl daemon-reload
  fi
}

start_lightbridge_once_for_schema() {
  [[ "$NO_SERVICE" == true || "$DRY_RUN" == true ]] && return 0
  detect_lightbridge_binary || die "LightBridge binary not found. Pass --version or --lightbridge-bin."
  install_lightbridge_service

  log "Starting LightBridge briefly so it can apply database migrations..."
  systemctl start "$LIGHTBRIDGE_SERVICE"
  sleep 8
  if ! systemctl is-active --quiet "$LIGHTBRIDGE_SERVICE"; then
    journalctl -u "$LIGHTBRIDGE_SERVICE" -n 80 --no-pager || true
    die "LightBridge failed to start for schema migration."
  fi
  systemctl stop "$LIGHTBRIDGE_SERVICE" 2>/dev/null || true
}

run_data_migrator() {
  local args=(
    "-source-driver" "$SOURCE_DRIVER"
    "-source-dsn" "$SOURCE_DSN"
    "-target-driver" "$TARGET_DRIVER"
    "-target-dsn" "$TARGET_DSN"
    "-module-data-dir" "$MODULE_DATA_DIR"
    "-module-registry-url" "$MODULE_REGISTRY_URL"
  )

  [[ "$DRY_RUN" == true ]] && args+=("-dry-run")
  [[ "$SKIP_OPENAI_PROVIDER" == true ]] && args+=("-skip-openai-module-install")
  [[ -n "$OPENAI_MODULE_PACKAGE" ]] && args+=("-openai-module-package" "$OPENAI_MODULE_PACKAGE")
  [[ -n "$OPENAI_MODULE_PUBLIC_KEY" ]] && args+=("-openai-module-public-key" "$OPENAI_MODULE_PUBLIC_KEY")

  log "Running Sub2API production data migration..."
  "$MIGRATOR_BIN" "${args[@]}" | tee "$BACKUP_DIR/sub2api-migrate-report.json"
}

verify_compatibility_modes() {
  [[ "$TARGET_DRIVER" == "postgres" ]] || return 0
  have psql || { warn "psql not found; skipping post-migration compatibility verification."; return 0; }
  [[ -n "$TARGET_DSN" ]] || return 0

  local sql
  sql="
WITH compatible AS (
  SELECT platform, COUNT(*) AS count
  FROM accounts
  WHERE deleted_at IS NULL
    AND platform IN ('anthropic', 'gemini')
    AND COALESCE((extra->'module_migration'->>'compatibility_mode')::boolean, false) = true
  GROUP BY platform
),
openai_module AS (
  SELECT COUNT(*) AS count
  FROM installed_modules
  WHERE id = 'openai' AND status IN ('installed', 'enabled', 'disabled')
)
SELECT 'openai_provider_installed', count::text FROM openai_module
UNION ALL
SELECT 'compat_' || platform, count::text FROM compatible
ORDER BY 1;"
  log "Verifying OpenAI Provider and Claude/Gemini compatibility markers..."
  psql "$TARGET_DSN" -Atc "$sql" | tee "$BACKUP_DIR/post-migration-verification.txt" || warn "Compatibility verification query failed."
}

finish_services() {
  [[ "$NO_SERVICE" == true || "$DRY_RUN" == true ]] && return 0
  disable_legacy_service
  install_lightbridge_service
  chown -R "$LIGHTBRIDGE_USER:$LIGHTBRIDGE_USER" "$LIGHTBRIDGE_DIR" "$LIGHTBRIDGE_CONFIG_DIR" 2>/dev/null || true
  systemctl start "$LIGHTBRIDGE_SERVICE"
  ok "LightBridge service started."
}

restore_path() {
  local backup_path="$1"
  local target_path="$2"
  [[ -e "$backup_path" ]] || return 0
  rm -rf "$target_path"
  mkdir -p "$(dirname "$target_path")"
  cp -a "$backup_path" "$target_path"
}

rollback_files() {
  local dir="$1"
  [[ -d "$dir/files" ]] || die "Invalid backup directory: $dir"
  log "Restoring files from $dir..."
  restore_path "$dir/files/opt-sub2api" "$SUB2API_DIR"
  restore_path "$dir/files/etc-sub2api" "$SUB2API_CONFIG_DIR"
  restore_path "$dir/files/opt-LightBridge" "$LIGHTBRIDGE_DIR"
  restore_path "$dir/files/etc-LightBridge" "$LIGHTBRIDGE_CONFIG_DIR"

  if [[ -f "$dir/files/${SUB2API_SERVICE}.service" ]]; then
    cp -a "$dir/files/${SUB2API_SERVICE}.service" "/etc/systemd/system/${SUB2API_SERVICE}.service"
  fi
  if [[ -f "$dir/files/${LIGHTBRIDGE_SERVICE}.service" ]]; then
    cp -a "$dir/files/${LIGHTBRIDGE_SERVICE}.service" "/etc/systemd/system/${LIGHTBRIDGE_SERVICE}.service"
  fi
  have systemctl && systemctl daemon-reload
}

rollback_database() {
  local dir="$1"
  local manifest="$dir/manifest.env"
  local restore_dsn="$TARGET_DSN"
  if [[ -f "$manifest" ]]; then
    # shellcheck disable=SC1090
    source "$manifest"
  fi
  [[ -n "$restore_dsn" ]] || restore_dsn="${TARGET_DSN:-}"
  [[ -n "$restore_dsn" ]] || { warn "No target DSN available; database rollback skipped."; return 0; }

  if [[ -f "$dir/db/target.dump" ]]; then
    if have pg_restore; then
      warn "Restoring target PostgreSQL backup. This may overwrite migrated data."
      pg_restore --clean --if-exists --no-owner --dbname="$restore_dsn" "$dir/db/target.dump"
      ok "Database restored from $dir/db/target.dump"
    else
      warn "pg_restore not found; database rollback skipped."
    fi
  else
    warn "No target.dump found; database rollback skipped."
  fi
}

rollback() {
  require_root
  [[ -n "$ROLLBACK_DIR" ]] || die "rollback requires --backup-dir <dir>"
  rollback_files "$ROLLBACK_DIR"
  rollback_database "$ROLLBACK_DIR"
  if [[ "$NO_SERVICE" != true ]] && have systemctl; then
    systemctl stop "$LIGHTBRIDGE_SERVICE" 2>/dev/null || true
    systemctl enable "$SUB2API_SERVICE" 2>/dev/null || true
    systemctl start "$SUB2API_SERVICE" 2>/dev/null || true
  fi
  ok "Rollback complete: $ROLLBACK_DIR"
}

auto_rollback_on_failure() {
  local exit_code=$?
  [[ "$exit_code" -eq 0 ]] && return 0
  [[ "$COMMAND" != "migrate" ]] && return "$exit_code"
  [[ "$NO_AUTO_ROLLBACK" == true || "$DRY_RUN" == true ]] && return "$exit_code"
  [[ -n "${BACKUP_DIR:-}" && -d "${BACKUP_DIR:-}" ]] || return "$exit_code"
  warn "Migration failed. Restoring filesystem/service backup from $BACKUP_DIR"
  rollback_files "$BACKUP_DIR" || true
  warn "Database was not auto-restored. To restore DB too, run:"
  warn "  sudo $0 rollback --backup-dir $BACKUP_DIR --target-dsn '<target dsn>'"
  return "$exit_code"
}

migrate() {
  require_root
  detect_configs
  detect_dsns

  [[ -n "$SOURCE_DSN" ]] || die "Source DSN not found. Pass --source-dsn or --source-config."
  [[ -n "$TARGET_DSN" ]] || die "Target DSN not found. Pass --target-dsn or --target-config."

  log "Source driver: $SOURCE_DRIVER"
  log "Target driver: $TARGET_DRIVER"
  [[ -n "$SOURCE_CONFIG" ]] && log "Source config: $SOURCE_CONFIG"
  [[ -n "$TARGET_CONFIG" ]] && log "Target config: $TARGET_CONFIG"

  confirm "Proceed with Sub2API -> LightBridge migration?" || die "Cancelled."

  create_backup
  [[ "$DRY_RUN" == true ]] || stop_services_for_migration

  if [[ -n "$TARGET_VERSION" && "$DRY_RUN" != true ]]; then
    download_lightbridge_release "$TARGET_VERSION"
  fi
  if [[ "$DRY_RUN" != true ]]; then
    copy_legacy_runtime
    configure_module_release
    start_lightbridge_once_for_schema
  fi

  build_migrator
  run_data_migrator
  verify_compatibility_modes
  finish_services

  ok "Sub2API migration completed."
  log "Backup directory: $BACKUP_DIR"
  log "Migration report: $BACKUP_DIR/sub2api-migrate-report.json"
  log "Rollback command: sudo $0 rollback --backup-dir $BACKUP_DIR"
}

trap auto_rollback_on_failure EXIT

case "$COMMAND" in
  migrate) migrate ;;
  rollback) rollback ;;
  *) usage; exit 1 ;;
esac
