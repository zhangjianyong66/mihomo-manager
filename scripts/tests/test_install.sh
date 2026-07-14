#!/usr/bin/env bash

set -Eeuo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/mihomo-manager-install-test.XXXXXX")"
PASSED=0

cleanup() {
    rm -rf -- "$TEST_ROOT"
}

trap cleanup EXIT

pass() {
    PASSED=$((PASSED + 1))
    printf '[通过] %s\n' "$1"
}

fail() {
    printf '[失败] %s\n' "$1" >&2
    exit 1
}

assert_file_exists() {
    [[ -f "$1" ]] || fail "文件不存在: $1"
}

assert_not_exists() {
    [[ ! -e "$1" && ! -L "$1" ]] || fail "路径不应存在: $1"
}

test_pure_helpers() {
    (
        MM_INSTALL_LIB_ONLY=1 source "$PROJECT_DIR/scripts/install.sh"
        [[ "$(normalize_arch x86_64)" == "amd64" ]]
        [[ "$(normalize_arch aarch64)" == "arm64" ]]
        if normalize_arch riscv64 >/dev/null 2>&1; then
            exit 1
        fi
        semver_ge v1.19.28 v1.19.28
        semver_ge v1.20.0 v1.19.28
        if semver_ge v1.19.27 v1.19.28; then
            exit 1
        fi
        [[ "$(normalize_version 1.19.28)" == "v1.19.28" ]]
        if normalize_version latest >/dev/null 2>&1; then
            exit 1
        fi
        true
    )
    pass "架构与版本辅助函数"
}

test_preflight_rejections_are_clean() {
    local root_home="$TEST_ROOT/root-home"
    mkdir -p "$root_home"
    if HOME="$root_home" MM_EUID_OVERRIDE=0 bash "$PROJECT_DIR/scripts/install.sh" --yes >/dev/null 2>&1; then
        fail "root 预检应失败"
    fi
    assert_not_exists "$root_home/.local"

    local other_home="$TEST_ROOT/other-home"
    local os_file="$TEST_ROOT/other-os-release"
    mkdir -p "$other_home"
    printf 'ID=fedora\nID_LIKE="rhel"\n' >"$os_file"
    if HOME="$other_home" MM_OS_RELEASE_FILE="$os_file" bash "$PROJECT_DIR/scripts/install.sh" --yes >/dev/null 2>&1; then
        fail "非 Debian 平台预检应失败"
    fi
    assert_not_exists "$other_home/.local"

    local arch_home="$TEST_ROOT/arch-home"
    local ubuntu_file="$TEST_ROOT/ubuntu-os-release"
    mkdir -p "$arch_home"
    printf 'ID=ubuntu\nID_LIKE="debian"\n' >"$ubuntu_file"
    if HOME="$arch_home" MM_OS_RELEASE_FILE="$ubuntu_file" MM_ARCH=riscv64 \
        bash "$PROJECT_DIR/scripts/install.sh" --yes >/dev/null 2>&1; then
        fail "不支持架构预检应失败"
    fi
    assert_not_exists "$arch_home/.local"
    pass "root、平台和架构预检无持久化变更"
}

test_path_is_idempotent() {
    local home="$TEST_ROOT/path-home"
    mkdir -p "$home"
    (
        export HOME="$home"
        export SHELL=/bin/bash
        export PATH=/usr/bin:/bin
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        configure_path
        configure_path
        [[ "$(grep -c '^# >>> mihomo-manager PATH >>>$' "$HOME/.bashrc")" == "1" ]]
        [[ "$PATH_RC_MODIFIED" == "$HOME/.bashrc" ]]
    )
    pass "Bash PATH 配置幂等"
}

