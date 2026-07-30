#!/usr/bin/env bash

set -Eeuo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_DIR="${MM_INSTALL_DIR:-$HOME/.local/bin}"
CONFIG_DIR="${CONFIG_DIR:-$HOME/.config/mihomo}"
MIHOMO_BIN="${MIHOMO_BIN:-$INSTALL_DIR/mihomo}"
STATE_DIR="${MM_STATE_DIR:-$HOME/.local/share/mihomo-manager}"
STATE_FILE="$STATE_DIR/install-state"
OS_RELEASE_FILE="${MM_OS_RELEASE_FILE:-/etc/os-release}"

GO_VERSION="1.26.4"
MIN_GO_MAJOR=1
MIN_GO_MINOR=22
DEFAULT_MIHOMO_VERSION="v1.19.28"
DEFAULT_RULESET_BASE_URL="https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat"
DEFAULT_RULESET_REF="32ae0e8658ca541374b721efcee84955e8a59755"
DEFAULT_RULESET_DOMAIN_SHA256="52c146262ef51dc23a84533a0d13f8addd031c61708a863d17cdb75cc3089ee4"
DEFAULT_RULESET_IP_SHA256="206ad4cc22005976e8bfb50a869e5483cb81cc174a56c9a79c8a13e3e64e2eea"
RULESET_CATALOG_FILE="$PROJECT_DIR/internal/ruleset/catalog.json"
RULESET_DOMAIN_SOURCE_PATH="geo/geosite/cn.mrs"
RULESET_IP_SOURCE_PATH="geo/geoip/cn.mrs"

ASSUME_YES=0
FORCE_CORE=0
INSTALL_RULESETS=0
TARGET_OS=""
TARGET_ARCH=""
GO_BIN=""
MM_BUILD_PATH=""
CORE_MANAGED=0
CORE_VERSION=""
PATH_RC_MODIFIED=""
CONFIG_WARNING=0
CONFIG_CREATED=0
RULESET_DIR="$CONFIG_DIR/rulesets"
RULESET_DOMAIN_PATH="$RULESET_DIR/cn-domain.mrs"
RULESET_IP_PATH="$RULESET_DIR/cn-ip.mrs"
RULESET_INSTALLED_BASE_URL=""
RULESET_INSTALLED_REF=""
RULESET_INSTALLED_DOMAIN_SHA256=""
RULESET_INSTALLED_IP_SHA256=""
RULESET_REQUESTED_BASE_URL=""
RULESET_REQUESTED_REF=""
RULESET_REQUESTED_DOMAIN_SHA256=""
RULESET_REQUESTED_IP_SHA256=""
DAEMON_CORE_RUNNING=0
DAEMON_AVAILABLE=0
DAEMON_PREEXISTING=0
DAEMON_PREVIOUS_CORE_STATE=""
DAEMON_BACKEND=""
DAEMON_CONFIG_PATH=""
DAEMON_CONFIG_SHA256=""

TEMP_PATHS=()

if [[ -t 1 ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    NC=''
fi

info() {
    printf '%b[信息]%b %s\n' "$BLUE" "$NC" "$*"
}

success() {
    printf '%b[完成]%b %s\n' "$GREEN" "$NC" "$*"
}

warn() {
    printf '%b[警告]%b %s\n' "$YELLOW" "$NC" "$*" >&2
}

die() {
    printf '%b[错误]%b %s\n' "$RED" "$NC" "$*" >&2
    exit 1
}

cleanup() {
    local path
    for path in "${TEMP_PATHS[@]:-}"; do
        [[ -n "$path" ]] && rm -rf -- "$path"
    done
    return 0
}

trap cleanup EXIT

usage() {
    cat <<'EOF'
Mihomo Manager 安装器

用法：
  ./scripts/install.sh [选项]

选项：
  --yes          跳过 apt/Homebrew 安装确认
  --force-core   强制重新安装目标 mihomo core 版本
  --with-rulesets  同时下载并安装 CN 规则集（默认跳过网络下载）
  -h, --help     显示帮助

环境变量：
  MM_ASSUME_YES=1             等同 --yes
  MM_INSTALL_RULESETS=1       同时安装 CN 规则集
  MIHOMO_VERSION=v1.19.28     指定 mihomo core 版本
  MM_GITHUB_BASE_URL=...      覆盖 GitHub 下载基地址
  MM_GITHUB_API_BASE_URL=...  覆盖 GitHub API 基地址
  MM_GO_DOWNLOAD_BASE_URL=... 覆盖 Go 下载基地址
  MM_RULESET_BASE_URL=...       覆盖 CN 规则集下载基地址
  MM_RULESET_REF=...            覆盖 CN 规则集仓库引用
  MM_RULESET_DOMAIN_SHA256=...  自定义来源的 domain 规则集摘要
  MM_RULESET_IP_SHA256=...      自定义来源的 IP 规则集摘要
EOF
}

parse_args() {
    case "${MM_ASSUME_YES:-0}" in
        1|true|TRUE|yes|YES) ASSUME_YES=1 ;;
    esac
    case "${MM_INSTALL_RULESETS:-0}" in
        1|true|TRUE|yes|YES) INSTALL_RULESETS=1 ;;
    esac

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --yes)
                ASSUME_YES=1
                ;;
            --force-core)
                FORCE_CORE=1
                ;;
            --with-rulesets)
                INSTALL_RULESETS=1
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                die "未知参数: $1"
                ;;
        esac
        shift
    done
}

os_release_value() {
    local key="$1"
    awk -F= -v wanted="$key" '
        $1 == wanted {
            value = substr($0, index($0, "=") + 1)
            gsub(/^"|"$/, "", value)
            print value
            exit
        }
    ' "$OS_RELEASE_FILE"
}

linux_platform_supported() {
    [[ -r "$OS_RELEASE_FILE" ]] || return 1

    local id id_like
    id="$(os_release_value ID)"
    id_like="$(os_release_value ID_LIKE)"

    [[ "$id" == "ubuntu" || "$id" == "debian" || " $id_like " == *" debian "* ]]
}

normalize_arch() {
    local raw="$1"
    case "$raw" in
        amd64|x86_64)
            printf 'amd64\n'
            ;;
        arm64|aarch64)
            printf 'arm64\n'
            ;;
        *)
            return 1
            ;;
    esac
}

detect_arch() {
    local raw="${MM_ARCH:-}"
    if [[ -z "$raw" && "$TARGET_OS" == "linux" ]] && command -v dpkg >/dev/null 2>&1; then
        raw="$(dpkg --print-architecture 2>/dev/null || true)"
    fi
    if [[ -z "$raw" ]]; then
        raw="$(uname -m)"
    fi
    normalize_arch "$raw"
}

detect_os() {
    local raw="${MM_OS_OVERRIDE:-}"
    [[ -n "$raw" ]] || raw="$(uname -s)"
    case "$raw" in
        Linux|linux) printf 'linux\n' ;;
        Darwin|darwin) printf 'darwin\n' ;;
        *) return 1 ;;
    esac
}

macos_supported() {
    local version="${MM_MACOS_VERSION_OVERRIDE:-}" major
    [[ -n "$version" ]] || version="$(sw_vers -productVersion 2>/dev/null || true)"
    [[ "$version" =~ ^[0-9]+([.][0-9]+){0,2}$ ]] || return 1
    major="${version%%.*}"
    ((major >= 12))
}

