#!/usr/bin/env bash
# ============================================================================
#  lightbridge — LightBridge 安装管理 CLI
# ============================================================================
#  用法:
#    lightbridge                        # 交互式 TUI 向导
#    lightbridge install [-v VERSION]   # 全新安装
#    lightbridge upgrade [-v VERSION]   # 升级到最新版本
#    lightbridge migrate                # 从 Sub2API 迁移
#    lightbridge docker                 # Docker 一键部署
#    lightbridge health                 # 系统健康检查
#    lightbridge about                  # 关于 LightBridge
#    lightbridge versions               # 查看可用版本
#    lightbridge uninstall              # 卸载
#    lightbridge help [COMMAND]         # 查看帮助
#
#  快速体验:
#    curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/lightbridge-assistant.sh | sudo bash
# ============================================================================

# 不使用 set -e，改为手动捕获错误并给出友好提示
set -uo pipefail

# ── Bash version gate ──────────────────────────────────────────────────────
if [ -z "${BASH_VERSION:-}" ] || [ "${BASH_VERSION%%.*}" -lt 4 ]; then
  echo "Error: Bash 4.0+ required. Current: ${BASH_VERSION:-unknown}" >&2
  exit 1
fi

# ── 全局错误处理 ──────────────────────────────────────────────────────────
# 任何未捕获的错误都会走到这里，给出友好的中文提示
_on_error() {
  local exit_code=$?
  local line_no="${1:-unknown}"
  # 不在 cleanup 阶段报错
  [ "$exit_code" -eq 0 ] && return 0
  [ "$line_no" = "EXIT" ] && return 0

  echo ""
  echo -e "  ${C_RED}${C_BOLD}╔══════════════════════════════════════════╗${C_RESET}"
  echo -e "  ${C_RED}${C_BOLD}║          ❌  运行出错  ❌               ║${C_RESET}"
  echo -e "  ${C_RED}${C_BOLD}╚══════════════════════════════════════════╝${C_RESET}"
  echo ""
  echo -e "  ${C_BOLD}错误码:${C_RESET}  ${C_RED}${exit_code}${C_RESET}"
  echo -e "  ${C_BOLD}位置:${C_RESET}    第 ${C_RED}${line_no}${C_RESET} 行"
  echo ""
  echo -e "  ${C_YELLOW}可能的原因和解决方法:${C_RESET}"
  echo ""
  if [ "$exit_code" -eq 1 ]; then
    echo -e "    ${C_DIM}• 命令执行失败 — 请检查上方的错误输出${C_RESET}"
  elif [ "$exit_code" -eq 126 ]; then
    echo -e "    ${C_DIM}• 权限不足 — 请使用 sudo 运行${C_RESET}"
    echo -e "    ${C_DIM}  sudo $0${C_RESET}"
  elif [ "$exit_code" -eq 127 ]; then
    echo -e "    ${C_DIM}• 命令不存在 — 请安装缺失的依赖${C_RESET}"
    echo -e "    ${C_DIM}  运行 $0 health 查看依赖状态${C_RESET}"
  elif [ "$exit_code" -eq 130 ]; then
    echo -e "    ${C_DIM}• 用户中断 (Ctrl+C) — 已安全退出${C_RESET}"
    return 0
  elif [ "$exit_code" -eq 137 ]; then
    echo -e "    ${C_DIM}• 进程被系统终止 — 可能内存不足${C_RESET}"
    echo -e "    ${C_DIM}  运行 $0 health 查看内存使用${C_RESET}"
  elif [ "$exit_code" -eq 141 ]; then
    echo -e "    ${C_DIM}• 管道断裂 — 通常是正常的输出截断${C_RESET}"
    return 0
  else
    echo -e "    ${C_DIM}• 非预期错误 (退出码 ${exit_code})${C_RESET}"
  fi
  echo ""
  echo -e "  ${C_BOLD}排查建议:${C_RESET}"
  echo -e "    1. 检查网络连接: ${C_CYAN}curl -s https://github.com${C_RESET}"
  echo -e "    2. 检查权限: ${C_CYAN}sudo -l${C_RESET}"
  echo -e "    3. 查看系统健康: ${C_CYAN}$0 health${C_RESET}"
  echo -e "    4. 查看详细日志: ${C_CYAN}$0 health 2>&1 | tee /tmp/lb-debug.log${C_RESET}"
  echo ""
  return "$exit_code"
}
trap '_on_error $LINENO' ERR
trap 'rm -rf "${TEMP_DIR:-}" 2>/dev/null' EXIT

# ── Constants ──────────────────────────────────────────────────────────────
GITHUB_REPO="WilliamWang1721/LightBridge"
GITHUB_RAW="https://raw.githubusercontent.com/${GITHUB_REPO}/main/deploy"
MODULE_RELEASE_TAG="module-anthropic-oauth-provider-v0.1.0"
MODULE_REGISTRY_URL="https://github.com/${GITHUB_REPO}/releases/download/${MODULE_RELEASE_TAG}/registry.json"
MODULE_PUBLIC_KEY_URL="https://github.com/${GITHUB_REPO}/releases/download/${MODULE_RELEASE_TAG}/ed25519.pub"
INSTALL_DIR="/opt/LightBridge"
SERVICE_NAME="LightBridge"
SERVICE_USER="LightBridge"
CONFIG_DIR="/etc/LightBridge"
LEGACY_INSTALL_DIR="/opt/sub2api"
LEGACY_SERVICE_NAME="sub2api"
LEGACY_SERVICE_USER="sub2api"
LEGACY_CONFIG_DIR="/etc/sub2api"
MIGRATION_BACKUP_ROOT="/opt/LightBridge-migration-backups"
DOCKER_RAW_URL="https://raw.githubusercontent.com/${GITHUB_REPO}/main/deploy"
LATEST_VERSION=""
LANG_CHOICE="zh"
SERVER_HOST="0.0.0.0"
SERVER_PORT="8080"
PUBLIC_IP=""
TEMP_DIR=""

# ── Color palette (256-color where possible, fallback to basic) ────────────
if tput colors &>/dev/null && [ "$(tput colors)" -ge 256 ]; then
  C_RESET=$'\033[0m'
  C_BOLD=$'\033[1m'
  C_DIM=$'\033[2m'
  C_ITALIC=$'\033[3m'
  C_UNDERLINE=$'\033[4m'

  C_BLACK=$'\033[30m'
  C_RED=$'\033[31m'
  C_GREEN=$'\033[32m'
  C_YELLOW=$'\033[33m'
  C_BLUE=$'\033[34m'
  C_MAGENTA=$'\033[35m'
  C_CYAN=$'\033[36m'
  C_WHITE=$'\033[37m'

  C_BRIGHT_RED=$'\033[91m'
  C_BRIGHT_GREEN=$'\033[92m'
  C_BRIGHT_YELLOW=$'\033[93m'
  C_BRIGHT_BLUE=$'\033[94m'
  C_BRIGHT_MAGENTA=$'\033[95m'
  C_BRIGHT_CYAN=$'\033[96m'
  C_BRIGHT_WHITE=$'\033[97m'

  C_BG_BLUE=$'\033[44m'
  C_BG_GREEN=$'\033[42m'
  C_BG_RED=$'\033[41m'
  C_BG_YELLOW=$'\033[43m'
  C_BG_MAGENTA=$'\033[45m'
  C_BG_CYAN=$'\033[46m'

  C_GOLD=$'\033[38;5;220m'
  C_ORANGE=$'\033[38;5;208m'
  C_PINK=$'\033[38;5;213m'
  C_LAVENDER=$'\033[38;5;141m'
  C_MINT=$'\033[38;5;121m'
  C_SKY=$'\033[38;5;117m'
  C_PEACH=$'\033[38;5;217m'
else
  C_RESET=$'\033[0m'
  C_BOLD=$'\033[1m'
  C_DIM=$'\033[2m'
  C_ITALIC=$'\033[3m'
  C_UNDERLINE=$'\033[4m'
  C_RED=$'\033[0;31m'
  C_GREEN=$'\033[0;32m'
  C_YELLOW=$'\033[1;33m'
  C_BLUE=$'\033[0;34m'
  C_MAGENTA=$'\033[0;35m'
  C_CYAN=$'\033[0;36m'
  C_WHITE=$'\033[1;37m'
  C_BRIGHT_RED=$'\033[0;31m'
  C_BRIGHT_GREEN=$'\033[0;32m'
  C_BRIGHT_YELLOW=$'\033[1;33m'
  C_BRIGHT_BLUE=$'\033[0;34m'
  C_BRIGHT_CYAN=$'\033[0;36m'
  C_GOLD=$'\033[1;33m'
  C_ORANGE=$'\033[0;33m'
  C_PINK=$'\033[0;35m'
  C_LAVENDER=$'\033[0;35m'
  C_MINT=$'\033[0;32m'
  C_SKY=$'\033[0;36m'
  C_PEACH=$'\033[0;33m'
  C_BLACK=""
  C_DIM=""
  C_ITALIC=""
  C_UNDERLINE=""
  C_BRIGHT_WHITE=""
  C_BG_BLUE=""
  C_BG_GREEN=""
  C_BG_RED=""
  C_BG_YELLOW=""
  C_BG_MAGENTA=""
  C_BG_CYAN=""
fi

# ── I18n strings ───────────────────────────────────────────────────────────
declare -A MSG=(
  # Language
  ["lang_title"]="选择语言 / Select Language"
  ["lang_zh"]="中文 (Chinese)"
  ["lang_en"]="English"
  ["lang_prompt"]="请选择（默认: 1）"

  # Banner & Welcome
  ["banner_welcome"]="欢迎使用 LightBridge 安装助理"
  ["banner_subtitle"]="AI API 网关平台 · 一键部署"
  ["banner_desc1"]="LightBridge 可以帮您轻松搭建一个 AI 接口聚合网关，"
  ["banner_desc2"]="将 Anthropic、OpenAI、Gemini 等多家 AI 服务统一管理，"
  ["banner_desc3"]="一个入口即可调用所有模型，支持负载均衡与用量计费。"
  ["banner_hint"]="请选择下方对应数字开始操作，全程中文引导，小白也能轻松上手！"
  ["menu_tip"]="💡 提示：如果您是第一次使用，推荐选择 1（全新安装）"

  # Step names
  ["step_name_platform"]="检测平台"
  ["step_name_version"]="获取版本"
  ["step_name_download"]="下载安装"
  ["step_name_config"]="配置参数"
  ["step_name_install"]="安装服务"
  ["step_name_start"]="启动运行"
  ["step_name_done"]="完成"

  # Context-aware menu
  ["menu_lb_detected"]="检测到已安装 LightBridge"
  ["menu_lb_not_detected"]="未检测到 LightBridge 安装"
  ["menu_install_tip"]="💡 一行命令即可完成安装，全程自动引导"
  ["menu_docker_tip"]="💡 无需安装任何依赖，推荐新手使用"
  ["menu_upgrade_tip"]="💡 当前版本"
  ["menu_status_running"]="运行中"
  ["menu_status_stopped"]="已停止"
  ["menu_lb_version_label"]="已安装版本"

  # Common
  ["info"]="信息"
  ["ok"]="成功"
  ["warn"]="警告"
  ["err"]="错误"
  ["press_enter"]="按回车键继续..."

  # About
  ["about_title"]="关于 LightBridge"
  ["about_desc1"]="LightBridge 是一个自托管的多提供商 AI API 网关。"
  ["about_desc2"]="它位于您的应用与上游 AI 服务商（Anthropic、OpenAI、Gemini）之间。"
  ["about_desc3"]="只需注册一次服务商账户，LightBridge 即暴露统一的兼容接口。"
  ["about_tagline"]="一个网关，所有提供商，零厂商锁定。"

  ["feat_multi"]="多提供商网关"
  ["feat_multi_desc"]="单一主机统一提供 Anthropic / OpenAI / Gemini 兼容 API"
  ["feat_pool"]="账户池与高可用"
  ["feat_pool_desc"]="负载均衡、故障转移、健康感知的账户选择"
  ["feat_auth"]="灵活的身份认证"
  ["feat_auth_desc"]="API Key、OAuth、邮箱、社交登录（Google/GitHub/微信/钉钉）"
  ["feat_billing"]="计费与多租户"
  ["feat_billing_desc"]="按用户配额、Token 用量追踪、Stripe/Airwallex 支付集成"
  ["feat_privacy"]="隐私与安全"
  ["feat_privacy_desc"]="内置隐私过滤器、内容审核、TLS 指纹模拟"
  ["feat_console"]="管理控制台"
  ["feat_console_desc"]="拖拽式仪表盘、渐进式功能控制、实时监控"

  # Main menu
  ["menu_title"]="主菜单"
  ["menu_1"]="全新安装（二进制 + systemd）"
  ["menu_1_desc"]="在服务器上直接安装 LightBridge 并配置 systemd 服务"
  ["menu_2"]="Docker 部署"
  ["menu_2_desc"]="使用 Docker Compose 快速部署（推荐新手使用）"
  ["menu_3"]="从 Sub2API 迁移"
  ["menu_3_desc"]="将现有的 Sub2API 部署迁移至 LightBridge"
  ["menu_4"]="升级 LightBridge"
  ["menu_4_desc"]="将现有安装升级到最新版本"
  ["menu_5"]="系统健康检查"
  ["menu_5_desc"]="检测系统环境与依赖项是否满足要求"
  ["menu_6"]="关于 LightBridge"
  ["menu_6_desc"]="了解功能特性、技术架构与生态系统"
  ["menu_7"]="卸载 LightBridge"
  ["menu_7_desc"]="从系统中移除 LightBridge"
  ["menu_0"]="退出"
  ["menu_prompt"]="请选择操作"

  # Install steps
  ["step_detected"]="检测到平台"
  ["step_deps_check"]="正在检查依赖..."
  ["step_deps_ok"]="所有依赖已满足"
  ["step_deps_missing"]="缺少依赖"
  ["step_deps_install"]="请先安装以下依赖"
  ["step_fetch_ver"]="正在获取最新版本..."
  ["step_latest_ver"]="最新版本"
  ["step_download"]="正在下载 LightBridge"
  ["step_extract"]="正在解压..."
  ["step_checksum_ok"]="校验通过"
  ["step_checksum_fail"]="校验失败"
  ["step_checksum_skip"]="未找到校验文件，跳过校验"
  ["step_binary_ok"]="二进制文件已安装"
  ["step_user_create"]="正在创建系统用户"
  ["step_user_exists"]="用户已存在"
  ["step_dirs"]="正在配置目录..."
  ["step_dirs_ok"]="目录配置完成"
  ["step_service"]="正在安装 systemd 服务..."
  ["step_service_ok"]="服务安装完成"
  ["step_start"]="正在启动服务..."
  ["step_start_ok"]="服务已启动"
  ["step_start_fail"]="服务启动失败"
  ["step_enable"]="正在设置开机自启..."
  ["step_enable_ok"]="开机自启已启用"
  ["step_ip"]="正在检测公网 IP..."
  ["step_ip_ok"]="公网 IP"
  ["step_ip_fail"]="无法获取公网 IP"
  ["step_ready"]="设置向导已就绪"

  # Server config
  ["srv_title"]="服务器配置"
  ["srv_desc"]="配置 LightBridge 监听地址和端口"
  ["srv_host"]="监听地址"
  ["srv_host_hint"]="0.0.0.0 = 所有网卡 | 127.0.0.1 = 仅本地"
  ["srv_port"]="监听端口"
  ["srv_port_hint"]="建议使用 1024-65535 之间的端口"
  ["srv_invalid_port"]="无效端口号"
  ["srv_summary"]="服务器配置"

  # Complete
  ["done_title"]="安装完成！"
  ["done_dir"]="安装目录"
  ["done_url"]="访问地址"
  ["done_wizard"]="设置向导将引导您完成以下配置："
  ["done_wizard_db"]="数据库配置（PostgreSQL）"
  ["done_wizard_redis"]="Redis 配置"
  ["done_wizard_admin"]="管理员账号创建"
  ["done_cmds"]="常用命令"
  ["done_status"]="查看状态"
  ["done_logs"]="查看日志"
  ["done_restart"]="重启服务"
  ["done_stop"]="停止服务"

  # Migration
  ["mig_title"]="Sub2API 迁移"
  ["mig_detect"]="正在检测 Sub2API 安装..."
  ["mig_found"]="检测到 Sub2API"
  ["mig_not_found"]="未检测到 Sub2API 安装"
  ["mig_lb_exists"]="LightBridge 已安装"
  ["mig_backup"]="正在创建备份"
  ["mig_copy"]="正在迁移配置与数据..."
  ["mig_disable"]="正在禁用旧 Sub2API 服务..."
  ["mig_install_lb"]="正在安装 LightBridge 服务..."
  ["mig_complete"]="迁移完成！"
  ["mig_backup_dir"]="备份目录"
  ["mig_quick"]="快速迁移（仅配置和文件）"
  ["mig_quick_desc"]="将 Sub2API 的配置和运行文件复制到 LightBridge"
  ["mig_full"]="完整数据迁移（推荐）"
  ["mig_full_desc"]="迁移账户、提供商、数据库记录，并安装 OpenAI 模块"
  ["mig_choice_prompt"]="请选择迁移方式"

  # Upgrade
  ["upg_title"]="升级 LightBridge"
  ["upg_current"]="当前版本"
  ["upg_latest"]="最新版本"
  ["upg_same"]="已是最新版本"
  ["upg_stopping"]="正在停止服务..."
  ["upg_backing"]="正在备份当前二进制文件..."
  ["upg_done"]="升级完成！"
  ["upg_ver_prompt"]="请输入目标版本（留空则升级到最新）"

  # Health check
  ["hc_title"]="系统健康检查"
  ["hc_os"]="操作系统"
  ["hc_arch"]="系统架构"
  ["hc_ram"]="内存"
  ["hc_disk"]="磁盘空间"
  ["hc_root"]="Root 权限"
  ["hc_systemd"]="systemd"
  ["hc_curl"]="curl"
  ["hc_tar"]="tar"
  ["hc_go"]="Go（可选）"
  ["hc_pg"]="PostgreSQL"
  ["hc_redis"]="Redis"
  ["hc_lb_binary"]="LightBridge 二进制"
  ["hc_lb_service"]="LightBridge 服务"
  ["hc_lb_running"]="服务状态"
  ["hc_lb_version"]="已安装版本"
  ["hc_legacy"]="Sub2API 旧版"
  ["hc_docker"]="Docker"
  ["hc_pass"]="通过"
  ["hc_fail"]="未通过"
  ["hc_warn"]="警告"
  ["hc_na"]="未检测"
  ["hc_section_sys"]="系统信息"
  ["hc_section_prereq"]="前置条件"
  ["hc_section_svc"]="服务检测"
  ["hc_section_lb"]="LightBridge"
  ["hc_summary_pass"]="%d 项通过"
  ["hc_summary_warn"]="%d 项警告"
  ["hc_summary_fail"]="%d 项未通过"
  ["hc_summary_total"]="（共 %d 项检查）"

  # Uninstall
  ["uni_title"]="卸载 LightBridge"
  ["uni_confirm"]="这将从系统中移除 LightBridge"
  ["uni_sure"]="确定要继续吗？(y/N)"
  ["uni_cancelled"]="卸载已取消"
  ["uni_removing"]="正在移除文件..."
  ["uni_done"]="LightBridge 已卸载"
  ["uni_purge"]="是否同时删除配置和数据目录？"

  # Version listing
  ["ver_available"]="Available versions"

  # Rollback
  ["rb_title"]="Rollback / Install Specific Version"
  ["rb_prompt"]="Enter version to install"
  ["rb_usage"]="Usage: lightbridge-assistant.sh rollback <version>"
)