test_default_config_preserves_existing_file() {
    local home="$TEST_ROOT/config-home"
    local fake_core="$home/.local/bin/mihomo"
    mkdir -p "$(dirname "$fake_core")"
    printf '%s\n' '#!/usr/bin/env bash' 'exit 0' >"$fake_core"
    chmod +x "$fake_core"

    (
        export HOME="$home"
        export CONFIG_DIR="$home/.config/mihomo"
        export MIHOMO_BIN="$fake_core"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        configure_mihomo
        assert_file_exists "$CONFIG_DIR/config.yaml"
        printf '%s\n' '# 用户保留内容' >>"$CONFIG_DIR/config.yaml"
        local before after
        before="$(sha256sum "$CONFIG_DIR/config.yaml" | awk '{print $1}')"
        configure_mihomo
        after="$(sha256sum "$CONFIG_DIR/config.yaml" | awk '{print $1}')"
        [[ "$before" == "$after" ]]
    )
    pass "默认配置只创建一次且不覆盖已有文件"
}

test_go_selection_prefers_valid_system_then_managed() {
    local home="$TEST_ROOT/go-home"
    local fake_bin="$home/fake-bin"
    local managed_go="$home/.local/share/mihomo-manager/toolchains/go1.26.4/bin/go"
    mkdir -p "$fake_bin" "$(dirname "$managed_go")"
    printf '%s\n' '#!/usr/bin/env bash' 'printf "%s\n" "go version go1.21.9 linux/amd64"' >"$fake_bin/go"
    printf '%s\n' '#!/usr/bin/env bash' 'printf "%s\n" "go version go1.26.4 linux/amd64"' >"$managed_go"
    chmod +x "$fake_bin/go" "$managed_go"

    (
        export HOME="$home"
        export PATH="$fake_bin:/usr/bin:/bin"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        select_go
        [[ "$GO_BIN" == "$managed_go" ]]

        printf '%s\n' '#!/usr/bin/env bash' 'printf "%s\n" "go version go1.22.0 linux/amd64"' >"$fake_bin/go"
        chmod +x "$fake_bin/go"
        GO_BIN=""
        select_go
        [[ "$GO_BIN" == "$fake_bin/go" ]]
    )
    pass "Go 选择优先复用合格系统版本，否则使用隔离工具链"
}

test_newer_core_is_not_downgraded() {
    local home="$TEST_ROOT/core-home"
    local fake_core="$home/.local/bin/mihomo"
    mkdir -p "$(dirname "$fake_core")"
    printf '%s\n' '#!/usr/bin/env bash' 'printf "%s\n" "Mihomo Meta v9.0.0 linux amd64"' >"$fake_core"
    chmod +x "$fake_core"

    (
        export HOME="$home"
        export MIHOMO_BIN="$fake_core"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        TARGET_ARCH=amd64
        install_mihomo_core
        [[ "$CORE_VERSION" == "v9.0.0" ]]
        [[ "$CORE_MANAGED" == "0" ]]
        [[ ! -e "$fake_core.bak" ]]
    )
    pass "已有高版本 core 不降级"
}

test_force_core_replaces_after_validation_and_keeps_backup() {
    local home="$TEST_ROOT/force-core-home"
    local target="$home/.local/bin/mihomo"
    local new_core="$TEST_ROOT/new-mihomo"
    local fixture_archive="$TEST_ROOT/new-mihomo.gz"
    mkdir -p "$(dirname "$target")"
    printf '%s\n' '#!/usr/bin/env bash' 'printf "%s\n" "Mihomo Meta v9.0.0 linux amd64"' >"$target"
    printf '%s\n' '#!/usr/bin/env bash' 'printf "%s\n" "Mihomo Meta v1.19.28 linux amd64"' >"$new_core"
    chmod +x "$target" "$new_core"
    gzip -c "$new_core" >"$fixture_archive"

    (
        export HOME="$home"
        export MIHOMO_BIN="$target"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        TARGET_ARCH=amd64
        FORCE_CORE=1
        fetch_core_metadata() {
            CORE_ASSET_URL="file://$fixture_archive"
            CORE_ASSET_SHA256="$(sha256sum "$fixture_archive" | awk '{print $1}')"
        }
        install_mihomo_core
        [[ "$($target -v)" == *"v1.19.28"* ]]
        [[ "$($target.bak -v)" == *"v9.0.0"* ]]
        [[ "$CORE_MANAGED" == "1" ]]
    )
    pass "--force-core 校验后替换并保留旧版本备份"
}

