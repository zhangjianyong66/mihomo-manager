#!/usr/bin/env bash

set -Eeuo pipefail

INSTALL_DIR="${MM_INSTALL_DIR:-$HOME/.local/bin}"
STATE_DIR="${MM_STATE_DIR:-$HOME/.local/share/mihomo-manager}"
STATE_FILE="$STATE_DIR/install-state"
DEFAULT_CORE="$HOME/.local/bin/mihomo"
DEFAULT_CONFIG_DIR="$HOME/.config/mihomo"

PURGE_CORE=0
PURGE_CONFIG=0
ASSUME_YES=0
CORE_PURGED=0

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

usage() {
    cat <<'EOF'
Mihomo Manager 卸载器

用法：
  ./scripts/uninstall.sh [选项]

选项：
  --purge         删除可确认由安装器管理的 mihomo core
  --purge-config  删除用户配置目录（高风险）
  --yes           跳过 --purge-config 的确认
  -h, --help      显示帮助

默认保留 mihomo core 与 ~/.config/mihomo。
EOF
}

parse_args() {
    case "${MM_ASSUME_YES:-0}" in
        1|true|TRUE|yes|YES) ASSUME_YES=1 ;;
    esac

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --purge) PURGE_CORE=1 ;;
            --purge-config) PURGE_CONFIG=1 ;;
            --yes) ASSUME_YES=1 ;;
            -h|--help)
                usage
                exit 0
                ;;
            *) die "未知参数: $1" ;;
        esac
        shift
    done
}

state_value() {
    local key="$1"
    [[ -r "$STATE_FILE" ]] || return 1
    awk -F= -v wanted="$key" '$1 == wanted { print substr($0, index($0, "=") + 1); exit }' "$STATE_FILE"
}

remove_mm() {
    local recorded target
    recorded="$(state_value mm_path 2>/dev/null || true)"
    target="${recorded:-$INSTALL_DIR/mm}"

    if [[ "$target" != "$INSTALL_DIR/mm" ]]; then
        warn "状态中的 mm 路径超出预期，已保留: $target"
        target="$INSTALL_DIR/mm"
    fi

    if [[ -e "$target" || -L "$target" ]]; then
        rm -f -- "$target"
        success "已删除 mm: $target"
    else
        info "mm 未安装或已删除: $target"
    fi
}

remove_path_block() {
    local file="$1"
    local temp_file
    [[ -f "$file" ]] || return 0
    grep -q '^# >>> mihomo-manager PATH >>>$' "$file" || return 0

    temp_file="$(mktemp "${file}.mihomo-manager.XXXXXX")"
    if ! awk '
        BEGIN { in_block = 0 }
        $0 == "# >>> mihomo-manager PATH >>>" {
            if (in_block) exit 2
            in_block = 1
            next
        }
        $0 == "# <<< mihomo-manager PATH <<<" {
            if (!in_block) exit 3
            in_block = 0
            next
        }
        !in_block { print }
        END { if (in_block) exit 4 }
    ' "$file" >"$temp_file"; then
        rm -f -- "$temp_file"
        warn "PATH 标记块不完整，已保留并请手动检查: $file"
        return 0
    fi
    chmod --reference="$file" "$temp_file"
    mv -f -- "$temp_file" "$file"
    success "已移除 PATH 配置: $file"
}

remove_path_configuration() {
    local recorded
    recorded="$(state_value path_rc 2>/dev/null || true)"
    if [[ -n "$recorded" && "$recorded" != "$HOME/.bashrc" && "$recorded" != "$HOME/.zshrc" ]]; then
        warn "状态中的 shell 配置路径超出预期，已保留: $recorded"
    fi
    remove_path_block "$HOME/.bashrc"
    remove_path_block "$HOME/.zshrc"
}

remove_toolchain() {
    local recorded default_path
    recorded="$(state_value go_toolchain_path 2>/dev/null || true)"
    default_path="$STATE_DIR/toolchains/go1.26.4"

    if [[ -n "$recorded" ]]; then
        if [[ "$recorded" == "$default_path" ]]; then
            if [[ -d "$recorded" ]]; then
                rm -rf -- "$recorded"
                success "已删除隔离 Go 工具链: $recorded"
            fi
        else
            warn "状态中的 Go 工具链路径超出预期，已保留: $recorded"
        fi
    elif [[ -d "$default_path" ]]; then
        rm -rf -- "$default_path"
        success "已删除隔离 Go 工具链: $default_path"
    fi

    [[ -d "$STATE_DIR/toolchains" ]] && rmdir "$STATE_DIR/toolchains" 2>/dev/null || true
}