# ── Helper functions ────────────────────────────────────────────────────────
msg() { echo -e "${MSG[$1]:-$1}"; }
msg_val() { echo -e "${MSG[$1]:-$1} $2"; }

TERM_WIDTH=$(tput cols 2>/dev/null || echo 80)
CENTER_PAD() {
  local text="$1"
  local w="${#text}"
  local pad=$(( (TERM_WIDTH - w) / 2 ))
  [ "$pad" -gt 0 ] && printf '%*s' "$pad" "" || true
}

# ── UI Drawing primitives ──────────────────────────────────────────────────

# Draw a horizontal rule
hr() {
  local color="${1:-$C_DIM}"
  local char="${2:-─}"
  printf '%s' "$color"
  printf '%*s' "$TERM_WIDTH" '' | tr ' ' "$char"
  printf '%s\n' "$C_RESET"
}

# Draw a double horizontal rule
hr_double() {
  local color="${1:-$C_DIM}"
  printf '%s' "$color"
  printf '%*s' "$TERM_WIDTH" '' | tr ' ' '═'
  printf '%s\n' "$C_RESET"
}

# Print centered text
center() {
  local color="${1:-}"
  local text="$2"
  local stripped
  stripped=$(echo -e "$text" | sed 's/\x1b\[[0-9;]*m//g')
  local w=${#stripped}
  local pad=$(( (TERM_WIDTH - w) / 2 ))
  [ "$pad" -gt 0 ] && printf '%*s' "$pad" ""
  echo -e "${color}${text}${C_RESET}"
}

# Print a box with content
print_box() {
  local title_color="${1:-$C_CYAN}"
  local border_color="${2:-$C_BLUE}"
  local title="$3"
  local content_width=$((TERM_WIDTH - 8))

  echo ""
  echo -e "${border_color}╔$(printf '═%.0s' $(seq 1 $content_width))╗${C_RESET}"
  if [ -n "$title" ]; then
    local tw=${#title}
    local lp=$(( (content_width - tw) / 2 ))
    local rp=$(( content_width - tw - lp ))
    printf "${border_color}║${C_RESET}%*s${title_color}${C_BOLD}%s${C_RESET}%*s${border_color}║${C_RESET}\n" \
      "$lp" "" "$title" "$rp" ""
    echo -e "${border_color}╠$(printf '═%.0s' $(seq 1 $content_width))╣${C_RESET}"
  fi

  local line
  while IFS= read -r line; do
    [ -z "$line" ] && line=" "
    local stripped
    stripped=$(echo -e "$line" | sed 's/\x1b\[[0-9;]*m//g')
    local lw=${#stripped}
    if [ "$lw" -le "$content_width" ]; then
      local rp=$(( content_width - lw ))
      printf "${border_color}║${C_RESET}%s%*s${border_color}║${C_RESET}\n" \
        "$line" "$rp" ""
    else
      printf "${border_color}║${C_RESET}%s${border_color}║${C_RESET}\n" "${line:0:$content_width}"
    fi
  done

  echo -e "${border_color}╚$(printf '═%.0s' $(seq 1 $content_width))╝${C_RESET}"
}

# Print content inside a box (from variable, line by line)
print_box_content() {
  local border_color="${1:-$C_BLUE}"
  local content_width=$((TERM_WIDTH - 8))
  local line
  while IFS= read -r line; do
    [ -z "$line" ] && line=" "
    local stripped
    stripped=$(echo -e "$line" | sed 's/\x1b\[[0-9;]*m//g')
    local lw=${#stripped}
    if [ "$lw" -le "$content_width" ]; then
      local rp=$(( content_width - lw ))
      printf "${border_color}║${C_RESET}%s%*s${border_color}║${C_RESET}\n" \
        "$line" "$rp" ""
    else
      printf "${border_color}║${C_RESET}%s${border_color}║${C_RESET}\n" "${line:0:$content_width}"
    fi
  done
}

# Close a box
close_box() {
  local border_color="${1:-$C_BLUE}"
  local content_width=$((TERM_WIDTH - 8))
  echo -e "${border_color}╚$(printf '═%.0s' $(seq 1 $content_width))╝${C_RESET}"
}

# Draw a step indicator (e.g., ●───○───○───○)
draw_steps() {
  local current="$1"
  shift
  local steps=("$@")
  local total=${#steps[@]}
  local width=$((TERM_WIDTH - 4))
  local step_width=$(( width / total ))

  echo ""
  local i=0
  for step in "${steps[@]}"; do
    i=$((i + 1))
    if [ "$i" -lt "$current" ]; then
      printf "${C_GREEN}●${C_RESET}"
      printf '%s' "$(printf '─%.0s' $(seq 1 $((step_width - 1))))"
    elif [ "$i" -eq "$current" ]; then
      printf "${C_CYAN}${C_BOLD}◉${C_RESET}"
      printf '%s' "$(printf '─%.0s' $(seq 1 $((step_width - 1))))"
    else
      printf "${C_DIM}○${C_RESET}"
      printf '%s' "$(printf '─%.0s' $(seq 1 $((step_width - 1))))"
    fi
  done
  echo ""

  i=0
  for step in "${steps[@]}"; do
    i=$((i + 1))
    if [ "$i" -eq "$current" ]; then
      printf "${C_CYAN}${C_BOLD}%-*s${C_RESET}" "$step_width" "$step"
    elif [ "$i" -lt "$current" ]; then
      printf "${C_GREEN}%-*s${C_RESET}" "$step_width" "$step"
    else
      printf "${C_DIM}%-*s${C_RESET}" "$step_width" "$step"
    fi
  done
  echo ""
  echo ""
}

# Animated spinner
spinner() {
  local pid=$1
  local msg="${2:-Working...}"
  local frames=('⠋' '⠙' '⠹' '⠸' '⠼' '⠴' '⠦' '⠧' '⠇' '⠏')
  local i=0
  tput civis 2>/dev/null || true
  while kill -0 "$pid" 2>/dev/null; do
    printf "\r  ${C_CYAN}%s${C_RESET} %s" "${frames[$((i % ${#frames[@]}))]}" "$msg"
    i=$((i + 1))
    sleep 0.1
  done
  tput cnorm 2>/dev/null || true
  printf "\r"
}

# Progress bar
progress_bar() {
  local current=$1
  local total=$2
  local width=${3:-40}
  local pct=$(( current * 100 / total ))
  local filled=$(( current * width / total ))
  local empty=$(( width - filled ))

  printf "\r  ${C_CYAN}[" 
  printf '%0.s█' $(seq 1 "$filled" 2>/dev/null) || true
  printf '%0.s░' $(seq 1 "$empty" 2>/dev/null) || true
  printf "]${C_RESET} %3d%%" "$pct"
}

# Status badge
badge_pass() { echo -e "${C_GREEN}${C_BOLD}  ✓ $(L hc_pass)${C_RESET}"; }
badge_fail() { echo -e "${C_RED}${C_BOLD}  ✗ $(L hc_fail)${C_RESET}"; }
badge_warn() { echo -e "${C_YELLOW}${C_BOLD}  ⚠ $(L hc_warn)${C_RESET}"; }
badge_na()   { echo -e "${C_DIM}  — $(L hc_na)${C_RESET}"; }

# Print a feature card
feature_card() {
  local icon="$1"
  local title="$2"
  local desc="$3"
  local icon_color="${4:-$C_CYAN}"

  echo -e "  ${icon_color}${C_BOLD}${icon}${C_RESET}  ${C_BOLD}${title}${C_RESET}"
  echo -e "     ${C_DIM}${desc}${C_RESET}"
  echo ""
}

# ── Utility functions ──────────────────────────────────────────────────────
cmd_exists() { command -v "$1" &>/dev/null; }
is_interactive() {
  # Check if stdin is a terminal, or if /dev/tty is available
  if [ -t 0 ] 2>/dev/null; then return 0; fi
  [ -e /dev/tty ] && [ -r /dev/tty ] && [ -w /dev/tty ] && { : < /dev/tty; } 2>/dev/null
}
# Read from /dev/tty if available, otherwise from stdin
# Usage: read_input variable_name (for reading into a variable)
#        read_input (to consume/discard input)
read_input() {
  local _dev="/dev/tty"
  [ -e "$_dev" ] && [ -r "$_dev" ] || _dev="/dev/stdin"

  if [ $# -eq 0 ]; then
    IFS= read -r _discard < "$_dev" 2>/dev/null || true
  else
    read -r "$@" < "$_dev" 2>/dev/null || true
  fi
}
check_root() {
  if [ "$(id -u)" -ne 0 ]; then
    echo ""
    echo -e "  ${C_RED}${C_BOLD}╔══════════════════════════════════════════╗${C_RESET}"
    echo -e "  ${C_RED}${C_BOLD}║      ✗  需要 root 权限  ✗              ║${C_RESET}"
    echo -e "  ${C_RED}${C_BOLD}╚══════════════════════════════════════════╝${C_RESET}"
    echo ""
    echo -e "  ${C_BOLD}当前用户:${C_RESET}  $(whoami) (uid=$(id -u))"
    echo ""
    echo -e "  ${C_YELLOW}${C_BOLD}🔧 解决方法:${C_RESET}"
    echo -e "    1. 使用 sudo 运行: ${C_CYAN}sudo $0 install${C_RESET}"
    echo -e "    2. 切换到 root:     ${C_CYAN}sudo -i${C_RESET}"
    echo -e "    3. 检查 sudo 权限:  ${C_CYAN}sudo -l${C_RESET}"
    echo ""
    exit 1
  fi
}

service_exists() {
  local name="$1"
  cmd_exists systemctl || return 1
  local fragment
  fragment=$(systemctl show -p FragmentPath --value "$name" 2>/dev/null | head -1 || true)
  [ -n "$fragment" ] && [ "$fragment" != "n/a" ]
}

get_service_fragment() {
  systemctl show -p FragmentPath --value "$1" 2>/dev/null | head -1 || true
}

get_service_exec() {
  systemctl show -p ExecStart --value "$1" 2>/dev/null \
    | tr ' ' '\n' | sed -n 's/^path=//p' | head -1 || true
}

get_service_user() {
  systemctl show -p User --value "$1" 2>/dev/null | head -1 || true
}

get_service_env() {
  local svc="$1" key="$2"
  systemctl show -p Environment --value "$svc" 2>/dev/null \
    | tr ' ' '\n' | sed -n "s/^${key}=//p" | head -1 || true
}

find_first_file() {
  local f; for f in "$@"; do [ -f "$f" ] && { echo "$f"; return 0; }; done; return 1
}

# ── Language selection ──────────────────────────────────────────────────────
select_language() {
  if ! is_interactive; then
    LANG_CHOICE="zh"
    return
  fi

  clear 2>/dev/null || true
  echo ""
  hr "$C_CYAN"
  center "$C_CYAN" "  🌐  ${MSG[lang_title]}  🌐"
  hr "$C_CYAN"
  echo ""
  echo -e "    ${C_BOLD}1)${C_RESET}  ${C_BRIGHT_GREEN}中文 (Chinese)${C_RESET}  ${C_DIM}← 默认${C_RESET}"
  echo -e "    ${C_BOLD}2)${C_RESET}  ${C_BRIGHT_CYAN}English${C_RESET}"
  echo ""
  hr "$C_DIM"
  printf "    ${C_DIM}❯${C_RESET} "
  lang_input=""
  read_input lang_input
  case "$lang_input" in
    2|en|EN|english|English) LANG_CHOICE="en" ;;
    *) LANG_CHOICE="zh" ;;
  esac
  echo ""
}

# ── Get localized message ──────────────────────────────────────────────────
# Simple: MSG array stores default (zh) strings; for English we override inline
# A more complete approach uses separate MSG_EN array
declare -A MSG_EN=(
  ["lang_title"]="Select Language"
  ["lang_prompt"]="Please select (default: 1)"
  ["info"]="INFO"
  ["ok"]="OK"
  ["warn"]="WARN"
  ["err"]="ERROR"
  ["press_enter"]="Press Enter to continue..."
  ["run_as_root"]="请使用 root 权限运行（使用 sudo）"
  ["banner_welcome"]="Welcome to LightBridge Installation Assistant"
  ["banner_subtitle"]="AI API Gateway Platform · One-Click Deploy"
  ["banner_desc1"]="LightBridge helps you build an AI API aggregation gateway,"
  ["banner_desc2"]="uniting Anthropic, OpenAI, Gemini and more behind one endpoint,"
  ["banner_desc3"]="with load balancing, failover, and usage billing built in."
  ["banner_hint"]="Select a number below to get started — guided step by step!"
  ["menu_tip"]="💡 Tip: New to LightBridge? Choose 1 (Fresh Install) to begin"

  # Step names
  ["step_name_platform"]="Platform"
  ["step_name_version"]="Version"
  ["step_name_download"]="Download"
  ["step_name_config"]="Configure"
  ["step_name_install"]="Install"
  ["step_name_start"]="Start"
  ["step_name_done"]="Done"
  ["menu_lb_detected"]="LightBridge is already installed"
  ["menu_lb_not_detected"]="LightBridge not detected"
  ["menu_install_tip"]="💡 One command to install — fully guided"
  ["menu_docker_tip"]="💡 No dependencies needed — recommended for beginners"
  ["menu_upgrade_tip"]="💡 Current version"
  ["menu_status_running"]="running"
  ["menu_status_stopped"]="stopped"
  ["menu_lb_version_label"]="Installed version"
  ["about_title"]="About LightBridge"
  ["menu_title"]="Main Menu"
  ["menu_1"]="Fresh Install (Binary + systemd)"
  ["menu_1_desc"]="Install LightBridge directly on your server"
  ["menu_2"]="Docker Deployment"
  ["menu_2_desc"]="Quick setup with Docker Compose"
  ["menu_3"]="Migrate from Sub2API"
  ["menu_3_desc"]="Convert an existing Sub2API deployment"
  ["menu_4"]="Upgrade LightBridge"
  ["menu_4_desc"]="Upgrade to the latest version"
  ["menu_5"]="System Health Check"
  ["menu_5_desc"]="Verify prerequisites and compatibility"
  ["menu_6"]="About LightBridge"
  ["menu_6_desc"]="Features, architecture, ecosystem"
  ["menu_7"]="Uninstall LightBridge"
  ["menu_7_desc"]="Remove LightBridge from your system"
  ["menu_0"]="Exit"
  ["menu_prompt"]="Select an option"
  ["srv_title"]="Server Configuration"
  ["srv_desc"]="Configure listen address and port"
  ["srv_host"]="Listen address"
  ["srv_host_hint"]="0.0.0.0 = all interfaces | 127.0.0.1 = local only"
  ["srv_port"]="Listen port"
  ["srv_port_hint"]="Recommended: 1024-65535"
  ["srv_invalid_port"]="Invalid port"
  ["srv_summary"]="Server config"
  ["done_title"]="Installation Complete!"
  ["done_dir"]="Install directory"
  ["done_url"]="Access URL"
  ["done_wizard"]="The Setup Wizard will guide you through:"
  ["done_wizard_db"]="Database configuration (PostgreSQL)"
  ["done_wizard_redis"]="Redis configuration"
  ["done_wizard_admin"]="Admin account creation"
  ["done_cmds"]="Quick Commands"
  ["done_status"]="Check status"
  ["done_logs"]="View logs"
  ["done_restart"]="Restart"
  ["done_stop"]="Stop"
  ["step_fetch_ver"]="Fetching latest version..."
  ["step_latest_ver"]="Latest version"
  ["step_download"]="Downloading LightBridge"
  ["step_extract"]="Extracting archive..."
  ["step_checksum_ok"]="Checksum verified"
  ["step_checksum_fail"]="Checksum mismatch"
  ["step_checksum_skip"]="Checksum file not found"
  ["step_binary_ok"]="Binary installed"
  ["step_user_create"]="Creating system user"
  ["step_user_exists"]="User already exists"
  ["step_dirs"]="Setting up directories..."
  ["step_dirs_ok"]="Directories configured"
  ["step_service"]="Installing systemd service..."
  ["step_service_ok"]="Service installed"
  ["step_start"]="Starting service..."
  ["step_start_ok"]="Service started"
  ["step_start_fail"]="Service failed to start"
  ["step_enable"]="Enabling auto-start..."
  ["step_enable_ok"]="Auto-start enabled"
  ["step_ip"]="Detecting public IP..."
  ["step_ip_ok"]="Public IP"
  ["step_ip_fail"]="Could not detect public IP"
  ["step_ready"]="Setup wizard ready"
  ["mig_title"]="Sub2API Migration"
  ["mig_detect"]="Detecting Sub2API installation..."
  ["mig_found"]="Sub2API detected"
  ["mig_not_found"]="No Sub2API installation found"
  ["mig_lb_exists"]="LightBridge already installed"
  ["mig_backup"]="Creating backup"
  ["mig_copy"]="Migrating configuration and data..."
  ["mig_disable"]="Disabling legacy Sub2API service..."
  ["mig_install_lb"]="Installing LightBridge service..."
  ["mig_complete"]="Migration complete!"
  ["mig_backup_dir"]="Backup directory"
  ["mig_quick"]="Quick Migration (config + files only)"
  ["mig_quick_desc"]="Copy config/runtime files to LightBridge layout"
  ["mig_full"]="Full Data Migration (recommended)"
  ["mig_full_desc"]="Migrate accounts, providers, database, OpenAI module"
  ["mig_choice_prompt"]="Choose migration type"
  ["upg_title"]="Upgrade LightBridge"
  ["upg_current"]="Current version"
  ["upg_latest"]="Latest version"
  ["upg_same"]="Already on the latest version"
  ["upg_stopping"]="Stopping service..."
  ["upg_backing"]="Backing up current binary..."
  ["upg_done"]="Upgrade complete!"
  ["upg_ver_prompt"]="Target version (empty for latest)"
  ["hc_title"]="System Health Check"
  ["hc_os"]="Operating System"
  ["hc_arch"]="Architecture"
  ["hc_ram"]="Memory"
  ["hc_disk"]="Disk Space"
  ["hc_root"]="Root Access"
  ["hc_systemd"]="systemd"
  ["hc_curl"]="curl"
  ["hc_tar"]="tar"
  ["hc_go"]="Go (optional)"
  ["hc_pg"]="PostgreSQL"
  ["hc_redis"]="Redis"
  ["hc_lb_binary"]="LightBridge Binary"
  ["hc_lb_service"]="LightBridge Service"
  ["hc_lb_running"]="Service Status"
  ["hc_lb_version"]="Installed Version"
  ["hc_legacy"]="Sub2API Legacy"
  ["hc_docker"]="Docker"
  ["hc_pass"]="PASS"
  ["hc_fail"]="FAIL"
  ["hc_warn"]="WARN"
  ["hc_na"]="N/A"
  ["hc_section_sys"]="System Information"
  ["hc_section_prereq"]="Prerequisites"
  ["hc_section_svc"]="Services"
  ["hc_section_lb"]="LightBridge"
  ["hc_summary_pass"]="%d passed"
  ["hc_summary_warn"]="%d warnings"
  ["hc_summary_fail"]="%d failed"
  ["hc_summary_total"]="(%d checks)"
  ["uni_title"]="Uninstall LightBridge"
  ["uni_confirm"]="This will remove LightBridge from your system"
  ["uni_sure"]="Are you sure? (y/N)"
  ["uni_cancelled"]="Uninstall cancelled"
  ["uni_removing"]="Removing files..."
  ["uni_done"]="LightBridge has been uninstalled"
  ["uni_purge"]="Also remove config and data directory?"
  ["ver_available"]="Available versions"
  ["rb_prompt"]="Enter version to install"
  ["feat_multi"]="Multi-Provider Gateway"
  ["feat_multi_desc"]="Unified Anthropic / OpenAI / Gemini APIs from a single host"
  ["feat_pool"]="Account Pooling & Reliability"
  ["feat_pool_desc"]="Load balancing, failover, and health-aware account selection"
  ["feat_auth"]="Flexible Authentication"
  ["feat_auth_desc"]="API Keys, OAuth, email, social login (Google/GitHub/WeChat)"
  ["feat_billing"]="Billing & Multi-Tenancy"
  ["feat_billing_desc"]="Per-user quotas, token tracking, Stripe/Airwallex payments"
  ["feat_privacy"]="Privacy & Security"
  ["feat_privacy_desc"]="Privacy filter, content moderation, TLS fingerprint simulation"
  ["feat_console"]="Admin Console"
  ["feat_console_desc"]="Drag-and-drop dashboard, progressive feature controls, real-time monitoring"
  ["about_desc1"]="LightBridge is a self-hosted, multi-provider AI API gateway."
  ["about_desc2"]="It sits between your apps and upstream AI providers."
  ["about_desc3"]="Register provider accounts once, get a unified endpoint."
  ["about_tagline"]="One gateway. Every provider. Zero vendor lock-in."
)

L() {
  local key="$1"
  if [ "$LANG_CHOICE" = "en" ] && [ -n "${MSG_EN[$key]:-}" ]; then
    echo "${MSG_EN[$key]}"
  else
    echo "${MSG[$key]:-$key}"
  fi
}

# ── Platform detection ─────────────────────────────────────────────────────
detect_platform() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo -e "${C_RED}Unsupported architecture: $ARCH${C_RESET}"; exit 1 ;;
  esac
  case "$OS" in
    linux|darwin) ;;
    *) echo -e "${C_RED}Unsupported OS: $OS${C_RESET}"; exit 1 ;;
  esac
}

# ── Show about / feature page ──────────────────────────────────────────────
show_about() {
  clear 2>/dev/null || true
  echo ""
  hr "$C_CYAN"
  center "$C_BRIGHT_WHITE" ""
  center "$C_BRIGHT_CYAN" "  ╔══════════════════════════════════════════╗"
  center "$C_BRIGHT_CYAN" "  ║     ⚡  LightBridge  ⚡                 ║"
  center "$C_BRIGHT_CYAN" "  ║     AI API Gateway Platform              ║"
  center "$C_BRIGHT_CYAN" "  ╚══════════════════════════════════════════╝"
  center "$C_GOLD" ""
  center "$C_GOLD" "  One gateway. Every provider. Zero vendor lock-in."
  center "$C_GOLD" ""
  hr "$C_CYAN"
  echo ""

  echo -e "  ${C_DIM}$(L about_desc1)${C_RESET}"
  echo -e "  ${C_DIM}$(L about_desc2)${C_RESET}"
  echo -e "  ${C_DIM}$(L about_desc3)${C_RESET}"
  echo ""

  hr "$C_BLUE"
  center "$C_BRIGHT_BLUE" "  ✦  FEATURES  ✦"
  hr "$C_BLUE"
  echo ""

  feature_card "🔌" "$(L feat_multi)" "$(L feat_multi_desc)" "$C_CYAN"
  feature_card "⚖️"  "$(L feat_pool)" "$(L feat_pool_desc)" "$C_GREEN"
  feature_card "🔐" "$(L feat_auth)" "$(L feat_auth_desc)" "$C_MAGENTA"
  feature_card "💳" "$(L feat_billing)" "$(L feat_billing_desc)" "$C_GOLD"
  feature_card "🛡️"  "$(L feat_privacy)" "$(L feat_privacy_desc)" "$C_RED"
  feature_card "📊" "$(L feat_console)" "$(L feat_console_desc)" "$C_BLUE"

  hr "$C_BLUE"
  center "$C_BRIGHT_BLUE" "  ✦  COMPATIBLE PROTOCOLS  ✦"
  hr "$C_BLUE"
  echo ""
  echo -e "  ${C_CYAN}╔═════════════════╦══════════════════════════════════════════╗${C_RESET}"
  echo -e "  ${C_CYAN}║${C_RESET}  ${C_BOLD}Protocol${C_RESET}       ${C_CYAN}║${C_RESET}  ${C_BOLD}Endpoint${C_RESET}                            ${C_CYAN}║${C_RESET}"
  echo -e "  ${C_CYAN}╠═════════════════╬══════════════════════════════════════════╣${C_RESET}"
  echo -e "  ${C_CYAN}║${C_RESET}  ${C_GREEN}Anthropic${C_RESET}     ${C_CYAN}║${C_RESET}  POST /v1/messages                    ${C_CYAN}║${C_RESET}"
  echo -e "  ${C_CYAN}║${C_RESET}  ${C_GREEN}OpenAI${C_RESET}        ${C_CYAN}║${C_RESET}  POST /v1/chat/completions            ${C_CYAN}║${C_RESET}"
  echo -e "  ${C_CYAN}║${C_RESET}  ${C_GREEN}Gemini${C_RESET}       ${C_CYAN}║${C_RESET}  POST /v1beta/models/{model}:...       ${C_CYAN}║${C_RESET}"
  echo -e "  ${C_CYAN}╚═════════════════╩══════════════════════════════════════════╝${C_RESET}"
  echo ""

  hr "$C_BLUE"
  center "$C_BRIGHT_BLUE" "  ✦  TECH STACK  ✦"
  hr "$C_BLUE"
  echo ""
  echo -e "  ${C_SKY}Backend${C_RESET}    Go 1.26 · Gin · Ent ORM · Wire (DI)"
  echo -e "  ${C_MINT}Frontend${C_RESET}  Vue 3 · Vite · Pinia · Chart.js"
  echo -e "  ${C_PEACH}Data${C_RESET}      PostgreSQL 16 · Redis"
  echo -e "  ${C_LAVENDER}Delivery${C_RESET}  GoReleaser · Docker / GHCR · systemd"
  echo ""

  hr "$C_CYAN"
  echo ""
  if is_interactive; then
    printf "  ${C_DIM}$(L press_enter)${C_RESET} "
    read_input
  fi
}

# ── Show system health check ───────────────────────────────────────────────
show_health_check() {
  clear 2>/dev/null || true
  echo ""
  hr "$C_CYAN"
  center "$C_BRIGHT_CYAN" "  🔍  $(L hc_title)  🔍"
  hr "$C_CYAN"
  echo ""

  local pass_count=0
  local warn_count=0
  local fail_count=0

  check_item() {
    local label="$1"
    local status="$2"
    local detail="$3"
    printf "  ${C_BOLD}%-22s${C_RESET}" "$label"
    case "$status" in
      pass) badge_pass; pass_count=$((pass_count+1)) ;;
      fail) badge_fail; fail_count=$((fail_count+1)) ;;
      warn) badge_warn; warn_count=$((warn_count+1)) ;;
      na)   badge_na ;;
    esac
    if [ -n "$detail" ]; then
      echo -e "    ${C_DIM}${detail}${C_RESET}"
    else
      echo ""
    fi
  }

  # ── Collect device info ───────────────────────────────────────────────
  local hostname_info cpu_model cpu_cores ram_total_gb ram_avail_gb disk_total_gb disk_avail_gb kernel_info uptime_info os_full
  hostname_info=$(hostname 2>/dev/null || echo "unknown")
  kernel_info=$(uname -r 2>/dev/null || echo "unknown")
  os_full=$(uname -srm 2>/dev/null || echo "unknown")
  uptime_info=$(uptime -p 2>/dev/null | sed 's/up //' || uptime 2>/dev/null | awk -F'up ' '{print $2}' | awk -F',' '{print $1}' || echo "unknown")

  # CPU
  cpu_cores=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo "unknown")
  if [ -f /proc/cpuinfo ]; then
    cpu_model=$(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | awk -F: '{print $2}' | sed 's/^ *//' || echo "unknown")
  elif cmd_exists sysctl; then
    cpu_model=$(sysctl -n machdep.cpu.brand_string 2>/dev/null || echo "unknown")
  else
    cpu_model="unknown"
  fi

  # RAM (in GB, with 1 decimal)
  if cmd_exists free; then
    ram_total_gb=$(free -g 2>/dev/null | awk '/^Mem:/{print $2}' || echo "0")
    ram_avail_gb=$(free -g 2>/dev/null | awk '/^Mem:/{print $7}' || echo "0")
    # Fallback to MB if <1GB
    if [ "${ram_total_gb:-0}" -eq 0 ] 2>/dev/null; then
      ram_total_gb=$(free -m 2>/dev/null | awk '/^Mem:/{printf "%.1f", $2/1024}' || echo "0")
      ram_avail_gb=$(free -m 2>/dev/null | awk '/^Mem:/{printf "%.1f", $7/1024}' || echo "0")
    fi
  elif [ -f /proc/meminfo ]; then
    ram_total_gb=$(awk '/MemTotal/{printf "%.1f", $2/1024/1024}' /proc/meminfo 2>/dev/null || echo "0")
    ram_avail_gb=$(awk '/MemAvailable/{printf "%.1f", $2/1024/1024}' /proc/meminfo 2>/dev/null || echo "0")
    [ -z "$ram_avail_gb" ] || [ "$ram_avail_gb" = "0" ] && ram_avail_gb="$ram_total_gb"
  elif cmd_exists sysctl; then
    local mem_bytes
    mem_bytes=$(sysctl -n hw.memsize 2>/dev/null || echo "0")
    ram_total_gb=$(echo "scale=1; $mem_bytes / 1073741824" | bc 2>/dev/null || echo "0")
    ram_avail_gb="$ram_total_gb"
  else
    ram_total_gb="0"
    ram_avail_gb="0"
  fi

  # Disk
  disk_total_gb=$(df -g / 2>/dev/null | awk 'NR==2{print $2}' || echo "0")
  disk_avail_gb=$(df -h / 2>/dev/null | awk 'NR==2{print $4}' || echo "0")

  # ── Section 1: Device Info ───────────────────────────────────────────
  echo -e "  ${C_BRIGHT_BLUE}${C_BOLD}▸ 设备详情${C_RESET}"
  echo ""
  check_item "主机名"         "pass" "$hostname_info"
  check_item "$(L hc_os)"     "pass" "$os_full"
  check_item "内核版本"       "pass" "$kernel_info"
  check_item "$(L hc_arch)"   "pass" "$ARCH"
  check_item "CPU 型号"       "pass" "$cpu_model"
  check_item "CPU 核心数"     "pass" "${cpu_cores} 核"
  check_item "总内存"         "pass" "${ram_total_gb} GB"
  check_item "可用内存"       "pass" "${ram_avail_gb} GB"
  check_item "磁盘总量"       "pass" "${disk_total_gb} GB"
  check_item "磁盘可用"       "pass" "${disk_avail_gb}"
  check_item "系统运行时间"   "pass" "${uptime_info}"

  echo ""
  echo -e "  ${C_BRIGHT_BLUE}${C_BOLD}▸ $(L hc_section_prereq)${C_RESET}"
  echo ""

  # Root
  if [ "$(id -u)" -eq 0 ]; then
    check_item "$(L hc_root)" "pass" "uid=0"
  else
    check_item "$(L hc_root)" "fail" "uid=$(id -u) — 需要 sudo 权限"
  fi

  # systemd
  if cmd_exists systemctl; then
    check_item "$(L hc_systemd)" "pass" "$(systemctl --version 2>/dev/null | head -1 || echo 'installed')"
  else
    check_item "$(L hc_systemd)" "warn" "未安装（Docker 模式无需此依赖）"
  fi

  # curl
  if cmd_exists curl; then
    check_item "$(L hc_curl)" "pass" "$(curl --version 2>/dev/null | head -1 | cut -d' ' -f1-3 || echo 'installed')"
  else
    check_item "$(L hc_curl)" "fail" "安装所必需"
  fi

  # tar
  if cmd_exists tar; then
    check_item "$(L hc_tar)" "pass" "已安装"
  else
    check_item "$(L hc_tar)" "fail" "安装所必需"
  fi

  # Go (optional)
  if cmd_exists go; then
    check_item "$(L hc_go)" "pass" "$(go version 2>/dev/null || echo 'installed')"
  else
    check_item "$(L hc_go)" "na" "仅源码编译时需要"
  fi

  echo ""
  echo -e "  ${C_BRIGHT_BLUE}${C_BOLD}▸ $(L hc_section_svc)${C_RESET}"
  echo ""

  # PostgreSQL
  if cmd_exists psql; then
    local pg_ver
    pg_ver=$(psql --version 2>/dev/null | awk '{print $3}' || echo "installed")
    check_item "$(L hc_pg)" "pass" "$pg_ver"
  elif systemctl is-active --quiet postgresql 2>/dev/null; then
    check_item "$(L hc_pg)" "pass" "运行中"
  else
    check_item "$(L hc_pg)" "warn" "未检测到（二进制安装必需）"
  fi

  # Redis
  if cmd_exists redis-cli; then
    if redis-cli ping 2>/dev/null | grep -q PONG; then
      check_item "$(L hc_redis)" "pass" "运行中"
    else
      check_item "$(L hc_redis)" "warn" "已安装但未响应"
    fi
  elif systemctl is-active --quiet redis 2>/dev/null || systemctl is-active --quiet redis-server 2>/dev/null; then
    check_item "$(L hc_redis)" "pass" "运行中"
  else
    check_item "$(L hc_redis)" "warn" "未检测到（二进制安装必需）"
  fi

  echo ""
  echo -e "  ${C_BRIGHT_BLUE}${C_BOLD}▸ $(L hc_section_lb)${C_RESET}"
  echo ""

  # Binary
  local lb_binary=""
  if [ -f "$INSTALL_DIR/LightBridge" ]; then
    lb_binary="$INSTALL_DIR/LightBridge"
  elif service_exists "$SERVICE_NAME"; then
    lb_binary=$(get_service_exec "$SERVICE_NAME" 2>/dev/null || true)
  fi

  if [ -n "$lb_binary" ] && [ -f "$lb_binary" ]; then
    check_item "$(L hc_lb_binary)" "pass" "$lb_binary"
    local ver
    ver=$("$lb_binary" --version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "unknown")
    check_item "$(L hc_lb_version)" "pass" "$ver"
  else
    check_item "$(L hc_lb_binary)" "na" "未安装"
    check_item "$(L hc_lb_version)" "na" "—"
  fi

  # Service
  if service_exists "$SERVICE_NAME"; then
    check_item "$(L hc_lb_service)" "pass" "已安装"
    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
      check_item "$(L hc_lb_running)" "pass" "运行中"
    else
      check_item "$(L hc_lb_running)" "warn" "已停止"
    fi
  else
    check_item "$(L hc_lb_service)" "na" "未安装"
    check_item "$(L hc_lb_running)" "na" "—"
  fi

  # Legacy Sub2API
  local legacy_found=false
  if service_exists "$LEGACY_SERVICE_NAME"; then
    legacy_found=true
  fi
  for p in "$LEGACY_INSTALL_DIR/sub2api" "$LEGACY_INSTALL_DIR/LightBridge" /usr/local/bin/sub2api; do
    [ -f "$p" ] && legacy_found=true && break
  done
  if $legacy_found; then
    check_item "$(L hc_legacy)" "warn" "检测到旧版 — 建议迁移"
  else
    check_item "$(L hc_legacy)" "pass" "未检测到"
  fi

  # Docker
  if cmd_exists docker; then
    if docker info &>/dev/null; then
      check_item "$(L hc_docker)" "pass" "$(docker --version 2>/dev/null | head -1 || echo 'installed')"
    else
      check_item "$(L hc_docker)" "warn" "已安装但守护进程未运行"
    fi
  else
    check_item "$(L hc_docker)" "na" "未安装"
  fi

  # ── Summary ──────────────────────────────────────────────────────────
  echo ""
  hr "$C_DIM"
  local total=$((pass_count + warn_count + fail_count))
  echo -e "  ${C_GREEN}✓ $(L hc_summary_pass | sed "s/%d/$pass_count/")${C_RESET}  ${C_YELLOW}⚠ $(L hc_summary_warn | sed "s/%d/$warn_count/")${C_RESET}  ${C_RED}✗ $(L hc_summary_fail | sed "s/%d/$fail_count/")${C_RESET}  ${C_DIM}$(L hc_summary_total | sed "s/%d/$total/")${C_RESET}"
  hr "$C_DIM"
  echo ""

  # ── Performance Assessment & Recommendation ──────────────────────────
  local score=0
  local max_score=100

  # RAM scoring (max 35 points)
  # 1GB=5, 2GB=10, 4GB=20, 8GB+=35
  if [ "${ram_total_gb%%.*}" -ge 8 ] 2>/dev/null; then
    score=$((score+35))
  elif [ "${ram_total_gb%%.*}" -ge 4 ] 2>/dev/null; then
    score=$((score+20))
  elif [ "${ram_total_gb%%.*}" -ge 2 ] 2>/dev/null; then
    score=$((score+10))
  elif [ "${ram_total_gb%%.*}" -ge 1 ] 2>/dev/null; then
    score=$((score+5))
  fi

  # CPU scoring (max 25 points)
  # 1 core=5, 2 cores=10, 4 cores=15, 8+ cores=25
  if [ "${cpu_cores:-0}" -ge 8 ] 2>/dev/null; then
    score=$((score+25))
  elif [ "${cpu_cores:-0}" -ge 4 ] 2>/dev/null; then
    score=$((score+15))
  elif [ "${cpu_cores:-0}" -ge 2 ] 2>/dev/null; then
    score=$((score+10))
  elif [ "${cpu_cores:-0}" -ge 1 ] 2>/dev/null; then
    score=$((score+5))
  fi

  # Disk scoring (max 20 points)
  local disk_avail_num=${disk_avail_gb%%[A-Za-z ]*}
  disk_avail_num=${disk_avail_num%%.*}
  if [ "${disk_avail_num:-0}" -ge 20 ] 2>/dev/null; then
    score=$((score+20))
  elif [ "${disk_avail_num:-0}" -ge 10 ] 2>/dev/null; then
    score=$((score+15))
  elif [ "${disk_avail_num:-0}" -ge 5 ] 2>/dev/null; then
    score=$((score+10))
  elif [ "${disk_avail_num:-0}" -ge 2 ] 2>/dev/null; then
    score=$((score+5))
  fi

  # Services scoring (max 20 points)
  # PostgreSQL running = +10, Redis running = +10
  if cmd_exists psql || systemctl is-active --quiet postgresql 2>/dev/null; then
    score=$((score+10))
  fi
  if cmd_exists redis-cli || systemctl is-active --quiet redis 2>/dev/null || systemctl is-active --quiet redis-server 2>/dev/null; then
    score=$((score+10))
  fi

  # Draw score bar
  local bar_width=40
  local filled=$(( score * bar_width / max_score ))
  local empty=$(( bar_width - filled ))
  local score_color="${C_RED}"
  [ "$score" -ge 60 ] && score_color="${C_YELLOW}"
  [ "$score" -ge 80 ] && score_color="${C_GREEN}"

  echo -e "  ${C_BOLD}⚡ 设备性能评分${C_RESET}"
  echo ""
  printf "  ["
  printf "${score_color}"
  printf '%0.s█' $(seq 1 "$filled" 2>/dev/null) || true
  printf "${C_DIM}"
  printf '%0.s░' $(seq 1 "$empty" 2>/dev/null) || true
  printf "${C_RESET}"
  echo "] ${score_color}${C_BOLD}${score}/100${C_RESET}"
  echo ""

  # Recommendation
  hr "$C_CYAN"
  echo -e "  ${C_BOLD}📋 部署建议${C_RESET}"
  hr "$C_CYAN"
  echo ""

  if [ "$score" -ge 80 ]; then
    # Full binary install
    echo -e "  ${C_GREEN}${C_BOLD}✅ 推荐：二进制安装（完整版）${C_RESET}"
    echo ""
    echo -e "  ${C_DIM}您的设备性能充足，完全满足 LightBridge 全量版本的运行要求。${C_RESET}"
    echo -e "  ${C_DIM}建议使用 systemd 部署，可获得最佳性能和稳定性。${C_RESET}"
    echo ""
    echo -e "  ${C_BOLD}安装命令:${C_RESET}"
    echo -e "    ${C_CYAN}curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/lightbridge-assistant.sh | sudo bash -s -- install${C_RESET}"
    echo ""
  elif [ "$score" -ge 50 ]; then
    # Docker or limited binary
    echo -e "  ${C_YELLOW}${C_BOLD}⚠️  推荐：Docker 部署${C_RESET}"
    echo ""
    echo -e "  ${C_DIM}您的设备可以运行 LightBridge，但内存或磁盘偏小。${C_RESET}"
    echo -e "  ${C_DIM}建议使用 Docker 部署以降低资源占用，获得更好的隔离性。${C_RESET}"
    echo ""
    echo -e "  ${C_BOLD}安装命令:${C_RESET}"
    echo -e "    ${C_CYAN}curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/lightbridge-assistant.sh | sudo bash -s -- docker${C_RESET}"
    echo ""
  else
    # Cannot run
    echo -e "  ${C_RED}${C_BOLD}❌ 设备资源不足${C_RESET}"
    echo ""
    echo -e "  ${C_DIM}您的设备内存或磁盘空间不足，无法稳定运行 LightBridge。${C_RESET}"
    echo -e "  ${C_DIM}建议升级服务器配置或选择更高规格的实例。${C_RESET}"
    echo ""
    echo -e "  ${C_BOLD}最低要求:${C_RESET}"
    echo -e "    • 内存: ${C_YELLOW}≥ 1 GB${C_RESET}（当前 ${ram_total_gb} GB）"
    echo -e "    • 磁盘: ${C_YELLOW}≥ 2 GB 可用${C_RESET}"
    echo -e "    • CPU:  ${C_YELLOW}≥ 1 核${C_RESET}"
    echo ""
  fi

  echo ""
  hr "$C_DIM"
  echo ""

  if is_interactive; then
    printf "  ${C_DIM}$(L press_enter)${C_RESET} "
    read_input
  fi
}

