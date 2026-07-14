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

ASSUME_YES=0
FORCE_CORE=0
TARGET_ARCH=""
GO_BIN=""
MM_BUILD_PATH=""
CORE_MANAGED=0
CORE_VERSION=""
PATH_RC_MODIFIED=""
CONFIG_WARNING=0

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
  --yes          跳过 apt 安装确认
  --force-core   强制重新安装目标 mihomo core 版本
  -h, --help     显示帮助

环境变量：
  MM_ASSUME_YES=1             等同 --yes
  MIHOMO_VERSION=v1.19.28     指定 mihomo core 版本
  MM_GITHUB_BASE_URL=...      覆盖 GitHub 下载基地址
  MM_GITHUB_API_BASE_URL=...  覆盖 GitHub API 基地址
  MM_GO_DOWNLOAD_BASE_URL=... 覆盖 Go 下载基地址
EOF
}

parse_args() {
    case "${MM_ASSUME_YES:-0}" in
        1|true|TRUE|yes|YES) ASSUME_YES=1 ;;
    esac

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --yes)
                ASSUME_YES=1
                ;;
            --force-core)
                FORCE_CORE=1
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

platform_supported() {
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
    if [[ -z "$raw" ]] && command -v dpkg >/dev/null 2>&1; then
        raw="$(dpkg --print-architecture 2>/dev/null || true)"
    fi
    if [[ -z "$raw" ]]; then
        raw="$(uname -m)"
    fi
    normalize_arch "$raw"
}

preflight() {
    local effective_uid="${MM_EUID_OVERRIDE:-$EUID}"
    [[ "$effective_uid" != "0" ]] || die "请以普通用户运行安装器，不要使用 sudo 执行整个脚本。"
    platform_supported || die "首版仅支持 Ubuntu/Debian。当前系统不受支持，未执行任何安装变更。"
    command -v apt-get >/dev/null 2>&1 || die "未找到 apt-get，首版仅支持 apt 包管理器。"

    TARGET_ARCH="$(detect_arch)" || die "不支持当前 CPU 架构，仅支持 amd64 和 arm64。"
    info "检测到受支持环境: $TARGET_ARCH"
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

install_system_dependencies() {
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

verify_required_commands() {
    local required=(curl tar gzip jq sha256sum pgrep pkill awk sed grep head mktemp mv cp chmod mkdir dirname basename cat touch)
    local command_name missing=()
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
        warn "下载失败: $url"
        warn "可设置 HTTP_PROXY/HTTPS_PROXY/ALL_PROXY，或使用显式下载源覆盖变量。"
        return 1
    fi
}

verify_sha256() {
    local file="$1"
    local expected="$2"
    printf '%s  %s\n' "$expected" "$file" | sha256sum --check --status
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
    case "$TARGET_ARCH" in
        amd64) printf '%s\n' '1153d3d50e0ac764b447adfe05c2bcf08e889d42a02e0fe0259bd47f6733ad7f' ;;
        arm64) printf '%s\n' 'ef758ae7c6cf9267c9c0ef080b8965f453d89ab2d25d9eb22de4405925238768' ;;
        *) return 1 ;;
    esac
}

install_go_toolchain() {
    local base_url="${MM_GO_DOWNLOAD_BASE_URL:-https://go.dev/dl}"
    local filename="go${GO_VERSION}.linux-${TARGET_ARCH}.tar.gz"
    local expected target archive extract_dir old_target
    base_url="${base_url%/}"
    expected="$(go_archive_sha256)"
    target="$STATE_DIR/toolchains/go${GO_VERSION}"

    mkdir -p "$STATE_DIR/toolchains"
    archive="$(mktemp "$STATE_DIR/.${filename}.XXXXXX")"
    extract_dir="$(mktemp -d "$STATE_DIR/.go-extract.XXXXXX")"
    TEMP_PATHS+=("$archive" "$extract_dir")

    info "下载官方 Go ${GO_VERSION} ($TARGET_ARCH)..."
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
    case "$TARGET_ARCH" in
        amd64) printf 'mihomo-linux-amd64-v1-%s.gz\n' "$version" ;;
        arm64) printf 'mihomo-linux-arm64-%s.gz\n' "$version" ;;
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
            info "保留现有 mihomo core: $current（目标 $requested）"
            return 0
        fi
    fi

    fetch_core_metadata "$requested"
    target_dir="$(dirname "$MIHOMO_BIN")"
    mkdir -p "$target_dir"
    archive="$(mktemp "$target_dir/.mihomo-download.XXXXXX.gz")"
    temp_bin="$(mktemp "$target_dir/.mihomo-new.XXXXXX")"
    TEMP_PATHS+=("$archive" "$temp_bin")

    info "下载 mihomo core $requested ($TARGET_ARCH)..."
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
mixed-port: 10808
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
    success "已创建最小 DIRECT 配置: $config_file"
}

install_mm_binary() {
    local target="$INSTALL_DIR/mm"
    [[ -x "$MM_BUILD_PATH" ]] || die "mm 临时构建产物不存在。"
    mv -f -- "$MM_BUILD_PATH" "$target"
    "$target" --help >/dev/null
    success "已安装独立 mm 命令: $target"
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

    if [[ "$GO_BIN" == "$STATE_DIR"/toolchains/*/bin/go ]]; then
        go_toolchain_path="$(dirname "$(dirname "$GO_BIN")")"
    elif [[ -d "$STATE_DIR/toolchains/go${GO_VERSION}" ]]; then
        go_toolchain_path="$STATE_DIR/toolchains/go${GO_VERSION}"
    fi

    temp_state="$(mktemp "$STATE_DIR/.install-state.XXXXXX")"
    TEMP_PATHS+=("$temp_state")
    {
        printf 'installer_version=1\n'
        printf 'mm_path=%s\n' "$INSTALL_DIR/mm"
        printf 'go_toolchain_path=%s\n' "$go_toolchain_path"
        printf 'path_rc=%s\n' "$PATH_RC_MODIFIED"
        printf 'core_managed=%s\n' "$CORE_MANAGED"
        printf 'core_path=%s\n' "$MIHOMO_BIN"
        printf 'core_version=%s\n' "$CORE_VERSION"
        printf 'config_dir=%s\n' "$CONFIG_DIR"
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
    select_go
    build_mm
    install_mihomo_core
    configure_mihomo
    install_mm_binary
    configure_path
    write_state
    print_summary
}

if [[ "${MM_INSTALL_LIB_ONLY:-0}" != "1" ]]; then
    main "$@"
fi