purge_core_if_owned() {
    ((PURGE_CORE == 1)) || return 0

    local managed path
    managed="$(state_value core_managed 2>/dev/null || printf '0')"
    path="$(state_value core_path 2>/dev/null || true)"

    if [[ "$managed" != "1" ]]; then
        warn "无法确认 mihomo core 由安装器管理，已保留。"
        return 0
    fi
    if [[ "$path" != "$DEFAULT_CORE" ]]; then
        warn "core 路径不是预期默认路径，已保留: ${path:-未知}"
        return 0
    fi

    if [[ -e "$path" || -L "$path" ]]; then
        rm -f -- "$path"
        success "已删除安装器管理的 mihomo core: $path"
    else
        info "mihomo core 已不存在: $path"
    fi
    CORE_PURGED=1
    [[ -e "$path.bak" || -L "$path.bak" ]] && info "旧 core 备份已保留: $path.bak"
}

confirm_config_purge() {
    local path="$1"
    ((ASSUME_YES == 1)) && return 0

    printf '即将永久删除配置目录: %s\n' "$path" >&2
    local answer=""
    if ! read -r -p "确认删除？[y/N] " answer </dev/tty; then
        die "当前环境无法交互确认。请增加 --yes。"
    fi
    case "$answer" in
        y|Y|yes|YES) ;;
        *) die "用户取消删除配置。" ;;
    esac
}

purge_config_if_requested() {
    ((PURGE_CONFIG == 1)) || return 0

    local recorded path
    recorded="$(state_value config_dir 2>/dev/null || true)"
    path="${CONFIG_DIR:-${recorded:-$DEFAULT_CONFIG_DIR}}"

    [[ -n "$path" && "$path" != "/" && "$path" != "$HOME" ]] || die "拒绝删除危险配置路径: $path"
    case "$path" in
        "$HOME"/*) ;;
        *) die "配置目录不在当前用户 HOME 下，拒绝自动删除: $path" ;;
    esac

    if [[ -d "$path" ]]; then
        confirm_config_purge "$path"
        rm -rf -- "$path"
        success "已删除配置目录: $path"
    else
        info "配置目录不存在: $path"
    fi
}

cleanup_legacy_launchd() {
    local launchd_dir="$HOME/Library/LaunchAgents"
    local plist
    for plist in com.mihomo.monitor.plist com.openclaw.mihomo-monitor.plist; do
        if [[ -f "$launchd_dir/$plist" ]]; then
            if command -v launchctl >/dev/null 2>&1; then
                launchctl unload "$launchd_dir/$plist" 2>/dev/null || true
            fi
            rm -f -- "$launchd_dir/$plist"
            info "已清理旧 launchd 配置: $plist"
        fi
    done
}

cleanup_state() {
    local managed="0"
    managed="$(state_value core_managed 2>/dev/null || printf '0')"

    if ((CORE_PURGED == 1)) || [[ "$managed" != "1" ]]; then
        rm -f -- "$STATE_FILE"
    else
        info "保留安装状态，用于后续安全清理 mihomo core: $STATE_FILE"
    fi
    rmdir "$STATE_DIR" 2>/dev/null || true
}

main() {
    parse_args "$@"
    local effective_uid="${MM_EUID_OVERRIDE:-$EUID}"
    [[ "$effective_uid" != "0" ]] || die "请以普通用户运行卸载器，不要使用 sudo 执行整个脚本。"

    remove_mm
    remove_path_configuration
    remove_toolchain
    purge_core_if_owned
    purge_config_if_requested
    cleanup_legacy_launchd
    cleanup_state

    printf '\n'
    success "Mihomo Manager 卸载完成。"
    ((PURGE_CORE == 0)) && printf 'mihomo core 已保留: %s\n' "$DEFAULT_CORE"
    ((PURGE_CONFIG == 0)) && printf '用户配置已保留: %s\n' "$DEFAULT_CONFIG_DIR"
}

main "$@"