# ── Version fetching ───────────────────────────────────────────────────────
get_latest_version() {
  sub_step "从 GitHub API 获取最新版本..."
  explain "请求: api.github.com/repos/${GITHUB_REPO}/releases"

  local api_response
  api_response=$(curl -s --connect-timeout 10 --max-time 30 \
    -w "\n%{http_code}" \
    "https://api.github.com/repos/${GITHUB_REPO}/releases?per_page=30" 2>/dev/null)
  local http_code
  http_code=$(echo "$api_response" | tail -1)
  api_response=$(echo "$api_response" | sed '$d')

  if [ "$http_code" != "200" ]; then
    sub_fail "GitHub API 请求失败" "HTTP ${http_code}"
    echo ""
    echo -e "  ${C_YELLOW}${C_BOLD}🔧 解决方法:${C_RESET}"
    case "${http_code}" in
      000)
        echo -e "    • 网络连接失败 — 检查网络和代理设置"
        echo -e "    • 检查: ${C_CYAN}curl -I https://api.github.com${C_RESET}"
        ;;
      403)
        echo -e "    • GitHub API 速率限制 — 等待几分钟后重试"
        echo -e "    • 或设置 GitHub Token: ${C_CYAN}export GITHUB_TOKEN=xxx${C_RESET}"
        ;;
      404)
        echo -e "    • 仓库不存在或已私有化"
        echo -e "    • 请确认: ${C_CYAN}https://github.com/${GITHUB_REPO}${C_RESET}"
        ;;
      5*)
        echo -e "    • GitHub 服务器错误 — 稍后重试"
        ;;
      *)
        echo -e "    • 未知错误 (HTTP ${http_code}) — 检查网络连接"
        ;;
    esac
    # 回退：尝试从已知版本列表推断
    LATEST_VERSION=""
    return 1
  fi

  LATEST_VERSION=$(echo "$api_response" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/' \
    | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1)

  if [ -z "$LATEST_VERSION" ]; then
    sub_fail "未找到可用版本"
    echo ""
    echo -e "  ${C_YELLOW}${C_BOLD}🔧 解决方法:${C_RESET}"
    echo -e "    1. 仓库可能没有正式 release，请检查:"
    echo -e "       ${C_CYAN}https://github.com/${GITHUB_REPO}/releases${C_RESET}"
    echo -e "    2. 可手动指定版本: ${C_CYAN}$0 install -v v0.2.13${C_RESET}"
    return 1
  fi

  sub_done "最新稳定版" "${C_BOLD}${LATEST_VERSION}${C_RESET}"
}