preflight() {
    local effective_uid="${MM_EUID_OVERRIDE:-$EUID}"
    [[ "$effective_uid" != "0" ]] || die "请以普通用户运行安装器，不要使用 sudo 执行整个脚本。"
    TARGET_OS="$(detect_os)" || die "仅支持 Ubuntu/Debian 和 macOS 12 及以上版本，未执行任何安装变更。"
    case "$TARGET_OS" in
        linux)
            linux_platform_supported || die "Linux 一键安装仅支持 Ubuntu/Debian，未执行任何安装变更。"
            command -v apt-get >/dev/null 2>&1 || die "未找到 apt-get，Ubuntu/Debian 安装需要 apt。"
            ;;
        darwin)
            macos_supported || die "macOS 一键安装要求 macOS 12 Monterey 或更高版本，未执行任何安装变更。"
            ;;
    esac

    TARGET_ARCH="$(detect_arch)" || die "不支持当前 CPU 架构，仅支持 amd64 和 arm64。"
    info "检测到受支持环境: $TARGET_OS/$TARGET_ARCH"
}

package_installed() {
    dpkg-query -W -f='${Status}' "$1" 2>/dev/null | grep -q '^install ok installed$'
}

confirm_apt_install() {
    local packages="$1"
    ((ASSUME_YES == 1)) && return 0

    printf '将通过 sudo apt-get 安装缺失包: %s\n' "$packages" >&2
    local answer=""
    if ! read -r -p "是否继续？[y/N] " answer </dev/tty; then
        die "当前环境无法交互确认。请使用 --yes 或 MM_ASSUME_YES=1。"
    fi
    case "$answer" in
        y|Y|yes|YES) ;;
        *) die "用户取消安装。" ;;
    esac
}

confirm_brew_install() {
    local packages="$1"
    ((ASSUME_YES == 1)) && return 0

    printf '将通过 Homebrew 安装缺失包: %s\n' "$packages" >&2
    local answer=""
    if ! read -r -p "是否继续？[y/N] " answer </dev/tty; then
        die "当前环境无法交互确认。请使用 --yes 或 MM_ASSUME_YES=1。"
    fi
    case "$answer" in
        y|Y|yes|YES) ;;
        *) die "用户取消安装。" ;;
    esac
}

install_linux_dependencies() {
    local required=(ca-certificates curl tar gzip procps jq coreutils)
    local missing=()
    local package

    command -v dpkg-query >/dev/null 2>&1 || die "未找到 dpkg-query，无法检查系统依赖。"
    for package in "${required[@]}"; do
        package_installed "$package" || missing+=("$package")
    done

    if ((${#missing[@]} == 0)); then
        info "系统依赖已满足，无需 sudo。"
        return 0
    fi

    command -v sudo >/dev/null 2>&1 || die "缺少系统包 ${missing[*]}，且当前系统没有 sudo。"
    confirm_apt_install "${missing[*]}"

    info "更新 apt 软件包索引..."
    sudo apt-get update
    info "安装系统依赖: ${missing[*]}"
    sudo apt-get install -y "${missing[@]}"
}

install_macos_dependencies() {
    local required=(curl tar gzip jq shasum awk sed grep head mktemp mv cp chmod mkdir dirname basename cat touch rm tr)
    local command_name missing=() packages=()
    for command_name in "${required[@]}"; do
        command -v "$command_name" >/dev/null 2>&1 || missing+=("$command_name")
    done
    ((${#missing[@]} > 0)) || {
        info "macOS 系统依赖已满足，无需 Homebrew 操作。"
        return 0
    }

    for command_name in "${missing[@]}"; do
        case "$command_name" in
            jq) packages+=(jq) ;;
            curl) packages+=(curl) ;;
            gzip) packages+=(gzip) ;;
            tar) packages+=(gnu-tar) ;;
            *) die "macOS 缺少系统命令 ${command_name}，安装器不会自动替换系统基础工具。" ;;
        esac
    done
    command -v brew >/dev/null 2>&1 || die "缺少 ${missing[*]} 且未安装 Homebrew；请先安装 Homebrew 后重试。"
    confirm_brew_install "${packages[*]}"
    brew install "${packages[@]}"
}

install_system_dependencies() {
    case "$TARGET_OS" in
        linux) install_linux_dependencies ;;
        darwin) install_macos_dependencies ;;
        *) die "内部目标平台无效: $TARGET_OS" ;;
    esac
}