test_sha_failure_is_detected() {
    local file="$TEST_ROOT/checksum.txt"
    printf 'fixture\n' >"$file"
    (
        MM_INSTALL_LIB_ONLY=1 source "$PROJECT_DIR/scripts/install.sh"
        if verify_sha256 "$file" "0000000000000000000000000000000000000000000000000000000000000000"; then
            exit 1
        fi
        true
    )
    pass "错误 SHA-256 被拒绝"
}

test_bootstrap_uses_archive_and_cleans_up() {
    local mirror="$TEST_ROOT/mirror"
    local fixture="$TEST_ROOT/bootstrap-fixture/mihomo-manager-master"
    local marker="$TEST_ROOT/bootstrap-marker"
    local tmpdir="$TEST_ROOT/bootstrap-tmp"
    local archive_dir="$mirror/zhangjianyong66/mihomo-manager/archive"
    mkdir -p "$fixture/scripts" "$archive_dir" "$tmpdir"
    printf '%s\n' \
        '#!/usr/bin/env bash' \
        'printf "%s" "$*" >"$BOOTSTRAP_MARKER"' >"$fixture/scripts/install.sh"
    chmod +x "$fixture/scripts/install.sh"
    tar -czf "$archive_dir/master.tar.gz" -C "$(dirname "$fixture")" "$(basename "$fixture")"

    HOME="$TEST_ROOT/bootstrap-home" \
        TMPDIR="$tmpdir" \
        BOOTSTRAP_MARKER="$marker" \
        MM_GITHUB_BASE_URL="file://$mirror" \
        bash "$PROJECT_DIR/scripts/bootstrap.sh" --yes >/dev/null

    assert_file_exists "$marker"
    [[ "$(cat "$marker")" == "--yes" ]]
    [[ -z "$(find "$tmpdir" -mindepth 1 -maxdepth 1 -print -quit)" ]]
    pass "远程引导使用源码归档、透传参数并清理临时目录"
}

test_default_uninstall_preserves_core_and_config() {
    local home="$TEST_ROOT/uninstall-home"
    local state_dir="$home/.local/share/mihomo-manager"
    mkdir -p "$home/.local/bin" "$state_dir/toolchains/go1.26.4" "$home/.config/mihomo"
    printf 'mm\n' >"$home/.local/bin/mm"
    printf 'core\n' >"$home/.local/bin/mihomo"
    printf 'config\n' >"$home/.config/mihomo/config.yaml"
    printf '%s\n' \
        '# >>> mihomo-manager PATH >>>' \
        'export PATH="$HOME/.local/bin:$PATH"' \
        '# <<< mihomo-manager PATH <<<' >"$home/.bashrc"
    printf '%s\n' \
        "installer_version=1" \
        "mm_path=$home/.local/bin/mm" \
        "go_toolchain_path=$state_dir/toolchains/go1.26.4" \
        "path_rc=$home/.bashrc" \
        "core_managed=1" \
        "core_path=$home/.local/bin/mihomo" \
        "core_version=v1.19.28" \
        "config_dir=$home/.config/mihomo" >"$state_dir/install-state"

    HOME="$home" SHELL=/bin/bash bash "$PROJECT_DIR/scripts/uninstall.sh" >/dev/null

    assert_not_exists "$home/.local/bin/mm"
    assert_not_exists "$state_dir/toolchains/go1.26.4"
    assert_file_exists "$home/.local/bin/mihomo"
    assert_file_exists "$home/.config/mihomo/config.yaml"
    if grep -q '^# >>> mihomo-manager PATH >>>$' "$home/.bashrc"; then
        fail "卸载后 PATH 标记仍存在"
    fi
    assert_file_exists "$state_dir/install-state"
    pass "默认卸载删除 mm/工具链/PATH，保留 core 与配置"
}