list_versions() {
  sub_step "获取可用版本列表..."
  explain "从 GitHub Releases API 获取最近 30 个版本"

  local api_response
  api_response=$(curl -s --connect-timeout 10 --max-time 30 \
    -w "\n%{http_code}" \
    "https://api.github.com/repos/${GITHUB_REPO}/releases?per_page=30" 2>/dev/null)
  local http_code
  http_code=$(echo "$api_response" | tail -1)
  api_response=$(echo "$api_response" | sed '$d')

  if [ "$http_code" != "200" ]; then
    sub_fail "获取版本列表失败" "HTTP ${http_code}"
    explain "请检查网络连接: ${C_CYAN}curl -I https://api.github.com${C_RESET}"
    return 1
  fi

  local versions
  versions=$(echo "$api_response" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/' \
    | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -20)
  echo -e "\r                            "
  echo ""
  echo -e "  ${C_BOLD}$(L ver_available):${C_RESET}"
  echo -e "  ${C_DIM}────────────────────────────────────────${C_RESET}"
  echo "$versions" | while read -r v; do
    echo -e "    ${C_CYAN}●${C_RESET} $v"
  done
  echo -e "  ${C_DIM}────────────────────────────────────────${C_RESET}"
}

# ── UX helpers: 傻瓜式全程引导 ────────────────────────────────────────────
STEP_START_TIME=0

# Clear screen + show step header with description
step_header() {
  local num="$1" total="$2" title="$3" desc="${4:-}"
  clear 2>/dev/null || true
  echo ""
  hr "$C_CYAN"
  center "${C_BRIGHT_CYAN}${C_BOLD}" "  安装进度  ${num}/${total}"
  hr "$C_CYAN"
  echo ""
  echo -e "  ${C_BOLD}${C_GOLD}▶ 第${num}步：${title}${C_RESET}"
  [ -n "$desc" ] && echo -e "  ${C_DIM}${desc}${C_RESET}"
  echo ""
  STEP_START_TIME=$(date +%s 2>/dev/null || echo 0)
}

# Show a sub-step being processed
sub_step() {
  local text="$1"
  printf "  ${C_CYAN}⠋${C_RESET} ${text}"
}

# Mark a sub-step as done
sub_done() {
  local text="$1" detail="${2:-}"
  if [ -n "$detail" ]; then
    echo -e "\r  ${C_GREEN}✓${C_RESET} ${text}  ${C_DIM}${detail}${C_RESET}          "
  else
    echo -e "\r  ${C_GREEN}✓${C_RESET} ${text}          "
  fi
}

# Mark a sub-step as failed
sub_fail() {
  local text="$1" detail="${2:-}"
  if [ -n "$detail" ]; then
    echo -e "\r  ${C_RED}✗${C_RESET} ${text}  ${C_DIM}${detail}${C_RESET}          "
  else
    echo -e "\r  ${C_RED}✗${C_RESET} ${text}          "
  fi
}

# Mark a sub-step as warning
sub_warn() {
  local text="$1" detail="${2:-}"
  if [ -n "$detail" ]; then
    echo -e "\r  ${C_YELLOW}⚠${C_RESET} ${text}  ${C_DIM}${detail}${C_RESET}          "
  else
    echo -e "\r  ${C_YELLOW}⚠${C_RESET} ${text}          "
  fi
}

# Show explanatory text
explain() {
  echo -e "  ${C_DIM}  ℹ  $1${C_RESET}"
}

# Show a divider between logical sections within a step
step_divider() {
  echo -e "  ${C_DIM}────────────────────────────────────────────────${C_RESET}"
}

# Confirm before proceeding (interactive only)
confirm_go() {
  local msg="${1:-确认继续？}"
  if ! is_interactive; then return 0; fi
  echo ""
  printf "  ${C_BOLD}${msg} [Y/n]:${C_RESET} "
  local ans=""
  read_input ans
  [[ "$ans" =~ ^[Nn]$ ]] && { echo -e "  ${C_YELLOW}已取消${C_RESET}"; exit 0; }
  echo ""
}

# Show elapsed time for a step
step_elapsed() {
  if [ "$STEP_START_TIME" -gt 0 ] 2>/dev/null; then
    local now
    now=$(date +%s 2>/dev/null || echo 0)
    local elapsed=$(( now - STEP_START_TIME ))
    if [ "$elapsed" -gt 0 ]; then
      echo -e "  ${C_DIM}  ⏱  耗时 ${elapsed}s${C_RESET}"
    fi
  fi
}

# ── Server configuration prompt ────────────────────────────────────────────
configure_server() {
  if ! is_interactive; then
    explain "$(L srv_summary): ${SERVER_HOST}:${SERVER_PORT} (使用默认值)"
    return
  fi

  sub_step "配置服务器监听地址..."
  echo ""
  explain "这里配置 LightBridge 服务监听的网络地址和端口。"
  explain "如果不确定，直接按回车使用默认值即可。"
  echo ""

  # Host
  sub_step "$(L srv_host)"
  echo ""
  explain "$(L srv_host_hint)"
  printf "  ${C_BOLD}输入地址${C_RESET} [${C_CYAN}${SERVER_HOST}${C_RESET}]: "
  input_host=""
  read_input input_host
  [ -n "$input_host" ] && SERVER_HOST="$input_host"
  sub_done "$(L srv_host)" "${SERVER_HOST}"
  echo ""

  # Port
  sub_step "$(L srv_port)"
  echo ""
  explain "$(L srv_port_hint)"
  while true; do
    printf "  ${C_BOLD}输入端口${C_RESET} [${C_CYAN}${SERVER_PORT}${C_RESET}]: "
    input_port=""
    read_input input_port
    if [ -z "$input_port" ]; then
      break
    elif [[ "$input_port" =~ ^[0-9]+$ ]] && [ "$input_port" -ge 1 ] && [ "$input_port" -le 65535 ]; then
      # 检查端口是否被占用
      local port_in_use=false
      if cmd_exists ss; then
        if ss -tlnp 2>/dev/null | grep -q ":${input_port} "; then
          port_in_use=true
        fi
      elif cmd_exists lsof; then
        if lsof -i ":${input_port}" &>/dev/null; then
          port_in_use=true
        fi
      elif cmd_exists netstat; then
        if netstat -tlnp 2>/dev/null | grep -q ":${input_port} "; then
          port_in_use=true
        fi
      fi
      if $port_in_use; then
        echo -e "  ${C_YELLOW}  ⚠ 端口 ${input_port} 已被其他进程占用${C_RESET}"
        echo -e "  ${C_DIM}    查看占用进程: ss -tlnp | grep :${input_port}${C_RESET}"
        echo -e "  ${C_DIM}    请选择其他端口，或先停止占用进程${C_RESET}"
        continue
      fi
      SERVER_PORT="$input_port"
      break
    else
      echo -e "  ${C_RED}  ✗ $(L srv_invalid_port)，请输入 1-65535 之间的数字${C_RESET}"
    fi
  done
  sub_done "$(L srv_port)" "${SERVER_PORT}"
  echo ""
  sub_done "服务器配置完成" "${C_BOLD}${SERVER_HOST}:${SERVER_PORT}${C_RESET}"
  echo ""
}

# ── Version prompt ─────────────────────────────────────────────────────────
prompt_version() {
  local prompt_text="${1:-$(L rb_prompt)}"
  if is_interactive; then
    printf "  ${C_BOLD}${prompt_text}${C_RESET} [${C_CYAN}${LATEST_VERSION}${C_RESET}]: "
    ver_input=""
    read_input ver_input
    if [ -n "$ver_input" ]; then
      [[ "$ver_input" == v* ]] || ver_input="v$ver_input"
      LATEST_VERSION="$ver_input"
    fi
  fi
}

# ── Download & install binary ──────────────────────────────────────────────
download_and_extract() {
  local target_binary="${1:-$INSTALL_DIR/LightBridge}"
  local target_dir
  target_dir=$(dirname "$target_binary")
  local version_num=${LATEST_VERSION#v}
  local archive_name="LightBridge_${version_num}_${OS}_${ARCH}.tar.gz"
  local download_url="https://github.com/${GITHUB_REPO}/releases/download/${LATEST_VERSION}/${archive_name}"
  local checksum_url="https://github.com/${GITHUB_REPO}/releases/download/${LATEST_VERSION}/checksums.txt"

  # ── 预检：网络连通性 ──
  sub_step "检测网络连通性..."
  if ! curl -s --connect-timeout 5 --max-time 10 "https://github.com" -o /dev/null 2>/dev/null; then
    sub_fail "无法连接到 GitHub"
    echo ""
    echo -e "  ${C_YELLOW}${C_BOLD}🔧 解决方法:${C_RESET}"
    echo -e "    1. 检查网络: ${C_CYAN}ping github.com${C_RESET}"
    echo -e "    2. 检查代理: ${C_CYAN}echo \$http_proxy \$https_proxy${C_RESET}"
    echo -e "    3. 如需代理，设置环境变量后重试:"
    echo -e "       ${C_CYAN}export https_proxy=http://127.0.0.1:7890${C_RESET}"
    echo -e "       ${C_CYAN}$0 install${C_RESET}"
    echo -e "    4. 检查 DNS: ${C_CYAN}nslookup github.com${C_RESET}"
    echo -e "    5. 检查防火墙是否放行 443 端口"
    return 1
  fi
  sub_done "网络连通" "github.com 可达"

  # Sub-step 1: Download
  sub_step "从 GitHub 下载 LightBridge ${C_BOLD}${LATEST_VERSION}${C_RESET}..."
  explain "文件: ${archive_name}"
  explain "来源: github.com/${GITHUB_REPO}"

  TEMP_DIR=$(mktemp -d)
  trap 'rm -rf "$TEMP_DIR"' EXIT

  # 带重试的下载
  local dl_success=false
  local dl_retries=3
  local dl_delay=5
  for attempt in $(seq 1 $dl_retries); do
    if [ "$attempt" -gt 1 ]; then
      explain "第 ${attempt}/${dl_retries} 次重试，${dl_delay}s 后开始..."
      sleep "$dl_delay"
      dl_delay=$((dl_delay * 2))
    fi
    sub_step "下载中... (第 ${attempt}/${dl_retries} 次)"
    if curl -fSL --connect-timeout 15 --max-time 300 --retry 2 \
         -o "$TEMP_DIR/$archive_name" "$download_url" 2>/dev/null; then
      # 验证下载的文件不是 HTML 错误页
      local file_header
      file_header=$(file -b "$TEMP_DIR/$archive_name" 2>/dev/null || echo "")
      if echo "$file_header" | grep -qi "html\|text\|empty"; then
        sub_warn "下载内容异常" "收到的不是有效压缩包"
        explain "服务器可能返回了错误页面"
        rm -f "$TEMP_DIR/$archive_name"
        continue
      fi
      dl_success=true
      break
    fi
    local curl_exit=$?
    case $curl_exit in
      6)  explain "DNS 解析失败 — 检查 DNS 配置" ;;
      7)  explain "连接被拒绝 — 检查防火墙或代理" ;;
      28) explain "连接超时 — 网络不稳定，将自动重试" ;;
      35) explain "SSL/TLS 握手失败 — 检查证书或代理" ;;
      56) explain "接收数据失败 — 网络中断" ;;
      *)  explain "curl 错误码: ${curl_exit}" ;;
    esac
  done

  if ! $dl_success; then
    sub_fail "下载失败" "已重试 ${dl_retries} 次"
    echo ""
    echo -e "  ${C_YELLOW}${C_BOLD}🔧 解决方法:${C_RESET}"
    echo -e "    1. 检查网络稳定性: ${C_CYAN}curl -I https://github.com${C_RESET}"
    echo -e "    2. 手动下载后离线安装:"
    echo -e "       ${C_CYAN}wget ${download_url}${C_RESET}"
    echo -e "       ${C_CYAN}tar -xzf ${archive_name}${C_RESET}"
    echo -e "       ${C_CYAN}cp LightBridge ${target_binary}${C_RESET}"
    echo -e "    3. 如在大陆地区，可能需要配置代理或使用镜像"
    echo -e "    4. 检查版本是否存在: ${C_CYAN}$0 versions${C_RESET}"
    return 1
  fi

  local file_size
  file_size=$(du -h "$TEMP_DIR/$archive_name" 2>/dev/null | awk '{print $1}')
  sub_done "下载完成" "${C_BOLD}${file_size}${C_RESET}"

  # Sub-step 2: Checksum
  step_divider
  sub_step "校验文件完整性..."
  explain "下载完成后自动校验文件的 SHA256 哈希值，确保文件未被篡改"
  if curl -fSL --connect-timeout 10 --max-time 30 -o "$TEMP_DIR/checksums.txt" "$checksum_url" 2>/dev/null; then
    local expected actual
    expected=$(grep "$archive_name" "$TEMP_DIR/checksums.txt" | awk '{print $1}')
    actual=$(sha256sum "$TEMP_DIR/$archive_name" 2>/dev/null | awk '{print $1}' \
          || shasum -a 256 "$TEMP_DIR/$archive_name" 2>/dev/null | awk '{print $1}')
    if [ -z "$expected" ]; then
      sub_warn "校验文件中未找到该版本" "跳过校验，继续安装"
    elif [ "$expected" = "$actual" ]; then
      sub_done "校验通过" "SHA256 匹配"
    else
      sub_fail "校验失败" "文件可能已损坏或被篡改"
      echo ""
      echo -e "  ${C_YELLOW}${C_BOLD}🔧 解决方法:${C_RESET}"
      echo -e "    1. 重新运行安装命令（会重新下载）"
      echo -e "    2. 如反复失败，可能是网络中间人攻击"
      echo -e "       期望: ${C_DIM}${expected}${C_RESET}"
      echo -e "       实际: ${C_DIM}${actual}${C_RESET}"
      echo -e "    3. 可跳过校验直接安装（不推荐）:"
      echo -e "       ${C_CYAN}cp ${TEMP_DIR}/${archive_name} /tmp/${C_RESET}"
      echo -e "       ${C_CYAN}tar -xzf /tmp/${archive_name} -C /tmp/${C_RESET}"
      echo -e "       ${C_CYAN}sudo cp /tmp/LightBridge ${target_binary}${C_RESET}"
      return 1
    fi
  else
    sub_warn "校验文件下载失败" "跳过校验，继续安装"
    explain "可能是该版本较旧，校验文件已被清理"
  fi

  # Sub-step 3: Extract
  step_divider
  sub_step "解压安装包..."
  if ! tar -xzf "$TEMP_DIR/$archive_name" -C "$TEMP_DIR" 2>/dev/null; then
    sub_fail "解压失败" "安装包可能已损坏"
    echo ""
    echo -e "  ${C_YELLOW}${C_BOLD}🔧 解决方法:${C_RESET}"
    echo -e "    1. 重新运行安装命令"
    echo -e "    2. 检查磁盘空间: ${C_CYAN}df -h${C_RESET}"
    echo -e "    3. 手动解压测试: ${C_CYAN}tar -tzf ${TEMP_DIR}/${archive_name}${C_RESET}"
    return 1
  fi

  if [ ! -f "$TEMP_DIR/LightBridge" ]; then
    sub_fail "解压后未找到 LightBridge 二进制文件"
    echo ""
    echo -e "  ${C_YELLOW}${C_BOLD}🔧 可能原因:${C_RESET}"
    echo -e "    1. 下载的文件格式不匹配 (OS=${OS}, ARCH=${ARCH})"
    echo -e "    2. 版本包结构已变更"
    echo -e "    3. 可用版本列表: ${C_CYAN}$0 versions${C_RESET}"
    return 1
  fi

  mkdir -p "$target_dir"
  if ! cp "$TEMP_DIR/LightBridge" "$target_binary" 2>/dev/null; then
    sub_fail "无法写入目标路径" "${C_DIM}${target_binary}${C_RESET}"
    echo ""
    echo -e "  ${C_YELLOW}${C_BOLD}🔧 解决方法:${C_RESET}"
    echo -e "    1. 需要 root 权限: ${C_CYAN}sudo $0 install${C_RESET}"
    echo -e "    2. 检查目录权限: ${C_CYAN}ls -la $(dirname "$target_binary")${C_RESET}"
    echo -e "    3. 目标磁盘可能已满: ${C_CYAN}df -h ${target_dir}${C_RESET}"
    return 1
  fi

  chmod +x "$target_binary"
  sub_done "解压完成"
  sub_done "安装到" "${C_DIM}${target_binary}${C_RESET}"
}