verify_required_commands() {
    local required=(curl tar gzip jq awk sed grep head mktemp mv cp chmod mkdir dirname basename cat touch rm tr)
    local command_name missing=()
	if [[ "$TARGET_OS" == "linux" ]]; then
		required+=(sha256sum pgrep pkill)
	else
		required+=(shasum)
	fi
    for command_name in "${required[@]}"; do
        command -v "$command_name" >/dev/null 2>&1 || missing+=("$command_name")
    done
    ((${#missing[@]} == 0)) || die "系统依赖安装后仍缺少命令: ${missing[*]}"
}

download_file() {
    local url="$1"
    local output="$2"

    if ! curl --fail --location --silent --show-error \
        --retry 3 --connect-timeout 15 \
        "$url" --output "$output"; then
        warn "下载失败，请检查下载源和网络连接。"
        warn "可设置 HTTP_PROXY/HTTPS_PROXY/ALL_PROXY，或使用显式下载源覆盖变量。"
        return 1
    fi
}

verify_sha256() {
    local file="$1"
    local expected="$2"
    local actual
    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "$file" | awk '{print $1}')" || return 1
    elif command -v shasum >/dev/null 2>&1; then
        actual="$(shasum -a 256 "$file" | awk '{print $1}')" || return 1
    else
        return 1
    fi
    [[ "$(lowercase "$actual")" == "$(lowercase "$expected")" ]]
}

sha256_file() {
    local file="$1"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$file" | awk '{print $1}'
    else
        shasum -a 256 "$file" | awk '{print $1}'
    fi
}

lowercase() {
    printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

single_line_value() {
    local value="$1"
    [[ -n "$value" && "$value" != *$'\n'* && "$value" != *$'\r'* ]]
}

validate_ruleset_settings() {
    local custom_source=0
    RULESET_REQUESTED_BASE_URL="${MM_RULESET_BASE_URL:-$DEFAULT_RULESET_BASE_URL}"
    RULESET_REQUESTED_REF="${MM_RULESET_REF:-$DEFAULT_RULESET_REF}"
    RULESET_REQUESTED_DOMAIN_SHA256="${MM_RULESET_DOMAIN_SHA256:-$DEFAULT_RULESET_DOMAIN_SHA256}"
    RULESET_REQUESTED_IP_SHA256="${MM_RULESET_IP_SHA256:-$DEFAULT_RULESET_IP_SHA256}"

    single_line_value "$RULESET_REQUESTED_BASE_URL" || die "MM_RULESET_BASE_URL 不能为空或包含换行。"
    case "$RULESET_REQUESTED_BASE_URL" in
        http://*|https://*|file://*) ;;
        *) die "MM_RULESET_BASE_URL 必须使用 http、https 或 file URL。" ;;
    esac
    [[ "$RULESET_REQUESTED_BASE_URL" != *[[:space:]]* ]] || die "MM_RULESET_BASE_URL 不能包含空白字符。"
    [[ "$RULESET_REQUESTED_BASE_URL" != *"@"* && "$RULESET_REQUESTED_BASE_URL" != *"?"* && "$RULESET_REQUESTED_BASE_URL" != *"#"* ]] \
        || die "MM_RULESET_BASE_URL 不能包含凭据、查询参数或片段。"
    RULESET_REQUESTED_BASE_URL="${RULESET_REQUESTED_BASE_URL%/}"

    single_line_value "$RULESET_REQUESTED_REF" || die "MM_RULESET_REF 不能为空或包含换行。"
    [[ "$RULESET_REQUESTED_REF" =~ ^[A-Za-z0-9._/-]+$ ]] \
        || die "MM_RULESET_REF 只能包含字母、数字、点、下划线、斜线和连字符。"
    [[ "$RULESET_REQUESTED_REF" != /* && "/$RULESET_REQUESTED_REF/" != *"/../"* ]] \
        || die "MM_RULESET_REF 不能是绝对路径或包含 .. 路径段。"

    if [[ "$RULESET_REQUESTED_BASE_URL" != "$DEFAULT_RULESET_BASE_URL" || "$RULESET_REQUESTED_REF" != "$DEFAULT_RULESET_REF" ]]; then
        custom_source=1
    fi
    if ((custom_source == 1)) && [[ -z "${MM_RULESET_DOMAIN_SHA256:-}" || -z "${MM_RULESET_IP_SHA256:-}" ]]; then
        die "覆盖规则集来源时必须同时设置 MM_RULESET_DOMAIN_SHA256 和 MM_RULESET_IP_SHA256。"
    fi
    if [[ -n "${MM_RULESET_DOMAIN_SHA256:-}" || -n "${MM_RULESET_IP_SHA256:-}" ]]; then
        [[ -n "${MM_RULESET_DOMAIN_SHA256:-}" && -n "${MM_RULESET_IP_SHA256:-}" ]] \
            || die "规则集摘要必须成对覆盖。"
    fi
    RULESET_REQUESTED_DOMAIN_SHA256="$(lowercase "$RULESET_REQUESTED_DOMAIN_SHA256")"
    RULESET_REQUESTED_IP_SHA256="$(lowercase "$RULESET_REQUESTED_IP_SHA256")"
    [[ "$RULESET_REQUESTED_DOMAIN_SHA256" =~ ^[0-9a-f]{64}$ ]] \
        || die "MM_RULESET_DOMAIN_SHA256 必须是 64 位十六进制。"
    [[ "$RULESET_REQUESTED_IP_SHA256" =~ ^[0-9a-f]{64}$ ]] \
        || die "MM_RULESET_IP_SHA256 必须是 64 位十六进制。"
}

load_ruleset_catalog() {
    [[ -r "$RULESET_CATALOG_FILE" ]] || die "规则集 catalog 不存在: $RULESET_CATALOG_FILE"
    DEFAULT_RULESET_BASE_URL="$(jq -er '.source' "$RULESET_CATALOG_FILE")" || die "无法读取规则集 catalog source。"
    DEFAULT_RULESET_REF="$(jq -er '.ref' "$RULESET_CATALOG_FILE")" || die "无法读取规则集 catalog ref。"
    DEFAULT_RULESET_DOMAIN_SHA256="$(jq -er '.domain_sha256' "$RULESET_CATALOG_FILE")" || die "无法读取规则集 catalog domain 摘要。"
    DEFAULT_RULESET_IP_SHA256="$(jq -er '.ip_sha256' "$RULESET_CATALOG_FILE")" || die "无法读取规则集 catalog IP 摘要。"
    RULESET_DOMAIN_SOURCE_PATH="$(jq -er '.domain_path' "$RULESET_CATALOG_FILE")" || die "无法读取规则集 catalog domain 路径。"
    RULESET_IP_SOURCE_PATH="$(jq -er '.ip_path' "$RULESET_CATALOG_FILE")" || die "无法读取规则集 catalog IP 路径。"
}

path_has_symlink_component() {
    local current="$1"
    while [[ "$current" != "/" && "$current" != "." ]]; do
        [[ -L "$current" ]] && return 0
        current="$(dirname "$current")"
    done
    return 1
}

prepare_install_state_dir() {
    single_line_value "$STATE_DIR" || die "MM_STATE_DIR 不能为空或包含换行。"
    [[ "$STATE_DIR" == /* && "$STATE_DIR" != "/" && "$STATE_DIR" != "$HOME" ]] \
        || die "MM_STATE_DIR 必须是非 HOME 的安全绝对路径。"
    [[ "$STATE_DIR" != *"//"* ]] || die "MM_STATE_DIR 不能包含重复路径分隔符。"
    [[ "/${STATE_DIR#/}/" != *"/../"* && "/${STATE_DIR#/}/" != *"/./"* ]] \
        || die "MM_STATE_DIR 不能包含 . 或 .. 路径段。"
    case "$STATE_DIR" in
        /tmp|/var|/usr|/etc|/home|/root) die "拒绝使用危险 MM_STATE_DIR: $STATE_DIR" ;;
    esac
    path_has_symlink_component "$STATE_DIR" && die "MM_STATE_DIR 路径不能包含符号链接: $STATE_DIR"
    [[ ! -e "$STATE_DIR" || -d "$STATE_DIR" ]] || die "MM_STATE_DIR 不是目录: $STATE_DIR"
    mkdir -p "$STATE_DIR"
    chmod 0700 "$STATE_DIR"
}

validate_ruleset_target() {
    local target="$1"
    [[ ! -L "$target" ]] || die "规则集目标不能是符号链接: $target"
    [[ ! -e "$target" || -f "$target" ]] || die "规则集目标不是普通文件: $target"
}

prepare_ruleset_dir() {
    single_line_value "$CONFIG_DIR" || die "CONFIG_DIR 不能为空或包含换行。"
    [[ "$CONFIG_DIR" == /* && "$CONFIG_DIR" != "/" && "$CONFIG_DIR" != "$HOME" ]] \
        || die "CONFIG_DIR 必须是非 HOME 的安全绝对路径。"
    [[ "$CONFIG_DIR" != *"//"* ]] || die "CONFIG_DIR 不能包含重复路径分隔符。"
    [[ "/${CONFIG_DIR#/}/" != *"/../"* && "/${CONFIG_DIR#/}/" != *"/./"* ]] \
        || die "CONFIG_DIR 不能包含 . 或 .. 路径段。"
    CONFIG_DIR="${CONFIG_DIR%/}"
    RULESET_DIR="$CONFIG_DIR/rulesets"
    RULESET_DOMAIN_PATH="$RULESET_DIR/cn-domain.mrs"
    RULESET_IP_PATH="$RULESET_DIR/cn-ip.mrs"

    path_has_symlink_component "$CONFIG_DIR" && die "CONFIG_DIR 路径不能包含符号链接: $CONFIG_DIR"
    [[ ! -e "$CONFIG_DIR" || -d "$CONFIG_DIR" ]] || die "CONFIG_DIR 不是目录: $CONFIG_DIR"
    [[ ! -L "$RULESET_DIR" ]] || die "规则集目录不能是符号链接: $RULESET_DIR"
    [[ ! -e "$RULESET_DIR" || -d "$RULESET_DIR" ]] || die "规则集路径不是目录: $RULESET_DIR"

    mkdir -p "$RULESET_DIR"
    chmod 0700 "$RULESET_DIR"
    validate_ruleset_target "$RULESET_DOMAIN_PATH"
    validate_ruleset_target "$RULESET_IP_PATH"
}

validate_ruleset_pair() {
    local domain_file="$1"
    local ip_file="$2"
    local validation_dir validation_config
    validation_dir="$(mktemp -d "$RULESET_DIR/.ruleset-validate.XXXXXX")" || return 1
    validation_config="$validation_dir/config.yaml"
    TEMP_PATHS+=("$validation_dir")

    cp -- "$domain_file" "$validation_dir/cn-domain.mrs" || return 1
    cp -- "$ip_file" "$validation_dir/cn-ip.mrs" || return 1
    chmod 0600 "$validation_dir/cn-domain.mrs" "$validation_dir/cn-ip.mrs" || return 1
    cat >"$validation_config" <<'EOF' || return 1
mode: rule
log-level: silent
rule-providers:
  mm-cn-domain:
    type: file
    behavior: domain
    format: mrs
    path: ./cn-domain.mrs
  mm-cn-ip:
    type: file
    behavior: ipcidr
    format: mrs
    path: ./cn-ip.mrs
rules:
  - RULE-SET,mm-cn-domain,DIRECT
  - RULE-SET,mm-cn-ip,DIRECT,no-resolve
  - MATCH,DIRECT
EOF
    chmod 0600 "$validation_config" || return 1
    "$MIHOMO_BIN" -t -d "$validation_dir" -f "$validation_config" >/dev/null 2>&1
}

load_valid_ruleset_cache() {
    local recorded_base recorded_ref recorded_domain_path recorded_ip_path
    local recorded_domain_sha recorded_ip_sha
    recorded_base="$(state_value ruleset_base_url 2>/dev/null || true)"
    recorded_ref="$(state_value ruleset_ref 2>/dev/null || true)"
    recorded_domain_path="$(state_value ruleset_domain_path 2>/dev/null || true)"
    recorded_ip_path="$(state_value ruleset_ip_path 2>/dev/null || true)"
    recorded_domain_sha="$(state_value ruleset_domain_sha256 2>/dev/null || true)"
    recorded_ip_sha="$(state_value ruleset_ip_sha256 2>/dev/null || true)"

    [[ "$recorded_domain_path" == "$RULESET_DOMAIN_PATH" && "$recorded_ip_path" == "$RULESET_IP_PATH" ]] || return 1
    single_line_value "$recorded_base" && single_line_value "$recorded_ref" || return 1
    case "$recorded_base" in
        http://*|https://*|file://*) ;;
        *) return 1 ;;
    esac
    [[ "$recorded_base" != *[[:space:]]* && "$recorded_base" != *"@"* && "$recorded_base" != *"?"* && "$recorded_base" != *"#"* ]] || return 1
    [[ "$recorded_ref" =~ ^[A-Za-z0-9._/-]+$ && "$recorded_ref" != /* && "/$recorded_ref/" != *"/../"* ]] || return 1
    [[ "$recorded_domain_sha" =~ ^[0-9a-fA-F]{64}$ && "$recorded_ip_sha" =~ ^[0-9a-fA-F]{64}$ ]] || return 1
    validate_ruleset_target "$RULESET_DOMAIN_PATH"
    validate_ruleset_target "$RULESET_IP_PATH"
    [[ -f "$RULESET_DOMAIN_PATH" && -f "$RULESET_IP_PATH" ]] || return 1
    verify_sha256 "$RULESET_DOMAIN_PATH" "$recorded_domain_sha" || return 1
    verify_sha256 "$RULESET_IP_PATH" "$recorded_ip_sha" || return 1
    validate_ruleset_pair "$RULESET_DOMAIN_PATH" "$RULESET_IP_PATH" || return 1
    chmod 0600 "$RULESET_DOMAIN_PATH" "$RULESET_IP_PATH" || return 1

    RULESET_INSTALLED_BASE_URL="$recorded_base"
    RULESET_INSTALLED_REF="$recorded_ref"
    RULESET_INSTALLED_DOMAIN_SHA256="$(lowercase "$recorded_domain_sha")"
    RULESET_INSTALLED_IP_SHA256="$(lowercase "$recorded_ip_sha")"
}

ruleset_publish_file() {
    mv -f -- "$1" "$2"
}

restore_ruleset_target() {
    local target="$1"
    local backup="$2"
    local existed="$3"
    if [[ "$existed" == "1" ]]; then
        cp -p -- "$backup" "$target"
    else
        rm -f -- "$target"
    fi
}

publish_ruleset_pair() {
    local domain_temp="$1"
    local ip_temp="$2"
    local domain_backup ip_backup domain_existed=0 ip_existed=0
    validate_ruleset_target "$RULESET_DOMAIN_PATH"
    validate_ruleset_target "$RULESET_IP_PATH"
    domain_backup="$(mktemp "$RULESET_DIR/.cn-domain.previous.XXXXXX")" || return 1
    TEMP_PATHS+=("$domain_backup")
    ip_backup="$(mktemp "$RULESET_DIR/.cn-ip.previous.XXXXXX")" || return 1
    TEMP_PATHS+=("$ip_backup")

    if [[ -f "$RULESET_DOMAIN_PATH" ]]; then
        cp -p -- "$RULESET_DOMAIN_PATH" "$domain_backup" || return 1
        domain_existed=1
    fi
    if [[ -f "$RULESET_IP_PATH" ]]; then
        cp -p -- "$RULESET_IP_PATH" "$ip_backup" || return 1
        ip_existed=1
    fi

    if ! ruleset_publish_file "$domain_temp" "$RULESET_DOMAIN_PATH"; then
        return 1
    fi
    if ! ruleset_publish_file "$ip_temp" "$RULESET_IP_PATH"; then
        restore_ruleset_target "$RULESET_DOMAIN_PATH" "$domain_backup" "$domain_existed" \
            || die "规则集发布失败，且无法恢复 domain 规则集。"
        restore_ruleset_target "$RULESET_IP_PATH" "$ip_backup" "$ip_existed" \
            || die "规则集发布失败，且无法恢复 IP 规则集。"
        return 1
    fi
    if ! chmod 0600 "$RULESET_DOMAIN_PATH" "$RULESET_IP_PATH"; then
        restore_ruleset_target "$RULESET_DOMAIN_PATH" "$domain_backup" "$domain_existed" \
            || die "规则集权限设置失败，且无法恢复 domain 规则集。"
        restore_ruleset_target "$RULESET_IP_PATH" "$ip_backup" "$ip_existed" \
            || die "规则集权限设置失败，且无法恢复 IP 规则集。"
        return 1
    fi
}

install_rulesets() {
    local domain_url ip_url domain_temp ip_temp
    domain_url="$RULESET_REQUESTED_BASE_URL/$RULESET_REQUESTED_REF/$RULESET_DOMAIN_SOURCE_PATH"
    ip_url="$RULESET_REQUESTED_BASE_URL/$RULESET_REQUESTED_REF/$RULESET_IP_SOURCE_PATH"
    domain_temp="$(mktemp "$RULESET_DIR/.cn-domain.download.XXXXXX")" \
        || die "无法在规则集目录创建 domain 临时文件。"
    TEMP_PATHS+=("$domain_temp")
    ip_temp="$(mktemp "$RULESET_DIR/.cn-ip.download.XXXXXX")" \
        || die "无法在规则集目录创建 IP 临时文件。"
    TEMP_PATHS+=("$ip_temp")
    chmod 0600 "$domain_temp" "$ip_temp"

    info "下载固定 CN 规则集: $RULESET_REQUESTED_REF"
    if download_file "$domain_url" "$domain_temp" \
        && download_file "$ip_url" "$ip_temp" \
        && verify_sha256 "$domain_temp" "$RULESET_REQUESTED_DOMAIN_SHA256" \
        && verify_sha256 "$ip_temp" "$RULESET_REQUESTED_IP_SHA256" \
        && validate_ruleset_pair "$domain_temp" "$ip_temp"; then
        publish_ruleset_pair "$domain_temp" "$ip_temp" || die "无法原子发布 CN 规则集，旧缓存已恢复。"
        RULESET_INSTALLED_BASE_URL="$RULESET_REQUESTED_BASE_URL"
        RULESET_INSTALLED_REF="$RULESET_REQUESTED_REF"
        RULESET_INSTALLED_DOMAIN_SHA256="$RULESET_REQUESTED_DOMAIN_SHA256"
        RULESET_INSTALLED_IP_SHA256="$RULESET_REQUESTED_IP_SHA256"
        success "已安装 CN 规则集: $RULESET_DIR"
        return 0
    fi

    warn "新 CN 规则集下载、摘要或格式校验失败。"
    if load_valid_ruleset_cache; then
        warn "继续使用 install-state 已验证的旧 CN 规则集缓存。"
        return 0
    fi
    die "没有可安全复用的 CN 规则集缓存，安装已停止。"
}

go_meets_minimum() {
    local go_bin="$1"
    local output major minor
    output="$($go_bin version 2>/dev/null)" || return 1
    if [[ "$output" =~ go([0-9]+)\.([0-9]+) ]]; then
        major="${BASH_REMATCH[1]}"
        minor="${BASH_REMATCH[2]}"
        ((major > MIN_GO_MAJOR || (major == MIN_GO_MAJOR && minor >= MIN_GO_MINOR)))
        return
    fi
    return 1
}

go_archive_sha256() {
    case "${TARGET_OS:-linux}/$TARGET_ARCH" in
        linux/amd64) printf '%s\n' '1153d3d50e0ac764b447adfe05c2bcf08e889d42a02e0fe0259bd47f6733ad7f' ;;
        linux/arm64) printf '%s\n' 'ef758ae7c6cf9267c9c0ef080b8965f453d89ab2d25d9eb22de4405925238768' ;;
        darwin/amd64) printf '%s\n' '05dc9b5f9997744520aaebb3d5deaa7c755371aebbfb7f97c2511a9f3367538d' ;;
        darwin/arm64) printf '%s\n' 'b62ad2b6d7d2464f12a5bcad7ff47f19d08325773b5efd21610e445a05a9bf53' ;;
        *) return 1 ;;
    esac
}

install_go_toolchain() {
    local base_url="${MM_GO_DOWNLOAD_BASE_URL:-https://go.dev/dl}"
    local filename="go${GO_VERSION}.${TARGET_OS:-linux}-${TARGET_ARCH}.tar.gz"
    local expected target archive extract_dir old_target
    base_url="${base_url%/}"
    expected="$(go_archive_sha256)"
    target="$STATE_DIR/toolchains/go${GO_VERSION}"

    mkdir -p "$STATE_DIR/toolchains"
    archive="$(mktemp "$STATE_DIR/.${filename}.XXXXXX")"
    extract_dir="$(mktemp -d "$STATE_DIR/.go-extract.XXXXXX")"
    TEMP_PATHS+=("$archive" "$extract_dir")

    info "下载官方 Go ${GO_VERSION} (${TARGET_OS:-linux}/$TARGET_ARCH)..."
    download_file "$base_url/$filename" "$archive" || die "无法下载 Go 工具链。"
    verify_sha256 "$archive" "$expected" || die "Go 工具链 SHA-256 校验失败。"
    tar -xzf "$archive" -C "$extract_dir"
    [[ -x "$extract_dir/go/bin/go" ]] || die "Go 工具链归档内容无效。"

    old_target="$target.old.$$"
    rm -rf "$old_target"
    if [[ -d "$target" ]]; then
        mv "$target" "$old_target"
    fi
    if ! mv "$extract_dir/go" "$target"; then
        [[ -d "$old_target" ]] && mv "$old_target" "$target"
        die "无法替换隔离 Go 工具链。"
    fi
    rm -rf "$old_target"
    GO_BIN="$target/bin/go"
    go_meets_minimum "$GO_BIN" || die "隔离 Go 工具链安装后版本检查失败。"
    success "已安装隔离 Go 工具链: $target"
}

select_go() {
    local system_go=""
    local managed_go="$STATE_DIR/toolchains/go${GO_VERSION}/bin/go"

    system_go="$(command -v go 2>/dev/null || true)"
    if [[ -n "$system_go" ]] && go_meets_minimum "$system_go"; then
        GO_BIN="$system_go"
        info "复用现有 Go: $($GO_BIN version)"
        return 0
    fi

    if [[ -x "$managed_go" ]] && go_meets_minimum "$managed_go"; then
        GO_BIN="$managed_go"
        info "复用隔离 Go: $($GO_BIN version)"
        return 0
    fi

    install_go_toolchain
}

normalize_version() {
    local version="${1#v}"
    [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || return 1
    printf 'v%s\n' "$version"
}

semver_ge() {
    local actual="${1#v}"
    local target="${2#v}"
    local a1 a2 a3 t1 t2 t3
    IFS=. read -r a1 a2 a3 <<<"$actual"
    IFS=. read -r t1 t2 t3 <<<"$target"
    ((a1 > t1)) && return 0
    ((a1 < t1)) && return 1
    ((a2 > t2)) && return 0
    ((a2 < t2)) && return 1
    ((a3 >= t3))
}

state_value() {
    local key="$1"
    [[ -r "$STATE_FILE" ]] || return 1
    awk -F= -v wanted="$key" '$1 == wanted { print substr($0, index($0, "=") + 1); exit }' "$STATE_FILE"
}

existing_core_version() {
    local output
    output="$($MIHOMO_BIN -v 2>&1)" || return 1
    if [[ "$output" =~ v([0-9]+\.[0-9]+\.[0-9]+) ]]; then
        printf 'v%s\n' "${BASH_REMATCH[1]}"
        return 0
    fi
    return 1
}

core_asset_name() {
    local version="$1"
    case "${TARGET_OS:-linux}/$TARGET_ARCH" in
        linux/amd64) printf 'mihomo-linux-amd64-v1-%s.gz\n' "$version" ;;
        linux/arm64) printf 'mihomo-linux-arm64-%s.gz\n' "$version" ;;
        darwin/amd64) printf 'mihomo-darwin-amd64-v1-%s.gz\n' "$version" ;;
        darwin/arm64) printf 'mihomo-darwin-arm64-%s.gz\n' "$version" ;;
        *) return 1 ;;
    esac
}

CORE_ASSET_URL=""
CORE_ASSET_SHA256=""

fetch_core_metadata() {
    local version="$1"
    local asset_name api_base github_base metadata digest
    asset_name="$(core_asset_name "$version")"
    api_base="${MM_GITHUB_API_BASE_URL:-https://api.github.com}"
    github_base="${MM_GITHUB_BASE_URL:-https://github.com}"
    api_base="${api_base%/}"
    github_base="${github_base%/}"

    mkdir -p "$STATE_DIR"
    chmod 0700 "$STATE_DIR"
    metadata="$(mktemp "$STATE_DIR/.mihomo-release.XXXXXX.json")"
    TEMP_PATHS+=("$metadata")
    download_file "$api_base/repos/MetaCubeX/mihomo/releases/tags/$version" "$metadata" \
        || die "无法读取 mihomo $version 发布信息。"

    CORE_ASSET_URL="$(jq -r --arg name "$asset_name" '.assets[] | select(.name == $name) | .browser_download_url' "$metadata" | head -n 1)"
    digest="$(jq -r --arg name "$asset_name" '.assets[] | select(.name == $name) | .digest' "$metadata" | head -n 1)"
    [[ -n "$CORE_ASSET_URL" && "$CORE_ASSET_URL" != "null" ]] || die "发布 $version 中未找到资产: $asset_name"
    [[ "$digest" == sha256:* ]] || die "发布 $version 的资产缺少 SHA-256 摘要: $asset_name"
    CORE_ASSET_SHA256="${digest#sha256:}"
    [[ "$CORE_ASSET_SHA256" =~ ^[0-9a-fA-F]{64}$ ]] || die "mihomo SHA-256 摘要格式无效。"

    if [[ "$github_base" != "https://github.com" ]]; then
        CORE_ASSET_URL="${CORE_ASSET_URL/https:\/\/github.com/$github_base}"
    fi
}

install_mihomo_core() {
    local requested current previous_managed previous_path
    local target_dir archive temp_bin installed_version
    requested="$(normalize_version "${MIHOMO_VERSION:-$DEFAULT_MIHOMO_VERSION}")" \
        || die "MIHOMO_VERSION 必须是 X.Y.Z 或 vX.Y.Z 格式。"
    CORE_VERSION="$requested"
    previous_managed="$(state_value core_managed 2>/dev/null || printf '0')"
    previous_path="$(state_value core_path 2>/dev/null || true)"
    CORE_MANAGED=0

    if [[ -x "$MIHOMO_BIN" ]] && ((FORCE_CORE == 0)); then
        current="$(existing_core_version || true)"
        if [[ -n "$current" ]] && semver_ge "$current" "$requested"; then
            CORE_VERSION="$current"
            if [[ "$previous_managed" == "1" && "$previous_path" == "$MIHOMO_BIN" ]]; then
                CORE_MANAGED=1
            fi
            info "保留现有 mihomo core: ${current}（目标 ${requested}）"
            return 0
        fi
    fi

    fetch_core_metadata "$requested"
    target_dir="$(dirname "$MIHOMO_BIN")"
    mkdir -p "$target_dir"
    archive="$(mktemp "$target_dir/.mihomo-download.XXXXXX.gz")"
    temp_bin="$(mktemp "$target_dir/.mihomo-new.XXXXXX")"
    TEMP_PATHS+=("$archive" "$temp_bin")

    info "下载 mihomo core $requested (${TARGET_OS:-linux}/$TARGET_ARCH)..."
    download_file "$CORE_ASSET_URL" "$archive" || die "无法下载 mihomo core。"
    verify_sha256 "$archive" "$CORE_ASSET_SHA256" || die "mihomo core SHA-256 校验失败。"
    gzip -dc "$archive" >"$temp_bin"
    chmod 0755 "$temp_bin"

    installed_version="$($temp_bin -v 2>&1)" || die "下载的 mihomo core 无法执行版本检查。"
    [[ "$installed_version" == *"$requested"* ]] || die "下载的 mihomo core 版本与目标版本不一致。"

    if [[ -L "$MIHOMO_BIN" ]]; then
        cp -P -- "$MIHOMO_BIN" "$MIHOMO_BIN.bak"
        info "已备份旧 core 链接: $MIHOMO_BIN.bak"
    elif [[ -e "$MIHOMO_BIN" ]]; then
        cp -p -- "$MIHOMO_BIN" "$MIHOMO_BIN.bak"
        info "已备份旧 core: $MIHOMO_BIN.bak"
    fi
    mv -f -- "$temp_bin" "$MIHOMO_BIN"
    CORE_MANAGED=1
    success "已安装 mihomo core: $MIHOMO_BIN ($requested)"
}

build_mm() {
    mkdir -p "$INSTALL_DIR"
    MM_BUILD_PATH="$(mktemp "$INSTALL_DIR/.mm-build.XXXXXX")"
    TEMP_PATHS+=("$MM_BUILD_PATH")

    info "使用 $($GO_BIN version) 构建 mm..."
    (
        cd "$PROJECT_DIR"
        CGO_ENABLED=0 "$GO_BIN" build -trimpath -o "$MM_BUILD_PATH" ./cmd/mm
    )
    chmod 0755 "$MM_BUILD_PATH"
    "$MM_BUILD_PATH" --help >/dev/null
    success "mm 构建与冒烟验证通过。"
}

configure_mihomo() {
    local config_file="$CONFIG_DIR/config.yaml"
    local temp_config
    CONFIG_CREATED=0
    mkdir -p "$CONFIG_DIR"

    if [[ -f "$config_file" ]]; then
        if "$MIHOMO_BIN" -t -d "$CONFIG_DIR" -f "$config_file" >/dev/null 2>&1; then
            info "保留并验证现有配置: $config_file"
        else
            CONFIG_WARNING=1
            warn "现有配置未通过 mihomo 校验，安装器未修改它: $config_file"
        fi
        return 0
    fi

    temp_config="$(mktemp "$CONFIG_DIR/.config.yaml.new.XXXXXX")"
    TEMP_PATHS+=("$temp_config")
    cat >"$temp_config" <<'EOF'
mixed-port: 7890
socks-port: 7891
allow-lan: false
mode: rule
log-level: info
external-controller: 127.0.0.1:9090
rules:
  - MATCH,DIRECT
EOF
    chmod 0600 "$temp_config"
    "$MIHOMO_BIN" -t -d "$CONFIG_DIR" -f "$temp_config" >/dev/null 2>&1 \
        || die "新建的最小配置未通过 mihomo 校验。"
    mv -f -- "$temp_config" "$config_file"
    CONFIG_CREATED=1
    success "已创建最小 DIRECT 配置: $config_file"
}

install_mm_binary() {
    local target="$INSTALL_DIR/mm"
    [[ -x "$MM_BUILD_PATH" ]] || die "mm 临时构建产物不存在。"
    mv -f -- "$MM_BUILD_PATH" "$target"
    "$target" --help >/dev/null
    success "已安装独立 mm 命令: $target"
}

probe_daemon_before_upgrade() {
    local installed_mm="$INSTALL_DIR/mm"
    local status_file core_state
    DAEMON_PREEXISTING=0
    DAEMON_CORE_RUNNING=0
    DAEMON_PREVIOUS_CORE_STATE=""
    [[ -x "$installed_mm" ]] || return 0

    mkdir -p "$STATE_DIR"
    status_file="$(mktemp "$STATE_DIR/.daemon-before-upgrade.XXXXXX.json")"
    TEMP_PATHS+=("$status_file")
    if ! "$installed_mm" daemon status --output json >"$status_file" 2>/dev/null; then
        return 0
    fi
    jq -e '.apiVersion == "mm/v1" and .kind == "DaemonStatus" and (.data | type == "object")' "$status_file" >/dev/null \
        || return 0
    core_state="$(jq -er '.data.core.state' "$status_file" 2>/dev/null || true)"
    [[ -n "$core_state" ]] || return 0
    DAEMON_PREEXISTING=1
    DAEMON_PREVIOUS_CORE_STATE="$core_state"
    if [[ "$core_state" == "running" ]]; then
        DAEMON_CORE_RUNNING=1
        info "检测到受管 core 正在运行；本次升级不会停止或重启 daemon。"
    fi
}

run_installed_mm_json() {
    local output_file="$1"
    local expected_kind="$2"
    shift 2
    if ! CONFIG_DIR="$CONFIG_DIR" MIHOMO_BIN="$MIHOMO_BIN" MIHOMO_API_PORT="${MIHOMO_API_PORT:-9090}" \
        "$INSTALL_DIR/mm" "$@" --output json >"$output_file"; then
        return 1
    fi
    jq -e --arg kind "$expected_kind" \
        '.apiVersion == "mm/v1" and .kind == $kind and (.data | type == "object")' \
        "$output_file" >/dev/null
}

apply_fresh_migration() {
    local migrate_file migration_state
    migrate_file="$(mktemp "$STATE_DIR/.migration-apply.XXXXXX.json")"
    TEMP_PATHS+=("$migrate_file")
    info "注册本次新建的 legacy 配置..."
    run_installed_mm_json "$migrate_file" "MigrationApply" migrate apply \
        || die "mm 已安装且 daemon 可用，但新建配置注册失败；请运行 mm migrate plan 后重试。"
    migration_state="$(jq -er '.data.migration.state' "$migrate_file" 2>/dev/null || true)"
    [[ "$migration_state" == "succeeded" ]] \
        || die "新建配置迁移未进入 succeeded 状态，请运行 mm migrate status 检查。"
    success "已注册新建配置为活动 legacy 档案。"
}

configure_daemon_and_migration() {
    local enable_file start_file stop_file status_file enabled core_state daemon_state hint preserve_existing=0
    enable_file="$(mktemp "$STATE_DIR/.daemon-enable.XXXXXX.json")"
    start_file="$(mktemp "$STATE_DIR/.daemon-start.XXXXXX.json")"
    stop_file="$(mktemp "$STATE_DIR/.daemon-stop.XXXXXX.json")"
    status_file="$(mktemp "$STATE_DIR/.daemon-status.XXXXXX.json")"
    TEMP_PATHS+=("$enable_file" "$start_file" "$stop_file" "$status_file")

    info "安装并启用用户级 manager daemon..."
    run_installed_mm_json "$enable_file" "DaemonControl" daemon enable \
        || die "mm 与规则集已安装，但 daemon 后台服务安装或启用失败；请运行 mm daemon enable 检查。"
    enabled="$(jq -er '.data.enabled' "$enable_file" 2>/dev/null || true)"
    [[ "$enabled" == "true" || "$enabled" == "false" ]] \
        || die "daemon enable 返回了无效 enabled 状态。"
	DAEMON_BACKEND="$(jq -r '.data.backend // empty' "$enable_file" 2>/dev/null || true)"
	if [[ -z "$DAEMON_BACKEND" ]]; then
		[[ "${TARGET_OS:-linux}" == "darwin" ]] && DAEMON_BACKEND="launchd" || DAEMON_BACKEND="systemd"
	fi

    if ((DAEMON_PREEXISTING == 1)) && [[ "$DAEMON_PREVIOUS_CORE_STATE" != "stopped" ]]; then
        preserve_existing=1
    elif ((DAEMON_PREEXISTING == 1)) && [[ "$enabled" == "true" ]]; then
        run_installed_mm_json "$status_file" "DaemonStatus" daemon status \
            || die "重启 daemon 前无法复核 core 状态；为避免中断 core，已停止升级编排。"
        core_state="$(jq -er '.data.core.state' "$status_file" 2>/dev/null || true)"
        if [[ "$core_state" != "stopped" ]]; then
            preserve_existing=1
            DAEMON_PREVIOUS_CORE_STATE="$core_state"
            [[ "$core_state" == "running" ]] && DAEMON_CORE_RUNNING=1
        fi
    fi

    if ((preserve_existing == 1)); then
        DAEMON_AVAILABLE=1
        warn "受管 core 状态为 ${DAEMON_PREVIOUS_CORE_STATE}，已跳过 daemon stop/start；新 mm 将在后续 daemon 重启时加载。"
    elif [[ "$enabled" == "true" ]]; then
        if ((DAEMON_PREEXISTING == 1)); then
            info "core 已停止，重启 daemon 以加载新 mm..."
            run_installed_mm_json "$stop_file" "DaemonControl" daemon stop \
                || die "core 已停止，但旧 daemon 停止失败；请运行 mm daemon status 检查。"
        fi
        run_installed_mm_json "$start_file" "DaemonControl" daemon start \
            || die "daemon socket 启动失败；请运行 mm daemon start 检查。"
        DAEMON_AVAILABLE=1
    elif ((DAEMON_PREEXISTING == 1)); then
        DAEMON_AVAILABLE=1
        warn "用户级后台服务管理器不可用，现有 daemon 保持运行且未自动重启。"
    else
        DAEMON_AVAILABLE=0
        hint="$(jq -r '.data.hint // empty' "$enable_file")"
        warn "用户级 daemon 配置已安装但未启用。"
        [[ -z "$hint" ]] || warn "$hint"
        printf -v hint 'CONFIG_DIR=%q MIHOMO_BIN=%q MIHOMO_API_PORT=%q mm daemon run' \
            "$CONFIG_DIR" "$MIHOMO_BIN" "${MIHOMO_API_PORT:-9090}"
        warn "前台启动并保持相同路径: $hint"
    fi

    if ((DAEMON_AVAILABLE == 1)); then
        run_installed_mm_json "$status_file" "DaemonStatus" daemon status \
            || die "daemon 启动或升级后状态确认失败；请运行 mm daemon status 检查。"
        daemon_state="$(jq -er '.data.state' "$status_file" 2>/dev/null || true)"
        core_state="$(jq -er '.data.core.state' "$status_file" 2>/dev/null || true)"
        [[ "$daemon_state" == "running" ]] || die "daemon 未进入 running 状态。"
        if ((preserve_existing == 0)) && [[ "$core_state" != "stopped" ]]; then
            die "安装器未启动 core，但 daemon 报告 core 状态为 ${core_state}；请人工检查。"
        fi
        success "manager daemon 已就绪，core 状态: $core_state"
    fi

    if ((CONFIG_CREATED == 1)); then
        if ((DAEMON_AVAILABLE == 1)); then
            apply_fresh_migration
        else
            warn "新建配置尚未注册；启动 daemon 后运行: mm migrate apply"
        fi
    else
        info "已有配置未自动迁移。请先运行: mm migrate plan"
        info "确认后显式运行: mm migrate apply"
    fi
}

path_block_exists() {
    local file="$1"
    [[ -f "$file" ]] && grep -q '^# >>> mihomo-manager PATH >>>$' "$file"
}

path_equivalent_exists() {
    local file="$1"
    [[ -f "$file" ]] || return 1
    grep -Eq '(\$HOME|~)/\.local/bin|/\.local/bin' "$file"
}

configure_path() {
    local shell_name rc_file previous_rc
    previous_rc="$(state_value path_rc 2>/dev/null || true)"
    PATH_RC_MODIFIED=""

    if [[ -n "$previous_rc" ]] && path_block_exists "$previous_rc"; then
        PATH_RC_MODIFIED="$previous_rc"
        return 0
    fi

    case ":$PATH:" in
        *":$INSTALL_DIR:"*)
            info "$INSTALL_DIR 已在当前 PATH 中。"
            return 0
            ;;
    esac

    if [[ "${TARGET_OS:-linux}" == "darwin" ]]; then
        rc_file="$HOME/.zshrc"
    else
        shell_name="$(basename "${SHELL:-}")"
        case "$shell_name" in
            bash) rc_file="$HOME/.bashrc" ;;
            zsh) rc_file="$HOME/.zshrc" ;;
            *)
                warn "未识别当前 shell，未自动修改 PATH。"
                warn "请手动执行: export PATH=\"$INSTALL_DIR:\$PATH\""
                return 0
                ;;
        esac
    fi

    if path_block_exists "$rc_file"; then
        PATH_RC_MODIFIED="$rc_file"
        return 0
    fi
    if path_equivalent_exists "$rc_file"; then
        info "$rc_file 已包含等效的 ~/.local/bin PATH 配置。"
        return 0
    fi

    touch "$rc_file"
    {
        printf '\n%s\n' '# >>> mihomo-manager PATH >>>'
        printf '%s\n' 'export PATH="$HOME/.local/bin:$PATH"'
        printf '%s\n' '# <<< mihomo-manager PATH <<<'
    } >>"$rc_file"
    PATH_RC_MODIFIED="$rc_file"
    success "已更新 PATH 配置: $rc_file"
}

write_state() {
    local temp_state go_toolchain_path=""
    mkdir -p "$STATE_DIR"
    chmod 0700 "$STATE_DIR"

    if [[ "$GO_BIN" == "$STATE_DIR"/toolchains/*/bin/go ]]; then
        go_toolchain_path="$(dirname "$(dirname "$GO_BIN")")"
    elif [[ -d "$STATE_DIR/toolchains/go${GO_VERSION}" ]]; then
        go_toolchain_path="$STATE_DIR/toolchains/go${GO_VERSION}"
    fi

	if [[ "${TARGET_OS:-linux}" == "darwin" ]]; then
		DAEMON_BACKEND="${DAEMON_BACKEND:-launchd}"
		DAEMON_CONFIG_PATH="$HOME/Library/LaunchAgents/com.zhangjianyong.mihomo-manager.daemon.plist"
		if [[ -f "$DAEMON_CONFIG_PATH" ]]; then
			DAEMON_CONFIG_SHA256="$(sha256_file "$DAEMON_CONFIG_PATH")"
		fi
	else
		DAEMON_BACKEND="${DAEMON_BACKEND:-systemd}"
		DAEMON_CONFIG_PATH="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
		DAEMON_CONFIG_SHA256=""
	fi

    temp_state="$(mktemp "$STATE_DIR/.install-state.XXXXXX")"
    TEMP_PATHS+=("$temp_state")
    {
		printf 'installer_version=3\n'
		printf 'platform=%s\n' "${TARGET_OS:-linux}"
		printf 'daemon_backend=%s\n' "$DAEMON_BACKEND"
		printf 'daemon_config_path=%s\n' "$DAEMON_CONFIG_PATH"
		printf 'daemon_config_sha256=%s\n' "$DAEMON_CONFIG_SHA256"
        printf 'mm_path=%s\n' "$INSTALL_DIR/mm"
        printf 'go_toolchain_path=%s\n' "$go_toolchain_path"
        printf 'path_rc=%s\n' "$PATH_RC_MODIFIED"
        printf 'core_managed=%s\n' "$CORE_MANAGED"
        printf 'core_path=%s\n' "$MIHOMO_BIN"
        printf 'core_version=%s\n' "$CORE_VERSION"
        printf 'config_dir=%s\n' "$CONFIG_DIR"
        printf 'ruleset_base_url=%s\n' "$RULESET_INSTALLED_BASE_URL"
        printf 'ruleset_ref=%s\n' "$RULESET_INSTALLED_REF"
        printf 'ruleset_domain_path=%s\n' "$RULESET_DOMAIN_PATH"
        printf 'ruleset_domain_sha256=%s\n' "$RULESET_INSTALLED_DOMAIN_SHA256"
        printf 'ruleset_ip_path=%s\n' "$RULESET_IP_PATH"
        printf 'ruleset_ip_sha256=%s\n' "$RULESET_INSTALLED_IP_SHA256"
    } >"$temp_state"
    chmod 0600 "$temp_state"
    mv -f -- "$temp_state" "$STATE_FILE"
}

print_summary() {
    printf '\n'
    success "Mihomo Manager 安装完成。"
    printf '  mm:      %s\n' "$INSTALL_DIR/mm"
    printf '  mihomo:  %s (%s)\n' "$MIHOMO_BIN" "$CORE_VERSION"
    printf '  配置目录: %s\n' "$CONFIG_DIR"
    if [[ -n "$RULESET_INSTALLED_REF" ]]; then
        printf '  CN 规则集: %s (%s，已安装/复用)\n' "$RULESET_DIR" "$RULESET_INSTALLED_REF"
    else
        printf '  CN 规则集: 待安装（执行 mm ruleset install）\n'
    fi
    printf '\n安装器没有启动 mihomo，也没有修改系统代理。\n'
    if ((CONFIG_WARNING == 1)); then
        warn "mm 已安装，但现有配置需要修复后才能启动 mihomo。"
    fi
    case ":$PATH:" in
        *":$INSTALL_DIR:"*) ;;
        *) printf '当前终端可先执行: export PATH="%s:$PATH"\n' "$INSTALL_DIR" ;;
    esac
    printf '运行命令: mm\n'
}

