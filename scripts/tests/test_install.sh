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

assert_file_content() {
    local got
    got="$(cat "$1")"
    [[ "$got" == "$2" ]] || fail "文件内容不符: $1; got=$got want=$2"
}

write_fake_core() {
    local path="$1"
    mkdir -p "$(dirname "$path")"
    printf '%s\n' '#!/usr/bin/env bash' 'exit 0' >"$path"
    chmod +x "$path"
}

write_fake_mm() {
    local path="$1"
    mkdir -p "$(dirname "$path")"
    printf '%s\n' \
        '#!/usr/bin/env bash' \
        'printf "%s\n" "$*" >>"$MM_FAKE_LOG"' \
        '[[ -z "${MM_FAKE_ENV_LOG:-}" ]] || printf "%s|%s|%s\n" "$CONFIG_DIR" "$MIHOMO_BIN" "$MIHOMO_API_PORT" >>"$MM_FAKE_ENV_LOG"' \
        '[[ "${MM_FAKE_FAIL_ACTION:-}" != "$1 $2" ]] || exit 9' \
        'case "$1 $2" in' \
	        '  "daemon enable") printf '\''{"apiVersion":"mm/v1","kind":"DaemonControl","data":{"backend":"%s","installed":true,"enabled":%s,"active":%s,"message":"fixture","hint":"运行 mm daemon run"},"warnings":[]}\n'\'' "${MM_FAKE_BACKEND:-}" "$MM_FAKE_ENABLED" "$MM_FAKE_ENABLED" ;;' \
        '  "daemon start"|"daemon stop") printf '\''{"apiVersion":"mm/v1","kind":"DaemonControl","data":{"installed":true,"enabled":true,"active":true,"message":"fixture"},"warnings":[]}\n'\'' ;;' \
        '  "daemon status") printf '\''{"apiVersion":"mm/v1","kind":"DaemonStatus","data":{"state":"running","core":{"state":"%s"}},"warnings":[]}\n'\'' "$MM_FAKE_CORE_STATE" ;;' \
        '  "migrate apply") printf '\''{"apiVersion":"mm/v1","kind":"MigrationApply","data":{"migration":{"state":"succeeded"}},"warnings":[]}\n'\'' ;;' \
        '  *) exit 2 ;;' \
        'esac' >"$path"
    chmod +x "$path"
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

test_macos_preflight_matrix() {
	local arch
	for arch in amd64 arm64; do
		(
			export HOME="$TEST_ROOT/macos-preflight-$arch-home"
			export MM_EUID_OVERRIDE=1000
			export MM_OS_OVERRIDE=Darwin
			export MM_MACOS_VERSION_OVERRIDE=12.0
			export MM_ARCH="$arch"
			export MM_INSTALL_LIB_ONLY=1
			source "$PROJECT_DIR/scripts/install.sh"
			preflight
			[[ "$TARGET_OS" == "darwin" && "$TARGET_ARCH" == "$arch" ]]
		) >/dev/null
	done

    local old_home="$TEST_ROOT/macos-old-home"
    mkdir -p "$old_home"
    if HOME="$old_home" MM_EUID_OVERRIDE=1000 MM_OS_OVERRIDE=Darwin MM_MACOS_VERSION_OVERRIDE=11.7 MM_ARCH=arm64 \
        bash "$PROJECT_DIR/scripts/install.sh" --yes >/dev/null 2>&1; then
        fail "macOS 12 以下版本预检应失败"
    fi
    assert_not_exists "$old_home/.local"

    local arch_home="$TEST_ROOT/macos-arch-home"
    mkdir -p "$arch_home"
    if HOME="$arch_home" MM_EUID_OVERRIDE=1000 MM_OS_OVERRIDE=Darwin MM_MACOS_VERSION_OVERRIDE=14.0 MM_ARCH=riscv64 \
        bash "$PROJECT_DIR/scripts/install.sh" --yes >/dev/null 2>&1; then
        fail "Darwin 非 amd64/arm64 架构预检应失败"
    fi
    assert_not_exists "$arch_home/.local"
    pass "macOS 12+ 双架构预检且失败无持久化变更"
}