# ── Create system user ─────────────────────────────────────────────────────
create_user() {
  if id "$SERVICE_USER" &>/dev/null; then
    sub_done "系统用户已存在" "${C_DIM}${SERVICE_USER}${C_RESET}"
    local cur_shell
    cur_shell=$(getent passwd "$SERVICE_USER" 2>/dev/null | cut -d: -f7)
    if [ "$cur_shell" = "/bin/false" ] || [ "$cur_shell" = "/sbin/nologin" ]; then
      explain "修复用户 shell 以支持 sudo 操作"
      if ! usermod -s /bin/sh "$SERVICE_USER" 2>/dev/null; then
        sub_warn "shell 修复失败" "服务可能无法自动重启"
        explain "手动修复: sudo usermod -s /bin/sh ${SERVICE_USER}"
      fi
    fi
  else
    sub_step "创建系统用户 ${C_DIM}${SERVICE_USER}${C_RESET}..."
    explain "用于运行 LightBridge 服务，限制权限以增强安全性"
    if ! useradd -r -s /bin/sh -d "$INSTALL_DIR" "$SERVICE_USER" 2>/dev/null; then
      sub_fail "用户创建失败"
      echo ""
      echo -e "  ${C_YELLOW}${C_BOLD}🔧 解决方法:${C_RESET}"
      echo -e "    1. 确认已使用 root 权限: ${C_CYAN}sudo $0 install${C_RESET}"
      echo -e "    2. 检查用户是否已存在: ${C_CYAN}id ${SERVICE_USER}${C_RESET}"
      echo -e "    3. 检查 /etc/passwd 是否可写"
      return 1
    fi
    sub_done "用户创建成功" "${C_DIM}${SERVICE_USER}${C_RESET}"
  fi
}

# ── Setup directories ──────────────────────────────────────────────────────
setup_directories() {
  sub_step "创建目录结构..."
  explain "${INSTALL_DIR}          — 程序运行目录"
  explain "${INSTALL_DIR}/data     — 模块和运行数据"
  explain "${CONFIG_DIR}        — 配置文件目录"

  local dir_failed=false
  for dir in "$INSTALL_DIR" "$INSTALL_DIR/data" "$CONFIG_DIR"; do
    if ! mkdir -p "$dir" 2>/dev/null; then
      sub_fail "无法创建目录" "${C_DIM}${dir}${C_RESET}"
      echo ""
      echo -e "  ${C_YELLOW}${C_BOLD}🔧 解决方法:${C_RESET}"
      echo -e "    1. 需要 root 权限: ${C_CYAN}sudo $0 install${C_RESET}"
      echo -e "    2. 磁盘可能已满: ${C_CYAN}df -h${C_RESET}"
      echo -e "    3. 检查父目录权限: ${C_CYAN}ls -la $(dirname "$dir")${C_RESET}"
      dir_failed=true
      break
    fi
  done
  $dir_failed && return 1

  chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR" 2>/dev/null || \
    explain "权限设置部分失败，不影响核心功能"
  chown -R "$SERVICE_USER:$SERVICE_USER" "$CONFIG_DIR" 2>/dev/null || true
  sub_done "目录创建完成" "已设置权限 ${C_DIM}${SERVICE_USER}:${SERVICE_USER}${C_RESET}"
}

# ── Install systemd service ────────────────────────────────────────────────
install_service() {
  sub_step "安装 systemd 服务..."
  explain "创建 /etc/systemd/system/LightBridge.service"

  if ! command -v systemctl &>/dev/null; then
    sub_warn "systemctl 不存在" "跳过 systemd 服务安装"
    explain "您的系统可能没有 systemd，Docker 部署模式不受影响"
    explain "手动管理: ${INSTALL_DIR}/LightBridge"
    return 0
  fi

  if ! cat > /etc/systemd/system/LightBridge.service << EOF
[Unit]
Description=LightBridge - AI API Gateway Platform
Documentation=https://github.com/${GITHUB_REPO}
After=network.target postgresql.service redis.service
Wants=postgresql.service redis.service

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/LightBridge
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=${INSTALL_DIR} ${CONFIG_DIR}
Environment=GIN_MODE=release
Environment=DATA_DIR=${INSTALL_DIR}
Environment=SERVER_HOST=${SERVER_HOST}
Environment=SERVER_PORT=${SERVER_PORT}

[Install]
WantedBy=multi-user.target
EOF
  2>/dev/null; then
    sub_fail "systemd 服务文件写入失败"
    echo ""
    echo -e "  ${C_YELLOW}${C_BOLD}🔧 解决方法:${C_RESET}"
    echo -e "    1. 需要 root 权限: ${C_CYAN}sudo $0 install${C_RESET}"
    echo -e "    2. 检查 /etc/systemd/system/ 目录权限"
    return 1
  fi

  if ! systemctl daemon-reload 2>/dev/null; then
    sub_warn "daemon-reload 失败" "服务配置可能需要手动加载"
    explain "手动执行: sudo systemctl daemon-reload"
  fi
  sub_done "systemd 服务安装完成" "开机自启已启用"
}

# ── Start service ──────────────────────────────────────────────────────────
start_service() {
  sub_step "启动 LightBridge 服务..."
  explain "执行 systemctl start LightBridge"

  # 检查端口是否被占用
  if cmd_exists ss; then
    local port_user
    port_user=$(ss -tlnp 2>/dev/null | grep ":${SERVER_PORT} " | head -1 || true)
    if [ -n "$port_user" ]; then
      sub_warn "端口 ${SERVER_PORT} 已被占用"
      echo ""
      echo -e "  ${C_YELLOW}${C_BOLD}🔧 解决方法:${C_RESET}"
      echo -e "    1. 查看占用进程: ${C_CYAN}ss -tlnp | grep :${SERVER_PORT}${C_RESET}"
      echo -e "    2. 停止占用进程: ${C_CYAN}sudo kill <PID>${C_RESET}"
      echo -e "    3. 或更改端口: 重新运行安装并选择其他端口"
      echo ""
    fi
  elif cmd_exists lsof; then
    if lsof -i ":${SERVER_PORT}" &>/dev/null; then
      sub_warn "端口 ${SERVER_PORT} 可能已被占用"
      explain "检查: sudo lsof -i :${SERVER_PORT}"
    fi
  fi

  if ! systemctl start LightBridge 2>/dev/null; then
    sub_fail "服务启动失败"
    echo ""
    echo -e "  ${C_YELLOW}${C_BOLD}🔧 排查步骤:${C_RESET}"
    echo -e "    1. 查看启动错误: ${C_CYAN}sudo journalctl -u LightBridge -n 30 --no-pager${C_RESET}"
    echo -e "    2. 检查配置文件: ${C_CYAN}cat ${CONFIG_DIR}/config.yaml${C_RESET}"
    echo -e "    3. 检查数据库连接: ${C_CYAN}sudo -u ${SERVICE_USER} pg_isready${C_RESET}"
    echo -e "    4. 检查 Redis 连接: ${C_CYAN}redis-cli ping${C_RESET}"
    echo -e "    5. 手动启动调试: ${C_CYAN}sudo -u ${SERVICE_USER} ${INSTALL_DIR}/LightBridge${C_RESET}"
    return 1
  fi

  sleep 2
  if systemctl is-active --quiet LightBridge 2>/dev/null; then
    sub_done "服务启动成功" "PID: $(systemctl show -p MainPID --value LightBridge 2>/dev/null || echo '?')"
    return 0
  fi

  # 服务启动了但很快退出
  sub_fail "服务启动后立即退出"
  echo ""
  echo -e "  ${C_YELLOW}${C_BOLD}🔧 排查步骤:${C_RESET}"
  echo -e "    1. 查看退出原因: ${C_CYAN}sudo journalctl -u LightBridge -n 50 --no-pager${C_RESET}"
  echo -e "    2. 检查二进制文件: ${C_CYAN}file ${INSTALL_DIR}/LightBridge${C_RESET}"
  echo -e "    3. 检查依赖库: ${C_CYAN}ldd ${INSTALL_DIR}/LightBridge${C_RESET}"
  echo -e "    4. 手动运行看报错: ${C_CYAN}sudo -u ${SERVICE_USER} ${INSTALL_DIR}/LightBridge${C_RESET}"
  echo -e "    5. 检查 SELinux: ${C_CYAN}getenforce 2>/dev/null${C_RESET}"
  return 1
}

enable_autostart() {
  sub_step "设置开机自启..."
  if systemctl enable LightBridge 2>/dev/null; then
    sub_done "开机自启已启用" "服务器重启后自动运行"
  else
    sub_warn "开机自启设置失败" "服务已安装但需手动启用"
    explain "手动启用: ${C_CYAN}sudo systemctl enable LightBridge${C_RESET}"
  fi
}

# ── Get public IP ──────────────────────────────────────────────────────────
get_public_ip() {
  sub_step "检测公网 IP..."
  explain "通过 ipinfo.io 获取服务器的公网 IP 地址"
  local response
  response=$(curl -s --connect-timeout 5 --max-time 10 "https://ipinfo.io/json" 2>/dev/null || true)
  if [ -n "$response" ]; then
    PUBLIC_IP=$(echo "$response" | grep -o '"ip": *"[^"]*"' | sed 's/"ip": *"\([^"]*\)"/\1/' || true)
    if [ -n "$PUBLIC_IP" ]; then
      sub_done "公网 IP 检测成功" "${C_BOLD}${PUBLIC_IP}${C_RESET}"
      return 0
    fi
  fi
  PUBLIC_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "YOUR_SERVER_IP")
  sub_warn "无法获取公网 IP" "使用本地 IP → ${PUBLIC_IP}"
  explain "您可能需要手动配置防火墙或安全组规则"
  return 1
}

# ── Configure module release ──────────────────────────────────────────────
configure_module_release() {
  sub_step "配置 OpenAI Provider 模块..."
  explain "下载模块签名公钥并写入配置文件"
  local config_file=""
  local key_path="$INSTALL_DIR/data/modules/ed25519.pub"
  mkdir -p "$(dirname "$key_path")"

  if [ -f "$CONFIG_DIR/config.yaml" ]; then
    config_file="$CONFIG_DIR/config.yaml"
  elif [ -f "$INSTALL_DIR/config.yaml" ]; then
    config_file="$INSTALL_DIR/config.yaml"
  fi

  if curl -fsSL "$MODULE_PUBLIC_KEY_URL" -o "$key_path" 2>/dev/null; then
    chown "$SERVICE_USER:$SERVICE_USER" "$key_path" 2>/dev/null || true
    sub_done "签名公钥已下载" "${C_DIM}${key_path}${C_RESET}"
  else
    sub_warn "签名公钥下载失败" "模块安装可能受限"
  fi

  if [ -z "$config_file" ]; then
    config_file="$CONFIG_DIR/config.yaml"
    mkdir -p "$CONFIG_DIR"
    touch "$config_file"
  fi

  if grep -q '^modules:' "$config_file" 2>/dev/null; then
    grep -q 'marketplace_registry_url:' "$config_file" || \
      sed -i.bak "/^modules:/a\\  marketplace_registry_url: \"${MODULE_REGISTRY_URL}\"" "$config_file" 2>/dev/null || \
      sed -i '' "/^modules:/a\\
  marketplace_registry_url: \"${MODULE_REGISTRY_URL}\"" "$config_file" 2>/dev/null || true
    grep -q 'signature_public_key_path:' "$config_file" || \
      sed -i.bak "/^modules:/a\\  signature_public_key_path: \"${key_path}\"" "$config_file" 2>/dev/null || \
      sed -i '' "/^modules:/a\\
  signature_public_key_path: \"${key_path}\"" "$config_file" 2>/dev/null || true
  else
    cat >> "$config_file" << EOF

modules:
  marketplace_registry_url: "${MODULE_REGISTRY_URL}"
  signature_public_key_path: "${key_path}"
  marketplace_timeout_seconds: 20
EOF
  fi

  [ -n "$SERVICE_USER" ] && id "$SERVICE_USER" &>/dev/null && \
    chown "$SERVICE_USER:$SERVICE_USER" "$config_file" 2>/dev/null || true
}

# ── Installation completion message ────────────────────────────────────────
print_completion() {
  local display_host="${PUBLIC_IP:-YOUR_SERVER_IP}"
  [ "$SERVER_HOST" = "127.0.0.1" ] && display_host="127.0.0.1"

  echo ""
  hr_double "$C_GREEN"
  center "$C_BRIGHT_GREEN" "  ✨  $(L done_title)  ✨"
  hr_double "$C_GREEN"
  echo ""
  echo -e "  ${C_BOLD}$(L done_dir):${C_RESET}     ${C_CYAN}${INSTALL_DIR}${C_RESET}"
  echo -e "  ${C_BOLD}$(L srv_summary):${C_RESET}  ${C_CYAN}${SERVER_HOST}:${SERVER_PORT}${C_RESET}"
  echo ""
  hr "$C_BLUE"
  center "$C_BRIGHT_BLUE" "  🌐  $(L done_url)"
  hr "$C_BLUE"
  echo ""
  center "$C_BRIGHT_CYAN" "  http://${display_host}:${SERVER_PORT}"
  echo ""
  echo -e "  ${C_BOLD}$(L done_wizard)${C_RESET}"
  echo -e "    ${C_GREEN}●${C_RESET} $(L done_wizard_db)"
  echo -e "    ${C_GREEN}●${C_RESET} $(L done_wizard_redis)"
  echo -e "    ${C_GREEN}●${C_RESET} $(L done_wizard_admin)"
  echo ""
  hr "$C_BLUE"
  center "$C_BRIGHT_BLUE" "  ⚡  $(L done_cmds)"
  hr "$C_BLUE"
  echo ""
  echo -e "    ${C_CYAN}sudo systemctl status LightBridge${C_RESET}    ${C_DIM}# $(L done_status)${C_RESET}"
  echo -e "    ${C_CYAN}sudo journalctl -u LightBridge -f${C_RESET}    ${C_DIM}# $(L done_logs)${C_RESET}"
  echo -e "    ${C_CYAN}sudo systemctl restart LightBridge${C_RESET}   ${C_DIM}# $(L done_restart)${C_RESET}"
  echo -e "    ${C_CYAN}sudo systemctl stop LightBridge${C_RESET}      ${C_DIM}# $(L done_stop)${C_RESET}"
  echo ""
  hr_double "$C_GREEN"
  echo ""
}

# ════════════════════════════════════════════════════════════════════════════
#  MAIN MENU
# ════════════════════════════════════════════════════════════════════════════
# ── Auto-detect LightBridge installation status ────────────────────────────
# Sets global vars: LB_INSTALLED, LB_VERSION, LB_RUNNING
LB_INSTALLED=false
LB_VERSION=""
LB_RUNNING=""

detect_lightbridge() {
  LB_INSTALLED=false
  LB_VERSION=""
  LB_RUNNING=""

  local bin=""
  # Check standard install path
  if [ -f "$INSTALL_DIR/LightBridge" ]; then
    bin="$INSTALL_DIR/LightBridge"
  # Check systemd service
  elif service_exists "$SERVICE_NAME"; then
    bin=$(get_service_exec "$SERVICE_NAME" 2>/dev/null || true)
  # Check legacy Sub2API paths that might be LightBridge
  elif [ -f "$LEGACY_INSTALL_DIR/LightBridge" ]; then
    bin="$LEGACY_INSTALL_DIR/LightBridge"
  fi

  if [ -n "$bin" ] && [ -f "$bin" ]; then
    LB_INSTALLED=true
    LB_VERSION=$("$bin" --version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "")
    if service_exists "$SERVICE_NAME"; then
      if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        LB_RUNNING="running"
      else
        LB_RUNNING="stopped"
      fi
    else
      LB_RUNNING=""
    fi
  fi
}