main() {
    parse_args "$@"
    preflight
    install_system_dependencies
    verify_required_commands
    load_ruleset_catalog
    if ((INSTALL_RULESETS == 1)); then
        validate_ruleset_settings
    else
        RULESET_REQUESTED_BASE_URL="$DEFAULT_RULESET_BASE_URL"
        RULESET_REQUESTED_REF="$DEFAULT_RULESET_REF"
        RULESET_REQUESTED_DOMAIN_SHA256="$DEFAULT_RULESET_DOMAIN_SHA256"
        RULESET_REQUESTED_IP_SHA256="$DEFAULT_RULESET_IP_SHA256"
    fi
    prepare_install_state_dir
    prepare_ruleset_dir
    select_go
    build_mm
    install_mihomo_core
    if ((INSTALL_RULESETS == 1)); then
        install_rulesets
    else
        if load_valid_ruleset_cache; then
            info "已复用本地验证通过的 CN 规则集缓存。"
        else
            RULESET_INSTALLED_BASE_URL=""
            RULESET_INSTALLED_REF=""
            RULESET_INSTALLED_DOMAIN_SHA256=""
            RULESET_INSTALLED_IP_SHA256=""
            info "默认跳过 CN 规则集下载；稍后可执行 mm ruleset install。"
        fi
    fi
    configure_mihomo
    probe_daemon_before_upgrade
    install_mm_binary
    configure_path
    configure_daemon_and_migration
    write_state
    print_summary
}

if [[ "${MM_INSTALL_LIB_ONLY:-0}" != "1" ]]; then
    main "$@"
fi