test_purge_core_requires_ownership() {
    local home="$TEST_ROOT/purge-home"
    local state_dir="$home/.local/share/mihomo-manager"
    mkdir -p "$home/.local/bin" "$state_dir" "$home/.config/mihomo"
    printf 'core\n' >"$home/.local/bin/mihomo"
    printf 'old-core\n' >"$home/.local/bin/mihomo.bak"
    printf 'config\n' >"$home/.config/mihomo/config.yaml"
    printf '%s\n' \
        "installer_version=1" \
        "mm_path=$home/.local/bin/mm" \
        "go_toolchain_path=" \
        "path_rc=" \
        "core_managed=1" \
        "core_path=$home/.local/bin/mihomo" \
        "core_version=v1.19.28" \
        "config_dir=$home/.config/mihomo" >"$state_dir/install-state"

    HOME="$home" bash "$PROJECT_DIR/scripts/uninstall.sh" --purge --yes >/dev/null
    assert_not_exists "$home/.local/bin/mihomo"
    assert_file_exists "$home/.local/bin/mihomo.bak"
    assert_file_exists "$home/.config/mihomo/config.yaml"
    assert_not_exists "$state_dir/install-state"
    pass "--purge 只清理有归属记录的 core，并保留旧 core 备份"
}

test_rejected_purge_keeps_ownership_state() {
    local home="$TEST_ROOT/rejected-purge-home"
    local state_dir="$home/.local/share/mihomo-manager"
    local custom_core="$home/custom/mihomo"
    mkdir -p "$(dirname "$custom_core")" "$state_dir"
    printf 'core\n' >"$custom_core"
    printf '%s\n' \
        "installer_version=1" \
        "mm_path=$home/.local/bin/mm" \
        "go_toolchain_path=" \
        "path_rc=" \
        "core_managed=1" \
        "core_path=$custom_core" \
        "core_version=v1.19.28" \
        "config_dir=$home/.config/mihomo" >"$state_dir/install-state"

    HOME="$home" bash "$PROJECT_DIR/scripts/uninstall.sh" --purge --yes >/dev/null 2>&1
    assert_file_exists "$custom_core"
    assert_file_exists "$state_dir/install-state"
    pass "拒绝非默认 core purge 时保留文件与归属状态"
}

test_malformed_path_block_is_not_truncated() {
    local home="$TEST_ROOT/malformed-path-home"
    mkdir -p "$home"
    printf '%s\n' \
        '# >>> mihomo-manager PATH >>>' \
        'export PATH="$HOME/.local/bin:$PATH"' \
        '# 用户后续配置必须保留' >"$home/.bashrc"

    HOME="$home" SHELL=/bin/bash bash "$PROJECT_DIR/scripts/uninstall.sh" >/dev/null 2>&1
    grep -q '^# >>> mihomo-manager PATH >>>$' "$home/.bashrc"
    grep -q '^# 用户后续配置必须保留$' "$home/.bashrc"
    pass "不完整 PATH 标记块不会导致 shell 配置被截断"
}

test_pure_helpers
test_preflight_rejections_are_clean
test_path_is_idempotent
test_default_config_preserves_existing_file
test_go_selection_prefers_valid_system_then_managed
test_newer_core_is_not_downgraded
test_force_core_replaces_after_validation_and_keeps_backup
test_sha_failure_is_detected
test_bootstrap_uses_archive_and_cleans_up
test_default_uninstall_preserves_core_and_config
test_purge_core_requires_ownership
test_rejected_purge_keeps_ownership_state
test_malformed_path_block_is_not_truncated

printf '\n全部安装测试通过，共 %d 项。\n' "$PASSED"