show_main_menu() {
  # Run detection every time menu is shown
  detect_lightbridge

  clear 2>/dev/null || true
  echo ""
  hr "$C_BRIGHT_CYAN"
  center "${C_GOLD}${C_BOLD}" "  ⚡ LightBridge ⚡"
  center "${C_BRIGHT_CYAN}" "  $(L banner_subtitle)"
  hr "$C_BRIGHT_CYAN"
  echo ""
  center "${C_DIM}" "$(L banner_welcome)"
  echo ""
  echo -e "  ${C_SKY}$(L banner_desc1)${C_RESET}"
  echo -e "  ${C_SKY}$(L banner_desc2)${C_RESET}"
  echo -e "  ${C_SKY}$(L banner_desc3)${C_RESET}"
  echo ""

  # Status badge
  if $LB_INSTALLED; then
    local status_color="${C_GREEN}"
    local status_text="$(L menu_status_running)"
    [ "$LB_RUNNING" = "stopped" ] && status_color="${C_YELLOW}" && status_text="$(L menu_status_stopped)"
    hr "$C_DIM"
    echo -e "  ${C_GREEN}●${C_RESET} $(L menu_lb_detected)  ${status_color}[${status_text}]${C_RESET}  ${C_DIM}${LB_VERSION}${C_RESET}"
    hr "$C_DIM"
  else
    hr "$C_DIM"
    echo -e "  ${C_DIM}● $(L menu_lb_not_detected)${C_RESET}"
    hr "$C_DIM"
  fi
  echo ""

  hr "$C_DIM"
  center "$C_BRIGHT_WHITE" "  $(L menu_title)"
  hr "$C_DIM"
  echo ""

  # Dynamic menu numbering
  local n=0

  if ! $LB_INSTALLED; then
    # ── Not installed: show install + docker + about ──
    n=$((n+1))
    echo -e "    ${C_BRIGHT_GREEN}${C_BOLD}${n}${C_RESET}  ┃  ${C_BOLD}$(L menu_1)${C_RESET}"
    echo -e "       ${C_DIM}┃  $(L menu_1_desc)${C_RESET}"
    echo -e "       ${C_MINT}┃  $(L menu_install_tip)${C_RESET}"
    echo ""
    n=$((n+1))
    echo -e "    ${C_BRIGHT_CYAN}${C_BOLD}${n}${C_RESET}  ┃  ${C_BOLD}$(L menu_2)${C_RESET}"
    echo -e "       ${C_DIM}┃  $(L menu_2_desc)${C_RESET}"
    echo -e "       ${C_MINT}┃  $(L menu_docker_tip)${C_RESET}"
    echo ""
    n=$((n+1))
    echo -e "    ${C_LAVENDER}${C_BOLD}${n}${C_RESET}  ┃  ${C_BOLD}$(L menu_6)${C_RESET}"
    echo -e "       ${C_DIM}┃  $(L menu_6_desc)${C_RESET}"
    echo ""

    # Store mapping for dispatch
    eval "MENU_KEY_1='install'"
    eval "MENU_KEY_2='docker'"
    eval "MENU_KEY_3='about'"
    MENU_MAX=3
  else
    # ── Installed: show upgrade + health + about + uninstall ──
    n=$((n+1))
    echo -e "    ${C_BRIGHT_MAGENTA}${C_BOLD}${n}${C_RESET}  ┃  ${C_BOLD}$(L menu_4)${C_RESET}"
    echo -e "       ${C_DIM}┃  $(L menu_4_desc)${C_RESET}"
    if [ -n "$LB_VERSION" ]; then
      echo -e "       ${C_MINT}┃  $(L menu_upgrade_tip) ${C_BOLD}${LB_VERSION}${C_RESET}"
    fi
    echo ""
    eval "MENU_KEY_${n}='upgrade'"

    n=$((n+1))
    echo -e "    ${C_SKY}${C_BOLD}${n}${C_RESET}  ┃  ${C_BOLD}$(L menu_5)${C_RESET}"
    echo -e "       ${C_DIM}┃  $(L menu_5_desc)${C_RESET}"
    echo ""
    eval "MENU_KEY_${n}='health'"

    n=$((n+1))
    echo -e "    ${C_LAVENDER}${C_BOLD}${n}${C_RESET}  ┃  ${C_BOLD}$(L menu_6)${C_RESET}"
    echo -e "       ${C_DIM}┃  $(L menu_6_desc)${C_RESET}"
    echo ""
    eval "MENU_KEY_${n}='about'"

    n=$((n+1))
    echo -e "    ${C_RED}${C_BOLD}${n}${C_RESET}  ┃  ${C_BOLD}$(L menu_7)${C_RESET}"
    echo -e "       ${C_DIM}┃  $(L menu_7_desc)${C_RESET}"
    echo ""
    eval "MENU_KEY_${n}='uninstall'"

    MENU_MAX=$n
  fi

  hr "$C_DIM"
  echo -e "    ${C_DIM}0${C_RESET}  ┃  ${C_DIM}$(L menu_0)${C_RESET}"
  hr "$C_DIM"
  echo ""

  if ! $LB_INSTALLED; then
    echo -e "  ${C_MINT}$(L menu_tip)${C_RESET}"
  fi
  echo ""
  printf "    ${C_GOLD}❯${C_RESET} ${C_BOLD}$(L menu_prompt):${C_RESET} "
}

# Dispatch menu choice to action
dispatch_menu() {
  local choice="$1"
  if [ "$choice" = "0" ] || [ "$choice" = "q" ] || [ "$choice" = "Q" ]; then
    clear 2>/dev/null || true
    echo ""
    center "$C_DIM" "Goodbye! ✨"
    echo ""
    exit 0
  fi

  # Validate range
  if ! [[ "$choice" =~ ^[0-9]+$ ]] || [ "$choice" -gt "$MENU_MAX" ] || [ "$choice" -lt 1 ]; then
    echo -e "  ${C_RED}无效选项${C_RESET}"
    sleep 1
    return
  fi

  local action
  eval "action=\${MENU_KEY_${choice}}"
  case "$action" in
    install)   do_fresh_install ;;
    docker)    do_docker_deploy ;;
    upgrade)   do_upgrade ;;
    health)    show_health_check ;;
    about)     show_about ;;
    uninstall) do_uninstall ;;
    *)         echo -e "  ${C_RED}未知操作${C_RESET}"; sleep 1 ;;
  esac
}

# ════════════════════════════════════════════════════════════════════════════
#  FRESH INSTALL
# ════════════════════════════════════════════════════════════════════════════
do_fresh_install() {
  check_root
  detect_platform

  # ── Step 1/7: 检测平台 ──
  step_header 1 7 "检测系统环境" "自动检测您的操作系统和 CPU 架构"
  sub_step "检测操作系统..."
  explain "uname -srm → $(uname -srm 2>/dev/null || echo 'unknown')"
  sub_done "检测完成" "${C_BOLD}${OS}_${ARCH}${C_RESET}"
  sub_step "检测 root 权限..."
  sub_done "uid=$(id -u)" "已获取 root 权限"
  confirm_go "环境检测通过，继续安装？"

  # ── Step 2/7: 获取版本 ──
  step_header 2 7 "获取安装版本" "从 GitHub 获取最新的 LightBridge 稳定版"
  get_latest_version
  explain "安装版本: ${C_BOLD}${LATEST_VERSION}${C_RESET}"
  prompt_version
  explain "最终安装版本: ${C_BOLD}${LATEST_VERSION}${C_RESET}"
  confirm_go "确认安装 ${LATEST_VERSION}？"

  # ── Step 3/7: 下载安装 ──
  step_header 3 7 "下载并安装" "从 GitHub Release 下载二进制文件"
  download_and_extract "$INSTALL_DIR/LightBridge"
  step_elapsed

  # ── Step 4/7: 配置参数 ──
  step_header 4 7 "配置参数" "设置服务器监听地址和端口"
  configure_server
  configure_module_release
  sub_done "配置完成"
  echo ""

  # ── Step 5/7: 安装服务 ──
  step_header 5 7 "安装系统服务" "创建用户、目录、systemd 服务"
  create_user
  step_divider
  setup_directories
  step_divider
  install_service
  step_elapsed

  # ── Step 6/7: 启动运行 ──
  step_header 6 7 "启动服务" "启动 LightBridge 并检测公网 IP"
  start_service || true
  enable_autostart
  get_public_ip
  step_elapsed

  # ── Step 7/7: 完成 ──
  step_header 7 7 "安装完成" "LightBridge 已成功安装并运行"
  print_completion
}

# ════════════════════════════════════════════════════════════════════════════
#  DOCKER DEPLOYMENT
# ════════════════════════════════════════════════════════════════════════════
do_docker_deploy() {
  # ── Step 1/5: 检测 Docker ──
  step_header 1 5 "检测 Docker 环境" "确认 Docker 已安装且正常运行"
  sub_step "检测 Docker 安装..."
  if ! cmd_exists docker; then
    sub_fail "Docker 未安装"
    echo ""
    explain "请先安装 Docker: https://docs.docker.com/get-docker/"
    explain "安装完成后重新运行此命令"
    if is_interactive; then printf "  ${C_DIM}$(L press_enter)${C_RESET} "; read_input; fi
    return
  fi
  local docker_ver
  docker_ver=$(docker --version 2>/dev/null | head -1 || echo "installed")
  sub_done "Docker 已安装" "${C_DIM}${docker_ver}${C_RESET}"

  sub_step "检测 Docker 守护进程..."
  if ! docker info &>/dev/null; then
    sub_fail "Docker 守护进程未运行"
    echo ""
    explain "请先启动 Docker: sudo systemctl start docker"
    if is_interactive; then printf "  ${C_DIM}$(L press_enter)${C_RESET} "; read_input; fi
    return
  fi
  sub_done "Docker 运行正常"
  confirm_go "Docker 环境就绪，继续部署？"

  # ── Step 2/5: 下载配置文件 ──
  step_header 2 5 "下载配置文件" "从 GitHub 下载 docker-compose.yml 和 .env 模板"
  local deploy_dir
  deploy_dir=$(pwd)

  sub_step "下载 docker-compose.yml..."
  explain "来源: github.com/${GITHUB_REPO}/deploy/docker-compose.local.yml"
  if cmd_exists curl; then
    curl -sSL "${DOCKER_RAW_URL}/docker-compose.local.yml" -o "$deploy_dir/docker-compose.yml"
  elif cmd_exists wget; then
    wget -q "${DOCKER_RAW_URL}/docker-compose.local.yml" -O "$deploy_dir/docker-compose.yml"
  fi
  sub_done "docker-compose.yml 已下载" "${C_DIM}${deploy_dir}/docker-compose.yml${C_RESET}"

  sub_step "下载 .env.example 模板..."
  curl -sSL "${DOCKER_RAW_URL}/.env.example" -o "$deploy_dir/.env.example" 2>/dev/null || true
  sub_done ".env.example 已下载"

  # ── Step 3/5: 生成密钥 ──
  step_header 3 5 "生成安全密钥" "自动随机生成数据库密码和 JWT 密钥"
  explain "使用 openssl 生成密码学安全的随机密钥"
  local JWT_SECRET TOTP_ENCRYPTION_KEY POSTGRES_PASSWORD
  sub_step "生成 POSTGRES_PASSWORD..."
  POSTGRES_PASSWORD=$(openssl rand -hex 32 2>/dev/null || head -c 64 /dev/urandom | sha256sum | cut -d' ' -f1)
  sub_done "POSTGRES_PASSWORD 已生成" "${C_DIM}${POSTGRES_PASSWORD:0:8}...${C_RESET}"

  sub_step "生成 JWT_SECRET..."
  JWT_SECRET=$(openssl rand -hex 32 2>/dev/null || head -c 64 /dev/urandom | sha256sum | cut -d' ' -f1)
  sub_done "JWT_SECRET 已生成" "${C_DIM}${JWT_SECRET:0:8}...${C_RESET}"

  sub_step "生成 TOTP_ENCRYPTION_KEY..."
  TOTP_ENCRYPTION_KEY=$(openssl rand -hex 32 2>/dev/null || head -c 64 /dev/urandom | sha256sum | cut -d' ' -f1)
  sub_done "TOTP_ENCRYPTION_KEY 已生成" "${C_DIM}${TOTP_ENCRYPTION_KEY:0:8}...${C_RESET}"

  step_divider
  sub_step "写入 .env 配置文件..."
  cp "$deploy_dir/.env.example" "$deploy_dir/.env"
  if sed --version >/dev/null 2>&1; then
    sed -i "s/^JWT_SECRET=.*/JWT_SECRET=${JWT_SECRET}/" "$deploy_dir/.env"
    sed -i "s/^TOTP_ENCRYPTION_KEY=.*/TOTP_ENCRYPTION_KEY=${TOTP_ENCRYPTION_KEY}/" "$deploy_dir/.env"
    sed -i "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=${POSTGRES_PASSWORD}/" "$deploy_dir/.env"
  else
    sed -i '' "s/^JWT_SECRET=.*/JWT_SECRET=${JWT_SECRET}/" "$deploy_dir/.env"
    sed -i '' "s/^TOTP_ENCRYPTION_KEY=.*/TOTP_ENCRYPTION_KEY=${TOTP_ENCRYPTION_KEY}/" "$deploy_dir/.env"
    sed -i '' "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=${POSTGRES_PASSWORD}/" "$deploy_dir/.env"
  fi
  chmod 600 "$deploy_dir/.env"
  sub_done ".env 文件已生成并加密" "权限已设置为 600（仅当前用户可读写）"

  # ── Step 4/5: 创建目录 ──
  step_header 4 5 "创建数据目录" "为 PostgreSQL、Redis 和 LightBridge 创建持久化存储目录"
  sub_step "创建 data/ postgres_data/ redis_data/..."
  mkdir -p "$deploy_dir/data" "$deploy_dir/postgres_data" "$deploy_dir/redis_data"
  sub_done "目录创建完成"
  explain "data/         — LightBridge 应用数据"
  explain "postgres_data/ — PostgreSQL 数据库文件"
  explain "redis_data/    — Redis 缓存数据"

  # ── Step 5/5: 完成 ──
  step_header 5 5 "部署完成" "所有配置文件已准备就绪"
  hr_double "$C_GREEN"
  center "$C_BRIGHT_GREEN" "  ✨  Docker 部署准备完成！  ✨"
  hr_double "$C_GREEN"
  echo ""
  echo -e "  ${C_BOLD}已生成的安全凭据:${C_RESET}"
  echo -e "    ${C_CYAN}POSTGRES_PASSWORD:${C_RESET}     ${C_BRIGHT_GREEN}${POSTGRES_PASSWORD}${C_RESET}"
  echo -e "    ${C_CYAN}JWT_SECRET:${C_RESET}            ${C_BRIGHT_GREEN}${JWT_SECRET}${C_RESET}"
  echo -e "    ${C_CYAN}TOTP_ENCRYPTION_KEY:${C_RESET}   ${C_BRIGHT_GREEN}${TOTP_ENCRYPTION_KEY}${C_RESET}"
  echo ""
  explain "凭据已保存到 .env 文件，请妥善保管，不要泄露！"
  echo ""
  hr "$C_BLUE"
  center "$C_BRIGHT_BLUE" "  🚀  接下来的操作"
  hr "$C_BLUE"
  echo ""
  echo -e "  ${C_BOLD}步骤 1:${C_RESET}  启动所有服务"
  echo -e "    ${C_CYAN}docker compose up -d${C_RESET}"
  echo ""
  echo -e "  ${C_BOLD}步骤 2:${C_RESET}  查看运行日志"
  echo -e "    ${C_CYAN}docker compose logs -f LightBridge${C_RESET}"
  echo ""
  echo -e "  ${C_BOLD}步骤 3:${C_RESET}  查看管理员密码"
  echo -e "    ${C_CYAN}docker compose logs LightBridge | grep 'admin password'${C_RESET}"
  echo ""
  echo -e "  ${C_BOLD}步骤 4:${C_RESET}  打开浏览器访问"
  echo -e "    ${C_BRIGHT_CYAN}http://localhost:8080${C_RESET}"
  echo ""
  hr "$C_GREEN"
  echo ""

  if is_interactive; then
    printf "  ${C_DIM}$(L press_enter)${C_RESET} "
    read_input
  fi
}

# ════════════════════════════════════════════════════════════════════════════
#  SUB2API MIGRATION
# ════════════════════════════════════════════════════════════════════════════
do_migration() {
  check_root

  clear 2>/dev/null || true
  echo ""
  hr "$C_YELLOW"
  center "$C_BRIGHT_YELLOW" "  🔄  $(L mig_title)  🔄"
  hr "$C_YELLOW"
  echo ""

  # Check LightBridge already installed
  if [ -f "$INSTALL_DIR/LightBridge" ] || service_exists "$SERVICE_NAME"; then
    echo -e "  ${C_YELLOW}⚠ $(L mig_lb_exists)${C_RESET}"
    echo -e "  ${C_DIM}Use option 4 (Upgrade) instead.${C_RESET}"
    echo ""
    if is_interactive; then
      printf "  ${C_DIM}$(L press_enter)${C_RESET} "
      read_input
    fi
    return
  fi

  # Detect Sub2API
  echo -e "  ${C_CYAN}⠋${C_RESET} $(L mig_detect)"

  local legacy_binary="" legacy_fragment="" legacy_user="$LEGACY_SERVICE_USER"

  if service_exists "$LEGACY_SERVICE_NAME"; then
    legacy_fragment=$(get_service_fragment "$LEGACY_SERVICE_NAME" 2>/dev/null || true)
    legacy_binary=$(get_service_exec "$LEGACY_SERVICE_NAME" 2>/dev/null || true)
    legacy_user=$(get_service_user "$LEGACY_SERVICE_NAME" 2>/dev/null || true)
    [ -z "$legacy_user" ] && legacy_user="$LEGACY_SERVICE_USER"
  fi

  if [ -z "$legacy_binary" ] || [ ! -f "$legacy_binary" ]; then
    for p in "$LEGACY_INSTALL_DIR/sub2api" "$LEGACY_INSTALL_DIR/LightBridge" /usr/local/bin/sub2api; do
      if [ -f "$p" ]; then legacy_binary="$p"; break; fi
    done
  fi

  if [ -z "$legacy_binary" ]; then
    echo -e "\r  ${C_RED}✗${C_RESET} $(L mig_not_found)          "
    echo ""
    if is_interactive; then
      printf "  ${C_DIM}$(L press_enter)${C_RESET} "
      read_input
    fi
    return
  fi

  echo -e "\r  ${C_GREEN}✓${C_RESET} $(L mig_found): ${C_DIM}${legacy_binary}${C_RESET}          "
  echo ""

  # Choose migration type
  echo -e "  ${C_BOLD}$(L mig_choice_prompt):${C_RESET}"
  echo ""
  echo -e "    ${C_GREEN}${C_BOLD}1${C_RESET}  ┃  ${C_BOLD}$(L mig_quick)${C_RESET}"
  echo -e "       ${C_DIM}┃  $(L mig_quick_desc)${C_RESET}"
  echo ""
  echo -e "    ${C_BRIGHT_YELLOW}${C_BOLD}2${C_RESET}  ┃  ${C_BOLD}$(L mig_full)${C_RESET}"
  echo -e "       ${C_DIM}┃  $(L mig_full_desc)${C_RESET}"
  echo ""
  hr "$C_DIM"
  echo -e "    ${C_DIM}0${C_RESET}  ┃  ${C_DIM}Cancel${C_RESET}"
  hr "$C_DIM"
  echo ""
  printf "    ${C_GOLD}❯${C_RESET} "
  local choice
  choice=""
  read_input choice
  [ -z "$choice" ] && choice="1"

  if [ "$choice" = "2" ]; then
    do_full_migration "$legacy_binary" "$legacy_fragment" "$legacy_user"
  elif [ "$choice" = "1" ]; then
    do_quick_migration "$legacy_binary" "$legacy_fragment" "$legacy_user"
  fi
}

