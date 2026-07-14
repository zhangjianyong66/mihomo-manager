#!/usr/bin/env bash

set -Eeuo pipefail

MM_REF="${MM_REF:-master}"
GITHUB_BASE_URL="${MM_GITHUB_BASE_URL:-https://github.com}"
REPOSITORY="zhangjianyong66/mihomo-manager"
TEMP_DIR=""

cleanup() {
    [[ -n "$TEMP_DIR" ]] && rm -rf -- "$TEMP_DIR"
}

trap cleanup EXIT

fail() {
    printf '[错误] %s\n' "$*" >&2
    exit 1
}

command -v curl >/dev/null 2>&1 || fail "远程入口需要 curl。请先安装 curl 后重试。"
command -v tar >/dev/null 2>&1 || fail "远程入口需要 tar。请先安装 tar 后重试。"

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/mihomo-manager-bootstrap.XXXXXX")"
ARCHIVE="$TEMP_DIR/source.tar.gz"
SOURCE_DIR="$TEMP_DIR/source"
mkdir -p "$SOURCE_DIR"

GITHUB_BASE_URL="${GITHUB_BASE_URL%/}"
SOURCE_URL="$GITHUB_BASE_URL/$REPOSITORY/archive/$MM_REF.tar.gz"

printf '[信息] 下载 Mihomo Manager 源码: %s\n' "$MM_REF"
if ! curl --fail --location --silent --show-error \
    --retry 3 --connect-timeout 15 \
    "$SOURCE_URL" --output "$ARCHIVE"; then
    printf '[提示] 可设置 HTTP_PROXY/HTTPS_PROXY/ALL_PROXY 或 MM_GITHUB_BASE_URL。\n' >&2
    fail "无法下载源码归档: $SOURCE_URL"
fi

tar -xzf "$ARCHIVE" -C "$SOURCE_DIR" --strip-components=1
[[ -f "$SOURCE_DIR/scripts/install.sh" ]] || fail "源码归档缺少 scripts/install.sh。"

bash "$SOURCE_DIR/scripts/install.sh" "$@"