test_macos_homebrew_installs_only_missing_packages() {
    local log="$TEST_ROOT/brew.log"
    (
        export HOME="$TEST_ROOT/macos-brew-home"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        TARGET_OS=darwin
        ASSUME_YES=1
        command() {
            if [[ "$1" == "-v" && "$2" == "jq" ]]; then
                return 1
            fi
            if [[ "$1" == "-v" && "$2" == "brew" ]]; then
                return 0
            fi
            builtin command "$@"
        }
        brew() { printf '%s\n' "$*" >>"$log"; }
        install_macos_dependencies
    ) >/dev/null
    [[ "$(cat "$log")" == "install jq" ]]
    pass "macOS 仅通过现有 Homebrew 补齐缺失依赖"
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

test_macos_path_uses_zshrc_idempotently() {
    local home="$TEST_ROOT/macos-path-home"
    mkdir -p "$home"
    (
        export HOME="$home"
        export SHELL=/bin/bash
        export PATH=/usr/bin:/bin
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        TARGET_OS=darwin
        configure_path
        configure_path
        [[ "$(grep -c '^# >>> mihomo-manager PATH >>>$' "$HOME/.zshrc")" == "1" ]]
        [[ "$PATH_RC_MODIFIED" == "$HOME/.zshrc" ]]
        [[ ! -e "$HOME/.bashrc" ]]
    )
    pass "macOS Zsh PATH 配置幂等"
}

test_platform_asset_matrix() {
    (
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        TARGET_OS=darwin
        TARGET_ARCH=amd64
        [[ "$(go_archive_sha256)" == "05dc9b5f9997744520aaebb3d5deaa7c755371aebbfb7f97c2511a9f3367538d" ]]
        [[ "$(core_asset_name v1.19.28)" == "mihomo-darwin-amd64-v1-v1.19.28.gz" ]]
        TARGET_ARCH=arm64
        [[ "$(go_archive_sha256)" == "b62ad2b6d7d2464f12a5bcad7ff47f19d08325773b5efd21610e445a05a9bf53" ]]
        [[ "$(core_asset_name v1.19.28)" == "mihomo-darwin-arm64-v1.19.28.gz" ]]
        TARGET_OS=linux
        [[ "$(core_asset_name v1.19.28)" == "mihomo-linux-arm64-v1.19.28.gz" ]]
    )
    pass "Go 与 mihomo 资产按 Linux/macOS 双架构选择"
}

test_macos_daemon_and_state_round_trip() {
	local home="$TEST_ROOT/macos-state-home"
	local state_dir="$home/.local/share/mihomo-manager"
	local mm="$home/.local/bin/mm"
	local plist="$home/Library/LaunchAgents/com.zhangjianyong.mihomo-manager.daemon.plist"
	local log="$home/mm.log"
	mkdir -p "$(dirname "$plist")" "$state_dir"
	write_fake_mm "$mm"
	printf '%s\n' '<string>com.zhangjianyong.mihomo-manager.daemon</string>' >"$plist"
	chmod 0600 "$plist"

	(
		export HOME="$home"
		export MM_STATE_DIR="$state_dir"
		export MM_INSTALL_DIR="$home/.local/bin"
		export CONFIG_DIR="$home/.config/mihomo"
		export MIHOMO_BIN="$home/.local/bin/mihomo"
		export MM_FAKE_LOG="$log"
		export MM_FAKE_ENABLED=true
		export MM_FAKE_CORE_STATE=stopped
		export MM_FAKE_BACKEND=launchd
		export MM_INSTALL_LIB_ONLY=1
		source "$PROJECT_DIR/scripts/install.sh"
		TARGET_OS=darwin
		CONFIG_CREATED=0
		CORE_VERSION=v1.19.28
		configure_daemon_and_migration
		write_state
	) >/dev/null

	grep -Fxq 'platform=darwin' "$state_dir/install-state"
	grep -Fxq 'daemon_backend=launchd' "$state_dir/install-state"
	grep -Fxq "daemon_config_path=$plist" "$state_dir/install-state"
	grep -Fxq "daemon_config_sha256=$(sha256sum "$plist" | awk '{print $1}')" "$state_dir/install-state"
	[[ "$(stat -c '%a' "$state_dir/install-state")" == "600" ]]
	[[ "$(cat "$log")" == $'daemon enable --output json\ndaemon start --output json\ndaemon status --output json' ]]
	pass "macOS launchd 编排与 install-state v3 往返"
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
        [[ "$CONFIG_CREATED" == "1" ]]
        grep -Fxq 'mixed-port: 7890' "$CONFIG_DIR/config.yaml"
        printf '%s\n' '# 用户保留内容' >>"$CONFIG_DIR/config.yaml"
        local before after
        before="$(sha256sum "$CONFIG_DIR/config.yaml" | awk '{print $1}')"
        configure_mihomo
        [[ "$CONFIG_CREATED" == "0" ]]
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
    local expected
    printf 'fixture\n' >"$file"
	expected="$(shasum -a 256 "$file" | awk '{print $1}')"
    (
        MM_INSTALL_LIB_ONLY=1 source "$PROJECT_DIR/scripts/install.sh"
        if verify_sha256 "$file" "0000000000000000000000000000000000000000000000000000000000000000"; then
            exit 1
        fi
		command() {
			if [[ "$1" == "-v" && "$2" == "sha256sum" ]]; then
				return 1
			fi
			builtin command "$@"
		}
		verify_sha256 "$file" "$expected"
        true
    )
    pass "错误 SHA-256 被拒绝且兼容 shasum"
}

test_ruleset_override_requires_trusted_digests() {
    if (
        export HOME="$TEST_ROOT/ruleset-settings-home"
        export MM_RULESET_BASE_URL="file://$TEST_ROOT/custom-rulesets"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        validate_ruleset_settings
    ) >/dev/null 2>&1; then
        fail "自定义规则集来源缺少摘要时应失败"
    fi
    pass "自定义规则集来源必须同时提供可信摘要"

    if (
        export HOME="$TEST_ROOT/ruleset-credential-home"
        export MM_RULESET_BASE_URL="https://user:password@example.com/rules"
        export MM_RULESET_DOMAIN_SHA256="$(printf domain | sha256sum | awk '{print $1}')"
        export MM_RULESET_IP_SHA256="$(printf ip | sha256sum | awk '{print $1}')"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        validate_ruleset_settings
    ) >/dev/null 2>&1; then
        fail "包含凭据的规则集来源应被拒绝"
    fi
    pass "规则集来源拒绝可能泄露的凭据和查询信息"
}

test_ruleset_path_rejects_symlink() {
    local home="$TEST_ROOT/ruleset-symlink-home"
    local config="$home/.config/mihomo"
    mkdir -p "$config" "$home/redirected-rulesets"
    ln -s "$home/redirected-rulesets" "$config/rulesets"
    if (
        export HOME="$home"
        export CONFIG_DIR="$config"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        validate_ruleset_settings
        prepare_install_state_dir
        prepare_ruleset_dir
    ) >/dev/null 2>&1; then
        fail "规则集符号链接目录应被拒绝"
    fi
    [[ "$(stat -c '%a' "$home/.local/share/mihomo-manager")" == "700" ]]
    pass "规则集目录拒绝符号链接"

    if (
        export HOME="$home"
        export CONFIG_DIR="//$home/config"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        validate_ruleset_settings
        prepare_ruleset_dir
    ) >/dev/null 2>&1; then
        fail "包含重复分隔符的 CONFIG_DIR 应被拒绝"
    fi
    pass "CONFIG_DIR 拒绝重复路径分隔符"

    local state_home="$TEST_ROOT/state-symlink-home"
    mkdir -p "$state_home/.local/share" "$state_home/redirected-state"
    ln -s "$state_home/redirected-state" "$state_home/.local/share/mihomo-manager"
    if (
        export HOME="$state_home"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        prepare_install_state_dir
    ) >/dev/null 2>&1; then
        fail "安装状态目录符号链接应被拒绝"
    fi
    pass "安装状态目录使用 0700 且拒绝符号链接"
}

test_ruleset_install_and_state_round_trip() {
    local home="$TEST_ROOT/ruleset-install-home"
    local config="$home/.config/mihomo"
    local mirror="$TEST_ROOT/ruleset-mirror"
    local ref="fixture-ref"
    local domain_source="$mirror/$ref/geo/geosite/cn.mrs"
    local ip_source="$mirror/$ref/geo/geoip/cn.mrs"
    local fake_core="$home/.local/bin/mihomo"
    mkdir -p "$(dirname "$domain_source")" "$(dirname "$ip_source")"
    printf 'domain-fixture\n' >"$domain_source"
    printf 'ip-fixture\n' >"$ip_source"
    write_fake_core "$fake_core"

    (
        export HOME="$home"
        export CONFIG_DIR="$config"
        export MIHOMO_BIN="$fake_core"
        export MM_RULESET_BASE_URL="file://$mirror"
        export MM_RULESET_REF="$ref"
        export MM_RULESET_DOMAIN_SHA256="$(sha256sum "$domain_source" | awk '{print $1}')"
        export MM_RULESET_IP_SHA256="$(sha256sum "$ip_source" | awk '{print $1}')"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        validate_ruleset_settings
        prepare_ruleset_dir
        install_rulesets
        assert_file_content "$RULESET_DOMAIN_PATH" "domain-fixture"
        assert_file_content "$RULESET_IP_PATH" "ip-fixture"
        [[ "$(stat -c '%a' "$RULESET_DIR")" == "700" ]]
        [[ "$(stat -c '%a' "$RULESET_DOMAIN_PATH")" == "600" ]]
        [[ "$(stat -c '%a' "$RULESET_IP_PATH")" == "600" ]]

        GO_BIN=/usr/bin/go
        CORE_VERSION=v1.19.28
        CORE_MANAGED=1
        write_state
        [[ "$(state_value installer_version)" == "3" ]]
        [[ "$(state_value ruleset_ref)" == "$ref" ]]
        [[ "$(state_value ruleset_domain_path)" == "$RULESET_DOMAIN_PATH" ]]
        [[ "$(state_value ruleset_ip_sha256)" == "$MM_RULESET_IP_SHA256" ]]
        [[ "$(stat -c '%a' "$STATE_DIR")" == "700" ]]
        [[ "$(stat -c '%a' "$STATE_FILE")" == "600" ]]
    )
    pass "规则集事务安装、权限和 install-state round-trip"
}

test_ruleset_publish_failure_restores_pair() {
    local home="$TEST_ROOT/ruleset-rollback-home"
    local config="$home/.config/mihomo"
    local fake_core="$home/.local/bin/mihomo"
    write_fake_core "$fake_core"
    (
        export HOME="$home"
        export CONFIG_DIR="$config"
        export MIHOMO_BIN="$fake_core"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        validate_ruleset_settings
        prepare_ruleset_dir
        printf 'old-domain\n' >"$RULESET_DOMAIN_PATH"
        printf 'old-ip\n' >"$RULESET_IP_PATH"
        local domain_temp ip_temp publish_calls=0
        domain_temp="$(mktemp "$RULESET_DIR/.domain-new.XXXXXX")"
        ip_temp="$(mktemp "$RULESET_DIR/.ip-new.XXXXXX")"
        printf 'new-domain\n' >"$domain_temp"
        printf 'new-ip\n' >"$ip_temp"
        ruleset_publish_file() {
            publish_calls=$((publish_calls + 1))
            if ((publish_calls == 2)); then
                return 1
            fi
            mv -f -- "$1" "$2"
        }
        if publish_ruleset_pair "$domain_temp" "$ip_temp"; then
            exit 1
        fi
        assert_file_content "$RULESET_DOMAIN_PATH" "old-domain"
        assert_file_content "$RULESET_IP_PATH" "old-ip"
    )
    pass "第二份规则集发布失败时恢复完整旧版本"
}

test_ruleset_download_failure_uses_only_verified_cache() {
    local home="$TEST_ROOT/ruleset-cache-home"
    local config="$home/.config/mihomo"
    local fake_core="$home/.local/bin/mihomo"
    local requested_mirror="$TEST_ROOT/partial-rulesets"
    local domain_sha ip_sha
    write_fake_core "$fake_core"
    mkdir -p "$config/rulesets" "$home/.local/share/mihomo-manager"
    printf 'cached-domain\n' >"$config/rulesets/cn-domain.mrs"
    printf 'cached-ip\n' >"$config/rulesets/cn-ip.mrs"
    mkdir -p "$requested_mirror/missing-ref/geo/geosite"
    printf 'requested-domain\n' >"$requested_mirror/missing-ref/geo/geosite/cn.mrs"
    domain_sha="$(sha256sum "$config/rulesets/cn-domain.mrs" | awk '{print $1}')"
    ip_sha="$(sha256sum "$config/rulesets/cn-ip.mrs" | awk '{print $1}')"
    printf '%s\n' \
        "installer_version=2" \
        "ruleset_base_url=file://previous" \
        "ruleset_ref=previous-ref" \
        "ruleset_domain_path=$config/rulesets/cn-domain.mrs" \
        "ruleset_domain_sha256=$domain_sha" \
        "ruleset_ip_path=$config/rulesets/cn-ip.mrs" \
        "ruleset_ip_sha256=$ip_sha" >"$home/.local/share/mihomo-manager/install-state"

    (
        export HOME="$home"
        export CONFIG_DIR="$config"
        export MIHOMO_BIN="$fake_core"
        export MM_RULESET_BASE_URL="file://$requested_mirror"
        export MM_RULESET_REF="missing-ref"
        export MM_RULESET_DOMAIN_SHA256="$(sha256sum "$requested_mirror/missing-ref/geo/geosite/cn.mrs" | awk '{print $1}')"
        export MM_RULESET_IP_SHA256="$(printf requested-ip | sha256sum | awk '{print $1}')"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        validate_ruleset_settings
        prepare_ruleset_dir
        install_rulesets
        assert_file_content "$RULESET_DOMAIN_PATH" "cached-domain"
        assert_file_content "$RULESET_IP_PATH" "cached-ip"
        [[ "$RULESET_INSTALLED_REF" == "previous-ref" ]]
    )

    local empty_home="$TEST_ROOT/ruleset-no-cache-home"
    write_fake_core "$empty_home/.local/bin/mihomo"
    if (
        export HOME="$empty_home"
        export CONFIG_DIR="$empty_home/.config/mihomo"
        export MIHOMO_BIN="$empty_home/.local/bin/mihomo"
        export MM_RULESET_BASE_URL="file://$TEST_ROOT/missing-rulesets"
        export MM_RULESET_REF="missing-ref"
        export MM_RULESET_DOMAIN_SHA256="$(printf requested-domain | sha256sum | awk '{print $1}')"
        export MM_RULESET_IP_SHA256="$(printf requested-ip | sha256sum | awk '{print $1}')"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        validate_ruleset_settings
        prepare_ruleset_dir
        install_rulesets
    ) >/dev/null 2>&1; then
        fail "无缓存规则集下载失败时安装应失败"
    fi
    pass "下载失败仅复用 install-state 可验证缓存，无缓存时硬失败"
}

test_daemon_fresh_install_starts_and_migrates() {
    local home="$TEST_ROOT/daemon-fresh-home"
    local log="$home/mm-calls.log"
    local env_log="$home/mm-env.log"
    mkdir -p "$home/.local/share/mihomo-manager"
    write_fake_mm "$home/.local/bin/mm"
    (
        export HOME="$home"
        export MM_FAKE_LOG="$log"
        export MM_FAKE_ENV_LOG="$env_log"
        export MM_FAKE_ENABLED=true
        export MM_FAKE_CORE_STATE=stopped
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        CONFIG_CREATED=1
        configure_daemon_and_migration
        [[ "$DAEMON_AVAILABLE" == "1" ]]
    )
    grep -q '^daemon enable --output json$' "$log"
    grep -q '^daemon start --output json$' "$log"
    grep -q '^daemon status --output json$' "$log"
    grep -q '^migrate apply --output json$' "$log"
    [[ "$(cat "$log")" == $'daemon enable --output json\ndaemon start --output json\ndaemon status --output json\nmigrate apply --output json' ]]
    [[ "$(sort -u "$env_log")" == "$home/.config/mihomo|$home/.local/bin/mihomo|9090" ]]
    if grep -q '^daemon stop ' "$log"; then
        fail "fresh install 不应先停止 daemon"
    fi
    pass "fresh 配置启用 daemon、确认 core stopped 并自动迁移"
}

test_daemon_failure_returns_nonzero_without_follow_up() {
    local home="$TEST_ROOT/daemon-failure-home"
    local log="$home/mm-calls.log"
    mkdir -p "$home/.local/share/mihomo-manager"
    write_fake_mm "$home/.local/bin/mm"
    if (
        export HOME="$home"
        export MM_FAKE_LOG="$log"
        export MM_FAKE_ENABLED=true
        export MM_FAKE_CORE_STATE=stopped
        export MM_FAKE_FAIL_ACTION="daemon start"
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        CONFIG_CREATED=1
        configure_daemon_and_migration
    ) >/dev/null 2>&1; then
        fail "daemon start 失败应返回非零"
    fi
    [[ "$(cat "$log")" == $'daemon enable --output json\ndaemon start --output json' ]]
    pass "daemon 控制失败返回非零且不继续 status/migrate"
}

test_daemon_stopped_upgrade_restarts_but_running_upgrade_does_not() {
    local stopped_home="$TEST_ROOT/daemon-stopped-home"
    local stopped_log="$stopped_home/mm-calls.log"
    mkdir -p "$stopped_home/.local/share/mihomo-manager"
    write_fake_mm "$stopped_home/.local/bin/mm"
    (
        export HOME="$stopped_home"
        export MM_FAKE_LOG="$stopped_log"
        export MM_FAKE_ENABLED=true
        export MM_FAKE_CORE_STATE=stopped
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        DAEMON_PREEXISTING=1
        DAEMON_PREVIOUS_CORE_STATE=stopped
        CONFIG_CREATED=0
        configure_daemon_and_migration
    )
    grep -q '^daemon stop --output json$' "$stopped_log"
    grep -q '^daemon start --output json$' "$stopped_log"
    [[ "$(grep -c '^daemon status --output json$' "$stopped_log")" == "2" ]]
    if grep -q '^migrate apply ' "$stopped_log"; then
        fail "已有配置不应自动迁移"
    fi

    local running_home="$TEST_ROOT/daemon-running-home"
    local running_log="$running_home/mm-calls.log"
    mkdir -p "$running_home/.local/share/mihomo-manager"
    write_fake_mm "$running_home/.local/bin/mm"
    (
        export HOME="$running_home"
        export MM_FAKE_LOG="$running_log"
        export MM_FAKE_ENABLED=true
        export MM_FAKE_CORE_STATE=running
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        probe_daemon_before_upgrade
        [[ "$DAEMON_PREEXISTING" == "1" && "$DAEMON_CORE_RUNNING" == "1" ]]
        CONFIG_CREATED=0
        configure_daemon_and_migration
    )
    grep -q '^daemon enable --output json$' "$running_log"
    if grep -Eq '^daemon (stop|start) ' "$running_log"; then
        fail "core running 升级不得 stop/start daemon"
    fi
    pass "core stopped 升级重启 daemon，core running 升级保持进程"
}

test_daemon_state_change_before_restart_is_preserved() {
    local home="$TEST_ROOT/daemon-state-change-home"
    local log="$home/mm-calls.log"
    mkdir -p "$home/.local/share/mihomo-manager"
    write_fake_mm "$home/.local/bin/mm"
    (
        export HOME="$home"
        export MM_FAKE_LOG="$log"
        export MM_FAKE_ENABLED=true
        export MM_FAKE_CORE_STATE=running
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        DAEMON_PREEXISTING=1
        DAEMON_PREVIOUS_CORE_STATE=stopped
        CONFIG_CREATED=0
        configure_daemon_and_migration
    )
    if grep -Eq '^daemon (stop|start) ' "$log"; then
        fail "stop 前 core 变为 running 时不得重启 daemon"
    fi
    pass "daemon 重启前复核 core 状态并保留并发启动的 core"
}

test_daemon_without_systemd_keeps_units_and_skips_migration() {
    local home="$TEST_ROOT/daemon-no-systemd-home"
    local log="$home/mm-calls.log"
    mkdir -p "$home/.local/share/mihomo-manager"
    write_fake_mm "$home/.local/bin/mm"
    (
        export HOME="$home"
        export MM_FAKE_LOG="$log"
        export MM_FAKE_ENABLED=false
        export MM_FAKE_CORE_STATE=stopped
        export MM_INSTALL_LIB_ONLY=1
        source "$PROJECT_DIR/scripts/install.sh"
        CONFIG_CREATED=1
        configure_daemon_and_migration
        [[ "$DAEMON_AVAILABLE" == "0" ]]
    )
    [[ "$(wc -l <"$log")" == "1" ]]
    grep -q '^daemon enable --output json$' "$log"
    pass "无 systemd user 会话时保留 unit 提示且不启动临时 daemon"
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
    mkdir -p "$home/.local/bin" "$state_dir/toolchains/go1.26.4" "$home/.config/mihomo/rulesets"
    printf 'mm\n' >"$home/.local/bin/mm"
    printf 'core\n' >"$home/.local/bin/mihomo"
    printf 'config\n' >"$home/.config/mihomo/config.yaml"
    printf 'domain\n' >"$home/.config/mihomo/rulesets/cn-domain.mrs"
    printf 'ip\n' >"$home/.config/mihomo/rulesets/cn-ip.mrs"
    printf '%s\n' \
        '# >>> mihomo-manager PATH >>>' \
        'export PATH="$HOME/.local/bin:$PATH"' \
        '# <<< mihomo-manager PATH <<<' >"$home/.bashrc"
    printf '%s\n' \
        "installer_version=2" \
        "mm_path=$home/.local/bin/mm" \
        "go_toolchain_path=$state_dir/toolchains/go1.26.4" \
        "path_rc=$home/.bashrc" \
        "core_managed=1" \
        "core_path=$home/.local/bin/mihomo" \
        "core_version=v1.19.28" \
        "config_dir=$home/.config/mihomo" \
        "ruleset_domain_path=$home/.config/mihomo/rulesets/cn-domain.mrs" \
        "ruleset_ip_path=$home/.config/mihomo/rulesets/cn-ip.mrs" >"$state_dir/install-state"

    HOME="$home" SHELL=/bin/bash bash "$PROJECT_DIR/scripts/uninstall.sh" >/dev/null

    assert_not_exists "$home/.local/bin/mm"
    assert_not_exists "$state_dir/toolchains/go1.26.4"
    assert_file_exists "$home/.local/bin/mihomo"
    assert_file_exists "$home/.config/mihomo/config.yaml"
    assert_file_exists "$home/.config/mihomo/rulesets/cn-domain.mrs"
    assert_file_exists "$home/.config/mihomo/rulesets/cn-ip.mrs"
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

test_macos_uninstall_removes_only_verified_manager_plist() {
    local home="$TEST_ROOT/macos-uninstall-home"
    local state_dir="$home/.local/share/mihomo-manager"
    local plist="$home/Library/LaunchAgents/com.zhangjianyong.mihomo-manager.daemon.plist"
    local mm="$home/.local/bin/mm"
    local disable_log="$home/disable.log"
    mkdir -p "$(dirname "$plist")" "$(dirname "$mm")" "$state_dir"
    printf '%s\n' \
        '<?xml version="1.0" encoding="UTF-8"?>' \
        '<plist><dict><key>Label</key><string>com.zhangjianyong.mihomo-manager.daemon</string></dict></plist>' >"$plist"
    printf '%s\n' '#!/usr/bin/env bash' 'printf "%s\n" "$*" >>"$MM_DISABLE_LOG"' >"$mm"
    chmod +x "$mm"
    local digest
    digest="$(sha256sum "$plist" | awk '{print $1}')"
    printf '%s\n' \
        'installer_version=3' \
        'platform=darwin' \
        'daemon_backend=launchd' \
        "daemon_config_path=$plist" \
        "daemon_config_sha256=$digest" \
        "mm_path=$mm" \
        'core_managed=0' >"$state_dir/install-state"

    HOME="$home" MM_DISABLE_LOG="$disable_log" bash "$PROJECT_DIR/scripts/uninstall.sh" >/dev/null
    assert_not_exists "$plist"
    assert_not_exists "$mm"
    [[ "$(cat "$disable_log")" == "daemon disable --output json" ]]

    local changed_home="$TEST_ROOT/macos-uninstall-modified-home"
    local changed_state="$changed_home/.local/share/mihomo-manager"
    local changed_plist="$changed_home/Library/LaunchAgents/com.zhangjianyong.mihomo-manager.daemon.plist"
    mkdir -p "$(dirname "$changed_plist")" "$changed_state"
    printf '%s\n' '<string>com.zhangjianyong.mihomo-manager.daemon</string>' >"$changed_plist"
    digest="$(sha256sum "$changed_plist" | awk '{print $1}')"
    printf '# external edit\n' >>"$changed_plist"
    printf '%s\n' \
        'installer_version=3' \
        'platform=darwin' \
        'daemon_backend=launchd' \
        "daemon_config_path=$changed_plist" \
        "daemon_config_sha256=$digest" \
        "mm_path=$changed_home/.local/bin/mm" \
        'core_managed=0' >"$changed_state/install-state"
    HOME="$changed_home" bash "$PROJECT_DIR/scripts/uninstall.sh" >/dev/null 2>&1
    assert_file_exists "$changed_plist"
    pass "macOS 卸载仅删除 install-state 可验证的受管 LaunchAgent"
}

test_pure_helpers
test_preflight_rejections_are_clean
test_macos_preflight_matrix
test_macos_homebrew_installs_only_missing_packages
test_path_is_idempotent
test_macos_path_uses_zshrc_idempotently
test_platform_asset_matrix
test_macos_daemon_and_state_round_trip
test_default_config_preserves_existing_file
test_go_selection_prefers_valid_system_then_managed
test_newer_core_is_not_downgraded
test_force_core_replaces_after_validation_and_keeps_backup
test_sha_failure_is_detected
test_ruleset_override_requires_trusted_digests
test_ruleset_path_rejects_symlink
test_ruleset_install_and_state_round_trip
test_ruleset_publish_failure_restores_pair
test_ruleset_download_failure_uses_only_verified_cache
test_daemon_fresh_install_starts_and_migrates
test_daemon_failure_returns_nonzero_without_follow_up
test_daemon_stopped_upgrade_restarts_but_running_upgrade_does_not
test_daemon_state_change_before_restart_is_preserved
test_daemon_without_systemd_keeps_units_and_skips_migration
test_bootstrap_uses_archive_and_cleans_up
test_default_uninstall_preserves_core_and_config
test_purge_core_requires_ownership
test_rejected_purge_keeps_ownership_state
test_malformed_path_block_is_not_truncated
test_macos_uninstall_removes_only_verified_manager_plist

printf '\n全部安装测试通过，共 %d 项。\n' "$PASSED"