do_quick_migration() {
  local legacy_binary="$1" legacy_fragment="$2" legacy_user="$3"

  echo ""
  local _steps=("Detect" "Version" "Download" "Backup" "Migrate" "Start" "Done")
  draw_steps 1 "${_steps[@]}"

  # Version
  draw_steps 2 "${_steps[@]}"
  get_latest_version
  prompt_version

  # Download
  draw_steps 3 "${_steps[@]}"
  download_and_extract "$INSTALL_DIR/LightBridge"

  # Stop legacy
  if systemctl is-active --quiet "$LEGACY_SERVICE_NAME" 2>/dev/null; then
    echo -e "  ${C_CYAN}⠋${C_RESET} Stopping ${LEGACY_SERVICE_NAME}..."
    systemctl stop "$LEGACY_SERVICE_NAME" 2>/dev/null || true
  fi

  # Backup
  draw_steps 4 "${_steps[@]}"
  local timestamp backup_dir
  timestamp=$(date +%Y%m%d-%H%M%S)
  backup_dir="$MIGRATION_BACKUP_ROOT/$timestamp"
  mkdir -p "$backup_dir"
  echo -e "  ${C_CYAN}⠋${C_RESET} $(L mig_backup) → ${C_DIM}${backup_dir}${C_RESET}"

  [ -e "$LEGACY_INSTALL_DIR" ] && cp -a "$LEGACY_INSTALL_DIR" "$backup_dir/opt-sub2api" 2>/dev/null || true
  [ -e "$LEGACY_CONFIG_DIR" ] && cp -a "$LEGACY_CONFIG_DIR" "$backup_dir/etc-sub2api" 2>/dev/null || true
  [ -n "$legacy_fragment" ] && [ -f "$legacy_fragment" ] && cp -a "$legacy_fragment" "$backup_dir/" 2>/dev/null || true
  echo -e "\r  ${C_GREEN}✓${C_RESET} $(L mig_backup)          "

  # Migrate files
  draw_steps 5 "${_steps[@]}"
  echo -e "  ${C_CYAN}⠋${C_RESET} $(L mig_copy)"
  mkdir -p "$INSTALL_DIR" "$CONFIG_DIR" "$INSTALL_DIR/data"
  [ -d "$LEGACY_CONFIG_DIR" ] && cp -a "$LEGACY_CONFIG_DIR"/. "$CONFIG_DIR"/ 2>/dev/null || true
  [ -d "$LEGACY_INSTALL_DIR/data" ] && cp -a "$LEGACY_INSTALL_DIR/data"/. "$INSTALL_DIR/data"/ 2>/dev/null || true
  [ -f "$LEGACY_INSTALL_DIR/config.yaml" ] && cp -a "$LEGACY_INSTALL_DIR/config.yaml" "$CONFIG_DIR/config.yaml" 2>/dev/null || true
  [ -f "$LEGACY_CONFIG_DIR/config.yaml" ] && cp -a "$LEGACY_CONFIG_DIR/config.yaml" "$CONFIG_DIR/config.yaml" 2>/dev/null || true
  echo -e "\r  ${C_GREEN}✓${C_RESET} $(L mig_copy)          "

  # Import server environment
  if service_exists "$LEGACY_SERVICE_NAME"; then
    local legacy_host legacy_port
    legacy_host=$(get_service_env "$LEGACY_SERVICE_NAME" "SERVER_HOST" 2>/dev/null || true)
    legacy_port=$(get_service_env "$LEGACY_SERVICE_NAME" "SERVER_PORT" 2>/dev/null || true)
    [ -n "$legacy_host" ] && SERVER_HOST="$legacy_host"
    [ -n "$legacy_port" ] && SERVER_PORT="$legacy_port"
  fi

  configure_server
  configure_module_release
  create_user
  setup_directories
  install_service

  # Disable legacy
  echo -e "  ${C_CYAN}⠋${C_RESET} $(L mig_disable)"
  if service_exists "$LEGACY_SERVICE_NAME"; then
    systemctl disable "$LEGACY_SERVICE_NAME" 2>/dev/null || true
    if [ -n "$legacy_fragment" ] && [ -f "$legacy_fragment" ] && [[ "$legacy_fragment" == /etc/systemd/system/* ]]; then
      mv "$legacy_fragment" "${legacy_fragment}.migrated-to-LightBridge" 2>/dev/null || true
    fi
    systemctl daemon-reload 2>/dev/null || true
  fi
  echo -e "\r  ${C_GREEN}✓${C_RESET} $(L mig_disable)          "

  # Start
  draw_steps 6 "${_steps[@]}"
  start_service || true
  enable_autostart
  get_public_ip

  # Done
  draw_steps 7 "${_steps[@]}"
  echo ""
  hr_double "$C_GREEN"
  center "$C_BRIGHT_GREEN" "  ✨  $(L mig_complete)  ✨"
  hr_double "$C_GREEN"
  echo ""
  echo -e "  ${C_BOLD}$(L mig_backup_dir):${C_RESET} ${C_DIM}${backup_dir}${C_RESET}"
  echo ""
  print_completion
}

do_full_migration() {
  local legacy_binary="$1" legacy_fragment="$2" legacy_user="$3"

  echo ""
  echo -e "  ${C_BRIGHT_YELLOW}  ⚠ Full Data Migration requires the sub2api-migrate tool.${C_RESET}"
  echo -e "  ${C_DIM}  This will invoke the dedicated migration script:${C_RESET}"
  echo -e "  ${C_DIM}    deploy/sub2api-full-migrate.sh${C_RESET}"
  echo ""

  if is_interactive; then
    printf "  ${C_BOLD}Proceed? [y/N]:${C_RESET} "
    confirm=""
    read_input confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
      echo -e "  ${C_DIM}Cancelled.${C_RESET}"
      return
    fi
  fi

  echo ""
  echo -e "  ${C_CYAN}The full migration script provides:${C_RESET}"
  echo -e "    ${C_GREEN}●${C_RESET} Automatic config.yaml & database DSN detection"
  echo -e "    ${C_GREEN}●${C_RESET} Full filesystem + PostgreSQL backup"
  echo -e "    ${C_GREEN}●${C_RESET} Proxy & account data migration"
  echo -e "    ${C_GREEN}●${C_RESET} OpenAI Provider module installation"
  echo -e "    ${C_GREEN}●${C_RESET} Claude/Gemini compatibility markers"
  echo -e "    ${C_GREEN}●${C_RESET} JSON migration report & rollback support"
  echo ""
  echo -e "  ${C_BOLD}Run the following command:${C_RESET}"
  echo -e "    ${C_CYAN}sudo ./sub2api-full-migrate.sh migrate${C_RESET}"
  echo ""
  echo -e "  ${C_DIM}For detailed options:${C_RESET}"
  echo -e "    ${C_CYAN}sudo ./sub2api-full-migrate.sh --help${C_RESET}"
  echo ""

  if is_interactive; then
    printf "  ${C_DIM}$(L press_enter)${C_RESET} "
    read_input
  fi
}

# ════════════════════════════════════════════════════════════════════════════
#  UPGRADE
# ════════════════════════════════════════════════════════════════════════════
do_upgrade() {
  check_root
  detect_platform

  # ── Step 1/5: 检测当前版本 ──
  step_header 1 5 "检测当前版本" "查找已安装的 LightBridge"
  local active_binary="" active_service="$SERVICE_NAME"
  if [ -f "$INSTALL_DIR/LightBridge" ]; then
    active_binary="$INSTALL_DIR/LightBridge"
  elif service_exists "$SERVICE_NAME"; then
    active_binary=$(get_service_exec "$SERVICE_NAME" 2>/dev/null || true)
  elif service_exists "$LEGACY_SERVICE_NAME"; then
    active_binary=$(get_service_exec "$LEGACY_SERVICE_NAME" 2>/dev/null || true)
    active_service="$LEGACY_SERVICE_NAME"
  fi

  if [ -z "$active_binary" ] || [ ! -f "$active_binary" ]; then
    sub_fail "未检测到 LightBridge"
    explain "请先使用全新安装功能"
    if is_interactive; then printf "  ${C_DIM}$(L press_enter)${C_RESET} "; read_input; fi
    return
  fi
  sub_done "找到 LightBridge" "${C_DIM}${active_binary}${C_RESET}"

  local current_ver
  current_ver=$("$active_binary" --version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "unknown")
  sub_done "当前版本" "${C_BOLD}${current_ver}${C_RESET}"

  # ── Step 2/5: 获取最新版本 ──
  step_header 2 5 "检查最新版本" "从 GitHub 获取最新的稳定版本"
  get_latest_version
  sub_done "最新版本" "${C_BOLD}${LATEST_VERSION}${C_RESET}"

  if [ "$current_ver" = "$LATEST_VERSION" ] || [ "$current_ver" = "${LATEST_VERSION#v}" ]; then
    sub_done "已是最新版本" "无需升级"
    explain "当前 ${current_ver} 与最新版 ${LATEST_VERSION} 相同"
    if is_interactive; then printf "  ${C_DIM}$(L press_enter)${C_RESET} "; read_input; fi
    return
  fi

  explain "将从 ${C_BOLD}${current_ver}${C_RESET} 升级到 ${C_BOLD}${LATEST_VERSION}${C_RESET}"
  prompt_version "$(L upg_ver_prompt)"
  confirm_go "确认升级到 ${LATEST_VERSION}？"

  # ── Step 3/5: 停止服务并备份 ──
  step_header 3 5 "停止服务并备份" "安全停止当前服务并备份二进制文件"
  sub_step "停止 LightBridge 服务..."
  if systemctl is-active --quiet "$active_service" 2>/dev/null; then
    systemctl stop "$active_service" 2>/dev/null || true
  fi
  sub_done "服务已停止"

  sub_step "备份当前二进制文件..."
  local backup_path="${active_binary}.backup.$(date +%Y%m%d%H%M%S)"
  cp "$active_binary" "$backup_path"
  sub_done "备份完成" "${C_DIM}${backup_path}${C_RESET}"
  explain "如升级失败可从备份恢复"

  # ── Step 4/5: 下载并安装新版本 ──
  step_header 4 5 "下载并安装新版本" "下载 ${LATEST_VERSION} 并替换当前版本"
  download_and_extract "$active_binary"
  configure_module_release
  if id "$SERVICE_USER" &>/dev/null; then
    chown "$SERVICE_USER:$SERVICE_USER" "$active_binary" 2>/dev/null || true
  fi

  # ── Step 5/5: 启动并验证 ──
  step_header 5 5 "启动并验证" "启动新版本并确认运行正常"
  sub_step "启动 LightBridge 服务..."
  systemctl start "$active_service" 2>/dev/null || true
  sub_done "服务已启动"

  local new_ver
  new_ver=$("$active_binary" --version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "unknown")
  sub_done "版本验证" "${C_BOLD}${new_ver}${C_RESET}"
  step_elapsed

  echo ""
  hr_double "$C_GREEN"
  center "$C_BRIGHT_GREEN" "  ✨  升级完成！  ✨"
  hr_double "$C_GREEN"
  echo ""
  echo -e "  ${C_BOLD}旧版本:${C_RESET}  ${C_DIM}${current_ver}${C_RESET}"
  echo -e "  ${C_BOLD}新版本:${C_RESET}  ${C_BRIGHT_GREEN}${new_ver}${C_RESET}"
  echo ""

  if is_interactive; then
    printf "  ${C_DIM}$(L press_enter)${C_RESET} "
    read_input
  fi
}

# ════════════════════════════════════════════════════════════════════════════
#  UNINSTALL
# ════════════════════════════════════════════════════════════════════════════
do_uninstall() {
  check_root

  clear 2>/dev/null || true
  echo ""
  hr "$C_RED"
  center "$C_BRIGHT_RED" "  🗑️  $(L uni_title)  🗑️"
  hr "$C_RED"
  echo ""
  echo -e "  ${C_YELLOW}$(L uni_confirm)${C_RESET}"
  echo ""

  if is_interactive; then
    printf "  ${C_BOLD}$(L uni_sure)${C_RESET} "
    confirm=""
    read_input confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
      echo -e "  ${C_DIM}$(L uni_cancelled)${C_RESET}"
      return
    fi
  fi

  echo -e "  ${C_CYAN}⠋${C_RESET} Stopping service..."
  systemctl stop LightBridge 2>/dev/null || true
  systemctl disable LightBridge 2>/dev/null || true
  echo -e "\r  ${C_GREEN}✓${C_RESET} Service stopped          "

  echo -e "  ${C_CYAN}⠋${C_RESET} $(L uni_removing)"
  rm -f /etc/systemd/system/LightBridge.service
  systemctl daemon-reload 2>/dev/null || true
  rm -rf "$INSTALL_DIR"
  userdel "$SERVICE_USER" 2>/dev/null || true
  echo -e "\r  ${C_GREEN}✓${C_RESET} $(L uni_removing)          "

  echo -e "  ${C_CYAN}⠋${C_RESET} Removing install lock..."
  rm -f "$CONFIG_DIR/.installed" 2>/dev/null || true
  rm -f "$INSTALL_DIR/.installed" 2>/dev/null || true
  echo -e "\r  ${C_GREEN}✓${C_RESET} Lock removed          "

  local remove_config=false
  if is_interactive; then
    printf "  ${C_BOLD}$(L uni_purge) [y/N]:${C_RESET} "
    purge_choice=""
    read_input purge_choice
    [[ "$purge_choice" =~ ^[Yy]$ ]] && remove_config=true
  fi

  if $remove_config; then
    echo -e "  ${C_CYAN}⠋${C_RESET} Removing config directory..."
    rm -rf "$CONFIG_DIR"
    echo -e "\r  ${C_GREEN}✓${C_RESET} Config removed          "
  else
    echo -e "  ${C_YELLOW}⚠${C_RESET} Config kept at: ${C_DIM}${CONFIG_DIR}${C_RESET}"
  fi

  echo ""
  hr_double "$C_GREEN"
  center "$C_BRIGHT_GREEN" "  ✨  $(L uni_done)  ✨"
  hr_double "$C_GREEN"
  echo ""

  if is_interactive; then
    printf "  ${C_DIM}$(L press_enter)${C_RESET} "
    read_input
  fi
}

# ════════════════════════════════════════════════════════════════════════════
#  CLI — 初始化（init）
# ════════════════════════════════════════════════════════════════════════════

do_init() {
  check_root

  local bin_target="/usr/local/bin/lightbridge"
  local script_source
  script_source="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
  local bash_completion_dir="/etc/bash_completion.d"
  local zsh_completion_dir="/usr/local/share/zsh/site-functions"

  echo ""
  hr "$C_CYAN"
  center "$C_BRIGHT_CYAN" "  ⚙  初始化 LightBridge CLI  ⚙"
  hr "$C_CYAN"
  echo ""

  # Step 1: Copy script to /usr/local/bin/
  echo -e "  ${C_CYAN}⠋${C_RESET} 安装 CLI 到 ${C_BOLD}${bin_target}${C_RESET}..."
  mkdir -p /usr/local/bin
  cp "$script_source" "$bin_target"
  chmod +x "$bin_target"
  echo -e "\r  ${C_GREEN}✓${C_RESET} CLI 已安装到 ${C_DIM}${bin_target}${C_RESET}            "

  # Step 2: Generate bash completion
  echo -e "  ${C_CYAN}⠋${C_RESET} 生成 bash 补全脚本..."
  mkdir -p "$bash_completion_dir"
  cat > "$bash_completion_dir/lightbridge" << 'BASH_COMP'
_lightbridge_completions() {
  local cur prev commands
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  commands="init install upgrade migrate docker health about versions uninstall help"

  if [[ ${cur} == -* ]]; then
    COMPREPLY=( $(compgen -W "-v --version -h --help" -- "${cur}") )
    return 0
  fi

  if [[ ${cur} == -* && ${prev} == "-v" ]]; then
    COMPREPLY=( $(compgen -W "v0.1.0 v0.2.0 v0.2.1 v0.2.2 v0.2.3 v0.2.4 v0.2.5 v0.2.6 v0.2.7 v0.2.8 v0.2.9 v0.2.10 v0.2.11 v0.2.12 v0.2.13" -- "${cur}") )
    return 0
  fi

  if [[ ${COMP_CWORD} -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
    return 0
  fi

  case "${prev}" in
    install|upgrade)
      COMPREPLY=( $(compgen -W "-v --version" -- "${cur}") )
      ;;
    help)
      COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
      ;;
  esac
  return 0
}
complete -F _lightbridge_completions lightbridge
BASH_COMP
  echo -e "\r  ${C_GREEN}✓${C_RESET} bash 补全已安装到 ${C_DIM}${bash_completion_dir}/lightbridge${C_RESET}   "

  # Step 3: Generate zsh completion
  echo -e "  ${C_CYAN}⠋${C_RESET} 生成 zsh 补全脚本..."
  mkdir -p "$zsh_completion_dir"
  cat > "$zsh_completion_dir/_lightbridge" << 'ZSH_COMP'
#compdef lightbridge

_lightbridge() {
  local -a commands
  commands=(
    'init:初始化 CLI，添加到 PATH 并配置 shell 补全'
    'install:全新安装 LightBridge（二进制 + systemd）'
    'upgrade:升级到最新或指定版本'
    'migrate:从 Sub2API 迁移数据'
    'docker:Docker 一键部署'
    'health:检测系统环境是否满足安装要求'
    'about:查看功能介绍与技术架构'
    'versions:列出所有可用版本'
    'uninstall:卸载 LightBridge'
    'help:显示帮助信息'
  )

  local -a versions=(
    'v0.1.0' 'v0.2.0' 'v0.2.1' 'v0.2.2' 'v0.2.3'
    'v0.2.4' 'v0.2.5' 'v0.2.6' 'v0.2.7' 'v0.2.8'
    'v0.2.9' 'v0.2.10' 'v0.2.11' 'v0.2.12' 'v0.2.13'
  )

  _arguments -C \
    '1:command:->cmd' \
    '*::arg:->args'

  case "$state" in
    cmd)
      _describe 'command' commands
      ;;
    args)
      case ${words[1]} in
        install|upgrade)
          _arguments \
            '-v[指定版本号]:版本:()' \
            '--version[指定版本号]:版本:()'
          ;;
        help)
          _describe 'command' commands
          ;;
      esac
      ;;
  esac
}

_lightbridge "$@"
ZSH_COMP
  echo -e "\r  ${C_GREEN}✓${C_RESET} zsh 补全已安装到 ${C_DIM}${zsh_completion_dir}/_lightbridge${C_RESET}   "

  # Step 4: Detect and configure shell RC
  echo -e "  ${C_CYAN}⠋${C_RESET} 检测 shell 配置..."

  local shell_name="${SHELL##*/}"
  local rc_file=""
  local reload_cmd=""

  case "$shell_name" in
    zsh)
      rc_file="$HOME/.zshrc"
      reload_cmd="source $rc_file"
      ;;
    bash)
      if [ -f "$HOME/.bashrc" ]; then
        rc_file="$HOME/.bashrc"
      elif [ -f "$HOME/.bash_profile" ]; then
        rc_file="$HOME/.bash_profile"
      fi
      reload_cmd="source $rc_file"
      ;;
  esac

  if [ -n "$rc_file" ] && [ -f "$rc_file" ]; then
    # Check if lightbridge init line already exists
    if ! grep -q "lightbridge" "$rc_file" 2>/dev/null; then
      {
        echo ""
        echo "# LightBridge CLI 初始化 (由 lightbridge init 自动生成)"
        echo "export PATH=\"/usr/local/bin:\$PATH\""
      } >> "$rc_file"
      echo -e "\r  ${C_GREEN}✓${C_RESET} 已写入 ${C_DIM}${rc_file}${C_RESET}              "
    else
      echo -e "\r  ${C_GREEN}✓${C_RESET} ${C_DIM}${rc_file}${C_RESET} 已包含 LightBridge 配置      "
    fi
  else
    echo -e "\r  ${C_YELLOW}⚠${C_RESET} 未检测到 shell 配置文件，跳过          "
  fi

  # Step 5: Verify
  echo ""
  echo -e "  ${C_CYAN}⠋${C_RESET} 验证安装..."
  if [ -x "$bin_target" ]; then
    local installed_ver
    installed_ver=$("$bin_target" --version 2>/dev/null || echo "unknown")
    echo -e "\r  ${C_GREEN}✓${C_RESET} 验证通过: ${C_BOLD}${installed_ver}${C_RESET}          "
  else
    echo -e "\r  ${C_RED}✗${C_RESET} 验证失败，请检查 ${bin_target}${C_RESET}          "
  fi

  # Done
  echo ""
  hr_double "$C_GREEN"
  center "$C_BRIGHT_GREEN" "  ✨  CLI 初始化完成！  ✨"
  hr_double "$C_GREEN"
  echo ""
  echo -e "  ${C_BOLD}安装位置:${C_RESET}  ${C_CYAN}${bin_target}${C_RESET}"
  echo -e "  ${C_BOLD}Shell 补全:${C_RESET} bash + zsh 已配置"
  echo ""
  echo -e "  ${C_YELLOW}请执行以下命令使配置生效:${C_RESET}"
  if [ -n "$reload_cmd" ]; then
    echo -e "    ${C_CYAN}${reload_cmd}${C_RESET}"
  fi
  echo ""
  echo -e "  ${C_BOLD}现在可以直接使用:${C_RESET}"
  echo -e "    ${C_CYAN}lightbridge${C_RESET}                     ${C_DIM}# 交互式安装向导${C_RESET}"
  echo -e "    ${C_CYAN}lightbridge install${C_RESET}               ${C_DIM}# 全新安装${C_RESET}"
  echo -e "    ${C_CYAN}lightbridge help install${C_RESET}           ${C_DIM}# 查看子命令帮助${C_RESET}"
  echo -e "    ${C_CYAN}lightbridge <Tab><Tab>${C_RESET}              ${C_DIM}# Tab 补全${C_RESET}"
  echo ""
  hr "$C_DIM"
  echo ""
}

# ════════════════════════════════════════════════════════════════════════════
#  CLI — 帮助与版本
# ════════════════════════════════════════════════════════════════════════════

VERSION="2.0.0"

print_banner() {
  echo ""
  hr "$C_BRIGHT_CYAN"
  center "${C_GOLD}${C_BOLD}" "  ⚡ LightBridge ⚡"
  center "${C_BRIGHT_CYAN}" "  AI API 网关平台 · 一键部署"
  hr "$C_BRIGHT_CYAN"
  echo ""
}

show_help() {
  local bin="${0##*/}"
  print_banner
  center "$C_BRIGHT_WHITE" "  LightBridge 安装管理工具 v${VERSION}"
  echo ""
  echo -e "  ${C_DIM}一个命令搞定 LightBridge 的安装、升级、迁移和管理。${C_RESET}"
  echo ""
  hr "$C_DIM"
  echo -e "  ${C_BOLD}用法:${C_RESET}"
  echo ""
  echo -e "    ${C_CYAN}${bin}${C_RESET}                        ${C_DIM}进入交互式安装向导（推荐新手）${C_RESET}"
  echo -e "    ${C_CYAN}${bin} <子命令>${C_RESET}               ${C_DIM}执行指定操作${C_RESET}"
  echo ""
  hr "$C_DIM"
  echo -e "  ${C_BOLD}子命令:${C_RESET}"
  echo ""
  echo -e "    ${C_GREEN}init${C_RESET}       ${C_DIM}—${C_RESET} 初始化 CLI，添加到 PATH 并配置 shell 补全"
  echo -e "    ${C_GREEN}install${C_RESET}    ${C_DIM}—${C_RESET} 全新安装 LightBridge（二进制 + systemd）"
  echo -e "    ${C_GREEN}docker${C_RESET}     ${C_DIM}—${C_RESET} Docker 一键部署（推荐新手）"
  echo -e "    ${C_GREEN}upgrade${C_RESET}    ${C_DIM}—${C_RESET} 升级到最新或指定版本"
  echo -e "    ${C_GREEN}migrate${C_RESET}    ${C_DIM}—${C_RESET} 从 Sub2API 迁移数据"
  echo -e "    ${C_GREEN}health${C_RESET}     ${C_DIM}—${C_RESET} 检测系统环境是否满足安装要求"
  echo -e "    ${C_GREEN}about${C_RESET}      ${C_DIM}—${C_RESET} 查看功能介绍与技术架构"
  echo -e "    ${C_GREEN}versions${C_RESET}   ${C_DIM}—${C_RESET} 列出所有可用版本"
  echo -e "    ${C_GREEN}uninstall${C_RESET}  ${C_DIM}—${C_RESET} 卸载 LightBridge"
  echo -e "    ${C_GREEN}help${C_RESET}       ${C_DIM}—${C_RESET} 显示本帮助信息"
  echo ""
  hr "$C_DIM"
  echo -e "  ${C_BOLD}选项:${C_RESET}"
  echo ""
  echo -e "    ${C_YELLOW}-v, --version <ver>${C_RESET}   ${C_DIM}指定版本号（例如: v0.2.3）${C_RESET}"
  echo -e "    ${C_YELLOW}-h, --help${C_RESET}            ${C_DIM}显示帮助信息${C_RESET}"
  echo -e "    ${C_YELLOW}--version${C_RESET}             ${C_DIM}显示当前版本号${C_RESET}"
  echo ""
  hr "$C_DIM"
  echo -e "  ${C_BOLD}示例:${C_RESET}"
  echo ""
  echo -e "    ${C_CYAN}${bin}${C_RESET}                          ${C_DIM}# 交互式安装向导${C_RESET}"
  echo -e "    ${C_CYAN}${bin} install${C_RESET}                    ${C_DIM}# 全新安装最新版${C_RESET}"
  echo -e "    ${C_CYAN}${bin} install -v v0.2.3${C_RESET}           ${C_DIM}# 安装指定版本${C_RESET}"
  echo -e "    ${C_CYAN}${bin} upgrade${C_RESET}                    ${C_DIM}# 升级到最新版${C_RESET}"
  echo -e "    ${C_CYAN}${bin} upgrade -v v0.2.3${C_RESET}           ${C_DIM}# 升级到指定版本${C_RESET}"
  echo -e "    ${C_CYAN}${bin} docker${C_RESET}                     ${C_DIM}# Docker 部署${C_RESET}"
  echo -e "    ${C_CYAN}${bin} migrate${C_RESET}                    ${C_DIM}# Sub2API 迁移${C_RESET}"
  echo -e "    ${C_CYAN}${bin} health${C_RESET}                     ${C_DIM}# 系统健康检查${C_RESET}"
  echo -e "    ${C_CYAN}${bin} versions${C_RESET}                   ${C_DIM}# 查看可用版本${C_RESET}"
  echo ""
  hr "$C_DIM"
  echo -e "  ${C_DIM}文档: https://github.com/WilliamWang1721/LightBridge${C_RESET}"
  echo -e "  ${C_DIM}问题反馈: https://github.com/WilliamWang1721/LightBridge/issues${C_RESET}"
  echo ""
}

show_command_help() {
  local cmd="$1"
  local bin="${0##*/}"
  echo ""
  case "$cmd" in
    init)
      echo -e "  ${C_BOLD}用法:${C_RESET}  ${C_CYAN}${bin} init${C_RESET}"
      echo ""
      echo -e "  ${C_BOLD}说明:${C_RESET}  初始化 CLI 环境，自动完成以下操作："
      echo -e "          • 将脚本复制到 /usr/local/bin/lightbridge"
      echo -e "          • 生成 bash / zsh / fish shell 补全脚本"
      echo -e "          • 写入对应的 shell 配置文件（~/.bashrc / ~/.zshrc）"
      echo -e "          • 设置可执行权限"
      echo ""
      echo -e "  ${C_BOLD}前置条件:${C_RESET}"
      echo -e "    • root 权限（sudo）"
      echo -e "    • 脚本所在目录可读"
      echo ""
      echo -e "  ${C_BOLD}示例:${C_RESET}"
      echo -e "    ${C_CYAN}sudo ${bin} init${C_RESET}                    ${C_DIM}# 初始化 CLI${C_RESET}"
      echo ""
      echo -e "  ${C_BOLD}初始化后:${C_RESET}"
      echo -e "    ${C_GREEN}lightbridge${C_RESET}                   ${C_DIM}# 直接使用，无需路径${C_RESET}"
      echo -e "    ${C_GREEN}lightbridge install${C_RESET}             ${C_DIM}# Tab 补全子命令${C_RESET}"
      echo -e "    ${C_GREEN}lightbridge install -v${C_RESET} <Tab>    ${C_DIM}# 补全版本号${C_RESET}"
      ;;
    install)
      echo -e "  ${C_BOLD}用法:${C_RESET}  ${C_CYAN}${bin} install${C_RESET} [选项]"
      echo ""
      echo -e "  ${C_BOLD}说明:${C_RESET}  全新安装 LightBridge，包括下载二进制、创建系统用户、"
      echo -e "          安装 systemd 服务、设置开机自启。安装完成后会自动启动"
      echo -e "          Web 设置向导，引导您完成数据库和管理员配置。"
      echo ""
      echo -e "  ${C_BOLD}选项:${C_RESET}"
      echo -e "    ${C_YELLOW}-v, --version <ver>${C_RESET}   指定安装版本（默认: 最新稳定版）"
      echo ""
      echo -e "  ${C_BOLD}前提条件:${C_RESET}"
      echo -e "    • Linux 服务器（Ubuntu 20.04+ / Debian 11+ / CentOS 8+）"
      echo -e "    • root 权限（sudo）"
      echo -e "    • PostgreSQL 14+ 和 Redis 6+（需要提前安装）"
      echo ""
      echo -e "  ${C_BOLD}示例:${C_RESET}"
      echo -e "    ${C_CYAN}sudo ${bin} install${C_RESET}                 ${C_DIM}# 安装最新版${C_RESET}"
      echo -e "    ${C_CYAN}sudo ${bin} install -v v0.2.3${C_RESET}        ${C_DIM}# 安装指定版本${C_RESET}"
      ;;
    upgrade)
      echo -e "  ${C_BOLD}用法:${C_RESET}  ${C_CYAN}${bin} upgrade${C_RESET} [选项]"
      echo ""
      echo -e "  ${C_BOLD}说明:${C_RESET}  将现有 LightBridge 升级到最新或指定版本。"
      echo -e "          升级前会自动备份当前二进制文件。"
      echo ""
      echo -e "  ${C_BOLD}选项:${C_RESET}"
      echo -e "    ${C_YELLOW}-v, --version <ver>${C_RESET}   目标版本（默认: 最新稳定版）"
      ;;
    migrate)
      echo -e "  ${C_BOLD}用法:${C_RESET}  ${C_CYAN}${bin} migrate${C_RESET} [选项]"
      echo ""
      echo -e "  ${C_BOLD}说明:${C_RESET}  将现有的 Sub2API 部署迁移至 LightBridge。"
      echo -e "          自动检测 Sub2API 安装位置，备份配置和数据，"
      echo -e "          复制运行文件，安装 LightBridge 服务。"
      ;;
    docker)
      echo -e "  ${C_BOLD}用法:${C_RESET}  ${C_CYAN}${bin} docker${C_RESET}"
      echo ""
      echo -e "  ${C_BOLD}说明:${C_RESET}  使用 Docker Compose 一键部署 LightBridge 全套服务"
      echo -e "          （包含 PostgreSQL、Redis、LightBridge）。自动下载配置文件、"
      echo -e "          生成安全密钥、创建数据目录。"
      ;;
    health)
      echo -e "  ${C_BOLD}用法:${C_RESET}  ${C_CYAN}${bin} health${C_RESET}"
      echo ""
      echo -e "  ${C_BOLD}说明:${C_RESET}  检测系统环境是否满足安装要求，包括："
      echo -e "          • 系统信息（操作系统、架构、内存、磁盘）"
      echo -e "          • 前置条件（root 权限、systemd、curl、tar）"
      echo -e "          • 服务状态（PostgreSQL、Redis、Docker）"
      echo -e "          • LightBridge 安装状态"
      ;;
    about)
      echo -e "  ${C_BOLD}用法:${C_RESET}  ${C_CYAN}${bin} about${C_RESET}"
      echo ""
      echo -e "  ${C_BOLD}说明:${C_RESET}  查看 LightBridge 的功能介绍、兼容协议和技术架构。"
      ;;
    versions)
      echo -e "  ${C_BOLD}用法:${C_RESET}  ${C_CYAN}${bin} versions${C_RESET}"
      echo ""
      echo -e "  ${C_BOLD}说明:${C_RESET}  列出 GitHub 上所有可用的 LightBridge 版本。"
      ;;
    uninstall)
      echo -e "  ${C_BOLD}用法:${C_RESET}  ${C_CYAN}${bin} uninstall${C_RESET}"
      echo ""
      echo -e "  ${C_BOLD}说明:${C_RESET}  从系统中移除 LightBridge，包括停止服务、删除二进制文件、"
      echo -e "          移除 systemd 配置。可选择是否同时删除配置目录。"
      ;;
    *)
      echo -e "  未知命令: ${C_RED}${cmd}${C_RESET}"
      echo -e "  运行 ${C_CYAN}${bin} help${C_RESET} 查看所有可用命令。"
      ;;
  esac
  echo ""
}

# ════════════════════════════════════════════════════════════════════════════
#  CLI — 参数解析
# ════════════════════════════════════════════════════════════════════════════

# Parse global flags: --version (display), --help, -v VERSION
parse_global_flags() {
  TARGET_VERSION=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -v|--version)
        if [ -n "${2:-}" ] && [[ ! "$2" =~ ^- ]]; then
          TARGET_VERSION="$2"; shift 2
        else
          # bare --version = show own version
          echo "lightbridge v${VERSION}"
          exit 0
        fi
        ;;
      --help|-h)
        # Defer to subcommand help or main help
        if [ -n "${SUBCMD:-}" ]; then
          show_command_help "$SUBCMD"
        else
          show_help
        fi
        exit 0
        ;;
      *)
        # Unknown flag — stop parsing
        break
        ;;
    esac
  done
  return 0
}

# ════════════════════════════════════════════════════════════════════════════
#  CLI — 主入口
# ════════════════════════════════════════════════════════════════════════════

# No arguments → interactive TUI
if [ $# -eq 0 ]; then
  LANG_CHOICE="zh"
  if is_interactive; then
    select_language
  fi
  while true; do
    show_main_menu
    choice=""
    read_input choice
    [ -z "$choice" ] && choice="0"
    dispatch_menu "$choice"
  done
fi

# Subcommand dispatch
SUBCMD="${1:-}"; shift 2>/dev/null || true

case "$SUBCMD" in
  --help|-h|help)
    if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
      show_help
    elif [ -n "${1:-}" ]; then
      show_command_help "$1"
    else
      show_help
    fi
    exit 0
    ;;
  --version|-V)
    echo "lightbridge v${VERSION}"
    exit 0
    ;;
  init)
    parse_global_flags "$@"
    do_init
    ;;
  install)
    LANG_CHOICE="zh"
    if is_interactive; then select_language; fi
    parse_global_flags "$@"
    check_root
    detect_platform 2>/dev/null || true
    if [ -n "$TARGET_VERSION" ]; then
      LATEST_VERSION="$TARGET_VERSION"
      [[ "$LATEST_VERSION" == v* ]] || LATEST_VERSION="v$LATEST_VERSION"
    fi
    do_fresh_install
    ;;
  upgrade)
    LANG_CHOICE="zh"
    if is_interactive; then select_language; fi
    parse_global_flags "$@"
    check_root
    detect_platform 2>/dev/null || true
    if [ -n "$TARGET_VERSION" ]; then
      LATEST_VERSION="$TARGET_VERSION"
      [[ "$LATEST_VERSION" == v* ]] || LATEST_VERSION="v$LATEST_VERSION"
    fi
    do_upgrade
    ;;
  migrate)
    LANG_CHOICE="zh"
    if is_interactive; then select_language; fi
    parse_global_flags "$@"
    check_root
    detect_platform 2>/dev/null || true
    do_migration
    ;;
  docker)
    LANG_CHOICE="zh"
    if is_interactive; then select_language; fi
    parse_global_flags "$@"
    detect_platform 2>/dev/null || true
    do_docker_deploy
    ;;
  health)
    LANG_CHOICE="zh"
    if is_interactive; then select_language; fi
    parse_global_flags "$@"
    detect_platform 2>/dev/null || true
    show_health_check
    ;;
  about)
    LANG_CHOICE="zh"
    if is_interactive; then select_language; fi
    parse_global_flags "$@"
    show_about
    ;;
  versions)
    list_versions
    ;;
  uninstall)
    LANG_CHOICE="zh"
    if is_interactive; then select_language; fi
    parse_global_flags "$@"
    check_root
    do_uninstall
    ;;
  *)
    echo -e "${C_RED}未知命令: ${SUBCMD}${C_RESET}" >&2
    echo -e "运行 ${C_CYAN}${0##*/} help${C_RESET} 查看帮助。" >&2
    exit 1
    ;;
esac
